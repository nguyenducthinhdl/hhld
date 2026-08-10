package sim_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/sim"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// testBooks builds a two-step dual-venue book history for paper arb backtests.
//
// Why: Replay groups books by time into steps (see sim.Replay / spec/roadmap/p6.md).
//   - t0: no tradeable gap (min_gap 0.3) — strategy should stay quiet.
//   - t1: clear gap buy HL ask 100.1 vs sell GRVT bid 101.0 — matches the
//     paper-arb pipeline in spec/trading.md (Strategy → Risk → place → PnL).
func testBooks() []exchange.Book {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	t1 := t0.Add(time.Second)
	return []exchange.Book{
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
			Time: t0,
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.2", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.3", Size: "2"}},
			Time: t0,
		},
		// Later step: arb gap (buy HL 100.1, sell GRVT 101.0) — see spec/trading.md.
		{
			Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
			Time: t1,
		},
		{
			Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
			Bids: []exchange.Level{{Price: "101.0", Size: "2"}},
			Asks: []exchange.Level{{Price: "101.1", Size: "2"}},
			Time: t1,
		},
	}
}

// TestReplay_RunAccumulatesPnL checks Simulator.Run end-to-end on gapped books.
//
// Why: P6 done-when requires Run(input, strategy, risk, tracker) → PnL snapshot
// (spec/roadmap/p6.md). This proves backtest reuses the same Strategy/Risk/paper
// path as live paper trading (spec/tech-stack.md Backtesting; spec/trading.md pipeline),
// and that successful dual-leg paper fills move realized PnL (spec/roadmap/p5.md).
func TestReplay_RunAccumulatesPnL(t *testing.T) {
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Size:    "1",
		MinGap:  0.3,
	})
	// Mild costs so miss-more gate still allows the ~0.9 raw gap (spec/networking.md miss-more).
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0.01, PartialFillFactor: 1,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	tracker := pnl.NewMemory()
	rep := sim.NewReplay(nil)

	snap, err := rep.Run(context.Background(), sim.Input{Books: testBooks()}, arb, gate, tracker)
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.ParseFloat(snap.Realized, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got <= 0 {
		t.Fatalf("want positive realized PnL from arb, got %s", snap.Realized)
	}
	if len(tracker.Fills()) < 2 {
		t.Fatalf("want fills from paper legs, got %d", len(tracker.Fills()))
	}
}

// TestReplay_WinningDistribution checks Analyzer sample shape and empirical win rate.
//
// Why: Risk calibration needs WinningDistribution over
// (symbol, gap, volume1, volume2, exchange1, exchange2, time) — see spec/tech-stack.md
// and spec/trading.md “Simulation: winning rate and distribution”. Research/ML stays
// in a side project; this Go Analyzer is the empirical hook (spec/roadmap/p6.md).
// Filter{Symbol: BTCUSD} exercises multi-symbol-ready filtering from config.
func TestReplay_WinningDistribution(t *testing.T) {
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Size:    "1",
		MinGap:  0.3,
	})
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0.01, PartialFillFactor: 1,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	rep := sim.NewReplay(nil)

	dist, err := rep.WinningDistribution(context.Background(), sim.Input{Books: testBooks()}, arb, gate, sim.Filter{
		Symbol: "BTCUSD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dist.Samples) == 0 {
		t.Fatal("expected at least one win sample from gapped books")
	}
	s := dist.Samples[0]
	if s.Dims.Symbol != "BTCUSD" || s.Dims.Exchange1 == "" || s.Dims.Exchange2 == "" {
		t.Fatalf("dims: %+v", s.Dims)
	}
	if s.Dims.Gap < 0.3 {
		t.Fatalf("gap too small: %v", s.Dims.Gap)
	}
	if !s.Won {
		t.Fatalf("expected winning sample, pnl=%s", s.PnL)
	}
	if dist.WinRate <= 0 || dist.WinRate > 1 {
		t.Fatalf("win rate: %v", dist.WinRate)
	}

	// WinningRate is the scalar view of the same Analyzer contract (spec/roadmap/p6.md).
	rate, err := rep.WinningRate(context.Background(), sim.Input{Books: testBooks()}, arb, gate, sim.Filter{Symbol: "BTCUSD"})
	if err != nil {
		t.Fatal(err)
	}
	if rate != dist.WinRate {
		// Independent replay run — still must be a positive empirical rate for this fixture.
		if rate <= 0 {
			t.Fatalf("winning rate %v", rate)
		}
	}
}

// TestReplay_RiskRejectsNegativeEdge ensures miss-more Risk drops decisions from the distribution.
//
// Why: Even when Strategy emits a gap Decision, Risk.Evaluate may reject under harsh
// fees/latency/partial-fill (spec/mission.md risk doctrine; spec/trading.md “Risk before place”).
// Analyzer must not count rejected opportunities as wins — preferred behavior is miss
// (spec/roadmap/p4.md, spec/networking.md). Empty samples prove sim respects the hard gate.
func TestReplay_RiskRejectsNegativeEdge(t *testing.T) {
	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"},
		Size:    "1",
		MinGap:  0.3,
	})
	// Harsh costs → Evaluate rejects even when strategy emits a Decision.
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 100, LatencyPenalty: 5, PartialFillFactor: 0.5,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	rep := sim.NewReplay(nil)
	dist, err := rep.WinningDistribution(context.Background(), sim.Input{Books: testBooks()}, arb, gate, sim.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dist.Samples) != 0 {
		t.Fatalf("expected no samples when risk rejects all, got %+v", dist)
	}
}
