// Package steam is a small Steam client: CM connection over WebSocket, login, and the
// handful of messages SteamIdler needs. Message and field numbers follow SteamDatabase/Protobufs.
package steam

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"
)

// EMsg values
const (
	EMsgMulti                                = 1
	EMsgServiceMethod                        = 146
	EMsgServiceMethodResponse                = 147
	EMsgServiceMethodCallFromClient          = 151
	EMsgClientHeartBeat                      = 703
	EMsgClientLogOff                         = 706
	EMsgClientChangeStatus                   = 716
	EMsgClientRegisterKey                    = 743
	EMsgClientLogOnResponse                  = 751
	EMsgClientLoggedOff                      = 757
	EMsgClientPurchaseResponse               = 763
	EMsgClientPersonaState                   = 766
	EMsgClientFriendsList                    = 767
	EMsgClientAccountInfo                    = 768
	EMsgClientRequestFriendData              = 815
	EMsgClientGetUserStats                   = 818
	EMsgClientGetUserStatsResponse           = 819
	EMsgClientStoreUserStatsResponse         = 821
	EMsgClientGamesPlayedWithDataBlob        = 5410
	EMsgClientStoreUserStats2                = 5466
	EMsgClientLogon                          = 5514
	EMsgClientWalletInfoUpdate               = 5528
	EMsgClientRequestFreeLicense             = 5572
	EMsgClientRequestFreeLicenseResponse     = 5573
	EMsgClientPlayingSessionState            = 9600
	EMsgClientKickPlayingSession             = 9601
	EMsgServiceMethodCallFromClientNonAuthed = 9804
	EMsgClientHello                          = 9805
)

const (
	protoMask       = 0x80000000
	jobNone         = ^uint64(0)
	protocolVersion = 65580
	osWin11         = 20
	// NonSteamGameID is what the official client sends for "non-Steam game" entries.
	NonSteamGameID = 15190414816125648896
)

// EResult values used by SteamIdler
const (
	ResOK                   = 1
	ResFail                 = 2
	ResInvalidPassword      = 5
	ResLoggedInElsewhere    = 6
	ResFileNotFound         = 9
	ResBusy                 = 10
	ResAccessDenied         = 15
	ResServiceUnavailable   = 20
	ResExpired              = 27
	ResDuplicateRequest     = 29
	ResLogonSessionReplaced = 34
	ResAccountDisabled      = 43
	ResTryAnotherCM         = 48
	ResInvalidAuthCode      = 65
	ResRateLimitExceeded    = 84
	ResLoginDeniedThrottle  = 87
	ResTwoFactorMismatch    = 88
)

var resultNames = map[int]string{
	1: "OK", 2: "Fail", 3: "NoConnection", 5: "InvalidPassword", 6: "LoggedInElsewhere", 8: "InvalidParam",
	9: "FileNotFound", 10: "Busy", 11: "InvalidState", 15: "AccessDenied", 16: "Timeout", 20: "ServiceUnavailable",
	27: "Expired", 29: "DuplicateRequest", 34: "LogonSessionReplaced", 43: "AccountDisabled", 48: "TryAnotherCM",
	65: "InvalidLoginAuthCode", 84: "RateLimitExceeded", 87: "AccountLoginDeniedThrottle", 88: "TwoFactorCodeMismatch",
}

func ResultName(r int) string {
	if n, ok := resultNames[r]; ok {
		return n
	}
	return fmt.Sprintf("EResult %d", r)
}

type Packet struct {
	EMsg   uint32
	Header Fields
	Body   []byte
}

func (p *Packet) Fields() Fields {
	f, _ := Parse(p.Body)
	return f
}

// Client is one connection to a Steam CM server.
type Client struct {
	ws        *wsConn
	mu        sync.Mutex
	steamID   uint64
	sessionID int32
	jobID     uint64
	jobs      map[uint64]chan *Packet
	waiters   map[uint32][]chan *Packet // replies that may arrive without a job id
	closed    bool
	onPacket  func(*Packet)
	onClose   func(error)
}

