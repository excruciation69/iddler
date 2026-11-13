package steam

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestGuardCode(t *testing.T) {
	// reference values from the steam-totp npm package
	cases := []struct {
		secret string
		at     int64
		want   string
	}{
		{"cnOgv/KdpLoP6Nbh0GMkXkPXALQ=", 1700000000, "X45RP"},
		{"cnOgv/KdpLoP6Nbh0GMkXkPXALQ=", 1234567890, "VYNVB"},
		{"AAAAAAAAAAAAAAAAAAAAAAAAAAA=", 1700000000, "THTN4"},
		{"AAAAAAAAAAAAAAAAAAAAAAAAAAA=", 1234567890, "76TJG"},
	}
	for _, c := range cases {
		got, err := GuardCode(c.secret, time.Unix(c.at, 0))
		if err != nil || got != c.want {
			t.Errorf("GuardCode(%s, %d) = %q, %v; want %q", c.secret, c.at, got, err, c.want)
		}
	}
}

// kvBuilder writes binary KeyValues for tests.
type kvBuilder struct{ bytes.Buffer }

func (k *kvBuilder) open(name string) *kvBuilder { k.WriteByte(0); k.str(name); return k }
func (k *kvBuilder) close() *kvBuilder           { k.WriteByte(8); return k }
func (k *kvBuilder) str(s string)                { k.WriteString(s); k.WriteByte(0) }
func (k *kvBuilder) s(name, v string) *kvBuilder { k.WriteByte(1); k.str(name); k.str(v); return k }
func (k *kvBuilder) i(name string, v int32) *kvBuilder {
	k.WriteByte(2)
	k.str(name)
	binary.Write(k, binary.LittleEndian, v)
	return k
}

func TestParseKVReceipt(t *testing.T) {
	var k kvBuilder
	k.open("MessageObject").i("PaymentMethod", 4).i("LineItemCount", 2).open("lineitems").
		open("0").i("PackageID", 1).s("ItemDescription", "Portal 2").close().
		open("1").i("PackageID", 2).s("ItemDescription", "Portal 2 Soundtrack").close().
		close().close().close()
	kv, err := ParseKV(k.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	items := kv.Path("MessageObject", "lineitems")
	if items == nil || len(items.Children) != 2 || items.Children[1].Get("ItemDescription").Str() != "Portal 2 Soundtrack" {
		t.Fatalf("bad parse: %+v", kv)
	}
	if kv.Path("MessageObject", "LineItemCount").Int() != 2 {
		t.Fatal("int value")
	}
}

func TestParsePrice(t *testing.T) {
	for in, want := range map[string]float64{"1,23₴": 1.23, "$0.05": 0.05, "1 234,50 руб.": 1234.50, "12₸": 12, "€1.234,99": 1234.99} {
		if got := parsePrice(in); got != want {
			t.Errorf("parsePrice(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestXPForLevel(t *testing.T) {
	if XPForLevel(10) != 1000 || XPForLevel(11) != 1200 || XPForLevel(20) != 3000 {
		t.Fatal(XPForLevel(10), XPForLevel(11), XPForLevel(20))
	}
}
