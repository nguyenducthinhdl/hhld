package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/risk"
	"github.com/nguyenducthinhdl/hhld/src/strategy"
)

func TestHandler_HTMLAcceptAndHedgeQuery(t *testing.T) {
	tr := pnl.NewMemory()
	aud := admin.NewMemory(tr)
	ctx := t.Context()
	_ = tr.RecordFill(ctx, exchange.Fill{
		OrderID: "a", HedgeID: "h1", Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1", Fee: "0", Time: time.Unix(1, 0).UTC(),
	})
	_ = tr.RecordFill(ctx, exchange.Fill{
		OrderID: "b", HedgeID: "h1", Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideSell, Price: "101", Size: "1", Fee: "0", Time: time.Unix(2, 0).UTC(),
	})

	mux := http.NewServeMux()
	admin.Handler{Auditor: aud}.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/trading/pnl?hedge_id=h1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Trading PnL") {
		t.Fatalf("html pnl: %d %s", rec.Code, rec.Body.String())
	}

	reqJSON := httptest.NewRequest(http.MethodGet, "/trading/pnl?hedge_id=h1", nil)
	reqJSON.Header.Set("Accept", "application/json")
	recJSON := httptest.NewRecorder()
	mux.ServeHTTP(recJSON, reqJSON)
	if recJSON.Code != http.StatusOK || !strings.Contains(recJSON.Body.String(), `"hedge_id"`) {
		t.Fatalf("json hedge: %s", recJSON.Body.String())
	}

	reqOrd := httptest.NewRequest(http.MethodGet, "/trading/orders", nil)
	recOrd := httptest.NewRecorder()
	mux.ServeHTTP(recOrd, reqOrd)
	if recOrd.Code != http.StatusOK || !strings.Contains(recOrd.Body.String(), "Orders") {
		t.Fatalf("html orders: %d %s", recOrd.Code, recOrd.Body.String())
	}
}

func TestHandler_NilAuditor(t *testing.T) {
	mux := http.NewServeMux()
	admin.Handler{}.Register(mux)
	for _, path := range []string{"/trading/pnl", "/trading/orders"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: want 503, got %d", path, rec.Code)
		}
	}
}

func TestMemory_FiltersPnLByHedgeAndNilTracker(t *testing.T) {
	aud := admin.NewMemory(nil)
	if aud.Tracker() == nil {
		t.Fatal("expected tracker")
	}
	ts := time.Unix(10, 0).UTC()
	_ = aud.RecordOrder(context.Background(), admin.OrderRecord{
		OrderID: "1", ClientOrderID: "c1", TraceID: "t1", HedgeID: "hx",
		Venue: "hyperliquid", Symbol: "BTCUSD", Status: "accepted", Time: ts,
	})
	_ = aud.RecordOrder(context.Background(), admin.OrderRecord{
		OrderID: "2", ClientOrderID: "c2", TraceID: "t2", HedgeID: "hy",
		Venue: "grvt", Symbol: "ETHUSD", Status: "accepted", Time: ts.Add(time.Hour),
	})

	got, err := aud.ListOrders(context.Background(), admin.Filter{
		HedgeID: "hx", Venue: "hyperliquid", Symbol: "BTCUSD",
		From: ts.Add(-time.Second), To: ts.Add(time.Second),
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("filter: %+v err=%v", got, err)
	}

	_ = aud.Tracker().RecordFill(context.Background(), exchange.Fill{
		OrderID: "1", HedgeID: "hx", Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1", Fee: "0", Time: ts,
	})
	snap, err := aud.PnLByHedge(context.Background(), "hx")
	if err != nil || snap.Realized == "" {
		t.Fatalf("pnl by hedge: %+v err=%v", snap, err)
	}

	if err := aud.RecordOrder(context.Background(), admin.OrderRecord{}); err == nil {
		t.Fatal("want missing id error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := aud.RecordOrder(ctx, admin.OrderRecord{OrderID: "x"}); err == nil {
		t.Fatal("want ctx error")
	}
	if _, err := aud.ListOrders(ctx, admin.Filter{}); err == nil {
		t.Fatal("want list ctx error")
	}
}

func TestRecordPaperDecision_NilTracker(t *testing.T) {
	aud := admin.NewMemory(pnl.NewMemory())
	err := admin.RecordPaperDecision(context.Background(), aud, nil, strategy.Decision{TraceID: "t"}, nil, risk.FeeSchedule{})
	if err == nil {
		t.Fatal("want tracker required")
	}
}
