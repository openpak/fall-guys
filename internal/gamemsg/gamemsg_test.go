package gamemsg

import (
	"bytes"
	"encoding/binary"
	"testing"

	"openpak/fall-guys/internal/hlapi"
)

// clientConnectInitial builds the client's message the way its Serialize does.
func clientConnectInitial(n, m int) []byte {
	var pv [8]byte
	binary.LittleEndian.PutUint64(pv[:], ProtocolVersion)
	w := hlapi.Writer{}
	w.Start(ClientConnectInitial.MsgID())
	w.I32(connectMarker)
	w.I32(int32(n + 3))
	i := 0
	for ; i < n; i++ {
		w.I32(int32(0x5a00 | int(pv[i])))
	}
	w.String("support")
	w.String("11.6.1")
	w.I32(int32(m + connectMarker))
	for k := 0; k < m; k, i = k+1, i+1 {
		if i < 8 {
			w.I32(int32(0x7700 | int(pv[i])))
		} else {
			w.I32(0x12345678)
		}
	}
	return w.B
}

func TestDecodeConnectInitial(t *testing.T) {
	for _, nm := range [][2]int{{5, 4}, {6, 7}} {
		r := hlapi.NewReader(clientConnectInitial(nm[0], nm[1]))
		if typ, ok := TypeOf(r.Byte()); !ok || typ != ClientConnectInitial {
			t.Fatal("type")
		}
		c, err := DecodeConnectInitial(r)
		if err != nil || c.Protocol != ProtocolVersion || c.SupportID != "support" || c.Build != "11.6.1" || r.More() {
			t.Fatalf("%+v %v", c, err)
		}
	}
}

func TestHalfAndQuat(t *testing.T) {
	for f, h := range map[float32]uint16{0: 0, 1: 0x3c00, -2: 0xc000, 0.5: 0x3800, 65504: 0x7bff, 1e6: 0x7c00} {
		if got := Half(f); got != h {
			t.Errorf("Half(%v) = %#x, want %#x", f, got, h)
		}
	}
	w := hlapi.Writer{}
	CompressedQuat(&w, YawQuat(0))
	if !bytes.Equal(w.B, []byte{7}) {
		t.Fatalf("identity = %x", w.B)
	}
	w = hlapi.Writer{}
	CompressedQuat(&w, YawQuat(90)) // y = w = 0.7071: largest is y (first found)
	if len(w.B) != 7 || w.B[0] != 1 {
		t.Fatalf("yaw 90 = %x", w.B)
	}
}

func TestSpawnLayoutLength(t *testing.T) {
	b := Spawn{IsAuth: true, NetID: 5, PrefabHash: -1, Player: Player{PlayerID: 1, AccountID: "a"}}.Encode()
	// id, bool, packed, byte, int, 6, quat 1, 6, bool, byte, byte, bool, float, float, packed,
	// u64, packed, 4 strings (3 bytes for "a", 2 each empty), packed, int, string, int, bool,
	// 25 uints, int, int, long
	want := 1 + 1 + 1 + 1 + 4 + 6 + 1 + 6 + 1 + 1 + 1 + 1 + 4 + 4 + 1 + 8 + 1 + 3 + 2 + 2 + 2 + 1 + 4 + 2 + 4 + 1 + 25*4 + 4 + 4 + 8
	if len(b) != want {
		t.Fatalf("spawn is %d bytes, want %d", len(b), want)
	}
}

func TestConnectProof(t *testing.T) {
	if ConnectProof(0x8000000000000001) != (0x8000000000000001<<(0x8000000000000001%30))&0x8000000000000001 {
		t.Fatal("proof")
	}
}
