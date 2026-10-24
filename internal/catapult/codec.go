package catapult

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

//go:embed schema.json
var schemaJSON []byte

type typeDef struct {
	Enum []string          `json:"enum"`
	M    [][3]any          `json:"m"`   // [tag, name, CLR type]
	Sub  map[string]string `json:"sub"` // subtype tag -> type
	Base string            `json:"base"`
}

type member struct {
	tag  int
	name string
	typ  string
}

// Schema is the client's contract set.
type Schema struct {
	types map[string]*typeDef
	subOf map[string]map[string]int // base -> subtype name -> tag
}

func LoadSchema() (*Schema, error) {
	s := &Schema{subOf: map[string]map[string]int{}}
	if err := json.Unmarshal(schemaJSON, &s.types); err != nil {
		return nil, err
	}
	for base, t := range s.types {
		for tag, sub := range t.Sub {
			n, _ := strconv.Atoi(tag)
			if s.subOf[base] == nil {
				s.subOf[base] = map[string]int{}
			}
			s.subOf[base][sub] = n
		}
	}
	return s, nil
}

// Has reports whether name is a known message type.
func (s *Schema) Has(name string) bool { _, ok := s.def(name); return ok }

// def finds a type, instantiating single-parameter generics ("X`1<Arg>" from "X`1").
func (s *Schema) def(name string) (*typeDef, bool) {
	if t, ok := s.types[name]; ok {
		return t, true
	}
	base, args := splitGeneric(name)
	if len(args) == 1 {
		if t, ok := s.types[base]; ok {
			inst := &typeDef{Sub: t.Sub, Base: t.Base}
			for _, m := range t.M {
				typ := m[2].(string)
				if typ == "!T" {
					typ = args[0]
				}
				inst.M = append(inst.M, [3]any{m[0], m[1], typ})
			}
			return inst, true
		}
	}
	return nil, false
}

func (t *typeDef) members() []member {
	out := make([]member, 0, len(t.M))
	for _, m := range t.M {
		out = append(out, member{tag: int(m[0].(float64)), name: m[1].(string), typ: m[2].(string)})
	}
	return out
}

// splitGeneric("A`2<B,C<D>>") = "A`2", ["B", "C<D>"].
func splitGeneric(name string) (string, []string) {
	i := strings.IndexByte(name, '<')
	if i < 0 || !strings.HasSuffix(name, ">") {
		return name, nil
	}
	inner := name[i+1 : len(name)-1]
	var args []string
	depth, start := 0, 0
	for j, c := range inner {
		switch c {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, inner[start:j])
				start = j + 1
			}
		}
	}
	return name[:i], append(args, inner[start:])
}

var listTypes = map[string]bool{
	"System.Collections.Generic.List`1": true, "System.Collections.Generic.IEnumerable`1": true,
	"System.Collections.Generic.HashSet`1": true, "System.Collections.Generic.IReadOnlyCollection`1": true,
	"System.Collections.Generic.IReadOnlyList`1": true, "System.Collections.Generic.IList`1": true,
	"System.Collections.Generic.ICollection`1": true,
}

// elem returns the element type when typ is repeated, and whether it is a dictionary.
func elem(typ string) (string, bool, bool) {
	if strings.HasSuffix(typ, "[]") && typ != "byte[]" {
		return strings.TrimSuffix(typ, "[]"), true, false
	}
	base, args := splitGeneric(typ)
	if listTypes[base] && len(args) == 1 {
		return args[0], true, false
	}
	if (base == "System.Collections.Generic.Dictionary`2" || base == "System.Collections.Generic.IReadOnlyDictionary`2" ||
		base == "System.Collections.Generic.IDictionary`2") && len(args) == 2 {
		return "", true, true
	}
	return "", false, false
}

// ---- decoding ----

