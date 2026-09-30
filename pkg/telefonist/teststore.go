package telefonist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// TestStore is a small SQLite-backed store for persisted "testfiles" (named case lists).
// The database file is created next to the running executable.
type TestStore struct {
	db     *sql.DB
	dbPath string
	wavDir string
}

// TestfileRow is a stored testfile entry.
type TestfileRow struct {
	Name        string    `json:"name"`
	ProjectName string    `json:"project"`
	Content     string    `json:"content,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectRow is a stored project entry.
type ProjectRow struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TestRunRow is a stored test run entry.
type TestRunRow struct {
	ID            int       `json:"id"`
	TestfileName  string    `json:"testfile_name"`
	ProjectName   string    `json:"project_name"`
	RunNumber     int       `json:"run_number"`
	Hash          string    `json:"hash"`
	Status        string    `json:"status"`
	FlowEvents    string    `json:"flow_events,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	FailureReason string    `json:"failure_reason,omitempty"`
	Source        string    `json:"source,omitempty"`
	Script        string    `json:"script,omitempty"`
	BatchID       string    `json:"batch_id,omitempty"`
	FinishedAt    string    `json:"finished_at,omitempty"`
}

// CronJobRow is a stored cron job entry.
type CronJobRow struct {
	ID        int       `json:"id"`
	Project   string    `json:"project"`
	Testfile  string    `json:"testfile"` // empty implies whole project
	CronExpr  string    `json:"cron_expr"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// SIPAccount is a centralized SIP account containing credentials.
type SIPAccount struct {
	Name       string    `json:"name"`
	SIPURI     string    `json:"sip_uri"`
	Password   string    `json:"password"`
	URIParams  string    `json:"uri_params"`
	AddrParams string    `json:"addr_params"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// OpenTestStore opens (and initializes) the SQLite database at <dataDir>/telefonist_tests.db.
func OpenTestStore(ctx context.Context, dataDir string) (*TestStore, error) {
	if dataDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable path: %w", err)
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			// Not fatal; keep the original path.
		}
		dataDir = filepath.Dir(exe)
	}
	dbPath := filepath.Join(dataDir, "telefonist_tests.db")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0700); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	db.SetMaxOpenConns(1)

	wavDir := filepath.Join(dataDir, "recorded_wavs")
	if err := os.MkdirAll(wavDir, 0755); err != nil {
		db.Close()
		return nil, fmt.Errorf("create wav directory: %w", err)
	}

	scriptsDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		db.Close()
		return nil, fmt.Errorf("create scripts directory: %w", err)
	}

	if err := migrateTestStore(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	if err := os.Chmod(dbPath, 0600); err != nil {
		db.Close()
		return nil, err
	}
	s := &TestStore{db: db, dbPath: dbPath, wavDir: wavDir}
	if err := s.purgeArtifacts(ctx); err != nil {
		log.Printf("pending artifact cleanup: %v", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *TestStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the database path on disk.
func (s *TestStore) Path() string { return s.dbPath }

func migrateTestStore(ctx context.Context, db *sql.DB) error {
	// 1. Create projects table
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  name TEXT PRIMARY KEY,
  created_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create projects table: %w", err)
	}

	// 2. Create testfiles table
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS testfiles (
  name TEXT NOT NULL,
  project_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(name, project_name)
);`); err != nil {
		return fmt.Errorf("create testfiles table: %w", err)
	}

	// 3. Check if migration for testruns is needed (Foreign Key check)
	var schema string
	err := db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='testruns'").Scan(&schema)
	if err == nil {
		if !strings.Contains(strings.ToLower(schema), "references testfiles") {
			// Migration needed!
			if _, err := db.ExecContext(ctx, `
BEGIN TRANSACTION;
-- 1. Clean up orphan runs that would violate the new FK constraint
DELETE FROM testruns
WHERE NOT EXISTS (
  SELECT 1 FROM testfiles
  WHERE testfiles.name = testruns.testfile_name
    AND testfiles.project_name = testruns.project_name
);

-- 2. Create the new table
CREATE TABLE testruns_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  testfile_name TEXT NOT NULL,
  project_name TEXT NOT NULL DEFAULT '',
  run_number INTEGER NOT NULL,
  hash TEXT NOT NULL,
  status TEXT NOT NULL,
  flow_events TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(testfile_name, project_name) REFERENCES testfiles(name, project_name) ON UPDATE CASCADE ON DELETE CASCADE
);

-- 3. Copy data
INSERT INTO testruns_new (id, testfile_name, project_name, run_number, hash, status, flow_events, created_at)
SELECT id, testfile_name, project_name, run_number, hash, status, flow_events, created_at FROM testruns;

-- 4. Replace old table
DROP TABLE testruns;
ALTER TABLE testruns_new RENAME TO testruns;
CREATE INDEX idx_testruns_testfile_project ON testruns(testfile_name, project_name);
COMMIT;
`); err != nil {
				return fmt.Errorf("migrate testruns table: %w", err)
			}
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		// table doesn't exist, create it anew
		if _, err := db.ExecContext(ctx, `
CREATE TABLE testruns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  testfile_name TEXT NOT NULL,
  project_name TEXT NOT NULL DEFAULT '',
  run_number INTEGER NOT NULL,
  hash TEXT NOT NULL,
  status TEXT NOT NULL,
  flow_events TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(testfile_name, project_name) REFERENCES testfiles(name, project_name) ON UPDATE CASCADE ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_testruns_testfile_project ON testruns(testfile_name, project_name);
`); err != nil {
			return fmt.Errorf("create testruns table: %w", err)
		}
	} else {
		return fmt.Errorf("check testruns schema: %w", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_testruns_created_at ON testruns(created_at);`); err != nil {
		return fmt.Errorf("create testruns created_at index: %w", err)
	}

	// 5. Create testrun_wavs table (path-based)
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS testrun_wavs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  testrun_id INTEGER NOT NULL,
  filename TEXT NOT NULL,
  file_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(testrun_id) REFERENCES testruns(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_testrun_wavs_testrun_id ON testrun_wavs(testrun_id);
`); err != nil {
		return fmt.Errorf("create testrun_wavs table: %w", err)
	}

	// 6. Create cronjobs table
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS cronjobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL,
	testfile TEXT NOT NULL DEFAULT '',
	cron_expr TEXT NOT NULL,
	active BOOLEAN NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL
);
	`); err != nil {
		return fmt.Errorf("create cronjobs table: %w", err)
	}

	// 7. Create sip_accounts table
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS sip_accounts (
  name TEXT PRIMARY KEY,
  sip_uri TEXT NOT NULL,
  password TEXT NOT NULL,
  uri_params TEXT NOT NULL DEFAULT '',
  addr_params TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create sip_accounts table: %w", err)
	}

	// 8. Migration: Alter sip_accounts to add uri_params and addr_params columns if missing
	var saSchema string
	err = db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='sip_accounts'").Scan(&saSchema)
	if err == nil {
		saSchemaLower := strings.ToLower(saSchema)
		if !strings.Contains(saSchemaLower, "uri_params") {
			if _, err := db.ExecContext(ctx, "ALTER TABLE sip_accounts ADD COLUMN uri_params TEXT NOT NULL DEFAULT '';"); err != nil {
				return fmt.Errorf("alter sip_accounts table add uri_params: %w", err)
			}
		}
		if !strings.Contains(saSchemaLower, "addr_params") {
			if _, err := db.ExecContext(ctx, "ALTER TABLE sip_accounts ADD COLUMN addr_params TEXT NOT NULL DEFAULT '';"); err != nil {
				return fmt.Errorf("alter sip_accounts table add addr_params: %w", err)
			}
		}
	}

	// 9. Create testfile_versions table
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS testfile_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  project_name TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create testfile_versions table: %w", err)
	}

	// 10. Create webhooks table
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS webhooks (
  alias TEXT PRIMARY KEY,
  url TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create webhooks table: %w", err)
	}

	// 11. Create app_settings table for small global UI/runtime settings.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create app_settings table: %w", err)
	}
	return migrateRunDetails(ctx, db)
}

var testfileNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validateTestfileName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("testfile name is required")
	}
	if !testfileNameRE.MatchString(name) {
		return fmt.Errorf("invalid testfile name %q (allowed: [A-Za-z0-9._-], length 1-64)", name)
	}
	return nil
}

func validateProjectName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil // Empty project name is allowed (default)
	}
	if !testfileNameRE.MatchString(name) {
		return fmt.Errorf("invalid project name %q (allowed: [A-Za-z0-9._-], length 1-64)", name)
	}
	return nil
}

// Save upserts a testfile by name and stores a new version if changed.
func (s *TestStore) Save(ctx context.Context, name, projectName, content string) error {
	return s.SaveExpected(ctx, name, projectName, content, nil)
}

var ErrSaveConflict = errors.New("file changed on server; reload and merge your unsaved edits")

func (s *TestStore) SaveExpected(ctx context.Context, name, projectName, content string, expected *string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateTestfileName(name); err != nil {
		return err
	}
	if err := validateProjectName(projectName); err != nil {
		return err
	}
	projectName = strings.TrimSpace(projectName)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision string
	err = tx.QueryRowContext(ctx, `SELECT updated_at FROM testfiles WHERE name=? AND project_name=?`, name, projectName).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if expected != nil && revision != *expected {
		return ErrSaveConflict
	}
	nowTime := time.Now().UTC()
	if previous, e := time.Parse(time.RFC3339Nano, revision); e == nil && !nowTime.After(previous) {
		nowTime = previous.Add(time.Nanosecond)
	}
	now := nowTime.Format(time.RFC3339Nano)

	// Check if content has changed from the latest version to prevent redundant entries
	var lastContent string
	err = tx.QueryRowContext(ctx, `
SELECT content FROM testfile_versions
WHERE name = ? AND project_name = ?
ORDER BY id DESC LIMIT 1;
`, name, projectName).Scan(&lastContent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if err != nil || lastContent != content {
		_, err = tx.ExecContext(ctx, `
INSERT INTO testfile_versions(name, project_name, content, created_at)
VALUES(?, ?, ?, ?);
`, name, projectName, content, now)
		if err != nil {
			return err
		}
	}

	// SQLite UPSERT.
	_, err = tx.ExecContext(ctx, `
INSERT INTO testfiles(name, project_name, content, created_at, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(name, project_name) DO UPDATE SET
  content = excluded.content,
  updated_at = excluded.updated_at;
`, name, projectName, content, now, now)
	if err != nil {
		return fmt.Errorf("save testfile %q (project %q): %w", name, projectName, err)
	}
	return tx.Commit()
}

// Load returns the stored testfile row by name and project.
func (s *TestStore) Load(ctx context.Context, name, projectName string) (TestfileRow, error) {
	if s == nil || s.db == nil {
		return TestfileRow{}, errors.New("test store is not initialized")
	}
	if err := validateTestfileName(name); err != nil {
		return TestfileRow{}, err
	}
	if err := validateProjectName(projectName); err != nil {
		return TestfileRow{}, err
	}

	var r TestfileRow
	var created, updated string

	err := s.db.QueryRowContext(ctx, `
SELECT name, project_name, content, created_at, updated_at
FROM testfiles
WHERE name = ? AND project_name = ?;
`, name, projectName).Scan(&r.Name, &r.ProjectName, &r.Content, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TestfileRow{}, fmt.Errorf("testfile %q (project %q) not found", name, projectName)
		}
		return TestfileRow{}, fmt.Errorf("load testfile %q (project %q): %w", name, projectName, err)
	}

	// Parse times defensively (if parse fails, keep zero time).
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		log.Printf("failed to parse testfile created_at %q for %q (project %q): %v", created, name, projectName, err)
	}
	if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		log.Printf("failed to parse testfile updated_at %q for %q (project %q): %v", updated, name, projectName, err)
	}

	return r, nil
}

