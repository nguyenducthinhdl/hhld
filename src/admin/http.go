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
	// Market, when set, serves GET /trading/market (books, gap, signal, config).
	Market func(symbol exchange.Symbol) any
}

// Register mounts:
//
//	GET /trading/pnl     — PnL snapshot (HTML or JSON)
//	GET /trading/orders  — order list; query: trace_id, hedge_id, venue, symbol, format=json
//	GET /trading/market  — dual books, gap, signal, config (when Market is set)
func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /trading/pnl", h.handlePnL)
	mux.HandleFunc("GET /trading/orders", h.handleOrders)
	mux.HandleFunc("GET /trading/market", h.handleMarket)
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
	_ = marketPage.Execute(w, map[string]any{
		"Symbol": string(sym),
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
  <nav><a href="/trading/pnl">PnL</a> · <a href="/trading/orders">Orders</a> · <a href="/trading/market">Market</a> · <a href="/trading/pnl?format=json">JSON</a></nav>
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
  <nav><a href="/trading/pnl">PnL</a> · <a href="/trading/orders">Orders</a> · <a href="/trading/market">Market</a> · <a href="/trading/orders?format=json">JSON</a></nav>
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

var marketPage = template.Must(template.New("market").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>HHLD Market</title>
  <style>
    :root { --bg:#0f1419; --panel:#151b24; --line:#243044; --muted:#94a3b8; --text:#e7ecf1; --bid:#34d399; --ask:#f87171; --gap:#fbbf24; --ok:#86efac; }
    body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 1.5rem; background: var(--bg); color: var(--text); }
    a { color: #7dd3fc; }
    nav { margin-bottom: 1rem; font-size: 0.9rem; }
    h1 { font-size: 1.35rem; margin: 0 0 0.25rem; letter-spacing: -0.02em; }
    .sub { color: var(--muted); font-size: 0.85rem; margin-bottom: 1.25rem; }
    .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
    @media (max-width: 900px) { .grid { grid-template-columns: 1fr; } }
    .panel { border: 1px solid var(--line); background: var(--panel); border-radius: 6px; padding: 1rem; }
    .panel h2 { font-size: 0.95rem; margin: 0 0 0.75rem; color: var(--muted); font-weight: 600; }
    .gap { font-size: 1.75rem; font-variant-numeric: tabular-nums; }
    .gap.ok { color: var(--ok); }
    .gap.low { color: var(--gap); }
    .gap.none { color: var(--muted); }
    table.depth { width: 100%; border-collapse: collapse; font-size: 0.85rem; font-variant-numeric: tabular-nums; }
    table.depth th { text-align: left; color: var(--muted); font-weight: 500; padding: 0.2rem 0.35rem 0.45rem; border-bottom: 1px solid var(--line); }
    table.depth td { padding: 0.2rem 0.35rem; }
    .bid { color: var(--bid); }
    .ask { color: var(--ask); }
    .tob { font-size: 0.85rem; margin: 0 0 0.75rem; font-variant-numeric: tabular-nums; line-height: 1.45; }
    .tob .ask { margin-right: 0.75rem; }
    .lat { font-size: 0.8rem; color: var(--muted); margin-top: 0.35rem; }
    .lat.ok { color: var(--ok); }
    .lat.warn { color: var(--gap); }
    .lat.bad { color: #fca5a5; }
    .native { color: var(--muted); font-weight: 400; font-size: 0.8rem; }
    .bar { display: inline-block; height: 0.55rem; background: currentColor; opacity: 0.35; vertical-align: middle; margin-left: 0.35rem; max-width: 6rem; }
    .signal { font-size: 0.95rem; }
    .signal.miss { color: #fca5a5; }
    .signal.decision { color: var(--ok); }
    dl.cfg { display: grid; grid-template-columns: 10rem 1fr; gap: 0.35rem 0.75rem; font-size: 0.85rem; margin: 0; }
    dl.cfg dt { color: var(--muted); }
    dl.cfg dd { margin: 0; font-variant-numeric: tabular-nums; }
    .ticks { font-size: 0.8rem; color: var(--muted); max-height: 8rem; overflow: auto; }
    .row { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
    @media (max-width: 900px) { .row { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <nav>
    <a href="/trading/pnl">PnL</a> ·
    <a href="/trading/orders">Orders</a> ·
    <a href="/trading/market">Market</a> ·
    <a id="jsonLink" href="/trading/market?format=json">JSON</a>
  </nav>
  <h1>Market <span id="symLabel"></span></h1>
  <p class="sub">As of <span id="asOf">—</span> · poll 300ms · config read-only</p>

  <div class="row">
    <div class="panel">
      <h2>Gap</h2>
      <div id="gapVal" class="gap none">—</div>
      <p id="gapDetail" class="sub" style="margin:0.5rem 0 0"></p>
    </div>
    <div class="panel">
      <h2>Trade signal</h2>
      <div id="signal" class="signal">—</div>
    </div>
  </div>

  <div class="grid" style="margin-bottom:1rem">
    <div class="panel">
      <h2 id="venueA">Venue A</h2>
      <div class="tob" id="tobA"></div>
      <table class="depth" id="bookA">
        <thead><tr><th>Price</th><th>Size</th><th></th></tr></thead>
        <tbody></tbody>
      </table>
    </div>
    <div class="panel">
      <h2 id="venueB">Venue B</h2>
      <div class="tob" id="tobB"></div>
      <table class="depth" id="bookB">
        <thead><tr><th>Price</th><th>Size</th><th></th></tr></thead>
        <tbody></tbody>
      </table>
    </div>
  </div>

  <div class="row">
    <div class="panel">
      <h2>Signal config</h2>
      <dl class="cfg" id="cfg"></dl>
    </div>
    <div class="panel">
      <h2>Recent ticks</h2>
      <div class="ticks" id="ticks">—</div>
    </div>
  </div>

<script>
const q = new URLSearchParams(location.search);
const symbol = q.get("symbol") || "";
document.getElementById("jsonLink").href = "/trading/market?format=json" + (symbol ? "&symbol="+encodeURIComponent(symbol) : "");

function esc(s){ return String(s??"").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":"&#39;"}[c])); }

function fmtMs(ms) {
  if (ms == null || ms === undefined) return "—";
  if (ms < 1000) return ms + "ms";
  return (ms / 1000).toFixed(2) + "s";
}

function latClass(ms, maxBookAgeMs) {
  if (ms == null || ms === undefined) return "";
  const limit = maxBookAgeMs || 2000;
  if (ms <= limit) return "ok";
  if (ms <= limit * 2) return "warn";
  return "bad";
}

function renderTob(el, book, maxBookAgeMs) {
  if (!book || !book.ready) {
    el.innerHTML = '<span style="color:var(--muted)">waiting for book…</span>';
    return;
  }
  const lat = book.latency_ms;
  const exAge = book.exchange_age_ms;
  const latCls = latClass(lat, maxBookAgeMs);
  let latLine = '<div class="lat '+latCls+'">feed latency '+fmtMs(lat);
  if (exAge != null && exAge !== undefined) latLine += ' · exchange age '+fmtMs(exAge);
  latLine += '</div>';
  el.innerHTML =
    '<span class="ask">ask '+esc(book.best_ask)+' × '+esc(book.best_ask_size)+'</span>' +
    '<span class="bid">bid '+esc(book.best_bid)+' × '+esc(book.best_bid_size)+'</span>' +
    (book.mid ? '<div style="color:var(--muted);margin-top:0.25rem">mid '+esc(book.mid)+'</div>' : '') +
    latLine;
}

function renderBook(tbody, book) {
  tbody.innerHTML = "";
  if (!book || !book.ready) {
    tbody.innerHTML = "<tr><td colspan=3 style=color:var(--muted)>no book</td></tr>";
    return;
  }
  const asks = (book.asks||[]).slice().reverse();
  const bids = book.bids||[];
  const maxSz = Math.max(1e-12, ...asks.concat(bids).map(lv => +lv.size || 0));
  for (const lv of asks) {
    const tr = document.createElement("tr");
    const w = Math.min(80, ((+lv.size||0)/maxSz)*80);
    tr.innerHTML = '<td class="ask">'+esc(lv.price)+'</td><td>'+esc(lv.size)+'</td><td><span class="bar ask" style="width:'+w+'px"></span></td>';
    tbody.appendChild(tr);
  }
  for (const lv of bids) {
    const tr = document.createElement("tr");
    const w = Math.min(80, ((+lv.size||0)/maxSz)*80);
    tr.innerHTML = '<td class="bid">'+esc(lv.price)+'</td><td>'+esc(lv.size)+'</td><td><span class="bar bid" style="width:'+w+'px"></span></td>';
    tbody.appendChild(tr);
  }
}

function venueTitle(book, fallback) {
  if (!book || !book.venue) return fallback;
  return book.native_symbol
    ? book.venue+' <span class="native">'+esc(book.native_symbol)+'</span>'
    : esc(book.venue);
}

function parseMaxBookAgeMs(s) {
  if (!s) return 2000;
  const m = String(s).match(/^([\d.]+)(ms|s|m|h)?$/);
  if (!m) return 2000;
  const n = parseFloat(m[1]);
  switch (m[2] || "s") {
    case "ms": return n;
    case "m": return n * 60000;
    case "h": return n * 3600000;
    default: return n * 1000;
  }
}

function renderCfg(c) {
  if (!c) return;
  const fees = c.fees_by_venue || {};
  const feeLines = Object.keys(fees).map(v => {
    const f = fees[v];
    return v+": rate_bps="+f.rate_bps+" fixed="+f.fixed+" commission_bps="+f.commission_bps+" commission_fixed="+f.commission_fixed;
  }).join("; ") || "—";
  const budgets = c.budgets || {};
  const budLines = Object.keys(budgets).map(k => k+"="+budgets[k]).join(", ") || "—";
  const rows = [
    ["venues", (c.venue_a||"")+" / "+(c.venue_b||"")],
    ["size", c.size],
    ["effective_size", c.effective_size],
    ["max_volume_trade", c.max_volume_trade || "—"],
    ["min_gap", c.min_gap],
    ["order_interval", c.order_interval || "—"],
    ["fee_bps_per_leg", c.fee_bps_per_leg],
    ["fees", feeLines],
    ["latency_penalty", c.latency_penalty],
    ["partial_fill_factor", c.partial_fill_factor],
    ["max_book_age", c.max_book_age],
    ["max_in_flight", c.max_in_flight],
    ["budgets", budLines],
  ];
  document.getElementById("cfg").innerHTML = rows.map(([k,v]) => "<dt>"+esc(k)+"</dt><dd>"+esc(v)+"</dd>").join("");
}

async function refresh() {
  const url = "/trading/market?format=json" + (symbol ? "&symbol="+encodeURIComponent(symbol) : "");
  const res = await fetch(url);
  if (!res.ok) return;
  const s = await res.json();
  document.getElementById("symLabel").textContent = s.symbol || "";
  document.getElementById("asOf").textContent = s.as_of || "—";
  document.getElementById("venueA").innerHTML = venueTitle(s.venue_a, "Venue A");
  document.getElementById("venueB").innerHTML = venueTitle(s.venue_b, "Venue B");
  const maxBookAgeMs = parseMaxBookAgeMs(s.config && s.config.max_book_age);
  renderTob(document.getElementById("tobA"), s.venue_a, maxBookAgeMs);
  renderTob(document.getElementById("tobB"), s.venue_b, maxBookAgeMs);
  renderBook(document.querySelector("#bookA tbody"), s.venue_a);
  renderBook(document.querySelector("#bookB tbody"), s.venue_b);

  const g = s.gap;
  const gapEl = document.getElementById("gapVal");
  const gapDet = document.getElementById("gapDetail");
  if (!g || !g.ready) {
    gapEl.textContent = "—";
    gapEl.className = "gap none";
    gapDet.textContent = "waiting for both venue books";
  } else {
    gapEl.textContent = g.value.toFixed(4);
    gapEl.className = "gap " + (g.above_min_gap ? "ok" : "low");
    gapDet.textContent = "buy "+g.buy_venue+" @"+g.buy_ask+" / sell "+g.sell_venue+" @"+g.sell_bid
      + (g.above_min_gap ? " (≥ min_gap)" : " (below min_gap)");
  }

  const sig = s.signal;
  const sigEl = document.getElementById("signal");
  if (!sig) {
    sigEl.className = "signal";
    sigEl.textContent = "no signal yet";
  } else {
    sigEl.className = "signal " + (sig.kind === "decision" ? "decision" : "miss");
    let t = sig.kind + ": " + sig.reason;
    if (sig.trace_id) t += " · " + sig.trace_id;
    if (sig.gross_gap) t += " · gap=" + Number(sig.gross_gap).toFixed(4);
    if (sig.legs && sig.legs.length) {
      t += " · " + sig.legs.map(l => l.side+" "+l.venue+" @"+l.price+" x"+l.size).join(" | ");
    }
    sigEl.textContent = t;
  }

  renderCfg(s.config);
  const ticks = s.ticks || [];
  document.getElementById("ticks").innerHTML = ticks.length
    ? ticks.slice().reverse().map(t => esc(t.time)+" "+esc(t.venue)+" "+esc(t.side)+" "+esc(t.price)+" x"+esc(t.size)).join("<br/>")
    : "—";
}
refresh();
setInterval(refresh, 300);
</script>
</body>
</html>`))
