// Package bot runs one Steam account: connection, login, and all of its modules.
package bot

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"iddler/internal/steam"
)

// Host is what a bot needs from the app around it.
type Host interface {
	Log(botID, kind, key string, args ...string)
	Changed(botID string)
	GamesChanged(botID string)
	Notify(category, key string, args ...string)
	Secret(key string) string
	SetSecret(key, value string) error
	DeleteSecret(key string)
}

const machineName = "Iddler"

type Bot struct {
	ID   string
	dir  string
	host Host

	mu   sync.Mutex
	cfg  Config
	data Data

	token string
	totp  string

	cm       *steam.Client
	gen      int
	manual   bool
	retries  int
	relog    *time.Timer
	logonTO  *time.Timer
	hbStop   chan struct{}
	guardCh  chan string
	cancelCh chan struct{}

	status      string // offline | connecting | guard | online
	guardKind   string // email | code | confirm
	guardDomain string
	guardWrong  bool
	steamID     uint64
	name        string
	avatar      string
	currency    int
	friends     map[uint64]string

	games        []steam.OwnedGame
	gamesLoading bool
	accrued      map[uint32]float64 // seconds idled per app since the games list was loaded

	playing    []uint32
	sent       bool // playing list was sent on this connection
	sentCustom string
	customNow  string
	blocked    bool
	blockedApp uint32
	sessStart  time.Time
	sessDur    time.Duration
	slideIdx   int
	slideAt    time.Time
	nickSet    string
	lastTick   time.Time

	cards        []steam.BadgeGame
	cardsSkipped []steam.BadgeGame // have drops but excluded by the black or white list
	craftable    []steam.BadgeGame
	cardsAt      time.Time
	cardsBusy    bool

	ach    map[uint32]*steam.GameStats
	achJob *achJob

	keyQueue []string
	keyBusy  bool
	keyPause time.Time
	autoSent map[uint64]time.Time
	level    steam.Level
	inv      invState

	// inventory manager
	invList   []ItemGroup
	invKey    string
	invAt     time.Time
	prices    map[string]priceEntry
	mjob      *marketJob
	identity  string // from the .maFile: signs mobile confirmations
	deviceID  string
	web       *steam.Web
	webAt     time.Time
	lastFree  time.Time
	lastTrade time.Time
	lastWish  time.Time

	stop chan struct{}
}

func New(id, dir, account string, host Host) *Bot {
	b := &Bot{ID: id, dir: dir, host: host, status: "offline", stop: make(chan struct{}),
		friends: map[uint64]string{}, accrued: map[uint32]float64{}, ach: map[uint32]*steam.GameStats{}, autoSent: map[uint64]time.Time{}}
	b.cfg.Account = account
	os.MkdirAll(dir, 0o700)
	b.load()
	b.token = host.Secret("token:" + b.cfg.Account)
	b.totp = host.Secret("totp:" + b.cfg.Account)
	b.identity, b.deviceID, _ = strings.Cut(host.Secret("conf:"+b.cfg.Account), "|")
	go b.worker()
	return b
}

func (b *Bot) Account() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Account
}

func (b *Bot) HasToken() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.token != ""
}

func (b *Bot) log(kind, key string, args ...string) { b.host.Log(b.ID, kind, key, args...) }
func (b *Bot) changed()                             { b.host.Changed(b.ID) }

// ---------------------------------------------------------------- connection

func (b *Bot) closeConn() {
	b.gen++
	for _, t := range []*time.Timer{b.relog, b.logonTO} {
		if t != nil {
			t.Stop()
		}
	}
	b.relog, b.logonTO = nil, nil
	if b.hbStop != nil {
		close(b.hbStop)
		b.hbStop = nil
	}
	if b.cancelCh != nil {
		close(b.cancelCh)
		b.cancelCh = nil
	}
	if b.cm != nil {
		go b.cm.Close()
		b.cm = nil
	}
	b.web = nil
	b.pauseSession()
	b.playing, b.sent = nil, false
}

func (b *Bot) callbacks(gen int) (func(*steam.Packet), func(error)) {
	onPacket := func(p *steam.Packet) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if gen == b.gen {
			b.onPacket(p)
		}
	}
	onClose := func(err error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if gen != b.gen || b.manual {
			return
		}
		b.log("warn", "log.connLost", errKey(err))
		b.cm = nil
		b.closeConn()
		b.scheduleReconnect(0)
	}
	return onPacket, onClose
}

func (b *Bot) scheduleReconnect(delay time.Duration) {
	if b.token == "" {
		b.status = "offline"
		b.changed()
		return
	}
	if delay == 0 {
		delay = min(time.Duration(5<<min(b.retries, 5))*time.Second, 2*time.Minute)
	}
	b.retries++
	b.status = "connecting"
	b.log("info", "log.reconnectIn", fmtDelay(delay))
	gen := b.gen
	b.relog = time.AfterFunc(delay, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if gen == b.gen && !b.manual {
			b.tokenLogin()
		}
	})
	b.changed()
}

