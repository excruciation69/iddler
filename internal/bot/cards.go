package bot

// Card farming: read the badges page and idle games that still have drops.

import (
	"errors"
	"slices"
	"sort"
	"time"

	"iddler/internal/steam"
)

type DropRecord struct {
	At    int64  `json:"at"`
	AppID uint32 `json:"appid"`
	Game  string `json:"game"`
	Count int    `json:"count"`
}

// webSession returns a logged-in web client, creating a fresh access token when needed.
func (b *Bot) webSession() (*steam.Web, error) {
	b.mu.Lock()
	if b.web != nil && time.Since(b.webAt) < 12*time.Hour {
		w := b.web
		b.mu.Unlock()
		return w, nil
	}
	cm, token, sid := b.cm, b.token, b.steamID
	online := b.status == "online"
	b.mu.Unlock()
	if !online || cm == nil {
		return nil, errors.New("offline")
	}
	at, err := cm.GenerateAccessToken(token, sid)
	if err != nil {
		return nil, err
	}
	w := steam.NewWeb(sid, at)
	b.mu.Lock()
	b.web, b.webAt = w, time.Now()
	b.mu.Unlock()
	return w, nil
}

// withWeb runs fn with a web session and retries once if the session expired.
func (b *Bot) withWeb(fn func(*steam.Web) error) error {
	for attempt := 0; attempt < 2; attempt++ {
		w, err := b.webSession()
		if err != nil {
			return err
		}
		err = fn(w)
		if !errors.Is(err, steam.ErrWebAuth) {
			return err
		}
		b.mu.Lock()
		b.web = nil
		b.mu.Unlock()
	}
	return steam.ErrWebAuth
}

func (b *Bot) refreshCards() {
	var badges []steam.BadgeGame
	err := b.withWeb(func(w *steam.Web) (err error) {
		badges, err = w.Badges()
		return err
	})

	b.mu.Lock()
	defer b.mu.Unlock()
	b.cardsBusy = false
	b.cardsAt = time.Now()
	if err != nil {
		b.log("warn", "log.cardsFailed", errKey(err))
		b.cardsAt = time.Now().Add(-10 * time.Minute) // retry in ~5 minutes
		return
	}
	old := map[uint32]int{}
	for _, c := range b.cards {
		old[c.AppID] = c.DropsLeft
	}
	var withDrops, skipped []steam.BadgeGame
	for _, g := range badges {
		if prev, ok := old[g.AppID]; ok && g.DropsLeft < prev {
			n := prev - g.DropsLeft
			b.data.Drops = append(b.data.Drops, DropRecord{time.Now().Unix(), g.AppID, g.Name, n})
			b.log("ok", "log.cardDropped", g.Name, itoa(n), itoa(g.DropsLeft))
			b.host.Notify("cards", "notify.card", b.displayName(), g.Name, itoa(g.DropsLeft))
		}
		if g.DropsLeft > 0 {
			if b.farmAllowed(g.AppID) {
				withDrops = append(withDrops, g)
			} else {
				skipped = append(skipped, g)
			}
		}
	}
	if len(b.data.Drops) > 100 {
		b.data.Drops = b.data.Drops[len(b.data.Drops)-100:]
	}
	// most played first: drops come faster on games past the 2 hour mark
	sort.SliceStable(withDrops, func(i, j int) bool { return withDrops[i].Hours > withDrops[j].Hours })
	b.cards, b.cardsSkipped = withDrops, skipped
	b.rememberCraftable(badges)
	if len(withDrops) == 0 && b.cfg.CardsOn {
		b.cfg.CardsOn = false
		b.saveConfig()
		b.log("ok", "log.cardsDone")
		b.host.Notify("cards", "notify.cardsDone", b.displayName())
		if b.cfg.Cards.ThenHours && len(b.cfg.Hours.Selected) > 0 && !b.cfg.HoursOn {
			b.cfg.HoursOn = true // the next task in the chain
			b.saveConfig()
			b.log("info", "log.thenHours")
		}
	}
	b.applyPlaying()
	b.changed()
}

// refilterCards re-sorts the known games after the black or white list changed, without a new
// badge check, so the queue reacts at once. Caller holds b.mu.
func (b *Bot) refilterCards() {
	all := append(append([]steam.BadgeGame(nil), b.cards...), b.cardsSkipped...)
	b.cards, b.cardsSkipped = nil, nil
	for _, g := range all {
		if b.farmAllowed(g.AppID) {
			b.cards = append(b.cards, g)
		} else {
			b.cardsSkipped = append(b.cardsSkipped, g)
		}
	}
	sort.SliceStable(b.cards, func(i, j int) bool { return b.cards[i].Hours > b.cards[j].Hours })
	b.applyPlaying()
}

// farmAllowed applies the black and white lists. Caller holds b.mu.
func (b *Bot) farmAllowed(app uint32) bool {
	if slices.Contains(b.cfg.Cards.Blacklist, app) {
		return false
	}
	return len(b.cfg.Cards.Whitelist) == 0 || slices.Contains(b.cfg.Cards.Whitelist, app)
}

// cardsToIdle picks the apps for card farming.
func (b *Bot) cardsToIdle(slots int) []uint32 {
	if len(b.cards) == 0 {
		return nil
	}
	var ids []uint32
	if b.cfg.Cards.TwoHour {
		// new games give drops only after ~2 hours: idle all of them together first
		for _, c := range b.cards {
			if c.Hours+b.accrued[c.AppID]/3600 < 2 {
				ids = append(ids, c.AppID)
			}
		}
		if len(ids) > 0 {
			return ids[:min(len(ids), slots)]
		}
	}
	if b.cfg.Cards.Mode == "many" {
		for _, c := range b.cards {
			ids = append(ids, c.AppID)
		}
		return ids[:min(len(ids), slots)]
	}
	return []uint32{b.cards[0].AppID}
}

func (b *Bot) cardsLeft() int {
	n := 0
	for _, c := range b.cards {
		n += c.DropsLeft
	}
	return n
}

// rememberCraftable keeps badges that are ready to craft (from the same page). Caller holds b.mu.
func (b *Bot) rememberCraftable(badges []steam.BadgeGame) {
	b.craftable = b.craftable[:0]
	for _, g := range badges {
		if g.CanCraft || g.CanCraftFoil {
			b.craftable = append(b.craftable, g)
		}
	}
}
