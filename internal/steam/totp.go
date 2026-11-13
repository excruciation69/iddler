package steam

// Steam Guard mobile codes from a shared secret (the same algorithm the Steam app uses).

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const guardAlphabet = "23456789BCDFGHJKMNPQRTVWXY"

func GuardCode(sharedSecret string, at time.Time) (string, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sharedSecret))
	if err != nil || len(key) == 0 {
		return "", errors.New("invalid shared_secret")
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[19] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:]) & 0x7fffffff
	code := make([]byte, 5)
	for i := range code {
		code[i] = guardAlphabet[v%uint32(len(guardAlphabet))]
		v /= uint32(len(guardAlphabet))
	}
	return string(code), nil
}

// MaFile holds what SteamIdler uses from a Steam Desktop Authenticator .maFile.
type MaFile struct {
	Account        string
	SharedSecret   string // login codes
	IdentitySecret string // signs confirmations (market listings, trades)
	DeviceID       string
}

// ParseMaFile reads a Steam Desktop Authenticator .maFile.
func ParseMaFile(data []byte) (MaFile, error) {
	var f struct {
		AccountName    string `json:"account_name"`
		SharedSecret   string `json:"shared_secret"`
		IdentitySecret string `json:"identity_secret"`
		DeviceID       string `json:"device_id"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return MaFile{}, errors.New("not a .maFile (encrypted files are not supported)")
	}
	if f.SharedSecret == "" {
		return MaFile{}, errors.New("shared_secret is missing")
	}
	if _, err := GuardCode(f.SharedSecret, time.Now()); err != nil {
		return MaFile{}, err
	}
	return MaFile{f.AccountName, f.SharedSecret, f.IdentitySecret, f.DeviceID}, nil
}
