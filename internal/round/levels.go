package round

// DefaultLevel is the round roundd serves when ROUND_LEVEL is unset.
const DefaultLevel = "openpak_round_door_dash"

// Levels are the rounds this server can referee, by CMS round id. Start positions and finish
// volumes are measurements of the client's own scenes (facts the protocol needs: the server
// places players and decides who finished).
var Levels = map[string]Level{
	"openpak_round_door_dash": {
		RoundID: "openpak_round_door_dash", Scene: "FallGuy_DoorDash", Duration: 180, QualifyPct: 50,
	},
}
