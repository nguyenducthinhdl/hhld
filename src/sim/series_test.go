package sim_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/sim"
)

func TestTrace_SampleGapAndMinGap(t *testing.T) {
	in, err := sim.InputFromNDJSON("../../data/samples/btcusd_books.ndjson", "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Risk.MaxBookAge = config.Duration(24 * time.Hour)
		e.Trading.MinSize = "0.01"
		e.Trading.MaxSize = "1"
	})
	series, err := sim.Trace(context.Background(), in, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if series.VenueA != "hyperliquid" || series.VenueB != "grvt" {
		t.Fatalf("venues: %+v", series)
	}
	if len(series.Steps) < 2 {
		t.Fatalf("steps: %+v", series.Steps)
	}
	var sawGap, sawDecision bool
	for _, st := range series.Steps {
		if st.Gap != nil && st.Gap.Ready && st.Gap.Value > 0.5 {
			sawGap = true
		}
		if st.Signal != nil && st.Signal.Kind == "decision" {
			sawDecision = true
		}
	}
	if !sawGap {
		t.Fatalf("want a positive gap: %+v", series.Steps)
	}
	if !sawDecision {
		t.Fatalf("want a decision: %+v", series.Steps[len(series.Steps)-1])
	}

	high := 10.0
	cfg2 := sim.ApplyOverlay(cfg, sim.Overlay{MinGap: &high})
	series2, err := sim.Trace(context.Background(), in, cfg2)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range series2.Steps {
		if st.Signal != nil && st.Signal.Kind == "decision" {
			t.Fatalf("high min_gap should not trade: %+v", st)
		}
	}
}

func TestTrace_GenericVenuesAndIgnoreThird(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	in := sim.Input{Books: []exchange.Book{
		{Venue: "ex_a", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: t0,
			Bids: []exchange.Level{{Price: "100", Size: "2"}},
			Asks: []exchange.Level{{Price: "100.1", Size: "2"}}},
		{Venue: "ex_b", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: t0,
			Bids: []exchange.Level{{Price: "101", Size: "2"}},
			Asks: []exchange.Level{{Price: "101.1", Size: "2"}}},
		{Venue: "ex_c", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: t0,
			Bids: []exchange.Level{{Price: "999", Size: "9"}},
			Asks: []exchange.Level{{Price: "1000", Size: "9"}}},
	}}
	cfg := config.Default()
	cfg.Venues.A, cfg.Venues.B = "ex_a", "ex_b"
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Risk.MaxBookAge = config.Duration(24 * time.Hour)
		if e.Venues == nil {
			e.Venues = map[string]config.VenueSpec{}
		}
		e.Venues["ex_a"] = config.VenueSpec{SymbolName: "BTC", Fees: config.VenueFee{RateBps: 1}}
		e.Venues["ex_b"] = config.VenueSpec{SymbolName: "BTC", Fees: config.VenueFee{RateBps: 1}}
	})
	series, err := sim.Trace(context.Background(), in, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if series.VenueA != "ex_a" || series.VenueB != "ex_b" {
		t.Fatalf("%+v", series)
	}
	if len(series.Steps) != 1 {
		t.Fatalf("steps: %+v", series.Steps)
	}
	if series.Steps[0].VenueA.BestAsk != "100.1" || series.Steps[0].VenueB.BestBid != "101" {
		t.Fatalf("tob: %+v", series.Steps[0])
	}
	if series.Steps[0].Gap == nil || !series.Steps[0].Gap.Ready || series.Steps[0].Gap.Value < 0.8 {
		t.Fatalf("gap: %+v", series.Steps[0].Gap)
	}
	ex := series.Steps[0].Explain
	if ex == nil || ex.Formula == "" || len(ex.Legs) != 2 {
		t.Fatalf("explain: %+v", ex)
	}
	if ex.Legs[0].Side != "buy" || ex.Legs[1].Side != "sell" {
		t.Fatalf("sides: %+v", ex.Legs)
	}
	if ex.Legs[0].Venue != "ex_a" || ex.Legs[1].Venue != "ex_b" {
		t.Fatalf("venues: %+v", ex.Legs)
	}
	if ex.Fee <= 0 || ex.Gross <= 0 || ex.Net >= ex.Gross {
		t.Fatalf("economics: %+v", ex)
	}
}
