# HHLD finance

**Constitution:** [spec/mission.md](spec/mission.md) · [spec/architect.md](spec/architect.md) · [spec/exchange/](spec/exchange/) · [spec/tech-stack.md](spec/tech-stack.md) · [spec/roadmap.md](spec/roadmap.md) · [spec/trading.md](spec/trading.md) · [spec/ledger.md](spec/ledger.md) · [spec/networking.md](spec/networking.md) · [spec/concurrency.md](spec/concurrency.md) · [spec/quality-assurance.md](spec/quality-assurance.md) · [implementation](spec/roadmap/README.md)

## Layout (P0)

Go module `github.com/nguyenducthinhdl/hhld`. Packages under [`src/`](src/) (P0 skeleton, P1 interfaces):

| Package | Role |
|---------|------|
| [`src/config`](src/config/) | Symbols, venues, trading conditions (parameterized) |
| [`src/exchange`](src/exchange/) | Venue-agnostic Exchange (+ `fake`, `hyperliquid`, `grvt` read adapters) |
| [`src/strategy`](src/strategy/) | Trading decisions (arb, hedge, …) |
| [`src/risk`](src/risk/) | Risk Management: miss-more gates + VaR/win-rate estimators |
| [`src/pnl`](src/pnl/) | Profit and loss |
| [`src/sim`](src/sim/) | Backtest sim + crawl replay series (`hhld-sim`) |
| [`src/warehouse`](src/warehouse/) | Market data store (SQLite) |
| [`src/crawl`](src/crawl/) | Crawl stubs → warehouse; live multi-exchange NDJSON feed (`hhld-feed`) |
| [`src/market`](src/market/) | Event-driven BookStore, Bus, Runner (P8.5) |
| [`src/monitor`](src/monitor/) | Order forensics, unknown-ack reconcile, unpaired halt (P10) |
| [`src/paperlive`](src/paperlive/) | Paper arb on live HL+GRVT books (P9) |
| [`src/ledger`](src/ledger/) | Durable orders/fills (`hhld-ledger.db`) |
| [`src/secrets`](src/secrets/) | Env pin, refuse live keys, kill switch |
| [`src/place`](src/place/) | One-leg place (`hhld-place`: local fake + HL testnet) |
| [`src/viz`](src/viz/) | Market dashboard snapshots (gap, signal, config) |
| [`src/admin`](src/admin/) | Order / PnL audit (+ `/trading/pnl`, `/trading/orders`, `/trading/market`, `/trading/forensics`, `/health`, `/sim`) |
| [`view`](view/) | Dashboard HTML/CSS/JS served at `/view/` |

Example config: [`configs/default.json`](configs/default.json).

```bash
go build ./...
go test ./...

# PnL / orders dashboard (seeded demo data)
go run ./cmd/hhld -demo
# → http://127.0.0.1:8080/trading/pnl

# Market visualization (fake books)
go run ./cmd/hhld -demo-market
# → http://127.0.0.1:8080/trading/market

# Live Hyperliquid + GRVT prices/sizes (paper place only; refuses live keys)
go run ./cmd/hhld -paper-live
# → http://127.0.0.1:8080/trading/pnl
# → http://127.0.0.1:8080/trading/orders
# → http://127.0.0.1:8080/trading/market
# → http://127.0.0.1:8080/health
# → http://127.0.0.1:8080/trading/forensics?trace_id=
# (-live-market is an alias)

# P11 local + HL testnet writes
go run ./cmd/hhld-place -config configs/default.json -venue hyperliquid \
  -env local -kind perp -symbol BTCUSD -side buy -size 0.00003
# HL testnet one-leg (requires HHLD_LIVE_ORDERS=1, HL_ACCOUNT_ADDRESS, HL_TESTNET_PRIVATE_KEY;
# unset HHLD_VENUE_ENV or set it to testnet — do not leave it as local)
go run ./cmd/hhld-place -config configs/default-staging.json -venue hyperliquid \
  -env testnet -kind perp -symbol BTCUSD -side buy -size 0.00003
go run ./cmd/hhld -trade-local -config configs/default.json
# optional: -trade-local -live-books   # public books, still fake place
# Knobs: configs/default.json (local), default-staging.json, default-production.json
# GRVT place and prod/mainnet place are not in this slice.

# crawl sample books into local SQLite warehouse (P7)
go run ./cmd/hhld-crawl -sample data/samples/btcusd_books.ndjson -db ./hhld.db

# capture live books/ticks from configured exchanges to NDJSON (research)
go run ./cmd/hhld-feed -config configs/crawl.json
go run ./cmd/hhld-feed -config configs/crawl-ethusd.json
go run ./cmd/hhld-feed -config configs/crawl-solusd.json
go run ./cmd/hhld-feed -config configs/crawl-trumpusd.json
# ETHUSD knobs: configs/craw-ethusd.json
# SOLUSD knobs: configs/craw-solusd.json
# TRUMPUSD knobs: configs/craw-trumpusd.json
# replay: go run ./cmd/hhld-sim -ndjson data/crawl/ethusd-live.ndjson -config configs/craw-ethusd.json
#         go run ./cmd/hhld-sim -ndjson data/crawl/trumpusd-live-60m.ndjson -config configs/craw-trumpusd.json
# maker-taker vs taker-taker: spec/trading.md

# replay crawled NDJSON (any two venues) — gap / PnL / signals
go run ./cmd/hhld-sim -ndjson data/crawl/btcusd-live.ndjson
# fallback: -ndjson data/samples/btcusd_books.ndjson
# → http://127.0.0.1:8080/sim
```

See [spec/trading.md](spec/trading.md#taker-taker-vs-maker-taker) (taker-taker vs maker-taker, crawl tapes).

## Input from the owner
- This is the project aims to do a trading system to gain money efficiency and safety 
- There are serveral methods for trading, but the owner would like to trade firstly with some symbol like btcusd.
- The trading strategies:
+ Arbitrage: 1st method and we aim to do it for trading a symbol between two coin exchange once there are GAP from it. We need to care about the risk like latency, networking.
+ Prediction strategy: 2nd method, will be test after 1st method
- The functional feature of System:
 - There are some component of systems:
    - Trading Core: Core of trading where decide which volume, price of buy order and sell order. There are server submodules like: Connection Module, Risk Management Module, Testing Module, Strategy-Trading Module, Profit and Loss (PnL) Module. Every module must be written by interface for lose coupling and easy for testing and backtesting
    - BackTesting: Incharge to build a backtesting method for testing the Trading Core to verify the performance of the chosen trading strategy by simulating and get the profit and lost. Submodules: Simulation Module (Input is the trading data by tick and the strategy trading, output is PnL), Visualization Module (Visualize PnL time by time which helps trader to understand the Alpha)
    - Dataware House: A small dataware house to convert any crawling data to the Dataware House for Backtesting
    - Administrator: Contains auditable value: PnL, Storing Trading Orders
- The Non-functional:
    - High Performance: System should be fast and easy creating orders
    - Maintainable: Sourcode is readable
    - Monitoring and Tracing: Easy to trace the mismatching / losing order
    - Cost saving: In some first version, the infra deployment cost is small ( 1 medium instance )
    - Ready for Production of a 1 man company