package store

// audit.go: append-only audit log (schema in docs/audit-log.md) and the
// stable deploy ID. Append-only is enforced at the code level: no update
// or delete methods exist for audit_log, and the table intentionally has
// no FK to flags so history outlives deleted resources.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// AuditEntry is one append-only mutation record.
type AuditEntry struct {
	ID        int64  `json:"id"`
	TS        string `json:"ts"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Resource  string `json:"resource"`
	Key       string `json:"key"`
	Env       string `json:"env,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// PutAudit appends one row. TS is set by the store (UTC RFC 3339) so
// callers cannot forge timestamps retroactively.
func (s *Store) PutAudit(ctx context.Context, e AuditEntry) error {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log(ts, actor, action, resource, key, env, before, after, request_id)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TS, e.Actor, e.Action, e.Resource, e.Key, nullIfEmpty(e.Env),
		nullIfEmpty(e.Before), nullIfEmpty(e.After), nullIfEmpty(e.RequestID))
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// ListAudit returns the most recent `limit` entries, newest last
// (append order — SIEM consumers diff sequentially).
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	// Inner query takes the newest `limit` rows; outer query restores
	// ascending append order. A plain `ORDER BY id ASC LIMIT ?` would
	// freeze the window on the oldest page forever.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ts, actor, action, resource, key, env, before, after, request_id
		 FROM (SELECT id, ts, actor, action, resource, key, env, before, after, request_id
		       FROM audit_log ORDER BY id DESC LIMIT ?)
		 ORDER BY id ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var env, before, after, req sql.NullString
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Resource, &e.Key, &env, &before, &after, &req); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		e.Env, e.Before, e.After, e.RequestID = env.String, before.String, after.String, req.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// EnsureDeployID returns the stable instance identifier, creating it on
// first call. Survives restarts; used by the evidence loop to distinguish
// real weekly-active deploys from repeated curls of one healthz.
func (s *Store) EnsureDeployID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM instance_meta WHERE key = 'deploy_id'").Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("read deploy id: %w", err)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate deploy id: %w", err)
	}
	// UUID v4 shape: 4 marks version, variant bits set.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	id = fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO instance_meta(key, value) VALUES('deploy_id', ?) ON CONFLICT(key) DO NOTHING", id); err != nil {
		return "", fmt.Errorf("persist deploy id: %w", err)
	}
	// Re-read: a concurrent writer may have won the race.
	err = s.db.QueryRowContext(ctx, "SELECT value FROM instance_meta WHERE key = 'deploy_id'").Scan(&id)
	if err != nil {
		return "", fmt.Errorf("re-read deploy id: %w", err)
	}
	return id, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
