let series = null;
let xDomainFull = null;

function esc(s){ return String(s??"").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":"&#39;"}[c])); }

function sliderRow(id, label, value, min, max, step) {
  return '<tr><th>'+esc(label)+'</th><td>'
    +'<input type="range" id="'+id+'_r" min="'+min+'" max="'+max+'" step="'+step+'" value="'+value+'"/> '
    +'<input type="number" id="'+id+'" step="'+step+'" value="'+value+'"/>'
    +'</td></tr>';
}

function bindSlider(id) {
  const r = document.getElementById(id+'_r');
  const n = document.getElementById(id);
  if (!r || !n) return;
  r.addEventListener('input', () => { n.value = r.value; });
  n.addEventListener('input', () => { r.value = n.value; });
}

function fillVenues(ids, a, b) {
  const sa = document.getElementById('venue_a');
  const sb = document.getElementById('venue_b');
  sa.innerHTML = ''; sb.innerHTML = '';
  (ids||[]).forEach(v => {
    sa.appendChild(new Option(v, v, false, v===a));
    sb.appendChild(new Option(v, v, false, v===b));
  });
  if (a && ![...sa.options].some(o => o.value===a)) sa.appendChild(new Option(a, a, false, true));
  if (b && ![...sb.options].some(o => o.value===b)) sb.appendChild(new Option(b, b, false, true));
}

function renderCfg(s) {
  const c = s.config || {};
  document.getElementById('symbol').value = s.symbol || '';
  fillVenues(s.venues_in_file, s.venue_a, s.venue_b);
  const minSize = parseFloat(c.min_size)||0.000015;
  const maxSize = parseFloat(c.max_size)||0.00003;
  const minGap = c.min_gap ?? 0.3;
  const fee = c.fee_bps_per_leg ?? 5;
  const lat = c.latency_penalty ?? 0.05;
  const pff = c.partial_fill_factor ?? 0.95;
  const mif = c.max_in_flight ?? 4;
  const sizeMax = Math.max(0.001, maxSize*10);
  document.getElementById('knobs').innerHTML =
    sliderRow('min_size', 'min_size', minSize, 0, sizeMax, 0.000001) +
    sliderRow('max_size', 'max_size', maxSize, 0, sizeMax, 0.000001) +
    sliderRow('min_gap', 'min_gap', minGap, 0, Math.max(2, minGap*10), 0.01) +
    sliderRow('fee_bps_per_leg', 'fee_bps_per_leg', fee, 0, Math.max(50, fee*10), 0.1) +
    sliderRow('latency_penalty', 'latency_penalty', lat, 0, Math.max(1, lat*10), 0.01) +
    sliderRow('partial_fill_factor', 'partial_fill_factor', pff, 0, 1, 0.01) +
    sliderRow('max_in_flight', 'max_in_flight', mif, 1, Math.max(16, mif*2), 1) +
    '<tr><th>max_book_age</th><td><input type="text" id="max_book_age" value="'+esc(c.max_book_age||'2s')+'"/></td></tr>';
  ['min_size','max_size','min_gap','fee_bps_per_leg','latency_penalty','partial_fill_factor','max_in_flight'].forEach(bindSlider);
  renderFees(s);
  document.getElementById('venue_a').onchange = () => renderFees(s);
  document.getElementById('venue_b').onchange = () => renderFees(s);
}

const feeFields = ['rate_bps', 'fixed', 'commission_bps', 'commission_fixed'];

function feeNum(v) {
  const n = parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}

function renderFees(s) {
  const fees = Object.assign({}, (s.config || {}).fees_by_venue || {});
  const fallback = feeNum((s.config || {}).fee_bps_per_leg);
  for (const v of [s.venue_a, s.venue_b, document.getElementById('venue_a').value, document.getElementById('venue_b').value]) {
    if (v && !fees[v]) {
      fees[v] = { rate_bps: fallback, fixed: 0, commission_bps: 0, commission_fixed: 0 };
    }
  }
  const keys = Object.keys(fees).sort();
  const head = '<thead><tr><th>venue fees</th>' + feeFields.map(k => '<th>'+k+'</th>').join('') + '</tr></thead>';
  const body = '<tbody>' + keys.map(v => {
    const f = fees[v] || {};
    return '<tr data-fee-venue="'+esc(v)+'"><th>'+esc(v)+'</th>' +
      feeFields.map(k => '<td><input data-fee-field="'+k+'" type="number" step="0.01" value="'+feeNum(f[k])+'"/></td>').join('') +
      '</tr>';
  }).join('') + '</tbody>';
  document.getElementById('fees').innerHTML = head + body;
}

function overlayFromForm() {
  const fees = {};
  document.querySelectorAll('[data-fee-venue]').forEach(tr => {
    const v = tr.getAttribute('data-fee-venue');
    const o = {};
    tr.querySelectorAll('[data-fee-field]').forEach(el => {
      o[el.getAttribute('data-fee-field')] = feeNum(el.value);
    });
    fees[v] = o;
  });
  const fallback = feeNum(document.getElementById('fee_bps_per_leg').value);
  for (const id of ['venue_a', 'venue_b']) {
    const v = document.getElementById(id).value;
    if (v && !fees[v]) {
      fees[v] = { rate_bps: fallback, fixed: 0, commission_bps: 0, commission_fixed: 0 };
    }
  }
  return {
    venue_a: document.getElementById('venue_a').value,
    venue_b: document.getElementById('venue_b').value,
    symbol: document.getElementById('symbol').value,
    size: String(document.getElementById('max_size').value),
    min_size: String(document.getElementById('min_size').value),
    max_size: String(document.getElementById('max_size').value),
    min_gap: parseFloat(document.getElementById('min_gap').value),
    fee_bps_per_leg: parseFloat(document.getElementById('fee_bps_per_leg').value),
    latency_penalty: parseFloat(document.getElementById('latency_penalty').value),
    partial_fill_factor: parseFloat(document.getElementById('partial_fill_factor').value),
    max_in_flight: parseInt(document.getElementById('max_in_flight').value, 10),
    max_book_age: document.getElementById('max_book_age').value,
    fees_by_venue: fees,
  };
}

function draw() {
  const s = series;
  const svg = d3.select('#chart');
  svg.selectAll('*').remove();
  const steps = (s && s.steps) || [];
  document.getElementById('realized').textContent = 'realized ' + (s && s.realized ? s.realized : '0');
  if (!steps.length) {
    drawSignals();
    return;
  }
  const data = steps.map(st => ({
    t: new Date(st.time),
    gap: st.gap && st.gap.ready ? st.gap.value : null,
    pnl: st.cumulative_pnl,
    raw: st,
  }));
  const w = Math.max(640, document.querySelector('.panel').clientWidth - 32);
  const h = 320, m = {top: 16, right: 56, bottom: 64, left: 52};
  svg.attr('width', w).attr('height', h);
  const innerW = w - m.left - m.right;
  const innerH = h - m.top - m.bottom;
  const g = svg.append('g').attr('transform', 'translate('+m.left+','+m.top+')');

  if (!xDomainFull) xDomainFull = d3.extent(data, d => d.t);
  const x = d3.scaleTime().domain(xDomainFull).range([0, innerW]);
  const gaps = data.map(d => d.gap).filter(v => v != null);
  const pnls = data.map(d => d.pnl);
  const gapAbs = Math.max(1e-9, d3.max(gaps, v => Math.abs(v)) || 1e-9);
  const pnlAbs = Math.max(1e-9, d3.max(pnls, v => Math.abs(v)) || 1e-9);
  const gz = parseFloat(document.getElementById('gapZoom').value) || 1;
  const pz = parseFloat(document.getElementById('pnlZoom').value) || 1;
  const yGap = d3.scaleLinear().domain([-gapAbs/gz, gapAbs/gz]).nice().range([innerH, 0]);
  const yPnl = d3.scaleLinear().domain([-pnlAbs/pz, pnlAbs/pz]).nice().range([innerH, 0]);

  g.append('g').attr('transform','translate(0,'+innerH+')').call(d3.axisBottom(x).ticks(6)).selectAll('text').attr('fill','#94a3b8');
  g.append('g').call(d3.axisLeft(yGap).ticks(5)).selectAll('text').attr('fill','#fbbf24');
  g.append('g').attr('transform','translate('+innerW+',0)').call(d3.axisRight(yPnl).ticks(5)).selectAll('text').attr('fill','#86efac');

  const lineGap = d3.line().defined(d => d.gap != null).x(d => x(d.t)).y(d => yGap(d.gap));
  const linePnl = d3.line().x(d => x(d.t)).y(d => yPnl(d.pnl));
  g.append('path').datum(data).attr('fill','none').attr('stroke','#fbbf24').attr('stroke-width',1.5).attr('d', lineGap);
  g.append('path').datum(data).attr('fill','none').attr('stroke','#86efac').attr('stroke-width',1.5).attr('d', linePnl);

  g.selectAll('circle.sig').data(data.filter(d => d.raw.signal)).enter().append('circle')
    .attr('class','sig')
    .attr('cx', d => x(d.t)).attr('cy', d => yGap(d.gap != null ? d.gap : 0))
    .attr('r', 4)
    .attr('fill', d => d.raw.signal.kind === 'decision' ? '#34d399' : '#f87171');

  const tip = document.getElementById('tip');
  g.selectAll('rect.hit').data(data).enter().append('rect')
    .attr('class','hit').attr('x', (d,i) => {
      const next = data[i+1] ? x(data[i+1].t) : innerW;
      const prev = i>0 ? x(data[i-1].t) : 0;
      return (x(d.t)+prev)/2;
    })
    .attr('width', (d,i) => {
      const next = data[i+1] ? x(data[i+1].t) : innerW;
      const prev = i>0 ? x(data[i-1].t) : 0;
      return Math.max(4, (next - prev)/2);
    })
    .attr('y', 0).attr('height', innerH).attr('fill', 'transparent')
    .on('mousemove', (ev, d) => {
      const st = d.raw;
      tip.style.display = 'block';
      tip.style.left = (ev.pageX+12)+'px';
      tip.style.top = (ev.pageY+12)+'px';
      tip.innerHTML = hoverHTML(st);
    })
    .on('mouseleave', () => { tip.style.display='none'; });

  const brushH = 28;
  const bx = d3.scaleTime().domain(d3.extent(data, d => d.t)).range([0, innerW]);
  const brush = d3.brushX().extent([[0,0],[innerW, brushH]]).on('end', ev => {
    if (!ev.selection) { xDomainFull = d3.extent(data, d => d.t); draw(); return; }
    xDomainFull = ev.selection.map(bx.invert);
    draw();
  });
  svg.append('g').attr('transform','translate('+m.left+','+(h-brushH-8)+')').call(brush);
  drawSignals();
}

function fmtN(v, d) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '—';
  return n.toFixed(d == null ? 6 : d);
}

