# GRVT

How HHLD interacts with GRVT and how vendor payloads become HHLD `Book` / `Tick` / book deltas.

**Doc version:** 2026-08-10 (manual WS commands)  
**Docs:** [API docs](https://api-docs.grvt.io/)  
**Code:** [`src/exchange/grvt`](../../src/exchange/grvt/)  
**VenueID:** `grvt`

## Status

| Path | Status |
|------|--------|
| Market data REST + WS (`v1.book.s`, `v1.book.d`, `v1.trade`) | Implemented (full field names) |
| Place / Cancel | Returns `exchange.ErrReadOnly` |
| Auth for market-data | Not required (public) |
| Trading WS / cookie auth | Out of scope until live capital (`trades.*` hosts) |

## Endpoints

HHLD uses **full** (long field names), not lite.

| Env | Market REST | Market WS |
|-----|-------------|-----------|
| Mainnet (default) | `https://market-data.grvt.io` | `wss://market-data.grvt.io/ws/full` |

Defaults: `exchange.DefaultGRVTMainnet()`.

Trading hosts (`trades.*`) and auth (`gravity` cookie + `X-Grvt-Account-Id`) are **not** used for P8/P9 reads.

## Manual WebSocket connect

Requires [`wscat`](https://github.com/websockets/wscat) (`npm i -g wscat`). Market-data WS needs **no** cookie.

### Book snapshots (`v1.book.s`)

```bash
wscat -c wss://market-data.grvt.io/ws/full
```

**Status (2026-08-11):** The GRVT WS gateway at `wss://market-data.grvt.io/ws/full` now requires `"jsonrpc":"2.0"` in every message, but also rejects the flat format with code 1003 and `v1.book.s`/`v1.book.d` via the JSON-RPC params form with code 1100 ("Stream not supported on this gateway"). HHLD falls back to REST polling (`BridgeGRVTPoll`) until the API stabilises.

After connect, paste (may not work — see status above):

```json
{"jsonrpc":"2.0","id":1,"stream":"v1.book.s","feed":["BTC_USDT_Perp@500-10"],"method":"subscribe","is_full":true}
```

Expect a subscribe ack (`subs` / `unsubs`), then messages with `stream:"v1.book.s"` and a `feed` book body.

### Book deltas (`v1.book.d`)

Same dial; subscribe:

```json
{"jsonrpc":"2.0","id":1,"stream":"v1.book.d","feed":["BTC_USDT_Perp@500"],"method":"subscribe","is_full":true}
```

First feed after connect is typically a full book; later feeds are level deltas (`size:"0"` = delete).

### Trades (`v1.trade`)

```json
{"jsonrpc":"2.0","id":1,"stream":"v1.trade","feed":["BTC_USDT_Perp@50"],"method":"subscribe","is_full":true}
```

### One-shot with `websocat` (optional)

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"stream":"v1.book.s","feed":["BTC_USDT_Perp@500-10"],"method":"subscribe","is_full":true}' \
  | websocat -n wss://market-data.grvt.io/ws/full
```

### Trading WS (not used by HHLD reads)

Private streams need auth — see [GRVT docs](https://api-docs.grvt.io/) (cookie + `X-Grvt-Account-Id`). Example shape only:

```bash
# After API-key login obtained GRVT_COOKIE and GRVT_ACCOUNT_ID:
wscat -c "wss://trades.grvt.io/ws/full" \
  -H "Cookie: $GRVT_COOKIE" \
  -H "X-Grvt-Account-Id: $GRVT_ACCOUNT_ID"
```

## Symbol map

| HHLD `Symbol` | GRVT instrument (example) |
|---------------|---------------------------|
| `BTCUSD` | `BTC_USDT_Perp` |

Config: `symbol_map[].venues.grvt.symbol_name`. Adapter: `Config.Symbols[symbol] → instrument`.

## Adapter knobs

| Config | Default | Use |
|--------|---------|-----|
| `BookDepth` | `10` | REST depth; `v1.book.s` feed depth |
| `BookRateMS` | `500` | WS rate for book.s / book.d (`500` or `1000`) |
| `TradeLimit` | `50` | `v1.trade` feed limit |
| `ReconnectWait` | `1s` | Backoff between WS sessions |

## Interact — market data

### REST snapshot — `SnapshotBook`

```http
POST {REST}/full/v1/book
Content-Type: application/json

{"instrument":"BTC_USDT_Perp","depth":10}
```

Response wraps the book under `result`. Parsed by `parseBookREST`.

### WS snapshot stream — `SubscribeBook` (`v1.book.s`)

Feed selector: `{instrument}@{rate}-{depth}` e.g. `BTC_USDT_Perp@500-10`.

```json
{
  "stream": "v1.book.s",
  "feed": ["BTC_USDT_Perp@500-10"],
  "method": "subscribe",
  "is_full": true
}
```

Ack: `{ "stream":"v1.book.s", "subs":[...], "unsubs":[] }` (no `feed` → ignore).  
Updates:

```json
{
  "stream": "v1.book.s",
  "selector": "BTC_USDT_Perp@500-10",
  "feed": { /* book body */ }
}
```

### WS delta stream — `SubscribeBookDeltas` (`v1.book.d`)

Feed selector: `{instrument}@{rate}` e.g. `BTC_USDT_Perp@500`.

```json
{
  "stream": "v1.book.d",
  "feed": ["BTC_USDT_Perp@500"],
  "method": "subscribe",
  "is_full": true
}
```

HHLD session rules (reconnect doctrine):

1. On each new WS session, call `onReconnect` so callers can `BookStore.Clear(venue, symbol)`.
2. **First** `feed` after connect → treat as **snapshot** (`onSnapshot` / full `Book`).
3. Later feeds → **delta** (`onDelta`); size `"0"` deletes a price level.

Wire via `market.BridgeGRVTDeltas`.

### WS trades — `SubscribeTicks` (`v1.trade`)

Feed: `{instrument}@{limit}` e.g. `BTC_USDT_Perp@50`.

## Parse — vendor → HHLD

### Book body → `exchange.Book`

Vendor feed / REST `result`:

```json
{
  "event_time": "1700000000000000000",
  "instrument": "BTC_USDT_Perp",
  "bids": [ { "price": "100.0", "size": "1.5" } ],
  "asks": [ { "price": "100.1", "size": "0.8" } ]
}
```

| Vendor | HHLD |
|--------|------|
| (fixed) | `Venue = "grvt"` |
| (request symbol) | `Symbol` = HHLD symbol |
| (adapter `Kind`) | `Kind` |
| `bids[].price` / `size` | `Bids[].Price` / `Size` |
| `asks[].price` / `size` | `Asks[].Price` / `Size` |
| `event_time` (string int) | `Time` via `parseUnixNano` (see below) |
| `num_orders` (if present) | **Dropped** |

REST: unwrap `{"result":{...}}` first.

### Delta body → `market.BookDelta` (via adapter `DeltaHandler`)

```json
{
  "event_time": "1700000001000000000",
  "instrument": "BTC_USDT_Perp",
  "bids": [ { "price": "100.0", "size": "0" } ],
  "asks": [],
  "sequence_number": "2"
}
```

| Vendor | HHLD / market |
|--------|----------------|
| `bids` / `asks` levels | `BookDelta.Bids` / `Asks` |
| `size: "0"` | Delete that price in `BookStore` |
| `sequence_number` | `BookDelta.Seq` (uint64); used to reject backward seq |
| | `Venue`, `Symbol`, `Kind`, `Time` as for books |

### Tick → `exchange.Tick`

Feed may be one object or an array:

```json
{
  "event_time": "...",
  "instrument": "BTC_USDT_Perp",
  "price": "100.05",
  "size": "0.1",
  "side": "BUY",
  "is_taker_buyer": true
}
```

| Vendor | HHLD |
|--------|------|
| `price` / `size` | `Price` / `Size` |
| `side` BUY/Bid | `SideBuy` |
| `side` SELL/Ask | `SideSell` |
| else `is_taker_buyer` | buy if true, sell if false |
| `event_time` | `Time` UTC |

### Time parsing (`event_time`)

String integer heuristics in `parseUnixNano`:

| Magnitude | Interpretation |
|-----------|----------------|
| `< 1e12` | unix **seconds** |
| `< 1e14` | unix **milliseconds** |
| else | unix **nanoseconds** (GRVT streams typically) |

Empty / bad → fallback `time.Now().UTC()`.

## Bridge into HHLD event core

```text
v1.book.s  → BridgeBooks        → SnapshotEvent
v1.book.d  → BridgeGRVTDeltas   → Clear on session; Snapshot then DeltaEvent
```

Strategy still sees full books from `BookStore` via `OnBooks` (deltas stay below Strategy).

## Orders (not implemented live)

Trading requires auth against `trades.*` (API key or wallet login → cookie + account id). Order DTOs are richer than HHLD `OrderRequest` today. See [p9.md](../roadmap/p9.md). Adapter place/cancel → `ErrReadOnly`.

## Related

- Config example: [`configs/default.json`](../../configs/default.json)
- BookStore delta rules: [trading.md](../trading.md) (event-driven section)
- Architecture: [architect.md](../architect.md)
