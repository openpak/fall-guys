package round

import "openpak/fall-guys/internal/cms"

// DefaultLevel is the round roundd serves when ROUND_LEVEL is unset.
const DefaultLevel = cms.RoundID

// The networked prefab of a fall guy is "FallGuy"; the client keys its prefabs by the name's
// String.GetHashCode (the legacy 32-bit .NET hash in this build).
const fallGuyPrefab int32 = -491682846

// Levels are the rounds this server can referee, by CMS round id. Start positions and finish
// volumes are measurements of the client's own scenes: the server places the players and
// decides who finished, so it has to know where those are.
var Levels = map[string]Level{
	// A race with only local, time-driven obstacles: nothing for the server to possess.
	cms.RoundID: {
		RoundID: cms.RoundID, Scene: cms.RoundScene, Duration: 180, QualifyPct: 50,
		PlayerPrefab: fallGuyPrefab,
		Starts:       grid([]float32{-86.08, -89.4, -92.56, -95.56}, -56.732, -3, 10, 100.813, 90),
		// The finish platform's trigger; players who reach it leave the course.
		Finish:          []Box{{Center: [3]float32{-47.244, 17.75, 233.53}, Half: [3]float32{29.535, 13, 14.4}}},
		UnspawnFinished: true,
	},
}

// grid lays out start positions row by row, front row first: rows at the given x, cols along z.
func grid(rows []float32, z0, dz float32, cols int, y, yaw float32) []Start {
	var out []Start
	for _, x := range rows {
		for c := 0; c < cols; c++ {
			out = append(out, Start{Pos: [3]float32{x, y, z0 + dz*float32(c)}, Yaw: yaw})
		}
	}
	return out
}
