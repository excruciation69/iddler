package steam

// Binary KeyValues (Valve's "VDF" binary format), used in purchase receipts and stats schemas.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
)

// KV is a node: either a value or a list of children (order preserved).
type KV struct {
	Name     string
	Value    string
	Children []*KV
}

func (k *KV) Get(name string) *KV {
	if k == nil {
		return nil
	}
	for _, c := range k.Children {
		if equalFold(c.Name, name) {
			return c
		}
	}
	return nil
}

// Path walks nested children: kv.Path("stats", "1", "bits").
func (k *KV) Path(names ...string) *KV {
	for _, n := range names {
		k = k.Get(n)
	}
	return k
}

func (k *KV) Str() string {
	if k == nil {
		return ""
	}
	return k.Value
}

func (k *KV) Int() int64 {
	if k == nil {
		return 0
	}
	v, _ := strconv.ParseInt(k.Value, 10, 64)
	return v
}

func equalFold(a, b string) bool { return len(a) == len(b) && bytes.EqualFold([]byte(a), []byte(b)) }

func ParseKV(b []byte) (*KV, error) {
	root := &KV{}
	_, err := parseKV(b, 0, root)
	return root, err
}

func parseKV(b []byte, off int, parent *KV) (int, error) {
	cstr := func() (string, error) {
		i := bytes.IndexByte(b[off:], 0)
		if i < 0 {
			return "", errors.New("kv: unterminated string")
		}
		s := string(b[off : off+i])
		off += i + 1
		return s, nil
	}
	need := func(n int) error {
		if off+n > len(b) {
			return errors.New("kv: truncated")
		}
		return nil
	}
	for off < len(b) {
		t := b[off]
		off++
		if t == 8 || t == 11 {
			return off, nil
		}
		name, err := cstr()
		if err != nil {
			return off, err
		}
		node := &KV{Name: name}
		switch t {
		case 0:
			if off, err = parseKV(b, off, node); err != nil {
				return off, err
			}
		case 1:
			if node.Value, err = cstr(); err != nil {
				return off, err
			}
		case 2, 4, 6:
			if err := need(4); err != nil {
				return off, err
			}
			node.Value = strconv.Itoa(int(int32(binary.LittleEndian.Uint32(b[off:]))))
			off += 4
		case 3:
			if err := need(4); err != nil {
				return off, err
			}
			node.Value = strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(b[off:]))), 'f', -1, 32)
			off += 4
		case 7:
			if err := need(8); err != nil {
				return off, err
			}
			node.Value = strconv.FormatUint(binary.LittleEndian.Uint64(b[off:]), 10)
			off += 8
		case 10:
			if err := need(8); err != nil {
				return off, err
			}
			node.Value = strconv.FormatInt(int64(binary.LittleEndian.Uint64(b[off:])), 10)
			off += 8
		default:
			return off, errors.New("kv: unknown type")
		}
		parent.Children = append(parent.Children, node)
	}
	return off, nil
}
