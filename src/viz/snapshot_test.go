package viz_test

import (
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/market"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
	"github.com/nguyenducthinhdl/hhld/src/viz"
)

func TestConfigView_FromDefault(t *testing.T) {
	cfg := config.Default()
	cv := viz.ConfigFrom(cfg, "BTCUSD")
	if cv.VenueA != "hyperliquid" || cv.VenueB != "grvt" {
		t.Fatalf("%+v", cv)
	}
	if cv.EffectiveSize != "0.00003" || cv.MinGap != 0.3 {
		t.Fatalf("%+v", cv)
	}
	if cv.MinSize != "0.000015" || cv.MaxSize != "0.00003" {
		t.Fatalf("size range: %+v", cv)
	}
	hl := cv.FeesByVenue["hyperliquid"]
	if hl.Buy.RateBps != 1 || hl.Sell.RateBps != 1 {
		t.Fatalf("fees: %+v", cv.FeesByVenue)
	}
	gr := cv.FeesByVenue["grvt"]
	if gr.Buy.RateBps != 2 || gr.Sell.RateBps != 2 || gr.Buy.CommissionFixed != 0.01 || gr.Sell.CommissionFixed != 0.01 {
		t.Fatalf("grvt fees: %+v", cv.FeesByVenue["grvt"])
	}
	if cv.Budgets["hyperliquid/BTCUSD"] != "10000" {
		t.Fatalf("budgets: %+v", cv.Budgets)
	}
	if cv.OrderInterval != "1s" {
		t.Fatalf("interval: %q", cv.OrderInterval)
	}
}

func TestGapView_PositiveAndMissingPeer(t *testing.T) {
	a := viz.BookView{
		Venue: "hyperliquid", Ready: true,
		Asks: []viz.LevelView{{Price: "100.1", Size: "1"}},
		Bids: []viz.LevelView{{Price: "100.0", Size: "1"}},
	}
	b := viz.BookView{
		Venue: "grvt", Ready: true,
		Asks: []viz.LevelView{{Price: "100.9", Size: "1"}},
		Bids: []viz.LevelView{{Price: "100.8", Size: "1"}},
	}
	g := viz.ComputeGap(a, b, 0.3)
	if !g.Ready || g.Value < 0.6 || !g.AboveMin {
		t.Fatalf("%+v", g)
	}
	missing := viz.ComputeGap(a, viz.BookView{Venue: "grvt"}, 0.3)
	if missing.Ready {
		t.Fatalf("want not ready: %+v", missing)
	}
}

func TestBuildSnapshot_AndSignal(t *testing.T) {
	cfg := config.Default()
	store := market.NewBookStore()
	ts := time.Unix(1_700_000_000, 0).UTC()
	_, _ = store.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "100.0", Size: "2"}},
		Asks: []exchange.Level{{Price: "100.1", Size: "2"}},
	}))
	_, _ = store.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "100.8", Size: "2"}},
		Asks: []exchange.Level{{Price: "100.9", Size: "2"}},
	}))
	sigs := viz.NewSignalLog()
	ticks := viz.NewTickRing(8)
	ticks.Push(exchange.Tick{
		Venue: "hyperliquid", Symbol: "BTCUSD", Price: "100.05", Size: "0.1",
		Side: exchange.SideBuy, Time: ts,
	})
	src := viz.Source{Cfg: cfg, Store: store, Signals: sigs, Ticks: ticks}
	snap := src.BuildSnapshot("BTCUSD")
	if !snap.VenueA.Ready || !snap.VenueB.Ready || snap.Gap == nil || !snap.Gap.Ready {
		t.Fatalf("%+v", snap)
	}
	if snap.VenueA.BestAsk != "100.1" || snap.VenueA.BestAskSize != "2" || snap.VenueA.NativeSymbol != "BTC" {
		t.Fatalf("venue A tob: %+v", snap.VenueA)
	}
	if snap.VenueB.BestBid != "100.8" || snap.VenueB.NativeSymbol != "BTC_USDT_Perp" {
		t.Fatalf("venue B tob: %+v", snap.VenueB)
	}
	if snap.VenueA.LatencyMs < 0 || snap.VenueA.LatencyMs > 5000 {
		t.Fatalf("venue A latency: %d", snap.VenueA.LatencyMs)
	}
	if snap.Config.MinGap != 0.3 {
		t.Fatalf("config: %+v", snap.Config)
	}
	if len(snap.Ticks) != 1 {
		t.Fatalf("ticks: %+v", snap.Ticks)
	}

	sigs.NotifyDecision("BTCUSD", strategy.Decision{
		TraceID: "arb-1", Reason: "gap ok",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Side: exchange.SideBuy, Price: "100.1", Size: "1"},
			{Venue: "grvt", Side: exchange.SideSell, Price: "100.8", Size: "1"},
		},
	}, 0.7)
	snap2 := src.BuildSnapshot("BTCUSD")
	if snap2.Signal == nil || snap2.Signal.Kind != "decision" || snap2.Signal.TraceID != "arb-1" {
		t.Fatalf("signal: %+v", snap2.Signal)
	}
	sigs.NotifyMiss("BTCUSD", "rate_limited", nil)
	snap3 := src.BuildSnapshot("BTCUSD")
	if snap3.Signal.Kind != "miss" || snap3.Signal.Reason != "rate_limited" {
		t.Fatalf("miss: %+v", snap3.Signal)
	}
	sigs.NotifyMiss("BTCUSD", "stale_book", map[string]any{"gap_time": int64(5123), "venue": "hyperliquid"})
	snap4 := src.BuildSnapshot("BTCUSD")
	if snap4.Signal.GapTime != 5123 || snap4.Signal.Reason != "stale_book" || snap4.Signal.Venue != "hyperliquid" {
		t.Fatalf("stale gap_time: %+v", snap4.Signal)
	}
}
