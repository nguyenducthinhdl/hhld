package sim_test

import (
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/sim"
)

func TestApplyOverlay_PreservesVenueFeeComponents(t *testing.T) {
	cfg := config.Default()
	got := sim.ApplyOverlay(cfg, sim.Overlay{
		FeesByVenue: map[string]config.VenueFee{
			"hyperliquid": {RateBps: 3.5, CommissionBps: 1, CommissionFixed: 0.02},
			"grvt":        {RateBps: 5, Fixed: 0.1, CommissionFixed: 0.01},
		},
	})
	hl := got.SymbolMap[0].Venues["hyperliquid"].Fees
	if hl.RateBps != 3.5 || hl.CommissionBps != 1 || hl.CommissionFixed != 0.02 {
		t.Fatalf("hyperliquid: %+v", hl)
	}
	gr := got.SymbolMap[0].Venues["grvt"].Fees
	if gr.RateBps != 5 || gr.Fixed != 0.1 || gr.CommissionFixed != 0.01 {
		t.Fatalf("grvt: %+v", gr)
	}
}
