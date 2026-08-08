package risk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

var _ risk.Risk = (*stubRisk)(nil)

// stubRisk rejects empty legs or any leg with empty price (miss-more stub).
type stubRisk struct{}

func (stubRisk) Evaluate(ctx context.Context, d strategy.Decision) (risk.Verdict, error) {
	if len(d.Legs) == 0 {
		return risk.Verdict{OK: false, Reason: "no legs"}, nil
	}
	for _, leg := range d.Legs {
		if strings.TrimSpace(leg.Price) == "" || strings.TrimSpace(leg.Size) == "" {
			return risk.Verdict{OK: false, Reason: "missing price or size"}, nil
		}
	}
	if d.HedgeID != "" && len(d.Legs) < 2 {
		return risk.Verdict{OK: false, Reason: "hedge requires >= 2 legs"}, nil
	}
	return risk.Verdict{OK: true, Reason: "ok"}, nil
}

func TestRisk_AcceptsValidArbDecision(t *testing.T) {
	r := stubRisk{}
	v, err := r.Evaluate(context.Background(), strategy.Decision{
		TraceID: "t1",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101", Size: "1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Fatalf("want OK, got %+v", v)
	}
}

func TestRisk_RejectsIncompleteHedge(t *testing.T) {
	r := stubRisk{}
	v, err := r.Evaluate(context.Background(), strategy.Decision{
		TraceID: "t1",
		HedgeID: "h1",
		Legs: []strategy.Leg{
			{Venue: "polymarket", Symbol: "BTC-UP", Kind: exchange.KindPrediction, Side: exchange.SideBuy, Price: "0.55", Size: "10"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || v.Reason == "" {
		t.Fatalf("want reject, got %+v", v)
	}
}

func TestRisk_RejectsEmptyLegs(t *testing.T) {
	r := stubRisk{}
	v, err := r.Evaluate(context.Background(), strategy.Decision{TraceID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatalf("want reject empty legs, got %+v", v)
	}
}