// Endpoints can be overridden in tests.
var (
	DirectoryURL = "https://api.steampowered.com/ISteamDirectory/GetCMListForConnect/v1/?cellid=0&cmtype=websockets&format=json"
	CMPath       = "/cmsocket/"
	HTTP         = &http.Client{Timeout: 25 * time.Second}
)

func cmServers() ([]string, error) {
	resp, err := HTTP.Get(DirectoryURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Response struct {
			Serverlist []struct {
				Endpoint string  `json:"endpoint"`
				Type     string  `json:"type"`
				Realm    string  `json:"realm"`
				Load     float64 `json:"wtd_load"`
			} `json:"serverlist"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	list := out.Response.Serverlist[:0]
	for _, s := range out.Response.Serverlist {
		if s.Type == "websockets" && (s.Realm == "" || s.Realm == "steamglobal") {
			list = append(list, s)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Load < list[j].Load })
	if len(list) > 6 {
		list = list[:6]
	}
	rand.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	var eps []string
	for _, s := range list {
		eps = append(eps, s.Endpoint)
	}
	if len(eps) == 0 {
		return nil, errors.New("empty server list")
	}
	return eps, nil
}

// Connect opens a CM connection. onPacket gets every message that isn't a job reply.
func Connect(onPacket func(*Packet), onClose func(error)) (*Client, error) {
	servers, err := cmServers()
	if err != nil {
		return nil, fmt.Errorf("steam directory: %w", err)
	}
	var lastErr error
	for i, s := range servers {
		if i == 3 {
			break
		}
		ws, err := wsDial(s, CMPath, 15*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		c := &Client{ws: ws, jobs: map[uint64]chan *Packet{}, waiters: map[uint32][]chan *Packet{}, onPacket: onPacket, onClose: onClose}
		go c.readLoop()
		return c, nil
	}
	return nil, fmt.Errorf("steam servers unreachable: %v", lastErr)
}

func (c *Client) SteamID() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.steamID
}

func (c *Client) setSteamID(id uint64) {
	c.mu.Lock()
	c.steamID = id
	c.mu.Unlock()
}

func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.dropJobs()
	c.mu.Unlock()
	c.ws.Close()
}

func (c *Client) dropJobs() {
	done := map[chan *Packet]bool{}
	closeOnce := func(ch chan *Packet) {
		if !done[ch] {
			done[ch] = true
			close(ch)
		}
	}
	for id, ch := range c.jobs {
		closeOnce(ch)
		delete(c.jobs, id)
	}
	for em, ws := range c.waiters {
		for _, ch := range ws {
			closeOnce(ch)
		}
		delete(c.waiters, em)
	}
}

func (c *Client) readLoop() {
	for {
		data, err := c.ws.Read()
		if err != nil {
			c.mu.Lock()
			wasClosed := c.closed
			c.closed = true
			c.dropJobs()
			c.mu.Unlock()
			c.ws.c.Close()
			if !wasClosed && c.onClose != nil {
				c.onClose(err)
			}
			return
		}
		c.handle(data)
	}
}

func (c *Client) handle(data []byte) {
	if len(data) < 8 || binary.LittleEndian.Uint32(data)&protoMask == 0 {
		return // only protobuf messages are used
	}
	emsg := binary.LittleEndian.Uint32(data) &^ protoMask
	hlen := int(binary.LittleEndian.Uint32(data[4:]))
	if hlen > len(data)-8 {
		return
	}
	hdr, _ := Parse(data[8 : 8+hlen])
	body := data[8+hlen:]

	if emsg == EMsgMulti {
		c.handleMulti(body)
		return
	}
	if sid, sess := hdr.U(1), int32(hdr.U(2)); sid != 0 && sess != 0 {
		c.mu.Lock()
		c.steamID, c.sessionID = sid, sess
		c.mu.Unlock()
	}
	p := &Packet{EMsg: emsg, Header: hdr, Body: body}
	// replies are delivered under the lock with a non-blocking send, so Close can't race them
	c.mu.Lock()
	var ch chan *Packet
	if target, ok := hdr.last(11); ok && target.num != jobNone {
		ch = c.jobs[target.num]
		delete(c.jobs, target.num)
	}
	if ws := c.waiters[emsg]; ch == nil && len(ws) > 0 {
		ch = ws[0]
		c.waiters[emsg] = ws[1:]
	}
	if ch != nil && !c.closed {
		select {
		case ch <- p:
		default:
		}
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if c.onPacket != nil {
		c.onPacket(p)
	}
}

func (c *Client) handleMulti(body []byte) {
	m, err := Parse(body)
	if err != nil {
		return
	}
	payload := m.Raw(2)
	if m.U(1) > 0 {
		zr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return
		}
		if payload, err = io.ReadAll(zr); err != nil {
			return
		}
	}
	for len(payload) >= 4 {
		n := int(binary.LittleEndian.Uint32(payload))
		if n > len(payload)-4 {
			return
		}
		c.handle(payload[4 : 4+n])
		payload = payload[4+n:]
	}
}

func (c *Client) send(emsg uint32, body []byte, jobName string, jobSource uint64) error {
	c.mu.Lock()
	sid, sess, closed := c.steamID, c.sessionID, c.closed
	c.mu.Unlock()
	if closed {
		return errors.New("not connected")
	}
	if emsg == EMsgClientHello || emsg == EMsgServiceMethodCallFromClientNonAuthed {
		sid, sess = 0, 0
	}
	h := NewPB().Fixed64(1, sid).Int(2, int64(sess)).Fixed64(10, jobSource).Fixed64(11, jobNone)
	if jobName != "" {
		h.Str(12, jobName).Uint(32, 1)
	}
	buf := make([]byte, 8, 8+len(h.b)+len(body))
	binary.LittleEndian.PutUint32(buf, emsg|protoMask)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(h.b)))
	buf = append(append(buf, h.b...), body...)
	return c.ws.Send(buf)
}

// Send writes a message without waiting for a reply.
func (c *Client) Send(emsg uint32, body *PB) error { return c.send(emsg, body.b, "", jobNone) }

func (c *Client) newJob() (uint64, chan *Packet, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, nil, errors.New("not connected")
	}
	c.jobID++
	ch := make(chan *Packet, 1)
	c.jobs[c.jobID] = ch
	return c.jobID, ch, nil
}

func (c *Client) await(id uint64, ch chan *Packet, timeout time.Duration) (*Packet, error) {
	select {
	case p, ok := <-ch:
		if !ok {
			return nil, errors.New("connection closed")
		}
		return p, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.jobs, id)
		c.mu.Unlock()
		return nil, errors.New("steam did not answer in time")
	}
}

// Request sends a message as a job and waits for the reply addressed to it.
func (c *Client) Request(emsg uint32, body *PB, timeout time.Duration) (*Packet, error) {
	id, ch, err := c.newJob()
	if err != nil {
		return nil, err
	}
	if err := c.send(emsg, body.b, "", id); err != nil {
		return nil, err
	}
	return c.await(id, ch, timeout)
}

// RequestReply is like Request, but also accepts a reply of type replyEMsg that comes without a job id.
func (c *Client) RequestReply(emsg uint32, body *PB, replyEMsg uint32, timeout time.Duration) (*Packet, error) {
	id, ch, err := c.newJob()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.waiters[replyEMsg] = append(c.waiters[replyEMsg], ch)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		ws := c.waiters[replyEMsg]
		for i, w := range ws {
			if w == ch {
				c.waiters[replyEMsg] = append(ws[:i], ws[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
	}()
	if err := c.send(emsg, body.b, "", id); err != nil {
		return nil, err
	}
	return c.await(id, ch, timeout)
}

// Call runs a unified service method such as "Player.GetOwnedGames#1".
func (c *Client) Call(method string, body *PB, authed bool) (int, Fields, error) {
	id, ch, err := c.newJob()
	if err != nil {
		return 0, nil, err
	}
	emsg := uint32(EMsgServiceMethodCallFromClientNonAuthed)
	if authed {
		emsg = EMsgServiceMethodCallFromClient
	}
	if err := c.send(emsg, body.b, method, id); err != nil {
		return 0, nil, err
	}
	p, err := c.await(id, ch, 25*time.Second)
	if err != nil {
		return 0, nil, err
	}
	res := ResOK
	if p.Header.Has(13) {
		res = int(p.Header.I(13))
	}
	f, _ := Parse(p.Body)
	return res, f, nil
}
