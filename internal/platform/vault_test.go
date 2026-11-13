package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVault(t *testing.T) {
	d := t.TempDir()
	v := newVault(d, fileKey{filepath.Join(d, "k")}, func(k string) (string, error) {
		if k == "old" {
			return "legacy", nil
		}
		return "", os.ErrNotExist
	})
	long := ""
	for i := 0; i < 900; i++ {
		long += "x"
	}
	if err := v.Set("token:a", long); err != nil {
		t.Fatal(err)
	}
	v2 := newVault(d, fileKey{filepath.Join(d, "k")}, nil)
	if s, _ := v2.Get("token:a"); s != long {
		t.Fatal("roundtrip")
	}
	if s, _ := v.Get("old"); s != "legacy" {
		t.Fatal("legacy")
	}
	v2.Delete("token:a")
	v3 := newVault(d, fileKey{filepath.Join(d, "k")}, nil)
	if _, err := v3.Get("token:a"); err == nil {
		t.Fatal("deleted")
	}
	os.Remove(filepath.Join(d, "k"))
	v4 := newVault(d, fileKey{filepath.Join(d, "k")}, nil)
	if err := v4.Set("x", "y"); err == nil {
		t.Fatal("must refuse to overwrite vault without key")
	}
}
