package sim_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/sim"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
	"github.com/nguyenducthinhdl/hhld/src/warehouse"
)

type bareRisk struct{}

func (bareRisk) Evaluate(ctx context.Context, d strategy.Decision, mkt risk.MarketView) (risk.Verdict, error) {
	return risk.Verdict{OK: true, Reason: "ok"}, nil
}

func TestReplay_NilArgsAndFilters(t *testing.T) {
	rep := sim.NewReplay(nil)
	if _, err := rep.Run(context.Background(), sim.Input{}, nil, bareRisk{}, pnl.NewMemory()); err == nil {
		t.Fatal("want nil strategy")
	}
	if _, err := rep.WinningDistribution(context.Background(), sim.Input{}, nil, bareRisk{}, sim.Filter{}); err == nil {
		t.Fatal("want nil strategy for dist")
	}

	arb := strategy.NewCrossVenueArb(strategy.ArbConfig{
		Symbols: []exchange.Symbol{"BTCUSD"}, Size: "1", MinGap: 0.3,
	})
	gate := risk.NewGate(risk.Params{
		FeeBpsPerLeg: 1, LatencyPenalty: 0.01, PartialFillFactor: 1,
		MaxBookAge: 10 * time.Second, MaxInFlight: 8,
	})
	in := sim.Input{Books: testBooks()}
	rate, err := rep.WinningRate(context.Background(), in, arb, gate, sim.Filter{
		Symbol: "BTCUSD", Exchange1: "hyperliquid", Exchange2: "grvt",
		MinGap: 0.1, MaxGap: 5, From: time.Unix(1_700_000_000, 0).UTC().Add(-time.Hour),
		To: time.Unix(1_700_000_000, 0).UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rate < 0 {
		t.Fatalf("rate=%v", rate)
	}

	// Filter that excludes all samples
	dist, err := rep.WinningDistribution(context.Background(), in, arb, gate, sim.Filter{Symbol: "NOPE"})
	if err != nil || len(dist.Samples) != 0 {
		t.Fatalf("filtered dist=%+v err=%v", dist, err)
	}

	// bareRisk path still runs (fee schedule empty)
	if _, err := rep.Run(context.Background(), in, arb, bareRisk{}, pnl.NewMemory()); err != nil {
		t.Fatal(err)
	}
}

func TestInputFromStore_Ticks(t *testing.T) {
	st, err := warehouse.OpenSQLite(filepath.Join(t.TempDir(), "hhld.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := time.Unix(400, 0).UTC()
	_ = st.AppendTick(context.Background(), exchange.Tick{
		Venue: "hl", Symbol: "BTCUSD", Kind: exchange.KindPerp, Price: "1", Size: "1", Time: ts,
	})
	in, err := sim.InputFromStore(context.Background(), st, "BTCUSD", ts.Add(-time.Second), ts.Add(time.Second))
	if err != nil || len(in.Ticks) != 1 {
		t.Fatalf("%+v err=%v", in, err)
	}
}
