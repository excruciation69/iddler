package bot

// Snapshots for the UI and the controls it calls.

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"iddler/internal/steam"
)

type Summary struct {
	ID        string          `json:"id"`
	Account   string          `json:"account"`
	Name      string          `json:"name"`
	Avatar    string          `json:"avatar"`
	Status    string          `json:"status"`
	HoursOn   bool            `json:"hoursOn"`
	CardsOn   bool            `json:"cardsOn"`
	Playing   int             `json:"playing"`
	Blocked   bool            `json:"blocked"`
	SessionMs int64           `json:"sessionMs"`
	TodaySec  int             `json:"todaySec"`
	WeekSec   int             `json:"weekSec"`
	CardsLeft int             `json:"cardsLeft"`
	Modules   map[string]bool `json:"modules"`
	HasToken  bool            `json:"hasToken"`
}

func (b *Bot) summary(now time.Time) Summary {
	mods := map[string]bool{}
	for k, v := range b.cfg.Modules {
		mods[k] = v
	}
	s := Summary{ID: b.ID, Account: b.cfg.Account, Name: b.displayName(), Avatar: b.avatar, Status: b.status,
		HoursOn: b.cfg.HoursOn, CardsOn: b.cfg.CardsOn, Playing: len(b.playing), Blocked: b.blocked,
		SessionMs: b.sessionMs(), CardsLeft: b.cardsLeft(), Modules: mods, HasToken: b.token != ""}
	for i := 0; i < 7; i++ {
		for _, sec := range b.data.History[now.AddDate(0, 0, -i).Format("2006-01-02")] {
			if i == 0 {
				s.TodaySec += sec
			}
			s.WeekSec += sec
		}
	}
	return s
}

func (b *Bot) Summary() Summary {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.summary(time.Now())
}

type Detail struct {
	Summary
	Config       Config             `json:"cfg"`
	SteamID      string             `json:"steamID"`
	GuardKind    string             `json:"guardKind"`
	GuardDomain  string             `json:"guardDomain"`
	GuardWrong   bool               `json:"guardWrong"`
	HasTOTP      bool               `json:"hasTOTP"`
	TokenExpiry  int64              `json:"tokenExpiry"`
	PlayingIDs   []uint32           `json:"playingIDs"`
	Custom       string             `json:"custom"`
	Hours        map[uint32]float64 `json:"hours"` // live playtime of selected/goal games
	GamesLoad    bool               `json:"gamesLoading"`
	Cards        []steam.BadgeGame  `json:"cards"`
	CardsSkipped []steam.BadgeGame  `json:"cardsSkipped"`
	CardsAt      int64              `json:"cardsAt"`
	Drops        []DropRecord       `json:"drops"`
	AchJob       *achJob            `json:"achJob"`
	KeyQueue     []string           `json:"keyQueue"`
	KeyPause     int64              `json:"keyPause"`
	Keys         []KeyRecord        `json:"keys"`
	Claimed      []ClaimRecord      `json:"claimed"`
	Deals        []Deal             `json:"deals"`
	Crafted      []CraftRecord      `json:"crafted"`
	Friends      []Friend           `json:"friends"`
	Days         []DayStat          `json:"days"`
	PerGame      []GameStat         `json:"perGame"`
	Level        steam.Level        `json:"level"`
	Inventory    invState           `json:"inventory"`
	MarketJob    *marketJob         `json:"marketJob"`
	CanConfirm   bool               `json:"canConfirm"`
	Currency     int                `json:"currency"`
}

type Friend struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DayStat struct {
	Day string `json:"day"`
	Sec int    `json:"sec"`
}

type GameStat struct {
	AppID uint32 `json:"appid"`
	Name  string `json:"name"`
	Sec   int    `json:"sec"`
}

