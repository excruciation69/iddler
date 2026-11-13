package bot

// What the account reports as "playing": hours, goals and card farming share Steam's 32 slots.

import (
	"math/rand"
	"sort"
	"strings"
	"time"

	"iddler/internal/steam"
)

const maxSlots = 32

func (b *Bot) LoadGames() {
	cm := b.conn()
	if cm == nil {
		return
	}
	b.mu.Lock()
	b.gamesLoading = true
	sid := b.steamID
	b.changed()
	b.mu.Unlock()

	games, err := cm.OwnedGames(sid)
	if err != nil || len(games) == 0 {
		// fall back to the Web API (works even when the CM call is refused)
		var webGames []steam.OwnedGame
		werr := b.withWeb(func(w *steam.Web) (e error) { webGames, e = w.OwnedGames(); return e })
		if werr == nil && len(webGames) > 0 {
			games, err = webGames, nil
		} else if err == nil && werr != nil {
			err = werr
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.gamesLoading = false
	if err != nil {
		b.log("warn", "log.gamesFailed", errKey(err))
	} else {
		b.accrued = map[uint32]float64{}
		b.log("ok", "log.gamesLoaded", itoa(len(games)))
	}
	seen := map[uint32]bool{}
	for _, g := range games {
		seen[g.AppID] = true
	}
	for _, m := range b.cfg.Hours.Manual {
		if !seen[m.AppID] {
			m.Manual = true
			games = append(games, m)
		}
	}
	sort.Slice(games, func(i, j int) bool { return strings.ToLower(games[i].Name) < strings.ToLower(games[j].Name) })
	b.games = games
	b.host.GamesChanged(b.ID)
	b.changed()
}

func (b *Bot) game(id uint32) *steam.OwnedGame {
	for i := range b.games {
		if b.games[i].AppID == id {
			return &b.games[i]
		}
	}
	return nil
}

// hoursOf is the current playtime of an app, including what was idled since the list was loaded.
func (b *Bot) hoursOf(id uint32) float64 {
	h := b.accrued[id] / 3600
	if g := b.game(id); g != nil {
		h += float64(g.Minutes) / 60
	}
	return h
}

func (b *Bot) goalReached(id uint32) bool {
	goal := b.cfg.Hours.Goals[id]
	return goal > 0 && b.hoursOf(id) >= goal
}

func inWindow(now time.Time, from, to string) bool {
	parse := func(s string) int {
		t, err := time.Parse("15:04", s)
		if err != nil {
			return -1
		}
		return t.Hour()*60 + t.Minute()
	}
	f, t := parse(from), parse(to)
	if f < 0 || t < 0 || f == t {
		return true
	}
	cur := now.Hour()*60 + now.Minute()
	if f < t {
		return cur >= f && cur < t
	}
	return cur >= f || cur < t // crosses midnight
}

func (b *Bot) scheduleOpen(now time.Time) bool {
	s := b.cfg.Hours.Schedule
	return !s.On || inWindow(now, s.From, s.To)
}

func (b *Bot) customName() string {
	p := b.cfg.Profile
	if !b.cfg.Modules[ModProfile] || !p.SlideOn {
		return ""
	}
	var names []string
	for _, n := range p.Names {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return truncate(names[b.slideIdx%len(names)], 64)
}

// wanted computes the app list for Steam, in priority order.
func (b *Bot) wanted() []uint32 {
	slots := maxSlots
	if b.customNow != "" {
		slots--
	}
	var ids []uint32
	has := map[uint32]bool{}
	add := func(id uint32) {
		if len(ids) < slots && !has[id] {
			has[id] = true
			ids = append(ids, id)
		}
	}
	// the game whose achievements are opening runs too, when asked (looks natural in the profile)
	if j := b.achJob; j != nil && b.cfg.Ach.IdleWhile {
		add(j.AppID)
	}
	for _, p := range b.cfg.Priority {
		switch p {
		case ModCards:
			if b.cfg.CardsOn && b.cfg.Modules[ModCards] {
				for _, id := range b.cardsToIdle(slots) {
					add(id)
				}
			}
		case "goals":
			if b.cfg.HoursOn && b.cfg.Modules[ModHours] {
				for _, id := range b.cfg.Hours.Selected {
					if b.cfg.Hours.Goals[id] > 0 && !b.goalReached(id) {
						add(id)
					}
				}
			}
		case ModHours:
			if b.cfg.HoursOn && b.cfg.Modules[ModHours] {
				for _, id := range b.cfg.Hours.Selected {
					if b.cfg.Hours.Goals[id] <= 0 {
						add(id)
					}
				}
			}
		}
	}
	return ids
}

// applyPlaying tells Steam what we play now. Caller holds b.mu.
func (b *Bot) applyPlaying() {
	if b.cm == nil || b.status != "online" {
		return
	}
	now := time.Now()
	b.customNow = b.customName()
	var ids []uint32
	if !b.blocked && b.scheduleOpen(now) {
		ids = b.wanted()
	}
	custom := b.customNow
	if len(ids) == 0 {
		custom = ""
	}
	if b.sent && equalIDs(ids, b.playing) && custom == b.sentCustom {
		return
	}
	if err := b.cm.GamesPlayed(custom, ids); err != nil {
		b.log("err", "log.error", err.Error())
		return
	}
	b.playing, b.sentCustom, b.sent = ids, custom, true
	if len(ids) > 0 && b.sessStart.IsZero() {
		b.sessStart = now
		b.lastTick = now
	}
	if len(ids) == 0 {
		b.pauseSession()
	}
	b.changed()
}

func (b *Bot) pauseSession() {
	if !b.sessStart.IsZero() {
		b.account(time.Now())
		b.sessDur += time.Since(b.sessStart)
		b.sessStart = time.Time{}
	}
}

func (b *Bot) sessionMs() int64 {
	d := b.sessDur
	if !b.sessStart.IsZero() {
		d += time.Since(b.sessStart)
	}
	return d.Milliseconds()
}

// account adds idled time to the history since the last tick.
func (b *Bot) account(now time.Time) {
	if b.sessStart.IsZero() || b.lastTick.IsZero() {
		b.lastTick = now
		return
	}
	sec := now.Sub(b.lastTick).Seconds()
	b.lastTick = now
	if sec <= 0 || sec > 600 {
		return // the Mac slept: Steam didn't count that time either
	}
	day := now.Format("2006-01-02")
	h := b.data.History[day]
	if h == nil {
		h = map[uint32]int{}
		b.data.History[day] = h
	}
	for _, id := range b.playing {
		b.accrued[id] += sec
		h[id] += int(sec + 0.5)
	}
}

func (b *Bot) onPlayingState(blocked bool, app uint32) {
	was := b.blocked
	b.blocked, b.blockedApp = blocked, 0
	if blocked {
		b.blockedApp = app
	}
	switch {
	case blocked && !was:
		if b.cfg.OnPlay == "kick" && (b.cfg.HoursOn || b.cfg.CardsOn) {
			b.cm.KickPlayingSession()
			b.log("warn", "log.kicked", itoa(int(app)))
			return
		}
		b.playing = nil
		b.pauseSession()
		b.log("warn", "log.paused", itoa(int(app)))
		b.host.Notify("pause", "notify.paused", b.displayName())
	case !blocked && was:
		b.log("ok", "log.resumed")
		gen := b.gen
		time.AfterFunc(5*time.Second, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if gen == b.gen {
				b.applyPlaying()
			}
		})
	}
	b.changed()
}

func (b *Bot) displayName() string {
	if b.name != "" {
		return b.name
	}
	return b.cfg.Account
}

// ---------------------------------------------------------------- background loop

func (b *Bot) worker() {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
			b.tick()
		}
	}
}

