package telefonist

import (
	"context"
	"errors"
	"fmt"
	"time"
)

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
		r.CreatedAt = scanTime(created, fmt.Sprintf("cronjob created_at for job %d", r.ID))
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

	now := time.Now().Format(time.RFC3339Nano)
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
