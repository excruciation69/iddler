package hub

import (
	"testing"
	"time"

	"iddler/internal/platform"
	"iddler/internal/steam/steamtest"
)

func TestQRSignIn(t *testing.T) {
	f := steamtest.Start()
	defer f.Close()
	h := New(t.TempDir(), platform.New())
	defer h.Shutdown()

	st, err := h.StartQR()
	if err != nil || st.URL != "https://s.team/q/1/888" {
		t.Fatalf("start: %+v %v", st, err)
	}
	for i := 0; i < 100 && !h.QR().Done; i++ {
		time.Sleep(30 * time.Millisecond)
	}
	if st := h.QR(); st.Error != "" || st.Account != "qr_user" {
		t.Fatalf("state: %+v", st)
	}
	b := h.Bot("qr_user")
	if b == nil || !b.HasToken() {
		t.Fatal("the account was not added with its session")
	}
	for i := 0; i < 100 && b.Summary().Status != "online"; i++ {
		time.Sleep(30 * time.Millisecond)
	}
	if b.Summary().Status != "online" {
		t.Fatal("not signed in")
	}
}
