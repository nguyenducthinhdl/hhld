# Architecture

How HHLD is wired end-to-end: venue connections, in-memory order books (snapshot + delta), and the automatic path from book update → Decision → Risk → place → audit.

Related: [mission.md](mission.md), [trading.md](trading.md), [concurrency.md](concurrency.md), [networking.md](networking.md), [tech-stack.md](tech-stack.md), [roadmap/p85.md](roadmap/p85.md).

## Big picture

HHLD is a **single-process** Go trading system. Market data enters from venues (or fakes), lands in an in-process event bus, updates a shared **BookStore**, and a **Runner** re-evaluates strategy whenever either venue’s book changes. If Risk accepts the edge, orders are placed through the same `Exchange` interface used for paper today and live later.

```text
┌──────────────────────────────────────────────────────────────────────────┐
│                         HHLD process (one instance)                      │
│                                                                          │
│  ┌─────────────┐   BookEvent    ┌─────────┐   Apply   ┌────────────┐   │
│  │ Adapters /  │ ─────────────► │ market  │ ────────► │ BookStore  │   │
│  │ fake feeds  │                │  .Bus   │           │ (A+B books)│   │
│  └─────────────┘                └────┬────┘           └──────┬─────┘   │
│         ▲                            │                       │         │
│         │ REST/WS                    │ dispatch              │ Get     │
│         │                            ▼                       ▼         │
│  ┌──────┴──────┐              ┌────────────────────────────────────┐   │
│  │ Hyperliquid │              │ Runner                             │   │
│  │ GRVT        │              │  both venues ready? → OnBooks      │   │
│  └─────────────┘              │  → TryAcquire → Evaluate → Place   │   │
│                               │  → RecordPaperDecision             │   │
│                               └───────────┬────────────────────────┘   │
│                                           │                            │
│                    ┌──────────────────────┼──────────────────┐         │
│                    ▼                      ▼                  ▼         │
│              strategy               risk.Gate            admin + pnl   │
│           CrossVenueArb            (miss-more)           (audit)       │
└──────────────────────────────────────────────────────────────────────────┘
```

Offline path (not on the hot loop): `crawl` → `warehouse` → `sim.Replay` uses the same Strategy/Risk/Place/Admin stack with historical books.

## Package map

| Package | Responsibility |
|---------|----------------|
| `config` | Symbols, venue A/B, `min_size` / `max_size`, `min_gap`, fees, timeouts, symbol map |
| `exchange` | Venue-agnostic `Book` / `Tick` / `PlaceOrder`; `fake`, `hyperliquid`, `grvt` |
| `market` | `BookEvent`, `BookStore`, bounded `Bus`, `Runner`, bridges |
| `strategy` | `OnBooks` → `Decision`; `PlaceDecision` places legs |
| `risk` | `TryAcquire` + `Evaluate` (fees, stale, overload) |
| `admin` / `pnl` | Order audit + realized PnL + market dashboard |
| `view` | HTML/CSS/JS for `/trading/*` and `/sim` (static `/view/`) |
| `viz` | Snapshot builders (gap, signal, config) for `/trading/market` |
| `sim` | Backtest replay + crawl series for `/sim` |
| `warehouse` / `crawl` | Offline data + live NDJSON feed |

Strategy never imports vendor SDKs. Adapters normalize vendor payloads into HHLD types.

## Connections (ingress)

Each live venue is a **read-first** adapter behind `exchange.Exchange`.

```text
Config.symbol_map[]
  BTCUSD.venues.<id>.symbol_name → HL coin "BTC", GRVT "BTC_USDT_Perp"
  BTCUSD.trading.max_size / order_interval → Strategy size + Risk cap
  BTCUSD.venues.<id>.budget → notional cap (key venue/symbol)
        │
        ▼
┌───────────────────┐              ┌───────────────────┐
│ Hyperliquid       │              │ GRVT              │
│ REST POST /info   │  snapshots   │ REST POST /book   │
│ WS  l2Book        │ ───────────► │ WS  v1.book.s     │  snapshots
│                   │              │ WS  v1.book.d     │  deltas
└─────────┬─────────┘              └─────────┬─────────┘
          │                                  │
          │  market.BridgeBooks              │  BridgeBooks / BridgeGRVTDeltas
          │  → SnapshotEvent                 │  → SnapshotEvent / DeltaEvent
          └──────────────────┬───────────────┘
                             ▼
                        market.Bus
```

| Venue | Book stream | Event published to Bus |
|-------|-------------|------------------------|
| Hyperliquid | WS `l2Book` (full book each push) | **Snapshot** only |
| GRVT | WS `v1.book.s` | **Snapshot** |
| GRVT | WS `v1.book.d` | First message after (re)connect = **Snapshot**; later = **Delta** |
| Fake | `SetBook` / `PushDelta` | Snapshot / Delta via bridges |

**Reconnect doctrine:** on WS drop, resubscribe and treat the next book as a fresh snapshot. `BridgeGRVTDeltas` calls `BookStore.Clear(venue, symbol)` at session start so stale levels are not merged across reconnects.

REST `SnapshotBook` is available for cold start / diagnostics; the hot path is WS → Bus.

