# Concurrency, performance, and hedge ordering

Constitution for when many orders are in flight at once. Goal: protect **risk correctness** first; cap concurrency so performance stays predictable on a single medium instance.

## Problem

Parallel Decisions (arb or hedge) can:

- Overwhelm venue rate limits and local CPU/IO → slower acks, more timeouts, worse fills.
- Interleave Risk/execution for the **same hedge or correlated symbols** → Risk sees inconsistent exposure (double size, overlapping legs, unpaired recovery racing a new entry).

Miss-more still applies: under load, **drop or queue** new opportunities rather than evaluate Risk on stale or racing state.

## Rules

1. **Per-hedge serial risk and execution** — for a given `HedgeID` (and, while in flight, its `TraceID` legs), only **one** Risk evaluation + place/cancel/reconcile pipeline runs at a time. Next Decision for that hedge waits or is rejected (miss).
2. **Per-symbol (or per arb key) mutex for same-kind arb** — when `HedgeID` is empty, serialize by a stable key such as `(symbol, venueA, venueB)` so two arb Decisions cannot both pass Risk on the same books/exposure concurrently.
3. **Global in-flight cap** — hard limit on concurrent order pipelines (and optionally concurrent Risk evaluations). When the cap is hit, new Decisions are **not** taken (miss), not piled into an unbounded queue.
4. **Bounded queues only** — if a short queue is used for fairness, it must be size-capped with drop-oldest or drop-newest policy; never unbounded buffers that hide overload.
5. **Prefer miss under overload** — latency budget for Risk+place is part of the edge check; if the pipeline is saturated, treat as failed latency gate.

## Ordering model

```text
Strategy emits Decision(TraceID, HedgeID?, Legs)
        │
        ▼
  lock key = HedgeID if set, else arb key (symbol + venues)
        │
        ▼
  [optional] admit if under global in-flight cap
        │
        ▼
  Risk.Evaluate(Decision)     ← exclusive for that lock key
        │
        ▼
  place / ack / reconcile legs ← same lock held through terminal state
        │
        ▼
  release lock; Admin persists under TraceID / HedgeID
```

Different hedges (different keys) **may** run in parallel up to the global cap. Same hedge (or same arb key) **must not**.

## Performance expectations

- Design for **one medium instance**: modest parallelism across independent keys, strict serial per key.
- Book subscriptions stay streaming; the bottleneck to protect is **Risk + order placement**, not book fan-in.
- Measure in-flight count, queue drops, Risk reject reasons (`overloaded`, `lock_busy`, `stale_book`) for P10.

## Phase ownership

| Concern | Phase |
|---------|-------|
| Risk gates including overload / lock-busy as miss | [P4](roadmap/p4.md) |
| Execution pipeline holding per-hedge order | [P3](roadmap/p3.md)–[P9](roadmap/p9.md) |
| Metrics / tracing of drops and in-flight | [P10](roadmap/p10.md) |

Related: networking fail-closed and unpaired-leg recovery in [networking.md](networking.md).
