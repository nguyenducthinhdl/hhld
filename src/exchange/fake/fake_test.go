package fake_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
)

func TestDualFeed_BooksDriveConsumerWithoutNetwork(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	dual := fake.NewDual("hyperliquid", "grvt", start)

	var mu sync.Mutex
	got := map[exchange.VenueID]exchange.Book{}

	handler := func(b exchange.Book) {
		mu.Lock()
		got[b.Venue] = b
		mu.Unlock()
	}
	if err := dual.A.SubscribeBook(context.Background(), "BTCUSD", handler); err != nil {
		t.Fatal(err)
	}
	if err := dual.B.SubscribeBook(context.Background(), "BTCUSD", handler); err != nil {
		t.Fatal(err)
	}

	dual.Clock.Advance(time.Second)
	dual.A.SetBook(exchange.Book{
		Symbol: "BTCUSD",
		Kind:   exchange.KindPerp,
		Bids:   []exchange.Level{{Price: "100.0", Size: "1"}},
		Asks:   []exchange.Level{{Price: "100.1", Size: "1"}},
	})
	dual.B.SetBook(exchange.Book{
		Symbol: "BTCUSD",
		Kind:   exchange.KindPerp,
		Bids:   []exchange.Level{{Price: "100.5", Size: "1"}},
		Asks:   []exchange.Level{{Price: "100.6", Size: "1"}},
	})

	mu.Lock()
	a := got["hyperliquid"]
	b := got["grvt"]
	mu.Unlock()
	if a.Symbol == "" || b.Symbol == "" {
		t.Fatalf("missing books: %+v", got)
	}
	if a.Bids[0].Price != "100.0" || b.Bids[0].Price != "100.5" {
		t.Fatalf("unexpected books A=%+v B=%+v", a, b)
	}
	wantT := start.Add(time.Second)
	if !a.Time.Equal(wantT) || !b.Time.Equal(wantT) {
		t.Fatalf("book times not from shared clock: A=%v B=%v want %v", a.Time, b.Time, wantT)
	}

	snap, err := dual.A.SnapshotBook(context.Background(), "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Asks[0].Price != "100.1" {
		t.Fatalf("snapshot: %+v", snap)
	}
}

func TestFake_PushTicksToConsumer(t *testing.T) {
	clock := exchange.NewManualClock(time.Unix(100, 0).UTC())
	ex := fake.New("hyperliquid", clock)

	var mu sync.Mutex
	var ticks []exchange.Tick
	if err := ex.SubscribeTicks(context.Background(), "BTCUSD", func(tk exchange.Tick) {
		mu.Lock()
		ticks = append(ticks, tk)
		mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}

	clock.Advance(time.Second)
	ex.PushTick(exchange.Tick{
		Symbol: "BTCUSD",
		Kind:   exchange.KindPerp,
		Price:  "100.2",
		Size:   "0.5",
		Side:   exchange.SideBuy,
	})

	mu.Lock()
	defer mu.Unlock()
	if len(ticks) != 1 {
		t.Fatalf("want 1 tick, got %d", len(ticks))
	}
	tk := ticks[0]
	if tk.Price != "100.2" || tk.Venue != "hyperliquid" {
		t.Fatalf("tick: %+v", tk)
	}
	if !tk.Time.Equal(time.Unix(101, 0).UTC()) {
		t.Fatalf("tick time=%v", tk.Time)
	}
}

func TestFake_PaperOrderUsesClock(t *testing.T) {
	clock := exchange.NewManualClock(time.Unix(50, 0).UTC())
	ex := fake.New("grvt", clock)
	ack, err := ex.PlaceOrder(context.Background(), exchange.OrderRequest{
		ClientOrderID: "c1",
		Symbol:        "BTCUSD",
		Kind:          exchange.KindPerp,
		Side:          exchange.SideBuy,
		Price:         "100",
		Size:          "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ack.Time.Equal(time.Unix(50, 0).UTC()) || ack.Status != "accepted" {
		t.Fatalf("ack: %+v", ack)
	}
}

func TestFake_SubscribeDeliversExistingBook(t *testing.T) {
	ex := fake.New("hyperliquid", exchange.NewManualClock(time.Unix(1, 0).UTC()))
	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD",
		Kind:   exchange.KindPerp,
		Bids:   []exchange.Level{{Price: "99", Size: "2"}},
		Asks:   []exchange.Level{{Price: "99.1", Size: "2"}},
	})

	var got exchange.Book
	if err := ex.SubscribeBook(context.Background(), "BTCUSD", func(b exchange.Book) { got = b }); err != nil {
		t.Fatal(err)
	}
	if got.Bids[0].Price != "99" {
		t.Fatalf("expected immediate book, got %+v", got)
	}
}

func TestFake_BookDelayThenSuccess(t *testing.T) {
	ex := fake.New("hyperliquid", exchange.NewManualClock(time.Unix(1, 0).UTC()))
	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD",
		Kind:   exchange.KindPerp,
		Bids:   []exchange.Level{{Price: "100", Size: "1"}},
		Asks:   []exchange.Level{{Price: "100.1", Size: "1"}},
	})
	ex.SetBookDelay(20 * time.Millisecond)

	start := time.Now()
	book, err := ex.SnapshotBook(context.Background(), "BTCUSD")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if book.Bids[0].Price != "100" {
		t.Fatalf("book: %+v", book)
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("expected >= 20ms delay, got %v", elapsed)
	}
}

func TestFake_BookDelayTimeout(t *testing.T) {
	ex := fake.New("hyperliquid", exchange.NewManualClock(time.Unix(1, 0).UTC()))
	ex.SetBook(exchange.Book{
		Symbol: "BTCUSD",
		Kind:   exchange.KindPerp,
		Bids:   []exchange.Level{{Price: "100", Size: "1"}},
		Asks:   []exchange.Level{{Price: "100.1", Size: "1"}},
	})
	ex.SetBookDelay(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := ex.SnapshotBook(ctx, "BTCUSD")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestFake_OrderDelayTimeout(t *testing.T) {
	ex := fake.New("grvt", exchange.NewManualClock(time.Unix(1, 0).UTC()))
	ex.SetOrderDelay(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := ex.PlaceOrder(ctx, exchange.OrderRequest{
		ClientOrderID: "c1",
		Symbol:        "BTCUSD",
		Kind:          exchange.KindPerp,
		Side:          exchange.SideBuy,
		Price:         "100",
		Size:          "1",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}
