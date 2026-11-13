package steam

// Web session: steamcommunity.com / store / Web API with the access token, plus the page parsers
// SteamIdler needs (badges, inventory, prices, free promos).

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Hosts can be overridden in tests.
var (
	CommunityURL = "https://steamcommunity.com"
	StoreURL     = "https://store.steampowered.com"
	APIURL       = "https://api.steampowered.com"
)

type Web struct {
	SteamID     uint64
	AccessToken string
	SessionID   string
	http        *http.Client
}

func NewWeb(steamID uint64, accessToken string) *Web {
	jar, _ := cookiejar.New(nil)
	b := make([]byte, 12)
	rand.Read(b)
	w := &Web{SteamID: steamID, AccessToken: accessToken, SessionID: hex.EncodeToString(b)}
	w.http = &http.Client{Timeout: 30 * time.Second, Jar: jar, Transport: HTTP.Transport}
	login := fmt.Sprintf("%d%%7C%%7C%s", steamID, accessToken)
	for _, host := range []string{CommunityURL, StoreURL} {
		u, _ := url.Parse(host)
		jar.SetCookies(u, []*http.Cookie{
			{Name: "steamLoginSecure", Value: login, Path: "/", Secure: true},
			{Name: "sessionid", Value: w.SessionID, Path: "/"},
			{Name: "Steam_Language", Value: "english", Path: "/"},
		})
	}
	return w
}

var ErrWebAuth = errors.New("web session expired")

func (w *Web) do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return body, ErrWebAuth
	}
	if resp.StatusCode == 429 {
		return body, errors.New("rate limited")
	}
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("http %d", resp.StatusCode)
	}
	// community redirects to the login page when the cookie is no longer valid
	if strings.Contains(resp.Request.URL.Path, "/login") {
		return body, ErrWebAuth
	}
	return body, nil
}

func (w *Web) get(u string) ([]byte, error) {
	req, _ := http.NewRequest("GET", u, nil)
	return w.do(req)
}

func (w *Web) post(u string, form url.Values, referer string) ([]byte, error) {
	form.Set("sessionid", w.SessionID)
	req, _ := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return w.do(req)
}

