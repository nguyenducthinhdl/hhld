# Mission

## Purpose

HHLD is a one-person trading system built for **capital efficiency and safety**. Earn by capturing cross-exchange price gaps first; prediction strategies come later.

## V1 success

Detect arbitrage gaps across two venues, decide size and price in Trading Core, and **paper-trade** those decisions. Persist orders and PnL for audit. No live capital at risk until later phases explicitly allow it ([P11](roadmap/p11.md) is local trading first, then gated venue writes).

How paper arb works in detail: [trading.md](trading.md). Durable fills and realized PnL: [ledger.md](ledger.md). Overall wiring (connections, books/deltas, automatic place): [architect.md](architect.md).

Symbols are **multi-symbol from day one** (BTCUSD is the first concrete pair; others via config, not hard-coded into strategy logic).

## Risk doctrine

Prefer **never lose on fees or latency**. Model worst-case costs (fees, latency, partial fills, networking). Only take trades where modeled edge survives those costs. Miss opportunities rather than take bad fills.

**Risk Management** is abstracted in `risk`: hard **gates** (miss-more Evaluate) plus pluggable **estimation** (Value at Risk, winning-rate prediction). Go sim provides empirical calibration; **ML/research stays in a separate side project** and plugs in through `Estimator` (see [tech-stack.md](tech-stack.md)).

Networking and unknown order acks: see [networking.md](networking.md) (fail closed on bad books; reconcile on uncertain orders; stop-and-recover unpaired legs).

Concurrent orders and per-hedge Risk ordering: see [concurrency.md](concurrency.md) (serialize Risk+execution per hedge/arb key; global in-flight cap; miss under overload).

## Venues

An **exchange** is any tradable venue behind the `Exchange` interface: crypto books (e.g. Hyperliquid, GRVT) **or** prediction markets (e.g. Polymarket). V1 arb uses crypto venues first; prediction-market adapters come after the crypto paper path is proven.

## Strategies (order)

1. **Arbitrage** — trade a symbol between two **same-kind** venues (e.g. HL ↔ GRVT) when a gap appears; latency and networking risk are first-class. Execution styles: **taker-taker** (implemented paper path) and **maker-taker** (rest then hedge; investigation in [trading.md](trading.md)). Extra USD perps (ETHUSD, SOLUSD, TRUMPUSD) use their own crawl configs, not `default.json`.
2. **Cross-venue hedge** — in some cases, hedge a **prediction-market leg** (up/down / Yes/No) against a **crypto venue leg** (Hyperliquid or GRVT perp/spot). Both legs are paper/live orders under one strategy decision; Risk and PnL must treat them as a linked hedge, not two independent trades.
3. **Prediction** — forecasting / event-driven strategies on prediction-market venues; out of scope until arb (and preferably hedge paper path) is proven and audited.

## System map

All modules are interface-first for loose coupling, testing, and backtest/live swap.

| Component | Role | Submodules |
|-----------|------|------------|
| **Config** | Parameterize symbols, venues, trading conditions | Load/validate JSON (and later env); feeds Strategy/Risk/Execution |
| **Trading Core** | Decide volume and price of buy/sell orders | Connection, Risk Management (gates + VaR/win-rate estimators), Testing, Strategy-Trading, PnL |
| **Backtesting** | Simulate strategy performance on historical ticks | Simulation (replay → PnL; winning rate / distribution), Visualization (PnL over time / alpha) |
| **Data Warehouse** | Convert crawled market data into store for backtest | Crawl → normalize → persist |
| **Administrator** | Auditable record of trading | PnL, stored trading orders |

## Non-functional bar

- **High performance** — fast path to create orders when edge is valid.
- **Maintainable** — readable source; one-person ownership.
- **Monitoring and tracing** — easy to trace mismatched or lost orders.
- **Cost saving** — early infra is one medium instance.
- **Production-ready for a 1-man company** — operable without a team.

## Non-goals (early phases)

- Live capital / automatic real order placement (P11 is local trading + gated one-leg `hhld-place` smoke, not live arb)
- Prediction strategy, Polymarket adapter, and prediction↔crypto hedge (interfaces must allow multi-leg / multi-venue hedges; implement later)
- Heavy multi-instance or multi-region infra