type TestfileVersionRow struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ProjectName string    `json:"project_name"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListVersions returns all stored versions for a testfile by name and project.
func (s *TestStore) ListVersions(ctx context.Context, name, projectName string) ([]TestfileVersionRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}
	if err := validateTestfileName(name); err != nil {
		return nil, err
	}
	if err := validateProjectName(projectName); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, project_name, content, created_at
FROM testfile_versions
WHERE name = ? AND project_name = ?
ORDER BY id DESC;
`, name, projectName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []TestfileVersionRow
	for rows.Next() {
		var r TestfileVersionRow
		var created string
		if err := rows.Scan(&r.ID, &r.Name, &r.ProjectName, &r.Content, &created); err != nil {
			return nil, err
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse testfile version created_at: %v", err)
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list versions rows: %w", err)
	}
	return list, nil
}

// DeleteVersion deletes a specific testfile version by ID.
func (s *TestStore) DeleteVersion(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM testfile_versions WHERE id = ?;", id)
	return err
}

// Delete removes a stored testfile by name and project and its associated WAV files.
// List returns all stored testfiles with metadata (no content, unless includeContent is true).
func (s *TestStore) List(ctx context.Context, includeContent bool) ([]TestfileRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	var rows *sql.Rows
	var err error
	if includeContent {
		rows, err = s.db.QueryContext(ctx, `
SELECT name, project_name, content, created_at, updated_at
FROM testfiles
ORDER BY name ASC;
`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT name, project_name, '' as content, created_at, updated_at
FROM testfiles
ORDER BY name ASC;
`)
	}
	if err != nil {
		return nil, fmt.Errorf("list testfiles: %w", err)
	}
	defer rows.Close()

	var out []TestfileRow
	for rows.Next() {
		var r TestfileRow
		var created, updated string
		if err := rows.Scan(&r.Name, &r.ProjectName, &r.Content, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan list row: %w", err)
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse testfile created_at %q while listing %q (project %q): %v", created, r.Name, r.ProjectName, err)
		}
		if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			log.Printf("failed to parse testfile updated_at %q while listing %q (project %q): %v", updated, r.Name, r.ProjectName, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rows: %w", err)
	}
	return out, nil
}

// ListCronJobs returns all stored cron jobs.
func (s *TestStore) ListCronJobs(ctx context.Context) ([]CronJobRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, project, testfile, cron_expr, active, created_at
FROM cronjobs
ORDER BY id ASC;
`)
	if err != nil {
		return nil, fmt.Errorf("list cronjobs: %w", err)
	}
	defer rows.Close()

	var out []CronJobRow
	for rows.Next() {
		var r CronJobRow
		var created string
		if err := rows.Scan(&r.ID, &r.Project, &r.Testfile, &r.CronExpr, &r.Active, &created); err != nil {
			return nil, fmt.Errorf("scan cronjob row: %w", err)
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse cronjob created_at %q for job %d: %v", created, r.ID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list cronjobs rows: %w", err)
	}
	return out, nil
}

// SaveCronJob inserts a new cron job.
func (s *TestStore) SaveCronJob(ctx context.Context, project, testfile, cronExpr string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("test store is not initialized")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO cronjobs(project, testfile, cron_expr, active, created_at)
VALUES(?, ?, ?, 1, ?);
`, project, testfile, cronExpr, now)
	if err != nil {
		return 0, fmt.Errorf("save cronjob: %w", err)
	}
	return res.LastInsertId()
}

// DeleteCronJob removes a cron job.
func (s *TestStore) DeleteCronJob(ctx context.Context, id int) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM cronjobs WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete cronjob: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cronjob %d not found", id)
	}
	return nil
}

// ToggleCronJob toggles the active status of a cron job.
func (s *TestStore) ToggleCronJob(ctx context.Context, id int, active bool) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE cronjobs SET active = ? WHERE id = ?;`, active, id)
	if err != nil {
		return fmt.Errorf("toggle cronjob: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cronjob %d not found", id)
	}
	return nil
}