function hoverHTML(st) {
  const a = st.venue_a || {}, b = st.venue_b || {};
  const g = st.gap;
  const gapTxt = g && g.ready ? fmtN(g.value, 4) : '—';
  let html = '<div class="tip-time">'+esc(st.time)+'</div>'
    + '<div>gap '+esc(gapTxt)+' · pnl '+esc(fmtN(st.cumulative_pnl, 6))+'</div>'
    + '<div>'+esc(a.venue)+' ask '+esc(a.best_ask)+' × '+esc(a.best_ask_size)
    + ' / bid '+esc(a.best_bid)+' × '+esc(a.best_bid_size)+'</div>'
    + '<div>'+esc(b.venue)+' ask '+esc(b.best_ask)+' × '+esc(b.best_ask_size)
    + ' / bid '+esc(b.best_bid)+' × '+esc(b.best_bid_size)+'</div>'
    + '<div>signal '+esc(formatSignal(st.signal))+'</div>';
  const ex = st.explain;
  if (ex) {
    html += '<div class="tip-formula">'+esc(ex.formula)+'</div>';
    html += '<div>volume '+esc(fmtN(ex.size, 8))
      + (ex.size_modeled && ex.size_modeled !== ex.size ? ' (modeled '+esc(fmtN(ex.size_modeled, 8))+')' : '')
      + '</div>';
    (ex.legs || []).forEach(leg => {
      html += '<div class="tip-leg"><strong>'+esc(leg.side)+'</strong> '+esc(leg.venue)
        + ' '+esc(leg.book)+' @ '+esc(leg.price)+' × '+esc(leg.size)
        + '<br/>notional '+esc(fmtN(leg.notional, 6))
        + ' · fee '+esc(fmtN(leg.fee, 8))
        + ' (rate '+esc(fmtN(leg.rate_bps, 4))+'bps='+esc(fmtN(leg.rate_fee, 8))
        + (leg.fixed ? ' + fixed '+esc(fmtN(leg.fixed, 8)) : '')
        + (leg.commission_bps ? ' + comm '+esc(fmtN(leg.commission_bps, 4))+'bps='+esc(fmtN(leg.commission_rate, 8)) : '')
        + (leg.commission_fixed ? ' + comm_fixed '+esc(fmtN(leg.commission_fixed, 8)) : '')
        + ')</div>';
    });
    html += '<div>gross '+esc(fmtN(ex.gross, 8))
      + ' − fee '+esc(fmtN(ex.fee, 8))
      + ' − latency '+esc(fmtN(ex.latency, 8))
      + ' = <strong>net '+esc(fmtN(ex.net, 8))+'</strong></div>';
  } else if (g && g.ready) {
    html += '<div class="tip-formula">gap = sell_bid('+esc(g.sell_venue)+') - buy_ask('+esc(g.buy_venue)
      + ') = '+esc(g.sell_bid)+' - '+esc(g.buy_ask)+' = '+esc(gapTxt)+'</div>';
  }
  return html;
}

