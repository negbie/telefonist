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

	now := time.Now().Format(time.RFC3339Nano)

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

// DeleteProject removes a project. Test files in it are moved to "".
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

	now := time.Now().Format(time.RFC3339Nano)

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
		r.CreatedAt = scanTime(created, "project created_at")
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects rows: %w", err)
	}
	return out, nil
}
