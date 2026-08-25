# Hyperliquid

How HHLD interacts with Hyperliquid and how vendor payloads become HHLD `Book` / `Tick`.

**Doc version:** 2026-08-25 (P11 HL testnet place)  
**Docs:** [API overview](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/) · [WS subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions) · [Exchange (orders)](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint)  
**Code:** [`src/exchange/hyperliquid`](../../src/exchange/hyperliquid/)  
**VenueID:** `hyperliquid`

## Status

| Path | Status |
|------|--------|
| Market data (REST + WS books/trades) | Implemented (read-only `New`) |
| Place / Cancel / GetOrder | Implemented on `NewLive` + Auth (testnet/staging via `hhld-place`) |
| Auth for books | Not required (public) |
| Default `New(...)` orders | Still `exchange.ErrReadOnly` (paper-live safe) |
| Mainnet / prod place | Not in this slice |

## Endpoints

| Env | REST | WS |
|-----|------|-----|
| Mainnet (default) | `https://api.hyperliquid.xyz` | `wss://api.hyperliquid.xyz/ws` |
| Testnet | `https://api.hyperliquid-testnet.xyz` | `wss://api.hyperliquid-testnet.xyz/ws` |

Defaults: `exchange.DefaultHyperliquidMainnet()`.

## Manual WebSocket connect

