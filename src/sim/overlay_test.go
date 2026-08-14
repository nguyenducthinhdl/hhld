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
			"hyperliquid": config.SideFee{RateBps: 3.5, CommissionBps: 1, CommissionFixed: 0.02}.Both(),
			"grvt":        {Buy: config.SideFee{RateBps: 5, Fixed: 0.1}, Sell: config.SideFee{RateBps: 4, CommissionFixed: 0.01}},
		},
	})
	hl := got.SymbolMap[0].Venues["hyperliquid"].Fees
	if hl.Buy.RateBps != 3.5 || hl.Sell.CommissionBps != 1 || hl.Buy.CommissionFixed != 0.02 {
		t.Fatalf("hyperliquid: %+v", hl)
	}
	gr := got.SymbolMap[0].Venues["grvt"].Fees
	if gr.Buy.RateBps != 5 || gr.Buy.Fixed != 0.1 || gr.Sell.CommissionFixed != 0.01 || gr.Sell.RateBps != 4 {
		t.Fatalf("grvt: %+v", gr)
	}
}
