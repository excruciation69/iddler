// SteamIdler: a Steam hub for macOS and Windows. Hours, cards, achievements, keys and more,
// for several accounts at once. https://github.com/excruciation69/SteamIdler
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"steamidler/internal/hub"
	"steamidler/internal/platform"
	"steamidler/internal/server"
	"steamidler/ui"
)

type instance struct {
	Port int    `json:"port"`
	Key  string `json:"key"`
}

func main() {
	background := flag.Bool("background", false, "start without opening the window")
	engine := flag.Bool("engine", false, "run the engine in this process (macOS starts it this way)")
	flag.Parse()

	debug.SetGCPercent(50) // the app mostly sleeps: keep the heap small
	debug.SetMemoryLimit(64 << 20)

	loginWasOn := platform.MigrateLegacy()
	dir := platform.DataDir()
	os.MkdirAll(dir, 0o700)
	instPath := filepath.Join(dir, "instance.json")
	if handOff(instPath, *background) {
		return
	}
	// On macOS the app you double-click only starts the engine and exits, so the next
	// double-click launches it again and can ask the running engine for the window.
	if platform.Name == "mac" && !*engine && spawnEngine(*background) == nil {
		return
	}

	p := platform.New()
	h := hub.New(dir, p)
	if !h.HasAccounts() {
		if account, token := platform.LegacyAccount(); account != "" && token != "" {
			h.ImportAccount(account, token)
		}
	}

	srv := server.New(h, p)
	if err := srv.Listen(); err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	raw, _ := json.Marshal(instance{srv.Port, srv.Key})
	os.WriteFile(instPath, raw, 0o600)

	var once sync.Once
	quit := func() {
		once.Do(func() {
			h.Shutdown()
			time.Sleep(1600 * time.Millisecond) // the menu bar script notices and exits
			os.Remove(instPath)
			os.Exit(0)
		})
	}
	srv.OnQuit = quit

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; quit() }()

	h.Start()
	if loginWasOn || p.LoginItemEnabled() {
		p.SetLoginItem(true) // refresh the path and arguments after an update or a move
	}
	if !*background {
		h.RequestShow() // the menu bar helper opens the window on its first poll
	}

	asset := "menubar.js"
	if platform.Name == "windows" {
		asset = "icon.ico"
	}
	script, _ := ui.FS.ReadFile(asset)
	icon, _ := ui.FS.ReadFile("AppIcon.icns")
	err := platform.StartShell(platform.ShellConfig{
		Base: srv.Base(), Key: srv.Key, Open: !*background, Dir: dir, Asset: script, Icon: icon,
		State: h.Brief, Alive: srv.MenuAlive, Quitting: h.Quitting,
		Action: func(a string) {
			switch a {
			case "start":
				h.StartAll()
			case "stop":
				h.StopAll()
			case "quit":
				go quit()
			}
		},
		OnExit: quit,
	})
	if err != nil {
		p.OpenURL(srv.Base() + "/?k=" + srv.Key)
	}
	select {}
}

// handOff asks an already running copy to show its window. Returns true if one is running.
func handOff(instPath string, background bool) bool {
	raw, err := os.ReadFile(instPath)
	if err != nil {
		return false
	}
	var inst instance
	if json.Unmarshal(raw, &inst) != nil || inst.Port == 0 {
		return false
	}
	c := &http.Client{Timeout: 2 * time.Second}
	base := fmt.Sprintf("http://127.0.0.1:%d", inst.Port)
	resp, err := c.Get(base + "/api/ping?k=" + inst.Key)
	if err != nil {
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	if !background {
		if r, err := c.Get(base + "/api/menu/show?k=" + inst.Key); err == nil {
			r.Body.Close()
		}
	}
	return true
}