func fmtDelay(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()+0.5))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// tokenLogin connects with the saved refresh token. Caller holds b.mu.
func (b *Bot) tokenLogin() {
	b.closeConn()
	b.manual = false
	if b.token == "" {
		b.status = "offline"
		b.changed()
		return
	}
	if ti, err := steam.ParseToken(b.token); err == nil && ti.Expires > 0 && time.Unix(ti.Expires, 0).Before(time.Now()) {
		b.log("warn", "log.sessionExpired")
		b.forgetToken()
		b.status = "offline"
		b.changed()
		return
	}
	b.status = "connecting"
	b.changed()
	gen := b.gen
	onPacket, onClose := b.callbacks(gen)
	go func() {
		cm, err := steam.Connect(onPacket, onClose)
		b.mu.Lock()
		defer b.mu.Unlock()
		if gen != b.gen {
			if cm != nil {
				cm.Close()
			}
			return
		}
		if err != nil {
			b.log("err", "log.error", errKey(err))
			b.scheduleReconnect(0)
			return
		}
		b.cm = cm
		b.sendLogon()
	}()
}

func (b *Bot) sendLogon() {
	if err := b.cm.Logon(b.token, b.cfg.Account, machineName, b.cfg.LogonID); err != nil {
		b.log("err", "log.error", errKey(err))
		b.closeConn()
		b.scheduleReconnect(0)
		return
	}
	gen := b.gen
	b.logonTO = time.AfterFunc(20*time.Second, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if gen == b.gen && b.status != "online" {
			b.log("warn", "log.logonTimeout")
			b.closeConn()
			b.scheduleReconnect(0)
		}
	})
}

func (b *Bot) forgetToken() {
	b.token = ""
	b.host.DeleteSecret("token:" + b.cfg.Account)
}

// ---------------------------------------------------------------- incoming

func (b *Bot) onPacket(p *steam.Packet) {
	m := p.Fields()
	switch p.EMsg {
	case steam.EMsgClientLogOnResponse:
		b.onLogon(int(m.I(1)), int(m.I(3)))
	case steam.EMsgClientLoggedOff:
		res := int(m.I(1))
		b.log("warn", "log.loggedOff", steam.ResultName(res))
		b.closeConn()
		if res == steam.ResLoggedInElsewhere || res == steam.ResLogonSessionReplaced {
			b.scheduleReconnect(2 * time.Minute)
		} else {
			b.scheduleReconnect(0)
		}
	case steam.EMsgClientAccountInfo:
		if n := m.S(1); n != "" {
			b.name = n
			b.changed()
		}
	case steam.EMsgClientWalletInfoUpdate:
		b.currency = int(m.I(3))
	case steam.EMsgClientFriendsList:
		var ids []uint64
		for _, f := range m.Msgs(2) {
			// relationship 3 is "friend" for people and "member" for groups: keep people only
			if id := f.U(1); f.U(2) == 3 && id>>52&0xF == 1 {
				if _, ok := b.friends[id]; !ok {
					b.friends[id] = ""
				}
				ids = append(ids, id)
			} else {
				delete(b.friends, f.U(1))
			}
		}
		if len(ids) > 0 && b.cm != nil {
			for i := 0; i < len(ids); i += 100 {
				b.cm.RequestPersonas(ids[i:min(i+100, len(ids))]...)
			}
		}
	case steam.EMsgClientPersonaState:
		for _, f := range m.Msgs(2) {
			id := f.U(1)
			if id == b.steamID {
				if n := f.S(15); n != "" {
					b.name = n
				}
				if h := f.Raw(31); len(h) > 0 {
					hs := hex.EncodeToString(h)
					if strings.Trim(hs, "0") == "" {
						hs = "fef49e7fa7e1997310d705b2a6158ff8dc1cdfeb"
					}
					b.avatar = "https://avatars.steamstatic.com/" + hs + "_full.jpg"
				}
				b.changed()
			} else if _, ok := b.friends[id]; ok {
				if n := f.S(15); n != "" {
					b.friends[id] = n
				}
			}
		}
	case steam.EMsgClientPlayingSessionState:
		b.onPlayingState(m.B(2), uint32(m.U(3)))
	case steam.EMsgServiceMethod:
		if p.Header.S(12) == "FriendMessagesClient.IncomingMessage#1" {
			b.onChat(m.U(1), int(m.I(2)), m.S(4))
		}
	}
}

