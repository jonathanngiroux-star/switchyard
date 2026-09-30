package store

// scim.go: SCIM 2.0 identity storage (users and groups). The cloud tier's
// IdP sync writes here; the SCIM HTTP endpoint (internal/scim, feature-
// flagged in serve) reads and writes here. Self-hosters get the schema in
// every binary — the v1.0 procurement requirement ("SCIM by v1.0") is a
// schema + skeleton contract, not a cloud-only feature.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/switchyard/switchyard/internal/model"
)

// SCIMUser is a provisioned user record.
type SCIMUser struct {
	ID          string `json:"id"`
	UserName    string `json:"userName"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	Active      bool   `json:"active"`
	Raw         string `json:"raw,omitempty"` // original SCIM payload
}

// SCIMGroup is a provisioned group with member user IDs.
type SCIMGroup struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Members     []string `json:"members,omitempty"`
	Raw         string   `json:"raw,omitempty"`
}

func (s *Store) PutSCIMUser(ctx context.Context, u SCIMUser) error {
	active := 0
	if u.Active {
		active = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scim_users(id, user_name, display_name, email, active, raw)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   user_name = excluded.user_name, display_name = excluded.display_name,
		   email = excluded.email, active = excluded.active, raw = excluded.raw`,
		u.ID, u.UserName, u.DisplayName, u.Email, active, u.Raw)
	if err != nil {
		return fmt.Errorf("upsert scim user %s: %w", u.ID, err)
	}
	return nil
}

func (s *Store) GetSCIMUser(ctx context.Context, id string) (SCIMUser, error) {
	var u SCIMUser
	var active int
	err := s.db.QueryRowContext(ctx,
		"SELECT id, user_name, display_name, email, active, raw FROM scim_users WHERE id = ?", id).
		Scan(&u.ID, &u.UserName, &u.DisplayName, &u.Email, &active, &u.Raw)
	if errors.Is(err, sql.ErrNoRows) {
		return SCIMUser{}, ErrNotFound
	}
	if err != nil {
		return SCIMUser{}, fmt.Errorf("get scim user %s: %w", id, err)
	}
	u.Active = active == 1
	return u, nil
}

func (s *Store) ListSCIMUsers(ctx context.Context) ([]SCIMUser, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, user_name, display_name, email, active, raw FROM scim_users ORDER BY user_name")
	if err != nil {
		return nil, fmt.Errorf("list scim users: %w", err)
	}
	defer rows.Close()
	users := []SCIMUser{}
	for rows.Next() {
		var u SCIMUser
		var active int
		if err := rows.Scan(&u.ID, &u.UserName, &u.DisplayName, &u.Email, &active, &u.Raw); err != nil {
			return nil, fmt.Errorf("scan scim user: %w", err)
		}
		u.Active = active == 1
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) DeleteSCIMUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM scim_users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete scim user %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) PutSCIMGroup(ctx context.Context, g SCIMGroup) error {
	members, err := json.Marshal(g.Members)
	if err != nil {
		return fmt.Errorf("encode members: %w", err)
	}
	if g.Members == nil {
		members = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO scim_groups(id, display_name, members, raw)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   display_name = excluded.display_name, members = excluded.members, raw = excluded.raw`,
		g.ID, g.DisplayName, string(members), g.Raw)
	if err != nil {
		return fmt.Errorf("upsert scim group %s: %w", g.ID, err)
	}
	return nil
}

func (s *Store) GetSCIMGroup(ctx context.Context, id string) (SCIMGroup, error) {
	var g SCIMGroup
	var members string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, display_name, members, raw FROM scim_groups WHERE id = ?", id).
		Scan(&g.ID, &g.DisplayName, &members, &g.Raw)
	if errors.Is(err, sql.ErrNoRows) {
		return SCIMGroup{}, ErrNotFound
	}
	if err != nil {
		return SCIMGroup{}, fmt.Errorf("get scim group %s: %w", id, err)
	}
	if err := json.Unmarshal([]byte(members), &g.Members); err != nil {
		return SCIMGroup{}, fmt.Errorf("decode members for %s: %w", id, err)
	}
	return g, nil
}

func (s *Store) DeleteSCIMGroup(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM scim_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete scim group %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// openRaw opens a raw database/sql handle for migration-test seeding.
func openRaw(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
}

// unused import guard while the model import rides along in future slices.
var _ = model.KindBoolean