func (b *Bot) tick() {
	now := time.Now()
	b.mu.Lock()
	online := b.status == "online" && b.cm != nil
	if online {
		b.account(now)
		// goals reached
		for _, id := range b.cfg.Hours.Selected {
			if g := b.cfg.Hours.Goals[id]; b.cfg.HoursOn && g > 0 && b.goalReached(id) && b.data.Notified[id] != -1 {
				b.data.Notified[id] = -1
				name := b.gameName(id)
				b.log("ok", "log.goalReached", name, ftoa(g))
				b.host.Notify("goals", "notify.goal", b.displayName(), name, ftoa(g))
			}
		}
		// slideshow
		if p := b.cfg.Profile; b.cfg.Modules[ModProfile] && p.SlideOn && len(p.Names) > 1 {
			every := time.Duration(max(p.Interval, 1)) * time.Minute
			if now.Sub(b.slideAt) >= every {
				b.slideAt = now
				if p.Shuffle {
					b.slideIdx = rand.Intn(len(p.Names))
				} else {
					b.slideIdx++
				}
			}
		}
		// nickname schedule
		if b.cfg.Modules[ModProfile] && b.cfg.Profile.NickOn && b.desiredNick(now) != b.nickSet {
			b.applyPersona()
		}
		b.applyPlaying()
		if len(b.playing) > 0 {
			b.changed()
		}
	}
	due := b.dueTasks(now)
	b.mu.Unlock()
	if online {
		for _, task := range due {
			task()
		}
	}
	b.mu.Lock()
	if now.Minute()%5 == 0 || len(due) > 0 {
		b.pruneHistory(now)
		b.saveData()
	}
	b.mu.Unlock()
}

// dueTasks returns network jobs that are due; they run without the lock held.
func (b *Bot) dueTasks(now time.Time) []func() {
	var tasks []func()
	if b.cfg.CardsOn && b.cfg.Modules[ModCards] && !b.cardsBusy && now.Sub(b.cardsAt) > 15*time.Minute {
		b.cardsBusy = true
		tasks = append(tasks, b.refreshCards)
	}
	if b.cfg.Modules[ModTrade] && (b.cfg.Trade.Craft || b.cfg.Trade.Gifts) && now.Sub(b.lastTrade) > 30*time.Minute {
		b.lastTrade = now
		tasks = append(tasks, b.tradeTasks)
	}
	if (b.cfg.Free.Promos || b.cfg.Free.Queue || b.cfg.Free.Sticker) && now.Sub(b.lastFree) > 30*time.Minute {
		b.lastFree = now
		tasks = append(tasks, b.freeTasks)
	}
	if b.cfg.Free.WishlistOn && b.cfg.Free.WishlistPct > 0 && now.Sub(b.lastWish) > 6*time.Hour {
		b.lastWish = now
		tasks = append(tasks, b.wishlistTask)
	}
	w := b.cfg.Ach.Window
	if b.achJob != nil && !b.achJob.busy && now.After(b.achJob.next) && (!w.On || inWindow(now, w.From, w.To)) {
		b.achJob.busy = true
		tasks = append(tasks, b.achStep)
	}
	return tasks
}

func (b *Bot) pruneHistory(now time.Time) {
	cut := now.AddDate(0, 0, -400).Format("2006-01-02")
	for d := range b.data.History {
		if d < cut {
			delete(b.data.History, d)
		}
	}
}

func (b *Bot) gameName(id uint32) string {
	if g := b.game(id); g != nil {
		return g.Name
	}
	for _, c := range b.cards {
		if c.AppID == id && c.Name != "" {
			return c.Name
		}
	}
	return "App " + itoa(int(id))
}

func equalIDs(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
