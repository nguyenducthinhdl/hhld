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
    el.innerHTML = '<span class="waiting">waiting for book…</span>';
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
    (book.mid ? '<div class="mid">mid '+esc(book.mid)+'</div>' : '') +
    latLine;
}

function renderBook(tbody, book) {
  tbody.innerHTML = "";
  if (!book || !book.ready) {
    tbody.innerHTML = "<tr><td colspan=3 class=waiting>no book</td></tr>";
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

function fmtSideFee(side, f) {
  f = f || {};
  return side+" rate_bps="+f.rate_bps+" fixed="+f.fixed+" commission_bps="+f.commission_bps+" commission_fixed="+f.commission_fixed;
}

function renderCfg(c) {
  if (!c) return;
  const fees = c.fees_by_venue || {};
  const feeLines = Object.keys(fees).map(v => {
    const f = fees[v] || {};
    return v+": "+fmtSideFee("buy", f.buy)+" | "+fmtSideFee("sell", f.sell);
  }).join("; ") || "—";
  const budgets = c.budgets || {};
  const budLines = Object.keys(budgets).map(k => k+"="+budgets[k]).join(", ") || "—";
  const rows = [
    ["venues", (c.venue_a||"")+" / "+(c.venue_b||"")],
    ["min_size", c.min_size],
    ["max_size", c.max_size],
    ["effective_size", c.effective_size],
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
    if (sig.gap_time) t += " · gap_time " + fmtMs(sig.gap_time);
    if (sig.venue) t += " · " + sig.venue;
    if (sig.net) t += " · net " + Number(sig.net).toFixed(4);
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
