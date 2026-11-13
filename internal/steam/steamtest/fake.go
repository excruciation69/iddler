// Package steamtest is a fake Steam (CM server, Web API, community) for tests.
package steamtest

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"iddler/internal/steam"
)

const SteamID = 76561198000000001

type Fake struct {
	Server   *httptest.Server
	key      *rsa.PrivateKey
	mu       sync.Mutex
	conn     net.Conn
	CodeOK   bool
	Played   [][]string
	Persona  []string
	Chats    []string
	Stored   []string
	Badges   string
	Sold     []string // market sellitem requests: assetid@price
	Removed  int      // removed listings
	unlocked uint32
	// FailOwned makes the CM refuse Player.GetOwnedGames (to test the Web API fallback)
	FailOwned bool
}

func Token(id uint64) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"EdDSA"}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":"steam","sub":"%d","aud":["client","web"],"exp":%d}`, id, time.Now().Add(200*24*time.Hour).Unix())))
	return h + "." + p + ".sig"
}

// Start runs the fake and points the steam package at it.
func Start() *Fake {
	key, _ := rsa.GenerateKey(rand.Reader, 1024)
	f := &Fake{key: key}
	mux := http.NewServeMux()
	f.Server = httptest.NewTLSServer(mux)
	host := strings.TrimPrefix(f.Server.URL, "https://")
	mux.HandleFunc("/cmsocket/", f.serveWS)
	mux.HandleFunc("/dir", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"serverlist": []map[string]any{
			{"endpoint": host, "type": "websockets", "realm": "steamglobal"}}}})
	})
	mux.HandleFunc("/profiles/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		io.WriteString(w, f.Badges)
	})
	mux.HandleFunc("/ISteamUserStats/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"achievementpercentages":{"achievements":[{"name":"ACH_A","percent":"71.5"},{"name":"ACH_B","percent":3.2}]}}`)
	})
	stub := func(path, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
	}
	stub("/IWishlistService/", `{"response":{"items":[{"appid":1245620},{"appid":1091500}]}}`)
	mux.HandleFunc("/api/appdetails", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filters") == "basic" {
			id := r.URL.Query().Get("appids")
			name := map[string]string{"1245620": "ELDEN RING", "1091500": "Cyberpunk 2077"}[id]
			fmt.Fprintf(w, `{"%s":{"success":true,"data":{"name":"%s"}}}`, id, name)
			return
		}
		io.WriteString(w, `{"1245620":{"success":true,"data":{"price_overview":{"discount_percent":60,"final_formatted":"719₴"}}},"1091500":{"success":true,"data":{"price_overview":{"discount_percent":20,"final_formatted":"1199₴"}}}}`)
	})
	stub("/search/results/", `{"results_html":"<a href=\"x\" data-ds-appid=\"2923300\" class=\"search_result_row\"><span class=\"title\">Banana Pack</span></a>"}`)
	stub("/IPlayerService/GetBadges/", `{"response":{"player_xp":4350,"player_level":27,"player_xp_needed_to_level_up":150,"player_xp_needed_current_level":4200}}`)
	stub("/IPlayerService/GetOwnedGames/", `{"response":{"game_count":2,"games":[{"appid":620,"name":"Portal 2","playtime_forever":640,"img_icon_url":"p2"},{"appid":105600,"name":"Terraria","playtime_forever":4400}]}}`)
	stub("/inventory/", fakeInventory)
	f.market(mux)
	stub("/mobileconf/getlist", `{"success":true,"conf":[{"id":"9001","nonce":"55","type":3,"headline":"Sell - Portal 2 card","summary":["4,50₴"]}]}`)
	stub("/mobileconf/multiajaxop", `{"success":true}`)
	stub("/ISaleItemRewardsService/", `{"response":{}}`)
	stub("/checkout/", `{}`)
	steam.DirectoryURL = f.Server.URL + "/dir"
	steam.CommunityURL, steam.StoreURL, steam.APIURL = f.Server.URL, f.Server.URL, f.Server.URL
	steam.HTTP = f.Server.Client()
	steam.TLSConfig = func(string) *tls.Config { return &tls.Config{InsecureSkipVerify: true} }
	return f
}

