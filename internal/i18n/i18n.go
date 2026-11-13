// Package i18n renders the shared RU/EN strings (ui/i18n.json) on the Go side:
// notifications, Telegram and the menu bar.
package i18n

import (
	"encoding/json"
	"strings"
	"sync"

	"steamidler/ui"
)

var (
	once sync.Once
	dict map[string]map[string]string
)

func load() {
	raw, _ := ui.FS.ReadFile("i18n.json")
	json.Unmarshal(raw, &dict)
}

// T returns the string for key in lang, with {0}, {1}... replaced by args.
func T(lang, key string, args ...string) string {
	once.Do(load)
	s, ok := dict[lang][key]
	if !ok {
		if s, ok = dict["en"][key]; !ok {
			s = key
		}
	}
	for i, a := range args {
		s = strings.ReplaceAll(s, "{"+string(rune('0'+i))+"}", a)
	}
	return s
}
