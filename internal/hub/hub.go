// Package hub owns all accounts, global settings, the event stream for the UI,
// notifications, the Telegram bot and the clipboard watcher.
package hub

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"iddler/internal/bot"
	"iddler/internal/i18n"
	"iddler/internal/platform"
	"iddler/internal/steam"
)

type Settings struct {
	Lang           string         `json:"lang"`
	KeepAwake      bool           `json:"keepAwake"`
	LoginAtStartup bool           `json:"loginAtStartup"`
	MenuTime       bool           `json:"menuTime"`
	HideDock       bool           `json:"hideDock"`
	LAN            bool           `json:"lan"`
	Accent         string         `json:"accent,omitempty"`       // interface accent colour, #rrggbb
	InviteHidden   bool           `json:"inviteHidden,omitempty"` // the Steam group invitation was dismissed
	Accounts       []string       `json:"accounts"`
	Telegram       TelegramConfig `json:"telegram"`
}

type TelegramConfig struct {
	Token  string          `json:"token,omitempty"`
	ChatID int64           `json:"chatID,omitempty"`
	User   string          `json:"user,omitempty"`
	Bot    string          `json:"bot,omitempty"`
	Notify map[string]bool `json:"notify"`
}

type LogEntry struct {
	ID      int      `json:"id"`
	At      int64    `json:"at"`
	Account string   `json:"account"`
	Kind    string   `json:"kind"`
	Key     string   `json:"key"`
	Args    []string `json:"args"`
}

type Event struct {
	Name string
	Data []byte
}

type Hub struct {
	dir string
	p   platform.Platform

	mu       sync.Mutex
	cfg      Settings
	bots     map[string]*bot.Bot
	qr       *qrJob
	logs     []LogEntry
	logSeq   int
	subs     map[chan Event]bool
	dirty    map[string]bool
	gamesDty map[string]bool
	hubDirty bool
	show     bool
	quit     bool
	awake    func()
	bindCode string
	clipLast string

	tg     *telegram
	lanURL string
}

func New(dir string, p platform.Platform) *Hub {
	h := &Hub{dir: dir, p: p, bots: map[string]*bot.Bot{}, subs: map[chan Event]bool{},
		dirty: map[string]bool{}, gamesDty: map[string]bool{}}
	os.MkdirAll(filepath.Join(dir, "accounts"), 0o700)
	h.cfg = Settings{Lang: "ru", MenuTime: true, Telegram: TelegramConfig{Notify: map[string]bool{
		"cards": true, "goals": true, "pause": true, "keys": true, "free": true}}}
	if raw, err := os.ReadFile(h.settingsPath()); err == nil {
		json.Unmarshal(raw, &h.cfg)
	}
	if h.cfg.Telegram.Notify == nil {
		h.cfg.Telegram.Notify = map[string]bool{}
	}
	h.cfg.LoginAtStartup = p.LoginItemEnabled()
	for _, acc := range h.cfg.Accounts {
		id := accountID(acc)
		h.bots[id] = bot.New(id, filepath.Join(dir, "accounts", id), acc, h)
	}
	h.tg = newTelegram(h)
	go h.broadcaster()
	go h.watchers()
	return h
}

func (h *Hub) settingsPath() string { return filepath.Join(h.dir, "settings.json") }

func (h *Hub) save() {
	raw, _ := json.MarshalIndent(h.cfg, "", "  ")
	os.WriteFile(h.settingsPath(), raw, 0o600)
}

var reID = regexp.MustCompile(`[^a-z0-9_.-]`)

func accountID(account string) string { return reID.ReplaceAllString(strings.ToLower(account), "_") }

// Start logs in every account that has a saved session.
func (h *Hub) Start() {
	for _, b := range h.Bots() {
		if b.HasToken() {
			b.LoginSaved()
		}
	}
	h.tg.start()
}

func (h *Hub) Lang() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.Lang
}