// SaveSIPAccount inserts or updates a SIP account, supporting renaming.
func (s *TestStore) SaveSIPAccount(ctx context.Context, oldName, name, sipURI, password, uriParams, addrParams string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("account name is required")
	}
	sipURI = strings.TrimSpace(sipURI)
	if sipURI == "" {
		return errors.New("SIP URI is required")
	}

	uriParams = normalizeParamString(uriParams)
	addrParams = normalizeParamString(addrParams)

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Handle renaming or creation checks
	oldName = strings.TrimSpace(oldName)
	if oldName == "" {
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sip_accounts WHERE name = ?);", name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check duplicate name: %w", err)
		}
		if exists {
			return fmt.Errorf("an account named %q already exists", name)
		}
	} else if oldName != name {
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sip_accounts WHERE name = ?);", name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check rename conflict: %w", err)
		}
		if exists {
			return fmt.Errorf("an account named %q already exists", name)
		}

		_, err = s.db.ExecContext(ctx, `
UPDATE sip_accounts
SET name = ?, sip_uri = ?, password = ?, uri_params = ?, addr_params = ?, updated_at = ?
WHERE name = ?;`, name, sipURI, password, uriParams, addrParams, now, oldName)
		if err != nil {
			return fmt.Errorf("rename SIP account: %w", err)
		}
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO sip_accounts(name, sip_uri, password, uri_params, addr_params, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  sip_uri = excluded.sip_uri,
  password = excluded.password,
  uri_params = excluded.uri_params,
  addr_params = excluded.addr_params,
  updated_at = excluded.updated_at;
`, name, sipURI, password, uriParams, addrParams, now, now)
	if err != nil {
		return fmt.Errorf("save SIP account: %w", err)
	}
	return nil
}

func normalizeParamString(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	for strings.Contains(p, ";;") {
		p = strings.ReplaceAll(p, ";;", ";")
	}
	if !strings.HasPrefix(p, ";") {
		p = ";" + p
	}
	p = strings.TrimRight(p, "; ")
	return p
}

// DeleteSIPAccount removes a SIP account by name.
func (s *TestStore) DeleteSIPAccount(ctx context.Context, name string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sip_accounts WHERE name = ?;`, name)
	if err != nil {
		return fmt.Errorf("delete SIP account: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("SIP account %q not found", name)
	}
	return nil
}

