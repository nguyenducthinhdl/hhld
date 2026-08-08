# Paper arbitrage (trading)

How HHLD **paper arb** works today: detect a price gap between two venues, decide buy-low / sell-high, place simulated orders (no real capital), then audit orders and PnL.

Related: [mission.md](mission.md), [networking.md](networking.md), [concurrency.md](concurrency.md), [roadmap/p3.md](roadmap/p3.md)–[p5.md](roadmap/p5.md).

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
- **`trading.size`** — size on each leg  
- **`trading.min_gap`** — minimum raw gap before emitting a Decision  
- **`venues.a` / `venues.b`** — intended dual venues (wired by the runner)  
- **`risk.*`** — fee bps, latency penalty, partial-fill factor, max book age, max in-flight  
- **`timeouts.book` / `timeouts.order`** — budgets used with fake delays and (later) live calls  

`strategy.ArbConfigFrom(cfg)` maps config → `CrossVenueArb`.

## Risk before place (miss-more)

Even if Strategy sees a gap, Risk may **reject** (miss the opportunity):

- Gross edge does not survive fees + latency penalty + partial-fill assumption  
- A required book is missing or **stale**  
- A leg’s venue is marked **unhealthy**  
- Same arb key already in flight (`lock_busy`) or global cap hit (`overloaded`)  

Caller pattern: `TryAcquire` → `Evaluate` → `PlaceDecision` → `Release`.

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

## Paper place and partial failure

`PlaceDecision` places **all legs concurrently**. If one venue times out and the other accepts:

- Results include per-leg success/error (1-leg failure)  
- `RecordPaperDecision` still writes both order rows (`accepted` vs `error`)  
- Only successful legs get PnL fills  

This matches [networking.md](networking.md): unknown/failed legs must be auditable; unpaired exposure is a later recovery playbook (P10), not ignored.

## PnL on paper arb

For the same symbol, buy then sell inventory-matches:

- Realized ≈ `(sell_price - buy_price) * size - fees`  
- Audit can list orders by `TraceID` and compare to fills on the tracker  

Unrealized mark-to-market is deferred (`Unrealized` is `"0"` in the memory tracker for now).

## What paper arb is not

- Not live capital or real exchange order placement  
- Not prediction-market trading or prediction↔crypto hedge (later)  
- Not full backtest warehouse replay (P6–P7) or live HL/GRVT adapters (P8–P9)  

## Minimal mental example

1. Fake HL book: ask `100.1`; fake GRVT book: bid `100.8`; `min_gap = 0.3` → Decision.  
2. Risk: fees/latency leave positive net edge; books fresh → OK.  
3. Paper place both legs → two `accepted` acks.  
4. Admin: two orders under one `TraceID`; PnL realized ≈ `0.7` per unit size (before modeled fees in PnL fills).  

That loop is the V1 paper-trading path HHLD is built to prove before live gates open.