func (f *Fake) Close() { f.Server.Close() }

func (f *Fake) serveWS(w http.ResponseWriter, r *http.Request) {
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	conn, brw, _ := w.(http.Hijacker).Hijack()
	f.mu.Lock()
	f.conn = conn
	f.mu.Unlock()
	fmt.Fprintf(brw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
	brw.Flush()
	for {
		msg, err := readFrame(brw.Reader)
		if err != nil {
			return
		}
		f.handle(conn, msg)
	}
}

func readFrame(r io.Reader) ([]byte, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		b := make([]byte, 2)
		io.ReadFull(r, b)
		n = uint64(binary.BigEndian.Uint16(b))
	case 127:
		b := make([]byte, 8)
		io.ReadFull(r, b)
		n = binary.BigEndian.Uint64(b)
	}
	mask := make([]byte, 4)
	io.ReadFull(r, mask)
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return nil, err
	}
	for i := range p {
		p[i] ^= mask[i&3]
	}
	if h[0]&0x0f == 8 {
		return nil, io.EOF
	}
	return p, nil
}

func send(conn net.Conn, payload []byte) {
	hdr := []byte{0x82}
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 65536:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	conn.Write(append(hdr, payload...))
}

func Packet(emsg uint32, hdr *steam.PB, body []byte) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, emsg|0x80000000)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(hdr.Data())))
	return append(append(buf, hdr.Data()...), body...)
}

// Push sends a server message to the connected client.
func (f *Fake) Push(emsg uint32, body *steam.PB) {
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	send(c, Packet(emsg, steam.NewPB(), body.Data()))
}

// Drop closes the current connection.
func (f *Fake) Drop() {
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	c.Close()
}

func (f *Fake) reply(conn net.Conn, emsg uint32, job uint64, res int, body *steam.PB) {
	send(conn, Packet(emsg, steam.NewPB().Fixed64(11, job).Int(13, int64(res)), body.Data()))
}

