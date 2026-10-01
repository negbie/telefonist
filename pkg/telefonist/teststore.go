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

// TestfileVersionRow is a stored testfile version entry.
type TestfileVersionRow struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ProjectName string    `json:"project_name"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
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

// Vacuum executes the SQLite VACUUM command to reclaim free space.
func (s *TestStore) Vacuum(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, "VACUUM;")
	return err
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

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
);
CREATE INDEX IF NOT EXISTS idx_testfile_versions_name_project ON testfile_versions(name, project_name);
`); err != nil {
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

// ---------------------------------------------------------------------------
// Validation helpers (used by multiple teststore_*.go files)
// ---------------------------------------------------------------------------

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
