package hub

// Sign-in with a QR code: the interface shows the code, the Steam mobile app approves it,
// and the account joins the hub with the session it gets.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"steamidler/internal/bot"
	"steamidler/internal/steam"
)

// QRState is what the interface polls while the QR code is on screen.
type QRState struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Account string `json:"account"` // the account id once signed in
	Error   string `json:"error"`
	Done    bool   `json:"done"`
}

type qrJob struct {
	state QRState
	stop  chan struct{}
}

// StartQR begins a QR sign-in (and cancels a previous one).
func (h *Hub) StartQR() (QRState, error) {
	h.CancelQR()
	cm, err := steam.Connect(func(*steam.Packet) {}, func(error) {})
	if err != nil {
		return QRState{}, errors.New("err.network")
	}
	cm.Hello()
	host, _ := os.Hostname()
	s, res, err := cm.BeginQR("SteamIdler · " + host)
	if err != nil || res != steam.ResOK {
		cm.Close()
		if err == nil {
			err = errors.New(steam.ResultName(res))
		}
		return QRState{}, err
	}
	id := make([]byte, 6)
	rand.Read(id)
	job := &qrJob{state: QRState{ID: hex.EncodeToString(id), URL: s.URL}, stop: make(chan struct{})}
	h.mu.Lock()
	h.qr = job
	h.mu.Unlock()
	go h.pollQR(cm, s, job)
	return job.state, nil
}

func (h *Hub) pollQR(cm *steam.Client, s *steam.QRSession, job *qrJob) {
	defer cm.Close()
	tick := time.NewTicker(time.Duration(float64(s.Interval) * float64(time.Second)))
	defer tick.Stop()
	deadline := time.After(3 * time.Minute)
	set := func(f func(*QRState)) {
		h.mu.Lock()
		f(&job.state)
		h.mu.Unlock()
	}
	for {
		select {
		case <-job.stop:
			return
		case <-deadline:
			set(func(st *QRState) { st.Error, st.Done = "err.qrExpired", true })
			return
		case <-tick.C:
		}
		r, res, err := cm.PollQR(s)
		if err != nil || res != steam.ResOK {
			set(func(st *QRState) { st.Error, st.Done = "err.qrExpired", true })
			return
		}
		if r == nil {
			set(func(st *QRState) { st.URL = s.URL })
			continue
		}
		accID := h.adoptSession(r.Account, r.RefreshToken)
		set(func(st *QRState) { st.Account, st.Done = accID, true })
		return
	}
}

// adoptSession adds (or refreshes) an account from a refresh token and signs it in.
func (h *Hub) adoptSession(account, token string) string {
	id := accountID(account)
	h.mu.Lock()
	b := h.bots[id]
	if b == nil {
		b = bot.New(id, filepath.Join(h.dir, "accounts", id), account, h)
		h.bots[id] = b
		h.cfg.Accounts = append(h.cfg.Accounts, account)
		h.save()
	}
	h.hubDirty = true
	h.mu.Unlock()
	b.SetToken(token)
	return id
}

// QR returns the state of the current QR sign-in.
func (h *Hub) QR() QRState {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.qr == nil {
		return QRState{Done: true, Error: "err.qrExpired"}
	}
	return h.qr.state
}

// CancelQR stops waiting for the phone.
func (h *Hub) CancelQR() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.qr != nil {
		select {
		case <-h.qr.stop:
		default:
			close(h.qr.stop)
		}
		h.qr = nil
	}
}
