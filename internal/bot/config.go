package bot

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"

	"iddler/internal/steam"
)

// Module ids
const (
	ModHours   = "hours"
	ModCards   = "cards"
	ModAch     = "ach"
	ModKeys    = "keys"
	ModFree    = "free"
	ModProfile = "profile"
	ModTrade   = "trade"
	ModStats   = "stats"
)

var AllModules = []string{ModHours, ModCards, ModAch, ModKeys, ModFree, ModProfile, ModTrade, ModStats}

type Config struct {
	Version   int             `json:"version"`
	Account   string          `json:"account"`
	Remember  bool            `json:"remember"`
	LogonID   uint32          `json:"logonID"`
	Persona   string          `json:"persona"`   // online | away | invisible | offline
	Favorites []uint32        `json:"favorites"` // games starred in the library
	Modules   map[string]bool `json:"modules"`
	Priority  []string        `json:"priority"` // cards, goals, hours
	OnPlay    string          `json:"onPlay"`   // pause | kick
	Guard     string          `json:"guard"`    // manual | mafile

	Hours   HoursConfig   `json:"hours"`
	Cards   CardsConfig   `json:"cards"`
	Ach     AchConfig     `json:"ach"`
	Keys    KeysConfig    `json:"keys"`
	Free    FreeConfig    `json:"free"`
	Profile ProfileConfig `json:"profile"`
	Trade   TradeConfig   `json:"trade"`
	Market  MarketConfig  `json:"market"`

	// run state survives restarts, so a Mac reboot resumes idling
	HoursOn bool `json:"hoursOn"`
	CardsOn bool `json:"cardsOn"`
}

type HoursConfig struct {
	Selected []uint32           `json:"selected"`
	Goals    map[uint32]float64 `json:"goals"` // appid -> target hours
	Manual   []steam.OwnedGame  `json:"manual"`
	Schedule Schedule           `json:"schedule"`
}

type Schedule struct {
	On   bool   `json:"on"`
	From string `json:"from"` // "23:00"
	To   string `json:"to"`   // "08:00"
}

type CardsConfig struct {
	Mode      string   `json:"mode"` // one | many
	TwoHour   bool     `json:"twoHour"`
	Blacklist []uint32 `json:"blacklist"` // never farm these
	Whitelist []uint32 `json:"whitelist"` // if set, farm only these
	ThenHours bool     `json:"thenHours"` // when the drops run out, start idling hours
}

type AchConfig struct {
	Human     bool     `json:"human"`
	ShowVAC   bool     `json:"showVAC"`
	MinDelay  int      `json:"minDelay"` // minutes between unlocks in the human-like mode
	MaxDelay  int      `json:"maxDelay"`
	Window    Schedule `json:"window"`    // unlock only inside this time window
	IdleWhile bool     `json:"idleWhile"` // keep the game running while unlocking
}

type MarketConfig struct {
	AutoConfirm bool   `json:"autoConfirm"` // confirm new listings with the .maFile
	Mode        string `json:"mode"`        // lowest | median | buy
	Undercut    int    `json:"undercut"`    // cents below the base price
	Min         int    `json:"min"`         // cents, 0 = no limit
	Max         int    `json:"max"`
}

type KeysConfig struct {
	Clipboard bool `json:"clipboard"`
}

type FreeConfig struct {
	WishlistOn  bool `json:"wishlistOn"`
	Promos      bool `json:"promos"`
	Queue       bool `json:"queue"`
	Sticker     bool `json:"sticker"`
	WishlistPct int  `json:"wishlistPct"`
}

type ProfileConfig struct {
	SlideOn  bool     `json:"slideOn"`
	Names    []string `json:"names"`
	Interval int      `json:"interval"` // minutes
	Shuffle  bool     `json:"shuffle"`

	AutoOn       bool     `json:"autoOn"`
	AutoText     string   `json:"autoText"`
	AutoCooldown int      `json:"autoCooldown"` // minutes
	AutoExcept   []string `json:"autoExcept"`   // steamid64 as strings (JS-safe)

	NickOn    bool   `json:"nickOn"`
	NickDay   string `json:"nickDay"`
	NickNight string `json:"nickNight"`
	NickFrom  string `json:"nickFrom"` // night starts
	NickTo    string `json:"nickTo"`
}

