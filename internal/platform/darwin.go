//go:build darwin

package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const Name = "mac"

type mac struct{}

func New() Platform { return mac{} }

func DataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Iddler")
}

// ---- secrets: an encrypted vault whose 32-byte master key lives in the Keychain

const keychainService = "Iddler"

func (mac) Secrets(dir string) Secrets {
	return sharedVault(dir, keychainKey{fileKey{filepath.Join(dir, "vault.key")}}, keychainGet)
}

func quote(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }

func keychainGet(account string) (string, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-a", account, "-s", keychainService, "-w").Output()
	return strings.TrimSpace(string(out)), err
}

type keychainKey struct{ fallback fileKey }

func (k keychainKey) load() (string, error) {
	if h, err := keychainGet("vault-key"); err == nil && len(h) == 64 {
		return h, nil
	}
	return k.fallback.load()
}

func (k keychainKey) save(h string) error {
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -a %s -s %s -l %s -w %s\n",
		quote("vault-key"), quote(keychainService), quote("Iddler vault"), quote(h)))
	cmd.Run()
	if got, err := keychainGet("vault-key"); err == nil && got == h {
		return nil
	}
	// no Keychain access: keep the key in a private file instead
	return k.fallback.save(h)
}

// LegacyAccount finds a session saved by Idler 2003 (the previous single-account app).
func LegacyAccount() (account, token string) {
	home, _ := os.UserHomeDir()
	raw, err := os.ReadFile(filepath.Join(home, "Library", "Application Support", "Idler 2003", "config.json"))
	if err != nil {
		return "", ""
	}
	var old struct {
		AccountName string `json:"accountName"`
	}
	if json.Unmarshal(raw, &old) != nil || old.AccountName == "" {
		return "", ""
	}
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-a", old.AccountName, "-s", "Idler 2003", "-w").Output()
	if err != nil {
		return old.AccountName, ""
	}
	return old.AccountName, strings.TrimSpace(string(out))
}

// ---- login item (LaunchAgent)

const agentLabel = "com.excruciation.iddler"

func agentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

func (mac) LoginItemEnabled() bool { _, err := os.Stat(agentPath()); return err == nil }

