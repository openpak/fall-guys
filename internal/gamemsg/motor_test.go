package gamemsg

import (
	"encoding/hex"
	"testing"

	"openpak/fall-guys/internal/hlapi"
)

// Vectors from the reference codec written alongside the client read (scratch notes).
const (
	clientMotorTasks = "0701f4e200000060c0000000000000000000000000803f0600bf000a3f86a0380070004033f88700"
	agentState       = "540701f4e20a0000803f00200000023f86a038007000408080"
)

func TestDecodeMotorTasks(t *testing.T) {
	b, _ := hex.DecodeString(clientMotorTasks)
	r := hlapi.NewReader(b)
	id, snaps, err := DecodeMotorTasks(r)
	if err != nil || id != 7 || len(snaps) != 1 || r.More() {
		t.Fatalf("id %d snaps %d err %v left %d", id, len(snaps), err, r.Left())
	}
	s := snaps[0]
	p := s.Transform.Position
	if s.LocalFrame != 1234 || s.TaskMask != 6 || abs32(p[0]-12.5) > 0.02 || abs32(p[1]-3.25) > 0.02 || abs32(p[2]+40) > 0.02 {
		t.Fatalf("%+v", s)
	}
	if abs32(s.Transform.Euler[1]-90) > 0.01 || abs32(s.Transform.Velocity[2]-5) > 0.05 {
		t.Fatalf("%+v", s.Transform)
	}
	// A player standing still, as the reference codec writes it.
	still := hlapi.Writer{}
	EncodeTransform(&still, [3]float32{12.5, 3.25, -40}, [3]float32{0, 90, 0}, [3]float32{})
	if got := hex.EncodeToString(AgentState(7, 1234, 10, 1, still.B, true)); got != agentState {
		t.Fatalf("agent state %s\nwant        %s", got, agentState)
	}
	// And the encoder writes what the client would.
	w := hlapi.Writer{}
	EncodeTransform(&w, [3]float32{12.5, 3.25, -40}, [3]float32{0, 90, 0}, [3]float32{2, 0, 5})
	if hex.EncodeToString(w.B) != hex.EncodeToString(s.TransformBytes) {
		t.Fatalf("encode %x, client %x", w.B, s.TransformBytes)
	}
}