type TradeConfig struct {
	Craft       bool     `json:"craft"`
	Foil        bool     `json:"foil"`
	Except      []uint32 `json:"except"`
	Gifts       bool     `json:"gifts"`
	FriendsOnly bool     `json:"friendsOnly"`
}

const configVersion = 4

func allModules() map[string]bool {
	m := map[string]bool{}
	for _, id := range AllModules {
		m[id] = true
	}
	return m
}

func defaultConfig(account string) Config {
	return Config{
		Account:  account,
		Remember: true,
		LogonID:  100000 + uint32(rand.Int31n(2000000000)),
		Persona:  "online",
		Version:  configVersion,
		Modules:  allModules(),
		Priority: []string{ModCards, "goals", ModHours},
		OnPlay:   "pause",
		Guard:    "manual",
		Hours:    HoursConfig{Goals: map[uint32]float64{}, Schedule: Schedule{From: "23:00", To: "08:00"}},
		Cards:    CardsConfig{Mode: "one", TwoHour: true},
		Ach:      AchConfig{Human: true, MinDelay: 3, MaxDelay: 20, Window: Schedule{From: "10:00", To: "23:00"}},
		Market:   MarketConfig{Mode: "lowest", Undercut: 1},
		Free:     FreeConfig{WishlistPct: 50},
		Profile: ProfileConfig{Interval: 15, AutoCooldown: 60, AutoText: "AFK",
			NickFrom: "23:00", NickTo: "08:00"},
		Trade: TradeConfig{FriendsOnly: true},
	}
}

// Data is what the bot remembers besides settings: history and logs of past actions.
type Data struct {
	Keys     []KeyRecord               `json:"keys"`
	Claimed  []ClaimRecord             `json:"claimed"`
	Drops    []DropRecord              `json:"drops"`
	Crafted  []CraftRecord             `json:"crafted"`
	History  map[string]map[uint32]int `json:"history"` // "2026-11-09" -> appid -> minutes
	Notified map[uint32]int            `json:"notified"`
	Deals    []Deal                    `json:"deals"`
	Prices   map[string]Price          `json:"prices"`
	SalesDay string                    `json:"salesDay"`
	Promos   map[string]bool           `json:"promos"` // already claimed promos
}

type Price struct {
	Value float64 `json:"v"`
	At    int64   `json:"at"`
}

func (b *Bot) cfgPath() string  { return filepath.Join(b.dir, "account.json") }
func (b *Bot) dataPath() string { return filepath.Join(b.dir, "data.json") }

func writeJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		os.Rename(tmp, path)
	}
}

func (b *Bot) saveConfig() { writeJSON(b.cfgPath(), b.cfg) }
func (b *Bot) saveData()   { writeJSON(b.dataPath(), b.data) }

func (b *Bot) load() {
	b.cfg = defaultConfig(b.cfg.Account)
	if raw, err := os.ReadFile(b.cfgPath()); err == nil {
		json.Unmarshal(raw, &b.cfg)
	}
	if b.cfg.Version < 4 {
		// 3.x had per-account module switches; sections are always available now,
		// so background features that were behind a disabled module stay off
		if !b.cfg.Modules[ModFree] {
			b.cfg.Free = FreeConfig{WishlistPct: 50}
		} else if b.cfg.Free.WishlistPct > 0 {
			b.cfg.Free.WishlistOn = true
		}
		if !b.cfg.Modules[ModTrade] {
			b.cfg.Trade.Craft, b.cfg.Trade.Gifts = false, false
		}
		if !b.cfg.Modules[ModProfile] {
			b.cfg.Profile.SlideOn, b.cfg.Profile.AutoOn, b.cfg.Profile.NickOn = false, false, false
		}
		b.cfg.Version = configVersion
	}
	b.cfg.Modules = allModules()
	if b.cfg.Hours.Goals == nil {
		b.cfg.Hours.Goals = map[uint32]float64{}
	}
	if raw, err := os.ReadFile(b.dataPath()); err == nil {
		json.Unmarshal(raw, &b.data)
	}
	if b.data.History == nil {
		b.data.History = map[string]map[uint32]int{}
	}
	if b.data.Notified == nil {
		b.data.Notified = map[uint32]int{}
	}
	if b.data.Prices == nil {
		b.data.Prices = map[string]Price{}
	}
	if b.data.Promos == nil {
		b.data.Promos = map[string]bool{}
	}
}
