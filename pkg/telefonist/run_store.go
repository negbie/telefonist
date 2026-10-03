package telefonist

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func (s *TestStore) BeginRun(ctx context.Context, name, project, source, batch string, repeat int) (int64, error) {
	if err := validateTestfileName(name); err != nil {
		return 0, err
	}
	if err := validateProjectName(project); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Format(time.RFC3339Nano)
	// Inline execution of a new file creates its identity, but never overwrites
	// an existing saved script with the editor's unsaved content.
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO testfiles(name,project_name,content,created_at,updated_at) VALUES(?,?,?,?,?)`, name, project, SanitizeString(source), now, now); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO testruns(testfile_name,project_name,run_number,hash,status,flow_events,created_at,source,batch_id) VALUES(?,?,?,'','RUNNING','',?,?,?)`, name, project, repeat, now, SanitizeString(source), batch)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *TestStore) FinishRun(ctx context.Context, id int64, status, reason, hash, events, script string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE testruns SET status=?,failure_reason=?,hash=?,flow_events=?,script=?,finished_at=? WHERE id=?`, status, SanitizeString(reason), hash, events, script, time.Now().Format(time.RFC3339Nano), id)
	return err
}

func copyArtifact(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err = os.Stat(dst); err == nil {
		return fmt.Errorf("artifact already exists: %s", filepath.Base(dst))
	}
	out, err := os.CreateTemp(filepath.Dir(dst), ".recording-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
