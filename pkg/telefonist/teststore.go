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
	ID           int       `json:"id"`
	TestfileName string    `json:"testfile_name"`
	ProjectName  string    `json:"project_name"`
	RunNumber    int       `json:"run_number"`
	Hash         string    `json:"hash"`
	Status       string    `json:"status"`
	FlowEvents   string    `json:"flow_events,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
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

	return &TestStore{db: db, dbPath: dbPath, wavDir: wavDir}, nil
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
	return nil
}

var testfileNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

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

// Save upserts a testfile by name. Content is stored verbatim as provided.
func (s *TestStore) Save(ctx context.Context, name, projectName, content string) error {
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
	content = strings.TrimSpace(content)

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// SQLite UPSERT.
	_, err := s.db.ExecContext(ctx, `
INSERT INTO testfiles(name, project_name, content, created_at, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(name, project_name) DO UPDATE SET
  content = excluded.content,
  updated_at = excluded.updated_at;
`, name, projectName, content, now, now)
	if err != nil {
		return fmt.Errorf("save testfile %q (project %q): %w", name, projectName, err)
	}
	return nil
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

// Delete removes a stored testfile by name and project and its associated WAV files.
func (s *TestStore) Delete(ctx context.Context, name, projectName string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateTestfileName(name); err != nil {
		return err
	}

	pName := projectName
	if pName == "" {
		pName = "default"
	}
	testfileDir := filepath.Join(s.wavDir, pName, name)
	if err := os.RemoveAll(testfileDir); err != nil {
		log.Printf("failed to remove testfile wav directory %q: %v", testfileDir, err)
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM testfiles WHERE name = ? AND project_name = ?;`, name, projectName)
	if err != nil {
		return fmt.Errorf("delete testfile %q (project %q): %w", name, projectName, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("testfile %q (project %q) not found", name, projectName)
	}
	return nil
}

// Rename renames an existing testfile and its associated WAV directory.
func (s *TestStore) Rename(ctx context.Context, oldName, oldProject, newName, newProject string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateTestfileName(oldName); err != nil {
		return err
	}
	if err := validateProjectName(oldProject); err != nil {
		return err
	}
	if err := validateTestfileName(newName); err != nil {
		return err
	}
	if err := validateProjectName(newProject); err != nil {
		return err
	}
	if oldName == newName && oldProject == newProject {
		return nil
	}

	// Rename folder on disk if it exists
	oldP := oldProject
	if oldP == "" {
		oldP = "default"
	}
	newP := newProject
	if newP == "" {
		newP = "default"
	}

	oldDir := filepath.Join(s.wavDir, oldP, oldName)
	newDir := filepath.Join(s.wavDir, newP, newName)

	if _, err := os.Stat(oldDir); err == nil {
		if err := os.MkdirAll(filepath.Dir(newDir), 0755); err == nil {
			if err := os.Rename(oldDir, newDir); err != nil {
				log.Printf("failed to rename wav directory %q -> %q: %v", oldDir, newDir, err)
			}
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE testfiles
SET name = ?, project_name = ?, updated_at = ?
WHERE name = ? AND project_name = ?;
`, newName, newProject, now, oldName, oldProject)
	if err != nil {
		return fmt.Errorf("rename testfile %q -> %q: %w", oldName, newName, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("testfile %q (project %q) not found", oldName, oldProject)
	}

	// Also update file paths in testrun_wavs table if they moved
	if oldDir != newDir {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE testrun_wavs
			SET file_path = REPLACE(file_path, ?, ?)
			WHERE testrun_id IN (
				SELECT id FROM testruns WHERE testfile_name = ? AND project_name = ?
			);`,
			filepath.Join(oldP, oldName),
			filepath.Join(newP, newName),
			newName, newProject); err != nil {
			log.Printf("failed to update wav paths for renamed testfile %q (project %q) -> %q (project %q): %v", oldName, oldProject, newName, newProject, err)
		}
	}

	return nil
}

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
	if err := s.PruneRuns(ctx, 100); err != nil {
		log.Printf("failed to prune old runs after saving run %d for %q (project %q): %v", id, testfileName, projectName, err)
	}

	return id, nil
}

