package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	emit "github.com/openpak/rewards-emit"
)

// SessionMetric is the key of the rewards session clock in fg_session_clock (roundd writes it).
const SessionMetric = "game_sessions.fall-guys"

// ScrubClock drops the clock keys (room NUL namespace NUL subject) that name one of the BAAS ids.
func ScrubClock(raw []byte, baas []string) (out []byte, removed int, ok bool) {
	var st emit.State
	if json.Unmarshal(raw, &st) != nil {
		return nil, 0, false
	}
	mine := func(k string) bool {
		for _, b := range baas {
			if strings.HasSuffix(k, "\x00switch\x00"+b) {
				return true
			}
		}
		return false
	}
	for k := range st.Since {
		if mine(k) {
			delete(st.Since, k)
			removed++
		}
	}
	kept := st.Emitted[:0:0]
	for _, k := range st.Emitted {
		if mine(k) {
			removed++
		} else {
			kept = append(kept, k)
		}
	}
	st.Emitted = kept
	out, err := json.Marshal(st)
	return out, removed, err == nil
}

// EraseBaas deletes the account map rows of these BAAS user ids and their entries in the session
// clock, in one transaction (card openpak/prds#120). It returns the Fall Guys account ids it
// dropped, so the caller can forget them in memory, and the number of rows removed.
func (d *DB) EraseBaas(ctx context.Context, baas []string) (accounts []string, n int, err error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `DELETE FROM fg_accounts WHERE baas_user_id = ANY($1) RETURNING account`, baas)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return nil, 0, err
		}
		accounts = append(accounts, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	n = len(accounts)
	if len(accounts) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM fg_settings WHERE account = ANY($1)`, accounts); err != nil {
			return nil, 0, err
		}
	}
	var raw string
	switch err := tx.QueryRow(ctx, `SELECT state FROM fg_session_clock WHERE metric = $1 FOR UPDATE`, SessionMetric).Scan(&raw); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, 0, err
	default:
		if out, removed, ok := ScrubClock([]byte(raw), baas); ok && removed > 0 {
			if _, err := tx.Exec(ctx, `UPDATE fg_session_clock SET state = $2, saved_at = now() WHERE metric = $1`, SessionMetric, string(out)); err != nil {
				return nil, 0, err
			}
			n += removed
		}
	}
	return accounts, n, tx.Commit(ctx)
}
