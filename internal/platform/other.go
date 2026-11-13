//go:build !darwin && !windows

package platform

// Linux and other systems: no native window, the hub opens in the browser.
// Autostart uses an XDG .desktop file; notifications, clipboard and keep-awake use the usual
// desktop tools (notify-send, wl-paste or xclip, systemd-inhibit) when they are installed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LegacyAccount exists only on macOS (Idler 2003 was Mac-only).
func LegacyAccount() (string, string) { return "", "" }

const Name = "linux"

type unix struct{}

func New() Platform { return unix{} }

func DataDir() string {
	if d := os.Getenv("IDDLER_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "iddler")
}

func (unix) Secrets(dir string) Secrets {
	return sharedVault(dir, fileKey{filepath.Join(dir, "vault.key")}, nil)
}

func autostartPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "iddler.desktop")
}

func (unix) LoginItemEnabled() bool { _, err := os.Stat(autostartPath()); return err == nil }

func (unix) SetLoginItem(on bool) error {
	if !on {
		if err := os.Remove(autostartPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=Iddler\nExec=\"" + exe + "\" --background\nX-GNOME-Autostart-enabled=true\n"
	os.MkdirAll(filepath.Dir(autostartPath()), 0o755)
	return os.WriteFile(autostartPath(), []byte(entry), 0o644)
}

func (unix) KeepAwake() func() {
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return func() {}
	}
	cmd := exec.Command(path, "--what=idle:sleep", "--who=Iddler", "--why=Idling Steam games", "sleep", "infinity")
	if cmd.Start() != nil {
		return func() {}
	}
	go cmd.Wait()
	return func() { cmd.Process.Kill() }
}

func (unix) Notify(title, body string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		exec.Command(path, "-a", "Iddler", title, body).Start()
	}
}

func (unix) OpenURL(url string) { exec.Command("xdg-open", url).Start() }

func (unix) Clipboard() string {
	for _, c := range [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"}, {"xsel", "-b", "-o"}} {
		if path, err := exec.LookPath(c[0]); err == nil {
			if out, err := exec.Command(path, c[1:]...).Output(); err == nil {
				return strings.TrimRight(string(out), "\n")
			}
		}
	}
	return ""
}

func StartShell(c ShellConfig) error {
	if c.Open {
		unix{}.OpenURL(c.Base + "/?k=" + c.Key)
	}
	return nil
}