// Decode reads a message of the declared type. Polymorphic values carry "$type".
func (s *Schema) Decode(typ string, b []byte) (map[string]any, error) {
	t, ok := s.def(typ)
	if !ok {
		return nil, fmt.Errorf("catapult: unknown type %s", typ)
	}
	fs, err := fields(b)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	byTag := map[int]member{}
	for _, m := range t.members() {
		byTag[m.tag] = m
	}
	for _, f := range fs {
		if sub, ok := t.Sub[strconv.Itoa(f.tag)]; ok && f.wt == wtBytes {
			inner, err := s.Decode(sub, f.p)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", sub, err)
			}
			for k, v := range inner {
				out[k] = v
			}
			if _, set := inner["$type"]; !set {
				out["$type"] = sub
			}
			continue
		}
		m, ok := byTag[f.tag]
		if !ok {
			out["#"+strconv.Itoa(f.tag)] = rawValue(f)
			continue
		}
		if e, rep, dict := elem(m.typ); rep {
			if dict {
				_, args := splitGeneric(m.typ)
				kv, err := s.decodeKV(args[0], args[1], f.p)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", m.name, err)
				}
				d, _ := out[m.name].(map[string]any)
				if d == nil {
					d = map[string]any{}
					out[m.name] = d
				}
				d[fmt.Sprint(kv[0])] = kv[1]
				continue
			}
			vals, err := s.decodeRepeated(e, f)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", m.name, err)
			}
			l, _ := out[m.name].([]any)
			out[m.name] = append(l, vals...)
			continue
		}
		v, err := s.decodeValue(m.typ, f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.name, err)
		}
		out[m.name] = v
	}
	return out, nil
}

func rawValue(f field) any {
	if f.wt == wtBytes {
		return f.p
	}
	return f.u
}

func (s *Schema) decodeKV(kt, vt string, b []byte) ([2]any, error) {
	var kv [2]any
	fs, err := fields(b)
	if err != nil {
		return kv, err
	}
	for _, f := range fs {
		switch f.tag {
		case 1:
			kv[0], err = s.decodeValue(kt, f)
		case 2:
			kv[1], err = s.decodeValue(vt, f)
		}
		if err != nil {
			return kv, err
		}
	}
	return kv, nil
}

