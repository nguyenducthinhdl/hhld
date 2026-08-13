# Exchange adapters

Per-venue notes: how HHLD talks to each exchange and how vendor payloads map to HHLD types (`Book`, `Tick`, …).

**Doc version:** 2026-08-10 (manual WS commands)

| Venue | Doc | Adapter |
|-------|-----|---------|
| Hyperliquid | [hyperliquid.md](hyperliquid.md) | [`src/exchange/hyperliquid`](../../src/exchange/hyperliquid/) |
| GRVT | [grvt.md](grvt.md) | [`src/exchange/grvt`](../../src/exchange/grvt/) |
| Fake (tests) | — | [`src/exchange/fake`](../../src/exchange/fake/) |

Each venue doc includes **Manual WebSocket connect** (`wscat` / `websocat`) for live smoke without running HHLD.

Shared contract: [`src/exchange/exchange.go`](../../src/exchange/exchange.go). Architecture: [architect.md](../architect.md). Roadmap phase notes: [p8.md](../roadmap/p8.md), [p9.md](../roadmap/p9.md).

When changing a venue doc or adapter parse map, bump that file’s **Doc version** date (and this index if the table changes).
