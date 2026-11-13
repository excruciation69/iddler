package hub

// Telegram bot: control idling from the phone and receive notifications.
// It answers only the one chat that was bound with a code shown in the app.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"iddler/internal/bot"
)

var TelegramAPI = "https://api.telegram.org"

type telegram struct {
	h      *Hub
	mu     sync.Mutex
	stop   chan struct{}
	client *http.Client
}

func newTelegram(h *Hub) *telegram {
	return &telegram{h: h, client: &http.Client{Timeout: 70 * time.Second}}
}

func (t *telegram) config() TelegramConfig {
	t.h.mu.Lock()
	defer t.h.mu.Unlock()
	return t.h.cfg.Telegram
}

func (t *telegram) start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
	}
	if t.config().Token == "" {
		return
	}
	t.stop = make(chan struct{})
	go t.poll(t.stop)
}

func (t *telegram) call(method string, params url.Values, out any) error {
	cfg := t.config()
	if cfg.Token == "" {
		return errors.New("no token")
	}
	resp, err := t.client.PostForm(fmt.Sprintf("%s/bot%s/%s", TelegramAPI, cfg.Token, method), params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if !r.OK {
		return errors.New(r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func (t *telegram) send(chatID int64, text string) {
	kb := `{"keyboard":[["▶ Start","■ Stop"],["📊 Status","🃏 Cards"]],"resize_keyboard":true}`
	t.call("sendMessage", url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}, "text": {text},
		"parse_mode": {"HTML"}, "reply_markup": {kb}}, nil)
}

func (t *telegram) notify(category, text string) {
	cfg := t.config()
	if cfg.Token == "" || cfg.ChatID == 0 || !cfg.Notify[category] {
		return
	}
	go t.send(cfg.ChatID, text)
}

func (t *telegram) poll(stop chan struct{}) {
	offset := int64(0)
	for {
		select {
		case <-stop:
			return
		default:
		}
		var updates []struct {
			ID      int64 `json:"update_id"`
			Message *struct {
				Text string `json:"text"`
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				From struct {
					Username string `json:"username"`
					First    string `json:"first_name"`
				} `json:"from"`
			} `json:"message"`
		}
		err := t.call("getUpdates", url.Values{"offset": {strconv.FormatInt(offset, 10)}, "timeout": {"50"}}, &updates)
		if err != nil {
			select {
			case <-stop:
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.ID + 1
			if u.Message == nil {
				continue
			}
			who := u.Message.From.Username
			if who == "" {
				who = u.Message.From.First
			}
			t.handle(u.Message.Chat.ID, who, strings.TrimSpace(u.Message.Text))
		}
	}
}

func (t *telegram) handle(chatID int64, who, text string) {
	h := t.h
	cfg := t.config()
	if cfg.ChatID == 0 {
		// not bound yet: accept only "/start <code>"
		h.mu.Lock()
		code := h.bindCode
		h.mu.Unlock()
		if code != "" && (text == "/start "+code || text == code) {
			h.mu.Lock()
			h.cfg.Telegram.ChatID, h.cfg.Telegram.User, h.bindCode = chatID, who, ""
			h.hubDirty = true
			h.save()
			h.mu.Unlock()
			t.send(chatID, h.T("tg.bound"))
			h.Log("", "ok", "log.tgBound", who)
		}
		return
	}
	if chatID != cfg.ChatID {
		return
	}
	cmd := strings.ToLower(strings.Fields(text + " ")[0])
	switch {
	case cmd == "/status" || strings.Contains(text, "Status"):
		t.send(chatID, t.status())
	case cmd == "/start" || strings.Contains(text, "Start"):
		h.StartAll()
		t.send(chatID, h.T("tg.started"))
	case cmd == "/stop" || strings.Contains(text, "Stop"):
		h.StopAll()
		t.send(chatID, h.T("tg.stopped"))
	case cmd == "/cards" || strings.Contains(text, "Cards"):
		t.send(chatID, t.cards())
	case len(bot.FindKeys(text)) > 0:
		b := t.keysBot()
		if b == nil {
			t.send(chatID, h.T("tg.noAccount"))
			return
		}
		n := b.QueueKeys(text)
		t.send(chatID, h.T("tg.keysQueued", itoa(n), b.Account()))
	default:
		t.send(chatID, h.T("tg.help"))
	}
}

func (t *telegram) keysBot() *bot.Bot {
	bots := t.h.Bots()
	for _, b := range bots {
		if s := b.Summary(); s.Modules[bot.ModKeys] && s.Status == "online" {
			return b
		}
	}
	if len(bots) > 0 {
		return bots[0]
	}
	return nil
}

func (t *telegram) status() string {
	h := t.h
	var lines []string
	for _, b := range h.Bots() {
		s := b.Summary()
		state := h.T("tg.st." + s.Status)
		switch {
		case s.Blocked:
			state = h.T("tg.st.paused")
		case s.HoursOn || s.CardsOn:
			state = h.T("tg.st.idling", itoa(s.Playing), fmtDur(s.SessionMs))
		}
		lines = append(lines, fmt.Sprintf("<b>%s</b>: %s · %s", esc(s.Name), state, h.T("tg.today", fmtHours(s.TodaySec))))
	}
	if len(lines) == 0 {
		return h.T("tg.noAccount")
	}
	return strings.Join(lines, "\n")
}

func (t *telegram) cards() string {
	h := t.h
	var lines []string
	for _, b := range h.Bots() {
		s := b.Summary()
		lines = append(lines, fmt.Sprintf("<b>%s</b>: %s", esc(s.Name), h.T("tg.cardsLeft", itoa(s.CardsLeft))))
	}
	return strings.Join(lines, "\n")
}

// ---- setup from the UI

func (h *Hub) TelegramConnect(token string) (string, error) {
	token = strings.TrimSpace(token)
	h.mu.Lock()
	h.cfg.Telegram.Token, h.cfg.Telegram.ChatID, h.cfg.Telegram.User = token, 0, ""
	h.mu.Unlock()
	var me struct {
		Username string `json:"username"`
	}
	if err := h.tg.call("getMe", url.Values{}, &me); err != nil {
		h.mu.Lock()
		h.cfg.Telegram.Token = ""
		h.mu.Unlock()
		return "", errors.New("err.tgToken")
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(900000))
	h.mu.Lock()
	h.cfg.Telegram.Bot = me.Username
	h.bindCode = strconv.FormatInt(n.Int64()+100000, 10)
	code := h.bindCode
	h.hubDirty = true
	h.save()
	h.mu.Unlock()
	h.tg.start()
	return code, nil
}

func (h *Hub) TelegramDisconnect() {
	h.mu.Lock()
	h.cfg.Telegram = TelegramConfig{Notify: h.cfg.Telegram.Notify}
	h.bindCode = ""
	h.hubDirty = true
	h.save()
	h.mu.Unlock()
	h.tg.start()
}

func (h *Hub) TelegramCode() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bindCode
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func fmtDur(ms int64) string {
	m := ms / 60000
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

func fmtHours(sec int) string { return strconv.FormatFloat(float64(sec)/3600, 'f', 1, 64) }

func itoa(n int) string { return strconv.Itoa(n) }