func (b *Bot) Detail(days int) Detail {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	var cfg Config // deep copy: the snapshot is serialized after the lock is released
	raw, _ := json.Marshal(b.cfg)
	json.Unmarshal(raw, &cfg)
	d := Detail{Summary: b.summary(now), Config: cfg, GuardKind: b.guardKind, GuardDomain: b.guardDomain,
		GuardWrong: b.guardWrong, HasTOTP: b.totp != "", PlayingIDs: b.playing, Custom: b.customNow,
		GamesLoad: b.gamesLoading, Cards: append([]steam.BadgeGame(nil), b.cards...), CardsSkipped: append([]steam.BadgeGame(nil), b.cardsSkipped...), Drops: lastN(b.data.Drops, 12), AchJob: b.achJobCopy(),
		KeyQueue: maskAll(b.keyQueue), Keys: lastN(b.data.Keys, 300), Claimed: lastN(b.data.Claimed, 50),
		Deals: append([]Deal(nil), b.data.Deals...), Crafted: lastN(b.data.Crafted, 30), Level: b.level, Inventory: b.inv,
		MarketJob: b.mjob, CanConfirm: b.identity != "" && b.deviceID != "", Currency: b.currency,
		Hours: map[uint32]float64{}}
	if b.steamID != 0 {
		d.SteamID = fmt.Sprint(b.steamID)
	}
	if !b.cardsAt.IsZero() {
		d.CardsAt = b.cardsAt.Unix()
	}
	if b.keyPause.After(now) {
		d.KeyPause = b.keyPause.Unix()
	}
	if ti, err := steam.ParseToken(b.token); err == nil {
		d.TokenExpiry = ti.Expires
	}
	for _, id := range b.cfg.Hours.Selected {
		d.Hours[id] = b.hoursOf(id)
	}
	for id, name := range b.friends {
		d.Friends = append(d.Friends, Friend{fmt.Sprint(id), name})
	}
	sort.Slice(d.Friends, func(i, j int) bool { return strings.ToLower(d.Friends[i].Name) < strings.ToLower(d.Friends[j].Name) })
	per := map[uint32]int{}
	for i := days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		sum := 0
		for id, sec := range b.data.History[day] {
			sum += sec
			per[id] += sec
		}
		d.Days = append(d.Days, DayStat{day, sum})
	}
	for id, sec := range per {
		d.PerGame = append(d.PerGame, GameStat{id, b.gameName(id), sec})
	}
	// a stable order (hours, then name) keeps the list from jumping between updates
	sort.Slice(d.PerGame, func(i, j int) bool {
		a, b := d.PerGame[i], d.PerGame[j]
		if a.Sec/60 != b.Sec/60 {
			return a.Sec > b.Sec
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if len(d.PerGame) > 300 {
		d.PerGame = d.PerGame[:300]
	}
	return d
}

func (b *Bot) achJobCopy() *achJob {
	if b.achJob == nil {
		return nil
	}
	j := *b.achJob
	j.Pending = append([]string(nil), j.Pending...)
	if !j.next.IsZero() {
		j.NextAt = j.next.Unix()
	}
	return &j
}

func (b *Bot) Games() []steam.OwnedGame {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]steam.OwnedGame(nil), b.games...)
}

func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		s = s[len(s)-n:]
	}
	out := make([]T, len(s))
	for i := range s {
		out[i] = s[len(s)-1-i] // newest first
	}
	return out
}

func maskAll(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = maskKey(k)
	}
	return out
}

// RevealKey returns the full key of a history entry (the UI shows masked keys).
func (b *Bot) RevealKey(masked string, at int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.data.Keys {
		if r.At == at && maskKey(r.Key) == masked {
			return r.Key
		}
	}
	return ""
}

// ---------------------------------------------------------------- controls

