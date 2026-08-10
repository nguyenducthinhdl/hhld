# Tech stack

Constitution for implementation choices. Optimize for a solo operator: speed of build, maintainability, and one cheap instance.

## Language and layout

- **Primary language: Go** (modules, clear interfaces, enough throughput for paper/live order paths).
- Package boundaries stay interface-first so a latency-critical path could move to Rust later without rewriting strategy logic.
- Suggested packages under `src/`: `config`, `exchange`, `strategy`, `risk`, `pnl`, `sim`, `warehouse`, `admin` (names may evolve; boundaries must not).
- **`config`**: parameterizes symbols, venues, trading conditions (size, min gap, timeouts) for other modules — no hard-coded BTCUSD in strategy logic.

## Exchange layer

- All venue access goes through an abstract **`Exchange`** interface: quotes / order book / ticks, and paper order APIs.
- **Venue kinds**: crypto order-book venues **and** prediction markets. An `Exchange` is any tradable venue—e.g. [Hyperliquid](https://hyperliquid.xyz/), [GRVT](https://grvt.io/), or [Polymarket](https://polymarket.com/)—not only coin CEXs/DEXs.
- **Instrument model**: adapters map venue instruments into HHLD symbols (perp coins, spot pairs, **or** prediction outcomes / condition tokens such as up/down). Strategy sees a unified book/tick; it does not know venue quirks.
- **Multi-venue legs**: Strategy may emit a **hedge set**—at least one prediction-market order (up or down) plus one or more crypto-venue orders (HL/GRVT)—as a single decision. Risk evaluates the set against fees/latency on **both** legs; PnL and admin audit store a shared hedge/trace id linking the legs.
- **First adapters**: Hyperliquid and GRVT (crypto arb path).
- **Later adapter**: Polymarket (prediction-market venue behind the same `Exchange`), then hedge strategies that combine Polymarket + HL/GRVT.
- Strategy, risk, and PnL code **must not** import vendor SDKs directly—only adapters implement venue details.
- Adding another exchange means a new adapter behind the same interface.

## Data warehouse

- Small local store for v1 cost: **SQLite** and/or **Parquet on disk**.
- Crawl jobs normalize ticks (and later OHLCV if needed) into the warehouse for backtest replay.
- No managed data warehouse or cloud analytics in early phases.

## Backtesting

- **Runtime language: Go** — same `Strategy` / `Risk` / `Exchange` path as paper trading (no dual implementation).
- Simulation: tick/book replay → order decisions → PnL series.
- **Winning analysis** (same `sim.Analyzer` abstraction): overall **winning rate**, and **winning distribution** over dimensions `(symbol, gap, volume1, volume2, exchange1, exchange2, time)` for Risk calibration.
- Visualization later: CSV export first; minimal plots after PnL is trustworthy.

## Risk Management

- **Hard gate** (`Risk.Evaluate` / `Gate`): miss-more fees, latency, staleness, concurrency.
- **Estimation** (`Estimator`): Value at Risk and/or predicted winning rate — detective formulas / empirical stats from Go sim first; richer models come from the research side project and plug in via this interface (`Method`: formula | historical | ml).
- **Manager** composes gate + estimator so paper can stay gate-only while later paths add WinRate/VaR thresholds.

## Research (side project)

- **Not in this repo’s runtime.** ML training, notebooks, and heavy statistical research live in a **separate side project** (typically Python).
- Contract with HHLD: consume exported sim/warehouse artifacts (e.g. Parquet/CSV of fills, PnL, winning distributions); publish calibrated priors or model artifacts that a Go `Estimator` implementation can load.
- Keeps solo trading system maintainable in one language while allowing research tooling to evolve independently.

## Ops and observability

- Deploy on **one medium VM or container**.
- Structured logs with **order / trace IDs** so mismatched or lost orders are diagnosable.
- Metrics dashboards only when paper live feeds prove the need.
- Networking / book-stale / unknown-ack doctrine: [networking.md](networking.md).
- Concurrent orders / per-hedge Risk ordering: [concurrency.md](concurrency.md).
- Tests and coverage bar: [quality-assurance.md](quality-assurance.md).

## Explicitly out of scope (this repo)

- Kubernetes, multi-region, auto-scaling fleets
- Research/ML training stack and notebooks (side project only)
- Real capital wiring until roadmap opens that gate
