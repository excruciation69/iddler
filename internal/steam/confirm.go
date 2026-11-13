package steam

// Mobile confirmations (market listings, trades) signed with the identity_secret from a .maFile,
// the same way the Steam mobile app does it.

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Confirmation is one pending item in the mobile confirmations list.
type Confirmation struct {
	ID       string `json:"id"`
	Nonce    string `json:"nonce"`
	Type     int    `json:"type"` // 2 trade, 3 market listing
	Headline string `json:"headline"`
	Summary  string `json:"summary"`
	Icon     string `json:"icon"`
}

// confKey signs a confirmation request: base64(HMAC-SHA1(identity_secret, time || tag)).
func confKey(identitySecret string, t int64, tag string) (string, error) {
	secret, err := base64.StdEncoding.DecodeString(identitySecret)
	if err != nil {
		return "", errors.New("bad identity_secret")
	}
	msg := make([]byte, 8, 8+len(tag))
	binary.BigEndian.PutUint64(msg, uint64(t))
	mac := hmac.New(sha1.New, secret)
	mac.Write(append(msg, tag...))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (w *Web) confParams(identitySecret, deviceID, tag string) (url.Values, error) {
	t := time.Now().Unix()
	k, err := confKey(identitySecret, t, tag)
	if err != nil {
		return nil, err
	}
	return url.Values{"p": {deviceID}, "a": {strconv.FormatUint(w.SteamID, 10)}, "k": {k},
		"t": {strconv.FormatInt(t, 10)}, "m": {"react"}, "tag": {tag}}, nil
}

// Confirmations lists what waits for a confirmation.
func (w *Web) Confirmations(identitySecret, deviceID string) ([]Confirmation, error) {
	q, err := w.confParams(identitySecret, deviceID, "list")
	if err != nil {
		return nil, err
	}
	body, err := w.get(CommunityURL + "/mobileconf/getlist?" + q.Encode())
	if err != nil {
		return nil, err
	}
	var r struct {
		Success  bool   `json:"success"`
		NeedAuth bool   `json:"needauth"`
		Message  string `json:"message"`
		Conf     []struct {
			ID       string   `json:"id"`
			Nonce    string   `json:"nonce"`
			Type     int      `json:"type"`
			Headline string   `json:"headline"`
			Summary  []string `json:"summary"`
			Icon     string   `json:"icon"`
		} `json:"conf"`
	}
	if json.Unmarshal(body, &r) != nil || !r.Success {
		if r.NeedAuth {
			return nil, ErrWebAuth
		}
		return nil, fmt.Errorf("confirmations are unavailable %s", r.Message)
	}
	out := make([]Confirmation, 0, len(r.Conf))
	for _, c := range r.Conf {
		s := ""
		if len(c.Summary) > 0 {
			s = c.Summary[0]
		}
		out = append(out, Confirmation{ID: c.ID, Nonce: c.Nonce, Type: c.Type, Headline: c.Headline, Summary: s, Icon: c.Icon})
	}
	return out, nil
}

// RespondConfirmations accepts or cancels confirmations in one request.
func (w *Web) RespondConfirmations(identitySecret, deviceID string, cs []Confirmation, accept bool) error {
	if len(cs) == 0 {
		return nil
	}
	op, tag := "cancel", "reject"
	if accept {
		op, tag = "allow", "accept"
	}
	form, err := w.confParams(identitySecret, deviceID, tag)
	if err != nil {
		return err
	}
	form.Set("op", op)
	for _, c := range cs {
		form.Add("cid[]", c.ID)
		form.Add("ck[]", c.Nonce)
	}
	body, err := w.post(CommunityURL+"/mobileconf/multiajaxop", form, CommunityURL+"/mobileconf/conf")
	if err != nil {
		return err
	}
	var r struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(body, &r) != nil || !r.Success {
		return errors.New("Steam refused the confirmation")
	}
	return nil
}
