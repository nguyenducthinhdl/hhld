package risk_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
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
	if v.OK || !strings.Contains(v.Reason, "negative_edge") {
		t.Fatalf("want negative_edge reject, got %+v", v)
	}
	if !strings.Contains(v.Reason, "net=") || !strings.Contains(v.Reason, "buy=") {
		t.Fatalf("want negative_edge detail, got %q", v.Reason)
	}
	if v.FloatInfo("net") >= 0 {
		t.Fatalf("want negative net in Info, got %+v", v)
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
	if !strings.Contains(v.Reason, "gap_time=") || !strings.Contains(v.Reason, "venue=") {
		t.Fatalf("want stale_book detail, got %q", v.Reason)
	}
	if v.GapTimeMS() != 5000 {
		t.Fatalf("want gap_time 5000ms, got %d info=%v", v.GapTimeMS(), v.Info)
	}
	if v.StringInfo("venue") == "" {
		t.Fatalf("want stale venue, got %+v", v)
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

func TestGate_RateLimited(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
		OrderInterval: map[exchange.Symbol]time.Duration{"BTCUSD": time.Second},
	})
	now := time.Unix(1000, 0).UTC()
	mkt := risk.MarketView{Books: freshBooks(now), Now: now}
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), mkt)
	if err != nil || !v.OK {
		t.Fatalf("first: %+v err=%v", v, err)
	}
	v2, err := g.Evaluate(context.Background(), sampleArbDecision(), risk.MarketView{
		Books: freshBooks(now.Add(100 * time.Millisecond)), Now: now.Add(100 * time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.OK || !strings.Contains(v2.Reason, "rate_limited") {
		t.Fatalf("want rate_limited, got %+v", v2)
	}
	v3, err := g.Evaluate(context.Background(), sampleArbDecision(), risk.MarketView{
		Books: freshBooks(now.Add(2 * time.Second)), Now: now.Add(2 * time.Second),
	})
	if err != nil || !v3.OK {
		t.Fatalf("after interval: %+v err=%v", v3, err)
	}
}

func TestGate_BudgetExceeded(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
		Budgets: map[string]float64{
			"hyperliquid/BTCUSD": 150, // first leg 100*1=100 OK; second Decision would need +100
			"grvt/BTCUSD":        10000,
		},
	})
	now := time.Unix(1000, 0).UTC()
	mkt := risk.MarketView{Books: freshBooks(now), Now: now}
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), mkt)
	if err != nil || !v.OK {
		t.Fatalf("first: %+v err=%v", v, err)
	}
	v2, err := g.Evaluate(context.Background(), sampleArbDecision(), mkt)
	if err != nil {
		t.Fatal(err)
	}
	if v2.OK || !strings.Contains(v2.Reason, "budget_exceeded") || !strings.Contains(v2.Reason, "hyperliquid/BTCUSD") {
		t.Fatalf("want budget_exceeded, got %+v", v2)
	}
}

func TestGate_MaxVolumeExceeded(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
		MaxVolumeTrade: map[exchange.Symbol]float64{"BTCUSD": 0.5},
	})
	now := time.Unix(1000, 0).UTC()
	d := sampleArbDecision() // size "1"
	v, err := g.Evaluate(context.Background(), d, risk.MarketView{Books: freshBooks(now), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !strings.Contains(v.Reason, "max_volume_exceeded") {
		t.Fatalf("want max_volume_exceeded, got %+v", v)
	}
}

func TestParamsFromConfig_BudgetsAndIntervals(t *testing.T) {
	cfg := config.Default()
	p := risk.ParamsFromConfig(cfg)
	if p.Budgets["hyperliquid/BTCUSD"] != 10000 {
		t.Fatalf("budgets: %+v", p.Budgets)
	}
	if p.OrderInterval["BTCUSD"] != time.Second {
		t.Fatalf("interval: %+v", p.OrderInterval)
	}
	if p.MaxVolumeTrade["BTCUSD"] != 0.0003 {
		t.Fatalf("max vol: %+v", p.MaxVolumeTrade)
	}
	if p.MinNotional["BTCUSD"] != 10 || p.MaxNotional["BTCUSD"] != 50 {
		t.Fatalf("notional: min=%v max=%v", p.MinNotional, p.MaxNotional)
	}
}

func TestGate_NotionalBelowMin(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
		MinNotional: map[exchange.Symbol]float64{"BTCUSD": 10},
		MaxNotional: map[exchange.Symbol]float64{"BTCUSD": 50},
	})
	now := time.Unix(1000, 0).UTC()
	d := strategy.Decision{
		TraceID: "t-ntl",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100000", Size: "0.00005"},
		},
	}
	v, err := g.Evaluate(context.Background(), d, risk.MarketView{Books: freshBooks(now), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !strings.Contains(v.Reason, "notional_below_min") {
		t.Fatalf("want notional_below_min, got %+v", v)
	}
}

func TestGate_NotionalAboveMax(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
		MinNotional: map[exchange.Symbol]float64{"BTCUSD": 10},
		MaxNotional: map[exchange.Symbol]float64{"BTCUSD": 50},
	})
	now := time.Unix(1000, 0).UTC()
	d := strategy.Decision{
		TraceID: "t-ntl",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100000", Size: "0.001"},
		},
	}
	v, err := g.Evaluate(context.Background(), d, risk.MarketView{Books: freshBooks(now), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !strings.Contains(v.Reason, "notional_above_max") {
		t.Fatalf("want notional_above_max, got %+v", v)
	}
}

func TestGate_HaltBlocksEvaluateUntilResume(t *testing.T) {
	g := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 5, LatencyPenalty: 0.05, PartialFillFactor: 1,
		MaxBookAge: 2 * time.Second, MaxInFlight: 4,
	})
	now := time.Unix(1000, 0).UTC()
	mkt := risk.MarketView{Books: freshBooks(now), Now: now}
	g.Halt("BTCUSD", "unpaired:t-arb")
	v, err := g.Evaluate(context.Background(), sampleArbDecision(), mkt)
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !strings.Contains(v.Reason, "halted") {
		t.Fatalf("want halted, got %+v", v)
	}
	halts := g.Halted()
	if halts["BTCUSD"] != "unpaired:t-arb" {
		t.Fatalf("halts: %+v", halts)
	}
	g.Resume("BTCUSD")
	v, err = g.Evaluate(context.Background(), sampleArbDecision(), mkt)
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Fatalf("want OK after resume, got %+v", v)
	}
	g.Halt("", "ignored")
	g.Halt("ETHUSD", "")
	if g.Halted()["ETHUSD"] != "halt" {
		t.Fatalf("empty reason default: %+v", g.Halted())
	}
	g.Resume("ETHUSD")
	if len(g.Halted()) != 0 {
		t.Fatalf("want empty halts, got %+v", g.Halted())
	}
}
