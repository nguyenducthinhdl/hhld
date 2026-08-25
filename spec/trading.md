# Paper arbitrage (trading)

How HHLD **paper arb** works today: detect a price gap between two venues, decide buy-low / sell-high, place simulated orders (no real capital), then audit orders and PnL.

Related: [mission.md](mission.md), [networking.md](networking.md), [concurrency.md](concurrency.md), [quality-assurance.md](quality-assurance.md), [roadmap/p3.md](roadmap/p3.md)–[p7.md](roadmap/p7.md).

## What “paper” means

- Orders go through the same `Exchange` interface as live will later.
- Venues may be **fake** (`src/exchange/fake`) or, later, live read adapters with paper fills.
- **No real capital** is risked. Acks/fills are simulated so strategy, risk, and audit can be proven end-to-end.



## Idea in one line

If venue A’s best **ask** is lower than venue B’s best **bid** by at least `min_gap` (USD quote), Strategy emits buy-A / sell-B at `max_size`. Risk then accepts only if **net edge** after fees, latency, and partial-fill is still positive.

```text
            Venue A (e.g. Hyperliquid)     Venue B (e.g. GRVT)
            ask 100.1                      bid 100.8
                 \                         /
                  \____ gap ≈ 0.7 ________/
                         buy A / sell B
```



## End-to-end pipeline