func (f *Fake) handle(conn net.Conn, data []byte) {
	emsg := binary.LittleEndian.Uint32(data) &^ 0x80000000
	hl := binary.LittleEndian.Uint32(data[4:])
	hdr, _ := steam.Parse(data[8 : 8+hl])
	body, _ := steam.Parse(data[8+hl:])
	job := hdr.U(10)
	switch emsg {
	case steam.EMsgServiceMethodCallFromClientNonAuthed, steam.EMsgServiceMethodCallFromClient:
		f.service(conn, hdr.S(12), job, body)
	case steam.EMsgClientLogon:
		resp := Packet(steam.EMsgClientLogOnResponse, steam.NewPB().Fixed64(1, SteamID).Int(2, 4242), steam.NewPB().Int(1, steam.ResOK).Int(3, 1).Data())
		info := Packet(steam.EMsgClientAccountInfo, steam.NewPB(), steam.NewPB().Str(1, personaName()).Data())
		friends := Packet(steam.EMsgClientFriendsList, steam.NewPB(), steam.NewPB().Msg(2, steam.NewPB().Fixed64(1, 76561198000000002).Uint(2, 3)).Data())
		var inner bytes.Buffer
		for _, p := range [][]byte{resp, info, friends} {
			binary.Write(&inner, binary.LittleEndian, uint32(len(p)))
			inner.Write(p)
		}
		var z bytes.Buffer
		zw := gzip.NewWriter(&z)
		zw.Write(inner.Bytes())
		zw.Close()
		send(conn, Packet(steam.EMsgMulti, steam.NewPB(), steam.NewPB().Uint(1, uint64(inner.Len())).Bytes(2, z.Bytes()).Data()))
	case steam.EMsgClientGamesPlayedWithDataBlob:
		var ids []string
		for _, g := range body.Msgs(1) {
			s := fmt.Sprint(g.U(2))
			if g.S(7) != "" {
				s = g.S(7)
			}
			ids = append(ids, s)
		}
		f.mu.Lock()
		f.Played = append(f.Played, ids)
		f.mu.Unlock()
	case steam.EMsgClientChangeStatus:
		f.mu.Lock()
		f.Persona = append(f.Persona, fmt.Sprintf("%d:%s", body.U(1), body.S(2)))
		f.mu.Unlock()
	case steam.EMsgClientRegisterKey:
		key := body.S(1)
		res, detail, name := steam.ResOK, 0, "Portal 2"
		switch {
		case strings.HasPrefix(key, "BAD"):
			res, detail, name = steam.ResFail, steam.PurchaseBadKey, ""
		case strings.HasPrefix(key, "OWN"):
			res, detail = steam.ResFail, steam.PurchaseAlreadyOwned
		}
		receipt := kvReceipt(name)
		f.reply(conn, steam.EMsgClientPurchaseResponse, job, 1, steam.NewPB().Int(1, int64(res)).Int(2, int64(detail)).Bytes(3, receipt))
	case steam.EMsgClientRequestFreeLicense:
		granted := steam.NewPB().Int(1, steam.ResOK)
		for _, id := range body.Uints(2) {
			granted.Uint(3, id)
		}
		f.reply(conn, steam.EMsgClientRequestFreeLicenseResponse, job, 1, granted)
	case steam.EMsgClientGetUserStats:
		f.mu.Lock()
		mask := f.unlocked
		f.mu.Unlock()
		// reply without a job id, like some CMs do
		send(conn, Packet(steam.EMsgClientGetUserStatsResponse, steam.NewPB(), steam.NewPB().Fixed64(1, body.U(1)).Int(2, steam.ResOK).Uint(3, 77).
			Bytes(4, schema()).Msg(5, steam.NewPB().Uint(1, 1).Uint(2, uint64(mask))).Data()))
	case steam.EMsgClientStoreUserStats2:
		st := body.Msgs(6)
		f.mu.Lock()
		for _, s := range st {
			f.unlocked = uint32(s.U(2))
			f.Stored = append(f.Stored, fmt.Sprintf("%d=%d crc=%d", s.U(1), s.U(2), body.U(4)))
		}
		f.mu.Unlock()
		f.reply(conn, steam.EMsgClientStoreUserStatsResponse, job, 1, steam.NewPB().Fixed64(1, body.U(1)).Int(2, steam.ResOK).Uint(3, 78))
	}
}