Requires [`wscat`](https://github.com/websockets/wscat) (`npm i -g wscat`) or any interactive WS client.

### Mainnet L2 book (`BTC`)

```bash
wscat -c wss://api.hyperliquid.xyz/ws
```

After connect, paste:

```json
{"method":"subscribe","subscription":{"type":"l2Book","coin":"BTC"}}
```

Expect a `subscriptionResponse`, then `channel:"l2Book"` messages with full `levels` snapshots.

### Mainnet trades

Same dial; subscribe:

```json
{"method":"subscribe","subscription":{"type":"trades","coin":"BTC"}}
```

### Testnet

```bash
wscat -c wss://api.hyperliquid-testnet.xyz/ws
```

Same subscribe payloads (coin must exist on testnet).

### One-shot with `websocat` (optional)

```bash
# brew install websocat   # or cargo install websocat
printf '%s\n' '{"method":"subscribe","subscription":{"type":"l2Book","coin":"BTC"}}' \
  | websocat -n wss://api.hyperliquid.xyz/ws
```

## Symbol map

HHLD symbols never appear on the wire. Config `symbol_map[].venues.hyperliquid.symbol_name` is the HL **coin** (perp name from `meta`).

| HHLD `Symbol` | HL coin (example) |
|---------------|-------------------|
| `BTCUSD` | `BTC` |
| `ETHUSD` | `ETH` |
| `SOLUSD` | `SOL` |
| `TRUMPUSD` | `TRUMP` |

**ETHBTC is not listed.** Checked 2026-08-17: perp `meta` universe has `ETH` but no `ETHBTC` / `ETH/BTC`; spot `spotMeta` has no ETH–BTC pair. Do not add an ETHBTC `symbol_map` row or crawl until both venues list the same-kind instrument. Do not arb HL `ETH` vs a GRVT ETH–BTC product.

Adapter: `Config.Symbols[symbol] → coin`. Missing map → error.

## Interact — market data

### REST snapshot — `SnapshotBook`

```http
POST {REST}/info
Content-Type: application/json

{"type":"l2Book","coin":"BTC"}
```

Response body is an L2 book object (same shape as WS `data`). Parsed by `parseL2Book`.

### WS book — `SubscribeBook`

1. Dial `{WS}` (no auth headers).
2. Send:

```json
{
  "method": "subscribe",
  "subscription": { "type": "l2Book", "coin": "BTC" }
}
```

3. Server acks with `channel: "subscriptionResponse"` (ignored).
4. Updates arrive as:

```json
{
  "channel": "l2Book",
  "data": { /* WsBook */ }
}
```

HL documents `l2Book` as a **snapshot feed** (full book each push). HHLD publishes each as `market.SnapshotEvent` via `market.BridgeBooks` — not deltas.

On WS drop: reconnect after `ReconnectWait`, resubscribe; next book is a fresh snapshot (P8 reconnect doctrine).

### WS trades — `SubscribeTicks`

Subscribe `{ "type": "trades", "coin": "BTC" }`. Channel `trades`; `data` is a JSON array of trades.

## Parse — vendor → HHLD

### Book (`WsBook` → `exchange.Book`)

Vendor (`data` / REST body):

```json
{
  "coin": "BTC",
  "time": 1700000000123,
  "levels": [
    [ { "px": "100.0", "sz": "1.5", "n": 2 } ],
    [ { "px": "100.1", "sz": "0.8", "n": 1 } ]
  ]
}
```

| Vendor | HHLD |
|--------|------|
| (fixed) | `Venue = "hyperliquid"` |
| (request symbol) | `Symbol` = HHLD symbol (e.g. `BTCUSD`), **not** `coin` |
| (adapter `Kind`) | `Kind` (usually `perp`) |
| `levels[0][].px` | `Bids[].Price` |
| `levels[0][].sz` | `Bids[].Size` |
| `levels[1][].px` | `Asks[].Price` |
| `levels[1][].sz` | `Asks[].Size` |
| `time` (unix **ms**) | `Time = time.UnixMilli(time).UTC()` |
| `n` (order count) | **Dropped** |

`levels` must have length ≥ 2; otherwise parse error.

### Tick (`WsTrade` → `exchange.Tick`)

Vendor `data` array element:

```json
{ "coin": "BTC", "side": "B", "px": "100.05", "sz": "0.1", "time": 1700000000456 }
```

| Vendor | HHLD |
|--------|------|
| `px` / `sz` | `Price` / `Size` |
| `side` `B` / `buy` | `SideBuy` |
| `side` `A` / `S` / `sell` | `SideSell` |
| `time` (ms) | `Time` UTC |
| | `Venue`, `Symbol`, `Kind` as for books |

### Orders (`NewLive` — testnet/staging via `hhld-place`)

Signed `POST {REST}/exchange` with L1 msgpack action hash + EIP-712 phantom Agent (`source` `"b"` testnet / `"a"` mainnet, domain chainId **1337**). `meta` universe index for mapped perp coin; IOC + `cloid`. `GetOrder` → `POST /info` `orderStatus`. Default `New` stays `ErrReadOnly`. Gates: `HHLD_LIVE_ORDERS=1`, `HL_TESTNET_PRIVATE_KEY`, `HL_ACCOUNT_ADDRESS`. Spot / mainnet place: leftover. Full contract: [p11.md](../roadmap/p11.md).

## Trading fees (Risk schedule)

V1 paper arb **crosses** both books (buy ask / sell bid) → both legs are **takers**. [HL fee tiers](https://hyperliquid.gitbook.io/hyperliquid-docs/trading/fees): Tier 0 perp **taker** is **0.045% = 4.5 bps**. Perp **maker** is **0.015% = 1.5 bps** (used only in the maker-taker investigation in [trading.md](../trading.md); config `fees.buy|sell` today still stores taker 4.5 on both sides).

```text
taker-taker:  buy  HL @ best ask  → lift → taker 4.5 bps
              sell HL @ best bid  → hit  → taker 4.5 bps
maker-taker:  rest HL (post-only) → maker 1.5 bps; hedge other venue as taker
```

## Bridge into HHLD event core

```text
SubscribeBook → BookHandler → market.BridgeBooks → SnapshotEvent → BookStore
```

No delta path for HL.

## Related

- Config example: [`configs/default.json`](../../configs/default.json) (BTCUSD); ETHUSD: [`configs/craw-ethusd.json`](../../configs/craw-ethusd.json); SOLUSD: [`configs/craw-solusd.json`](../../configs/craw-solusd.json); TRUMPUSD: [`configs/craw-trumpusd.json`](../../configs/craw-trumpusd.json)
- Maker-taker investigation: [trading.md](../trading.md#taker-taker-vs-maker-taker)
- Event bus: [architect.md](../architect.md)