func (h *Hub) T(key string, args ...string) string { return i18n.T(h.Lang(), key, args...) }

// ---------------------------------------------------------------- accounts

func (h *Hub) Bots() []*bot.Bot {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*bot.Bot
	for _, acc := range h.cfg.Accounts {
		if b := h.bots[accountID(acc)]; b != nil {
			out = append(out, b)
		}
	}
	return out
}

func (h *Hub) Bot(id string) *bot.Bot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bots[id]
}

// AddAccount creates (or reuses) an account and starts a password login.
func (h *Hub) AddAccount(account, password string, remember bool) (string, error) {
	account = strings.TrimSpace(account)
	if account == "" || password == "" {
		return "", errors.New("err.loginEmpty")
	}
	id := accountID(account)
	h.mu.Lock()
	b := h.bots[id]
	if b != nil && b.Summary().Status == "online" {
		// already signed in: just open it
		h.mu.Unlock()
		return id, nil
	}
	if b == nil {
		b = bot.New(id, filepath.Join(h.dir, "accounts", id), account, h)
		h.bots[id] = b
		h.cfg.Accounts = append(h.cfg.Accounts, account)
		h.save()
	}
	h.hubDirty = true
	h.mu.Unlock()
	b.SetRemember(remember)
	b.LoginPassword(password)
	return id, nil
}

// ImportAccount adds an account with an existing session (migration from Idler 2003).
func (h *Hub) ImportAccount(account, token string) {
	id := accountID(account)
	h.mu.Lock()
	if h.bots[id] != nil {
		h.mu.Unlock()
		return
	}
	h.SetSecret("token:"+account, token)
	h.bots[id] = bot.New(id, filepath.Join(h.dir, "accounts", id), account, h)
	h.cfg.Accounts = append(h.cfg.Accounts, account)
	h.save()
	h.mu.Unlock()
}

func (h *Hub) HasAccounts() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.cfg.Accounts) > 0
}

func (h *Hub) SetLANURL(u string) {
	h.mu.Lock()
	h.lanURL, h.hubDirty = u, true
	h.mu.Unlock()
}

func (h *Hub) RemoveAccount(id string) {
	h.mu.Lock()
	b := h.bots[id]
	if b == nil {
		h.mu.Unlock()
		return
	}
	delete(h.bots, id)
	var keep []string
	for _, a := range h.cfg.Accounts {
		if accountID(a) != id {
			keep = append(keep, a)
		}
	}
	h.cfg.Accounts = keep
	h.save()
	h.hubDirty = true
	h.mu.Unlock()
	b.Logout(true)
	b.SetMaFile(steam.MaFile{})
	b.Shutdown()
	os.RemoveAll(filepath.Join(h.dir, "accounts", id))
}

// ---------------------------------------------------------------- bot.Host

func (h *Hub) Log(botID, kind, key string, args ...string) {
	h.mu.Lock()
	h.logSeq++
	e := LogEntry{ID: h.logSeq, At: time.Now().Unix(), Account: botID, Kind: kind, Key: key, Args: args}
	h.logs = append(h.logs, e)
	if len(h.logs) > 500 {
		h.logs = h.logs[len(h.logs)-500:]
	}
	h.mu.Unlock()
	raw, _ := json.Marshal(e)
	h.broadcast(Event{"log", raw})
}

func (h *Hub) Changed(botID string) {
	h.mu.Lock()
	h.dirty[botID] = true
	h.mu.Unlock()
}

func (h *Hub) GamesChanged(botID string) {
	h.mu.Lock()
	h.gamesDty[botID] = true
	h.mu.Unlock()
}

func (h *Hub) Secret(key string) string {
	v, _ := h.p.Secrets(h.dir).Get(key)
	return v
}

func (h *Hub) SetSecret(key, value string) error { return h.p.Secrets(h.dir).Set(key, value) }

func (h *Hub) DeleteSecret(key string) { h.p.Secrets(h.dir).Delete(key) }

