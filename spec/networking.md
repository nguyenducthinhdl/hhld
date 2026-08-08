# Networking and order errors

Constitution for dealing with network failures on **Book** (read) and **Order** (write) paths. Aligns with the miss-more doctrine in [mission.md](mission.md): prefer missed opportunities over bad or half-complete trades.

## Rule of thumb

| Path | Stance |
|------|--------|
| **Book** net error / stale data | **No trade** (miss). |
| **Order** net error / unknown ack | **Assume nothing** → reconcile; never double-send. |
| One leg live, other failed | **Stop and recover** unpaired exposure; do not keep hunting arb. |

## Book errors (read path)

Examples: WebSocket disconnect, timeout on `SnapshotBook` / `SubscribeBook`, sequence gap after reconnect, one venue fresh and the other stale.

**Required behavior:**

1. **Fail closed for decisions** — if either venue required for the Decision is missing, unhealthy, or reconnecting, Strategy emits no Decision (or Risk rejects). Never arb on one fresh book + one stale book.
2. **Staleness gate** — each book has a max age from `Book.Time` / last-receive time; past the budget → treat as data failure (same as networking error).
3. **Reconnect = fresh snapshot** — after drop, resubscribe and wait for a new book before trading again (do not apply stale deltas across reconnect). See P8 adapter notes in [roadmap.md](roadmap.md).
4. **Typed failures** — adapters should surface classifiable errors (e.g. unavailable, timeout, stale) so Connection/Risk can count intentional misses vs bugs.

## Order errors (write path)

Examples: timeout on `PlaceOrder`, unclear whether the venue accepted, cancel failure, leg A filled and leg B network-fails (arb or prediction↔crypto hedge).

**Required behavior:**

| Case | Action |
|------|--------|
| Clear reject / never left client | Safe: no position; log under TraceID; skip |
| Timeout / unknown ack | **Do not retry a duplicate** until reconcile (query order status / opens / fills when the venue API allows) |
| Leg A filled, Leg B fails | **Unpaired exposure** — pause strategy for that symbol/hedge; cancel or flatten per playbook; alert using TraceID / HedgeID |

Multi-leg intent:

- **Pre-trade (Risk):** both venues healthy, books fresh, latency/fee budget OK — else reject the whole Decision.
- **Post-trade:** if reality breaks all-or-nothing, run the unpaired-leg playbook; Admin must be able to reconstruct the attempt.

## Layering

```text
Exchange adapter / Connection
  → classify net errors, reconnect books, order-status reconcile when needed

Risk (pre-trade) — see P4
  → venues OK + books fresh + latency budget; else reject (miss)

Execution (paper/live loops — P3+)
  → place legs; on unknown ack: reconcile; on one-leg fill: halt + recover

Admin / tracing — see P5, P10
  → persist attempts, acks, errors under TraceID and HedgeID
```

Related: per-hedge serial Risk/execution and in-flight caps in [concurrency.md](concurrency.md).

## Interface note

P1 does not require extra methods for this doctrine. Later phases may add venue reconcile helpers (e.g. get order by id) on adapters when live/paper execution needs them. Risk and tracing phases own the gates and audit trail.

## Phase ownership

| Concern | Phase |
|---------|-------|
| Stale book / venue unhealthy / fee+latency miss-more | [P4](roadmap/p4.md) |
| Persist orders/errors for audit | [P5](roadmap/p5.md) |
| Live feed reconnect + contract smoke | [P8](roadmap/p8.md)–[P9](roadmap/p9.md) |
| Mismatch/lost-order forensics, deploy hardening | [P10](roadmap/p10.md) |
