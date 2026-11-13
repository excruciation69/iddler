package steam

// A tiny protobuf encoder/decoder, just enough for Steam's messages.

import (
	"encoding/binary"
	"errors"
	"math"
)

// PB builds a protobuf message field by field.
type PB struct{ b []byte }

func NewPB() *PB { return &PB{} }

func (w *PB) Data() []byte { return w.b }

func (w *PB) key(field, wireType int) { w.b = binary.AppendUvarint(w.b, uint64(field<<3|wireType)) }

func (w *PB) Uint(field int, v uint64) *PB {
	w.key(field, 0)
	w.b = binary.AppendUvarint(w.b, v)
	return w
}

func (w *PB) Int(field int, v int64) *PB { return w.Uint(field, uint64(v)) }

func (w *PB) Bool(field int, v bool) *PB {
	if v {
		return w.Uint(field, 1)
	}
	return w.Uint(field, 0)
}

func (w *PB) Fixed64(field int, v uint64) *PB {
	w.key(field, 1)
	w.b = binary.LittleEndian.AppendUint64(w.b, v)
	return w
}

func (w *PB) Fixed32(field int, v uint32) *PB {
	w.key(field, 5)
	w.b = binary.LittleEndian.AppendUint32(w.b, v)
	return w
}

func (w *PB) Bytes(field int, v []byte) *PB {
	w.key(field, 2)
	w.b = binary.AppendUvarint(w.b, uint64(len(v)))
	w.b = append(w.b, v...)
	return w
}

func (w *PB) Str(field int, v string) *PB { return w.Bytes(field, []byte(v)) }

func (w *PB) Msg(field int, m *PB) *PB { return w.Bytes(field, m.b) }

// Fields is a decoded protobuf message: field number -> values (repeated fields keep order).
type Fields map[int][]field

type field struct {
	wire int
	num  uint64
	raw  []byte
}

func Parse(b []byte) (Fields, error) {
	m := Fields{}
	for len(b) > 0 {
		k, n := binary.Uvarint(b)
		if n <= 0 {
			return m, errors.New("pb: bad key")
		}
		b = b[n:]
		f := field{wire: int(k & 7)}
		switch f.wire {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return m, errors.New("pb: bad varint")
			}
			f.num, b = v, b[n:]
		case 1:
			if len(b) < 8 {
				return m, errors.New("pb: short fixed64")
			}
			f.num, b = binary.LittleEndian.Uint64(b), b[8:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return m, errors.New("pb: bad length")
			}
			f.raw, b = b[n:n+int(l)], b[n+int(l):]
		case 5:
			if len(b) < 4 {
				return m, errors.New("pb: short fixed32")
			}
			f.num, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		default:
			return m, errors.New("pb: unsupported wire type")
		}
		num := int(k >> 3)
		m[num] = append(m[num], f)
	}
	return m, nil
}

func (m Fields) last(n int) (field, bool) {
	if fs := m[n]; len(fs) > 0 {
		return fs[len(fs)-1], true
	}
	return field{}, false
}

func (m Fields) Has(n int) bool { _, ok := m.last(n); return ok }

func (m Fields) U(n int) uint64 { f, _ := m.last(n); return f.num }

func (m Fields) I(n int) int64 { return int64(int32(m.U(n))) }

func (m Fields) B(n int) bool { return m.U(n) != 0 }

func (m Fields) F32(n int) float32 { return math.Float32frombits(uint32(m.U(n))) }

func (m Fields) S(n int) string { f, _ := m.last(n); return string(f.raw) }

func (m Fields) Raw(n int) []byte { f, _ := m.last(n); return f.raw }

// Msgs decodes a repeated embedded message field.
func (m Fields) Msgs(n int) []Fields {
	var out []Fields
	for _, f := range m[n] {
		if sub, err := Parse(f.raw); err == nil {
			out = append(out, sub)
		}
	}
	return out
}

// Uints reads a repeated varint/fixed field, packed or not.
func (m Fields) Uints(n int) []uint64 {
	var out []uint64
	for _, f := range m[n] {
		if f.wire != 2 {
			out = append(out, f.num)
			continue
		}
		b := f.raw
		for len(b) > 0 {
			v, k := binary.Uvarint(b)
			if k <= 0 {
				break
			}
			out = append(out, v)
			b = b[k:]
		}
	}
	return out
}
