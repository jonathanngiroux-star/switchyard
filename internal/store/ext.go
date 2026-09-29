package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/switchyard/switchyard/internal/model"
)

// SetFlagEnvironment upserts a single flag's configuration for one
// environment, leaving other environments untouched. Unknown flag returns
// ErrNotFound; unknown environment fails on the foreign key (the caller
// decides how to surface it).
func (s *Store) SetFlagEnvironment(ctx context.Context, flagKey, envKey string, fe model.FlagEnvironment) error {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM flags WHERE key = ?", flagKey).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lookup flag %s: %w", flagKey, err)
	}
	rollout, err := encodeRollout(fe.Rollout)
	if err != nil {
		return err
	}
	rules, err := encodeRules(fe.Rules)
	if err != nil {
		return err
	}
	value, err := encodeValue(fe.Value)
	if err != nil {
		return err
	}
	enabled := 0
	if fe.On {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO flag_environments(flag_key, env_key, enabled, rollout, rules, value)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(flag_key, env_key) DO UPDATE SET
		   enabled = excluded.enabled, rollout = excluded.rollout, rules = excluded.rules, value = excluded.value`,
		flagKey, envKey, enabled, rollout, rules, value)
	if err != nil {
		return fmt.Errorf("upsert env %s/%s: %w", flagKey, envKey, err)
	}
	return nil
}

// PutSegment upserts a segment definition.
func (s *Store) PutSegment(ctx context.Context, seg model.Segment) error {
	rules, err := encodeRules(seg.Rules)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO segments(key, project_key, name, rules) VALUES(?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET name = excluded.name, rules = excluded.rules`,
		seg.Key, defaultProject, seg.Name, rules); err != nil {
		return fmt.Errorf("upsert segment %s: %w", seg.Key, err)
	}
	return nil
}

// GetSegment returns one segment by key, or ErrNotFound.
func (s *Store) GetSegment(ctx context.Context, key string) (model.Segment, error) {
	seg := model.Segment{Key: key}
	var name string
	var rules sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT name, rules FROM segments WHERE key = ?", key).
		Scan(&name, &rules)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Segment{}, ErrNotFound
	}
	if err != nil {
		return model.Segment{}, fmt.Errorf("get segment %s: %w", key, err)
	}
	seg.Name = name
	if err := decodeRulesInto(rules, &seg.Rules); err != nil {
		return model.Segment{}, err
	}
	return seg, nil
}

// ListSegments returns all segments ordered by key.
func (s *Store) ListSegments(ctx context.Context) ([]model.Segment, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, name, rules FROM segments ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("list segments: %w", err)
	}
	defer rows.Close()
	segs := []model.Segment{}
	for rows.Next() {
		var seg model.Segment
		var rules sql.NullString
		if err := rows.Scan(&seg.Key, &seg.Name, &rules); err != nil {
			return nil, fmt.Errorf("scan segment: %w", err)
		}
		if err := decodeRulesInto(rules, &seg.Rules); err != nil {
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, rows.Err()
}

// ListEnvironments returns environments in their defined order.
func (s *Store) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, name FROM environments ORDER BY sort_order")
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	defer rows.Close()
	envs := []model.Environment{}
	for rows.Next() {
		var e model.Environment
		if err := rows.Scan(&e.Key, &e.Name); err != nil {
			return nil, fmt.Errorf("scan environment: %w", err)
		}
		envs = append(envs, e)
	}
	return envs, rows.Err()
}

// decodeRulesInto decodes a JSON rules column into a rule slice pointer,
// treating NULL/empty as no rules.
func decodeRulesInto(ns sql.NullString, dst *[]model.Rule) error {
	if !ns.Valid || ns.String == "" || ns.String == "null" {
		*dst = nil
		return nil
	}
	var rules []model.Rule
	if err := json.Unmarshal([]byte(ns.String), &rules); err != nil {
		return fmt.Errorf("decode rules %q: %w", ns.String, err)
	}
	*dst = rules
	return nil
}
