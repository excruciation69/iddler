package bot

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"iddler/internal/steam"
	"iddler/internal/steam/steamtest"
)

type host struct {
	mu      sync.Mutex
	logs    []string
	secrets map[string]string
	notes   []string
}

func (h *host) Log(_, kind, key string, args ...string) {
	h.mu.Lock()
	h.logs = append(h.logs, key+" "+strings.Join(args, "|"))
	h.mu.Unlock()
}
func (h *host) Changed(string)      {}
func (h *host) GamesChanged(string) {}
func (h *host) Notify(cat, key string, args ...string) {
	h.mu.Lock()
	h.notes = append(h.notes, key)
	h.mu.Unlock()
}
func (h *host) Secret(k string) string { h.mu.Lock(); defer h.mu.Unlock(); return h.secrets[k] }
func (h *host) SetSecret(k, v string) error {
	h.mu.Lock()
	h.secrets[k] = v
	h.mu.Unlock()
	return nil
}
func (h *host) DeleteSecret(k string) { h.mu.Lock(); delete(h.secrets, k); h.mu.Unlock() }

func (h *host) has(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.logs {
		if strings.HasPrefix(l, key) {
			return true
		}
	}
	return false
}

func wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

func (b *Bot) get(f func(b *Bot) bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return f(b)
}

const badgesPage = `<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/730/"></a>
<div class="badge_title">Counter-Strike 2&nbsp;<span class="badge_view_details">View details</span></div>
<div class="badge_title_stats_playtime">&nbsp;12.5 hrs on record</div>
<div class="badge_title_stats_drops"><span class="progress_info_bold">%d card drops remaining</span></div></div>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/41000/"></a>
<div class="badge_title">Serious Sam: The Second Encounter&nbsp;<span>View details</span></div>
<div class="badge_title_stats_playtime">&nbsp;0.1 hrs on record</div>
<div class="badge_title_stats_drops"><span class="progress_info_bold">2 card drops remaining</span></div></div>
<div class="badge_row is_link"><a class="badge_row_overlay" href="https://steamcommunity.com/id/x/gamecards/440/"></a>
<div class="badge_title">Team Fortress 2&nbsp;</div><div class="badge_title_stats_playtime">&nbsp;1,024.0 hrs on record</div>
<span class="progress_info_bold">No card drops remaining</span>
<a class="badge_craft_button" href="https://steamcommunity.com/id/x/gamecards/440/">Ready</a></div>`

