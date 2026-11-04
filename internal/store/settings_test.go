package store

import (
	"context"
	"os"
	"testing"
)

// Needs a scratch Postgres: FALL_GUYS_TEST_DATABASE_URL; skipped otherwise.
func TestSettingsSurviveRestart(t *testing.T) {
	url := os.Getenv("FALL_GUYS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FALL_GUYS_TEST_DATABASE_URL unset")
	}
	ctx := context.Background()
	open := func() *DB {
		d, err := Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	a := open()
	a.pool.Exec(ctx, `DELETE FROM fg_settings WHERE account = 'acct-test'`)
	if _, err := a.MergeSettings(ctx, "acct-test", map[string]any{"x": "1", "y": "2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MergeSettings(ctx, "acct-test", map[string]any{"y": "3"}); err != nil {
		t.Fatal(err)
	}
	a.Close()
	b := open() // a restart: a new pool on the same database
	defer b.Close()
	v, err := b.Settings(ctx, "acct-test")
	if err != nil || v["x"] != "1" || v["y"] != "3" {
		t.Fatalf("after restart: %v %v", v, err)
	}
	if err := b.PutAccount(ctx, "acct-test", "baas-test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.EraseBaas(ctx, []string{"baas-test"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.Settings(ctx, "acct-test"); len(v) != 0 {
		t.Fatalf("settings survived erasure: %v", v)
	}
	b.pool.Exec(ctx, `DELETE FROM fg_settings WHERE account = 'acct-test'`)
}
