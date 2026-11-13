package steam

// Messages used after logon: presence, games, friends, chat, keys, licenses, stats.

import (
	"fmt"
	"strconv"
	"time"
)

func LogonRequest(token, account, machineName string, logonID uint32) *PB {
	return NewPB().
		Uint(1, protocolVersion).
		Str(6, "english").
		Uint(7, osWin11).
		Bool(8, true).
		Msg(11, NewPB().Fixed32(1, logonID)).
		Bytes(30, MachineID(account)).
		Uint(33, 2).
		Str(96, machineName).
		Bool(102, true).
		Str(108, token)
}

// Logon signs in with a refresh token. The reply arrives as EMsgClientLogOnResponse.
func (c *Client) Logon(token, account, machineName string, logonID uint32) error {
	ti, err := ParseToken(token)
	if err != nil {
		return err
	}
	c.setSteamID(ti.SteamID)
	return c.Send(EMsgClientLogon, LogonRequest(token, account, machineName, logonID))
}

func (c *Client) Heartbeat() error { return c.Send(EMsgClientHeartBeat, NewPB().Bool(1, false)) }

func (c *Client) LogOff() error { return c.Send(EMsgClientLogOff, NewPB()) }

// Persona states
const (
	PersonaOffline   = 0
	PersonaOnline    = 1
	PersonaAway      = 3
	PersonaInvisible = 7
)

func (c *Client) SetPersona(state uint32, name string) error {
	m := NewPB().Uint(1, uint64(state))
	if name != "" {
		m.Str(2, name)
	}
	return c.Send(EMsgClientChangeStatus, m)
}

func (c *Client) RequestPersonas(ids ...uint64) error {
	m := NewPB().Uint(1, 1|2|16|64)
	for _, id := range ids {
		m.Fixed64(2, id)
	}
	return c.Send(EMsgClientRequestFriendData, m)
}

func GamesPlayedRequest(custom string, appIDs []uint32) *PB {
	m := NewPB()
	if custom != "" {
		m.Msg(1, NewPB().Fixed64(2, NonSteamGameID).Str(7, custom))
	}
	for _, id := range appIDs {
		m.Msg(1, NewPB().Fixed64(2, uint64(id)))
	}
	return m.Uint(2, osWin11)
}

// GamesPlayed reports games as running. No games = stop.
func (c *Client) GamesPlayed(custom string, appIDs []uint32) error {
	return c.Send(EMsgClientGamesPlayedWithDataBlob, GamesPlayedRequest(custom, appIDs))
}

// KickPlayingSession stops a game running on another device.
func (c *Client) KickPlayingSession() error {
	return c.Send(EMsgClientKickPlayingSession, NewPB().Bool(1, true))
}

type OwnedGame struct {
	AppID    uint32 `json:"appid"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Minutes  int64  `json:"minutes"`
	HasStats bool   `json:"hasStats"`
	Manual   bool   `json:"manual,omitempty"`
}

func (c *Client) OwnedGames(steamID uint64) ([]OwnedGame, error) {
	req := NewPB().Uint(1, steamID).Bool(2, true).Bool(3, true).Bool(6, true).Str(7, "english")
	res, m, err := c.Call("Player.GetOwnedGames#1", req, true)
	if err != nil {
		return nil, err
	}
	if res != ResOK {
		return nil, fmt.Errorf("%s", ResultName(res))
	}
	var out []OwnedGame
	for _, g := range m.Msgs(2) {
		id := uint32(g.U(1))
		og := OwnedGame{AppID: id, Name: g.S(2), Minutes: g.I(4), HasStats: g.B(7)}
		if og.Name == "" {
			og.Name = "App " + strconv.Itoa(int(id))
		}
		if h := g.S(5); h != "" {
			og.Icon = fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steamcommunity/public/images/apps/%d/%s.jpg", id, h)
		}
		out = append(out, og)
	}
	return out, nil
}

// SendChat sends a plain text message to a friend.
func (c *Client) SendChat(to uint64, text string) error {
	_, _, err := c.Call("FriendMessages.SendMessage#1", NewPB().Fixed64(1, to).Int(2, 1).Str(3, text).Bool(4, false), true)
	return err
}

// ---- keys and free licenses

// Purchase result details (EPurchaseResultDetail) that matter for key activation.
const (
	PurchaseOK                   = 0
	PurchaseAlreadyOwned         = 9
	PurchaseRegionRestricted     = 13
	PurchaseBadKey               = 14
	PurchaseDuplicateKey         = 15
	PurchaseRequiresBaseGame     = 24
	PurchaseCannotRedeemFromHere = 50
	PurchaseRateLimited          = 53
)

type KeyResult struct {
	Result int      // EResult
	Detail int      // EPurchaseResultDetail
	Items  []string // what the key contains
}

func (c *Client) RedeemKey(key string) (KeyResult, error) {
	p, err := c.Request(EMsgClientRegisterKey, NewPB().Str(1, key), 90*time.Second)
	if err != nil {
		return KeyResult{}, err
	}
	m := p.Fields()
	r := KeyResult{Result: int(m.I(1)), Detail: int(m.I(2))}
	if kv, err := ParseKV(m.Raw(3)); err == nil {
		if items := kv.Path("MessageObject", "lineitems"); items != nil {
			for _, li := range items.Children {
				if d := li.Get("ItemDescription").Str(); d != "" {
					r.Items = append(r.Items, d)
				}
			}
		}
	}
	return r, nil
}

func (c *Client) RequestFreeLicense(appIDs ...uint32) (granted []uint32, err error) {
	m := NewPB()
	for _, id := range appIDs {
		m.Uint(2, uint64(id))
	}
	p, err := c.Request(EMsgClientRequestFreeLicense, m, 20*time.Second)
	if err != nil {
		return nil, err
	}
	for _, id := range p.Fields().Uints(3) {
		granted = append(granted, uint32(id))
	}
	return granted, nil
}

// ---- discovery queue (sales)

func (c *Client) DiscoveryQueue() ([]uint32, error) {
	res, m, err := c.Call("Store.GetDiscoveryQueue#1", NewPB().Uint(1, 0).Bool(6, true), true)
	if err != nil {
		return nil, err
	}
	if res != ResOK {
		return nil, fmt.Errorf("%s", ResultName(res))
	}
	var ids []uint32
	for _, id := range m.Uints(1) {
		ids = append(ids, uint32(id))
	}
	return ids, nil
}

func (c *Client) SkipDiscoveryItem(appID uint32) error {
	_, _, err := c.Call("Store.SkipDiscoveryQueueItem#1", NewPB().Uint(1, 0).Uint(2, uint64(appID)), true)
	return err
}
