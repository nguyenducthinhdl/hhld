package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/admin"
	"github.com/nguyenducthinhdl/hhld/src/config"
	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/src/market"
	"github.com/nguyenducthinhdl/hhld/src/pnl"
	"github.com/nguyenducthinhdl/hhld/src/viz"
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

func TestHandler_TradingMarketJSON(t *testing.T) {
	cfg := config.Default()
	store := market.NewBookStore()
	ts := time.Unix(1_700_000_000, 0).UTC()
	_, _ = store.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "hyperliquid", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "100.0", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.1", Size: "1"}},
	}))
	_, _ = store.Apply(market.SnapshotEvent(exchange.Book{
		Venue: "grvt", Symbol: "BTCUSD", Kind: exchange.KindPerp, Time: ts,
		Bids: []exchange.Level{{Price: "100.8", Size: "1"}},
		Asks: []exchange.Level{{Price: "100.9", Size: "1"}},
	}))
	src := viz.Source{Cfg: cfg, Store: store}
	mux := http.NewServeMux()
	admin.Handler{
		Auditor: admin.NewMemory(nil),
		Market:  func(sym exchange.Symbol) any { return src.BuildSnapshot(sym) },
	}.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/trading/market?format=json&symbol=BTCUSD", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"min_gap"`) || !strings.Contains(body, `"effective_size"`) {
		t.Fatalf("missing config: %s", body)
	}
	if !strings.Contains(body, `"above_min_gap"`) {
		t.Fatalf("missing gap: %s", body)
	}
	if !strings.Contains(body, `"best_bid"`) || !strings.Contains(body, `"best_ask_size"`) {
		t.Fatalf("missing tob: %s", body)
	}
	if !strings.Contains(body, `"native_symbol"`) {
		t.Fatalf("missing native: %s", body)
	}
	if !strings.Contains(body, `"latency_ms"`) {
		t.Fatalf("missing latency: %s", body)
	}
}
