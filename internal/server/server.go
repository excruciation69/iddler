// Package server serves the UI and its JSON API on 127.0.0.1 (and optionally the local network).
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"iddler/internal/bot"
	"iddler/internal/hub"
	"iddler/internal/platform"
	"iddler/internal/steam"
	"iddler/ui"
)

const lanPort = 47803

type Server struct {
	Hub    *hub.Hub
	P      platform.Platform
	Key    string
	Port   int
	OnQuit func()

	lanMu sync.Mutex
	lan   *http.Server

	menuAt atomic.Int64 // last poll from the menu bar helper (unix nanoseconds)
}

// MenuAlive reports whether the menu bar helper polled within the last few seconds.
func (s *Server) MenuAlive() bool {
	return time.Since(time.Unix(0, s.menuAt.Load())) < 6*time.Second
}

func New(h *hub.Hub, p platform.Platform) *Server {
	k := make([]byte, 16)
	rand.Read(k)
	return &Server{Hub: h, P: p, Key: hex.EncodeToString(k)}
}

func (s *Server) Base() string { return fmt.Sprintf("http://127.0.0.1:%d", s.Port) }

func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.Port = ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, s.handler(true))
	s.syncLAN()
	return nil
}

func (s *Server) handler(local bool) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(ui.FS, ".")
	if dir := os.Getenv("IDDLER_UI_DIR"); dir != "" { // development: serve the UI from disk
		static = os.DirFS(dir)
	}
	files := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".go") || strings.HasSuffix(r.URL.Path, "menubar.js") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/events", s.auth(s.events))
	mux.HandleFunc("/api/call/", s.auth(s.call))
	mux.HandleFunc("/api/menu", s.auth(s.menu))
	mux.HandleFunc("/api/menu/", s.auth(s.menu))
	mux.HandleFunc("/api/ping", s.auth(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))

	host := fmt.Sprintf("127.0.0.1:%d", s.Port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the loopback server only answers requests addressed to itself (blocks DNS rebinding)
		if local && r.Host != host {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := r.Header.Get("X-Iddler-Key")
		if k == "" {
			k = r.URL.Query().Get("k")
		}
		if subtle.ConstantTimeCompare([]byte(k), []byte(s.Key)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------- LAN panel

func lanIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.String()
		}
	}
	return ""
}

func (s *Server) syncLAN() {
	want := s.Hub.Settings().LAN
	s.lanMu.Lock()
	defer s.lanMu.Unlock()
	if want == (s.lan != nil) {
		return
	}
	if !want {
		s.lan.Close()
		s.lan = nil
		s.Hub.SetLANURL("")
		return
	}
	ip := lanIP()
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", lanPort))
	if err != nil || ip == "" {
		s.Hub.Log("", "err", "log.lanFailed")
		return
	}
	s.lan = &http.Server{Handler: s.handler(false)}
	go s.lan.Serve(ln)
	s.Hub.SetLANURL(fmt.Sprintf("http://%s:%d/?k=%s", ip, lanPort, s.Key))
}

// ---------------------------------------------------------------- events

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch, init := s.Hub.Subscribe()
	defer s.Hub.Unsubscribe(ch)
	write := func(e hub.Event) { fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, e.Data) }
	for _, e := range init {
		write(e)
	}
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			write(e)
			for more := true; more; {
				select {
				case e2 := <-ch:
					write(e2)
				default:
					more = false
				}
			}
			fl.Flush()
		case <-ping.C:
			io.WriteString(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// ---------------------------------------------------------------- menu bar (polled by the macOS script)

func (s *Server) menu(w http.ResponseWriter, r *http.Request) {
	s.menuAt.Store(time.Now().UnixNano())
	switch strings.TrimPrefix(r.URL.Path, "/api/menu") {
	case "/start":
		s.Hub.StartAll()
	case "/stop":
		s.Hub.StopAll()
	case "/show":
		s.Hub.RequestShow()
	case "/quit":
		go s.OnQuit()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.Hub.Brief())
}

// ---------------------------------------------------------------- calls

type args struct {
	ID       string          `json:"id"`
	Account  string          `json:"account"`
	Password string          `json:"password"`
	Remember bool            `json:"remember"`
	Code     string          `json:"code"`
	Forget   bool            `json:"forget"`
	Patch    json.RawMessage `json:"patch"`
	Module   string          `json:"module"`
	On       bool            `json:"on"`
	AppID    uint32          `json:"appid"`
	Name     string          `json:"name"`
	IDs      []string        `json:"ids"`
	Unlock   bool            `json:"unlock"`
	Human    bool            `json:"human"`
	Text     string          `json:"text"`
	At       int64           `json:"at"`
	Token    string          `json:"token"`
	URL      string          `json:"url"`
	Lang     string          `json:"lang"`
	File     string          `json:"file"` // base64

	// inventory manager
	Context  string          `json:"context"`
	Hash     string          `json:"hash"`
	Book     bool            `json:"book"`
	Refresh  bool            `json:"refresh"`
	Orders   []bot.SellOrder `json:"orders"`
	Mode     string          `json:"mode"`
	Undercut int             `json:"undercut"`
	Min      int             `json:"min"`
	Max      int             `json:"max"`
	Kinds    []string        `json:"kinds"`
	Query    string          `json:"query"`
	Start    int             `json:"start"`
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var a args
	json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&a)
	name := strings.TrimPrefix(r.URL.Path, "/api/call/")
	out, err := s.dispatch(name, a)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": out})
}