func (f *Fake) service(conn net.Conn, method string, job uint64, body steam.Fields) {
	r := func(res int, b *steam.PB) { f.reply(conn, steam.EMsgServiceMethodResponse, job, res, b) }
	switch method {
	case "Authentication.GetPasswordRSAPublicKey#1":
		r(steam.ResOK, steam.NewPB().Str(1, fmt.Sprintf("%x", f.key.N)).Str(2, "010001").Uint(3, 12345))
	case "Authentication.BeginAuthSessionViaCredentials#1":
		enc, _ := base64.StdEncoding.DecodeString(body.S(3))
		pw, err := rsa.DecryptPKCS1v15(rand.Reader, f.key, enc)
		if err != nil || string(pw) != "secret" {
			r(steam.ResInvalidPassword, steam.NewPB())
			return
		}
		conf := steam.NewPB().Uint(1, steam.GuardEmailCode).Str(2, "gmail.com")
		r(steam.ResOK, steam.NewPB().Uint(1, 777).Bytes(2, []byte{1}).Fixed32(3, 0x3f800000).Msg(4, conf).Uint(5, SteamID))
	case "Authentication.UpdateAuthSessionWithSteamGuardCode#1":
		ok := body.S(3) == "AB12C"
		f.mu.Lock()
		f.CodeOK = ok
		f.mu.Unlock()
		if ok {
			r(steam.ResOK, steam.NewPB())
		} else {
			r(steam.ResInvalidAuthCode, steam.NewPB())
		}
	case "Authentication.PollAuthSessionStatus#1":
		if body.U(1) == 888 { // QR sign-in: the phone approves on the first poll
			r(steam.ResOK, steam.NewPB().Str(3, Token(SteamID)).Str(6, "qr_user"))
			return
		}
		f.mu.Lock()
		ok := f.CodeOK
		f.mu.Unlock()
		if ok {
			r(steam.ResOK, steam.NewPB().Str(3, Token(SteamID)))
		} else {
			r(steam.ResOK, steam.NewPB())
		}
	case "Authentication.BeginAuthSessionViaQR#1":
		r(steam.ResOK, steam.NewPB().Uint(1, 888).Str(2, "https://s.team/q/1/888").Bytes(3, []byte{2}).Fixed32(4, 0x3f800000))
	case "Authentication.GenerateAccessTokenForApp#1":
		r(steam.ResOK, steam.NewPB().Str(1, "access-token"))
	case "Player.GetOwnedGames#1":
		f.mu.Lock()
		fail := f.FailOwned
		f.mu.Unlock()
		if fail {
			r(steam.ResAccessDenied, steam.NewPB())
			return
		}
		g1 := steam.NewPB().Uint(1, 730).Str(2, "Counter-Strike 2").Uint(4, 600).Str(5, "abc").Bool(7, true)
		g2 := steam.NewPB().Uint(1, 440).Str(2, "Team Fortress 2").Uint(4, 60).Bool(7, true)
		g3 := steam.NewPB().Uint(1, 41000).Str(2, "Serious Sam: The Second Encounter").Uint(4, 5).Bool(7, true)
		r(steam.ResOK, steam.NewPB().Uint(1, 3).Msg(2, g1).Msg(2, g2).Msg(2, g3))
	case "FriendMessages.SendMessage#1":
		f.mu.Lock()
		f.Chats = append(f.Chats, fmt.Sprintf("%d:%s", body.U(1), body.S(3)))
		f.mu.Unlock()
		r(steam.ResOK, steam.NewPB())
	default:
		r(steam.ResOK, steam.NewPB())
	}
}

// IncomingChat simulates a friend's message.
func (f *Fake) IncomingChat(from uint64, text string) {
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	hdr := steam.NewPB().Str(12, "FriendMessagesClient.IncomingMessage#1")
	send(c, Packet(steam.EMsgServiceMethod, hdr, steam.NewPB().Fixed64(1, from).Int(2, 1).Str(4, text).Data()))
}

func (f *Fake) LastPlayed() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Played) == 0 {
		return "<none>"
	}
	return strings.Join(f.Played[len(f.Played)-1], ",")
}

func (f *Fake) Snapshot() (chats, stored, persona []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Chats...), append([]string(nil), f.Stored...), append([]string(nil), f.Persona...)
}

// ---- binary KV helpers

type kv struct{ bytes.Buffer }

func (k *kv) cstr(s string)     { k.WriteString(s); k.WriteByte(0) }
func (k *kv) open(n string) *kv { k.WriteByte(0); k.cstr(n); return k }
func (k *kv) end() *kv          { k.WriteByte(8); return k }
func (k *kv) s(n, v string) *kv { k.WriteByte(1); k.cstr(n); k.cstr(v); return k }
func (k *kv) i(n string, v int32) *kv {
	k.WriteByte(2)
	k.cstr(n)
	binary.Write(k, binary.LittleEndian, v)
	return k
}

func kvReceipt(name string) []byte {
	var k kv
	k.open("MessageObject").open("lineitems")
	if name != "" {
		k.open("0").i("PackageID", 7).s("ItemDescription", name).end()
	}
	k.end().end().end()
	return k.Bytes()
}

func schema() []byte {
	var k kv
	k.open("41000").open("stats").open("1").s("type", "4").i("id", 1).open("bits").
		open("0").s("name", "ACH_A").open("display").open("name").s("english", "First Blood").s("russian", "Первая кровь").end().
		open("desc").s("english", "Kill 10 enemies").end().s("hidden", "0").s("icon", "a.jpg").end().end().
		open("1").s("name", "ACH_B").open("display").s("name", "Kamikaze Ninja").s("desc", "Kill 50 kamikaze").end().end().
		open("2").s("name", "ACH_P").i("permission", 2).open("display").s("name", "Server Champion").end().end().
		end().end().end().end().end()
	return k.Bytes()
}

