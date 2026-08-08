package risk_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func sampleArbDecision() strategy.Decision {
	return strategy.Decision{
		TraceID: "t-arb",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100.0", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101.0", Size: "1"},
		},
	}
}

func freshBooks(now time.Time) []exchange.Book {
	return []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "99.9", Size: "1"}},
			Asks: []exchange.Level{{Price: "100.0", Size: "1"}},
			Time: now,
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "101.0", Size: "1"}},
			Asks: []exchange.Level{{Price: "101.1", Size: "1"}},
			Time: now,
		},
	}
}

func TestGate_AcceptsPositiveEdge(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 5, LatencyPenalty: 0.05, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
	})
	now := time.Unix(1000, 0).UTC()
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), risk.MarketView{
		Books: freshBooks(now), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Fatalf("want OK, got %+v", v)
	}
}

func TestGate_RejectsNegativeEdge(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 50, LatencyPenalty: 2, PartialFillFactor: 0.5,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
	})
	now := time.Unix(1000, 0).UTC()
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), risk.MarketView{
		Books: freshBooks(now), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || v.Reason == "" {
		t.Fatalf("want negative_edge reject, got %+v", v)
	}
}

func TestGate_RejectsStaleBook(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 1 * time.Second, MaxInFlight: 4,
	})
	now := time.Unix(1000, 0).UTC()
	books := freshBooks(now.Add(-5 * time.Second))
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), risk.MarketView{
		Books: books, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !strings.Contains(v.Reason, "stale_book") {
		t.Fatalf("want stale_book, got %+v", v)
	}
}

func TestGate_RejectsUnhealthyVenue(t *testing.T) {
	g := risk.NewGate(risk.DefaultParams())
	now := time.Unix(1000, 0).UTC()
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), risk.MarketView{
		Books: freshBooks(now), Now: now,
		Unhealthy: map[exchange.VenueID]bool{"grvt": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !strings.Contains(v.Reason, "venue_unhealthy") {
		t.Fatalf("want venue_unhealthy, got %+v", v)
	}
}

func TestGate_RejectsIncompleteHedge(t *testing.T) {
	g := risk.NewGate(risk.DefaultParams())
	v, err := g.Evaluate(context.Background(), strategy.Decision{
		TraceID: "t1", HedgeID: "h1",
		Legs: []strategy.Leg{
			{Venue: "polymarket", Symbol: "BTC-UP", Kind: exchange.KindPrediction, Side: exchange.SideBuy, Price: "0.55", Size: "10"},
		},
	}, risk.MarketView{})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatalf("want reject, got %+v", v)
	}
}

func TestGate_TryAcquire_LockBusy(t *testing.T) {
	g := risk.NewGate(risk.Params{MaxInFlight: 4, PartialFillFactor: 1})
	d := sampleArbDecision()
	release, v := g.TryAcquire(d)
	if !v.OK || release == nil {
		t.Fatalf("first acquire: %+v", v)
	}
	_, v2 := g.TryAcquire(d)
	if v2.OK || v2.Reason != "lock_busy" {
		t.Fatalf("want lock_busy, got %+v", v2)
	}
	release()
	release2, v3 := g.TryAcquire(d)
	if !v3.OK {
		t.Fatalf("after release: %+v", v3)
	}
	release2()
}

func TestGate_TryAcquire_Overloaded(t *testing.T) {
	g := risk.NewGate(risk.Params{MaxInFlight: 1, PartialFillFactor: 1})
	d1 := sampleArbDecision()
	d1.Legs[0].Symbol = "BTCUSD"
	d2 := sampleArbDecision()
	d2.TraceID = "t2"
	d2.Legs[0].Symbol = "ETHUSD"
	d2.Legs[1].Symbol = "ETHUSD"

	rel1, v := g.TryAcquire(d1)
	if !v.OK {
		t.Fatal(v)
	}
	_, v2 := g.TryAcquire(d2)
	if v2.OK || v2.Reason != "overloaded" {
		t.Fatalf("want overloaded, got %+v", v2)
	}
	rel1()
}

func TestGate_ConcurrentSameKeySerialized(t *testing.T) {
	g := risk.NewGate(risk.Params{MaxInFlight: 8, PartialFillFactor: 1})
	d := sampleArbDecision()
	var busy int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, v := g.TryAcquire(d)
			if !v.OK {
				if v.Reason == "lock_busy" {
					mu.Lock()
					busy++
					mu.Unlock()
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
			rel()
		}()
	}
	wg.Wait()
	if busy == 0 {
		t.Fatal("expected some lock_busy misses under contention")
	}
}
