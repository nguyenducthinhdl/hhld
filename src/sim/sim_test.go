package sim_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/sim"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

var _ sim.Simulator = (*stubSimulator)(nil)
var _ strategy.Strategy = (*passStrategy)(nil)
var _ risk.Risk = (*passRisk)(nil)
var _ pnl.Tracker = (*memTracker)(nil)

type passStrategy struct{}

func (passStrategy) Name() string { return "pass" }
func (passStrategy) OnBooks(ctx context.Context, books []exchange.Book) ([]strategy.Decision, error) {
	return nil, nil
}

type passRisk struct{}

func (passRisk) Evaluate(ctx context.Context, d strategy.Decision, mkt risk.MarketView) (risk.Verdict, error) {
	_ = mkt
	return risk.Verdict{OK: true}, nil
}

type memTracker struct{}

func (memTracker) RecordFill(ctx context.Context, f exchange.Fill) error { return nil }
func (memTracker) Snapshot(ctx context.Context) (pnl.Snapshot, error) {
	return pnl.Snapshot{Realized: "0", Unrealized: "0", AsOf: time.Unix(1, 0).UTC()}, nil
}
func (memTracker) SnapshotByHedge(ctx context.Context, hedgeID string) (pnl.Snapshot, error) {
	return pnl.Snapshot{Realized: "0", Unrealized: "0", AsOf: time.Unix(1, 0).UTC()}, nil
}

type stubSimulator struct{}

func (stubSimulator) Run(ctx context.Context, in sim.Input, s strategy.Strategy, r risk.Risk, t pnl.Tracker) (pnl.Snapshot, error) {
	_, _ = s.OnBooks(ctx, in.Books)
	return t.Snapshot(ctx)
}

func TestSimulator_RunReturnsSnapshot(t *testing.T) {
	snap, err := stubSimulator{}.Run(
		context.Background(),
		sim.Input{Books: []exchange.Book{{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp}}},
		passStrategy{},
		passRisk{},
		memTracker{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if snap.AsOf.IsZero() {
		t.Fatal("expected non-zero snapshot time")
	}
}
