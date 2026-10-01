package telefonist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

	now := time.Now().Format(time.RFC3339Nano)
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
