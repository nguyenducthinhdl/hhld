package risk_test

import (
	"math"
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestExplainDecision_GapAndFees(t *testing.T) {
	p := risk.Params{
		Fees: risk.FeeSchedule{ByVenue: map[exchange.VenueID]risk.VenueFee{
			"hyperliquid": {RateBps: 1},
			"grvt":        {RateBps: 2, CommissionFixed: 0.01},
		}},
		LatencyPenalty:    0.05,
		PartialFillFactor: 1,
	}
	d := strategy.Decision{Legs: []strategy.Leg{
		{Venue: "hyperliquid", Side: exchange.SideBuy, Price: "100.1", Size: "1"},
		{Venue: "grvt", Side: exchange.SideSell, Price: "101.0", Size: "1"},
	}}
	ex, ok := risk.ExplainDecision(d, p)
	if !ok {
		t.Fatal("want explain")
	}
	if ex.Size != 1 || ex.Gross < 0.89 || ex.Gross > 0.91 {
		t.Fatalf("%+v", ex)
	}
	if len(ex.Legs) != 2 || ex.Legs[0].Book != "ask" || ex.Legs[1].Book != "bid" {
		t.Fatalf("legs: %+v", ex.Legs)
	}
	// HL rate 1bps * 100.1; GRVT 2bps * 101 + 0.01
	wantFee := 100.1*0.0001 + 101*0.0002 + 0.01
	if math.Abs(ex.Fee-wantFee) > 1e-9 {
		t.Fatalf("fee %v want %v", ex.Fee, wantFee)
	}
	if math.Abs(ex.Legs[1].CommissionFixed-0.01) > 1e-12 {
		t.Fatalf("commission: %+v", ex.Legs[1])
	}
	if ex.Formula == "" || ex.Net >= ex.Gross {
		t.Fatalf("formula/net: %+v", ex)
	}
}