function formatSignal(sig) {
  if (!sig) return 'none';
  let t = sig.kind + ': ' + sig.reason;
  if (sig.trace_id) t += ' · ' + sig.trace_id;
  if (sig.gap_time && !String(sig.reason).includes('gap_time')) t += ' · gap_time ' + sig.gap_time + 'ms';
  if (sig.net && !String(sig.reason).includes('net=')) t += ' · net ' + Number(sig.net).toFixed(6);
  return t;
}

const missCodes = [
  'stale_book', 'negative_edge', 'max_volume_exceeded', 'rate_limited', 'budget_exceeded',
  'missing_book', 'venue_unhealthy', 'missing price or size', 'need buy and sell legs',
  'bad size', 'bad price', 'peer_missing', 'no_edge', 'no_trade', 'evaluate_error',
];

function missKey(reason) {
  const r = String(reason || 'unknown');
  for (const k of missCodes) {
    if (r === k || r.startsWith(k + ' ') || r.startsWith(k + ':') || r.startsWith(k + '=')) return k;
  }
  const colon = r.indexOf(':');
  if (colon > 0 && colon < 28) return r.slice(0, colon);
  return r.split(/\s+/)[0];
}

function buyVenueOf(sig) {
  if (sig.buy_venue) return sig.buy_venue;
  const leg = (sig.legs || []).find(l => l.side === 'buy');
  return leg ? leg.venue : '';
}

