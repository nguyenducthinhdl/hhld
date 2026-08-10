package pnl_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
)

func TestMemory_CoverShortAndPartialClose(t *testing.T) {
	tr := pnl.NewMemory()
	ctx := context.Background()
	ts := time.Unix(1, 0).UTC()

	// Open short, then buy to cover partially and flip long.
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "s1", Symbol: "BTCUSD", Side: exchange.SideSell, Price: "100", Size: "2", Fee: "0", Time: ts,
		Venue: "hl", Kind: exchange.KindPerp,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "b1", Symbol: "BTCUSD", Side: exchange.SideBuy, Price: "99", Size: "3", Fee: "0", Time: ts,
		Venue: "hl", Kind: exchange.KindPerp,
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := tr.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.ParseFloat(snap.Realized, 64)
	// cover 2 @99 vs short avg 100 → +2 realized; leftover long 1
	if got < 1.999 || got > 2.001 {
		t.Fatalf("want ~2, got %s", snap.Realized)
	}

	// Sell more than long → flip short
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "s2", Symbol: "BTCUSD", Side: exchange.SideSell, Price: "105", Size: "2", Fee: "0.1", Time: ts,
		Venue: "hl", Kind: exchange.KindPerp,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "bad", Symbol: "BTCUSD", Side: exchange.SideBuy, Price: "x", Size: "1", Time: ts,
	}); err == nil {
		t.Fatal("want bad price")
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "bad2", Symbol: "BTCUSD", Side: exchange.SideBuy, Price: "1", Size: "0", Time: ts,
	}); err == nil {
		t.Fatal("want bad size")
	}
	if err := tr.RecordFill(ctx, exchange.Fill{
		OrderID: "bad3", Symbol: "BTCUSD", Side: exchange.SideBuy, Price: "1", Size: "1", Fee: "x", Time: ts,
	}); err == nil {
		t.Fatal("want bad fee")
	}
	ctxDone, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tr.RecordFill(ctxDone, exchange.Fill{Price: "1", Size: "1", Side: exchange.SideBuy}); err == nil {
		t.Fatal("want ctx error")
	}
	if _, err := tr.Snapshot(ctxDone); err == nil {
		t.Fatal("want snapshot ctx error")
	}
	if _, err := tr.SnapshotByHedge(ctxDone, "h"); err == nil {
		t.Fatal("want hedge snapshot ctx error")
	}
}
