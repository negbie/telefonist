package telefonist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// ErrSaveConflict is returned when a save fails due to a concurrent edit.
var ErrSaveConflict = errors.New("file changed on server; reload and merge your unsaved edits")

// Save upserts a testfile by name and stores a new version if changed.
func (s *TestStore) Save(ctx context.Context, name, projectName, content string) error {
	return s.SaveExpected(ctx, name, projectName, content, nil)
}

// SaveExpected upserts a testfile and optionally checks a revision for optimistic concurrency.
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
	if expected != nil && *expected != "" && revision != *expected {
		t1, e1 := time.Parse(time.RFC3339Nano, revision)
		t2, e2 := time.Parse(time.RFC3339Nano, *expected)
		if e1 != nil || e2 != nil || !t1.Equal(t2) {
			return ErrSaveConflict
		}
	}
	nowTime := time.Now()
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

	// SQLite UPSERT parent testfile first.
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

	if err != nil || lastContent != content {
		_, err = tx.ExecContext(ctx, `
INSERT INTO testfile_versions(name, project_name, content, created_at)
VALUES(?, ?, ?, ?);
`, name, projectName, content, now)
		if err != nil {
			return err
		}
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
	r.CreatedAt = scanTime(created, fmt.Sprintf("testfile created_at for %q (project %q)", name, projectName))
	r.UpdatedAt = scanTime(updated, fmt.Sprintf("testfile updated_at for %q (project %q)", name, projectName))

	return r, nil
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
		r.CreatedAt = scanTime(created, "testfile version created_at")
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
		r.CreatedAt = scanTime(created, fmt.Sprintf("testfile created_at for %q (project %q)", r.Name, r.ProjectName))
		r.UpdatedAt = scanTime(updated, fmt.Sprintf("testfile updated_at for %q (project %q)", r.Name, r.ProjectName))
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rows: %w", err)
	}
	return out, nil
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

	now := time.Now().Format(time.RFC3339Nano)

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

// Ensure unused imports don't cause issues — the log import is needed transitively
// via scanTime but the compiler needs to see direct usage in this file.
var _ = log.Printf