```text
Config (symbols, min_size, max_size, min_gap, venues, risk, timeouts)
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


| Step           | Package                  | Role                                                                            |
| -------------- | ------------------------ | ------------------------------------------------------------------------------- |
| Parameters     | `config`                 | Symbols (multi-symbol), min/max size, min gap, venue A/B, risk params, timeouts |
| Market data    | `exchange` / `fake`      | Normalized `Book` / `Tick`; delays for timeout tests                            |
| Decision       | `strategy.CrossVenueArb` | Per-symbol gap detection → two-leg `Decision`                                   |
| Execution      | `strategy.PlaceDecision` | Concurrent paper `PlaceOrder` per leg                                           |
| Risk           | `risk.Gate`              | Miss-more: reject bad edge / stale books; serialize per arb key                 |
| Audit          | `admin` + `pnl`          | Persist orders; record fills; realized PnL                                      |
| Backtest input | `warehouse` + `crawl`    | Persist crawled books/ticks; feed `sim.Replay` (offline, not hot path)          |




## Decision shape

A successful arb `Decision` has:

- `TraceID` — links both legs for audit (e.g. `arb-BTCUSD-1`)
- `HedgeID` — empty for same-kind arb (used later for prediction↔crypto hedges)
- `Legs` — typically two:
  1. Buy on the venue with the lowest ask
  2. Sell on the venue with the highest bid
- `Reason` — human-readable gap note

Strategy never imports vendor SDKs; it only sees HHLD `Book` / `Symbol` / `VenueID`.

## Config knobs (paper arb)

From `configs/default.json` / `config.Config`. For BTCUSD, **quote = USD** and **base = BTC**. Extra same-kind USD perps live in their own files: [`configs/craw-ethusd.json`](../configs/craw-ethusd.json) (HL `ETH` / GRVT `ETH_USDT_Perp`), [`configs/craw-solusd.json`](../configs/craw-solusd.json) (HL `SOL` / GRVT `SOL_USDT_Perp`), [`configs/craw-trumpusd.json`](../configs/craw-trumpusd.json) (HL `TRUMP` / GRVT `TRUMP_USDT_Perp`). `min_gap` is still USD quote (~9 bps of that market’s price). ETHBTC is **not** mapped: neither venue lists an ETH–BTC perp (see [hyperliquid.md](exchange/hyperliquid.md) / [grvt.md](exchange/grvt.md)). Other symbols use that market’s quote/base.

`strategy.ArbConfigFrom(cfg)` maps each `symbol_map` row → `CrossVenueArb` sizes and min gaps.

### Process-wide


| Knob                  | Unit            | Default | Meaning                                              |
| --------------------- | --------------- | ------- | ---------------------------------------------------- |
| `venues.a` / `venues.b` | venue id      | `hyperliquid` / `grvt` | Dual venues wired by the runner            |
| `timeouts.book`       | duration        | `25ms`  | Fake / live book-call latency budget                 |
| `timeouts.order`      | duration        | `25ms`  | Fake / live `PlaceOrder` latency budget              |
| `risk.max_in_flight`  | count           | `4`     | Global concurrent Risk+exec pipelines                |


### Per symbol (`symbol_map[]`)


| Knob | Unit | Default (BTCUSD) | Meaning |
| ---- | ---- | ---------------- | ------- |
| `symbol` | HHLD id | `BTCUSD` | Canonical symbol for Strategy / Risk / audit |
| `trading.strategy` | name | `cross-venue-arb` | Strategy implementation |
| `trading.kind` | enum | `perp` | Instrument kind |
| `trading.min_size` | **base** (BTC) | `0.0001` | Floor; `EffectiveSize` will not go below this |
| `trading.max_size` | **base** (BTC) | `0.0003` | Size Strategy emits; Risk rejects legs above this (`max_volume_exceeded`) |
| `trading.min_value` | **USD** notional | `10` | Reject leg if `price × size` is below this (`notional_below_min`; HL-style floor) |
| `trading.max_value` | **USD** notional | `50` | Reject leg if `price × size` is above this (`notional_above_max`) |
| `trading.min_gap` | **USD** (quote price) | `0.3` | Minimum raw `sell_bid − buy_ask` to emit a Decision — **not** a percent |
| `trading.order_interval` | duration | `1s` | Min time between accepted places; `0` disables |
| `risk.fee_bps_per_leg` | **bps** (1 bps = 0.01%) | `5` | Fallback rate when a venue has no `fees` entry |
| `risk.latency_penalty` | **USD per base** | `0.05` | Conservative haircut for price move between seeing the books and filling; **not** a timeout (`timeouts.*`) and **not** book age (`max_book_age`). Subtracted as `penalty × size_modeled` |
| `risk.partial_fill_factor` | fraction `(0, 1]` | `0.95` (= **95%**) | Pre-trade assumption: only this fraction of each leg fills. Scales `size_modeled` in the edge formula; does **not** change the size Strategy places. `≤0` or `>1` → treated as `1` |
| `risk.max_book_age` | duration | `2s` | Reject if `now − book.Time` exceeds this |
| `venues.<venue>.symbol_name` | venue-native id | HL `BTC`, GRVT `BTC_USDT_Perp` | Exchange instrument string |
| `venues.<venue>.budget` | **USD** notional | `50` | Process-lifetime cap `sum(price × size)` per venue+symbol; missing/`0` = unlimited |


### Per venue, per side (`venues.<venue>.fees.buy` / `.sell`)

Components are **additive**. Unused fields stay `0`.


| Knob | Unit | Example | Meaning |
| ---- | ---- | ------- | ------- |
| `rate_bps` | **bps** of notional (1 bps = 0.01%) | HL `4.5`, GRVT `4.5` | Perp **taker** fee (both buy and sell; arb crosses) |
| `fixed` | **USD** per fill | `0.10` | Flat venue fee |
| `commission_bps` | **bps** of notional | `1` | Broker / referral commission rate |
| `commission_fixed` | **USD** per fill | `0.01` | Flat commission |


JSON may use a flat `fees` object (copied onto both sides) or explicit `{ "buy": …, "sell": … }`. Paper arb is **taker-taker**: `buy` and `sell` both get the venue’s taker rate, not maker.

## Trading decision formula

Two stages: Strategy **emits** on a raw USD gap; Risk **accepts** only if modeled net PnL is still positive.

### 1. Strategy emit (`CrossVenueArb.OnBooks`)

Need at least two same-symbol books with a top-of-book bid and ask. Across those books:

```text
buy  = venue with the lowest  best ask
sell = venue with the highest best bid

