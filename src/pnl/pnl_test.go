package pnl_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
)

var _ pnl.Tracker = (*stubTracker)(nil)

type stubTracker struct {
	fills []exchange.Fill
}

func (t *stubTracker) RecordFill(ctx context.Context, f exchange.Fill) error {
	t.fills = append(t.fills, f)
	return nil
}

func (t *stubTracker) Snapshot(ctx context.Context) (pnl.Snapshot, error) {
	return pnl.Snapshot{Realized: "0", Unrealized: "0", AsOf: time.Unix(10, 0).UTC()}, nil
}

func (t *stubTracker) SnapshotByHedge(ctx context.Context, hedgeID string) (pnl.Snapshot, error) {
	_ = hedgeID
	return pnl.Snapshot{
		Realized:   "0",
		Unrealized: "0",
		AsOf:       time.Unix(10, 0).UTC(),
	}, nil
}

func TestTracker_RecordFillAndSnapshotByHedge(t *testing.T) {
	ctx := context.Background()
	tr := &stubTracker{}

	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "o1", HedgeID: "h1", Venue: "polymarket", Symbol: "BTC-UP",
		Kind: exchange.KindPrediction, Side: exchange.SideBuy, Price: "0.55", Size: "10", Fee: "0",
		Time: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "o2", HedgeID: "h1", Venue: "hyperliquid", Symbol: "BTCUSD",
		Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "100", Size: "1", Fee: "0.1",
		Time: time.Unix(2, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := tr.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.AsOf.IsZero() {
		t.Fatal("expected snapshot time")
	}

	byHedge, err := tr.SnapshotByHedge(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if byHedge.AsOf.IsZero() {
		t.Fatal("expected hedge snapshot time")
	}
	if len(tr.fills) != 2 {
		t.Fatalf("want 2 fills, got %d", len(tr.fills))
	}
}
