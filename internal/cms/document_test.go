package cms

import (
	"encoding/json"
	"testing"
)

func TestDocumentHasEveryTableAndAVersion(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Document(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range tables {
		v, ok := doc[k]
		if !ok {
			t.Fatalf("table %q missing: the client's parser would throw", k)
		}
		rows, ok := v.([]any)
		if !ok {
			t.Fatalf("table %q is not an array", k)
		}
		for _, r := range rows {
			if id, _ := r.(map[string]any)["id"].(string); id == "" {
				t.Fatalf("table %q has a row without a string id", k)
			}
		}
	}
	meta, _ := doc["_meta"].(map[string]any)
	if meta["_content_version"] != Version() || len(Version()) < 10 {
		t.Fatalf("version %v / %s", meta, Version())
	}
}

func TestAuthoredTablesAreKnown(t *testing.T) {
	known := map[string]bool{}
	for _, k := range tables {
		known[k] = true
	}
	for k := range authored() {
		if !known[k] {
			t.Errorf("authored table %q is not one the client parses", k)
		}
	}
}

func TestReferencesResolve(t *testing.T) {
	ids := func(table string) map[string]bool {
		m := map[string]bool{}
		for _, r := range authored()[table] {
			m[r["id"].(string)] = true
		}
		return m
	}
	a := authored()
	show := a["shows"][0]
	checks := []struct{ table, id string }{
		{"levels_episode", show["default_episode"].(string)},
		{"localised_strings", show["show_name"].(string)},
		{"levels_roundpool", a["levels_episode"][0]["roundpool"].(string)},
		{"levels_round", a["levels_episode"][0]["fallback_round"].(string)},
		{"levels_round", a["levels_roundpool"][0]["stages"].([]row)[0]["round"].(string)},
		{"game_rules", a["levels_round"][0]["game_rules"].(string)},
		{"prime_levels", a["levels_round"][0]["scene_type"].(row)["prime_level"].(string)},
		{"shows", a["matchmaking_gates"][0]["show_id"].(string)},
		{"shows", a["shows_selector_tab"][0]["tab_shows"].([]row)[0]["show"].(string)},
	}
	for _, c := range checks {
		if !ids(c.table)[c.id] {
			t.Errorf("%s has no row %q", c.table, c.id)
		}
	}
}
