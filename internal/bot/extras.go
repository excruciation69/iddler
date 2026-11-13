package bot

// Freebies, profile (auto-reply, nickname schedule), trades/badges and statistics.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"steamidler/internal/steam"
)

type ClaimRecord struct {
	At   int64  `json:"at"`
	Name string `json:"name"`
	Kind string `json:"kind"` // promo | sticker | queue
}

type CraftRecord struct {
	At   int64  `json:"at"`
	Game string `json:"game"`
	Foil bool   `json:"foil"`
}

type Deal struct {
	AppID    uint32 `json:"appid"`
	Name     string `json:"name"`
	Discount int    `json:"discount"`
	Price    string `json:"price"`
}

func (b *Bot) claim(name, kind string) {
	b.data.Claimed = append(b.data.Claimed, ClaimRecord{time.Now().Unix(), name, kind})
	if len(b.data.Claimed) > 200 {
		b.data.Claimed = b.data.Claimed[len(b.data.Claimed)-200:]
	}
}

// ---------------------------------------------------------------- freebies

func (b *Bot) freeTasks() {
	b.mu.Lock()
	cfg := b.cfg.Free
	today := time.Now().Format("2006-01-02")
	salesDone := b.data.SalesDay == today
	cm := b.cm
	b.mu.Unlock()
	if cm == nil {
		return
	}

	if cfg.Promos {
		var promos []steam.FreePromo
		err := b.withWeb(func(w *steam.Web) (err error) {
			promos, err = w.FreePromos()
			return err
		})
		if err != nil {
			b.log("warn", "log.freeFailed", errKey(err))
		}
		for _, p := range promos {
			key := fmt.Sprintf("%d/%d", p.AppID, p.PackageID)
			b.mu.Lock()
			done := b.data.Promos[key]
			b.mu.Unlock()
			if done {
				continue
			}
			if p.PackageID != 0 {
				err = b.withWeb(func(w *steam.Web) error { return w.AddFreeLicense(p.PackageID) })
			} else {
				_, err = cm.RequestFreeLicense(p.AppID)
			}
			b.mu.Lock()
			b.data.Promos[key] = true
			if err == nil {
				b.claim(p.Name, "promo")
				b.log("ok", "log.freeGame", p.Name)
				b.host.Notify("free", "notify.free", b.displayName(), p.Name)
			}
			b.mu.Unlock()
			time.Sleep(3 * time.Second)
		}
	}

	if !salesDone && (cfg.Queue || cfg.Sticker) {
		if cfg.Queue {
			if ids, err := cm.DiscoveryQueue(); err == nil && len(ids) > 0 {
				for _, id := range ids {
					cm.SkipDiscoveryItem(id)
					time.Sleep(time.Second)
				}
				b.mu.Lock()
				b.claim(itoa(len(ids)), "queue")
				b.log("ok", "log.queueDone", itoa(len(ids)))
				b.mu.Unlock()
			}
		}
		if cfg.Sticker {
			var got bool
			b.withWeb(func(w *steam.Web) (err error) {
				got, err = w.ClaimSaleSticker()
				return err
			})
			if got {
				b.mu.Lock()
				b.claim("", "sticker")
				b.log("ok", "log.sticker")
				b.mu.Unlock()
			}
		}
		b.mu.Lock()
		b.data.SalesDay = today
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.changed()
	b.mu.Unlock()
}

func (b *Bot) wishlistTask() {
	var deals []steam.StorePrice
	err := b.withWeb(func(w *steam.Web) error {
		ids, err := w.Wishlist()
		if err != nil {
			return err
		}
		for i := 0; i < len(ids); i += 100 {
			p, err := w.Prices(ids[i:min(i+100, len(ids))])
			if err != nil {
				return err
			}
			deals = append(deals, p...)
			time.Sleep(2 * time.Second)
		}
		return nil
	})
	if err != nil {
		b.log("warn", "log.wishFailed", errKey(err))
		return
	}
	// names of discounted games (wishlisted games usually aren't in the library)
	names := map[uint32]string{}
	for i, d := range deals {
		if d.Discount > 0 && i < 40 {
			b.withWeb(func(w *steam.Web) error { names[d.AppID] = w.AppName(d.AppID); return nil })
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	threshold := b.cfg.Free.WishlistPct
	b.data.Deals = nil
	for _, d := range deals {
		if d.Discount <= 0 {
			delete(b.data.Notified, d.AppID)
			continue
		}
		name := names[d.AppID]
		if name == "" {
			name = b.gameName(d.AppID)
		}
		b.data.Deals = append(b.data.Deals, Deal{d.AppID, name, d.Discount, d.Final})
		if d.Discount >= threshold && d.Discount > b.data.Notified[d.AppID] {
			b.data.Notified[d.AppID] = d.Discount
			b.log("ok", "log.wishDeal", name, itoa(d.Discount), d.Final)
			b.host.Notify("free", "notify.deal", name, itoa(d.Discount), d.Final)
		}
	}
	sort.Slice(b.data.Deals, func(i, j int) bool { return b.data.Deals[i].Discount > b.data.Deals[j].Discount })
	b.changed()
}

// ---------------------------------------------------------------- profile

func (b *Bot) desiredNick(now time.Time) string {
	p := b.cfg.Profile
	if inWindow(now, p.NickFrom, p.NickTo) {
		return strings.TrimSpace(p.NickNight)
	}
	return strings.TrimSpace(p.NickDay)
}

// onChat answers private messages while the account is idling. Caller holds b.mu.
func (b *Bot) onChat(from uint64, entryType int, text string) {
	if entryType != 1 || text == "" {
		return
	}
	name := b.friends[from]
	if name == "" {
		name = fmt.Sprint(from)
	}
	b.host.Notify("chat", "notify.chat", b.displayName(), name, truncate(text, 120))
	p := b.cfg.Profile
	if !b.cfg.Modules[ModProfile] || !p.AutoOn || len(b.playing) == 0 || strings.TrimSpace(p.AutoText) == "" {
		return
	}
	for _, ex := range p.AutoExcept {
		if ex == fmt.Sprint(from) {
			return
		}
	}
	if time.Since(b.autoSent[from]) < time.Duration(max(p.AutoCooldown, 1))*time.Minute {
		return
	}
	b.autoSent[from] = time.Now()
	cm, reply := b.cm, p.AutoText
	go cm.SendChat(from, reply)
	b.log("info", "log.autoReplied", name)
}

// ---------------------------------------------------------------- trades and badges

func (b *Bot) tradeTasks() {
	b.mu.Lock()
	cfg := b.cfg.Trade
	friends := map[uint64]bool{}
	for id := range b.friends {
		friends[id] = true
	}
	craftable := append([]steam.BadgeGame(nil), b.craftable...)
	fresh := time.Since(b.cardsAt) < 20*time.Minute && !b.cardsAt.IsZero()
	b.mu.Unlock()

	if cfg.Craft {
		if !fresh {
			b.withWeb(func(w *steam.Web) error {
				badges, err := w.Badges()
				if err == nil {
					b.mu.Lock()
					b.rememberCraftable(badges)
					craftable = append([]steam.BadgeGame(nil), b.craftable...)
					b.mu.Unlock()
				}
				return err
			})
		}
		except := map[uint32]bool{}
		for _, id := range cfg.Except {
			except[id] = true
		}
		for _, g := range craftable {
			if except[g.AppID] {
				continue
			}
			for _, foil := range []bool{false, true} {
				if (foil && (!cfg.Foil || !g.CanCraftFoil)) || (!foil && !g.CanCraft) {
					continue
				}
				err := b.withWeb(func(w *steam.Web) error { return w.CraftBadge(g.AppID, foil) })
				b.mu.Lock()
				if err == nil {
					b.data.Crafted = append(b.data.Crafted, CraftRecord{time.Now().Unix(), g.Name, foil})
					b.log("ok", "log.crafted", g.Name)
				} else {
					b.log("warn", "log.craftFailed", g.Name, err.Error())
				}
				b.mu.Unlock()
				time.Sleep(3 * time.Second)
			}
		}
		b.mu.Lock()
		b.craftable = nil
		b.mu.Unlock()
	}

	if cfg.Gifts {
		var offers []steam.TradeOffer
		b.withWeb(func(w *steam.Web) (err error) {
			offers, err = w.IncomingOffers()
			return err
		})
		for _, o := range offers {
			// only offers where nothing leaves the account
			if o.Give != 0 || o.Receive == 0 || (cfg.FriendsOnly && !friends[o.Partner]) {
				continue
			}
			err := b.withWeb(func(w *steam.Web) error { return w.AcceptOffer(o) })
			b.mu.Lock()
			if err == nil {
				b.log("ok", "log.giftAccepted", itoa(o.Receive))
			}
			b.mu.Unlock()
		}
	}
	b.mu.Lock()
	b.changed()
	b.mu.Unlock()
}

// ---------------------------------------------------------------- statistics

type invState struct {
	Busy     bool    `json:"busy"`
	Value    float64 `json:"value"`
	Items    int     `json:"items"`
	Priced   int     `json:"priced"`
	Total    int     `json:"total"`
	Currency int     `json:"currency"`
	At       int64   `json:"at"`
	Error    string  `json:"error"`
}

// RefreshStats loads the level and starts pricing the inventory in the background.
func (b *Bot) RefreshStats() {
	go func() {
		var lvl steam.Level
		if err := b.withWeb(func(w *steam.Web) (err error) { lvl, err = w.Level(); return err }); err == nil {
			b.mu.Lock()
			b.level = lvl
			b.changed()
			b.mu.Unlock()
		}
		b.mu.Lock()
		if b.inv.Busy {
			b.mu.Unlock()
			return
		}
		b.inv.Busy, b.inv.Error = true, ""
		cur := b.currency
		if cur == 0 {
			cur = 1
		}
		b.inv.Currency = cur
		b.mu.Unlock()
		b.priceInventory(cur)
	}()
}

func (b *Bot) priceInventory(currency int) {
	var items []steam.InventoryItem
	err := b.withWeb(func(w *steam.Web) (err error) { items, err = w.CommunityInventory(); return err })
	b.mu.Lock()
	if err != nil {
		b.inv.Busy, b.inv.Error = false, errKey(err)
		b.changed()
		b.mu.Unlock()
		return
	}
	b.inv.Total, b.inv.Priced, b.inv.Items = len(items), 0, 0
	for _, it := range items {
		b.inv.Items += it.Count
	}
	b.mu.Unlock()

	sum := 0.0
	for _, it := range items {
		key := fmt.Sprintf("%d:%s", currency, it.HashName)
		b.mu.Lock()
		cached, ok := b.data.Prices[key]
		b.mu.Unlock()
		price := cached.Value
		if !ok || time.Since(time.Unix(cached.At, 0)) > 24*time.Hour {
			var p float64
			err := b.withWeb(func(w *steam.Web) (err error) { p, err = w.MarketPrice(it.HashName, currency); return err })
			if err == nil {
				price = p
				b.mu.Lock()
				b.data.Prices[key] = Price{p, time.Now().Unix()}
				b.mu.Unlock()
			}
			select { // the market allows ~20 requests a minute
			case <-b.stop:
				return
			case <-time.After(3500 * time.Millisecond):
			}
		}
		sum += price * float64(it.Count)
		b.mu.Lock()
		b.inv.Priced++
		b.inv.Value = sum
		b.changed()
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.inv.Busy, b.inv.At = false, time.Now().Unix()
	b.saveData()
	b.changed()
	b.mu.Unlock()
}

// errKey turns common errors into translatable keys, the rest stay as they are.
func errKey(err error) string {
	switch msg := err.Error(); {
	case errors.Is(err, steam.ErrWebAuth):
		return "err.webAuth"
	case strings.Contains(msg, "did not answer"), strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "err.timeout"
	case strings.Contains(msg, "not connected"), msg == "offline", strings.Contains(msg, "connection closed"):
		return "err.offline"
	case strings.Contains(msg, "rate limited"), strings.Contains(msg, "http 429"):
		return "err.rateLimit"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "unreachable"), strings.Contains(msg, "directory"):
		return "err.network"
	default:
		return msg
	}
}

func itoa(n int) string     { return strconv.Itoa(n) }
func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