func (b *Bot) onLogon(res, heartbeat int) {
	if b.logonTO != nil {
		b.logonTO.Stop()
		b.logonTO = nil
	}
	switch res {
	case steam.ResOK:
	case steam.ResInvalidPassword, steam.ResAccessDenied, steam.ResExpired, steam.ResAccountDisabled:
		b.log("err", "log.sessionRejected", steam.ResultName(res))
		b.forgetToken()
		b.closeConn()
		b.status = "offline"
		b.changed()
		return
	case steam.ResRateLimitExceeded, steam.ResLoginDeniedThrottle:
		b.log("warn", "log.rateLimited")
		b.closeConn()
		b.scheduleReconnect(30 * time.Minute)
		return
	default:
		b.log("warn", "log.logonFailed", steam.ResultName(res))
		b.closeConn()
		b.scheduleReconnect(0)
		return
	}

	b.retries = 0
	b.steamID = b.cm.SteamID()
	b.status = "online"
	b.guardKind, b.guardDomain, b.guardWrong = "", "", false
	b.log("ok", "log.loggedIn", fmt.Sprint(b.steamID))

	if heartbeat <= 0 {
		heartbeat = 9
	}
	b.hbStop = make(chan struct{})
	go func(cm *steam.Client, stop chan struct{}, every time.Duration) {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				cm.Heartbeat()
			}
		}
	}(b.cm, b.hbStop, time.Duration(heartbeat)*time.Second)

	b.nickSet = ""
	b.applyPersona()
	b.cm.RequestPersonas(b.steamID)
	b.applyPlaying()
	b.changed()
	go b.LoadGames()
	if b.cfg.CardsOn {
		b.cardsAt = time.Time{}
	}
}

func personaState(p string) uint32 {
	switch p {
	case "away":
		return steam.PersonaAway
	case "invisible":
		return steam.PersonaInvisible
	case "offline":
		return steam.PersonaOffline
	}
	return steam.PersonaOnline
}

func (b *Bot) applyPersona() {
	if b.cm == nil || b.status != "online" {
		return
	}
	name := ""
	if b.cfg.Modules[ModProfile] && b.cfg.Profile.NickOn {
		name = b.desiredNick(time.Now())
		if name == b.nickSet {
			name = ""
		} else if name != "" {
			b.nickSet = name
		}
	}
	b.cm.SetPersona(personaState(b.cfg.Persona), name)
}

// ---------------------------------------------------------------- password login

func (b *Bot) LoginPassword(password string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeConn()
	b.manual = false
	b.status = "connecting"
	b.guardWrong = false
	b.guardCh = make(chan string, 1)
	b.cancelCh = make(chan struct{})
	gen, guardCh, cancelCh, account, totp := b.gen, b.guardCh, b.cancelCh, b.cfg.Account, b.totp
	onPacket, onClose := b.callbacks(gen)
	b.log("info", "log.signingIn", account)
	b.changed()
	go b.passwordFlow(gen, account, password, totp, guardCh, cancelCh, onPacket, onClose)
}

