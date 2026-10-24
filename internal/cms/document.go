// Package cms builds the content document Fall Guys downloads after login: the "CMS" that
// lists every show, round, cosmetic and setting the client offers.
//
// The document is authored here from the client's own parser (field names, enum names and
// reference rules in docs/protocol.md). Nothing in it is copied from Epic's live content: one
// show with one race round, the settings OpenPak needs, and every other table empty.
//
// Format, as the client reads it (MiniJSON):
//   - one JSON object; "_meta" holds "_content_version" (logged and compared, must be set);
//   - one key per table, each an array of rows, every row with a string "id";
//   - a field that names another row holds that row's id ("table.id" works as well: the client
//     keeps what follows the first dot);
//   - nested structures are objects, enums are their member names (case and "_" ignored),
//     dates are ISO 8601 strings, integers are JSON integers.
package cms

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
)

// Ids of the authored rows, used by the matchmaker and the round server as well.
const (
	ShowID    = "openpak_race_show"
	EpisodeID = "openpak_race_episode"
	PoolID    = "openpak_race_pool"
	RoundID   = "openpak_round_gauntlet_09"
	// RoundScene is the client's addressable scene key for the round (it ships in the build).
	RoundScene = "FallGuy_Gauntlet_09"
)

type row = map[string]any

func str(id, text string) row { return row{"id": id, "text": text} }

func setting(id string, v any) row { return row{"id": id, "value": v} }

// authored returns the non-empty tables.
func authored() map[string][]row {
	return map[string][]row{
		"localised_strings": {
			str("openpak_show_name", "OpenPak Race"),
			str("openpak_show_description", "One race. First half through the finish qualifies."),
			str("openpak_round_race_name", "Race"),
			str("openpak_round_race_description", "Reach the finish before the time runs out."),
		},
		"shows": {{
			"id":                     ShowID,
			"show_name":              "openpak_show_name",
			"show_description":       "openpak_show_description",
			"default_episode":        EpisodeID,
			"show_type":              row{"showtype_switch": "individual", "squad_size": 1},
			"size":                   row{"type": "default", "min_players": 1, "max_players": 40},
			"min_party_size":         1,
			"max_party_size":         4,
			"can_play_again":         true,
			"opens_at":               "2020-01-01T00:00:00Z",
			"closes_at":              "2099-12-31T00:00:00Z",
			"display_while_upcoming": false,
		}},
		"levels_episode": {{
			"id":             EpisodeID,
			"mode":           "knockout",
			"roundpool":      PoolID,
			"fallback_round": RoundID,
		}},
		"levels_roundpool": {{
			"id":     PoolID,
			"config": row{"type": "legacy"},
			"stages": []row{{"round": RoundID}},
		}},
		"levels_round": {{
			"id":                RoundID,
			"display_name":      "openpak_round_race_name",
			"round_description": "openpak_round_race_description",
			"scene_type":        row{"type": "prime", "prime_level": "openpak_prime_gauntlet_09"},
			"game_rules":        "openpak_race_rules",
		}},
		"prime_levels": {{"id": "openpak_prime_gauntlet_09", "scene_name": RoundScene}},
		"game_rules": {{
			"id":                       "openpak_race_rules",
			"team_mode":                "solo",
			"is_final_round":           false,
			"qualification_percentage": 50,
			"round_end_condition":      "success_quota",
			"has_timer":                true,
			"duration":                 180.0,
		}},
		"matchmaking_gates":  {{"id": "openpak_gate_race", "show_id": ShowID}},
		"shows_selector_tab": {{"id": "openpak_tab_main", "tab_shows": []row{{"show": ShowID}}}},
		"settings_global": {
			// The round server speaks Hazel without the sequencing header (client default true).
			setting("hazel_packet_sequencing", false),
			setting("hazel_disconnect_timeout", 10),
			setting("hazel_missing_server_pings", 10),
		},
	}
}

var (
	once    sync.Once
	body    []byte
	version string
)

func build() {
	doc := map[string]any{}
	for _, k := range tables {
		doc[k] = []row{}
	}
	for k, rows := range authored() {
		doc[k] = rows
	}
	// The version is the hash of the content, so any change reaches clients that cached an
	// older document under its version.
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	version = "openpak-" + hex.EncodeToString(sum[:6])
	doc["_meta"] = map[string]any{"_content_version": version}
	body, _ = json.Marshal(doc)
}

// Document is the serialised content document.
func Document() []byte { once.Do(build); return body }

// Version is the content version the login answer and the document's _meta carry.
func Version() string { once.Do(build); return version }