// ListSIPAccounts returns all stored SIP accounts.
func (s *TestStore) ListSIPAccounts(ctx context.Context) ([]SIPAccount, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT name, sip_uri, password, uri_params, addr_params, created_at, updated_at
FROM sip_accounts
ORDER BY name ASC;
`)
	if err != nil {
		return nil, fmt.Errorf("list SIP accounts: %w", err)
	}
	defer rows.Close()

	var out []SIPAccount
	for rows.Next() {
		var a SIPAccount
		var created, updated string
		if err := rows.Scan(&a.Name, &a.SIPURI, &a.Password, &a.URIParams, &a.AddrParams, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan SIP account row: %w", err)
		}
		if a.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse SIP account created_at %q: %v", created, err)
		}
		if a.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			log.Printf("failed to parse SIP account updated_at %q: %v", updated, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list SIP accounts rows: %w", err)
	}
	return out, nil
}

// GetSIPAccount retrieves a single SIP account by name.
func (s *TestStore) GetSIPAccount(ctx context.Context, name string) (*SIPAccount, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	var a SIPAccount
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT name, sip_uri, password, uri_params, addr_params, created_at, updated_at
FROM sip_accounts
WHERE name = ?;
`, name).Scan(&a.Name, &a.SIPURI, &a.Password, &a.URIParams, &a.AddrParams, &created, &updated)
	if err != nil {
		return nil, err
	}

	if a.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		log.Printf("failed to parse SIP account created_at %q: %v", created, err)
	}
	if a.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		log.Printf("failed to parse SIP account updated_at %q: %v", updated, err)
	}
	return &a, nil
}

type Webhook struct {
	Alias     string    `json:"alias"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SaveWebhook inserts or updates a webhook, supporting renaming.
func (s *TestStore) SaveWebhook(ctx context.Context, oldAlias, alias, url string, enabled bool) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}

	alias = strings.TrimSpace(alias)
	if alias == "" {
		return errors.New("webhook alias is required")
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("webhook URL is required")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}

	oldAlias = strings.TrimSpace(oldAlias)
	if oldAlias == "" {
		// New Webhook: Check for unique alias
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM webhooks WHERE alias = ?);", alias).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check duplicate webhook alias: %w", err)
		}
		if exists {
			return fmt.Errorf("a webhook with alias %q already exists", alias)
		}
	} else if oldAlias != alias {
		// Renaming: Check for rename conflict
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM webhooks WHERE alias = ?);", alias).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check rename webhook conflict: %w", err)
		}
		if exists {
			return fmt.Errorf("a webhook with alias %q already exists", alias)
		}

		_, err = s.db.ExecContext(ctx, `
UPDATE webhooks
SET alias = ?, url = ?, enabled = ?, updated_at = ?
WHERE alias = ?;`, alias, url, enabledInt, now, oldAlias)
		if err != nil {
			return fmt.Errorf("rename webhook: %w", err)
		}
		return nil
	}

	// Normal save/upsert
	_, err := s.db.ExecContext(ctx, `
INSERT INTO webhooks(alias, url, enabled, created_at, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(alias) DO UPDATE SET
  url = excluded.url,
  enabled = excluded.enabled,
  updated_at = excluded.updated_at;
`, alias, url, enabledInt, now, now)
	if err != nil {
		return fmt.Errorf("save webhook: %w", err)
	}
	return nil
}

// GetWebhook retrieves a single webhook by alias.
func (s *TestStore) GetWebhook(ctx context.Context, alias string) (*Webhook, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	var w Webhook
	var created, updated string
	var enabledInt int
	err := s.db.QueryRowContext(ctx, `
SELECT alias, url, enabled, created_at, updated_at
FROM webhooks
WHERE alias = ?;
`, alias).Scan(&w.Alias, &w.URL, &enabledInt, &created, &updated)
	if err != nil {
		return nil, err
	}

	w.Enabled = (enabledInt != 0)
	if w.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		log.Printf("failed to parse webhook created_at %q: %v", created, err)
	}
	if w.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		log.Printf("failed to parse webhook updated_at %q: %v", updated, err)
	}
	return &w, nil
}

// ListWebhooks returns all stored webhooks.
func (s *TestStore) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT alias, url, enabled, created_at, updated_at
FROM webhooks
ORDER BY alias ASC;
`)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()

	var out []Webhook
	for rows.Next() {
		var w Webhook
		var created, updated string
		var enabledInt int
		if err := rows.Scan(&w.Alias, &w.URL, &enabledInt, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan webhook row: %w", err)
		}
		w.Enabled = (enabledInt != 0)
		if w.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse webhook created_at %q: %v", created, err)
		}
		if w.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			log.Printf("failed to parse webhook updated_at %q: %v", updated, err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webhooks rows: %w", err)
	}
	return out, nil
}

// DeleteWebhook deletes a webhook by alias.
func (s *TestStore) DeleteWebhook(ctx context.Context, alias string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}

	res, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE alias = ?;", alias)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("webhook with alias %q not found", alias)
	}
	return nil
}

