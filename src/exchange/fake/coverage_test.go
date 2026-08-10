package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
)

func TestFake_IDCancelAndNilClock(t *testing.T) {
	ex := fake.New("hyperliquid", nil)
	if ex.ID() != "hyperliquid" {
		t.Fatal(ex.ID())
	}
	ack, err := ex.PlaceOrder(context.Background(), exchange.OrderRequest{
		ClientOrderID: "c1", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.CancelOrder(context.Background(), ack.OrderID); err != nil {
		t.Fatal(err)
	}
	// Cancel of unknown id is a no-op success (idempotent paper cancel).
	if err := ex.CancelOrder(context.Background(), "missing"); err != nil {
		t.Fatal(err)
	}
}

func TestWallClock_Now(t *testing.T) {
	before := time.Now().Add(-time.Second)
	now := exchange.WallClock{}.Now()
	if now.Before(before) {
		t.Fatalf("wall clock %v", now)
	}
}
