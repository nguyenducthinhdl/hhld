package pnl_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
)

func TestMemory_ArbBuySellRealizesGap(t *testing.T) {
	tr := pnl.NewMemory()
	ctx := context.Background()
	ts := time.Unix(10, 0).UTC()

	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "o1", TraceID: "t1", Venue: "hyperliquid", Symbol: "BTCUSD",
		Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100", Size: "1", Fee: "0", Time: ts,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "o2", TraceID: "t1", Venue: "grvt", Symbol: "BTCUSD",
		Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101", Size: "1", Fee: "0", Time: ts,
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := tr.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.ParseFloat(snap.Realized, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got < 0.999 || got > 1.001 {
		t.Fatalf("want realized ~1.0, got %s", snap.Realized)
	}
	if len(tr.Fills()) != 2 {
		t.Fatalf("fills: %d", len(tr.Fills()))
	}
}

func TestMemory_SnapshotByHedge(t *testing.T) {
	tr := pnl.NewMemory()
	ctx := context.Background()
	ts := time.Unix(10, 0).UTC()

	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "a", HedgeID: "hx", Symbol: "BTCUSD", Side: exchange.SideBuy, Price: "100", Size: "1", Fee: "0.1", Time: ts,
		Venue: "hyperliquid", Kind: exchange.KindPerp,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "b", HedgeID: "hx", Symbol: "BTCUSD", Side: exchange.SideSell, Price: "102", Size: "1", Fee: "0.1", Time: ts,
		Venue: "grvt", Kind: exchange.KindPerp,
	}); err != nil {
		t.Fatal(err)
	}
	hx, err := tr.SnapshotByHedge(ctx, "hx")
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.ParseFloat(hx.Realized, 64)
	if err != nil {
		t.Fatal(err)
	}
	// (102-100) - 0.1 - 0.1 = 1.8
	if got < 1.799 || got > 1.801 {
		t.Fatalf("want ~1.8, got %s", hx.Realized)
	}
}