// SaveRun stores an executed test run into the database and returns the new run ID.
func (s *TestStore) SaveRun(ctx context.Context, testfileName, projectName string, runNumber int, hash, status, flowEvents string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("test store is not initialized")
	}
	if err := validateTestfileName(testfileName); err != nil {
		return 0, err
	}
	if err := validateProjectName(projectName); err != nil {
		return 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	res, err := s.db.ExecContext(ctx, `
INSERT INTO testruns(testfile_name, project_name, run_number, hash, status, flow_events, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?);
`, testfileName, projectName, runNumber, hash, status, flowEvents, now)
	if err != nil {
		return 0, fmt.Errorf("save testrun for %q (project %q): %w", testfileName, projectName, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Prune old runs to prevent boundless database growth
	keep := 100
	if value, err := s.GetSetting(ctx, "retention_runs"); err == nil {
		if n, e := strconv.Atoi(value); e == nil && n > 0 {
			keep = n
		}
	}
	if err := s.PruneRuns(ctx, keep); err != nil {
		log.Printf("failed to prune old runs after saving run %d for %q (project %q): %v", id, testfileName, projectName, err)
	}

	return id, nil
}

func (s *TestStore) getRunWavLocation(ctx context.Context, testrunID int64) (testfileName string, projectName string, err error) {
	if s == nil || s.db == nil {
		return "", "", errors.New("test store is not initialized")
	}
	err = s.db.QueryRowContext(ctx, `SELECT testfile_name, project_name FROM testruns WHERE id = ?`, testrunID).Scan(&testfileName, &projectName)
	if err != nil {
		return "", "", fmt.Errorf("lookup testrun %d: %w", testrunID, err)
	}
	return testfileName, projectName, nil
}

// MoveWavWithLocation moves a WAV file from srcPath to the testrun's persistent path
// and records its metadata in the database without loading the file into memory.
func (s *TestStore) MoveWavWithLocation(ctx context.Context, testrunID int64, testfileName, projectName, filename, srcPath string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}

	relPath, absPath, err := s.prepareWavPath(testrunID, testfileName, projectName, filename)
	if err != nil {
		return err
	}
	// Copy first: metadata failure must never remove the only recording.
	if err := copyArtifact(srcPath, absPath); err != nil {
		return err
	}
	if err := s.recordWav(ctx, testrunID, filename, relPath); err != nil {
		_ = os.Remove(absPath)
		return err
	}
	return os.Remove(srcPath)
}

// MoveWav moves a WAV file from srcPath to the testrun's persistent path.
func (s *TestStore) MoveWav(ctx context.Context, testrunID int64, filename, srcPath string) error {
	testfileName, projectName, err := s.getRunWavLocation(ctx, testrunID)
	if err != nil {
		return err
	}
	return s.MoveWavWithLocation(ctx, testrunID, testfileName, projectName, filename, srcPath)
}

// SaveWav stores in-memory WAV content for a testrun on the filesystem and in the database.
func (s *TestStore) SaveWav(ctx context.Context, testrunID int64, filename string, content []byte) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	testfileName, projectName, err := s.getRunWavLocation(ctx, testrunID)
	if err != nil {
		return err
	}

	relPath, absPath, err := s.prepareWavPath(testrunID, testfileName, projectName, filename)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(content)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(absPath)
		return fmt.Errorf("write wav file: %w", err)
	}
	if err = s.recordWav(ctx, testrunID, filename, relPath); err != nil {
		_ = os.Remove(absPath)
	}
	return err
}

func (s *TestStore) prepareWavPath(testrunID int64, testfileName, projectName, filename string) (string, string, error) {
	if testrunID <= 0 || !validArtifactName(filename) {
		return "", "", errors.New("invalid artifact identity")
	}
	relPath := filepath.Join("runs", fmt.Sprintf("%d", testrunID), filename)
	absPath, err := s.artifactPath(relPath)
	if err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", "", fmt.Errorf("create wav directory: %w", err)
	}
	return relPath, absPath, nil
}

func (s *TestStore) recordWav(ctx context.Context, testrunID int64, filename, relPath string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO testrun_wavs(testrun_id, filename, file_path, created_at)
	VALUES(?, ?, ?, ?);
	`, testrunID, filename, relPath, now)
	if err != nil {
		return fmt.Errorf("save wav path %q for testrun %d: %w", filename, testrunID, err)
	}
	return nil
}

// WavRow represents a stored WAV metadata.
type WavRow struct {
	ID        int       `json:"id"`
	TestrunID int       `json:"testrun_id"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
}

