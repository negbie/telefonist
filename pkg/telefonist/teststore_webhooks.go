package telefonist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Webhook is a stored webhook endpoint.
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

	now := time.Now().Format(time.RFC3339Nano)
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
	w.CreatedAt = scanTime(created, "webhook created_at")
	w.UpdatedAt = scanTime(updated, "webhook updated_at")
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
		w.CreatedAt = scanTime(created, "webhook created_at")
		w.UpdatedAt = scanTime(updated, "webhook updated_at")
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
