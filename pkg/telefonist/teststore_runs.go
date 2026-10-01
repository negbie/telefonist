package telefonist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

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

	now := time.Now().Format(time.RFC3339Nano)

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
		r.CreatedAt = scanTime(created, fmt.Sprintf("testrun created_at for run %d", r.ID))
		if r.FinishedAt != "" {
			r.FinishedAt = scanTime(r.FinishedAt, fmt.Sprintf("testrun finished_at for run %d", r.ID)).Format(time.RFC3339Nano)
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
		r.CreatedAt = scanTime(created, fmt.Sprintf("testrun created_at for run %d", r.ID))
		if r.FinishedAt != "" {
			r.FinishedAt = scanTime(r.FinishedAt, fmt.Sprintf("testrun finished_at for run %d", r.ID)).Format(time.RFC3339Nano)
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

	r.CreatedAt = scanTime(created, fmt.Sprintf("testrun created_at for run %d", r.ID))
	if r.FinishedAt != "" {
		r.FinishedAt = scanTime(r.FinishedAt, fmt.Sprintf("testrun finished_at for run %d", r.ID)).Format(time.RFC3339Nano)
	}
	return r, nil
}

// WavRow represents a stored WAV metadata.
type WavRow struct {
	ID        int       `json:"id"`
	TestrunID int       `json:"testrun_id"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
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

func (s *TestStore) prepareWavPath(testrunID int64, _testfileName, _projectName, filename string) (string, string, error) {
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
	now := time.Now().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO testrun_wavs(testrun_id, filename, file_path, created_at)
	VALUES(?, ?, ?, ?);
	`, testrunID, filename, relPath, now)
	if err != nil {
		return fmt.Errorf("save wav path %q for testrun %d: %w", filename, testrunID, err)
	}
	return nil
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
		r.CreatedAt = scanTime(created, fmt.Sprintf("wav created_at for wav %d", r.ID))
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