// ListWavs returns all WAV metadata for a particular testrun.
func (s *TestStore) ListWavs(ctx context.Context, testrunID int64) ([]WavRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}
	if testrunID <= 0 {
		return nil, errors.New("invalid testrun ID")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, testrun_id, filename, created_at
FROM testrun_wavs
WHERE testrun_id = ?
ORDER BY id ASC;
`, testrunID)
	if err != nil {
		return nil, fmt.Errorf("list wavs for testrun %d: %w", testrunID, err)
	}
	defer rows.Close()

	var out []WavRow
	for rows.Next() {
		var r WavRow
		var created string
		if err := rows.Scan(&r.ID, &r.TestrunID, &r.Filename, &created); err != nil {
			return nil, fmt.Errorf("scan wav row: %w", err)
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse wav created_at %q for wav %d: %v", created, r.ID, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetWavPath returns the display filename and absolute file path of a stored WAV.
func (s *TestStore) GetWavPath(ctx context.Context, id int) (filename string, absPath string, err error) {
	if s == nil || s.db == nil {
		return "", "", errors.New("test store is not initialized")
	}

	var relPath, testfileName string
	err = s.db.QueryRowContext(ctx, `
SELECT w.filename, w.file_path, r.testfile_name
FROM testrun_wavs w
JOIN testruns r ON w.testrun_id = r.id
WHERE w.id = ?;
`, id).Scan(&filename, &relPath, &testfileName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("wav %d not found", id)
		}
		return "", "", fmt.Errorf("get wav %d: %w", id, err)
	}

	if testfileName != "" {
		filename = testfileName + "_" + filename
	}

	absPath, err = s.artifactPath(relPath)
	return filename, absPath, err
}

// GetWav returns the content and filename of a stored WAV from the filesystem.
func (s *TestStore) GetWav(ctx context.Context, id int) (filename string, content []byte, err error) {
	filename, absPath, err := s.GetWavPath(ctx, id)
	if err != nil {
		return "", nil, err
	}
	content, err = os.ReadFile(absPath)
	if err != nil {
		return filename, nil, fmt.Errorf("read wav file: %w", err)
	}
	return filename, content, nil
}

// ListRuns returns all stored testruns for a particular testfile and project (metadata only, no flow_events).
func (s *TestStore) ListRuns(ctx context.Context, testfileName, projectName string) ([]TestRunRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}
	if err := validateTestfileName(testfileName); err != nil {
		return nil, err
	}
	if err := validateProjectName(projectName); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, testfile_name, project_name, run_number, hash, status, created_at, failure_reason, finished_at, batch_id
FROM testruns
WHERE testfile_name = ? AND project_name = ?
ORDER BY id ASC;
`, testfileName, projectName)
	if err != nil {
		return nil, fmt.Errorf("list testruns for %q (project %q): %w", testfileName, projectName, err)
	}
	defer rows.Close()

	var out []TestRunRow
	for rows.Next() {
		var r TestRunRow
		var created string
		if err := rows.Scan(&r.ID, &r.TestfileName, &r.ProjectName, &r.RunNumber, &r.Hash, &r.Status, &created, &r.FailureReason, &r.FinishedAt, &r.BatchID); err != nil {
			return nil, fmt.Errorf("scan testrun row: %w", err)
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse testrun created_at %q for run %d: %v", created, r.ID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list testruns rows: %w", err)
	}
	return out, nil
}

// ListAllRuns returns all stored testruns regardless of testfile (no flow_events).
func (s *TestStore) ListAllRuns(ctx context.Context) ([]TestRunRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, testfile_name, project_name, run_number, hash, status, created_at, failure_reason, finished_at, batch_id
FROM testruns
ORDER BY id ASC;
`)
	if err != nil {
		return nil, fmt.Errorf("list all testruns: %w", err)
	}
	defer rows.Close()

	var out []TestRunRow
	for rows.Next() {
		var r TestRunRow
		var created string
		if err := rows.Scan(&r.ID, &r.TestfileName, &r.ProjectName, &r.RunNumber, &r.Hash, &r.Status, &created, &r.FailureReason, &r.FinishedAt, &r.BatchID); err != nil {
			return nil, fmt.Errorf("scan testrun row: %w", err)
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse testrun created_at %q for run %d: %v", created, r.ID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all testruns rows: %w", err)
	}
	return out, nil
}

// GetRun returns a specific stored testrun including flow_events by its ID.
func (s *TestStore) GetRun(ctx context.Context, id int) (TestRunRow, error) {
	if s == nil || s.db == nil {
		return TestRunRow{}, errors.New("test store is not initialized")
	}
	if id <= 0 {
		return TestRunRow{}, errors.New("invalid testrun ID")
	}

	var r TestRunRow
	var created string

	err := s.db.QueryRowContext(ctx, `
SELECT id, testfile_name, project_name, run_number, hash, status, flow_events, created_at, failure_reason,source,script,batch_id,finished_at
FROM testruns
WHERE id = ?;
`, id).Scan(&r.ID, &r.TestfileName, &r.ProjectName, &r.RunNumber, &r.Hash, &r.Status, &r.FlowEvents, &created, &r.FailureReason, &r.Source, &r.Script, &r.BatchID, &r.FinishedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TestRunRow{}, fmt.Errorf("testrun %d not found", id)
		}
		return TestRunRow{}, fmt.Errorf("get testrun %d: %w", id, err)
	}

	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		log.Printf("failed to parse testrun created_at %q for run %d: %v", created, r.ID, err)
	}
	return r, nil
}

// DeleteRun removes a single stored testrun by its ID and its associated WAV files.
// SaveProject upserts a project by name.
func (s *TestStore) SaveProject(ctx context.Context, name string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateProjectName(name); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("project name is required")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := s.db.ExecContext(ctx, `
INSERT INTO projects(name, created_at)
VALUES(?, ?)
ON CONFLICT(name) DO NOTHING;
`, name, now)
	if err != nil {
		return fmt.Errorf("save project %q: %w", name, err)
	}
	return nil
}

// DeleteProject removes a project. Test files in it are moved to ”.
func (s *TestStore) DeleteProject(ctx context.Context, name string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateProjectName(name); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Printf("failed to rollback project delete transaction for %q: %v", name, err)
		}
	}()

	if _, err := tx.ExecContext(ctx, `UPDATE testfiles SET project_name = '' WHERE project_name = ?;`, name); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE name = ?;`, name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE testfile_versions SET project_name='' WHERE project_name=?`, name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cronjobs WHERE project=?`, name); err != nil {
		return err
	}

	return tx.Commit()
}

