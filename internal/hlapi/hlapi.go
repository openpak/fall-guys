// Package hlapi is the message layer the Fall Guys client runs on top of Hazel: its fork of
// Unity's UNet HLAPI. A datagram's payload is a run of messages, each one byte of message id
// followed by its body, with no length prefix: the reader must consume exactly the body.
//
// Primitives are the public HLAPI ones: little-endian integers, floats as their bits, bool as
// one byte, strings as a u16 byte length and UTF-8, the HLAPI packed uint32, NetworkInstanceId
// packed, NetworkHash128 as 16 raw bytes.
//
// Ids 1-47 are HLAPI's own (public MsgType numbers); the game's messages are 57 + their
// EnumGameMessageType. Bodies larger than a datagram travel as Fragment (17) chunks.
package hlapi

import (
	"encoding/binary"
	"errors"
	"math"
)

// HLAPI system message ids.
const (
	MsgObjectDestroy        = 1
	MsgRpc                  = 2
	MsgObjectSpawn          = 3
	MsgOwner                = 4
	MsgCommand              = 5
	MsgLocalPlayerTransform = 6
	MsgSyncEvent            = 7
	MsgUpdateVars           = 8
	MsgSyncList             = 9
	MsgObjectSpawnScene     = 10
	MsgSpawnFinished        = 12
	MsgObjectHide           = 13
	MsgCRC                  = 14
	MsgLocalClientAuthority = 15
	MsgLocalChildTransform  = 16
	MsgFragment             = 17
	MsgReady                = 35
	MsgRemovePlayer         = 38

	// GameBase is added to an EnumGameMessageType to give its message id.
	GameBase = 57
)

// ErrShort: a body ended early.
var ErrShort = errors.New("hlapi: message ends early")

// Writer builds a payload: one or more messages.
type Writer struct{ B []byte }

func (w *Writer) Start(msgID byte) { w.B = append(w.B, msgID) }
func (w *Writer) Byte(v byte)      { w.B = append(w.B, v) }
func (w *Writer) Bool(v bool) {
	if v {
		w.B = append(w.B, 1)
	} else {
		w.B = append(w.B, 0)
	}
}
func (w *Writer) U16(v uint16)  { w.B = binary.LittleEndian.AppendUint16(w.B, v) }
func (w *Writer) I16(v int16)   { w.U16(uint16(v)) }
func (w *Writer) U32(v uint32)  { w.B = binary.LittleEndian.AppendUint32(w.B, v) }
func (w *Writer) I32(v int32)   { w.U32(uint32(v)) }
func (w *Writer) U64(v uint64)  { w.B = binary.LittleEndian.AppendUint64(w.B, v) }
func (w *Writer) I64(v int64)   { w.U64(uint64(v)) }
func (w *Writer) F32(v float32) { w.U32(math.Float32bits(v)) }
func (w *Writer) Vec3(v [3]float32) {
	for _, f := range v {
		w.F32(f)
	}
}
func (w *Writer) Quat(v [4]float32) {
	for _, f := range v {
		w.F32(f)
	}
}
func (w *Writer) String(s string) {
	w.U16(uint16(len(s)))
	w.B = append(w.B, s...)
}
func (w *Writer) Bytes(b []byte) { w.B = append(w.B, b...) }
func (w *Writer) Hash128(h [16]byte) {
	w.B = append(w.B, h[:]...)
}

// Packed is HLAPI's WritePackedUInt32.
func (w *Writer) Packed(v uint32) {
	switch {
	case v <= 240:
		w.B = append(w.B, byte(v))
	case v <= 2287:
		w.B = append(w.B, byte((v-240)/256+241), byte((v-240)%256))
	case v <= 67823:
		w.B = append(w.B, 249, byte((v-2288)/256), byte((v-2288)%256))
	case v <= 0xffffff:
		w.B = append(w.B, 250, byte(v), byte(v>>8), byte(v>>16))
	default:
		w.B = append(w.B, 251, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
}

// Reader walks a payload. The first short read sets Err and every later read returns zero.
type Reader struct {
	B   []byte
	Pos int
	Err error
}

func NewReader(b []byte) *Reader { return &Reader{B: b} }

func (r *Reader) Left() int  { return len(r.B) - r.Pos }
func (r *Reader) More() bool { return r.Err == nil && r.Pos < len(r.B) }

func (r *Reader) take(n int) []byte {
	if r.Err != nil {
		return nil
	}
	if n < 0 || r.Pos+n > len(r.B) {
		r.Err = ErrShort
		return nil
	}
	b := r.B[r.Pos : r.Pos+n]
	r.Pos += n
	return b
}

func (r *Reader) Byte() byte {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}
func (r *Reader) Bool() bool { return r.Byte() != 0 }
func (r *Reader) U16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}
func (r *Reader) I16() int16 { return int16(r.U16()) }
func (r *Reader) U32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}
func (r *Reader) I32() int32 { return int32(r.U32()) }
func (r *Reader) U64() uint64 {
	if b := r.take(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}
func (r *Reader) I64() int64   { return int64(r.U64()) }
func (r *Reader) F32() float32 { return math.Float32frombits(r.U32()) }
func (r *Reader) Vec3() (v [3]float32) {
	for i := range v {
		v[i] = r.F32()
	}
	return
}
func (r *Reader) Quat() (v [4]float32) {
	for i := range v {
		v[i] = r.F32()
	}
	return
}
func (r *Reader) String() string { return string(r.take(int(r.U16()))) }
func (r *Reader) Bytes(n int) []byte {
	return append([]byte(nil), r.take(n)...)
}
func (r *Reader) Hash128() (h [16]byte) {
	copy(h[:], r.take(16))
	return
}

// Packed is HLAPI's ReadPackedUInt32.
func (r *Reader) Packed() uint32 {
	a := uint32(r.Byte())
	switch {
	case a < 241:
		return a
	case a <= 248:
		return 240 + 256*(a-241) + uint32(r.Byte())
	case a == 249:
		b, c := uint32(r.Byte()), uint32(r.Byte())
		return 2288 + 256*b + c
	case a == 250:
		b := r.take(3)
		if b == nil {
			return 0
		}
		return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
	default:
		return r.U32()
	}
}

// Fragments splits one oversized message into Fragment chunks: [17][0][u16 len][bytes] each,
// then [17][1]. Each returned payload fits one datagram.
func Fragments(msg []byte, chunk int) [][]byte {
	var out [][]byte
	for len(msg) > 0 {
		n := min(chunk, len(msg))
		w := Writer{}
		w.Start(MsgFragment)
		w.Byte(0)
		w.U16(uint16(n))
		w.Bytes(msg[:n])
		out = append(out, w.B)
		msg = msg[n:]
	}
	return append(out, []byte{MsgFragment, 1})
}

// Defragmenter rebuilds fragmented messages from one sender.
type Defragmenter struct{ buf []byte }

// Add takes a Fragment body (after the id). It returns the whole message once the last
// fragment has arrived.
func (d *Defragmenter) Add(r *Reader) (msg []byte, done bool) {
	if r.Byte() == 0 {
		n := int(r.U16())
		d.buf = append(d.buf, r.take(n)...)
		return nil, false
	}
	msg, d.buf = d.buf, nil
	return msg, r.Err == nil
}
