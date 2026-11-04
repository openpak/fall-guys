// Package store is Fall Guys' durable state in Postgres (FALL_GUYS_DATABASE_URL): the account -> BAAS
// user id map (written by catapultd at login, read by roundd) and the rewards session clock (roundd).
// The pool is capped at two connections.
package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `CREATE TABLE IF NOT EXISTS fg_accounts (
	account      text PRIMARY KEY,
	baas_user_id text NOT NULL,
	last_seen    timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS fg_session_clock (
	metric   text PRIMARY KEY,
	state    text NOT NULL,
	saved_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS fg_settings (
	account  text PRIMARY KEY,
	settings jsonb NOT NULL DEFAULT '{}',
	saved_at timestamptz NOT NULL DEFAULT now()
)`

type DB struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{pool}, nil
}

func (d *DB) Close() { d.pool.Close() }

func (d *DB) PutAccount(ctx context.Context, account, baas string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO fg_accounts (account, baas_user_id, last_seen) VALUES ($1,$2,now())
		ON CONFLICT (account) DO UPDATE SET baas_user_id = EXCLUDED.baas_user_id, last_seen = now()`, account, baas)
	return err
}

// Account returns the BAAS user id of an account, "" when unknown.
func (d *DB) Account(ctx context.Context, account string) (string, error) {
	var baas string
	err := d.pool.QueryRow(ctx, `SELECT baas_user_id FROM fg_accounts WHERE account = $1`, account).Scan(&baas)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return baas, err
}

// The clock is stored as text: its keys contain NUL bytes, which jsonb refuses.
func (d *DB) SaveClock(ctx context.Context, metric string, raw []byte) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO fg_session_clock (metric, state, saved_at) VALUES ($1,$2,now())
		ON CONFLICT (metric) DO UPDATE SET state = EXCLUDED.state, saved_at = now()`, metric, string(raw))
	return err
}

func (d *DB) LoadClock(ctx context.Context, metric string) ([]byte, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx, `SELECT state FROM fg_session_clock WHERE metric = $1`, metric).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

// Settings returns an account's game settings ({} when it has none).
func (d *DB) Settings(ctx context.Context, account string) (map[string]any, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx, `SELECT settings FROM fg_settings WHERE account = $1`, account).Scan(&raw)
	out := map[string]any{}
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MergeSettings merges values into an account's settings (top-level keys replace) and returns the result.
func (d *DB) MergeSettings(ctx context.Context, account string, values map[string]any) (map[string]any, error) {
	in, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = d.pool.QueryRow(ctx, `INSERT INTO fg_settings (account, settings) VALUES ($1, $2::jsonb)
		ON CONFLICT (account) DO UPDATE SET settings = fg_settings.settings || EXCLUDED.settings, saved_at = now()
		RETURNING settings`, account, string(in)).Scan(&raw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	return out, json.Unmarshal(raw, &out)
}
