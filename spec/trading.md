# Paper arbitrage (trading)

How HHLD **paper arb** works today: detect a price gap between two venues, decide buy-low / sell-high, place simulated orders (no real capital), then audit orders and PnL.

Related: [mission.md](mission.md), [networking.md](networking.md), [concurrency.md](concurrency.md), [quality-assurance.md](quality-assurance.md), [roadmap/p3.md](roadmap/p3.md)–[p7.md](roadmap/p7.md).

## What “paper” means

- Orders go through the same `Exchange` interface as live will later.
- Venues may be **fake** (`src/exchange/fake`) or, later, live read adapters with paper fills.
- **No real capital** is risked. Acks/fills are simulated so strategy, risk, and audit can be proven end-to-end.

## Idea in one line

If venue A’s best **ask** is lower than venue B’s best **bid** by at least `min_gap` (after risk costs), buy on A and sell on B for the configured size.

```text
            Venue A (e.g. Hyperliquid)     Venue B (e.g. GRVT)
            ask 100.1                      bid 100.8
                 \                         /
                  \____ gap ≈ 0.7 ________/
                         buy A / sell B
```

## End-to-end pipeline

```text
Config (symbols, size, min_gap, venues, risk, timeouts)
        │
        ▼
Books from Exchange(s)     ← fake dual feed or (later) live reads
        │
        ▼
CrossVenueArb.OnBooks      ← emit Decision { TraceID, Legs[buy, sell] }
        │
        ▼
risk.Gate.TryAcquire       ← lock arb key; miss if busy/overloaded
risk.Gate.Evaluate         ← fees, latency, partial fill, stale/unhealthy books
        │
        ▼
strategy.PlaceDecision     ← PlaceOrder on each leg (concurrent)
        │
        ▼
admin.RecordPaperDecision  ← order store + pnl fills (reconstruct by TraceID)
```

## Modules involved

| Step | Package | Role |
|------|---------|------|
| Parameters | `config` | Symbols (multi-symbol), size, min gap, venue A/B, risk params, timeouts |
| Market data | `exchange` / `fake` | Normalized `Book` / `Tick`; delays for timeout tests |
| Decision | `strategy.CrossVenueArb` | Per-symbol gap detection → two-leg `Decision` |
| Execution | `strategy.PlaceDecision` | Concurrent paper `PlaceOrder` per leg |
| Risk | `risk.Gate` | Miss-more: reject bad edge / stale books; serialize per arb key |
| Audit | `admin` + `pnl` | Persist orders; record fills; realized PnL |
| Backtest input | `warehouse` + `crawl` | Persist crawled books/ticks; feed `sim.Replay` (offline, not hot path) |

## Decision shape

A successful arb `Decision` has:

- **`TraceID`** — links both legs for audit (e.g. `arb-BTCUSD-1`)
- **`HedgeID`** — empty for same-kind arb (used later for prediction↔crypto hedges)
- **`Legs`** — typically two:
  1. Buy on the venue with the lowest ask  
  2. Sell on the venue with the highest bid  
- **`Reason`** — human-readable gap note

Strategy never imports vendor SDKs; it only sees HHLD `Book` / `Symbol` / `VenueID`.

## Config knobs (paper arb)

From `configs/default.json` / `config.Config`:

- **`symbols`** — which HHLD symbols to evaluate (scales by list; BTCUSD first)
- **`trading.size`** — default order size on each leg  
- **`trading.min_gap`** — minimum raw gap before emitting a Decision  
- **`venues.a` / `venues.b`** — intended dual venues (wired by the runner)  
- **`symbol_map.<SYM>.venues`** — HHLD symbol → venue-native id (e.g. BTCUSD → HL `BTC`, GRVT `BTC_USDT_Perp`)  
- **`symbol_map.<SYM>.order_interval`** — min time between accepted places for that symbol (default `1s`; `0` disables)  
- **`symbol_map.<SYM>.max_volume_trade`** — max size per leg; effective size = `min(trading.size, max_volume_trade)`  
- **`risk.budgets.<venue/symbol>`** — process-lifetime notional cap `sum(price×size)` per venue+symbol (missing/`0` = unlimited)  
- **`risk.fees.<venue>`** — per-exchange fee model (`rate_bps`, `fixed`, `commission_bps`, `commission_fixed`; additive)  
- **`risk.fee_bps_per_leg`** — default rate when a venue is missing from `fees`  
- **`risk.*`** — latency penalty, partial-fill factor, max book age, max in-flight  
- **`timeouts.book` / `timeouts.order`** — budgets used with fake delays and (later) live calls  

`strategy.ArbConfigFrom(cfg)` maps config → `CrossVenueArb` (clamped sizes). Legacy flat `symbol_map` (`{"hyperliquid":"BTC",...}`) still loads; defaults fill `order_interval` / `max_volume_trade`.

## Risk before place (miss-more)