func (s *Schema) decodeRepeated(e string, f field) ([]any, error) {
	if f.wt == wtBytes && isScalar(e) && e != "string" && e != "byte[]" {
		var out []any // packed
		p := f.p
		for len(p) > 0 {
			var g field
			switch scalarWire(e) {
			case wtVarint:
				v, n := binary.Uvarint(p)
				if n <= 0 {
					return nil, errTruncated
				}
				g, p = field{wt: wtVarint, u: v}, p[n:]
			case wtFixed32:
				if len(p) < 4 {
					return nil, errTruncated
				}
				g, p = field{wt: wtFixed32, u: uint64(binary.LittleEndian.Uint32(p))}, p[4:]
			default:
				if len(p) < 8 {
					return nil, errTruncated
				}
				g, p = field{wt: wtFixed64, u: binary.LittleEndian.Uint64(p)}, p[8:]
			}
			v, err := s.decodeValue(e, g)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	v, err := s.decodeValue(e, f)
	if err != nil {
		return nil, err
	}
	return []any{v}, nil
}

func (s *Schema) decodeValue(typ string, f field) (any, error) {
	if base, args := splitGeneric(typ); base == "System.Nullable`1" && len(args) == 1 {
		typ = args[0]
	}
	switch typ {
	case "string":
		return string(f.p), nil
	case "byte[]":
		return f.p, nil
	case "bool":
		return f.u != 0, nil
	case "int", "short", "sbyte", "long":
		return int64(f.u), nil
	case "uint", "ushort", "byte", "ulong", "char":
		return f.u, nil
	case "float":
		return float64(f32(f.u)), nil
	case "double":
		return math.Float64frombits(f.u), nil
	case "System.DateTime":
		return decodeDateTime(f.p)
	case "System.TimeSpan":
		return decodeTimeSpan(f.p)
	case "System.Guid":
		return decodeGuid(f.p)
	}
	if t, ok := s.def(typ); ok && t.Enum == nil {
		return s.Decode(typ, f.p)
	}
	return int64(f.u), nil // enum
}

func isScalar(t string) bool {
	switch t {
	case "string", "byte[]", "System.DateTime", "System.TimeSpan", "System.Guid":
		return false
	}
	return !strings.Contains(t, ".") || strings.HasPrefix(t, "System.Nullable")
}

func scalarWire(t string) int {
	switch t {
	case "float":
		return wtFixed32
	case "double":
		return wtFixed64
	}
	return wtVarint
}

// ---- encoding ----

// Encode writes v as the declared type. A "$type" in v names the concrete subtype.
func (s *Schema) Encode(typ string, v map[string]any) ([]byte, error) {
	concrete, _ := v["$type"].(string)
	if concrete == "" || concrete == typ {
		return s.encodeOwn(typ, v)
	}
	// Chain from the concrete type up to the declared one.
	chain := []string{concrete}
	for cur := concrete; cur != typ; {
		t, ok := s.def(cur)
		if !ok || t.Base == "" {
			return nil, fmt.Errorf("catapult: %s does not derive from %s", concrete, typ)
		}
		cur = t.Base
		chain = append(chain, cur)
	}
	inner, err := s.encodeOwn(chain[0], v)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(chain); i++ {
		tag, ok := s.subOf[chain[i]][chain[i-1]]
		if !ok {
			return nil, fmt.Errorf("catapult: no subtype tag for %s in %s", chain[i-1], chain[i])
		}
		own, err := s.encodeOwn(chain[i], v)
		if err != nil {
			return nil, err
		}
		w := buf{}
		w.bytes(tag, inner)
		inner = append(w.b, own...)
	}
	return inner, nil
}

// encodeOwn writes only the members the type itself declares.
func (s *Schema) encodeOwn(typ string, v map[string]any) ([]byte, error) {
	t, ok := s.def(typ)
	if !ok {
		return nil, fmt.Errorf("catapult: unknown type %s", typ)
	}
	w := &buf{}
	for _, m := range t.members() {
		x, ok := v[m.name]
		if !ok || x == nil {
			continue
		}
		if e, rep, dict := elem(m.typ); rep {
			if dict {
				_, args := splitGeneric(m.typ)
				d, _ := x.(map[string]any)
				for k, val := range d {
					kw := &buf{}
					if err := s.encodeValue(kw, 1, args[0], dictKey(args[0], k)); err != nil {
						return nil, err
					}
					if err := s.encodeValue(kw, 2, args[1], val); err != nil {
						return nil, err
					}
					w.bytes(m.tag, kw.b)
				}
				continue
			}
			for _, item := range asList(x) {
				if err := s.encodeValue(w, m.tag, e, item); err != nil {
					return nil, fmt.Errorf("%s.%s: %w", typ, m.name, err)
				}
			}
			continue
		}
		if err := s.encodeValue(w, m.tag, m.typ, x); err != nil {
			return nil, fmt.Errorf("%s.%s: %w", typ, m.name, err)
		}
	}
	return w.b, nil
}

func dictKey(typ, k string) any {
	switch typ {
	case "string":
		return k
	default:
		n, _ := strconv.ParseInt(k, 10, 64)
		return n
	}
}

func asList(x any) []any {
	switch l := x.(type) {
	case []any:
		return l
	case []string:
		out := make([]any, len(l))
		for i, s := range l {
			out[i] = s
		}
		return out
	case []map[string]any:
		out := make([]any, len(l))
		for i, s := range l {
			out[i] = s
		}
		return out
	}
	return []any{x}
}

func (s *Schema) encodeValue(w *buf, tag int, typ string, x any) error {
	if base, args := splitGeneric(typ); base == "System.Nullable`1" && len(args) == 1 {
		typ = args[0]
	}
	switch typ {
	case "string":
		w.bytes(tag, []byte(fmt.Sprint(x)))
		return nil
	case "byte[]":
		p, _ := x.([]byte)
		w.bytes(tag, p)
		return nil
	case "bool":
		b, _ := x.(bool)
		w.key(tag, wtVarint)
		if b {
			w.varint(1)
		} else {
			w.varint(0)
		}
		return nil
	case "float":
		w.fixed32(tag, math.Float32bits(float32(toFloat(x))))
		return nil
	case "double":
		w.fixed64(tag, math.Float64bits(toFloat(x)))
		return nil
	case "System.DateTime":
		t, _ := x.(time.Time)
		w.bytes(tag, encodeDateTime(t))
		return nil
	case "System.TimeSpan":
		d, _ := x.(time.Duration)
		w.bytes(tag, encodeTimeSpan(d))
		return nil
	case "System.Guid":
		p, err := encodeGuid(fmt.Sprint(x))
		if err != nil {
			return err
		}
		w.bytes(tag, p)
		return nil
	}
	if t, ok := s.def(typ); ok && t.Enum == nil {
		m, ok := x.(map[string]any)
		if !ok {
			return fmt.Errorf("want an object for %s, got %T", typ, x)
		}
		p, err := s.Encode(typ, m)
		if err != nil {
			return err
		}
		w.bytes(tag, p)
		return nil
	}
	// integers and enums: protobuf-net writes negative ints as 10-byte varints.
	w.key(tag, wtVarint)
	w.varint(uint64(toInt(x)))
	return nil
}

func toInt(x any) int64 {
	switch n := x.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	case bool:
		if n {
			return 1
		}
	}
	return 0
}

func toFloat(x any) float64 {
	switch n := x.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	}
	return float64(toInt(x))
}