// Update merges a partial settings object from the UI.
func (b *Bot) Update(patch json.RawMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.cfg
	next := b.cfg
	next.Hours.Goals = map[uint32]float64{}
	for k, v := range old.Hours.Goals {
		next.Hours.Goals[k] = v
	}
	next.Modules = map[string]bool{}
	for k, v := range old.Modules {
		next.Modules[k] = v
	}
	if err := json.Unmarshal(patch, &next); err != nil {
		return err
	}
	next.Account, next.LogonID, next.HoursOn, next.CardsOn, next.Version = old.Account, old.LogonID, old.HoursOn, old.CardsOn, old.Version
	next.Modules = allModules()
	next.Hours.Manual = old.Hours.Manual
	for id, g := range next.Hours.Goals {
		if g <= 0 {
			delete(next.Hours.Goals, id)
		} else if g != old.Hours.Goals[id] {
			delete(b.data.Notified, id)
		}
	}
	next.Hours.Selected = dedupe(next.Hours.Selected, maxSlots)
	if len(next.Profile.Names) > 20 {
		next.Profile.Names = next.Profile.Names[:20]
	}
	b.cfg = next
	if !slices.Equal(old.Cards.Blacklist, next.Cards.Blacklist) || !slices.Equal(old.Cards.Whitelist, next.Cards.Whitelist) {
		b.refilterCards()
	}
	b.saveConfig()
	if old.Persona != next.Persona || old.Profile.NickOn != next.Profile.NickOn ||
		old.Profile.NickDay != next.Profile.NickDay || old.Profile.NickNight != next.Profile.NickNight {
		b.nickSet = ""
		b.applyPersona()
	}
	if strings.Join(old.Profile.Names, "\n") != strings.Join(next.Profile.Names, "\n") {
		b.slideIdx = 0
	}
	b.applyPlaying()
	b.changed()
	return nil
}

// SetRun starts or stops the hours or cards module.
func (b *Bot) SetRun(module string, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch module {
	case ModHours:
		if on && len(b.cfg.Hours.Selected) == 0 {
			return errors.New("err.noGames")
		}
		if on && !b.cfg.HoursOn {
			b.sessDur = 0
			b.log("ok", "log.hoursStarted", itoa(len(b.cfg.Hours.Selected)))
		} else if !on && b.cfg.HoursOn {
			b.log("info", "log.hoursStopped")
		}
		b.cfg.HoursOn = on
	case ModCards:
		if on && !b.cfg.CardsOn {
			b.cardsAt = time.Time{}
			b.log("ok", "log.cardsStarted")
		} else if !on && b.cfg.CardsOn {
			b.log("info", "log.cardsStopped")
		}
		b.cfg.CardsOn = on
	default:
		return errors.New("unknown module")
	}
	b.saveConfig()
	b.applyPlaying()
	b.changed()
	return nil
}

func (b *Bot) AddManualGame(id uint32, name string) error {
	if id == 0 {
		return errors.New("err.badAppID")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.game(id) != nil {
		return errors.New("err.gameExists")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "App " + itoa(int(id))
	}
	g := steam.OwnedGame{AppID: id, Name: name, Manual: true}
	b.cfg.Hours.Manual = append(b.cfg.Hours.Manual, g)
	b.saveConfig()
	b.games = append(b.games, g)
	sort.Slice(b.games, func(i, j int) bool { return strings.ToLower(b.games[i].Name) < strings.ToLower(b.games[j].Name) })
	b.host.GamesChanged(b.ID)
	b.changed()
	return nil
}

func (b *Bot) RemoveManualGame(id uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	keep := b.cfg.Hours.Manual[:0]
	for _, m := range b.cfg.Hours.Manual {
		if m.AppID != id {
			keep = append(keep, m)
		}
	}
	b.cfg.Hours.Manual = keep
	var sel []uint32
	for _, s := range b.cfg.Hours.Selected {
		if s != id {
			sel = append(sel, s)
		}
	}
	b.cfg.Hours.Selected = sel
	var games []steam.OwnedGame
	for _, g := range b.games {
		if !(g.Manual && g.AppID == id) {
			games = append(games, g)
		}
	}
	b.games = games
	b.saveConfig()
	b.applyPlaying()
	b.host.GamesChanged(b.ID)
	b.changed()
}

func (b *Bot) SetRemember(on bool) {
	b.mu.Lock()
	b.cfg.Remember = on
	b.saveConfig()
	b.mu.Unlock()
}

func (b *Bot) WatchesClipboard() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Keys.Clipboard && b.status == "online"
}

func (b *Bot) Active() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.HoursOn || b.cfg.CardsOn
}

func (b *Bot) StopAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg.HoursOn, b.cfg.CardsOn = false, false
	b.saveConfig()
	b.applyPlaying()
	b.changed()
}

func dedupe(ids []uint32, limit int) []uint32 {
	seen := map[uint32]bool{}
	var out []uint32
	for _, id := range ids {
		if id != 0 && !seen[id] && len(out) < limit {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
