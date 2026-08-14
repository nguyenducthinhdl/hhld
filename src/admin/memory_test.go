package admin_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/exchange/fake"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestMemory_RecordAndListByTrace(t *testing.T) {
	ctx := context.Background()
	tr := pnl.NewMemory()
	aud := admin.NewMemory(tr)

	rec := admin.OrderRecord{
		OrderID: "o1", ClientOrderID: "c1", TraceID: "trace-1",
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1", Status: "accepted",
		Time: time.Unix(1, 0).UTC(),
	}
	if err := aud.RecordOrder(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := aud.ListOrders(ctx, admin.Filter{TraceID: "trace-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OrderID != "o1" {
		t.Fatalf("list: %+v", got)
	}
}

func TestRecordPaperDecision_ReconstructableArb(t *testing.T) {
	ctx := context.Background()
	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	tr := pnl.NewMemory()
	aud := admin.NewMemory(tr)

	d := strategy.Decision{
		TraceID: "arb-paper-1",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101", Size: "1"},
		},
		Reason: "test-gap",
	}
	venues := strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B}
	results, err := strategy.PlaceDecision(ctx, venues, d)
	if err != nil {
		t.Fatal(err)
	}
	fees := risk.FeeSchedule{
		DefaultRateBps: 5,
		ByVenue: map[exchange.VenueID]risk.VenueFee{
			"hyperliquid": risk.SideFee{RateBps: 5}.Both(),
			"grvt":        risk.SideFee{RateBps: 5}.Both(),
		},
	}
	if err := admin.RecordPaperDecision(ctx, aud, tr, d, results, fees); err != nil {
		t.Fatal(err)
	}

	orders, err := aud.ListOrders(ctx, admin.Filter{TraceID: "arb-paper-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("want 2 orders, got %+v", orders)
	}
	for _, o := range orders {
		if o.Status != "accepted" || o.OrderID == "" {
			t.Fatalf("order: %+v", o)
		}
	}

	fills := tr.Fills()
	if len(fills) != 2 {
		t.Fatalf("want 2 fills, got %d", len(fills))
	}
	for _, f := range fills {
		fee, err := strconv.ParseFloat(f.Fee, 64)
		if err != nil || fee <= 0 {
			t.Fatalf("want positive modeled fee on fill, got %+v", f)
		}
	}

	snap, err := aud.PnL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.ParseFloat(snap.Realized, 64)
	if err != nil {
		t.Fatal(err)
	}
	// gross gap 1.0 minus Cost(100,1,5bps)+Cost(101,1,5bps) = 0.1005 → ~0.8995
	if got < 0.899 || got > 0.901 {
		t.Fatalf("want realized ~0.8995 after fees, got %s", snap.Realized)
	}
}

func TestRecordPaperDecision_PartialLegStillAuditable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	dual := fake.NewDual("hyperliquid", "grvt", time.Unix(1, 0).UTC())
	dual.A.SetOrderDelay(80 * time.Millisecond)
	dual.B.SetOrderDelay(0)

	tr := pnl.NewMemory()
	aud := admin.NewMemory(tr)
	d := strategy.Decision{
		TraceID: "arb-partial",
		Legs: []strategy.Leg{
			{Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideBuy, Price: "100", Size: "1"},
			{Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Side: exchange.SideSell, Price: "101", Size: "1"},
		},
	}
	results, err := strategy.PlaceDecision(ctx, strategy.Venues{"hyperliquid": dual.A, "grvt": dual.B}, d)
	if err == nil {
		t.Fatal("expected place error from timeout")
	}
	if err := admin.RecordPaperDecision(context.Background(), aud, tr, d, results, risk.FeeSchedule{}); err != nil {
		t.Fatal(err)
	}
	orders, err := aud.ListOrders(context.Background(), admin.Filter{TraceID: "arb-partial"})
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("want 2 order records, got %+v", orders)
	}
	var accepted, errored int
	for _, o := range orders {
		switch o.Status {
		case "accepted":
			accepted++
		case "error":
			errored++
		}
	}
	if accepted != 1 || errored != 1 {
		t.Fatalf("want 1 accepted + 1 error, got %+v", orders)
	}
	if len(tr.Fills()) != 1 {
		t.Fatalf("want 1 fill for successful leg, got %d", len(tr.Fills()))
	}
}

func TestRecordPaperDecision_RejectsUnparseablePriceSize(t *testing.T) {
	ctx := context.Background()
	ack := exchange.OrderAck{OrderID: "o1", ClientOrderID: "c1", Status: "accepted", Time: time.Unix(1, 0).UTC()}
	leg := strategy.Leg{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1",
	}
	fees := risk.FeeSchedule{DefaultRateBps: 5}

	t.Run("bad price", func(t *testing.T) {
		tr := pnl.NewMemory()
		aud := admin.NewMemory(tr)
		bad := leg
		bad.Price = "n/a"
		err := admin.RecordPaperDecision(ctx, aud, tr, strategy.Decision{TraceID: "bad-px"}, []strategy.LegResult{{
			Index: 0, Leg: bad, Ack: ack,
		}}, fees)
		if err == nil {
			t.Fatal("want parse error")
		}
		if len(tr.Fills()) != 0 {
			t.Fatalf("want no fill, got %+v", tr.Fills())
		}
	})
	t.Run("bad size", func(t *testing.T) {
		tr := pnl.NewMemory()
		aud := admin.NewMemory(tr)
		bad := leg
		bad.Size = "oops"
		err := admin.RecordPaperDecision(ctx, aud, tr, strategy.Decision{TraceID: "bad-sz"}, []strategy.LegResult{{
			Index: 0, Leg: bad, Ack: ack,
		}}, fees)
		if err == nil {
			t.Fatal("want parse error")
		}
		if len(tr.Fills()) != 0 {
			t.Fatalf("want no fill, got %+v", tr.Fills())
		}
	})
	t.Run("zero size", func(t *testing.T) {
		tr := pnl.NewMemory()
		aud := admin.NewMemory(tr)
		bad := leg
		bad.Size = "0"
		err := admin.RecordPaperDecision(ctx, aud, tr, strategy.Decision{TraceID: "zero-sz"}, []strategy.LegResult{{
			Index: 0, Leg: bad, Ack: ack,
		}}, fees)
		if err == nil {
			t.Fatal("want positive size error")
		}
		if len(tr.Fills()) != 0 {
			t.Fatalf("want no fill, got %+v", tr.Fills())
		}
	})
}
