package steam

// Inventory and Community Market: full inventories, prices and order books, selling,
// your own listings, market search and fees.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// EconImage is where item icons live; append the icon hash and a size such as "/96fx96f".
const EconImage = "https://community.cloudflare.steamstatic.com/economy/image/"

// Item is one stack of an inventory as Steam returns it (one asset, maybe with an amount > 1).
type Item struct {
	AssetID    string `json:"assetid"`
	AppID      uint32 `json:"appid"`
	ContextID  string `json:"contextid"`
	Amount     int    `json:"amount"`
	Name       string `json:"name"`
	HashName   string `json:"hashName"`
	Type       string `json:"type"`
	Game       string `json:"game"`
	Kind       string `json:"kind"` // card, foil, background, emoticon, booster, gems, other
	Icon       string `json:"icon"`
	Color      string `json:"color"`
	Marketable bool   `json:"marketable"`
	Tradable   bool   `json:"tradable"`
}

// Inventory reads a whole inventory (Steam items are app 753, context 6), page by page.
func (w *Web) Inventory(appID uint32, contextID string) ([]Item, error) {
	var out []Item
	start := ""
	for page := 0; page < 20; page++ {
		u := fmt.Sprintf("%s/inventory/%d/%d/%s?l=english&count=2000", CommunityURL, w.SteamID, appID, contextID)
		if start != "" {
			u += "&start_assetid=" + start
		}
		body, err := w.get(u)
		if err != nil {
			return out, err
		}
		var r struct {
			Assets []struct {
				AssetID    string `json:"assetid"`
				ClassID    string `json:"classid"`
				InstanceID string `json:"instanceid"`
				Amount     string `json:"amount"`
			} `json:"assets"`
			Descriptions []struct {
				ClassID    string `json:"classid"`
				InstanceID string `json:"instanceid"`
				Name       string `json:"name"`
				HashName   string `json:"market_hash_name"`
				Type       string `json:"type"`
				Icon       string `json:"icon_url"`
				Color      string `json:"name_color"`
				Marketable int    `json:"marketable"`
				Tradable   int    `json:"tradable"`
				Tags       []struct {
					Category string `json:"category"`
					Internal string `json:"internal_name"`
					Name     string `json:"localized_tag_name"`
				} `json:"tags"`
			} `json:"descriptions"`
			More int    `json:"more_items"` // 1 when another page follows
			Last string `json:"last_assetid"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return out, errors.New("inventory is private or unavailable")
		}
		type desc = Item
		descs := make(map[string]desc, len(r.Descriptions))
		for _, d := range r.Descriptions {
			it := desc{Name: d.Name, HashName: d.HashName, Type: d.Type, Color: d.Color,
				Marketable: d.Marketable == 1, Tradable: d.Tradable == 1, Kind: "other"}
			if d.Icon != "" {
				it.Icon = EconImage + d.Icon + "/96fx96f"
			}
			for _, t := range d.Tags {
				switch t.Category {
				case "Game":
					it.Game = t.Name
				case "item_class":
					it.Kind = itemKind(t.Internal)
				case "cardborder":
					if t.Internal == "cardborder_1" && it.Kind == "card" {
						it.Kind = "foil"
					}
				}
			}
			descs[d.ClassID+"_"+d.InstanceID] = it
		}
		for _, a := range r.Assets {
			it, ok := descs[a.ClassID+"_"+a.InstanceID]
			if !ok {
				continue
			}
			it.AssetID, it.AppID, it.ContextID = a.AssetID, appID, contextID
			it.Amount, _ = strconv.Atoi(a.Amount)
			it.Amount = max(it.Amount, 1)
			out = append(out, it)
		}
		if r.More == 0 || r.Last == "" {
			break
		}
		start = r.Last
	}
	return out, nil
}

func itemKind(internal string) string {
	switch internal {
	case "item_class_2":
		return "card"
	case "item_class_3":
		return "background"
	case "item_class_4":
		return "emoticon"
	case "item_class_5":
		return "booster"
	case "item_class_7":
		return "gems"
	}
	return "other"
}

// ---- prices

// Price is everything the market knows about an item, in cents of the wallet currency.
type Price struct {
	Lowest     int `json:"lowest"`     // cheapest listing now (what a buyer pays)
	Median     int `json:"median"`     // median sale price over the last day
	Volume     int `json:"volume"`     // sales in the last day
	HighestBuy int `json:"highestBuy"` // best buy order (0 if unknown)
	LowestSell int `json:"lowestSell"` // best sell order from the order book (0 if unknown)
}

// PriceOverview reads the quick price summary.
func (w *Web) PriceOverview(appID uint32, hashName string, currency int) (Price, error) {
	u := fmt.Sprintf("%s/market/priceoverview/?appid=%d&currency=%d&market_hash_name=%s", CommunityURL, appID, max(currency, 1), url.QueryEscape(hashName))
	body, err := w.get(u)
	if err != nil {
		return Price{}, err
	}
	var r struct {
		Success bool   `json:"success"`
		Lowest  string `json:"lowest_price"`
		Median  string `json:"median_price"`
		Volume  string `json:"volume"`
	}
	if json.Unmarshal(body, &r) != nil || !r.Success {
		return Price{}, errors.New("no price")
	}
	vol, _ := strconv.Atoi(strings.NewReplacer(",", "", ".", "", " ", "").Replace(r.Volume))
	return Price{Lowest: cents(r.Lowest), Median: cents(r.Median), Volume: vol}, nil
}

var reNameID = regexp.MustCompile(`Market_LoadOrderSpread\(\s*(\d+)\s*\)`)

// OrderBook adds the live buy and sell orders to p (two requests: the listing page, then the histogram).
func (w *Web) OrderBook(appID uint32, hashName string, currency int, p *Price) error {
	page, err := w.get(fmt.Sprintf("%s/market/listings/%d/%s", CommunityURL, appID, url.PathEscape(hashName)))
	if err != nil {
		return err
	}
	m := reNameID.FindSubmatch(page)
	if m == nil {
		return errors.New("no order book")
	}
	body, err := w.get(fmt.Sprintf("%s/market/itemordershistogram?country=US&language=english&currency=%d&item_nameid=%s&two_factor=0", CommunityURL, max(currency, 1), m[1]))
	if err != nil {
		return err
	}
	var r struct {
		Success    int    `json:"success"`
		HighestBuy string `json:"highest_buy_order"`
		LowestSell string `json:"lowest_sell_order"`
	}
	if json.Unmarshal(body, &r) != nil || r.Success != 1 {
		return errors.New("no order book")
	}
	p.HighestBuy, _ = strconv.Atoi(r.HighestBuy)
	p.LowestSell, _ = strconv.Atoi(r.LowestSell)
	return nil
}

func cents(s string) int {
	if s == "" {
		return 0
	}
	return int(parsePrice(s)*100 + 0.5)
}

// ---- fees

// BuyerPays is what a buyer pays when the seller should receive `receive` cents:
// Steam takes 5% and the game's publisher 10%, each at least one cent.
func BuyerPays(receive int) int {
	if receive <= 0 {
		return 0
	}
	return receive + max(1, receive*5/100) + max(1, receive*10/100)
}

// SellerReceives is the most the seller can get for a listing that costs the buyer `pay` cents.
func SellerReceives(pay int) int {
	r := pay * 100 / 115
	for BuyerPays(r+1) <= pay {
		r++
	}
	for r > 0 && BuyerPays(r) > pay {
		r--
	}
	return r
}

// ---- selling and listings

// SellResult tells whether the listing still waits for a confirmation in the Steam app.
type SellResult struct {
	NeedsConfirmation bool
}

// Sell lists an item; receive is what the seller gets in cents (Steam adds the fees on top).
func (w *Web) Sell(appID uint32, contextID, assetID string, amount, receive int) (SellResult, error) {
	form := url.Values{
		"appid": {strconv.Itoa(int(appID))}, "contextid": {contextID}, "assetid": {assetID},
		"amount": {strconv.Itoa(max(amount, 1))}, "price": {strconv.Itoa(receive)},
	}
	body, err := w.post(CommunityURL+"/market/sellitem/", form, w.profile()+"/inventory/")
	var r struct {
		Success      bool   `json:"success"`
		Message      string `json:"message"`
		Confirmation bool   `json:"requires_confirmation"`
		Mobile       bool   `json:"needs_mobile_confirmation"`
		Email        bool   `json:"needs_email_confirmation"`
	}
	json.Unmarshal(body, &r)
	if err != nil && r.Message == "" {
		return SellResult{}, err
	}
	if !r.Success {
		if r.Message == "" {
			r.Message = "the market refused the listing"
		}
		return SellResult{}, errors.New(stripTags(r.Message))
	}
	return SellResult{NeedsConfirmation: r.Confirmation || r.Mobile || r.Email}, nil
}

// Listing is one of your active market listings.
type Listing struct {
	ID       string `json:"id"`
	AppID    uint32 `json:"appid"`
	Name     string `json:"name"`
	HashName string `json:"hashName"`
	Icon     string `json:"icon"`
	Receive  int    `json:"receive"` // what you get, cents
	Pays     int    `json:"pays"`    // what the buyer pays, cents
	Created  int64  `json:"created"`
	Pending  bool   `json:"pending"` // still waits for a confirmation
}

// Listings returns all your active listings, including the ones still waiting for confirmation.
func (w *Web) Listings() ([]Listing, error) {
	var out []Listing
	for start := 0; start < 2000; start += 100 {
		body, err := w.get(fmt.Sprintf("%s/market/mylistings?norender=1&count=100&start=%d", CommunityURL, start))
		if err != nil {
			return out, err
		}
		var r struct {
			Success   bool         `json:"success"`
			Total     int          `json:"total_count"`
			Listings  []rawListing `json:"listings"`
			ToConfirm []rawListing `json:"listings_to_confirm"`
		}
		if json.Unmarshal(body, &r) != nil || !r.Success {
			return out, errors.New("listings are unavailable")
		}
		if start == 0 {
			for _, l := range r.ToConfirm {
				out = append(out, l.listing(true))
			}
		}
		for _, l := range r.Listings {
			out = append(out, l.listing(false))
		}
		if start+100 >= r.Total {
			break
		}
	}
	return out, nil
}

type rawListing struct {
	ID      string `json:"listingid"`
	Created int64  `json:"time_created"`
	Price   int    `json:"price"`
	Fee     int    `json:"fee"`
	Asset   struct {
		AppID    uint32 `json:"appid"`
		Name     string `json:"name"`
		HashName string `json:"market_hash_name"`
		Icon     string `json:"icon_url"`
	} `json:"asset"`
}

func (l rawListing) listing(pending bool) Listing {
	out := Listing{ID: l.ID, AppID: l.Asset.AppID, Name: l.Asset.Name, HashName: l.Asset.HashName,
		Receive: l.Price, Pays: l.Price + l.Fee, Created: l.Created, Pending: pending}
	if l.Asset.Icon != "" {
		out.Icon = EconImage + l.Asset.Icon + "/96fx96f"
	}
	return out
}

// RemoveListing takes a listing off the market; the item returns to the inventory.
func (w *Web) RemoveListing(id string) error {
	_, err := w.post(CommunityURL+"/market/removelisting/"+url.PathEscape(id), url.Values{}, CommunityURL+"/market/")
	return err
}

// ---- search

// MarketResult is one row of a market search.
type MarketResult struct {
	Name     string `json:"name"`
	HashName string `json:"hashName"`
	AppID    uint32 `json:"appid"`
	Game     string `json:"game"`
	Icon     string `json:"icon"`
	Price    int    `json:"price"` // cheapest listing, cents
	Text     string `json:"text"`  // the same price formatted by Steam
	Listings int    `json:"listings"`
}

// MarketSearch searches the Community Market (appID 0 means every game).
func (w *Web) MarketSearch(query string, appID uint32, start int) ([]MarketResult, int, error) {
	q := url.Values{"query": {query}, "start": {strconv.Itoa(start)}, "count": {"50"}, "search_descriptions": {"0"},
		"sort_column": {"popular"}, "sort_dir": {"desc"}, "norender": {"1"}}
	if appID != 0 {
		q.Set("appid", strconv.Itoa(int(appID)))
	}
	body, err := w.get(CommunityURL + "/market/search/render/?" + q.Encode())
	if err != nil {
		return nil, 0, err
	}
	var r struct {
		Success bool `json:"success"`
		Total   int  `json:"total_count"`
		Results []struct {
			Name     string `json:"name"`
			HashName string `json:"hash_name"`
			Listings int    `json:"sell_listings"`
			Price    int    `json:"sell_price"`
			Text     string `json:"sell_price_text"`
			Game     string `json:"app_name"`
			Asset    struct {
				AppID uint32 `json:"appid"`
				Icon  string `json:"icon_url"`
			} `json:"asset_description"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &r) != nil || !r.Success {
		return nil, 0, errors.New("the market search is unavailable")
	}
	out := make([]MarketResult, 0, len(r.Results))
	for _, x := range r.Results {
		m := MarketResult{Name: x.Name, HashName: x.HashName, AppID: x.Asset.AppID, Game: x.Game,
			Price: x.Price, Text: x.Text, Listings: x.Listings}
		if x.Asset.Icon != "" {
			m.Icon = EconImage + x.Asset.Icon + "/96fx96f"
		}
		out = append(out, m)
	}
	return out, r.Total, nil
}