if buy.venue == sell.venue → miss (no cross)

gap = sell.best_bid − buy.best_ask          # USD (quote)

emit Decision  iff  gap ≥ min_gap
size_emitted   = trading.max_size           # BTC; at least min_size
```

Legs: **buy** at `buy.best_ask` on the cheap venue, **sell** at `sell.best_bid` on the rich venue. Same `size_emitted` on both. Both legs **cross** the book (lift ask / hit bid) → **takers**. `Reason` looks like `gap=0.7000 >= min=0.3000`.

Worked example (prices from the diagram above, `min_gap = 0.3` USD):

```text
HL ask 100.1    GRVT bid 100.8
gap = 100.8 − 100.1 = 0.7 USD  ≥  0.3  → emit buy HL / sell GRVT
```

Strategy does **not** subtract fees. A gap above `min_gap` can still be rejected by Risk.

### 2. Risk accept (`Gate.Evaluate` → `edgeSurvives`)

```text
size_modeled = min(buy.size, sell.size) × partial_fill_factor     # BTC

gross    = (sell_bid − buy_ask) × size_modeled                    # USD
fee      = Fee(buy.venue,  buy,  buy_ask,  size_modeled)
         + Fee(sell.venue, sell, sell_bid, size_modeled)          # USD
latency  = latency_penalty × size_modeled                         # USD

net = gross − fee − latency                                       # USD

accept iff net > 0
```

`partial_fill_factor` is a **worst-case fill ratio**, not an order-size cap. Strategy still places `max_size`; budget still charges `price × emitted size`. Risk only pretends `95%` (default) of that size actually fills when computing `gross` / rate fees / latency. Proportional costs (bps, `latency_penalty`) shrink with `size_modeled`, so the **sign** of `net` only flips when **fixed** fees (`fixed`, `commission_fixed`) stay full-size and eat the remaining edge. `1` (or unset/`0`) means “assume a complete fill.”

`latency_penalty` is a **modeled slippage/latency cost**, not a clock. Risk pretends the quoted gap is worse by `penalty` USD per BTC so a thin edge that would vanish while orders are in flight is rejected. Raise it to miss more; `0` disables the haircut. Distinct from `timeouts.book` / `timeouts.order` (I/O budgets) and `max_book_age` (stale-book reject).

Reject reason is `negative_edge` (plus `net` / `gross` / `fee` / `latency` in `Verdict.Info`). Other gates (stale book, unhealthy, lock, volume, rate, budget) still apply — see below.

Same identities are what `risk.ExplainDecision` prints on `/sim` hover (`formula`, `gross`, `fee`, `latency`, `net`).

## Taker-taker vs maker-taker

Both styles are **same-kind cross-venue arb**: buy the cheap venue, sell the rich venue, flatten so there is no directional BTC/ETH/SOL/TRUMP bet. Maker vs taker is **how** a leg meets the book (rest vs cross), not buy vs sell.

| | Taker-taker (`cross-venue-arb`, implemented) | Maker-taker (investigation; not a Go `Strategy` yet) |
|---|---|---|
| Signal | `gap_tt = sell.best_bid − buy.best_ask` | `gap_mt` at a **resting** maker price vs the hedge TOB |
| Orders | Lift cheap ask + hit rich bid | **Post-only rest** on one venue; when it fills, **take** the other |
| Fill | Immediate (paper / `sim.Replay` fake place) | Delayed: fill only if a later **tick** trades through the rest |
| Fees | Taker + taker (~9 bps at Tier-0 / Level-1) | Maker + taker (~6 bps HL maker 1.5 + GRVT taker 4.5, or ~4.5 bps GRVT maker ≈ 0 + HL taker 4.5) |
| Extra risk | One-leg timeout | Queue wait, adverse selection, unmatched inventory if hedge misses |

Direction is always **sell rich / buy cheap** (the same identity as [`CrossVenueArb`](../src/strategy/arb.go)). A typical maker-taker rest is a **maker sell on the rich book** (join/improve the ask, do not cross). Someone lifts you → short on the expensive venue → immediately lift the cheap ask to flatten. The symmetric quote is maker buy on cheap, taker sell on rich. The short (or long) is only inventory between the two fills.

```text
# Maker sell on rich, taker buy on cheap (symmetric for maker buy)
maker_px = rich.best_ask          # join or improve; do not cross
hedge_px = cheap.best_ask         # lift when maker fills
gap_mt   = maker_px − hedge_px    # USD quote
```

**Quote-time edge** (accept to rest):

```text
gross_quote = (maker_px − hedge_px_now) × size
fee_quote   = Cost(maker_venue, maker, maker_px, size)
            + Cost(hedge_venue, taker, hedge_px_now, size)
