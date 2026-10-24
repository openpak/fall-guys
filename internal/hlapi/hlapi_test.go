package hlapi

import (
	"bytes"
	"testing"
)

func TestPackedMatchesHLAPI(t *testing.T) {
	cases := map[uint32][]byte{
		0:          {0},
		240:        {240},
		241:        {241, 1},
		2287:       {248, 255},
		2288:       {249, 0, 0},
		67823:      {249, 255, 255},
		67824:      {250, 0xf0, 0x08, 0x01},
		0xffffff:   {250, 0xff, 0xff, 0xff},
		0x01000000: {251, 0, 0, 0, 1},
	}
	for v, want := range cases {
		w := Writer{}
		w.Packed(v)
		if !bytes.Equal(w.B, want) {
			t.Errorf("Packed(%d) = %v, want %v", v, w.B, want)
		}
		if got := NewReader(want).Packed(); got != v {
			t.Errorf("read %v = %d, want %d", want, got, v)
		}
	}
}

func TestRoundTripAndShortReads(t *testing.T) {
	w := Writer{}
	w.Start(GameBase + 17)
	w.String("héllo")
	w.F32(1.5)
	w.Vec3([3]float32{1, 2, 3})
	w.Bool(true)
	w.I64(-2)
	r := NewReader(w.B)
	if r.Byte() != 74 || r.String() != "héllo" || r.F32() != 1.5 || r.Vec3() != [3]float32{1, 2, 3} || !r.Bool() || r.I64() != -2 || r.More() {
		t.Fatal("round trip")
	}
	r = NewReader([]byte{5, 0, 'a'})
	if r.String() != "" || r.Err != ErrShort || r.U32() != 0 {
		t.Fatal("short read")
	}
}

func TestFragments(t *testing.T) {
	msg := bytes.Repeat([]byte("0123456789"), 25)
	var d Defragmenter
	var got []byte
	for _, p := range Fragments(msg, 100) {
		r := NewReader(p)
		if r.Byte() != MsgFragment {
			t.Fatal("not a fragment")
		}
		if m, done := d.Add(r); done {
			got = m
		}
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %d bytes", len(got))
	}
}
