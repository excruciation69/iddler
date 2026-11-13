package bot

import (
	"testing"
	"time"

	"steamidler/internal/steam"
	"steamidler/internal/steam/steamtest"
)

func TestFees(t *testing.T) {
	for _, c := range []struct{ receive, pays int }{{1, 3}, {10, 12}, {20, 23}, {100, 115}, {391, 449}} {
		if got := steam.BuyerPays(c.receive); got != c.pays {
			t.Errorf("BuyerPays(%d) = %d, want %d", c.receive, got, c.pays)
		}
	}
	for pays := 3; pays < 5000; pays++ {
		r := steam.SellerReceives(pays)
		if steam.BuyerPays(r) > pays || steam.BuyerPays(r+1) <= pays {
			t.Fatalf("SellerReceives(%d) = %d is not the best fit", pays, r)
		}
	}
}

func TestInventoryManager(t *testing.T) {
	f := steamtest.Start()
	defer f.Close()
	marketDelay = 0
	h := &host{secrets: map[string]string{}}
	b := New("gleb", t.TempDir(), "gleb", h)
	defer b.Shutdown()
	b.LoginPassword("secret")
	wait(t, "guard", func() bool { return b.get(func(b *Bot) bool { return b.status == "guard" }) })
	b.Guard("AB12C")
	wait(t, "online", func() bool { return b.get(func(b *Bot) bool { return b.status == "online" }) })

	inv, err := b.Inventory(0, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv) != 3 || inv[0].Name != "Aperture Lab" || inv[1].Count != 3 || inv[1].Kind != "card" || inv[2].Marketable {
		t.Fatalf("grouping: %+v", inv)
	}
	if inv[1].Icon != steam.EconImage+"i1/96fx96f" || inv[1].Game != "Portal 2" {
		t.Fatalf("details: %+v", inv[1])
	}

	p, err := b.ItemPrice(753, "620-Chell", true)
	if err != nil || p.Lowest != 450 || p.Median != 420 || p.HighestBuy != 398 || p.Volume != 1204 {
		t.Fatalf("price %+v %v", p, err)
	}

	// duplicates: two extra Chell cards at the best buy order minus one cent
	n, err := b.SellDuplicates(753, "6", "buy", 1, 0, 0, []string{"card"})
	if err != nil || n != 2 {
		t.Fatalf("duplicates: %d %v", n, err)
	}
	wait(t, "sold", func() bool {
		return b.get(func(b *Bot) bool { return b.mjob != nil && !b.mjob.Running && b.mjob.Done == 2 && b.mjob.Pending == 2 })
	})
	if len(f.Sold) != 2 || f.Sold[0] != "12@"+itoa(steam.SellerReceives(397)) {
		t.Fatalf("sell requests %v", f.Sold)
	}
	inv, _ = b.Inventory(0, "", false)
	if inv[1].Count != 1 {
		t.Fatalf("sold copies should leave the inventory: %+v", inv[1])
	}

	// confirmations need the identity secret from a .maFile
	if _, err := b.ConfirmMarket(); err == nil {
		t.Fatal("confirming without a .maFile should fail")
	}
	b.SetMaFile(steam.MaFile{SharedSecret: "c2VjcmV0c2VjcmV0c2VjcmV0", IdentitySecret: "aWRlbnRpdHk=", DeviceID: "android:1"})
	if c, err := b.ConfirmMarket(); err != nil || c != 1 {
		t.Fatalf("confirm: %d %v", c, err)
	}

	ls, err := b.Listings()
	if err != nil || len(ls) != 1 || ls[0].Pays != 449 || ls[0].Receive != 391 {
		t.Fatalf("listings %+v %v", ls, err)
	}
	if err := b.RemoveListings(nil); err != nil {
		t.Fatal(err)
	}
	wait(t, "removed", func() bool { return b.get(func(b *Bot) bool { return !b.mjob.Running && f.Removed == 1 }) })

	res, total, err := b.MarketSearch("key", 440, 0)
	if err != nil || total != 1 || res[0].Price != 219 || res[0].AppID != 440 {
		t.Fatalf("search %+v %d %v", res, total, err)
	}
	time.Sleep(10 * time.Millisecond)
}