// Notify sends a system notification for important events and forwards to Telegram.
func (h *Hub) Notify(category, key string, args ...string) {
	text := h.T(key, args...)
	switch key {
	case "notify.goal", "notify.cardsDone", "notify.free", "notify.deal", "notify.key":
		h.p.Notify("Iddler", text)
	}
	h.tg.notify(category, text)
}

// ---------------------------------------------------------------- settings

func (h *Hub) Settings() Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.cfg
	s.Telegram.Token = "" // never sent to the UI
	return s
}

func (h *Hub) UpdateSettings(patch json.RawMessage) error {
	h.mu.Lock()
	old := h.cfg
	next := h.cfg
	next.Telegram.Notify = map[string]bool{}
	for k, v := range old.Telegram.Notify {
		next.Telegram.Notify[k] = v
	}
	if err := json.Unmarshal(patch, &next); err != nil {
		h.mu.Unlock()
		return err
	}
	next.Accounts, next.Telegram.Token, next.Telegram.ChatID = old.Accounts, old.Telegram.Token, old.Telegram.ChatID
	if next.Lang != "en" {
		next.Lang = "ru"
	}
	if !validColor(next.Accent) {
		next.Accent = ""
	}
	h.cfg = next
	h.hubDirty = true
	h.save()
	h.mu.Unlock()
	if next.LoginAtStartup != old.LoginAtStartup {
		if err := h.p.SetLoginItem(next.LoginAtStartup); err != nil {
			h.Log("", "err", "log.error", err.Error())
		}
	}
	return nil
}

// ---------------------------------------------------------------- events

func (h *Hub) Subscribe() (chan Event, []Event) {
	ch := make(chan Event, 256)
	init := []Event{h.hubEvent()}
	for _, b := range h.Bots() {
		init = append(init, gamesEvent(b), detailEvent(b))
	}
	h.mu.Lock()
	for _, l := range h.logs {
		raw, _ := json.Marshal(l)
		init = append(init, Event{"log", raw})
	}
	h.subs[ch] = true
	h.mu.Unlock()
	return ch, init
}

func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *Hub) broadcast(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

type hubState struct {
	Settings Settings      `json:"settings"`
	Accounts []bot.Summary `json:"accounts"`
	LANURL   string        `json:"lanURL"`
	Platform string        `json:"platform"`
	Version  string        `json:"version"`
}

var Version = "5.0.0"

func (h *Hub) hubEvent() Event {
	h.mu.Lock()
	lan := h.lanURL
	h.mu.Unlock()
	st := hubState{Settings: h.Settings(), LANURL: lan, Platform: platform.Name, Version: Version}
	for _, b := range h.Bots() {
		st.Accounts = append(st.Accounts, b.Summary())
	}
	raw, _ := json.Marshal(st)
	return Event{"hub", raw}
}

func detailEvent(b *bot.Bot) Event {
	raw, _ := json.Marshal(b.Detail(14))
	return Event{"bot", raw}
}

func gamesEvent(b *bot.Bot) Event {
	raw, _ := json.Marshal(map[string]any{"id": b.ID, "games": b.Games()})
	return Event{"games", raw}
}

// broadcaster batches changes: at most ~6 updates a second, plus a 1s tick while the UI is open.
func (h *Hub) broadcaster() {
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	lastFull := time.Now()
	for range t.C {
		h.mu.Lock()
		if len(h.subs) == 0 {
			h.dirty, h.gamesDty, h.hubDirty = map[string]bool{}, map[string]bool{}, false
			h.mu.Unlock()
			continue
		}
		dirty, games, hubDirty := h.dirty, h.gamesDty, h.hubDirty
		h.dirty, h.gamesDty, h.hubDirty = map[string]bool{}, map[string]bool{}, false
		full := time.Since(lastFull) >= time.Second
		h.mu.Unlock()

		for id := range games {
			if b := h.Bot(id); b != nil {
				h.broadcast(gamesEvent(b))
			}
		}
		if full {
			lastFull = time.Now()
			for _, b := range h.Bots() {
				if b.Active() || dirty[b.ID] {
					h.broadcast(detailEvent(b))
				}
			}
			h.broadcast(h.hubEvent())
			continue
		}
		for id := range dirty {
			if b := h.Bot(id); b != nil {
				h.broadcast(detailEvent(b))
			}
		}
		if hubDirty || len(dirty) > 0 {
			h.broadcast(h.hubEvent())
		}
	}
}

// ---------------------------------------------------------------- watchers

func (h *Hub) watchers() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for range t.C {
		active := false
		var clipBot *bot.Bot
		for _, b := range h.Bots() {
			if b.Active() {
				active = true
			}
			if clipBot == nil && b.WatchesClipboard() {
				clipBot = b
			}
		}
		h.mu.Lock()
		want := active && h.cfg.KeepAwake
		if want && h.awake == nil {
			h.awake = h.p.KeepAwake()
		} else if !want && h.awake != nil {
			h.awake()
			h.awake = nil
		}
		h.mu.Unlock()

		if clipBot != nil {
			text := h.p.Clipboard()
			h.mu.Lock()
			fresh := text != h.clipLast
			h.clipLast = text
			h.mu.Unlock()
			if fresh && len(text) < 20000 && len(bot.FindKeys(text)) > 0 {
				if n := clipBot.QueueKeys(text); n > 0 {
					h.Log(clipBot.ID, "info", "log.clipboardKeys", itoa(n))
				}
			}
		}
	}
}

