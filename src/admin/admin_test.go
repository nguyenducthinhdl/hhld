package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
)

var _ admin.Auditor = (*stubAuditor)(nil)

type stubAuditor struct {
	orders []admin.OrderRecord
}

func (a *stubAuditor) RecordOrder(ctx context.Context, rec admin.OrderRecord) error {
	a.orders = append(a.orders, rec)
	return nil
}

func (a *stubAuditor) ListOrders(ctx context.Context, f admin.Filter) ([]admin.OrderRecord, error) {
	var out []admin.OrderRecord
	for _, o := range a.orders {
		if f.HedgeID != "" && o.HedgeID != f.HedgeID {
			continue
		}
		if f.TraceID != "" && o.TraceID != f.TraceID {
			continue
		}
		if f.Venue != "" && o.Venue != f.Venue {
			continue
		}
		if f.Symbol != "" && o.Symbol != f.Symbol {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func (a *stubAuditor) PnL(ctx context.Context) (pnl.Snapshot, error) {
	return pnl.Snapshot{Realized: "0", Unrealized: "0", AsOf: time.Unix(1, 0).UTC()}, nil
}

func (a *stubAuditor) PnLByHedge(ctx context.Context, hedgeID string) (pnl.Snapshot, error) {
	return pnl.Snapshot{Realized: "0", Unrealized: "0", AsOf: time.Unix(1, 0).UTC()}, nil
}

func TestAuditor_RecordAndListByHedge(t *testing.T) {
	ctx := context.Background()
	a := &stubAuditor{}

	rec := admin.OrderRecord{
		OrderID: "o1", ClientOrderID: "c1", TraceID: "t1", HedgeID: "h1",
		Venue: "polymarket", Symbol: "BTC-UP", Kind: exchange.KindPrediction,
		Side: exchange.SideBuy, Price: "0.55", Size: "10", Status: "accepted",
		Time: time.Unix(1, 0).UTC(),
	}
	if err := a.RecordOrder(ctx, rec); err != nil {
		t.Fatal(err)
	}

	got, err := a.ListOrders(ctx, admin.Filter{HedgeID: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OrderID != "o1" {
		t.Fatalf("unexpected list: %+v", got)
	}

	snap, err := a.PnLByHedge(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if snap.AsOf.IsZero() {
		t.Fatal("expected pnl snapshot")
	}
}
