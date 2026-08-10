package admin

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/nguyenducthinhdl/hhld/src/exchange"
)

// Handler serves minimal trading audit dashboards over an Auditor.
type Handler struct {
	Auditor Auditor
}

// Register mounts:
//
//	GET /trading/pnl     — PnL snapshot (HTML or JSON)
//	GET /trading/orders  — order list; query: trace_id, hedge_id, venue, symbol, format=json
func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /trading/pnl", h.handlePnL)
	mux.HandleFunc("GET /trading/orders", h.handleOrders)
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
	_ = pnlPage.Execute(w, payload)
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
	_ = ordersPage.Execute(w, map[string]any{
		"Orders": orders,
		"Filter": f,
		"Count":  len(orders),
	})
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

var pnlPage = template.Must(template.New("pnl").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>HHLD PnL</title>
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 2rem; background: #0f1419; color: #e7ecf1; }
    a { color: #7dd3fc; }
    .card { border: 1px solid #243044; border-radius: 8px; padding: 1.25rem 1.5rem; max-width: 32rem; background: #151b24; }
    h1 { font-size: 1.25rem; margin: 0 0 1rem; }
    dt { color: #94a3b8; font-size: 0.85rem; }
    dd { margin: 0 0 0.75rem; font-size: 1.35rem; font-variant-numeric: tabular-nums; }
    nav { margin-bottom: 1.5rem; font-size: 0.9rem; }
  </style>
</head>
<body>
  <nav><a href="/trading/pnl">PnL</a> · <a href="/trading/orders">Orders</a> · <a href="/trading/pnl?format=json">JSON</a></nav>
  <div class="card">
    <h1>Trading PnL</h1>
    <dl>
      <dt>Realized</dt><dd>{{.realized}}</dd>
      <dt>Unrealized</dt><dd>{{.unrealized}}</dd>
      <dt>As of</dt><dd style="font-size:0.95rem">{{.as_of}}</dd>
      {{if .hedge_id}}<dt>Hedge {{.hedge_id}}</dt><dd>{{.hedge}}</dd>{{end}}
    </dl>
  </div>
</body>
</html>`))

var ordersPage = template.Must(template.New("orders").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>HHLD Orders</title>
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 2rem; background: #0f1419; color: #e7ecf1; }
    a { color: #7dd3fc; }
    table { border-collapse: collapse; width: 100%; font-size: 0.9rem; }
    th, td { border-bottom: 1px solid #243044; padding: 0.5rem 0.6rem; text-align: left; }
    th { color: #94a3b8; font-weight: 600; }
    .error { color: #fca5a5; }
    .accepted { color: #86efac; }
    nav { margin-bottom: 1.5rem; font-size: 0.9rem; }
  </style>
</head>
<body>
  <nav><a href="/trading/pnl">PnL</a> · <a href="/trading/orders">Orders</a> · <a href="/trading/orders?format=json">JSON</a></nav>
  <h1>Orders ({{.Count}})</h1>
  <p style="color:#94a3b8;font-size:0.85rem">Filter via query: trace_id, hedge_id, venue, symbol</p>
  <table>
    <thead>
      <tr>
        <th>Time</th><th>Trace</th><th>Venue</th><th>Symbol</th><th>Side</th>
        <th>Price</th><th>Size</th><th>Status</th><th>Order ID</th>
      </tr>
    </thead>
    <tbody>
      {{range .Orders}}
      <tr>
        <td>{{.Time.UTC.Format "15:04:05"}}</td>
        <td>{{.TraceID}}</td>
        <td>{{.Venue}}</td>
        <td>{{.Symbol}}</td>
        <td>{{.Side}}</td>
        <td>{{.Price}}</td>
        <td>{{.Size}}</td>
        <td class="{{.Status}}">{{.Status}}</td>
        <td>{{.OrderID}}</td>
      </tr>
      {{else}}
      <tr><td colspan="9">No orders yet</td></tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))
