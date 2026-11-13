//go:build !darwin && !windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("STEAMIDLER_DIR", "")
	old := filepath.Join(home, ".config", "iddler")
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "config.json"), []byte("{}"), 0o600)
	os.MkdirAll(filepath.Join(home, ".config", "autostart"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "autostart", "iddler.desktop"), nil, 0o600)

	if !MigrateLegacy() {
		t.Fatal("old autostart entry not detected")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "steamidler", "config.json")); err != nil {
		t.Fatal("data folder not moved:", err)
	}
	if MigrateLegacy() {
		t.Fatal("second run should do nothing")
	}
}
