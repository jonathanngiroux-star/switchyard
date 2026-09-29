// Package store implements the SQLite-backed flag store. Pure-Go driver
// (modernc.org/sqlite, no cgo) so the single-binary promise holds.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/switchyard/switchyard/internal/model"
	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers as "sqlite"
)

// ErrNotFound is returned when a flag key does not exist.
var ErrNotFound = errors.New("not found")

// defaultProject is the v0.1 single-project home for flags. Multi-project
// arrives with the importer write path (W5–7).
const defaultProject = "default"

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path, applies the schema,
// and seeds the default project and environments. Pragmas ride in the DSN so
// every pooled connection gets them: FK enforcement (cascades depend on it),
// busy timeout, WAL.
func Open(path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying handle.
func (s *Store) Close() error { return s.db.Close() }

// SchemaVersion reports PRAGMA user_version — 1 after initial migration.
func (s *Store) SchemaVersion(_ context.Context) (int, error) {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

func (s *Store) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(Schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO projects(key, name) VALUES(?, 'Default')`, defaultProject); err != nil {
		return fmt.Errorf("seed project: %w", err)
	}
	for i, key := range []string{"dev", "staging", "production"} {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO environments(key, name, sort_order) VALUES(?, ?, ?)`, key, key, i); err != nil {
			return fmt.Errorf("seed environment %s: %w", key, err)
		}
	}
	if _, err := tx.Exec("PRAGMA user_version = 1"); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return tx.Commit()
}

// PutFlag upserts a flag and its per-environment configuration.
func (s *Store) PutFlag(ctx context.Context, f model.Flag) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin put flag: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO flags(key, project_key, name, kind) VALUES(?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET name = excluded.name, kind = excluded.kind`,
		f.Key, defaultProject, f.Name, string(f.Kind)); err != nil {
		return fmt.Errorf("upsert flag %s: %w", f.Key, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM flag_environments WHERE flag_key = ?", f.Key); err != nil {
		return fmt.Errorf("clear envs for %s: %w", f.Key, err)
	}
	for envKey, fe := range f.Environments {
		rollout, err := encodeRollout(fe.Rollout)
		if err != nil {
			return err
		}
		rules, err := encodeRules(fe.Rules)
		if err != nil {
			return err
		}
		enabled := 0
		if fe.On {
			enabled = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO flag_environments(flag_key, env_key, enabled, rollout, rules) VALUES(?, ?, ?, ?, ?)`,
			f.Key, envKey, enabled, rollout, rules); err != nil {
			return fmt.Errorf("insert env %s/%s: %w", f.Key, envKey, err)
		}
	}
	return tx.Commit()
}

// GetFlag returns one flag by key, or ErrNotFound.
func (s *Store) GetFlag(ctx context.Context, key string) (model.Flag, error) {
	f := model.Flag{Key: key, Environments: map[string]model.FlagEnvironment{}}
	err := s.db.QueryRowContext(ctx, "SELECT name, kind FROM flags WHERE key = ?", key).
		Scan(&f.Name, &f.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Flag{}, ErrNotFound
	}
	if err != nil {
		return model.Flag{}, fmt.Errorf("get flag %s: %w", key, err)
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT env_key, enabled, rollout, rules FROM flag_environments WHERE flag_key = ?", key)
	if err != nil {
		return model.Flag{}, fmt.Errorf("get envs for %s: %w", key, err)
	}
	defer rows.Close()
	for rows.Next() {
		var envKey string
		var enabled int
		var rollout, rules sql.NullString
		if err := rows.Scan(&envKey, &enabled, &rollout, &rules); err != nil {
			return model.Flag{}, fmt.Errorf("scan env row: %w", err)
		}
		fe := model.FlagEnvironment{On: enabled == 1}
		if err := decodeRollout(rollout, &fe); err != nil {
			return model.Flag{}, err
		}
		if err := decodeRules(rules, &fe); err != nil {
			return model.Flag{}, err
		}
		f.Environments[envKey] = fe
	}
	return f, rows.Err()
}

// ListFlags returns all flags ordered by key.
func (s *Store) ListFlags(ctx context.Context) ([]model.Flag, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, name, kind FROM flags ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close()
	flags := []model.Flag{}
	for rows.Next() {
		var f model.Flag
		if err := rows.Scan(&f.Key, &f.Name, &f.Kind); err != nil {
			return nil, fmt.Errorf("scan flag: %w", err)
		}
		f.Environments = map[string]model.FlagEnvironment{}
		flags = append(flags, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	envRows, err := s.db.QueryContext(ctx,
		"SELECT flag_key, env_key, enabled, rollout, rules FROM flag_environments")
	if err != nil {
		return nil, fmt.Errorf("list envs: %w", err)
	}
	defer envRows.Close()
	byKey := map[string]*model.Flag{}
	for i := range flags {
		byKey[flags[i].Key] = &flags[i]
	}
	for envRows.Next() {
		var flagKey, envKey string
		var enabled int
		var rollout, rules sql.NullString
		if err := envRows.Scan(&flagKey, &envKey, &enabled, &rollout, &rules); err != nil {
			return nil, fmt.Errorf("scan env list row: %w", err)
		}
		f, ok := byKey[flagKey]
		if !ok {
			continue
		}
		fe := model.FlagEnvironment{On: enabled == 1}
		if err := decodeRollout(rollout, &fe); err != nil {
			return nil, err
		}
		if err := decodeRules(rules, &fe); err != nil {
			return nil, err
		}
		f.Environments[envKey] = fe
	}
	return flags, envRows.Err()
}

// DeleteFlag removes a flag (environments cascade). ErrNotFound if absent.
func (s *Store) DeleteFlag(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM flags WHERE key = ?", key)
	if err != nil {
		return fmt.Errorf("delete flag %s: %w", key, err)
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

func encodeRollout(r *model.Rollout) (sql.NullString, error) {
	if r == nil {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode rollout: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func decodeRollout(ns sql.NullString, fe *model.FlagEnvironment) error {
	if !ns.Valid || strings.TrimSpace(ns.String) == "" || ns.String == "null" {
		return nil
	}
	var r model.Rollout
	if err := json.Unmarshal([]byte(ns.String), &r); err != nil {
		return fmt.Errorf("decode rollout %q: %w", ns.String, err)
	}
	fe.Rollout = &r
	return nil
}

func encodeRules(rules []model.Rule) (sql.NullString, error) {
	if len(rules) == 0 {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode rules: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func decodeRules(ns sql.NullString, fe *model.FlagEnvironment) error {
	if !ns.Valid || strings.TrimSpace(ns.String) == "" || ns.String == "null" {
		return nil
	}
	var rules []model.Rule
	if err := json.Unmarshal([]byte(ns.String), &rules); err != nil {
		return fmt.Errorf("decode rules %q: %w", ns.String, err)
	}
	fe.Rules = rules
	return nil
}