// ---- protobuf-net BCL types ----

var epoch = time.Unix(0, 0).UTC()

// bcl.DateTime / bcl.TimeSpan: {1: sint64 value, 2: scale}. Scale 0 days, 1 hours, 2 minutes,
// 3 seconds, 4 milliseconds, 5 ticks, 15 min/max.
func scaleUnit(scale uint64) time.Duration {
	switch scale {
	case 0:
		return 24 * time.Hour
	case 1:
		return time.Hour
	case 2:
		return time.Minute
	case 3:
		return time.Second
	case 4:
		return time.Millisecond
	}
	return 100 * time.Nanosecond
}

func decodeScaled(b []byte) (int64, uint64, error) {
	fs, err := fields(b)
	if err != nil {
		return 0, 0, err
	}
	var v int64
	var scale uint64
	for _, f := range fs {
		switch f.tag {
		case 1:
			v = unzigzag(f.u)
		case 2:
			scale = f.u
		}
	}
	return v, scale, nil
}

func decodeDateTime(b []byte) (time.Time, error) {
	v, scale, err := decodeScaled(b)
	if err != nil {
		return time.Time{}, err
	}
	if scale == 15 {
		if v == 1 {
			return time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), nil
		}
		return time.Time{}, nil
	}
	return epoch.Add(time.Duration(v) * scaleUnit(scale)), nil
}

func encodeDateTime(t time.Time) []byte {
	w := &buf{}
	ms := t.Sub(epoch).Milliseconds()
	if ms != 0 {
		w.key(1, wtVarint)
		w.varint(zigzag(ms))
	}
	w.key(2, wtVarint)
	w.varint(4)
	return w.b
}

func decodeTimeSpan(b []byte) (time.Duration, error) {
	v, scale, err := decodeScaled(b)
	return time.Duration(v) * scaleUnit(scale), err
}

func encodeTimeSpan(d time.Duration) []byte {
	w := &buf{}
	if ms := d.Milliseconds(); ms != 0 {
		w.key(1, wtVarint)
		w.varint(zigzag(ms))
	}
	w.key(2, wtVarint)
	w.varint(4)
	return w.b
}

// bcl.Guid: {1: fixed64 lo, 2: fixed64 hi} over .NET's Guid.ToByteArray() order.
func decodeGuid(b []byte) (string, error) {
	fs, err := fields(b)
	if err != nil {
		return "", err
	}
	var raw [16]byte
	for _, f := range fs {
		switch f.tag {
		case 1:
			binary.LittleEndian.PutUint64(raw[0:8], f.u)
		case 2:
			binary.LittleEndian.PutUint64(raw[8:16], f.u)
		}
	}
	return fmt.Sprintf("%08x-%04x-%04x-%x-%x", binary.LittleEndian.Uint32(raw[0:4]),
		binary.LittleEndian.Uint16(raw[4:6]), binary.LittleEndian.Uint16(raw[6:8]), raw[8:10], raw[10:16]), nil
}

func encodeGuid(s string) ([]byte, error) {
	h := strings.ReplaceAll(s, "-", "")
	if len(h) != 32 {
		return nil, fmt.Errorf("catapult: bad guid %q", s)
	}
	var std [16]byte
	for i := 0; i < 16; i++ {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		std[i] = byte(v)
	}
	var raw [16]byte
	binary.LittleEndian.PutUint32(raw[0:4], binary.BigEndian.Uint32(std[0:4]))
	binary.LittleEndian.PutUint16(raw[4:6], binary.BigEndian.Uint16(std[4:6]))
	binary.LittleEndian.PutUint16(raw[6:8], binary.BigEndian.Uint16(std[6:8]))
	copy(raw[8:], std[8:])
	w := &buf{}
	if lo := binary.LittleEndian.Uint64(raw[0:8]); lo != 0 {
		w.fixed64(1, lo)
	}
	if hi := binary.LittleEndian.Uint64(raw[8:16]); hi != 0 {
		w.fixed64(2, hi)
	}
	return w.b, nil
}
