# Hyperliquid

How HHLD interacts with Hyperliquid and how vendor payloads become HHLD `Book` / `Tick`.

**Doc version:** 2026-08-10 (manual WS commands)  
**Docs:** [API overview](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/) · [WS subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions) · [Exchange (orders)](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint)  
**Code:** [`src/exchange/hyperliquid`](../../src/exchange/hyperliquid/)  
**VenueID:** `hyperliquid`

## Status

| Path | Status |
|------|--------|
| Market data (REST + WS books/trades) | Implemented (read-only adapter) |
| Place / Cancel | Returns `exchange.ErrReadOnly` (paper uses `fake`) |
| Auth for books | Not required (public) |
| Signed live orders | Out of scope until live-capital phase |

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

### Orders (not implemented live)

Live HL place needs signed `POST /exchange` with asset index, TIF (`Gtc`/`Ioc`/`Alo`), optional `cloid`, etc. See [exchange endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint) and [p9.md](../roadmap/p9.md) contract gaps. Adapter `PlaceOrder` / `CancelOrder` → `ErrReadOnly`.

## Bridge into HHLD event core

```text
SubscribeBook → BookHandler → market.BridgeBooks → SnapshotEvent → BookStore
```

No delta path for HL.

## Related

- Config example: [`configs/default.json`](../../configs/default.json)
- Sufficiency vs HHLD `Exchange`: [p9.md](../roadmap/p9.md)
- Event bus: [architect.md](../architect.md)