net_quote   = gross_quote − fee_quote − hedge_latency × size
rest iff net_quote > 0
```

**Realized** (what PnL must confirm): hedge is taken at the **then-current** cheap book, not the book at quote time.

```text
gross_real = (maker_fill_px − hedge_fill_px) × filled_size
fee_real   = maker_fee(filled) + taker_fee(hedge_fill)
net_real   = gross_real − fee_real
# unmatched maker fill: inventory until flatten; count as a loss path if hedge fails
```

Config today keys fees by **buy/sell**, not maker/taker (`rate_bps` must be ≥ 0, so GRVT’s tiny maker rebate cannot be stored). Investigation needs per-venue `{ maker, taker }` schedules and a `Leg.Role`. Until that lands, Layer A (below) can overlay maker bps by hand.

Starting perp table (Tier-0 / Level-1):

| Venue | Taker | Maker (investigation) |
|-------|-------|------------------------|
| Hyperliquid | 4.5 bps | 1.5 bps |
| GRVT | 4.5 bps | 0 (rebate ~−0.001 bps; treat as 0 until negative `rate_bps` is allowed) |

### How to simulate (confirm signal, fee, PnL)

Use the **same crawled tape** for both styles. `sim.Replay` today is taker-taker only and **ignores ticks**. Maker-taker Layer B needs ticks.

**Layer A — fee counterfactual (books only, upper bound).** Same (or maker-taker) signals, assume instant fill at quoted prices, apply maker+taker fees. Answers: if every maker filled immediately, would net still be positive? Not a real maker strategy.

**Layer B — rest then hedge (books + ticks).** Time-merge books and ticks. Rest on dual-book updates; fill maker on trade-through (back-of-queue: through, not touch); hedge at the current other book; cancel if `net_quote` dies or books go stale. `WinningRate` = P(net_real > 0 \| two-leg complete). Also report expected PnL **including unmatched** maker fills. Cancelled never-filled rests are not losses.

Accept maker-taker as a candidate only if Layer B realized PnL **and** unpaired-exposure rate beat taker-taker on that tape after fees — not if only Layer A looks good.

First-pass maker venue is **Hyperliquid** (crawl includes HL ticks). GRVT feeds are 1s book polls with **no trades** → do not claim GRVT-as-maker until `subscribe_ticks` is on that crawl.

Replay (taker-taker today; overlay fees/min_gap on `/sim`):

```bash
go run ./cmd/hhld-sim -ndjson data/crawl/btcusd-live-180m.ndjson -config configs/default.json
go run ./cmd/hhld-sim -ndjson data/crawl/ethusd-live.ndjson -config configs/craw-ethusd.json
go run ./cmd/hhld-sim -ndjson data/crawl/solusd-live-5m.ndjson -config configs/craw-solusd.json
go run ./cmd/hhld-sim -ndjson data/crawl/trumpusd-live-60m.ndjson -config configs/craw-trumpusd.json
```

Confirm per run: signal counts (emit / reject / fill), per-leg fee parts vs PnL drag, realized PnL and win rate. TRUMPUSD is a low-priced name (~$1.4): taker-taker fee hurdle is ~9 bps ≈ **$0.013** per TRUMP, not the BTCUSD demo `min_gap = 0.3`.

## Risk before place (miss-more)

Even if Strategy sees a gap, Risk may **reject** (miss the opportunity):

- Gross edge does not survive fees + latency penalty + partial-fill assumption (`negative_edge`; `net ≤ 0`)  
- A required book is missing or **stale** (`now − book.Time > max_book_age`)  
- A leg’s venue is marked **unhealthy**  
- Same arb key already in flight (`lock_busy`) or global cap hit (`overloaded`)  
- Leg size above `trading.max_size` (`max_volume_exceeded`)  
- Place too soon after last accept for that symbol (`rate_limited`)  
- Notional spend would exceed `venues.<venue>.budget` (USD) for a venue+symbol (`budget_exceeded:venue/symbol`)

Caller pattern: `TryAcquire` → `Evaluate` → `PlaceDecision` → `Release`.

Budget and rate limits are charged on **accepted Evaluate** (process lifetime; not persisted across restarts). If place fails after accept, the spend still counts (miss-more).

```text
notional_leg = price × size                       # USD; emitted size, not size_modeled
reject iff  spent[venue/symbol] + notional_leg > budget
on accept:  spent[venue/symbol] += notional_leg
```

### Why fees live in Risk (not Exchange)

Fee **schedules** are a pre-trade cost model for miss-more gating, not venue I/O:


| Layer                                   | Owns                                        | Example                                                  |
| --------------------------------------- | ------------------------------------------- | -------------------------------------------------------- |
| `risk.FeeSchedule` / `symbol_map[].venues.<venue>.fees` | Expected / worst-case cost **before** place | HL and GRVT buy/sell `rate_bps: 4.5` (Tier-0 / Level-1 perp taker 0.045%) |
| `exchange.Fill.Fee`                     | What was **actually** charged after a fill  | Live adapter reports venue fee on the fill               |


- **Risk** needs those numbers to accept or reject a `Decision` before `PlaceOrder`. Operators may set schedules **more conservative** than advertised venue rates.
- **Exchange** adapters own market data and order/fill transport. Putting policy schedules on `Exchange` would mix I/O with economic gates; Risk would still consume the same numbers.
- On paper today, `RecordPaperDecision` stamps the Risk schedule onto fills so PnL matches the gate. Later (live fills), prefer the real `Fill.Fee` from the venue; Risk may still gate on the configured schedule (or a conservative blend). Optional later: adapter `EstimateFee` as an input to Risk — Risk still decides.

### Fee calculation

`risk.FeeSchedule.Cost` / `SideFee.Parts` (`src/risk/fee.go`). One leg:

```text
notional = price × size                         # USD