func TestAccountFlow(t *testing.T) {
	f := steamtest.Start()
	defer f.Close()
	keyDelay = 50 * time.Millisecond
	h := &host{secrets: map[string]string{}}
	b := New("gleb", t.TempDir(), "gleb", h)
	defer b.Shutdown()

	// wrong password
	b.LoginPassword("nope")
	wait(t, "bad password", func() bool {
		return h.has("log.badPassword") && b.get(func(b *Bot) bool { return b.status == "offline" })
	})

	// email Steam Guard: wrong code, then the right one
	b.LoginPassword("secret")
	wait(t, "guard", func() bool {
		return b.get(func(b *Bot) bool {
			return b.status == "guard" && b.guardKind == "email" && b.guardDomain == "gmail.com"
		})
	})
	b.Guard("zzzzz")
	wait(t, "wrong code", func() bool { return b.get(func(b *Bot) bool { return b.guardWrong }) })
	b.Guard("ab12c")
	wait(t, "online", func() bool { return b.get(func(b *Bot) bool { return b.status == "online" && b.name == "Gleb" }) })
	if !strings.HasPrefix(h.Secret("token:gleb"), "ey") {
		t.Fatal("token not saved")
	}
	wait(t, "games", func() bool { return len(b.Games()) == 3 })
	wait(t, "friends", func() bool { return b.get(func(b *Bot) bool { _, ok := b.friends[76561198000000002]; return ok }) })

	// hours with a custom name from the slideshow
	patch := `{"modules":{"profile":true},"profile":{"slideOn":true,"names":["Serious Sam: The Third Encounter [Build 0.3.1 Internal]"]},"hours":{"selected":[730,440]}}`
	if err := b.Update(json.RawMessage(patch)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetRun(ModHours, true); err != nil {
		t.Fatal(err)
	}
	wait(t, "playing", func() bool {
		return f.LastPlayed() == "Serious Sam: The Third Encounter [Build 0.3.1 Internal],730,440"
	})

	// a goal that is already reached drops the game
	b.Update(json.RawMessage(`{"hours":{"goals":{"440":0.5}}}`))
	b.tick()
	wait(t, "goal", func() bool { return f.LastPlayed() == "Serious Sam: The Third Encounter [Build 0.3.1 Internal],730" })
	if !h.has("log.goalReached") {
		t.Fatal("goal not logged")
	}

	// played on another device: pause, then resume
	f.Push(steam.EMsgClientPlayingSessionState, steam.NewPB().Bool(2, true).Uint(3, 570))
	wait(t, "blocked", func() bool { return b.get(func(b *Bot) bool { return b.blocked && len(b.playing) == 0 }) })
	f.Push(steam.EMsgClientPlayingSessionState, steam.NewPB().Bool(2, false))
	wait(t, "unblocked", func() bool { return b.get(func(b *Bot) bool { return !b.blocked }) })

	// keys: one good, one bad, one owned, plus a duplicate in the text
	n := b.QueueKeys("here: AAAAA-BBBBB-CCCCC and BADXX-11111-22222, OWNXX-33333-44444 aaaaa-bbbbb-ccccc")
	if n != 3 {
		t.Fatalf("queued %d", n)
	}
	wait(t, "keys done", func() bool { return b.get(func(b *Bot) bool { return len(b.data.Keys) == 3 }) })
	b.mu.Lock()
	got := []string{b.data.Keys[0].Result + ":" + b.data.Keys[0].Game, b.data.Keys[1].Result, b.data.Keys[2].Result}
	b.mu.Unlock()
	if strings.Join(got, ",") != "ok:Portal 2,bad,owned" {
		t.Fatalf("keys: %v", got)
	}

	// achievements (Russian names, global %, protected flag), unlock one now
	list, err := b.Achievements(41000, "ru")
	if err != nil || len(list) != 3 {
		t.Fatalf("achievements: %v %v", list, err)
	}
	if list[0].Name != "Первая кровь" || list[0].Percent != 71.5 || !list[2].Protected || list[1].Unlocked {
		t.Fatalf("achievement fields: %+v", list)
	}
	if err := b.SetAchievements(41000, []string{"ACH_A", "ACH_P"}, true, false); err != nil {
		t.Fatal(err)
	}
	_, stored, _ := f.Snapshot()
	if len(stored) != 1 || stored[0] != "1=1 crc=77" {
		t.Fatalf("stored: %v", stored)
	}
	list, _ = b.Achievements(41000, "en")
	if !list[0].Unlocked || list[0].Name != "First Blood" {
		t.Fatalf("after unlock: %+v", list[0])
	}

	// auto-reply, once per cooldown
	b.Update(json.RawMessage(`{"profile":{"autoOn":true,"autoText":"AFK"}}`))
	f.IncomingChat(76561198000000002, "hi")
	f.IncomingChat(76561198000000002, "hello?")
	wait(t, "auto reply", func() bool { c, _, _ := f.Snapshot(); return len(c) == 1 })
	time.Sleep(200 * time.Millisecond)
	if c, _, _ := f.Snapshot(); len(c) != 1 || c[0] != "76561198000000002:AFK" {
		t.Fatalf("chats: %v", c)
	}

	// card farming takes priority: the new game is idled first (2 hour rule)
	f.Badges = strings.Replace(badgesPage, "%d", "3", 1)
	b.SetRun(ModCards, true)
	b.refreshCards()
	if b.get(func(b *Bot) bool { return len(b.cards) != 2 || len(b.craftable) != 1 }) {
		t.Fatalf("cards parsed: %+v", b.cards)
	}
	wait(t, "cards first", func() bool {
		return strings.HasPrefix(f.LastPlayed(), "Serious Sam: The Third Encounter [Build 0.3.1 Internal],41000,730")
	})
	f.Badges = strings.Replace(badgesPage, "%d", "1", 1)
	b.refreshCards()
	if !h.has("log.cardDropped Counter-Strike 2|2|1") {
		t.Fatalf("drop not logged: %v", h.logs)
	}

	// connection drop: reconnect with the saved token and resume
	f.Drop()
	wait(t, "reconnecting", func() bool { return b.get(func(b *Bot) bool { return b.status != "online" }) })
	for i := 0; i < 400 && b.get(func(b *Bot) bool { return b.status != "online" }); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	wait(t, "resumed", func() bool {
		return strings.Contains(f.LastPlayed(), "730") && b.get(func(b *Bot) bool { return b.status == "online" })
	})

	b.StopAll()
	wait(t, "stopped", func() bool { return f.LastPlayed() == "" })
}

func TestFindKeys(t *testing.T) {
	got := FindKeys("Portal 2: abcde-fghij-klmno\nXXXXX-YYYYY-ZZZZZ-AAAAA-BBBBB, again ABCDE-FGHIJ-KLMNO")
	if len(got) != 2 || got[0] != "ABCDE-FGHIJ-KLMNO" || got[1] != "XXXXX-YYYYY-ZZZZZ-AAAAA-BBBBB" {
		t.Fatal(got)
	}
	if maskKey("ABCDE-FGHIJ-KLMNO") != "ABCDE-*****-KLMNO" {
		t.Fatal(maskKey("ABCDE-FGHIJ-KLMNO"))
	}
}

func TestWindow(t *testing.T) {
	at := func(hm string) time.Time { tt, _ := time.Parse("15:04", hm); return tt }
	if !inWindow(at("23:30"), "23:00", "08:00") || !inWindow(at("03:00"), "23:00", "08:00") || inWindow(at("12:00"), "23:00", "08:00") {
		t.Fatal("overnight window")
	}
	if !inWindow(at("10:00"), "09:00", "18:00") || inWindow(at("19:00"), "09:00", "18:00") {
		t.Fatal("day window")
	}
}

func TestGamesFallback(t *testing.T) {
	f := steamtest.Start()
	defer f.Close()
	f.FailOwned = true
	h := &host{secrets: map[string]string{"token:gleb": steamtest.Token(steamtest.SteamID)}}
	b := New("gleb", t.TempDir(), "gleb", h)
	defer b.Shutdown()
	b.LoginSaved()
	wait(t, "web fallback games", func() bool { return len(b.Games()) == 2 })
	if g := b.Games(); g[0].Name != "Portal 2" || !strings.HasSuffix(g[0].Icon, "/620/p2.jpg") {
		t.Fatalf("games: %+v", g)
	}
}
