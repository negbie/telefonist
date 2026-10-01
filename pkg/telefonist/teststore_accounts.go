package telefonist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SIPAccount is a centralized SIP account containing credentials.
type SIPAccount struct {
	Name       string    `json:"name"`
	SIPURI     string    `json:"sip_uri"`
	Password   string    `json:"password"`
	URIParams  string    `json:"uri_params"`
	AddrParams string    `json:"addr_params"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SaveSIPAccount inserts or updates a SIP account, supporting renaming.
func (s *TestStore) SaveSIPAccount(ctx context.Context, oldName, name, sipURI, password, uriParams, addrParams string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("account name is required")
	}
	sipURI = strings.TrimSpace(sipURI)
	if sipURI == "" {
		return errors.New("SIP URI is required")
	}

	uriParams = NormalizeParamString(uriParams)
	addrParams = NormalizeParamString(addrParams)

	now := time.Now().Format(time.RFC3339Nano)

	// Handle renaming or creation checks
	oldName = strings.TrimSpace(oldName)
	if oldName == "" {
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sip_accounts WHERE name = ?);", name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check duplicate name: %w", err)
		}
		if exists {
			return fmt.Errorf("an account named %q already exists", name)
		}
	} else if oldName != name {
		var exists bool
		err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sip_accounts WHERE name = ?);", name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check rename conflict: %w", err)
		}
		if exists {
			return fmt.Errorf("an account named %q already exists", name)
		}

		_, err = s.db.ExecContext(ctx, `
UPDATE sip_accounts
SET name = ?, sip_uri = ?, password = ?, uri_params = ?, addr_params = ?, updated_at = ?
WHERE name = ?;`, name, sipURI, password, uriParams, addrParams, now, oldName)
		if err != nil {
			return fmt.Errorf("rename SIP account: %w", err)
		}
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO sip_accounts(name, sip_uri, password, uri_params, addr_params, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  sip_uri = excluded.sip_uri,
  password = excluded.password,
  uri_params = excluded.uri_params,
  addr_params = excluded.addr_params,
  updated_at = excluded.updated_at;
`, name, sipURI, password, uriParams, addrParams, now, now)
	if err != nil {
		return fmt.Errorf("save SIP account: %w", err)
	}
	return nil
}

// DeleteSIPAccount removes a SIP account by name.
func (s *TestStore) DeleteSIPAccount(ctx context.Context, name string) error {
	if s == nil || s.db == nil {
		return errors.New("test store is not initialized")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sip_accounts WHERE name = ?;`, name)
	if err != nil {
		return fmt.Errorf("delete SIP account: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("SIP account %q not found", name)
	}
	return nil
}

// ListSIPAccounts returns all stored SIP accounts.
func (s *TestStore) ListSIPAccounts(ctx context.Context) ([]SIPAccount, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT name, sip_uri, password, uri_params, addr_params, created_at, updated_at
FROM sip_accounts
ORDER BY name ASC;
`)
	if err != nil {
		return nil, fmt.Errorf("list SIP accounts: %w", err)
	}
	defer rows.Close()

	var out []SIPAccount
	for rows.Next() {
		var a SIPAccount
		var created, updated string
		if err := rows.Scan(&a.Name, &a.SIPURI, &a.Password, &a.URIParams, &a.AddrParams, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan SIP account row: %w", err)
		}
		a.CreatedAt = scanTime(created, "SIP account created_at")
		a.UpdatedAt = scanTime(updated, "SIP account updated_at")
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list SIP accounts rows: %w", err)
	}
	return out, nil
}

// GetSIPAccount retrieves a single SIP account by name.
func (s *TestStore) GetSIPAccount(ctx context.Context, name string) (*SIPAccount, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("test store is not initialized")
	}

	var a SIPAccount
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT name, sip_uri, password, uri_params, addr_params, created_at, updated_at
FROM sip_accounts
WHERE name = ?;
`, name).Scan(&a.Name, &a.SIPURI, &a.Password, &a.URIParams, &a.AddrParams, &created, &updated)
	if err != nil {
		return nil, err
	}

	a.CreatedAt = scanTime(created, "SIP account created_at")
	a.UpdatedAt = scanTime(updated, "SIP account updated_at")
	return &a, nil
}
