package bot

// Inventory manager: the inventory grouped into stacks, live prices, selling (one, many or all
// duplicates), active listings, market search and mobile confirmations from a .maFile.

import (
	"errors"
	"sort"
	"strings"
	"time"

	"steamidler/internal/steam"
)

// ItemGroup is every copy of one item: the interface shows and sells stacks, not single assets.
type ItemGroup struct {
	Key        string   `json:"key"`
	AppID      uint32   `json:"appid"`
	ContextID  string   `json:"contextid"`
	Name       string   `json:"name"`
	HashName   string   `json:"hashName"`
	Type       string   `json:"type"`
	Game       string   `json:"game"`
	Kind       string   `json:"kind"`
	Icon       string   `json:"icon"`
	Color      string   `json:"color"`
	Marketable bool     `json:"marketable"`
	Tradable   bool     `json:"tradable"`
	Count      int      `json:"count"`
	Assets     []string `json:"assets"`
}

// marketJob is the progress of a batch (selling or removing listings) shown live in the interface.
type marketJob struct {
	Kind    string `json:"kind"` // sell, remove
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Failed  int    `json:"failed"`
	Pending int    `json:"pending"` // listed, waiting for a confirmation
	Error   string `json:"error"`
	Running bool   `json:"running"`
	At      int64  `json:"at"`
}

type priceEntry struct {
	p  steam.Price
	at time.Time
}

// marketDelay keeps batches polite towards the market's rate limits (tests set it to zero).
var marketDelay = 1200 * time.Millisecond