function staleVenueOf(sig) {
  if (sig.venue) return sig.venue;
  const r = String(sig.reason || '');
  const i = r.indexOf(':');
  return i > 0 ? r.slice(i + 1) : '';
}

function netOf(sig) {
  if (typeof sig.net === 'number' && !Number.isNaN(sig.net)) return sig.net;
  const m = String(sig.reason || '').match(/net=([-\d.]+)/);
  return m ? parseFloat(m[1]) : null;
}

function signalStats(steps, domain, venueA, venueB) {
  let decisions = 0, misses = 0;
  const byReason = {};
  const staleA = [], staleB = [];
  const netAB = [], netBA = [];
  for (const st of steps) {
    const sig = st.signal;
    if (!sig) continue;
    if (domain && domain[0] && domain[1]) {
      const t = new Date(st.time);
      if (t < domain[0] || t > domain[1]) continue;
    }
    if (sig.kind === 'decision') {
      decisions++;
      continue;
    }
    misses++;
    const key = missKey(sig.reason);
    if (!byReason[key]) byReason[key] = { n: 0, examples: [] };
    byReason[key].n++;
    const full = String(sig.reason || key);
    if (full !== key && byReason[key].examples.length < 3 && !byReason[key].examples.includes(full)) {
      byReason[key].examples.push(full);
    }
    if (key === 'stale_book' && sig.gap_time) {
      const v = staleVenueOf(sig);
      if (v === venueA) staleA.push(sig.gap_time);
      else if (v === venueB) staleB.push(sig.gap_time);
    }
    if (key === 'negative_edge') {
      const net = netOf(sig);
      if (net == null || Number.isNaN(net)) continue;
      const buy = buyVenueOf(sig);
      if (buy === venueA) netAB.push(net);
      else if (buy === venueB) netBA.push(net);
    }
  }
  return { decisions, misses, byReason, staleA, staleB, netAB, netBA };
}