rate_fee         = notional × rate_bps         / 10_000
commission_rate  = notional × commission_bps   / 10_000
fixed            = fixed                        # USD / fill
commission_fixed = commission_fixed             # USD / fill

Fee(venue, side, price, size)
  = rate_fee + commission_rate + fixed + commission_fixed     # USD
```

Unknown venue (no `venues.<id>.fees`) falls back to the rate-only path:

```text
Fee = notional × fee_bps_per_leg / 10_000
```

`price ≤ 0` or `size ≤ 0` → fee `0`. Buy and sell may differ. Rate terms scale with `size`; `fixed` and `commission_fixed` are **per fill** (they do not shrink with `partial_fill_factor`).

Worked example using `partial_fill_factor = 0.95` (size `1` BTC so the arithmetic is readable; default `max_size` is `0.0003`):

```text
Buy  HL   @ 100.1   rate_bps = 4.5     # Tier-0 perp taker 0.045%
Sell GRVT @ 100.8   rate_bps = 4.5     # Level-1 perp taker 0.045%

size_modeled = 1 × 0.95 = 0.95

fee_buy  = 100.1 × 0.95 × 4.5 / 10_000            = 0.042793  USD
fee_sell = 100.8 × 0.95 × 4.5 / 10_000            = 0.043092  USD
fee      = 0.085885 USD

