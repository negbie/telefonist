package telefonist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Artifact paths are recorded independently of display names. Legacy paths
// remain readable; new files live under immutable run IDs.
func (s *TestStore) artifactPath(rel string) (string, error) {
	if rel == "." || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("unsafe artifact path %q", rel)
	}
	root, err := filepath.Abs(s.wavDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, rel)
	// Reject symlinks in every component, including the root. A recording
	// directory is private application storage, not a user-selected link tree.
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink in artifact path")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return path, nil
}

func (s *TestStore) purgeArtifacts(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT file_path FROM artifact_deletions`)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			break
		}
		paths = append(paths, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, rel := range paths {
		path, err := s.artifactPath(rel)
		if err != nil {
			return err
		}
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err = s.db.ExecContext(ctx, `DELETE FROM artifact_deletions WHERE file_path=?`, rel); err != nil {
			return err
		}
	}
	return nil
}

// Queue file removal in the same transaction as metadata deletion. On a crash
// or disk error, startup retries the persistent queue.
func queueRunArtifacts(ctx context.Context, tx *sql.Tx, where string, args ...any) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO artifact_deletions(file_path)
	SELECT file_path FROM testrun_wavs WHERE testrun_id IN (SELECT id FROM testruns WHERE `+where+`)`, args...)
	return err
}

func (s *TestStore) Delete(ctx context.Context, name, project string) error {
	if err := validateTestfileName(name); err != nil {
		return err
	}
	if err := validateProjectName(project); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = queueRunArtifacts(ctx, tx, "testfile_name=? AND project_name=?", name, project); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM testfiles WHERE name=? AND project_name=?`, name, project)
	if err != nil {
		return err
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return errors.New("testfile not found")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM testfile_versions WHERE name=? AND project_name=?`, name, project); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM cronjobs WHERE testfile=? AND project=?`, name, project); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.purgeArtifacts(ctx)
}

func (s *TestStore) Rename(ctx context.Context, oldName, oldProject, newName, newProject string) error {
	for _, n := range []string{oldName, newName} {
		if err := validateTestfileName(n); err != nil {
			return err
		}
	}
	for _, p := range []string{oldProject, newProject} {
		if err := validateProjectName(p); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE testfiles SET name=?,project_name=? WHERE name=? AND project_name=?`, newName, newProject, oldName, oldProject)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("testfile not found")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE testfile_versions SET name=?,project_name=? WHERE name=? AND project_name=?`, newName, newProject, oldName, oldProject); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cronjobs SET testfile=?,project=? WHERE testfile=? AND project=?`, newName, newProject, oldName, oldProject); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TestStore) RenameProject(ctx context.Context, oldName, newName string) error {
	for _, p := range []string{oldName, newName} {
		if err := validateProjectName(p); err != nil {
			return err
		}
		if p == "" {
			return errors.New("project name required")
		}
	}
	if oldName == newName {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE projects SET name=? WHERE name=?`, newName, oldName)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("project not found")
	}
	for _, q := range []string{`UPDATE testfiles SET project_name=? WHERE project_name=?`, `UPDATE testfile_versions SET project_name=? WHERE project_name=?`, `UPDATE cronjobs SET project=? WHERE project=?`} {
		if _, err = tx.ExecContext(ctx, q, newName, oldName); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *TestStore) deleteRuns(ctx context.Context, where string, args ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = queueRunArtifacts(ctx, tx, where, args...); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM testruns WHERE `+where, args...); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.purgeArtifacts(ctx)
}

func (s *TestStore) DeleteRun(ctx context.Context, id int) error {
	if id <= 0 {
		return errors.New("invalid run ID")
	}
	return s.deleteRuns(ctx, "id=?", id)
}
func (s *TestStore) DeleteRunsByTestfile(ctx context.Context, name, project string) error {
	if err := validateTestfileName(name); err != nil {
		return err
	}
	if err := validateProjectName(project); err != nil {
		return err
	}
	return s.deleteRuns(ctx, "testfile_name=? AND project_name=?", name, project)
}

func (s *TestStore) PruneRuns(ctx context.Context, keep int) error {
	if keep < 1 {
		return errors.New("retention must keep at least one run")
	}
	return s.deleteRuns(ctx, `status NOT IN ('RUNNING','QUEUED') AND id IN (
	SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY project_name,testfile_name ORDER BY id DESC) AS ordinal
	FROM testruns WHERE status NOT IN ('RUNNING','QUEUED')) WHERE ordinal>?)`, keep)
}

func validArtifactName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00")
}