var errNoAccount = errors.New("err.noAccount")

func (s *Server) dispatch(name string, a args) (any, error) {
	h := s.Hub
	// app-wide calls
	switch name {
	case "settings":
		err := h.UpdateSettings(a.Patch)
		s.syncLAN()
		return nil, err
	case "addAccount":
		return h.AddAccount(a.Account, a.Password, a.Remember)
	case "qrStart":
		return h.StartQR()
	case "qrState":
		return h.QR(), nil
	case "qrCancel":
		h.CancelQR()
		return nil, nil
	case "removeAccount":
		h.RemoveAccount(a.ID)
		return nil, nil
	case "startAll":
		h.StartAll()
		return nil, nil
	case "stopAll":
		h.StopAll()
		return nil, nil
	case "tgConnect":
		return h.TelegramConnect(a.Token)
	case "tgDisconnect":
		h.TelegramDisconnect()
		return nil, nil
	case "tgCode":
		return h.TelegramCode(), nil
	case "openExternal":
		for _, p := range []string{"https://store.steampowered.com/", "https://steamcommunity.com/", "https://t.me/", "https://github.com/", "https://tronscan.org/", "steam://url/SteamIDPage/", "steam://store/"} {
			if strings.HasPrefix(a.URL, p) {
				s.P.OpenURL(a.URL)
			}
		}
		return nil, nil
	case "quit":
		go s.OnQuit()
		return nil, nil
	}

	// account calls
	b := h.Bot(a.ID)
	if b == nil {
		return nil, errNoAccount
	}
	switch name {
	case "loginSaved":
		return b.LoginSaved(), nil
	case "login":
		if a.Password == "" {
			return nil, errors.New("err.loginEmpty")
		}
		b.SetRemember(a.Remember)
		b.LoginPassword(a.Password)
	case "guard":
		b.Guard(a.Code)
	case "logout":
		b.Logout(a.Forget)
	case "reloadGames":
		go b.LoadGames()
	case "update":
		return nil, b.Update(a.Patch)
	case "run":
		return nil, b.SetRun(a.Module, a.On)
	case "addGame":
		return nil, b.AddManualGame(a.AppID, a.Name)
	case "removeGame":
		b.RemoveManualGame(a.AppID)
	case "achGames":
		return b.AchievementGames(), nil
	case "achList":
		return b.Achievements(a.AppID, a.Lang)
	case "achSet":
		return nil, b.SetAchievements(a.AppID, a.IDs, a.Unlock, a.Human)
	case "achCancel":
		b.CancelAchievements()
	case "keys":
		return b.QueueKeys(a.Text), nil
	case "revealKey":
		return b.RevealKey(a.Code, a.At), nil
	case "refreshStats":
		b.RefreshStats()
	case "maFile":
		raw, err := base64.StdEncoding.DecodeString(a.File)
		if err != nil {
			return nil, err
		}
		f, err := steam.ParseMaFile(raw)
		if err != nil {
			return nil, err
		}
		if f.Account != "" && !strings.EqualFold(f.Account, b.Account()) {
			return nil, errors.New("err.maFileAccount")
		}
		b.SetMaFile(f)
	case "removeMaFile":
		b.SetMaFile(steam.MaFile{})
	case "guardCode":
		return b.GuardCode(), nil
	case "inventory":
		return b.Inventory(a.AppID, a.Context, a.Refresh)
	case "itemPrice":
		return b.ItemPrice(a.AppID, a.Hash, a.Book)
	case "sell":
		return nil, b.Sell(a.Orders)
	case "sellDuplicates":
		return b.SellDuplicates(a.AppID, a.Context, a.Mode, a.Undercut, a.Min, a.Max, a.Kinds)
	case "listings":
		return b.Listings()
	case "removeListings":
		return nil, b.RemoveListings(a.IDs)
	case "marketSearch":
		items, total, err := b.MarketSearch(a.Query, a.AppID, a.Start)
		return map[string]any{"items": items, "total": total}, err
	case "confirmMarket":
		return b.ConfirmMarket()
	case "stopMarket":
		b.StopMarketJob()
	default:
		return nil, fmt.Errorf("unknown call %q", name)
	}
	return nil, nil
}