// SaveWav stores a WAV file for a testrun on the filesystem and its path in the database.
func (s *TestStore) SaveWav(ctx context.Context, testrunID int64, filename string, content []byte) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}

	// Lookup testfile and project name for path organization
	var testfileName, projectName string
	err := s.db.QueryRowContext(ctx, `SELECT testfile_name, project_name FROM testruns WHERE id = ?`, testrunID).Scan(&testfileName, &projectName)
	if err != nil {
		return fmt.Errorf("lookup testrun %d: %w", testrunID, err)
	}

	pName := projectName
	if pName == "" {
		pName = "default"
	}

	relPath := filepath.Join(pName, testfileName, fmt.Sprintf("%d", testrunID), filename)
	absPath := filepath.Join(s.wavDir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("create wav directory: %w", err)
	}
	if err := os.WriteFile(absPath, content, 0644); err != nil {
		return fmt.Errorf("write wav file: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = s.db.ExecContext(ctx, `
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

// GetWav returns the content and filename of a stored WAV from the filesystem.
func (s *TestStore) GetWav(ctx context.Context, id int) (filename string, content []byte, err error) {
	if s == nil || s.db == nil {
		return "", nil, errors.New("test store is not initialized")
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
			return "", nil, fmt.Errorf("wav %d not found", id)
		}
		return "", nil, fmt.Errorf("get wav %d: %w", id, err)
	}

	if testfileName != "" {
		filename = testfileName + "_" + filename
	}

	absPath := filepath.Join(s.wavDir, relPath)
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
SELECT id, testfile_name, project_name, run_number, hash, status, created_at
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
		if err := rows.Scan(&r.ID, &r.TestfileName, &r.ProjectName, &r.RunNumber, &r.Hash, &r.Status, &created); err != nil {
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
SELECT id, testfile_name, project_name, run_number, hash, status, created_at
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
		if err := rows.Scan(&r.ID, &r.TestfileName, &r.ProjectName, &r.RunNumber, &r.Hash, &r.Status, &created); err != nil {
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
SELECT id, testfile_name, project_name, run_number, hash, status, flow_events, created_at
FROM testruns
WHERE id = ?;
`, id).Scan(&r.ID, &r.TestfileName, &r.ProjectName, &r.RunNumber, &r.Hash, &r.Status, &r.FlowEvents, &created)
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
func (s *TestStore) DeleteRun(ctx context.Context, id int) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if id <= 0 {
		return errors.New("invalid testrun ID")
	}

	// Lookup testfile and project name for path organization
	var testfileName, projectName string
	err := s.db.QueryRowContext(ctx, `SELECT testfile_name, project_name FROM testruns WHERE id = ?`, id).Scan(&testfileName, &projectName)
	if err == nil {
		pName := projectName
		if pName == "" {
			pName = "default"
		}
		// Delete WAV directory for this run.
		runDir := filepath.Join(s.wavDir, pName, testfileName, fmt.Sprintf("%d", id))
		if err := os.RemoveAll(runDir); err != nil {
			log.Printf("failed to remove run wav directory %q: %v", runDir, err)
		}
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM testruns WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete testrun %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("testrun %d not found", id)
	}
	return nil
}

// DeleteRunsByTestfile removes all stored testruns for a given testfile name and project, including WAV files.
func (s *TestStore) DeleteRunsByTestfile(ctx context.Context, testfileName, projectName string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateTestfileName(testfileName); err != nil {
		return err
	}
	if err := validateProjectName(projectName); err != nil {
		return err
	}

	pName := projectName
	if pName == "" {
		pName = "default"
	}
	testfileDir := filepath.Join(s.wavDir, pName, testfileName)
	if err := os.RemoveAll(testfileDir); err != nil {
		log.Printf("failed to remove testfile wav directory %q: %v", testfileDir, err)
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM testruns WHERE testfile_name = ? AND project_name = ?;`, testfileName, projectName)
	if err != nil {
		return fmt.Errorf("delete testruns for %q (project %q): %w", testfileName, projectName, err)
	}
	return nil
}

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

	return tx.Commit()
}

// RenameProject renames an entire project, including updating all its testfiles and testruns.
func (s *TestStore) RenameProject(ctx context.Context, oldName, newName string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	if err := validateProjectName(oldName); err != nil {
		return err
	}
	if err := validateProjectName(newName); err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Printf("failed to rollback project rename transaction for %q -> %q: %v", oldName, newName, err)
		}
	}()

	// 1. Rename physical WAV folders
	oldP := oldName
	if oldP == "" {
		oldP = "default"
	}
	newP := newName
	if newP == "" {
		newP = "default"
	}

	oldDir := filepath.Join(s.wavDir, oldP)
	newDir := filepath.Join(s.wavDir, newP)

	if _, err := os.Stat(oldDir); err == nil {
		if err := os.MkdirAll(filepath.Dir(newDir), 0755); err == nil {
			if err := os.Rename(oldDir, newDir); err != nil {
				log.Printf("failed to rename project directory %q -> %q: %v", oldDir, newDir, err)
			}
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 2. Insert new project
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(name, created_at) VALUES(?, ?) ON CONFLICT(name) DO NOTHING;`, newName, now); err != nil {
		return err
	}

	// 3. Update testfiles
	if _, err := tx.ExecContext(ctx, `UPDATE testfiles SET project_name = ?, updated_at = ? WHERE project_name = ?;`, newName, now, oldName); err != nil {
		return err
	}

	// 4. Update testrun_wavs paths
	if oldDir != newDir {
		// e.g. replacing 'alpha/' with 'beta/' at the beginning of the file_path
		prefixOld := oldP + string(filepath.Separator)
		prefixNew := newP + string(filepath.Separator)
		if _, err := tx.ExecContext(ctx, `
			UPDATE testrun_wavs
			SET file_path = ? || SUBSTR(file_path, ?)
			WHERE file_path LIKE ?;
		`, prefixNew, len(prefixOld)+1, prefixOld+"%"); err != nil {
			log.Printf("failed to update testrun wav file paths: %v", err)
		}
	}

	// 5. Update testruns (done later to prevent FK cascade issues if any)
	if _, err := tx.ExecContext(ctx, `UPDATE testruns SET project_name = ? WHERE project_name = ?;`, newName, oldName); err != nil {
		return err
	}

	// 6. Delete old project entry
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE name = ?;`, oldName); err != nil {
		return err
	}

	return tx.Commit()
}

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
	return out, nil
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
func (s *TestStore) PruneRuns(ctx context.Context, keep int) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM testruns
		WHERE id NOT IN (
			SELECT id FROM testruns
			ORDER BY created_at DESC
			LIMIT ?
		);`, keep)
	return err
}