func (b *Bot) passwordFlow(gen int, account, password, totp string, guardCh chan string, cancelCh chan struct{},
	onPacket func(*steam.Packet), onClose func(error)) {

	fail := func(key string, args ...string) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if gen != b.gen {
			return
		}
		b.log("err", key, args...)
		b.closeConn()
		b.status, b.guardKind = "offline", ""
		b.changed()
	}
	cm, err := steam.Connect(onPacket, onClose)
	if err != nil {
		fail("log.error", errKey(err))
		return
	}
	b.mu.Lock()
	if gen != b.gen {
		b.mu.Unlock()
		cm.Close()
		return
	}
	b.cm = cm
	b.mu.Unlock()

	cm.Hello()
	auth, res, err := cm.BeginAuth(account, password, machineName)
	switch {
	case err != nil:
		fail("log.error", errKey(err))
		return
	case res == steam.ResInvalidPassword:
		fail("log.badPassword")
		return
	case res == steam.ResRateLimitExceeded || res == steam.ResLoginDeniedThrottle:
		fail("log.rateLimited")
		return
	case res != steam.ResOK:
		fail("log.logonFailed", steam.ResultName(res))
		return
	}

	kind, domain, codeType := "", "", 0
	for _, c := range auth.Confirmations {
		switch c.Type {
		case steam.GuardDeviceCode:
			kind, codeType = "code", steam.GuardDeviceCode
		case steam.GuardEmailCode:
			if kind != "code" {
				kind, codeType, domain = "email", steam.GuardEmailCode, c.Message
			}
		case steam.GuardDeviceConfirmation, steam.GuardEmailConfirmation:
			if kind == "" {
				kind = "confirm"
			}
		}
	}
	if codeType == steam.GuardDeviceCode && totp != "" {
		if code, err := steam.GuardCode(totp, time.Now()); err == nil {
			guardCh <- code
			b.log("info", "log.autoGuard")
			kind = ""
		}
	}
	if kind != "" {
		b.mu.Lock()
		if gen == b.gen {
			b.status, b.guardKind, b.guardDomain, b.guardWrong = "guard", kind, domain, false
			b.changed()
		}
		b.mu.Unlock()
	}

	interval := max(time.Duration(float64(auth.Interval)*float64(time.Second)), 2*time.Second)
	poll := time.NewTicker(interval)
	defer poll.Stop()
	deadline := time.After(10 * time.Minute)

	var token string
	for token == "" {
		select {
		case <-cancelCh:
			return
		case <-deadline:
			fail("log.guardTimeout")
			return
		case code := <-guardCh:
			if codeType == 0 {
				continue
			}
			res, err := cm.SubmitGuardCode(auth, code, codeType)
			if err != nil {
				fail("log.error", errKey(err))
				return
			}
			if res == steam.ResInvalidAuthCode || res == steam.ResTwoFactorMismatch || res == steam.ResExpired {
				b.mu.Lock()
				if gen == b.gen {
					b.status, b.guardWrong = "guard", true
					if b.guardKind == "" {
						b.guardKind = "code"
					}
					b.log("warn", "log.badCode")
					b.changed()
				}
				b.mu.Unlock()
				continue
			}
			if res != steam.ResOK && res != steam.ResDuplicateRequest {
				fail("log.logonFailed", steam.ResultName(res))
				return
			}
			if t, res, err := cm.PollAuth(auth); err == nil && res == steam.ResOK {
				token = t
			}
		case <-poll.C:
			t, res, err := cm.PollAuth(auth)
			if err != nil {
				fail("log.error", errKey(err))
				return
			}
			if res != steam.ResOK {
				fail("log.logonFailed", steam.ResultName(res))
				return
			}
			token = t
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.gen {
		return
	}
	b.token = token
	if b.cfg.Remember {
		if err := b.host.SetSecret("token:"+account, token); err != nil {
			b.log("warn", "log.keychainFail", err.Error())
		} else {
			b.log("ok", "log.sessionSaved")
		}
	}
	b.status, b.guardKind = "connecting", ""
	b.changed()
	b.sendLogon()
}

// ---------------------------------------------------------------- public controls

// SetToken stores a refresh token obtained elsewhere (QR sign-in) and signs in with it.
func (b *Bot) SetToken(token string) {
	b.mu.Lock()
	b.token = token
	b.host.SetSecret("token:"+b.cfg.Account, token)
	b.cfg.Remember = true
	b.saveConfig()
	b.mu.Unlock()
	b.LoginSaved()
}

func (b *Bot) LoginSaved() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.token == "" {
		return false
	}
	b.retries = 0
	b.tokenLogin()
	return true
}

func (b *Bot) Guard(code string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.guardCh == nil {
		return
	}
	b.status, b.guardWrong = "connecting", false
	b.changed()
	select {
	case b.guardCh <- strings.ToUpper(strings.TrimSpace(code)):
	default:
	}
}

func (b *Bot) Logout(forget bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.manual = true
	if b.cm != nil && b.status == "online" {
		b.cm.GamesPlayed("", nil)
		b.cm.LogOff()
	}
	b.closeConn()
	if forget {
		b.forgetToken()
	}
	b.status, b.guardKind, b.blocked = "offline", "", false
	b.playing = nil
	b.log("info", "log.loggedOut")
	b.changed()
}

// Shutdown stops everything and logs off; used on quit and when the account is removed.
func (b *Bot) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
	b.manual = true
	if b.cm != nil && b.status == "online" {
		b.cm.GamesPlayed("", nil)
		b.cm.LogOff()
	}
	b.closeConn()
	b.saveData()
}

// SetMaFile stores the secrets of a .maFile (an empty MaFile removes them).
func (b *Bot) SetMaFile(f steam.MaFile) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.totp, b.identity, b.deviceID = f.SharedSecret, f.IdentitySecret, f.DeviceID
	if f.SharedSecret == "" {
		b.host.DeleteSecret("totp:" + b.cfg.Account)
		b.host.DeleteSecret("conf:" + b.cfg.Account)
		b.cfg.Guard = "manual"
	} else {
		b.host.SetSecret("totp:"+b.cfg.Account, f.SharedSecret)
		if f.IdentitySecret != "" && f.DeviceID != "" {
			b.host.SetSecret("conf:"+b.cfg.Account, f.IdentitySecret+"|"+f.DeviceID)
		}
		b.cfg.Guard = "mafile"
	}
	b.saveConfig()
	b.changed()
}

func (b *Bot) GuardCode() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.totp == "" {
		return ""
	}
	c, _ := steam.GuardCode(b.totp, time.Now())
	return c
}

// conn returns the live client, or nil when offline.
func (b *Bot) conn() *steam.Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status != "online" {
		return nil
	}
	return b.cm
}