function p50(arr) {
  if (!arr.length) return null;
  const s = arr.slice().sort((a, b) => a - b);
  return s[Math.floor((s.length - 1) / 2)];
}

function fmtMsVal(ms) {
  if (ms == null) return '—';
  if (Math.abs(ms) < 1000) return ms + 'ms';
  return (ms / 1000).toFixed(2) + 's';
}

function reasonDetail(key, row, stats, venueA, venueB) {
  if (key === 'stale_book') {
    return venueA + ' n=' + stats.staleA.length + ' p50 gap_time ' + fmtMsVal(p50(stats.staleA))
      + ' · ' + venueB + ' n=' + stats.staleB.length + ' p50 gap_time ' + fmtMsVal(p50(stats.staleB));
  }
  if (key === 'negative_edge') {
    const f = v => v == null ? '—' : Number(v).toFixed(4);
    return venueA + '→' + venueB + ' n=' + stats.netAB.length + ' p50 net ' + f(p50(stats.netAB))
      + ' · ' + venueB + '→' + venueA + ' n=' + stats.netBA.length + ' p50 net ' + f(p50(stats.netBA));
  }
  return (row.examples || []).join(' · ') || '—';
}

function drawHBars(svgSel, rows, colorFn) {
  const svg = d3.select(svgSel);
  svg.selectAll('*').remove();
  if (!rows.length) return;
  const labelW = Math.min(220, Math.max(80, d3.max(rows, d => String(d.label).length) * 6.5));
  const w = Math.max(280, svg.node().parentElement.clientWidth);
  const barH = 18, gap = 8;
  const m = { top: 4, right: 48, bottom: 4, left: labelW + 8 };
  const h = m.top + m.bottom + rows.length * (barH + gap);
  svg.attr('width', w).attr('height', h);
  const innerW = Math.max(40, w - m.left - m.right);
  const x = d3.scaleLinear().domain([0, d3.max(rows, d => d.n) || 1]).range([0, innerW]);
  const g = svg.append('g').attr('transform', 'translate('+m.left+','+m.top+')');
  rows.forEach((d, i) => {
    const y = i * (barH + gap);
    g.append('text').attr('x', -8).attr('y', y + barH * 0.72)
      .attr('text-anchor', 'end').attr('fill', '#94a3b8').attr('font-size', 11)
      .text(d.label);
    g.append('rect').attr('x', 0).attr('y', y).attr('height', barH)
      .attr('width', Math.max(2, x(d.n))).attr('fill', colorFn(d, i)).attr('rx', 2);
    g.append('text').attr('x', x(d.n) + 6).attr('y', y + barH * 0.72)
      .attr('fill', '#e7ecf1').attr('font-size', 11).text(d.n);
  });
}

function drawDualHist(sel, seriesA, seriesB, labelA, labelB, fmtTick) {
  const host = d3.select(sel);
  host.selectAll('*').remove();
  const all = seriesA.concat(seriesB);
  if (!all.length) {
    host.append('div').attr('class', 'hist-legend').text('no samples');
    return;
  }
  host.append('div').attr('class', 'hist-legend')
    .html('<span class="a">■ '+esc(labelA)+' ('+seriesA.length+')</span> · <span class="b">■ '+esc(labelB)+' ('+seriesB.length+')</span>');
  const svg = host.append('svg');
  const w = Math.max(220, host.node().clientWidth || 280);
  const h = 88, m = { top: 6, right: 8, bottom: 22, left: 8 };
  svg.attr('width', w).attr('height', h);
  const innerW = w - m.left - m.right;
  const innerH = h - m.top - m.bottom;
  const x = d3.scaleLinear().domain(d3.extent(all)).nice().range([0, innerW]);
  const binsSpec = d3.bin().domain(x.domain()).thresholds(10);
  const bA = binsSpec(seriesA);
  const bB = binsSpec(seriesB);
  const y = d3.scaleLinear()
    .domain([0, d3.max(bA.concat(bB), d => d.length) || 1])
    .range([innerH, 0]);
  const g = svg.append('g').attr('transform', 'translate('+m.left+','+m.top+')');
  const bw = Math.max(1, (bA[0] ? x(bA[0].x1) - x(bA[0].x0) : innerW / 10) - 2);
  g.selectAll('rect.a').data(bA).enter().append('rect').attr('class', 'a')
    .attr('x', d => x(d.x0)).attr('y', d => y(d.length))
    .attr('width', bw).attr('height', d => innerH - y(d.length))
    .attr('fill', '#7dd3fc').attr('opacity', 0.75);
  g.selectAll('rect.b').data(bB).enter().append('rect').attr('class', 'b')
    .attr('x', d => x(d.x0) + 1).attr('y', d => y(d.length))
    .attr('width', bw).attr('height', d => innerH - y(d.length))
    .attr('fill', '#fbbf24').attr('opacity', 0.65);
  g.append('g').attr('transform', 'translate(0,'+innerH+')')
    .call(d3.axisBottom(x).ticks(4).tickFormat(fmtTick))
    .selectAll('text').attr('fill', '#94a3b8').attr('font-size', 9);
  g.selectAll('.domain, .tick line').attr('stroke', '#243044');
}

