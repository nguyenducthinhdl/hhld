package admin_test

import (
	"context"
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
	"github.com/nguyenducthinhdl/hhld/src/sim"
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

func TestHandler_SimJSONAndRun(t *testing.T) {
	in, err := sim.InputFromNDJSON("../../data/samples/btcusd_books.ndjson", "BTCUSD")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.UpdateSymbol("BTCUSD", func(e *config.SymbolEntry) {
		e.Risk.MaxBookAge = config.Duration(24 * time.Hour)
		e.Trading.MinSize = "0.01"
		e.Trading.MaxSize = "1"
	})
	mux := http.NewServeMux()
	admin.Handler{
		Auditor: admin.NewMemory(nil),
		SimGet: func() any {
			s, err := sim.Trace(context.Background(), in, cfg)
			if err != nil {
				t.Fatal(err)
			}
			return s
		},
		SimRun: func(body []byte) (any, error) {
			var o sim.Overlay
			if err := json.Unmarshal(body, &o); err != nil {
				return nil, err
			}
			return sim.Trace(context.Background(), in, sim.ApplyOverlay(cfg, o))
		},
	}.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/sim?format=json", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"venues_in_file"`) || !strings.Contains(rec.Body.String(), `"steps"`) {
		t.Fatalf("missing series: %s", rec.Body.String())
	}

	high := `{"min_gap":10}`
	req2 := httptest.NewRequest(http.MethodPost, "/sim/run", strings.NewReader(high))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("run status %d %s", rec2.Code, rec2.Body.String())
	}

	reqHTML := httptest.NewRequest(http.MethodGet, "/sim", nil)
	recHTML := httptest.NewRecorder()
	mux.ServeHTTP(recHTML, reqHTML)
	if recHTML.Code != http.StatusOK || !strings.Contains(recHTML.Body.String(), "/view/sim.css") || !strings.Contains(recHTML.Body.String(), "id=\"sigMiss\"") {
		t.Fatalf("sim html: %d %s", recHTML.Code, recHTML.Body.String())
	}
	reqCSS := httptest.NewRequest(http.MethodGet, "/view/sim.css", nil)
	recCSS := httptest.NewRecorder()
	mux.ServeHTTP(recCSS, reqCSS)
	if recCSS.Code != http.StatusOK || !strings.Contains(recCSS.Body.String(), "#chart") {
		t.Fatalf("sim css: %d %s", recCSS.Code, recCSS.Body.String())
	}
}

func TestHandler_ForensicsHealthHalts(t *testing.T) {
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

	halts := map[exchange.Symbol]string{"BTCUSD": "unpaired:t-dash"}
	resumed := ""
	mux := http.NewServeMux()
	admin.Handler{
		Auditor: aud,
		Halted:  func() map[exchange.Symbol]string { return halts },
		Resume: func(sym exchange.Symbol) {
			resumed = string(sym)
			delete(halts, sym)
		},
	}.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/trading/forensics?trace_id=t-dash", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("forensics %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"outcome"`) || !strings.Contains(rec.Body.String(), "unpaired") {
		t.Fatalf("forensics body: %s", rec.Body.String())
	}

	reqBad := httptest.NewRequest(http.MethodGet, "/trading/forensics", nil)
	recBad := httptest.NewRecorder()
	mux.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", recBad.Code)
	}

	reqH := httptest.NewRequest(http.MethodGet, "/health", nil)
	recH := httptest.NewRecorder()
	mux.ServeHTTP(recH, reqH)
	if recH.Code != http.StatusOK || !strings.Contains(recH.Body.String(), `"ok": true`) {
		t.Fatalf("health: %d %s", recH.Code, recH.Body.String())
	}

	reqHalt := httptest.NewRequest(http.MethodGet, "/trading/halts", nil)
	recHalt := httptest.NewRecorder()
	mux.ServeHTTP(recHalt, reqHalt)
	if recHalt.Code != http.StatusOK || !strings.Contains(recHalt.Body.String(), "unpaired:t-dash") {
		t.Fatalf("halts: %s", recHalt.Body.String())
	}

	reqRes := httptest.NewRequest(http.MethodPost, "/trading/halts/resume?symbol=BTCUSD", nil)
	recRes := httptest.NewRecorder()
	mux.ServeHTTP(recRes, reqRes)
	if recRes.Code != http.StatusOK || resumed != "BTCUSD" {
		t.Fatalf("resume: %d %s resumed=%q", recRes.Code, recRes.Body.String(), resumed)
	}
}

func TestHandler_ResumeRequiresSymbolAndHandler(t *testing.T) {
	mux := http.NewServeMux()
	admin.Handler{}.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/trading/halts/resume?symbol=BTCUSD", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}

	mux2 := http.NewServeMux()
	admin.Handler{Resume: func(exchange.Symbol) {}}.Register(mux2)
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/trading/halts/resume", nil))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec2.Code)
	}

	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/trading/forensics?trace_id=x", nil))
	if rec3.Code != http.StatusServiceUnavailable {
		t.Fatalf("forensics nil auditor: %d", rec3.Code)
	}
}