Even if Strategy sees a gap, Risk may **reject** (miss the opportunity):

- Gross edge does not survive fees + latency penalty + partial-fill assumption  
- A required book is missing or **stale**  
- A leg’s venue is marked **unhealthy**  
- Same arb key already in flight (`lock_busy`) or global cap hit (`overloaded`)  
- Leg size above `max_volume_trade` (`max_volume_exceeded`)  
- Place too soon after last accept for that symbol (`rate_limited`)  
- Notional spend would exceed `risk.budgets` for a venue+symbol (`budget_exceeded:venue/symbol`)  

Caller pattern: `TryAcquire` → `Evaluate` → `PlaceDecision` → `Release`.

Budget and rate limits are charged on **accepted Evaluate** (process lifetime; not persisted across restarts). If place fails after accept, the spend still counts (miss-more).

### Why fees live in Risk (not Exchange)

Fee **schedules** are a pre-trade cost model for miss-more gating, not venue I/O:

| Layer | Owns | Example |
|-------|------|---------|
| `risk.FeeSchedule` / `config.risk.fees` | Expected / worst-case cost **before** place | HL `rate_bps: 3.5`, GRVT `rate_bps` + `commission_fixed` |
| `exchange.Fill.Fee` | What was **actually** charged after a fill | Live adapter reports venue fee on the fill |

- **Risk** needs those numbers to accept or reject a `Decision` before `PlaceOrder`. Operators may set schedules **more conservative** than advertised venue rates.
- **Exchange** adapters own market data and order/fill transport. Putting policy schedules on `Exchange` would mix I/O with economic gates; Risk would still consume the same numbers.
- On paper today, `RecordPaperDecision` stamps the Risk schedule onto fills so PnL matches the gate. Later (live fills), prefer the real `Fill.Fee` from the venue; Risk may still gate on the configured schedule (or a conservative blend). Optional later: adapter `EstimateFee` as an input to Risk — Risk still decides.

### Estimation (VaR / winning rate)

Beyond the hard gate, Risk Management exposes an **`Estimator`** abstraction:

- Predict **winning rate** and/or **Value at Risk** for a `Decision`
- Implementations: detective **formulas** / Go sim stats in this repo; **ML in a research side project** that exports artifacts for a Go `Estimator` (`Estimate.Method`)
- Optional `risk.Manager` = `Gate` + `Estimator` (paper may ignore estimates until calibrated)

Simulation feeds calibration via `sim.Analyzer` (winning rate + distribution). See below.

## Simulation: winning rate and distribution

`sim.Simulator` replays books/ticks through Strategy/Risk → PnL.

`sim.Analyzer` (same abstraction style) can report:

- **`WinningRate`** — empirical P(win) over a run/filter  
- **`WinningDistribution`** — samples with dims  

`(symbol, gap, volume1, volume2, exchange1, exchange2, time)`

so Risk can condition estimates on gap size, venues, and size — not only a single global win rate.

## Event-driven market data (P8.5)

Live and fake feeds publish **book events** into an in-process bus; a **BookStore** applies snapshots and deltas; a **Runner** re-evaluates Strategy on every update.

Full wiring (connections, delta rules, automatic place): [architect.md](architect.md).

```text
HL / GRVT / fake  →  market.Bus (BookEvent)  →  BookStore.Apply
                                                 │
                                                 ▼ BookUpdated(venue, symbol)
                                              Runner
                                                 │
                    both venues have book? ──no──► miss
                                                 │ yes
                                                 ▼
                                    Strategy.OnBooks([bookA, bookB])
                                                 │
                                                 ▼
                              Risk.TryAcquire → Evaluate → Place → Admin
```

| Rule | Behavior |
|------|----------|
| **Event kinds** | `Snapshot` (full replace) or `Delta` (merge by price; size `0` deletes a level) |
| **Delta before snapshot** | Reject for that `(venue,symbol)` until a snapshot establishes the book |
| **Evaluate trigger** | Every successful apply for either venue (no coalesce window) |
| **Peer missing** | Miss — do not call `OnBooks` until both configured venues have a book for the symbol |
| **Strategy API** | Unchanged: still `OnBooks([]Book)` with full books; deltas stay below Strategy |
| **Risk** | Events must not bypass `TryAcquire` / in-flight caps ([concurrency.md](concurrency.md)) |
| **HL note** | WS `l2Book` is already a full snapshot each push → publish as Snapshot |
| **GRVT note** | Prefer `v1.book.d` for deltas; `v1.book.s` as Snapshot; reconnect resets with Snapshot |

Sim/backtest (P6) still replays book lists through `OnBooks` without requiring the bus.

## Data warehouse and backtest replay (P7)

The **warehouse** is HHLD’s local **backtest tape library**: store normalized market history once, replay it through the same Strategy/Risk path as paper trading. It is **not** on the live or paper **hot path** — trading reads **current** books from `Exchange`; the warehouse serves **offline** replay and analysis.