func (mac) SetLoginItem(on bool) error {
	if !on {
		if err := os.Remove(agentPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(exe)
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + agentLabel + `</string>
  <key>ProgramArguments</key><array><string>` + esc + `</string><string>--engine</string><string>--background</string></array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`
	os.MkdirAll(filepath.Dir(agentPath()), 0o755)
	return os.WriteFile(agentPath(), []byte(plist), 0o644)
}

func (mac) KeepAwake() func() {
	cmd := exec.Command("/usr/bin/caffeinate", "-d", "-i", "-m", "-w", fmt.Sprint(os.Getpid())) // display, idle and disk sleep
	if cmd.Start() != nil {
		return func() {}
	}
	go cmd.Wait()
	return func() { cmd.Process.Kill() }
}

func (mac) Notify(title, body string) {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	exec.Command("/usr/bin/osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s"`, esc(body), esc(title))).Start()
}

func (mac) OpenURL(url string) { exec.Command("/usr/bin/open", url).Start() }

func (mac) Clipboard() string {
	out, _ := exec.Command("/usr/bin/pbpaste").Output()
	return string(out)
}

// ---- menu bar + window
//
// The window, the Dock icon and the menu bar square live in a small helper app that macOS builds
// on first launch: osacompile turns menubar.js (JavaScript for Automation) into a stay-open applet,
// and Iddler gives it its name and icon. Being a real app bundle is what puts "Iddler" in the Dock.
// If that fails for any reason, the same script runs under osascript (menu bar only), and if even
// that fails, the hub opens in the browser.

const shellID = "com.excruciation.iddler.window"

// appletRecipe changes whenever buildApplet changes, so existing helpers get rebuilt.
const appletRecipe = "2"

func StartShell(c ShellConfig) error {
	raw, _ := json.Marshal(map[string]string{"base": c.Base, "key": c.Key})
	if err := os.WriteFile(filepath.Join(c.Dir, "shell.json"), raw, 0o600); err != nil {
		return err
	}
	dir, _ := json.Marshal(c.Dir)
	script := strings.Replace(string(c.Asset), "__IDDLER_DIR__", string(dir), 1)
	if app, err := buildApplet(c.Dir, script, c.Icon); err == nil {
		go supervise(app, c)
		return nil
	} else {
		os.WriteFile(filepath.Join(c.Dir, "shell-error.txt"), []byte(err.Error()), 0o600)
	}
	return runScript(c, script)
}

// buildApplet compiles the helper app once per version of the script and icon.
func buildApplet(dir, script string, icon []byte) (string, error) {
	sum := sha256.Sum256(append([]byte(appletRecipe+script), icon...))
	stamp := hex.EncodeToString(sum[:8])
	root := filepath.Join(dir, "Shell")
	app := filepath.Join(root, "Iddler.app")
	stampPath := filepath.Join(app, "Contents", "Resources", "iddler.stamp")
	if old, err := os.ReadFile(stampPath); err == nil && string(old) == stamp {
		return app, nil
	}
	os.MkdirAll(root, 0o755)
	src := filepath.Join(root, "menubar.js")
	if err := os.WriteFile(src, []byte(script), 0o600); err != nil {
		return "", err
	}
	tmp := filepath.Join(root, "Iddler-build.app")
	os.RemoveAll(tmp)
	run := func(name string, args ...string) error {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %v: %s", filepath.Base(name), err, bytes.TrimSpace(out))
		}
		return nil
	}
	if err := run("/usr/bin/osacompile", "-l", "JavaScript", "-s", "-o", tmp, src); err != nil {
		return "", err
	}
	res := filepath.Join(tmp, "Contents", "Resources")
	if len(icon) > 0 {
		os.WriteFile(filepath.Join(res, "applet.icns"), icon, 0o644)
		// newer macOS takes the icon from Assets.car (CFBundleIconName) before applet.icns: drop it
		os.Remove(filepath.Join(res, "Assets.car"))
	}
	plist := filepath.Join(tmp, "Contents", "Info.plist")
	for _, kv := range [][2]string{{"CFBundleName", "Iddler"}, {"CFBundleDisplayName", "Iddler"}, {"CFBundleIdentifier", shellID}} {
		if err := run("/usr/bin/plutil", "-replace", kv[0], "-string", kv[1], plist); err != nil {
			return "", err
		}
	}
	run("/usr/bin/plutil", "-replace", "NSAppTransportSecurity", "-json", `{"NSAllowsLocalNetworking":true}`, plist)
	run("/usr/bin/plutil", "-remove", "LSUIElement", plist)
	run("/usr/bin/plutil", "-remove", "CFBundleIconName", plist)
	run("/usr/bin/plutil", "-replace", "CFBundleIconFile", "-string", "applet", plist)
	if err := os.WriteFile(filepath.Join(res, "iddler.stamp"), []byte(stamp), 0o644); err != nil {
		return "", err
	}
	// editing the bundle broke osacompile's signature; an ad-hoc one keeps macOS happy
	if err := run("/usr/bin/codesign", "--force", "--deep", "--sign", "-", tmp); err != nil {
		return "", err
	}
	exec.Command("/usr/bin/pkill", "-f", filepath.Join(app, "Contents", "MacOS")).Run()
	os.RemoveAll(app)
	if err := os.Rename(tmp, app); err != nil {
		return "", err
	}
	now := time.Now()
	os.Chtimes(app, now, now) // nudges Finder and the Dock to drop a cached icon
	exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", app).Run()
	return app, nil
}

// supervise starts the helper app and brings it back if it disappears while the engine runs.
func supervise(app string, c ShellConfig) {
	launch := func() { exec.Command("/usr/bin/open", "-g", app).Run() }
	launch()
	started := time.Now()
	for range time.Tick(3 * time.Second) {
		if c.Quitting() {
			return
		}
		if !c.Alive() && time.Since(started) > 12*time.Second {
			launch()
			started = time.Now()
		}
	}
}

// runScript is the fallback: the same script under osascript, menu bar only, no Dock icon.
func runScript(c ShellConfig, script string) error {
	path := filepath.Join(c.Dir, "menubar.js")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("/usr/bin/osascript", "-l", "JavaScript", path, "--child", c.Base, c.Key)
	if err := cmd.Start(); err != nil {
		return err
	}
	started := time.Now()
	go func() {
		cmd.Wait()
		if time.Since(started) < 8*time.Second && !c.Quitting() {
			mac{}.OpenURL(c.WindowURL()) // the native shell failed right away: use the browser
			return
		}
		c.OnExit()
	}()
	return nil
}
