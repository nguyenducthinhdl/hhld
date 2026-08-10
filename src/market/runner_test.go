package market_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/market"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func waitOnBooks(t *testing.T, r *market.Runner, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.OnBooksCalls() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want OnBooksCalls>=%d, got %d (peerMiss=%d)", n, r.OnBooksCalls(), r.PeerMisses())
}

func TestRunner_EvaluatesOnEachVenueUpdate(t *testing.T) {
	store := market.NewBookStore()
	bus := market.NewBus(64)
	defer bus.Close()

	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"}, Size: "1", MinGap: 0.3,
	})
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0.01, PartialFillFactor: 1,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	tr := pnl.NewMemory()
	run, err := market.NewRunner(market.RunnerConfig{
		VenueA: "hyperliquid", VenueB: "grvt",
		Symbols:  []exchange.Symbol{"BTCUSD"},
		Store:    store,
		Strategy: arb,
		Risk:     gate,
		Venues:   strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B},
		Tracker:  tr,
	})
	if err != nil {
		t.Fatal(err)
	}
	run.AttachBus(bus)

	ts := time.Unix(1_700_000_000, 0).UTC()
	// Only HL — peer miss
	bus.Publish(market.SnapshotEvent(exchange.Book{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
		Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
	}))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && run.PeerMisses() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if run.PeerMisses() == 0 {
		t.Fatal("want peer miss on A-only")
	}
	if run.OnBooksCalls() != 0 {
		t.Fatalf("OnBooks should not run yet, got %d", run.OnBooksCalls())
	}

	// Add GRVT with gap → first OnBooks
	bus.Publish(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "101.0", Size: "2"}},
		Asks: []exchange.Level{{Price: "101.1", Size: "2"}},
	}))
	waitOnBooks(t, run, 1)

	// Update GRVT again → second OnBooks
	bus.Publish(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts.Add(time.Second),
		Bids: []exchange.Level{{Price: "101.2", Size: "2"}},
		Asks: []exchange.Level{{Price: "101.3", Size: "2"}},
	}))
	waitOnBooks(t, run, 2)
}

func TestRunner_LockBusyUnderBurst(t *testing.T) {
	store := market.NewBookStore()
	bus := market.NewBus(1024)
	defer bus.Close()

	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())

	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"}, Size: "1", MinGap: 0.3,
	})
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	run, err := market.NewRunner(market.RunnerConfig{
		VenueA: "hyperliquid", VenueB: "grvt",
		Symbols:  []exchange.Symbol{"BTCUSD"},
		Store:    store,
		Strategy: arb,
		Risk:     gate,
		Venues:   strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B},
		Tracker:  pnl.NewMemory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	run.AttachBus(bus)

	ts := time.Unix(1_700_000_000, 0).UTC()
	seed := func(venue exchange.VenueID, bid string) {
		bus.Publish(market.SnapshotEvent(exchange.Book{
			Venue: venue, Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
			Bids: []exchange.Level{{Price: bid, Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
		}))
	}
	seed("hyperliquid", "100.0")
	seed("grvt", "101.0")
	waitOnBooks(t, run, 1)

	d := strategy.Decision{
		TraceID: "t",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101", Size: "1"},
		},
	}
	// Hold Gate while flooding events: OnBooks still runs; place path must hit lock_busy (not skip Risk).
	rel, v := gate.TryAcquire(d)
	if !v.OK {
		t.Fatalf("hold acquire: %+v", v)
	}
	before := run.OnBooksCalls()
	for i := 0; i < 20; i++ {
		seed("grvt", "101.0")
	}
	waitOnBooks(t, run, before+1)

	var busy atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, v2 := gate.TryAcquire(d)
			if !v2.OK && v2.Reason == "lock_busy" {
				busy.Add(1)
			}
		}()
	}
	wg.Wait()
	rel()
	if busy.Load() == 0 {
		t.Fatal("expected lock_busy under burst")
	}
}

func TestBus_DropsWhenFull(t *testing.T) {
	bus := market.NewBus(1)
	defer bus.Close()
	// Block handler so queue backs up
	block := make(chan struct{})
	bus.Subscribe(func(ev market.BookEvent) { <-block })
	bus.Publish(market.SnapshotEvent(exchange.Book{Venue: "a", Symbol: "S"}))
	// Give dispatch time to pull one and block
	time.Sleep(20 * time.Millisecond)
	bus.Publish(market.SnapshotEvent(exchange.Book{Venue: "a", Symbol: "S"}))
	bus.Publish(market.SnapshotEvent(exchange.Book{Venue: "a", Symbol: "S"}))
	time.Sleep(20 * time.Millisecond)
	if bus.Dropped() == 0 {
		t.Fatal("want drops when full")
	}
	close(block)
}
