package risk_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// TestVenueFee_RateFixedCommission covers exchange-dependent fee shapes.
//
// Why: venues differ — some charge bps, some flat, some add commission
// (spec/trading.md; miss-more must sum worst-case costs per leg).
func TestVenueFee_RateFixedCommission(t *testing.T) {
	rate := risk.SideFee{RateBps: 5}
	if got := rate.Cost(100, 1); math.Abs(got-0.05) > 1e-12 {
		t.Fatalf("rate: got %v want 0.05", got)
	}
	fixed := risk.SideFee{Fixed: 0.10}
	if got := fixed.Cost(100, 1); math.Abs(got-0.10) > 1e-12 {
		t.Fatalf("fixed: got %v want 0.10", got)
	}
	mixed := risk.SideFee{RateBps: 3.5, CommissionFixed: 0.01, CommissionBps: 1}
	// 100*3.5/10000 + 0.01 + 100*1/10000 = 0.035 + 0.01 + 0.01 = 0.055
	if got := mixed.Cost(100, 1); math.Abs(got-0.055) > 1e-12 {
		t.Fatalf("mixed: got %v want 0.055", got)
	}
}

func TestFeeSchedule_PerVenueAndDefault(t *testing.T) {
	s := risk.FeeSchedule{
		DefaultRateBps: 5,
		ByVenue: map[exchange.VenueID]risk.VenueFee{
			"hyperliquid": risk.SideFee{RateBps: 3.5}.Both(),
			"grvt":        risk.SideFee{Fixed: 0.02}.Both(),
		},
	}
	hl := s.Cost("hyperliquid", exchange.SideBuy, 100, 1)
	if math.Abs(hl-0.035) > 1e-12 {
		t.Fatalf("hl: %v", hl)
	}
	grvt := s.Cost("grvt", exchange.SideSell, 100, 1)
	if math.Abs(grvt-0.02) > 1e-12 {
		t.Fatalf("grvt fixed: %v", grvt)
	}
	other := s.Cost("unknown", exchange.SideBuy, 100, 1)
	if math.Abs(other-0.05) > 1e-12 {
		t.Fatalf("default: %v", other)
	}
}

func TestFeeSchedule_BuyVsSell(t *testing.T) {
	s := risk.FeeSchedule{ByVenue: map[exchange.VenueID]risk.VenueFee{
		"hl": {Buy: risk.SideFee{RateBps: 10}, Sell: risk.SideFee{RateBps: 1}},
	}}
	buy := s.Cost("hl", exchange.SideBuy, 100, 1)
	sell := s.Cost("hl", exchange.SideSell, 100, 1)
	if math.Abs(buy-0.10) > 1e-12 || math.Abs(sell-0.01) > 1e-12 {
		t.Fatalf("buy=%v sell=%v", buy, sell)
	}
}

// TestGate_UsesPerVenueFees ensures Evaluate costs each leg with its venue+side schedule.
func TestGate_UsesPerVenueFees(t *testing.T) {
	// Gross edge 1.0; HL buy 5bps on 100 = 0.05; GRVT sell fixed 0.9 → net 0.05 OK.
	// Opposite sides are punitive so a wrong-side lookup would reject.
	gOK := risk.NewGate(risk.Params{
		Fees: risk.FeeSchedule{ByVenue: map[exchange.VenueID]risk.VenueFee{
			"hyperliquid": {Buy: risk.SideFee{RateBps: 5}, Sell: risk.SideFee{RateBps: 1000}},
			"grvt":        {Buy: risk.SideFee{Fixed: 100}, Sell: risk.SideFee{Fixed: 0.9}},
		}},
		LatencyPenalty: 0, PartialFillFactor: 1, MaxBookAge: 2 * time.Second, MaxInFlight: 4,
	})
	// Same but GRVT sell fixed 0.96 → net negative
	gBad := risk.NewGate(risk.Params{
		Fees: risk.FeeSchedule{ByVenue: map[exchange.VenueID]risk.VenueFee{
			"hyperliquid": {Buy: risk.SideFee{RateBps: 5}, Sell: risk.SideFee{RateBps: 1000}},
			"grvt":        {Buy: risk.SideFee{Fixed: 100}, Sell: risk.SideFee{Fixed: 0.96}},
		}},
		LatencyPenalty: 0, PartialFillFactor: 1, MaxBookAge: 2 * time.Second, MaxInFlight: 4,
	})
	now := time.Unix(1000, 0).UTC()
	d := strategy.Decision{
		TraceID: "t-fee",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100.0", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101.0", Size: "1"},
		},
	}
	mkt := risk.MarketView{Books: []exchange.Book{
		{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Asks: []exchange.Level{{Price: "100", Size: "1"}}, Time: now},
		{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Bids: []exchange.Level{{Price: "101", Size: "1"}}, Time: now},
	}, Now: now}

	v, err := gOK.Evaluate(context.Background(), d, mkt)
	if err != nil || !v.OK {
		t.Fatalf("want OK with mixed fees, got %+v err=%v", v, err)
	}
	v2, err := gBad.Evaluate(context.Background(), d, mkt)
	if err != nil || v2.OK {
		t.Fatalf("want negative_edge reject, got %+v", v2)
	}
}