function drawSignals() {
  const steps = (series && series.steps) || [];
  const venueA = (series && series.venue_a) || 'A';
  const venueB = (series && series.venue_b) || 'B';
  const stats = signalStats(steps, xDomainFull, venueA, venueB);
  const total = stats.decisions + stats.misses;
  const ratio = stats.misses > 0
    ? (stats.decisions / stats.misses).toFixed(3)
    : (stats.decisions > 0 ? '∞' : '—');
  const pct = total ? ((100 * stats.decisions / total).toFixed(1) + '% decisions') : '';
  document.getElementById('sigSummary').textContent =
    'decisions ' + stats.decisions + ' / misses ' + stats.misses
    + '  ·  ratio ' + ratio
    + (pct ? '  ·  ' + pct : '');
  drawHBars('#sigKind', [
    { label: 'decision', n: stats.decisions },
    { label: 'miss', n: stats.misses },
  ], d => d.label === 'decision' ? '#34d399' : '#f87171');

  const reasons = Object.keys(stats.byReason).map(k => ({ label: k, n: stats.byReason[k].n, examples: stats.byReason[k].examples }))
    .sort((a, b) => b.n - a.n || a.label.localeCompare(b.label));
  const maxN = d3.max(reasons, d => d.n) || 1;
  const tbody = document.querySelector('#sigMiss tbody');
  tbody.innerHTML = reasons.map(d => {
    const w = Math.max(4, Math.round(48 * d.n / maxN));
    let dist = '—';
    if (d.label === 'stale_book') dist = '<div class="hist" id="histStale"></div>';
    else if (d.label === 'negative_edge') dist = '<div class="hist" id="histNet"></div>';
    const detail = reasonDetail(d.label, d, stats, venueA, venueB);
    return '<tr><td>'+esc(d.label)+'</td><td class="n"><span class="bar" style="width:'+w+'px"></span>'+d.n+'</td><td class="detail">'+esc(detail)+'</td><td>'+dist+'</td></tr>';
  }).join('');
  if (document.getElementById('histStale')) {
    drawDualHist('#histStale', stats.staleA, stats.staleB, venueA, venueB, d => d >= 1000 ? (d/1000).toFixed(1)+'s' : d+'ms');
  }
  if (document.getElementById('histNet')) {
    drawDualHist('#histNet', stats.netAB, stats.netBA, venueA+'→'+venueB, venueB+'→'+venueA, d => d3.format('.3~f')(d));
  }
}

async function load(url, opt) {
  const res = await fetch(url, opt);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

async function refresh() {
  series = await load('/sim?format=json');
  xDomainFull = null;
  document.getElementById('msg').textContent = series.message || '';
  renderCfg(series);
  draw();
}

document.getElementById('apply').onclick = async () => {
  series = await load('/sim/run', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify(overlayFromForm()),
  });
  xDomainFull = null;
  document.getElementById('msg').textContent = series.message || '';
  renderCfg(series);
  draw();
};
document.getElementById('gapZoom').oninput = draw;
document.getElementById('pnlZoom').oninput = draw;
document.getElementById('resetScale').onclick = () => {
  document.getElementById('gapZoom').value = 1;
  document.getElementById('pnlZoom').value = 1;
  xDomainFull = null;
  draw();
};
refresh();