// ---------------------------------------------------------------- menu bar / tray

func (h *Hub) Brief() platform.MenuState {
	br := platform.MenuState{Status: "offline"}
	online := 0
	for _, b := range h.Bots() {
		s := b.Summary()
		br.Accounts++
		if s.Status == "online" {
			online++
		}
		if s.Blocked {
			br.Blocked = true
		}
		if s.HoursOn || s.CardsOn {
			br.Running++
			br.SessionMs = max(br.SessionMs, s.SessionMs)
		}
	}
	h.mu.Lock()
	br.Lang, br.MenuTime, br.HideDock, br.Show, br.Quit = h.cfg.Lang, h.cfg.MenuTime, h.cfg.HideDock, h.show, h.quit
	h.show = false
	h.mu.Unlock()
	br.Active = br.Running > 0
	switch {
	case br.Running > 0:
		br.Status = "idling"
	case online > 0:
		br.Status = "online"
	}
	br.Labels = map[string]string{}
	for _, k := range []string{"menu.open", "menu.startAll", "menu.stopAll", "menu.quit", "menu.offline",
		"menu.online", "menu.idling", "menu.noEngine"} {
		br.Labels[k] = i18n.T(br.Lang, k, itoa(br.Running))
	}
	return br
}

func (h *Hub) RequestShow() {
	h.mu.Lock()
	h.show = true
	h.mu.Unlock()
}

func (h *Hub) StartAll() {
	for _, b := range h.Bots() {
		b.SetRun(bot.ModHours, true)
	}
}

func (h *Hub) StopAll() {
	for _, b := range h.Bots() {
		b.StopAll()
	}
}

// Quitting reports whether Shutdown has begun.
func (h *Hub) Quitting() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.quit
}

func (h *Hub) Shutdown() {
	h.mu.Lock()
	h.quit = true
	if h.awake != nil {
		h.awake()
		h.awake = nil
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, b := range h.Bots() {
		wg.Add(1)
		go func(b *bot.Bot) { defer wg.Done(); b.Shutdown() }(b)
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond) // let the log-off messages leave
}

// validColor accepts "#rrggbb" only, so a setting can never inject anything into the page's CSS.
func validColor(c string) bool {
	if c == "" {
		return true
	}
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
