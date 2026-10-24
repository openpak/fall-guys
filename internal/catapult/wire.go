// Package catapult is the Fall Guys gateway's wire format: protobuf-net messages driven by a
// schema of the client's own contracts (schema.json: type -> members [tag, name, CLR type],
// subtype tags, enum value names). Values are plain Go maps, so a handler can answer any of
// the client's ~800 message types without generated code.
package catapult

import (
	"encoding/binary"
	"errors"
	"math"
)

// Protobuf wire types.
const (
	wtVarint  = 0
	wtFixed64 = 1
	wtBytes   = 2
	wtFixed32 = 5
)

var errTruncated = errors.New("catapult: truncated message")

type buf struct{ b []byte }

func (w *buf) varint(v uint64) { w.b = binary.AppendUvarint(w.b, v) }
func (w *buf) key(tag, wt int) { w.varint(uint64(tag)<<3 | uint64(wt)) }
func (w *buf) bytes(tag int, p []byte) {
	w.key(tag, wtBytes)
	w.varint(uint64(len(p)))
	w.b = append(w.b, p...)
}
func (w *buf) fixed64(tag int, v uint64) {
	w.key(tag, wtFixed64)
	w.b = binary.LittleEndian.AppendUint64(w.b, v)
}
func (w *buf) fixed32(tag int, v uint32) {
	w.key(tag, wtFixed32)
	w.b = binary.LittleEndian.AppendUint32(w.b, v)
}

// field is one decoded protobuf field: varints and fixed values in u, bytes in p.
type field struct {
	tag, wt int
	u       uint64
	p       []byte
}

func fields(b []byte) ([]field, error) {
	var out []field
	for len(b) > 0 {
		k, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errTruncated
		}
		b = b[n:]
		f := field{tag: int(k >> 3), wt: int(k & 7)}
		switch f.wt {
		case wtVarint:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, errTruncated
			}
			f.u, b = v, b[n:]
		case wtFixed64:
			if len(b) < 8 {
				return nil, errTruncated
			}
			f.u, b = binary.LittleEndian.Uint64(b), b[8:]
		case wtFixed32:
			if len(b) < 4 {
				return nil, errTruncated
			}
			f.u, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		case wtBytes:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return nil, errTruncated
			}
			f.p, b = b[n:n+int(l)], b[n+int(l):]
		default:
			return nil, errors.New("catapult: unsupported wire type (group)")
		}
		out = append(out, f)
	}
	return out, nil
}

func zigzag(v int64) uint64   { return uint64((v << 1) ^ (v >> 63)) }
func unzigzag(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

func f32(u uint64) float32 { return math.Float32frombits(uint32(u)) }
