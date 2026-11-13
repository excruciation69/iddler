package server

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"iddler/internal/hub"
	"iddler/internal/platform"
	"iddler/internal/steam/steamtest"
)

// UI_SERVE=1 go test ./internal/server -run TestServeUI : a live demo against the fake Steam, for browser checks.
func TestServeUI(t *testing.T) {
	if os.Getenv("UI_SERVE") == "" {
		t.Skip("set UI_SERVE=1")
	}
	f := steamtest.Start()
	defer f.Close()
	f.Badges = `<div class="badge_row is_link"><a href="https://steamcommunity.com/id/x/gamecards/730/"></a><div class="badge_title">Counter-Strike 2&nbsp;</div><div>&nbsp;12.5 hrs on record</div><span class="progress_info_bold">3 card drops remaining</span></div>`
	h := hub.New(t.TempDir(), platform.New())
	s := New(h, platform.New())
	s.Key = "testkey"
	s.OnQuit = func() {}
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile("/tmp/iddler_ui_url", []byte(s.Base()), 0o644)
	fmt.Println("UI", s.Base()+"/?k=testkey")
	main, alt := "gleb", "gleb_alt"
	if os.Getenv("IDDLER_DEMO_NAME") != "" {
		main, alt = "kestrel", "marlin"
	}
	id, _ := h.AddAccount(main, "secret", true)
	time.Sleep(500 * time.Millisecond)
	b := h.Bot(id)
	b.Guard("AB12C")
	for i := 0; i < 100 && b.Summary().Status != "online"; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	b.Update(json.RawMessage(`{"modules":{"profile":true,"cards":true,"free":true},"profile":{"slideOn":true,"names":["Serious Sam: The Third Encounter [Build 0.3.1 Internal]","Half-Life 3 (Closed Beta)"]},"hours":{"selected":[730,41000],"goals":{"730":1000}}}`))
	b.SetRun("hours", true)
	b.QueueKeys("AAAAA-BBBBB-CCCCC BADXX-11111-22222")
	h.AddAccount(alt, "nope", true)
	time.Sleep(40 * time.Minute)
}