func (w *Web) api(method, iface string, params url.Values, out any) error {
	params.Set("access_token", w.AccessToken)
	u := fmt.Sprintf("%s/%s/v1/", APIURL, iface)
	var body []byte
	var err error
	if method == "GET" {
		body, err = w.get(u + "?" + params.Encode())
	} else {
		req, _ := http.NewRequest("POST", u, strings.NewReader(params.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		body, err = w.do(req)
	}
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (w *Web) profile() string { return fmt.Sprintf("%s/profiles/%d", CommunityURL, w.SteamID) }

// ---- badges: card drops and crafting

type BadgeGame struct {
	AppID        uint32  `json:"appid"`
	Name         string  `json:"name"`
	DropsLeft    int     `json:"dropsLeft"`
	Hours        float64 `json:"hours"`
	CanCraft     bool    `json:"canCraft"`
	CanCraftFoil bool    `json:"canCraftFoil"`
	Level        int     `json:"level"`
}

var (
	reBadgeRow  = regexp.MustCompile(`<div class="badge_row is_link"`)
	reGamecards = regexp.MustCompile(`/gamecards/(\d+)/`)
	reDrops     = regexp.MustCompile(`progress_info_bold">\s*(\d+) card drops? remaining`)
	reHours     = regexp.MustCompile(`([\d,.]+) hrs on record`)
	reTitle     = regexp.MustCompile(`(?s)<div class="badge_title">\s*(.*?)(?:&nbsp;)?<`)
	reCraft     = regexp.MustCompile(`badge_craft_button[^>]*href="[^"]*/gamecards/(\d+)/(\?border=1)?"`)
	reLevel     = regexp.MustCompile(`Level (\d+)`)
	rePageLinks = regexp.MustCompile(`\?p=(\d+)"`)
)

// Badges reads every page of the badges list.
func (w *Web) Badges() ([]BadgeGame, error) {
	var out []BadgeGame
	seen := map[uint32]int{}
	for page := 1; page <= 30; page++ {
		body, err := w.get(fmt.Sprintf("%s/badges/?l=english&p=%d", w.profile(), page))
		if err != nil {
			return nil, err
		}
		s := string(body)
		rows := reBadgeRow.Split(s, -1)
		for _, row := range rows[1:] {
			m := reGamecards.FindStringSubmatch(row)
			if m == nil {
				continue // non-game badge
			}
			id64, _ := strconv.ParseUint(m[1], 10, 32)
			b := BadgeGame{AppID: uint32(id64)}
			if t := reTitle.FindStringSubmatch(row); t != nil {
				b.Name = strings.TrimSpace(html.UnescapeString(stripTags(t[1])))
			}
			if d := reDrops.FindStringSubmatch(row); d != nil {
				b.DropsLeft, _ = strconv.Atoi(d[1])
			}
			if h := reHours.FindStringSubmatch(row); h != nil {
				b.Hours, _ = strconv.ParseFloat(strings.ReplaceAll(h[1], ",", ""), 64)
			}
			if l := reLevel.FindStringSubmatch(row); l != nil {
				b.Level, _ = strconv.Atoi(l[1])
			}
			for _, c := range reCraft.FindAllStringSubmatch(row, -1) {
				if c[2] != "" {
					b.CanCraftFoil = true
				} else {
					b.CanCraft = true
				}
			}
			if i, ok := seen[b.AppID]; ok {
				// foil and normal badges are separate rows
				out[i].CanCraft = out[i].CanCraft || b.CanCraft
				out[i].CanCraftFoil = out[i].CanCraftFoil || b.CanCraftFoil
				if b.DropsLeft > out[i].DropsLeft {
					out[i].DropsLeft = b.DropsLeft
				}
				continue
			}
			seen[b.AppID] = len(out)
			out = append(out, b)
		}
		last := 1
		for _, p := range rePageLinks.FindAllStringSubmatch(s, -1) {
			if n, _ := strconv.Atoi(p[1]); n > last {
				last = n
			}
		}
		if page >= last {
			break
		}
	}
	return out, nil
}

var reTags = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return reTags.ReplaceAllString(s, "") }

func (w *Web) CraftBadge(appID uint32, foil bool) error {
	border := "0"
	if foil {
		border = "1"
	}
	form := url.Values{"appid": {fmt.Sprint(appID)}, "series": {"1"}, "border_color": {border}, "levels": {"1"}}
	body, err := w.post(w.profile()+"/ajaxcraftbadge/", form, fmt.Sprintf("%s/gamecards/%d/", w.profile(), appID))
	if err != nil {
		return err
	}
	var r struct {
		Success int `json:"success"`
	}
	json.Unmarshal(body, &r)
	if r.Success != 1 {
		return errors.New("craft failed")
	}
	return nil
}

// ---- level

type Level struct {
	Level       int `json:"level"`
	XP          int `json:"xp"`
	XPToNext    int `json:"xpToNext"`
	XPThisLevel int `json:"xpThisLevel"`
}

func (w *Web) Level() (Level, error) {
	var r struct {
		Response struct {
			PlayerXP           int `json:"player_xp"`
			PlayerLevel        int `json:"player_level"`
			NeededToLevelUp    int `json:"player_xp_needed_to_level_up"`
			NeededCurrentLevel int `json:"player_xp_needed_current_level"`
		} `json:"response"`
	}
	err := w.api("GET", "IPlayerService/GetBadges", url.Values{"steamid": {fmt.Sprint(w.SteamID)}}, &r)
	return Level{r.Response.PlayerLevel, r.Response.PlayerXP, r.Response.NeededToLevelUp, r.Response.NeededCurrentLevel}, err
}

// XPForLevel is the total XP needed to reach a Steam level.
func XPForLevel(level int) int {
	xp := 0
	for l := 1; l <= level; l++ {
		xp += 100 * ((l-1)/10 + 1)
	}
	return xp
}

// ---- trade offers: accept gifts only

type TradeOffer struct {
	ID      string
	Partner uint64
	Give    int
	Receive int
}

func (w *Web) IncomingOffers() ([]TradeOffer, error) {
	var r struct {
		Response struct {
			Received []struct {
				ID      string `json:"tradeofferid"`
				Other   uint32 `json:"accountid_other"`
				State   int    `json:"trade_offer_state"`
				Give    []any  `json:"items_to_give"`
				Receive []any  `json:"items_to_receive"`
			} `json:"trade_offers_received"`
		} `json:"response"`
	}
	params := url.Values{"get_received_offers": {"1"}, "active_only": {"1"}, "get_descriptions": {"0"}}
	if err := w.api("GET", "IEconService/GetTradeOffers", params, &r); err != nil {
		return nil, err
	}
	var out []TradeOffer
	for _, o := range r.Response.Received {
		if o.State == 2 { // active
			out = append(out, TradeOffer{ID: o.ID, Partner: 76561197960265728 + uint64(o.Other), Give: len(o.Give), Receive: len(o.Receive)})
		}
	}
	return out, nil
}

func (w *Web) AcceptOffer(o TradeOffer) error {
	form := url.Values{"serverid": {"1"}, "tradeofferid": {o.ID}, "partner": {fmt.Sprint(o.Partner)}, "captcha": {""}}
	_, err := w.post(fmt.Sprintf("%s/tradeoffer/%s/accept", CommunityURL, o.ID), form, fmt.Sprintf("%s/tradeoffer/%s/", CommunityURL, o.ID))
	return err
}

// ---- inventory value (Steam items: cards, backgrounds, emoticons)

type InventoryItem struct {
	HashName string
	Count    int
}

func (w *Web) CommunityInventory() ([]InventoryItem, error) {
	body, err := w.get(fmt.Sprintf("%s/inventory/%d/753/6?l=english&count=2000", CommunityURL, w.SteamID))
	if err != nil {
		return nil, err
	}
	var r struct {
		Assets []struct {
			ClassID    string `json:"classid"`
			InstanceID string `json:"instanceid"`
			Amount     string `json:"amount"`
		} `json:"assets"`
		Descriptions []struct {
			ClassID    string `json:"classid"`
			InstanceID string `json:"instanceid"`
			HashName   string `json:"market_hash_name"`
			Marketable int    `json:"marketable"`
		} `json:"descriptions"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, d := range r.Descriptions {
		if d.Marketable == 1 {
			names[d.ClassID+"_"+d.InstanceID] = d.HashName
		}
	}
	counts := map[string]int{}
	for _, a := range r.Assets {
		if n := names[a.ClassID+"_"+a.InstanceID]; n != "" {
			c, _ := strconv.Atoi(a.Amount)
			counts[n] += max(c, 1)
		}
	}
	var out []InventoryItem
	for n, c := range counts {
		out = append(out, InventoryItem{n, c})
	}
	return out, nil
}

var reNumber = regexp.MustCompile(`[\d.,\s]+`)

// MarketPrice returns the lowest listing price for a Steam community item in the given currency.
func (w *Web) MarketPrice(hashName string, currency int) (float64, error) {
	u := fmt.Sprintf("%s/market/priceoverview/?appid=753&currency=%d&market_hash_name=%s", CommunityURL, currency, url.QueryEscape(hashName))
	body, err := w.get(u)
	if err != nil {
		return 0, err
	}
	var r struct {
		Success bool   `json:"success"`
		Lowest  string `json:"lowest_price"`
		Median  string `json:"median_price"`
	}
	json.Unmarshal(body, &r)
	price := r.Median
	if price == "" {
		price = r.Lowest
	}
	return parsePrice(price), nil
}

func parsePrice(s string) float64 {
	n := strings.TrimSpace(strings.ReplaceAll(reNumber.FindString(s), " ", ""))
	if i := strings.LastIndexAny(n, ".,"); i >= 0 && len(n)-i-1 <= 2 {
		n = strings.NewReplacer(".", "", ",", "").Replace(n[:i]) + "." + n[i+1:]
	} else {
		n = strings.NewReplacer(".", "", ",", "").Replace(n)
	}
	v, _ := strconv.ParseFloat(n, 64)
	return v
}

// ---- store: free promos, wishlist, sale stickers

type FreePromo struct {
	AppID     uint32
	PackageID uint32
	Name      string
}

var (
	reSearchRow = regexp.MustCompile(`(?s)(<a [^>]*search_result_row[^>]*>).*?<span class="title">(.*?)</span>`)
	reDSApp     = regexp.MustCompile(`data-ds-appid="([\d,]+)"`)
	reDSPackage = regexp.MustCompile(`data-ds-packageid="(\d+)"`)
)

// FreePromos lists games that are 100% off right now (free to keep).
func (w *Web) FreePromos() ([]FreePromo, error) {
	body, err := w.get(StoreURL + "/search/results/?query&start=0&count=50&maxprice=free&specials=1&infinite=1&l=english")
	if err != nil {
		return nil, err
	}
	var r struct {
		HTML string `json:"results_html"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	var out []FreePromo
	for _, m := range reSearchRow.FindAllStringSubmatch(r.HTML, -1) {
		p := FreePromo{Name: html.UnescapeString(strings.TrimSpace(m[2]))}
		if a := reDSApp.FindStringSubmatch(m[1]); a != nil {
			id, _ := strconv.ParseUint(strings.Split(a[1], ",")[0], 10, 32)
			p.AppID = uint32(id)
		}
		if k := reDSPackage.FindStringSubmatch(m[1]); k != nil {
			id, _ := strconv.ParseUint(k[1], 10, 32)
			p.PackageID = uint32(id)
		}
		if p.AppID != 0 || p.PackageID != 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

func (w *Web) AddFreeLicense(packageID uint32) error {
	_, err := w.post(fmt.Sprintf("%s/checkout/addfreelicense/%d", StoreURL, packageID), url.Values{"ajax": {"true"}}, StoreURL+"/")
	return err
}

func (w *Web) ClaimSaleSticker() (bool, error) {
	var r struct {
		Response struct {
			ItemID string `json:"communityitemid"`
		} `json:"response"`
	}
	err := w.api("POST", "ISaleItemRewardsService/ClaimItem", url.Values{"language": {"english"}}, &r)
	return r.Response.ItemID != "", err
}

func (w *Web) Wishlist() ([]uint32, error) {
	var r struct {
		Response struct {
			Items []struct {
				AppID uint32 `json:"appid"`
			} `json:"items"`
		} `json:"response"`
	}
	if err := w.api("GET", "IWishlistService/GetWishlist", url.Values{"steamid": {fmt.Sprint(w.SteamID)}}, &r); err != nil {
		return nil, err
	}
	var ids []uint32
	for _, it := range r.Response.Items {
		ids = append(ids, it.AppID)
	}
	return ids, nil
}

type StorePrice struct {
	AppID    uint32
	Discount int
	Final    string
}

// Prices returns current discounts for up to ~100 apps per call.
func (w *Web) Prices(appIDs []uint32) ([]StorePrice, error) {
	var ids []string
	for _, id := range appIDs {
		ids = append(ids, fmt.Sprint(id))
	}
	body, err := w.get(StoreURL + "/api/appdetails?filters=price_overview&appids=" + strings.Join(ids, ","))
	if err != nil {
		return nil, err
	}
	var r map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Price struct {
				Discount int    `json:"discount_percent"`
				Final    string `json:"final_formatted"`
			} `json:"price_overview"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, nil // apps without a price come back as [] instead of an object
	}
	var out []StorePrice
	for k, v := range r {
		id, _ := strconv.ParseUint(k, 10, 32)
		if v.Success {
			out = append(out, StorePrice{uint32(id), v.Data.Price.Discount, v.Data.Price.Final})
		}
	}
	return out, nil
}

// OwnedGames is the Web API twin of Client.OwnedGames (used when the CM call fails).
func (w *Web) OwnedGames() ([]OwnedGame, error) {
	var r struct {
		Response struct {
			Games []struct {
				AppID    uint32 `json:"appid"`
				Name     string `json:"name"`
				Minutes  int64  `json:"playtime_forever"`
				Icon     string `json:"img_icon_url"`
				HasStats bool   `json:"has_community_visible_stats"`
			} `json:"games"`
		} `json:"response"`
	}
	params := url.Values{"steamid": {fmt.Sprint(w.SteamID)}, "include_appinfo": {"1"}, "include_played_free_games": {"1"}, "skip_unvetted_apps": {"0"}}
	if err := w.api("GET", "IPlayerService/GetOwnedGames", params, &r); err != nil {
		return nil, err
	}
	var out []OwnedGame
	for _, g := range r.Response.Games {
		og := OwnedGame{AppID: g.AppID, Name: g.Name, Minutes: g.Minutes, HasStats: g.HasStats}
		if og.Name == "" {
			og.Name = fmt.Sprintf("App %d", g.AppID)
		}
		if g.Icon != "" {
			og.Icon = fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steamcommunity/public/images/apps/%d/%s.jpg", g.AppID, g.Icon)
		}
		out = append(out, og)
	}
	return out, nil
}

// AppName looks up a store name (for wishlist games that aren't owned).
func (w *Web) AppName(appID uint32) string {
	body, err := w.get(fmt.Sprintf("%s/api/appdetails?filters=basic&appids=%d", StoreURL, appID))
	if err != nil {
		return ""
	}
	var r map[string]struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	json.Unmarshal(body, &r)
	return r[fmt.Sprint(appID)].Data.Name
}

// AchievementPercents returns the global unlock percentage per achievement id.
func AchievementPercents(appID uint32) map[string]float64 {
	out := map[string]float64{}
	resp, err := HTTP.Get(fmt.Sprintf("%s/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v2/?gameid=%d", APIURL, appID))
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	var r struct {
		A struct {
			List []struct {
				Name    string      `json:"name"`
				Percent json.Number `json:"percent"`
			} `json:"achievements"`
		} `json:"achievementpercentages"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	for _, a := range r.A.List {
		f, _ := a.Percent.Float64()
		out[a.Name] = f
	}
	return out
}