Orders: adapters currently return `ErrReadOnly` for live HL/GRVT. Paper uses `fake` (or later paper fills on live books). Same `PlaceOrder` call site either way.

Per-venue wire formats and parse tables: [exchange/hyperliquid.md](exchange/hyperliquid.md), [exchange/grvt.md](exchange/grvt.md).

## Order book model

### Normalized book

Every venue’s book is stored as:

```text
Book {
  Venue, Symbol, Kind,
  Bids[] { Price, Size },   // strings (decimal), best first as provided
  Asks[] { Price, Size },
  Time
}
```

Key in the store: `(Venue, Symbol)`.

### BookStore apply rules

| Event | Effect |
|-------|--------|
| **Snapshot** | Replace the entire book for that key; marks the key as ready |
| **Delta** | Merge by **price**: update size, or **delete** the level if size is `"0"` (or empty) |
| Delta before any snapshot | **Rejected** (no invented book) |
| Sequence present and goes backward | **Rejected** |
| Apply error | Runner **does not** evaluate (miss) |

```text
Snapshot:  bids={100:1, 99:2}  asks={101:1}
Delta:     bids={100:0}         → remove 100
Result:    bids={99:2}          asks={101:1}
```

Strategy always sees **full books** via `OnBooks([]Book)`. Deltas never reach Strategy; they only mutate the BookStore.

### Bus

In-process only (**no** RabbitMQ on the hot path):

- Bounded queue; on overflow **drop** newest (miss-more)
- Handlers (Runner) run serially from the dispatch goroutine
- Event flood must still go through `risk.Gate.TryAcquire` — never bypass locks

## Automatic order path

“Automatic” means: **no human in the loop** once the process is running with config + feeds. Every book update can trigger a trade attempt.

```text
1. Venue pushes book (snapshot or delta)
2. Bridge publishes BookEvent on market.Bus
3. Runner.OnEvent:
     BookStore.Apply(event)
     if apply fails → stop
     if symbol not watched → stop
4. Runner.evaluate(symbol):
     load bookA, bookB from BookStore
     if either missing → miss (PeerMiss)
5. Strategy.OnBooks([bookA, bookB])
     CrossVenueArb: if best ask on one venue is enough below
     best bid on the other (≥ min_gap) → Decision{TraceID, Legs[buy, sell]}
6. For each Decision:
     Gate.TryAcquire(Decision)     // per arb key + global in-flight
     if lock_busy / overloaded → miss
     Gate.Evaluate(Decision, books) // fees, latency, stale, unhealthy
     if reject → miss
     PlaceDecision → PlaceOrder each leg (concurrent per Decision)
     RecordPaperDecision → admin orders + pnl fills
```

### Evaluate trigger

- **Every** successful BookStore apply for either venue (no coalesce window)
- Requires **both** configured venues to already have a book for that symbol
- First update that only has venue A → miss; once B arrives → evaluate; each later A or B update → evaluate again

### What makes a Decision placeable

| Gate | Miss reason (examples) |
|------|------------------------|
| Positive edge after fees / latency / partial-fill model | negative edge |
| Books younger than `max_book_age` | `stale_book` |
| Venues healthy | unhealthy venue |
| One pipeline per arb key; global in-flight cap | `lock_busy`, `overloaded` |
| Leg size ≤ `trading.max_size` | `max_volume_exceeded` |
| Min `order_interval` since last accept for symbol | `rate_limited` |
| Notional ≤ `venues.<venue>.budget` | `budget_exceeded:...` |

Effective order size is `trading.max_size` (at least `min_size`) before Risk sees the Decision.

Doctrine: **miss more** — skip the trade rather than take a bad or racing fill.

### Paper vs live capital

| Mode | Market data | PlaceOrder |
|------|-------------|------------|
| Sim / unit tests | Fake or warehouse replay | Fake acks |
| P8.5 / P9 paper on live | HL + GRVT WS → BookStore | Paper/fake fills (no real capital) |
| Later live | Same books | Real venue orders behind kill switches |

The **control flow is identical**; only the `Exchange` implementation behind `PlaceOrder` changes.

## Dual-path summary

```text
Live / fake WS ──► Bus ──► BookStore ──► Runner ──► Strategy ──► Risk ──► Place ──► Audit
                                                                    ▲
Historical NDJSON ──► warehouse ──► sim.Replay ─────────────────────┘
```

Same Decision shape (`TraceID`, legs), same Risk, same audit reconstruction.

## Explicit non-goals (this architecture)

- External message brokers on the book path
- Strategy consuming raw deltas
- Coalescing / debounce windows before evaluate
- Multi-region / multi-process book consensus
- Live capital until a later gated phase

## Where to read next

| Topic | Spec / code |
|-------|-------------|
| Paper arb economics | [trading.md](trading.md) |
| Locks and bus bounds | [concurrency.md](concurrency.md) |
| Timeouts / reconciling unknown acks | [networking.md](networking.md) |
| Phase status | [roadmap/README.md](roadmap/README.md) |
| Implementation | [`src/market`](../src/market/), [`src/strategy`](../src/strategy/), [`src/risk`](../src/risk/), [`src/exchange`](../src/exchange/) |
