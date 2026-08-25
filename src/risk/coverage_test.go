package risk_test

import (
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestParamsFromConfig_DefaultsAndAlign(t *testing.T) {
	cfg := config.Default()
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Risk.PartialFillFactor = 0
		e.Risk.MaxBookAge = 0
	})
	cfg.Risk.MaxInFlight = 0
	p := risk.ParamsFromConfig(cfg)
	if p.PartialFillFactor != 1 || p.MaxInFlight != 4 || p.MaxBookAge <= 0 {
		t.Fatalf("defaults: %+v", p)
	}
	hl := p.Fees.ByVenue["hyperliquid"]
	if hl.Buy.RateBps != 4.5 || hl.Sell.RateBps != 4.5 {
		t.Fatalf("venue fees: %+v", p.Fees)
	}
	g := risk.NewGate(p)
	if g.Params().FeeBpsPerLeg != p.FeeBpsPerLeg {
		t.Fatal("Params copy mismatch")
	}
	_ = risk.FormatFee(risk.LegFee(100, 1, 5))
}

func TestNewGate_ClampsBadParams(t *testing.T) {
	g := risk.NewGate(risk.Params{PartialFillFactor: 2, MaxInFlight: -1})
	p := g.Params()
	if p.PartialFillFactor != 1 || p.MaxInFlight != 4 {
		t.Fatalf("%+v", p)
	}
}

func TestLockKey_HedgeAndEmpty(t *testing.T) {
	if got := risk.LockKey(strategy.Decision{HedgeID: "h1"}); got != "hedge:h1" {
		t.Fatalf("hedge: %s", got)
	}
	if got := risk.LockKey(strategy.Decision{TraceID: "t1"}); got != "trace:t1" {
		t.Fatalf("empty legs: %s", got)
	}
	d := strategy.Decision{Legs: []strategy.Leg{
		{Venue: "grvt", Symbol: "BTCUSD"},
		{Venue: "hyperliquid", Symbol: "BTCUSD"},
		{Venue: "grvt", Symbol: "BTCUSD"},
	}}
	if got := risk.LockKey(d); got != "arb:BTCUSD:grvt|hyperliquid" {
		t.Fatalf("arb key: %s", got)
	}
}

func TestFeeSchedule_ZeroInputs(t *testing.T) {
	s := risk.FeeSchedule{DefaultRateBps: 5, ByVenue: map[exchange.VenueID]risk.VenueFee{
		"hl": risk.SideFee{RateBps: 1, Fixed: 1}.Both(),
	}}
	if s.Cost("hl", exchange.SideBuy, 0, 1) != 0 || s.Cost("hl", exchange.SideBuy, 1, 0) != 0 {
		t.Fatal("zero price/size")
	}
	if (risk.SideFee{RateBps: 5}).Cost(0, 1) != 0 {
		t.Fatal("venue zero")
	}
}
