package admin

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
	"github.com/nguyenducthinhdl/hhld/view"
)

var pages = template.Must(template.ParseFS(view.FS, "pnl.html", "orders.html", "sim.html", "market.html"))

// Handler serves minimal trading audit dashboards over an Auditor.
type Handler struct {
	Auditor Auditor
	// Market, when set, serves GET /trading/market (books, gap, signal, config).
	Market func(symbol exchange.Symbol) any
	// SimGet, when set, serves GET /sim (crawl replay series).
	SimGet func() any
	// SimRun, when set, handles POST /sim/run overlay JSON.
	SimRun func(body []byte) (any, error)
}

// Register mounts:
//
//	GET /trading/pnl     — PnL snapshot (HTML or JSON)
//	GET /trading/orders  — order list; query: trace_id, hedge_id, venue, symbol, format=json
//	GET /trading/market  — dual books, gap, signal, config (when Market is set)
//	GET /sim             — crawl replay series (when SimGet is set)
//	POST /sim/run        — overlay knobs and re-run (when SimRun is set)
//	GET /view/           — static HTML/CSS/JS from the view package
func (h Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /view/", http.StripPrefix("/view/", http.FileServer(http.FS(view.FS))))
	mux.HandleFunc("GET /trading/pnl", h.handlePnL)
	mux.HandleFunc("GET /trading/orders", h.handleOrders)
	mux.HandleFunc("GET /trading/market", h.handleMarket)
	mux.HandleFunc("GET /sim", h.handleSim)
	mux.HandleFunc("POST /sim/run", h.handleSimRun)
}

func (h Handler) handlePnL(w http.ResponseWriter, r *http.Request) {
	if h.Auditor == nil {
		http.Error(w, "admin: auditor not configured", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	snap, err := h.Auditor.PnL(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hedgeID := r.URL.Query().Get("hedge_id")
	var hedge any
	if hedgeID != "" {
		hs, err := h.Auditor.PnLByHedge(ctx, hedgeID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hedge = hs
	}
	payload := map[string]any{
		"realized":   snap.Realized,
		"unrealized": snap.Unrealized,
		"as_of":      snap.AsOf.UTC().Format(time.RFC3339Nano),
	}
	if hedge != nil {
		payload["hedge_id"] = hedgeID
		payload["hedge"] = hedge
	}
	if wantJSON(r) {
		writeJSON(w, payload)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ExecuteTemplate(w, "pnl.html", payload)
}

func (h Handler) handleOrders(w http.ResponseWriter, r *http.Request) {
	if h.Auditor == nil {
		http.Error(w, "admin: auditor not configured", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	f := Filter{
		TraceID: q.Get("trace_id"),
		HedgeID: q.Get("hedge_id"),
		Venue:   exchange.VenueID(q.Get("venue")),
		Symbol:  exchange.Symbol(q.Get("symbol")),
	}
	orders, err := h.Auditor.ListOrders(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantJSON(r) {
		writeJSON(w, map[string]any{"orders": orders, "count": len(orders)})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ExecuteTemplate(w, "orders.html", map[string]any{
		"Orders": orders,
		"Filter": f,
		"Count":  len(orders),
	})
}

func (h Handler) handleMarket(w http.ResponseWriter, r *http.Request) {
	if h.Market == nil {
		http.Error(w, "admin: market view not configured", http.StatusServiceUnavailable)
		return
	}
	sym := exchange.Symbol(r.URL.Query().Get("symbol"))
	snap := h.Market(sym)
	if wantJSON(r) {
		writeJSON(w, snap)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ExecuteTemplate(w, "market.html", map[string]any{
		"Symbol": string(sym),
	})
}

func (h Handler) handleSim(w http.ResponseWriter, r *http.Request) {
	if h.SimGet == nil {
		http.Error(w, "admin: sim view not configured", http.StatusServiceUnavailable)
		return
	}
	snap := h.SimGet()
	if wantJSON(r) {
		writeJSON(w, snap)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ExecuteTemplate(w, "sim.html", nil)
}

func (h Handler) handleSimRun(w http.ResponseWriter, r *http.Request) {
	if h.SimRun == nil {
		http.Error(w, "admin: sim run not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snap, err := h.SimRun(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, snap)
}

func wantJSON(r *http.Request) bool {
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
