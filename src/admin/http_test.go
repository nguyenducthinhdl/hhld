package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
)

// TestHandler_TradingPnL serves GET /trading/pnl for audit visibility.
//
// Why: Operators need a URL to inspect realized PnL without a polished UI
// (spec/trading.md PnL; visualization polish deferred — lightweight HTML/JSON is enough).
func TestHandler_TradingPnL(t *testing.T) {
	tr := pnl.NewMemory()
	aud := admin.NewMemory(tr)
	ctx := t.Context()
	_ = tr.RecordFill(ctx, exchange.Fill{
		OrderID: "o1", Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1", Fee: "0", Time: time.Unix(1, 0).UTC(),
	})
	_ = tr.RecordFill(ctx, exchange.Fill{
		OrderID: "o2", Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideSell, Price: "101", Size: "1", Fee: "0", Time: time.Unix(2, 0).UTC(),
	})

	mux := http.NewServeMux()
	admin.Handler{Auditor: aud}.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/trading/pnl?format=json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["realized"] == nil || body["realized"] == "0.00000000" {
		t.Fatalf("unexpected pnl payload: %v", body)
	}
}

// TestHandler_TradingOrders lists orders including error status for 1-leg failure forensics.
//
// Why: Same TraceID with accepted vs error is how unpaired legs are spotted
// (spec/networking.md; admin.RecordPaperDecision).
func TestHandler_TradingOrders(t *testing.T) {
	aud := admin.NewMemory(pnl.NewMemory())
	_ = aud.RecordOrder(t.Context(), admin.OrderRecord{
		OrderID: "ok-1", ClientOrderID: "c1", TraceID: "t-dash",
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideSell, Price: "101", Size: "1", Status: "accepted",
		Time: time.Unix(1, 0).UTC(),
	})
	_ = aud.RecordOrder(t.Context(), admin.OrderRecord{
		ClientOrderID: "c0", TraceID: "t-dash",
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp,
		Side: exchange.SideBuy, Price: "100", Size: "1", Status: "error",
		Time: time.Unix(1, 0).UTC(),
	})

	mux := http.NewServeMux()
	admin.Handler{Auditor: aud}.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/trading/orders?trace_id=t-dash&format=json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Count  int                 `json:"count"`
		Orders []admin.OrderRecord `json:"orders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 2 {
		t.Fatalf("want 2 orders, got %+v", body)
	}
}
