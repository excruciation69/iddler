package steam

// Password login through IAuthenticationService (the flow the official client uses).

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// Steam Guard confirmation types
const (
	GuardNone               = 1
	GuardEmailCode          = 2
	GuardDeviceCode         = 3
	GuardDeviceConfirmation = 4
	GuardEmailConfirmation  = 5
)

type AuthSession struct {
	ClientID      uint64
	RequestID     []byte
	Interval      float32
	SteamID       uint64
	Confirmations []AuthConfirmation
}

type AuthConfirmation struct {
	Type    int
	Message string // e.g. email domain
}

// MachineID mimics the official client's machine id blob (stable per account name).
func MachineID(account string) []byte {
	hash := func(s string) string {
		sum := sha1.Sum([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	var b bytes.Buffer
	str := func(s string) { b.WriteString(s); b.WriteByte(0) }
	b.WriteByte(0)
	str("MessageObject")
	for _, k := range []string{"BB3", "FF2", "3B3"} {
		b.WriteByte(1)
		str(k)
		str(hash("SteamUser Hash " + k + " " + account))
	}
	b.WriteByte(8)
	b.WriteByte(8)
	return b.Bytes()
}

func (c *Client) Hello() error {
	return c.Send(EMsgClientHello, NewPB().Uint(1, protocolVersion))
}

// BeginAuth starts a password login. A non-OK result means Steam rejected it.
func (c *Client) BeginAuth(account, password, deviceName string) (*AuthSession, int, error) {
	res, m, err := c.Call("Authentication.GetPasswordRSAPublicKey#1", NewPB().Str(1, account), false)
	if err != nil || res != ResOK {
		return nil, res, err
	}
	mod, ok1 := new(big.Int).SetString(m.S(1), 16)
	exp, ok2 := new(big.Int).SetString(m.S(2), 16)
	if !ok1 || !ok2 {
		return nil, 0, errors.New("bad RSA key from Steam")
	}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, &rsa.PublicKey{N: mod, E: int(exp.Int64())}, []byte(password))
	if err != nil {
		return nil, 0, err
	}
	req := BeginAuthRequest(account, base64.StdEncoding.EncodeToString(enc), m.U(3), deviceName)
	res, m, err = c.Call("Authentication.BeginAuthSessionViaCredentials#1", req, false)
	if err != nil || res != ResOK {
		return nil, res, err
	}
	s := &AuthSession{ClientID: m.U(1), RequestID: m.Raw(2), Interval: m.F32(3), SteamID: m.U(5)}
	for _, cf := range m.Msgs(4) {
		s.Confirmations = append(s.Confirmations, AuthConfirmation{Type: int(cf.U(1)), Message: cf.S(2)})
	}
	if s.Interval <= 0 {
		s.Interval = 5
	}
	return s, ResOK, nil
}

func BeginAuthRequest(account, encPassword string, timestamp uint64, deviceName string) *PB {
	device := NewPB().Str(1, deviceName).Uint(2, 1).Int(3, osWin11).Uint(4, 1).Bytes(6, MachineID(account))
	return NewPB().
		Str(2, account).
		Str(3, encPassword).
		Uint(4, timestamp).
		Bool(5, true).
		Uint(7, 1). // persistent session
		Str(8, "Unknown").
		Msg(9, device)
}

func (c *Client) SubmitGuardCode(s *AuthSession, code string, codeType int) (int, error) {
	req := NewPB().Uint(1, s.ClientID).Fixed64(2, s.SteamID).Str(3, code).Uint(4, uint64(codeType))
	res, _, err := c.Call("Authentication.UpdateAuthSessionWithSteamGuardCode#1", req, false)
	return res, err
}

// PollAuth returns the refresh token once the login is approved ("" while pending).
func (c *Client) PollAuth(s *AuthSession) (string, int, error) {
	res, m, err := c.Call("Authentication.PollAuthSessionStatus#1", NewPB().Uint(1, s.ClientID).Bytes(2, s.RequestID), false)
	if err != nil || res != ResOK {
		return "", res, err
	}
	if id := m.U(1); id != 0 {
		s.ClientID = id
	}
	return m.S(3), ResOK, nil
}

// GenerateAccessToken trades a refresh token for a short-lived access token (for web requests).
func (c *Client) GenerateAccessToken(refreshToken string, steamID uint64) (string, error) {
	res, m, err := c.Call("Authentication.GenerateAccessTokenForApp#1", NewPB().Str(1, refreshToken).Fixed64(2, steamID), true)
	if err != nil {
		return "", err
	}
	if res != ResOK || m.S(1) == "" {
		return "", errors.New(ResultName(res))
	}
	return m.S(1), nil
}

type TokenInfo struct {
	SteamID uint64
	Expires int64
}

// ParseToken reads the SteamID and expiry from a refresh token (a JWT).
func ParseToken(tok string) (TokenInfo, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return TokenInfo{}, errors.New("bad token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return TokenInfo{}, err
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return TokenInfo{}, err
	}
	id, err := strconv.ParseUint(claims.Sub, 10, 64)
	if err != nil {
		return TokenInfo{}, errors.New("token has no SteamID")
	}
	return TokenInfo{SteamID: id, Expires: claims.Exp}, nil
}

// ---- sign-in with a QR code scanned in the Steam mobile app

// QRSession is a pending QR sign-in. URL is what the QR code encodes; Steam may rotate it.
type QRSession struct {
	ClientID  uint64
	RequestID []byte
	Interval  float32
	URL       string
}

// QRResult is filled once the phone approved the sign-in.
type QRResult struct {
	Account      string
	RefreshToken string
}

// BeginQR starts a sign-in that the Steam mobile app approves by scanning a QR code.
func (c *Client) BeginQR(deviceName string) (*QRSession, int, error) {
	device := NewPB().Str(1, deviceName).Uint(2, 1).Int(3, osWin11).Uint(4, 1)
	req := NewPB().Str(1, deviceName).Uint(2, 1).Msg(3, device).Str(4, "Unknown")
	res, m, err := c.Call("Authentication.BeginAuthSessionViaQR#1", req, false)
	if err != nil || res != ResOK {
		return nil, res, err
	}
	s := &QRSession{ClientID: m.U(1), URL: m.S(2), RequestID: m.Raw(3), Interval: m.F32(4)}
	if s.Interval <= 0 {
		s.Interval = 5
	}
	return s, ResOK, nil
}

// PollQR checks a QR sign-in. It returns nil while the phone hasn't approved yet;
// s.URL changes when Steam rotates the code.
func (c *Client) PollQR(s *QRSession) (*QRResult, int, error) {
	res, m, err := c.Call("Authentication.PollAuthSessionStatus#1", NewPB().Uint(1, s.ClientID).Bytes(2, s.RequestID), false)
	if err != nil || res != ResOK {
		return nil, res, err
	}
	if id := m.U(1); id != 0 {
		s.ClientID = id
	}
	if u := m.S(2); u != "" {
		s.URL = u
	}
	if tok := m.S(3); tok != "" {
		return &QRResult{Account: m.S(6), RefreshToken: tok}, ResOK, nil
	}
	return nil, ResOK, nil
}