func groupItems(items []steam.Item) []ItemGroup {
	idx := map[string]int{}
	var out []ItemGroup
	for _, it := range items {
		key := it.AssetID
		if it.Marketable {
			key = it.HashName
		}
		key = it.ContextID + "|" + key
		if i, ok := idx[key]; ok {
			out[i].Count += it.Amount
			out[i].Assets = append(out[i].Assets, it.AssetID)
			continue
		}
		idx[key] = len(out)
		out = append(out, ItemGroup{Key: key, AppID: it.AppID, ContextID: it.ContextID, Name: it.Name, HashName: it.HashName,
			Type: it.Type, Game: it.Game, Kind: it.Kind, Icon: it.Icon, Color: it.Color, Marketable: it.Marketable,
			Tradable: it.Tradable, Count: it.Amount, Assets: []string{it.AssetID}})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Marketable != out[j].Marketable {
			return out[i].Marketable
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Inventory returns the inventory of one game (Steam items: 753/6), cached for a few minutes.
func (b *Bot) Inventory(appID uint32, contextID string, refresh bool) ([]ItemGroup, error) {
	if appID == 0 {
		appID, contextID = 753, "6"
	}
	key := contextKey(appID, contextID)
	b.mu.Lock()
	if !refresh && b.invKey == key && time.Since(b.invAt) < 5*time.Minute {
		out := b.invList
		b.mu.Unlock()
		return out, nil
	}
	b.mu.Unlock()
	var items []steam.Item
	err := b.withWeb(func(w *steam.Web) (err error) { items, err = w.Inventory(appID, contextID); return err })
	if err != nil {
		return nil, errors.New(errKey(err))
	}
	groups := groupItems(items)
	b.mu.Lock()
	b.invList, b.invKey, b.invAt = groups, key, time.Now()
	b.mu.Unlock()
	return groups, nil
}

func contextKey(appID uint32, contextID string) string { return itoa(int(appID)) + "/" + contextID }

// ItemPrice returns market prices for an item; withBook also reads the live order book.
func (b *Bot) ItemPrice(appID uint32, hashName string, withBook bool) (steam.Price, error) {
	b.mu.Lock()
	cur := max(b.currency, 1)
	key := itoa(cur) + "|" + itoa(int(appID)) + "|" + hashName
	if e, ok := b.prices[key]; ok && time.Since(e.at) < 10*time.Minute && (!withBook || e.p.HighestBuy+e.p.LowestSell > 0) {
		b.mu.Unlock()
		return e.p, nil
	}
	b.mu.Unlock()
	var p steam.Price
	err := b.withWeb(func(w *steam.Web) (err error) {
		p, err = w.PriceOverview(appID, hashName, cur)
		if withBook {
			w.OrderBook(appID, hashName, cur, &p) // the overview alone is still useful if this fails
		}
		if err != nil && p.LowestSell > 0 {
			err = nil
		}
		return err
	})
	if err != nil {
		return p, errors.New(errKey(err))
	}
	b.mu.Lock()
	if b.prices == nil {
		b.prices = map[string]priceEntry{}
	}
	b.prices[key] = priceEntry{p, time.Now()}
	b.mu.Unlock()
	return p, nil
}

// SellOrder lists some copies of one item at a price the buyer pays (cents per copy).
type SellOrder struct {
	AppID     uint32   `json:"appid"`
	ContextID string   `json:"contextid"`
	Assets    []string `json:"assets"`
	Pays      int      `json:"pays"`
}

// Sell starts listing the given items in the background; progress shows in the account's detail.
func (b *Bot) Sell(orders []SellOrder) error {
	total := 0
	for _, o := range orders {
		if o.Pays < 3 {
			return errors.New("err.priceTooLow")
		}
		total += len(o.Assets)
	}
	if total == 0 {
		return errors.New("err.nothingToSell")
	}
	if !b.startJob("sell", total) {
		return errors.New("err.marketBusy")
	}
	go func() {
		sold := map[string]bool{}
		for _, o := range orders {
			for _, asset := range o.Assets {
				if b.jobStopped() {
					return
				}
				var res steam.SellResult
				err := b.withWeb(func(w *steam.Web) (err error) {
					res, err = w.Sell(o.AppID, o.ContextID, asset, 1, steam.SellerReceives(o.Pays))
					return err
				})
				b.mu.Lock()
				if err != nil {
					b.mjob.Failed++
					b.mjob.Error = errKey(err)
				} else {
					b.mjob.Done++
					sold[asset] = true
					if res.NeedsConfirmation {
						b.mjob.Pending++
					}
				}
				b.changed()
				b.mu.Unlock()
				time.Sleep(marketDelay)
			}
		}
		b.dropSold(sold)
		b.finishJob()
		if b.CanConfirm() && b.cfgAutoConfirm() {
			b.ConfirmMarket()
		}
	}()
	return nil
}

// SellDuplicates lists every extra copy (keeps one of each) at an automatic price.
//
// mode picks the base price: "lowest" (cheapest listing), "median" (typical sale), "buy" (best buy
// order, sells fastest). undercut is taken off that price, min and max clamp the result (cents, 0 = no limit).
func (b *Bot) SellDuplicates(appID uint32, contextID, mode string, undercut, minPays, maxPays int, kinds []string) (int, error) {
	groups, err := b.Inventory(appID, contextID, false)
	if err != nil {
		return 0, err
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var orders []SellOrder
	for _, g := range groups {
		if !g.Marketable || len(g.Assets) < 2 || (len(want) > 0 && !want[g.Kind]) {
			continue
		}
		p, err := b.ItemPrice(g.AppID, g.HashName, mode == "buy")
		if err != nil {
			continue
		}
		base := map[string]int{"lowest": p.Lowest, "median": p.Median, "buy": p.HighestBuy}[mode]
		if base == 0 {
			base = max(p.Lowest, p.Median)
		}
		pays := base - undercut
		if minPays > 0 {
			pays = max(pays, minPays)
		}
		if maxPays > 0 {
			pays = min(pays, maxPays)
		}
		if pays < 3 {
			continue
		}
		orders = append(orders, SellOrder{AppID: g.AppID, ContextID: g.ContextID, Assets: g.Assets[1:], Pays: pays})
		time.Sleep(marketDelay / 3)
	}
	n := 0
	for _, o := range orders {
		n += len(o.Assets)
	}
	if n == 0 {
		return 0, nil
	}
	return n, b.Sell(orders)
}

// Listings returns your active market listings.
func (b *Bot) Listings() ([]steam.Listing, error) {
	var out []steam.Listing
	err := b.withWeb(func(w *steam.Web) (err error) { out, err = w.Listings(); return err })
	if err != nil {
		return nil, errors.New(errKey(err))
	}
	return out, nil
}

// RemoveListings cancels listings in the background (ids empty: all of them).
func (b *Bot) RemoveListings(ids []string) error {
	if len(ids) == 0 {
		all, err := b.Listings()
		if err != nil {
			return err
		}
		for _, l := range all {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) == 0 {
		return errors.New("err.noListings")
	}
	if !b.startJob("remove", len(ids)) {
		return errors.New("err.marketBusy")
	}
	go func() {
		for _, id := range ids {
			if b.jobStopped() {
				return
			}
			err := b.withWeb(func(w *steam.Web) error { return w.RemoveListing(id) })
			b.mu.Lock()
			if err != nil {
				b.mjob.Failed++
				b.mjob.Error = errKey(err)
			} else {
				b.mjob.Done++
			}
			b.changed()
			b.mu.Unlock()
			time.Sleep(marketDelay / 2)
		}
		b.mu.Lock()
		b.invAt = time.Time{} // items came back: reload the inventory next time
		b.mu.Unlock()
		b.finishJob()
	}()
	return nil
}

// MarketSearch searches the Community Market.
func (b *Bot) MarketSearch(query string, appID uint32, start int) ([]steam.MarketResult, int, error) {
	var out []steam.MarketResult
	var total int
	err := b.withWeb(func(w *steam.Web) (err error) { out, total, err = w.MarketSearch(query, appID, start); return err })
	if err != nil {
		return nil, 0, errors.New(errKey(err))
	}
	return out, total, nil
}

// CanConfirm reports whether a .maFile with an identity_secret is loaded.
func (b *Bot) CanConfirm() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.identity != "" && b.deviceID != ""
}

func (b *Bot) cfgAutoConfirm() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Market.AutoConfirm
}

// ConfirmMarket accepts every pending market-listing confirmation and returns how many.
func (b *Bot) ConfirmMarket() (int, error) {
	b.mu.Lock()
	identity, device := b.identity, b.deviceID
	b.mu.Unlock()
	if identity == "" || device == "" {
		return 0, errors.New("err.noIdentity")
	}
	n := 0
	err := b.withWeb(func(w *steam.Web) error {
		list, err := w.Confirmations(identity, device)
		if err != nil {
			return err
		}
		var market []steam.Confirmation
		for _, c := range list {
			if c.Type == 3 {
				market = append(market, c)
			}
		}
		n = len(market)
		return w.RespondConfirmations(identity, device, market, true)
	})
	if err != nil {
		return 0, errors.New(errKey(err))
	}
	b.mu.Lock()
	if b.mjob != nil {
		b.mjob.Pending = max(0, b.mjob.Pending-n)
		b.changed()
	}
	b.mu.Unlock()
	return n, nil
}

// StopMarketJob stops a running batch after the current item.
func (b *Bot) StopMarketJob() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mjob != nil && b.mjob.Running {
		b.mjob.Running = false
		b.changed()
	}
}

func (b *Bot) startJob(kind string, total int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mjob != nil && b.mjob.Running {
		return false
	}
	b.mjob = &marketJob{Kind: kind, Total: total, Running: true, At: time.Now().Unix()}
	b.changed()
	return true
}

func (b *Bot) jobStopped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mjob == nil || !b.mjob.Running || b.status != "online"
}

func (b *Bot) finishJob() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mjob != nil {
		b.mjob.Running = false
		b.mjob.At = time.Now().Unix()
		b.log("ok", "log.marketDone", itoa(b.mjob.Done), itoa(b.mjob.Total))
		b.changed()
	}
}

// dropSold removes listed assets from the cached inventory so the interface updates at once.
func (b *Bot) dropSold(sold map[string]bool) {
	if len(sold) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.invList[:0:0]
	for _, g := range b.invList {
		var keep []string
		for _, a := range g.Assets {
			if !sold[a] {
				keep = append(keep, a)
			}
		}
		if len(keep) == 0 {
			continue
		}
		g.Assets, g.Count = keep, len(keep)
		out = append(out, g)
	}
	b.invList = out
}