gross    = (100.8 − 100.1) × 0.95                 = 0.665 USD
latency  = 0.05 × 0.95                            = 0.0475 USD
net      = 0.665 − 0.085885 − 0.0475              ≈ 0.532 USD  > 0  → accept
```

### Estimation (VaR / winning rate)

Beyond the hard gate, Risk Management exposes an `Estimator` abstraction:

- Predict **winning rate** and/or **Value at Risk** for a `Decision`
- Implementations: detective **formulas** / Go sim stats in this repo; **ML in a research side project** that exports artifacts for a Go `Estimator` (`Estimate.Method`)
- Optional `risk.Manager` = `Gate` + `Estimator` (paper may ignore estimates until calibrated)

Simulation feeds calibration via `sim.Analyzer` (winning rate + distribution). See below.

## Simulation: winning rate and distribution

`sim.Simulator` replays books/ticks through Strategy/Risk → PnL.

`sim.Analyzer` (same abstraction style) can report:

- `WinningRate` — empirical P(win) over a run/filter  
- `WinningDistribution` — samples with dims

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


| Rule                      | Behavior                                                                                |
| ------------------------- | --------------------------------------------------------------------------------------- |
| **Event kinds**           | `Snapshot` (full replace) or `Delta` (merge by price; size `0` deletes a level)         |
| **Delta before snapshot** | Reject for that `(venue,symbol)` until a snapshot establishes the book                  |
| **Evaluate trigger**      | Every successful apply for either venue (no coalesce window)                            |
| **Peer missing**          | Miss — do not call `OnBooks` until both configured venues have a book for the symbol    |
| **Strategy API**          | Unchanged: still `OnBooks([]Book)` with full books; deltas stay below Strategy          |
| **Risk**                  | Events must not bypass `TryAcquire` / in-flight caps ([concurrency.md](concurrency.md)) |
| **HL note**               | WS `l2Book` is already a full snapshot each push → publish as Snapshot                  |
| **GRVT note**             | Prefer `v1.book.d` for deltas; `v1.book.s` as Snapshot; reconnect resets with Snapshot  |


Sim/backtest (P6) still replays book lists through `OnBooks` without requiring the bus.

## Data warehouse and backtest replay (P7)

The **warehouse** is HHLD’s local **backtest tape library**: store normalized market history once, replay it through the same Strategy/Risk path as paper trading. It is **not** on the live or paper **hot path** — trading reads **current** books from `Exchange`; the warehouse serves **offline** replay and analysis.

```text
Crawl (sample NDJSON / fake / later HL+GRVT)  →  Warehouse (SQLite)  →  sim.InputFromStore  →  sim.Replay  →  PnL / win rate
```

### Research crawl tapes (HL + GRVT)

Each extra symbol has its **own** HHLD config (not mixed into [`configs/default.json`](../configs/default.json)) plus a feed file. Same feed shape: HL `subscribe_book` + `subscribe_ticks`, GRVT `snapshot_book` interval `1s`.

| HHLD symbol | Trading knobs | Feed | Native HL / GRVT | Notes |
|-------------|---------------|------|------------------|-------|
| `BTCUSD` | [`configs/default.json`](../configs/default.json) | [`configs/crawl.json`](../configs/crawl.json) | `BTC` / `BTC_USDT_Perp` | First tape |
| `ETHUSD` | [`configs/craw-ethusd.json`](../configs/craw-ethusd.json) | [`configs/crawl-ethusd.json`](../configs/crawl-ethusd.json) | `ETH` / `ETH_USDT_Perp` | |
| `SOLUSD` | [`configs/craw-solusd.json`](../configs/craw-solusd.json) | [`configs/crawl-solusd.json`](../configs/crawl-solusd.json) | `SOL` / `SOL_USDT_Perp` | GRVT min notional $5 |
| `TRUMPUSD` | [`configs/craw-trumpusd.json`](../configs/craw-trumpusd.json) | [`configs/crawl-trumpusd.json`](../configs/crawl-trumpusd.json) | `TRUMP` / `TRUMP_USDT_Perp` | Low USD price; size/gap knobs in the trading file |

`ETHBTC` is **not** crawled: neither venue lists an ETH–BTC perp.

```bash
go run ./cmd/hhld-feed -config configs/crawl.json
go run ./cmd/hhld-feed -config configs/crawl-ethusd.json
go run ./cmd/hhld-feed -config configs/crawl-solusd.json
go run ./cmd/hhld-feed -config configs/crawl-trumpusd.json
```

### Purpose of SQLite (v1)


| Role                     | Why                                                                        |
| ------------------------ | -------------------------------------------------------------------------- |
| **Persist crawled data** | Books/ticks land in one normalized store instead of ad-hoc files per run   |
| **Feed P6 backtest**     | Query by symbol + time range → `sim.Input` for `Replay.Run`                |
| **Same types as live**   | Stored as HHLD `exchange.Book` / `exchange.Tick`, not vendor JSON          |
| **Low cost**             | Single `.db` file, no DB server — fits solo-operator / one-instance deploy |


Load sample data:

```bash
go run ./cmd/hhld-crawl -sample data/samples/btcusd_books.ndjson -db ./hhld.db
```

In Go: `crawl.SampleFile` or `crawl.FakeDual` → `warehouse.OpenSQLite` → `sim.InputFromStore` → `sim.NewReplay().Run(...)`.

Orders and realized PnL from backtests still go through `admin` / `pnl` during replay; the warehouse holds **market input only**, not trading audit. Live/local realized PnL belongs in the [trading ledger](ledger.md), not in `books`/`ticks`.

### JSON vs BSON (and Parquet)

**JSON is the default for HHLD v1** — human-readable, already used for config, crawl samples, and admin HTTP.


| Format                    | Use in HHLD                                                                      | Notes                                                                                                                   |
| ------------------------- | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| **JSON / NDJSON**         | Crawl interchange (`data/samples/*.ndjson`), nested bids/asks inside SQLite rows | Easy to inspect, diff, and debug; matches Go `encoding/json`                                                            |
| **SQLite + JSON columns** | Warehouse v1 (`warehouse.SQLite`)                                                | Scalar fields (symbol, time, venue) indexed; book levels as JSON text — fine at P7 scale                                |
| **BSON**                  | Not used today                                                                   | Fits MongoDB-style document stores or very large binary archives; adds tooling cost without benefit at current volume   |
| **Parquet**               | Later, optional                                                                  | Columnar exports for long historical analytics ([tech-stack.md](tech-stack.md)); not required for core SQLite warehouse |


Rule of thumb:

- **JSON** for crawl files, config, HTTP APIs, and nested book levels in SQLite.
- **BSON** only if you adopt a document DB or hit size/perf limits JSON cannot handle.
- **Parquet** when exporting large history for external analysis — not the primary v1 store.



## Paper place and partial failure

`PlaceDecision` places **all legs concurrently**. If one venue times out and the other accepts:

- Results include per-leg success/error (1-leg failure)  
- `RecordPaperDecision` still writes both order rows (`accepted` vs `error`)  
- Only successful legs get PnL fills

This matches [networking.md](networking.md): unknown/failed legs must be auditable; unpaired exposure is halted and traced in [P10](roadmap/p10.md) (operator flatten; no invented fill).

## PnL on paper arb

For the same symbol, buy then sell inventory-matches:

- Realized ≈ `(sell_price − buy_price) × size − fees` (USD)  
- Fees use the same per-venue, per-side formula as the gate (see [Fee calculation](#fee-calculation))  
- `admin.RecordPaperDecision(..., fees)` writes that fee onto each successful fill  
- Audit can list orders by `TraceID` and compare to fills on the tracker

Unrealized mark-to-market is deferred (`Unrealized` is `"0"` in the memory tracker for now).

**Persistence:** P5 keeps orders and fills **in RAM**. The warehouse SQLite (`books`, `ticks`) is market data only and **cannot** realize PnL. The P11 trading ledger (`orders`, `fills`, `positions_strategy`, `positions_venue`) is specified in [ledger.md](ledger.md): fills are the source of truth; strategy matching is `(env, symbol, kind)` so a HL buy and GRVT sell of the same perp **does** realize; venue books keep unpaired inventory.

## Audit dashboard (lightweight)

No polished charts yet (P5 skipped visualization). Operators inspect PnL and orders via:


| URL                               | Purpose                                                         |
| --------------------------------- | --------------------------------------------------------------- |
| `GET /trading/pnl`                | Realized / unrealized snapshot (HTML)                           |
| `GET /trading/pnl?format=json`    | Same as JSON                                                    |
| `GET /trading/orders`             | Order table; query `trace_id`, `hedge_id`, `venue`, `symbol`    |
| `GET /trading/orders?format=json` | Same as JSON                                                    |
| `GET /trading/market`             | Dual books, gap, trade signal, signal config (HTML; polls JSON) |
| `GET /trading/market?format=json` | Same as JSON; optional `symbol=`                                |
| `GET /sim`                        | Crawl replay: D3 gap/PnL/signals; hover shows gap formula, venues/sides, size, fees |
| `GET /sim?format=json`            | Series JSON                                                     |
| `POST /sim/run`                   | Overlay knobs (venues, min_gap, …) and re-run                   |


Run locally:

```bash
go run ./cmd/hhld -demo
# open http://127.0.0.1:8080/trading/pnl

go run ./cmd/hhld -demo-market
# open http://127.0.0.1:8080/trading/market

go run ./cmd/hhld -paper-live
# same URLs — live Hyperliquid + GRVT books (paper place via fake)
# → http://127.0.0.1:8080/trading/pnl
# → http://127.0.0.1:8080/trading/orders
# → http://127.0.0.1:8080/trading/market
# (-live-market is an alias)
# P11 local trading (planned): hhld-place -env local; hhld -trade-local

go run ./cmd/hhld-sim -ndjson data/samples/btcusd_books.ndjson
# open http://127.0.0.1:8080/sim

go run ./cmd/hhld-sim -ndjson data/crawl/trumpusd-live-60m.ndjson -config configs/craw-trumpusd.json
# same URL — TRUMPUSD taker-taker replay (maker-taker is investigation; see above)
```

`-demo` seeds one paper arb (`trace_id=demo-arb-1`). `-demo-market` drives fake dual books; `-paper-live` (alias `-live-market`) uses public HL/GRVT market data so each venue shows real **price** and **size** (plus native instrument / mid), and paper fills land on `/trading/pnl` and `/trading/orders`. Config on the market page is **read-only** (edit `configs/default.json`). `/sim` tables are **editable in-memory** (Apply re-runs; does not write the file). Wire your live `admin.Auditor` into `admin.Handler` for a real session.

## What paper arb is not

- Not live capital or automatic real exchange order placement (P8 adapters are **read-only**; orders return `ErrReadOnly`). Local + one-leg venue writes are [P11](roadmap/p11.md)  
- Not a coded **maker-taker** `Strategy` yet (documented above; paper path is taker-taker `cross-venue-arb`)  
- Not prediction-market trading or prediction↔crypto hedge (later)  
- Not full historical warehouse coverage (P7 is minimal SQLite + JSON/NDJSON crawl; Parquet/multi-cloud later)



## Minimal mental example

1. Fake HL book: ask `100.1`; fake GRVT book: bid `100.8`; `min_gap = 0.3` USD → `gap = 0.7 ≥ 0.3` → Decision (buy HL / sell GRVT, size = `max_size`).
2. Risk: `net = gross − fee − latency > 0` after per-venue bps fees, `partial_fill_factor`, and `latency_penalty`; books younger than `max_book_age` → OK.
3. Paper place both legs → two `accepted` acks.
4. Admin: two orders under one `TraceID`; PnL realized ≈ `(sell − buy) × size − modeled fees` (USD).

That loop is the V1 paper-trading path HHLD is built to prove before live gates open.