package bot

// Key activation: one key or a whole pasted text, queued and rate-limit aware, with history.

import (
	"regexp"
	"strings"
	"time"

	"steamidler/internal/steam"
)

type KeyRecord struct {
	At     int64  `json:"at"`
	Key    string `json:"key"`
	Game   string `json:"game"`
	Result string `json:"result"` // ok | owned | bad | used | region | base | limit | error
}

// keyDelay spaces out activations so Steam doesn't rate-limit the account.
var keyDelay = 4 * time.Second

var keyPattern = regexp.MustCompile(`\b[A-Z0-9]{5}(?:-[A-Z0-9]{5}){2,4}\b`)

// FindKeys pulls every Steam key out of arbitrary text.
func FindKeys(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range keyPattern.FindAllString(strings.ToUpper(text), -1) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// QueueKeys adds keys to the activation queue and returns how many were new.
func (b *Bot) QueueKeys(text string) int {
	keys := FindKeys(text)
	b.mu.Lock()
	defer b.mu.Unlock()
	known := map[string]bool{}
	for _, k := range b.keyQueue {
		known[k] = true
	}
	for _, r := range b.data.Keys {
		if r.Result == "ok" || r.Result == "owned" {
			known[r.Key] = true
		}
	}
	n := 0
	for _, k := range keys {
		if !known[k] {
			b.keyQueue = append(b.keyQueue, k)
			n++
		}
	}
	if n > 0 {
		b.log("info", "log.keysQueued", itoa(n))
		b.changed()
		if !b.keyBusy {
			b.keyBusy = true
			go b.keyWorker()
		}
	}
	return n
}

func (b *Bot) keyWorker() {
	for {
		b.mu.Lock()
		if len(b.keyQueue) == 0 {
			b.keyBusy = false
			b.changed()
			b.mu.Unlock()
			return
		}
		wait := time.Until(b.keyPause)
		key := b.keyQueue[0]
		cm := b.cm
		online := b.status == "online"
		b.mu.Unlock()

		if wait > 0 || !online || cm == nil {
			select {
			case <-b.stop:
				return
			case <-time.After(max(wait, 15*time.Second)):
			}
			continue
		}

		res, err := cm.RedeemKey(key)
		rec := KeyRecord{At: time.Now().Unix(), Key: key, Game: strings.Join(res.Items, ", ")}
		switch {
		case err != nil:
			rec.Result = "error"
		case res.Result == steam.ResOK:
			rec.Result = "ok"
		default:
			rec.Result = keyResult(res.Detail)
		}

		b.mu.Lock()
		if rec.Result == "limit" {
			b.keyPause = time.Now().Add(time.Hour)
			b.log("warn", "log.keysLimited")
		} else if err != nil && b.status != "online" {
			// connection dropped mid-request: keep the key for later
		} else {
			b.keyQueue = b.keyQueue[1:]
			b.data.Keys = append(b.data.Keys, rec)
			if len(b.data.Keys) > 1000 {
				b.data.Keys = b.data.Keys[len(b.data.Keys)-1000:]
			}
			b.saveData()
			game := rec.Game
			if game == "" {
				game = "?"
			}
			kind := "ok"
			if rec.Result != "ok" {
				kind = "warn"
			}
			b.log(kind, "log.key."+rec.Result, maskKey(key), game)
			if rec.Result == "ok" {
				b.host.Notify("keys", "notify.key", b.displayName(), game)
				go b.LoadGames()
			}
		}
		b.changed()
		b.mu.Unlock()

		select {
		case <-b.stop:
			return
		case <-time.After(keyDelay):
		}
	}
}

func keyResult(detail int) string {
	switch detail {
	case steam.PurchaseAlreadyOwned:
		return "owned"
	case steam.PurchaseBadKey:
		return "bad"
	case steam.PurchaseDuplicateKey:
		return "used"
	case steam.PurchaseRegionRestricted:
		return "region"
	case steam.PurchaseRequiresBaseGame:
		return "base"
	case steam.PurchaseRateLimited:
		return "limit"
	}
	return "error"
}

func maskKey(k string) string {
	parts := strings.Split(k, "-")
	for i := 1; i < len(parts)-1; i++ {
		parts[i] = "*****"
	}
	return strings.Join(parts, "-")
}