// personaName is "Gleb" in tests; IDDLER_DEMO_NAME changes it for screenshots.
func personaName() string {
	if n := os.Getenv("IDDLER_DEMO_NAME"); n != "" {
		return n
	}
	return "Gleb"
}

// ---- inventory and market

const fakeInventory = `{"assets":[
 {"assetid":"11","classid":"1","instanceid":"0","amount":"1"},{"assetid":"12","classid":"1","instanceid":"0","amount":"1"},{"assetid":"13","classid":"1","instanceid":"0","amount":"1"},
 {"assetid":"21","classid":"2","instanceid":"0","amount":"1"},{"assetid":"31","classid":"3","instanceid":"0","amount":"1"}],
 "descriptions":[
 {"classid":"1","instanceid":"0","name":"Chell","market_hash_name":"620-Chell","type":"Portal 2 Trading Card","icon_url":"i1","marketable":1,"tradable":1,
  "tags":[{"category":"Game","localized_tag_name":"Portal 2"},{"category":"item_class","internal_name":"item_class_2"},{"category":"cardborder","internal_name":"cardborder_0"}]},
 {"classid":"2","instanceid":"0","name":"Aperture Lab","market_hash_name":"620-Aperture Lab","type":"Portal 2 Profile Background","icon_url":"i2","marketable":1,"tradable":1,
  "tags":[{"category":"Game","localized_tag_name":"Portal 2"},{"category":"item_class","internal_name":"item_class_3"}]},
 {"classid":"3","instanceid":"0","name":"Gift","market_hash_name":"","type":"Gift","marketable":0,"tradable":0,"tags":[]}],
 "more_items":0}`

// Sold counts sellitem requests; Removed counts removed listings.
func (f *Fake) market(mux *http.ServeMux) {
	mux.HandleFunc("/market/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/market/priceoverview"):
			io.WriteString(w, `{"success":true,"lowest_price":"4,50₴","median_price":"4,20₴","volume":"1,204"}`)
		case strings.HasPrefix(p, "/market/listings/"):
			io.WriteString(w, `<script>Market_LoadOrderSpread( 176321 );</script>`)
		case strings.HasPrefix(p, "/market/itemordershistogram"):
			io.WriteString(w, `{"success":1,"highest_buy_order":"398","lowest_sell_order":"450"}`)
		case strings.HasPrefix(p, "/market/sellitem"):
			r.ParseForm()
			f.mu.Lock()
			f.Sold = append(f.Sold, r.Form.Get("assetid")+"@"+r.Form.Get("price"))
			f.mu.Unlock()
			io.WriteString(w, `{"success":true,"requires_confirmation":1,"needs_mobile_confirmation":true}`)
		case strings.HasPrefix(p, "/market/mylistings"):
			io.WriteString(w, `{"success":true,"total_count":1,"listings":[{"listingid":"5001","time_created":1700000000,"price":391,"fee":58,
				"asset":{"appid":753,"name":"Chell","market_hash_name":"620-Chell","icon_url":"i1"}}],"listings_to_confirm":[]}`)
		case strings.HasPrefix(p, "/market/removelisting/"):
			f.mu.Lock()
			f.Removed++
			f.mu.Unlock()
			io.WriteString(w, `[]`)
		case strings.HasPrefix(p, "/market/search/render"):
			io.WriteString(w, `{"success":true,"total_count":1,"results":[{"name":"Mann Co. Supply Crate Key","hash_name":"Mann Co. Supply Crate Key",
				"sell_listings":34012,"sell_price":219,"sell_price_text":"$2.19","app_name":"Team Fortress 2","asset_description":{"appid":440,"icon_url":"k"}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
}