// RenameProject renames an entire project, including updating all its testfiles and testruns.
// CloneProject creates a new project and copies all testfiles from the source project.
func (s *TestStore) CloneProject(ctx context.Context, srcName, targetName string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateProjectName(srcName); err != nil {
		return err
	}
	if err := validateProjectName(targetName); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Printf("failed to rollback project clone transaction for %q -> %q: %v", srcName, targetName, err)
		}
	}()

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 1. Create target project
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(name, created_at) VALUES(?, ?) ON CONFLICT(name) DO NOTHING;`, targetName, now); err != nil {
		return err
	}

	// 2. Insert testfiles from src into target
	// Uses INSERT OR IGNORE so if a testfile already exists in the target, it won't crash
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO testfiles(name, project_name, content, created_at, updated_at)
		SELECT name, ?, content, ?, ? FROM testfiles WHERE project_name = ?;
	`, targetName, now, now, srcName); err != nil {
		return err
	}

	return tx.Commit()
}

// CloneTestfile creates a new testfile by copying the content of the source testfile within the same project.
func (s *TestStore) CloneTestfile(ctx context.Context, projectName, srcName, targetName string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateProjectName(projectName); err != nil {
		return err
	}
	if err := validateTestfileName(srcName); err != nil {
		return err
	}
	if err := validateTestfileName(targetName); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Copy the testfile row only, no history.
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO testfiles(name, project_name, content, created_at, updated_at)
		SELECT ?, project_name, content, ?, ? FROM testfiles WHERE name = ? AND project_name = ?;
	`, targetName, now, now, srcName, projectName)
	if err != nil {
		return fmt.Errorf("clone testfile %q -> %q (project %q): %w", srcName, targetName, projectName, err)
	}

	return nil
}

// ListProjects returns all stored projects.
func (s *TestStore) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `SELECT name, created_at FROM projects ORDER BY name ASC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectRow
	for rows.Next() {
		var r ProjectRow
		var created string
		if err := rows.Scan(&r.Name, &created); err != nil {
			return nil, err
		}
		if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			log.Printf("failed to parse project created_at %q for project %q: %v", created, r.Name, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects rows: %w", err)
	}
	return out, nil
}

// GetSetting returns a global app setting value by key. Missing settings return an empty string.
func (s *TestStore) GetSetting(ctx context.Context, key string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("test store is not initialized")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("setting key is required")
	}

	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?;`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

// SaveSetting upserts a global app setting value.
func (s *TestStore) SaveSetting(ctx context.Context, key, value string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("setting key is required")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO app_settings(key, value, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
  value = excluded.value,
  updated_at = excluded.updated_at;
`, key, value, now)
	if err != nil {
		return fmt.Errorf("save setting %q: %w", key, err)
	}
	return nil
}

// GetLogFilters returns the persisted global log filter substrings.
func (s *TestStore) GetLogFilters(ctx context.Context) ([]string, error) {
	value, err := s.GetSetting(ctx, logFiltersSettingKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return normalizeLogFilters(strings.Split(value, "\n")), nil
}

// SaveLogFilters persists global log filter substrings.
func (s *TestStore) SaveLogFilters(ctx context.Context, filters []string) error {
	filters = normalizeLogFilters(filters)
	return s.SaveSetting(ctx, logFiltersSettingKey, strings.Join(filters, "\n"))
}

// Vacuum executes the SQLite VACUUM command to reclaim free space.
func (s *TestStore) Vacuum(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, "VACUUM;")
	return err
}

// PruneRuns removes old test runs, keeping only the most recent N runs.
