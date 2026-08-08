package contract_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

// Verifies P1 contracts compose: Strategy Decision → Risk → OrderRequest legs.
func TestDecisionFlowsToOrderRequests(t *testing.T) {
	ctx := context.Background()

	d := strategy.Decision{
		TraceID: "trace-1",
		HedgeID: "hedge-1",
		Legs: []strategy.Leg{
			{Venue: "polymarket", Symbol: "BTC-UP", Kind: exchange.KindPrediction, Side: exchange.SideBuy, Price: "0.55", Size: "10"},
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100", Size: "1"},
		},
		Reason: "hedge",
	}

	v, err := acceptRisk{}.Evaluate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Fatalf("risk rejected: %+v", v)
	}

	reqs := legsToOrderRequests(d)
	if len(reqs) != 2 {
		t.Fatalf("want 2 order requests, got %d", len(reqs))
	}
	for i, req := range reqs {
		if req.TraceID != d.TraceID || req.HedgeID != d.HedgeID {
			t.Fatalf("req %d ids mismatch: %+v", i, req)
		}
		if req.Symbol != d.Legs[i].Symbol || req.Side != d.Legs[i].Side {
			t.Fatalf("req %d field mismatch: %+v vs %+v", i, req, d.Legs[i])
		}
	}
}

type acceptRisk struct{}

func (acceptRisk) Evaluate(ctx context.Context, d strategy.Decision) (risk.Verdict, error) {
	if len(d.Legs) == 0 {
		return risk.Verdict{OK: false, Reason: "no legs"}, nil
	}
	return risk.Verdict{OK: true, Reason: "ok"}, nil
}

func legsToOrderRequests(d strategy.Decision) []exchange.OrderRequest {
	out := make([]exchange.OrderRequest, 0, len(d.Legs))
	for i, leg := range d.Legs {
		out = append(out, exchange.OrderRequest{
			ClientOrderID: time.Unix(int64(i+1), 0).UTC().Format("cli-20060102") + "-" + string(leg.Venue),
			TraceID:       d.TraceID,
			HedgeID:       d.HedgeID,
			Symbol:        leg.Symbol,
			Kind:          leg.Kind,
			Side:          leg.Side,
			Price:         leg.Price,
			Size:          leg.Size,
		})
	}
	return out
}