```text
Crawl (sample NDJSON / fake / later HL+GRVT)  →  Warehouse (SQLite)  →  sim.InputFromStore  →  sim.Replay  →  PnL / win rate
```

### Purpose of SQLite (v1)

| Role | Why |
|------|-----|
| **Persist crawled data** | Books/ticks land in one normalized store instead of ad-hoc files per run |
| **Feed P6 backtest** | Query by symbol + time range → `sim.Input` for `Replay.Run` |
| **Same types as live** | Stored as HHLD `exchange.Book` / `exchange.Tick`, not vendor JSON |
| **Low cost** | Single `.db` file, no DB server — fits solo-operator / one-instance deploy |

Load sample data:

```bash
go run ./cmd/hhld-crawl -sample data/samples/btcusd_books.ndjson -db ./hhld.db
```

In Go: `crawl.SampleFile` or `crawl.FakeDual` → `warehouse.OpenSQLite` → `sim.InputFromStore` → `sim.NewReplay().Run(...)`.

Orders and realized PnL from backtests still go through `admin` / `pnl` during replay; the warehouse holds **market input only**, not trading audit.

### JSON vs BSON (and Parquet)

**JSON is the default for HHLD v1** — human-readable, already used for config, crawl samples, and admin HTTP.

| Format | Use in HHLD | Notes |
|--------|-------------|-------|
| **JSON / NDJSON** | Crawl interchange (`data/samples/*.ndjson`), nested bids/asks inside SQLite rows | Easy to inspect, diff, and debug; matches Go `encoding/json` |
| **SQLite + JSON columns** | Warehouse v1 (`warehouse.SQLite`) | Scalar fields (symbol, time, venue) indexed; book levels as JSON text — fine at P7 scale |
| **BSON** | Not used today | Fits MongoDB-style document stores or very large binary archives; adds tooling cost without benefit at current volume |
| **Parquet** | Later, optional | Columnar exports for long historical analytics ([tech-stack.md](tech-stack.md)); not required for core SQLite warehouse |

Rule of thumb:

- **JSON** for crawl files, config, HTTP APIs, and nested book levels in SQLite.
- **BSON** only if you adopt a document DB or hit size/perf limits JSON cannot handle.
- **Parquet** when exporting large history for external analysis — not the primary v1 store.

## Paper place and partial failure

`PlaceDecision` places **all legs concurrently**. If one venue times out and the other accepts:

- Results include per-leg success/error (1-leg failure)  
- `RecordPaperDecision` still writes both order rows (`accepted` vs `error`)  
- Only successful legs get PnL fills  

This matches [networking.md](networking.md): unknown/failed legs must be auditable; unpaired exposure is a later recovery playbook (P10), not ignored.

## PnL on paper arb

For the same symbol, buy then sell inventory-matches:

- Realized ≈ `(sell_price - buy_price) * size - fees`  
- Fees use the same per-venue schedule as the gate (`risk.FeeSchedule` / `risk.fees` in config): rate (bps), fixed amount, and/or commission — summed per leg  
- `admin.RecordPaperDecision(..., fees)` writes that fee onto each successful fill  
- Audit can list orders by `TraceID` and compare to fills on the tracker  

Unrealized mark-to-market is deferred (`Unrealized` is `"0"` in the memory tracker for now).

## Audit dashboard (lightweight)

No polished charts yet (P5 skipped visualization). Operators inspect PnL and orders via:

| URL | Purpose |
|-----|---------|
| `GET /trading/pnl` | Realized / unrealized snapshot (HTML) |
| `GET /trading/pnl?format=json` | Same as JSON |
| `GET /trading/orders` | Order table; query `trace_id`, `hedge_id`, `venue`, `symbol` |
| `GET /trading/orders?format=json` | Same as JSON |

Run locally:

```bash
go run ./cmd/hhld -demo
# open http://127.0.0.1:8080/trading/pnl
```

`-demo` seeds one paper arb (`trace_id=demo-arb-1`) so the pages are non-empty. Wire your live `admin.Auditor` into `admin.Handler` for a real session.

## What paper arb is not

- Not live capital or real exchange order placement (P8 adapters are **read-only**; orders return `ErrReadOnly`)  
- Not prediction-market trading or prediction↔crypto hedge (later)  
- Not full historical warehouse coverage (P7 is minimal SQLite + JSON/NDJSON crawl; Parquet/multi-cloud later) or paper-on-live loop (P9)  

## Minimal mental example

1. Fake HL book: ask `100.1`; fake GRVT book: bid `100.8`; `min_gap = 0.3` → Decision.  
2. Risk: fees/latency leave positive net edge; books fresh → OK.  
3. Paper place both legs → two `accepted` acks.  
4. Admin: two orders under one `TraceID`; PnL realized ≈ gap − modeled fees (e.g. 5 bps/leg on default config).  

That loop is the V1 paper-trading path HHLD is built to prove before live gates open.
