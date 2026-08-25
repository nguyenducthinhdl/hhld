# Quality assurance

Constitution for how HHLD keeps Trading Core trustworthy for a solo operator: tests, coverage, and what “done” means before merging.

Related: [tech-stack.md](tech-stack.md), [trading.md](trading.md), [networking.md](networking.md), [roadmap/README.md](roadmap/README.md).

## Goals

- Catch regressions in Strategy / Risk / paper place / PnL / warehouse without live capital.
- Prefer **fast, deterministic** unit tests (`fake` clocks, SQLite temp DBs) over flaky network tests until P8+.
- Keep a clear coverage bar so critical packages do not drift untested.

## Commands

```bash
go test ./...
go test ./src/... -coverprofile=coverage.out
go tool cover -func=coverage.out   # statement (C0) summary
go tool cover -html=coverage.out   # optional local HTML report
```

Phase “done when” entries in [roadmap/](roadmap/) assume `go test ./...` passes.

**Status legend**


| Status      | Meaning                                                   |
| ----------- | --------------------------------------------------------- |
| **pass**    | Implemented; must stay green on `go test ./...`           |
| **pending** | Planned (usually P8+ live smoke); not blocking paper path |




## Coverage bar (C0 / statement)

**Target:** at least **80% statement coverage** on packages under `src/` (Go’s `-cover` is statement / C0 style).


| Package                                                                                                                                            | Role                              | Coverage target  | Status       |
| -------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- | ---------------- | ------------ |
| `[config](../src/config/)`                                                                                                                         | JSON load / validate              | ≥ 80%            | pass (~100%) |
| `[exchange](../src/exchange/)` / `[fake](../src/exchange/fake/)` / `[hyperliquid](../src/exchange/hyperliquid/)` / `[grvt](../src/exchange/grvt/)` | Types + fake + live read adapters | ≥ 80%            | pass         |
| `[strategy](../src/strategy/)`                                                                                                                     | Arb + paper place                 | ≥ 80%            | pass         |
| `[risk](../src/risk/)`                                                                                                                             | Gate, fees, params                | ≥ 80%            | pass         |
| `[pnl](../src/pnl/)`                                                                                                                               | Realized inventory PnL            | ≥ 80%            | pass         |
| `[admin](../src/admin/)`                                                                                                                           | Audit + HTTP dashboard            | ≥ 80%            | pass         |
| `[sim](../src/sim/)`                                                                                                                               | Replay + analyzer                 | ≥ 80%            | pass         |
| `[warehouse](../src/warehouse/)`                                                                                                                   | SQLite store                      | ≥ 80%            | pass         |
| `[crawl](../src/crawl/)`                                                                                                                           | Sample / fake crawl stubs         | ≥ 80%            | pass         |
| `[market](../src/market/)`                                                                                                                         | BookStore + Bus + Runner (P8.5)   | ≥ 80%            | pass         |
| `[monitor](../src/monitor/)`                                                                                                                       | Forensics + unknown-ack (P10)     | ≥ 80%            | pass         |
| `[paperlive](../src/paperlive/)`                                                                                                                   | Paper arb on live books (P9)      | ≥ 80%            | pass         |
| `[ledger](../src/ledger/)`                                                                                                                         | Durable fills / PnL (wired in P11)| ≥ 80%            | pass         |
| `[secrets](../src/secrets/)`                                                                                                                       | Env pin / refuse keys / kill      | ≥ 80%            | pass         |
| `[place](../src/place/)`                                                                                                                           | Local one-leg place (P11)         | ≥ 80%            | pass         |
| `src/` **total**                                                                                                                                   | Combined                          | **≥ 80%** (~80.5%) | pass         |


Remaining gaps are usually hard-to-hit I/O or marshal failure branches — acceptable if the happy path and miss-more rejects are covered.

### What coverage is not

- Not a substitute for **scenario tests** (1-leg timeout, stale book, negative edge).
- Not branch (C1) or path coverage — those may come later if a hot path needs them.
- Not a requirement for `cmd/` binaries (thin mains; exercised manually or via smoke).



## Test layers


| Layer          | Where                                             | Intent                                                            | Status  |
| -------------- | ------------------------------------------------- | ----------------------------------------------------------------- | ------- |
| **Unit**       | `*_test.go` next to packages                      | Interface stubs, Gate rejects, fee schedule, SQLite, crawl NDJSON | pass    |
| **Contract**   | `[src/contract_test.go](../src/contract_test.go)` | Decision → Risk → `OrderRequest` legs compose                     | pass    |
| **Pipeline**   | strategy / admin / sim / crawl tests              | Paper arb E2E; crawl → warehouse → replay                         | pass    |
| **Live smoke** | P8+                                               | Real WS subscribe one symbol per venue                            | pending |




## Must-cover behaviors → tests

Merge blockers: each row must stay **pass**.


| Behavior                  | Test case                                                                                            | Test meaning                                               | Status  |
| ------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | ------- |
| Positive edge accepted    | `[TestGate_AcceptsPositiveEdge](../src/risk/gate_test.go)`                                           | Miss-more gate allows net-positive arb after fees          | pass    |
| Negative edge rejected    | `[TestGate_RejectsNegativeEdge](../src/risk/gate_test.go)`                                           | Fees/latency/partial-fill kill edge → miss                 | pass    |
| Per-venue fees in gate    | `[TestGate_UsesPerVenueFees](../src/risk/fee_test.go)`                                               | Rate/fixed/commission schedule applied per leg venue       | pass    |
| Stale book miss           | `[TestGate_RejectsStaleBook](../src/risk/gate_test.go)`                                              | Books older than `max_book_age` reject Decision            | pass    |
| Unhealthy venue miss      | `[TestGate_RejectsUnhealthyVenue](../src/risk/gate_test.go)`                                         | Reconnecting/down venue → no trade                         | pass    |
| Lock busy / overload      | `[TestGate_TryAcquire_LockBusy](../src/risk/gate_test.go)`, `[Overloaded](../src/risk/gate_test.go)` | Concurrency: same arb key / global in-flight cap           | pass    |
| Rate limit                | `[TestGate_RateLimited](../src/risk/gate_test.go)`                                                   | Second accept within `order_interval` → `rate_limited`     | pass    |
| Notional budget           | `[TestGate_BudgetExceeded](../src/risk/gate_test.go)`                                                | `sum(price×size)` over venue budget → `budget_exceeded`    | pass    |
| Max size                    | `[TestGate_MaxVolumeExceeded](../src/risk/gate_test.go)`                                             | Leg size above `trading.max_size` rejected                 | pass    |
| Size by symbol              | `[TestCrossVenueArb_UsesSizeBySymbol](../src/strategy/coverage_test.go)`                              | Legs use per-symbol `max_size`                             | pass    |
| 1-leg order timeout       | `[TestPlaceDecision_OneLegOrderTimeout](../src/strategy/paper_test.go)`                              | One venue times out; other may accept                      | pass    |
| Partial-leg audit         | `[TestRecordPaperDecision_PartialLegStillAuditable](../src/admin/memory_test.go)`                    | TraceID shows `accepted` + `error`; one fill only          | pass    |
| PnL after modeled fees    | `[TestRecordPaperDecision_ReconstructableArb](../src/admin/memory_test.go)`                          | Realized ≈ gap − `FeeSchedule` costs                       | pass    |
| Warehouse → sim           | `[TestSampleFileToSimReplay](../src/crawl/crawl_test.go)`                                            | NDJSON crawl → SQLite → Replay → positive PnL              | pass    |
| Admin PnL URL             | `[TestHandler_TradingPnL](../src/admin/http_test.go)`                                                | `GET /trading/pnl?format=json`                             | pass    |
| Admin orders URL          | `[TestHandler_TradingOrders](../src/admin/http_test.go)`                                             | `GET /trading/orders` lists TraceID legs                   | pass    |
| Market dashboard          | `[TestHandler_TradingMarketJSON](../src/admin/http_test.go)`                                         | Dual books + gap + config knobs JSON                       | pass    |
| Sim replay series         | `[TestHandler_SimJSONAndRun](../src/admin/http_test.go)`                                             | `/sim` JSON + POST overlay                                 | pass    |
| NDJSON input              | `[TestInputFromNDJSON_SampleAndEmpty](../src/sim/input_test.go)`                                     | Sample + empty crawl files                                 | pass    |
| Trace any venues          | `[TestTrace_GenericVenuesAndIgnoreThird](../src/sim/series_test.go)`                                 | `ex_a`/`ex_b`; extra venue ignored; hover explain          | pass    |
| BookStore delta apply     | `[TestBookStore_SnapshotThenDelta](../src/market/store_test.go)`                                     | Size `0` removes level; merge by price                     | pass    |
| Delta before snapshot     | `[TestBookStore_DeltaBeforeSnapshotRejected](../src/market/store_test.go)`                           | Reject until snapshot for key                              | pass    |
| Runner every update       | `[TestRunner_EvaluatesOnEachVenueUpdate](../src/market/runner_test.go)`                              | A-only miss; then both → OnBooks; B update → OnBooks again | pass    |
| Runner respects Gate      | `[TestRunner_LockBusyUnderBurst](../src/market/runner_test.go)`                                      | Event flood still hits `lock_busy`                         | pass    |
| Paper on live feeds       | `[TestStart_PaperPlacesOnLiveGap](../src/paperlive/paperlive_test.go)`                                | Fake live books → paper place → orders + PnL + admin URLs  | pass    |
| Unpaired halt             | `[TestRunner_UnpairedLegHaltsSymbol](../src/market/runner_test.go)`, `[TestGate_HaltBlocksEvaluateUntilResume](../src/risk/gate_test.go)` | 1-leg fail → Halt; Evaluate misses until Resume            | pass    |
| Unknown-ack reconcile     | `[TestReconcileUnknown_ResolvesViaGetOrder](../src/monitor/monitor_test.go)`                         | `GetOrder` by cloid; never retries PlaceOrder              | pass    |
| Forensics HTTP            | `[TestHandler_ForensicsHealthHalts](../src/admin/http_test.go)`                                      | `/trading/forensics`, `/health`, `/trading/halts`          | pass    |
| Ledger realizes from fills | `[TestSQLite_RestartKeepsRealized](../src/ledger/sqlite_test.go)`                                    | Restart rebuilds `positions_strategy.realized`; unique fill_id; env isolation | pass    |
| Fake/HL snapshot bridge   | `[TestBridgeBooks_PublishesSnapshots](../src/market/bridge_test.go)`                                 | SubscribeBook → Snapshot events → store                    | pass    |
| Fake delta bridge         | `[TestBridgeFakeDeltas_AppliesViaBus](../src/market/bridge_test.go)`                                 | PushDelta size `0` removes level via bus                   | pass    |
| GRVT book.d bridge        | `[TestBridgeGRVTDeltas_ClearsAndApplies](../src/market/bridge_test.go)`                              | Clear on reconnect; snap+delta into store                  | pass    |
| GRVT book.d WS            | `[TestAdapter_SubscribeBookDeltas](../src/exchange/grvt/adapter_test.go)`                            | Snapshot then delta on `v1.book.d`                         | pass    |
| Live HL/GRVT tick/book WS | *(manual / P9)*                                                                                      | Mainnet WS smoke for one symbol per venue                  | pending |
| Live one-leg PlaceOrder   | *(P11; httptest, no real keys)*                                                                      | `{kind, symbol, side, size}` → signed envelope; env pin    | pending |
| Local PlaceOrder          | `[TestLocal_PlacesAndLedgers](../src/place/place_test.go)`                                           | `-env local` / `-trade-local` same request; no keys        | pass    |
| Local refuse live keys    | `[TestRefuseLocal_LiveKey](../src/secrets/secrets_test.go)`, `[TestLocal_RefuseLiveKey](../src/place/place_test.go)` | `-env local` exits if venue secrets are in the environment | pass    |
| Production dual-confirm   | *(P11)*                                                                                              | `mainnet`/`prod` refused without confirm + `HHLD_LIVE_ORDERS` | pending |
| Kill switch               | `[TestKillEngaged](../src/secrets/secrets_test.go)`, `[TestLocal_Kill](../src/place/place_test.go)`  | `HHLD_KILL` refuses place; no secret in logs               | pass    |




## Test catalog (by package)



### Contract


| Test case                                                     | Test meaning                                                        | Status |
| ------------------------------------------------------------- | ------------------------------------------------------------------- | ------ |
| `[TestDecisionFlowsToOrderRequests](../src/contract_test.go)` | Hedge Decision → Risk OK → `OrderRequest` legs keep TraceID/HedgeID | pass   |




### Config


| Test case                                                               | Test meaning                                   | Status |
| ----------------------------------------------------------------------- | ---------------------------------------------- | ------ |
| `[TestDefault_Validate](../src/config/config_test.go)`                  | Default BTCUSD / HL+GRVT config validates      | pass   |
| `[TestLoadJSON_CrawETHUSD](../src/config/config_test.go)`               | `configs/craw-ethusd.json` maps ETHUSD natives | pass   |
| `[TestLoadJSON_CrawSOLUSD](../src/config/config_test.go)`               | `configs/craw-solusd.json` maps SOLUSD natives | pass   |
| `[TestLoadJSON_CrawTRUMPUSD](../src/config/config_test.go)`             | `configs/craw-trumpusd.json` maps TRUMPUSD natives | pass   |
| `[TestParseJSON_OverridesAndMultiSymbol](../src/config/config_test.go)` | JSON overrides size/gap/timeouts; multi-symbol | pass   |
| `[TestLoadJSON_FromFile](../src/config/config_test.go)`                 | Load config from disk path                     | pass   |
| `[TestLoadJSON_RepoDefault](../src/config/config_test.go)`              | `configs/default.json` loads and maps natives  | pass   |
| `[TestValidate_RejectsBadVenues](../src/config/config_test.go)`         | Identical venues.a/b rejected                  | pass   |
| `[TestDuration_MarshalUnmarshal](../src/config/coverage_test.go)`       | Duration string / nanosecond JSON              | pass   |
| `[TestValidate_MoreRejects](../src/config/coverage_test.go)`            | Empty symbol_map, bad fees, fill factor, etc.  | pass   |
| `[TestParseJSON_InvalidAndLoadMissing](../src/config/coverage_test.go)` | Bad JSON / missing file / empty symbol_map     | pass   |
| `[TestNativeSymbol_FromDefaultMap](../src/config/symbolmap_test.go)`    | Default map HL/GRVT native ids                 | pass   |
| `[TestParseJSON_SymbolMapArray](../src/config/symbolmap_test.go)`       | Array symbol_map + default order_interval      | pass   |
| `[TestParseJSON_PerVenueBudgetsAndInterval](../src/config/symbolmap_test.go)` | Per-venue budget + effective size        | pass   |




### Exchange + fake


| Test case                                                                               | Test meaning                                      | Status  |
| --------------------------------------------------------------------------------------- | ------------------------------------------------- | ------- |
| `[TestManualClock_Deterministic](../src/exchange/clock_test.go)`                        | ManualClock set/advance for tests                 | pass    |
| `[TestExchangeStub_SnapshotAndSubscribe](../src/exchange/exchange_test.go)`             | Stub Exchange book/tick subscribe                 | pass    |
| `[TestExchangeStub_PlaceAndCancelOrder](../src/exchange/exchange_test.go)`              | Stub paper place/cancel                           | pass    |
| `[TestPredictionKind_OnBookAndOrder](../src/exchange/exchange_test.go)`                 | Prediction `Kind` on book/order shape             | pass    |
| `[TestDualFeed_BooksDriveConsumerWithoutNetwork](../src/exchange/fake/fake_test.go)`    | Dual fake venues push books, no network           | pass    |
| `[TestFake_PushTicksToConsumer](../src/exchange/fake/fake_test.go)`                     | `PushTick` → `SubscribeTicks`                     | pass    |
| `[TestFake_PaperOrderUsesClock](../src/exchange/fake/fake_test.go)`                     | Ack time from shared clock                        | pass    |
| `[TestFake_BookDelayThenSuccess](../src/exchange/fake/fake_test.go)`                    | Book delay under budget succeeds                  | pass    |
| `[TestFake_BookDelayTimeout](../src/exchange/fake/fake_test.go)`                        | Book path deadline → error                        | pass    |
| `[TestFake_OrderDelayTimeout](../src/exchange/fake/fake_test.go)`                       | Order path deadline → error                       | pass    |
| `[TestFake_IDCancelAndNilClock](../src/exchange/fake/coverage_test.go)`                 | Venue ID; cancel idempotent; nil clock default    | pass    |
| `[TestWallClock_Now](../src/exchange/fake/coverage_test.go)`                            | WallClock returns wall time                       | pass    |
| `[TestAdapter_SnapshotBook](../src/exchange/hyperliquid/adapter_test.go)` (HL)          | REST `l2Book` → normalized `Book`                 | pass    |
| `[TestAdapter_SubscribeBookAndTicks](../src/exchange/hyperliquid/adapter_test.go)` (HL) | WS `l2Book` / `trades` via fake dial              | pass    |
| `[TestAdapter_ReadOnlyOrders](../src/exchange/hyperliquid/adapter_test.go)` (HL)        | Place/Cancel/GetOrder → `ErrReadOnly`             | pass    |
| `[TestAdapter_SnapshotBook](../src/exchange/grvt/adapter_test.go)` (GRVT)               | REST `full/v1/book` → normalized `Book`           | pass    |
| `[TestAdapter_SubscribeBookAndTicks](../src/exchange/grvt/adapter_test.go)` (GRVT)      | WS `v1.book.s` / `v1.trade` via fake dial         | pass    |
| `[TestAdapter_ReadOnlyOrders](../src/exchange/grvt/adapter_test.go)` (GRVT)             | Place/Cancel/GetOrder → `ErrReadOnly`            | pass    |
| Live signed PlaceOrder (HL/GRVT)                                                        | P11: IOC envelope, env pin, no real keys          | pending |
| Live adapter book/tick smoke                                                            | HL/GRVT mainnet WS one-symbol check (manual / P9) | pending |




### Strategy


| Test case                                                                     | Test meaning                              | Status |
| ----------------------------------------------------------------------------- | ----------------------------------------- | ------ |
| `[TestStrategy_OnBooksEmitsTwoLegArb](../src/strategy/strategy_test.go)`      | Stub strategy emits buy/sell legs         | pass   |
| `[TestStrategy_HedgeDecisionShape](../src/strategy/strategy_test.go)`         | Prediction↔crypto hedge Decision shape    | pass   |
| `[TestCrossVenueArb_EmitsPaperDecisionOnGap](../src/strategy/paper_test.go)`  | Real arb detects gap → two-leg Decision   | pass   |
| `[TestPlaceDecision_OneLegOrderTimeout](../src/strategy/paper_test.go)`       | Concurrent place; one leg times out       | pass   |
| `[TestPlaceDecision_TwoLegsOrderTimeout](../src/strategy/paper_test.go)`      | Both legs time out                        | pass   |
| `[TestPlaceDecision_TwoLegsOrderDelaySuccess](../src/strategy/paper_test.go)` | Delay under budget → both accepted        | pass   |
| `[TestSnapshotBooks_OneLegBookTimeout](../src/strategy/paper_test.go)`        | One venue book snapshot times out         | pass   |
| `[TestSnapshotBooks_TwoLegsBookTimeout](../src/strategy/paper_test.go)`       | Both book paths time out                  | pass   |
| `[TestArbConfigFrom_AndName](../src/strategy/coverage_test.go)`               | Config → ArbConfig; strategy name         | pass   |
| `[TestCrossVenueArb_UsesSizeBySymbol](../src/strategy/coverage_test.go)`      | Legs use per-symbol max_size              | pass   |
| `[TestCrossVenueArb_EmptyDefaultsAndNoGap](../src/strategy/coverage_test.go)` | Below min_gap → no Decision; canceled ctx | pass   |




### Risk


| Test case                                                               | Test meaning                         | Status |
| ----------------------------------------------------------------------- | ------------------------------------ | ------ |
| `[TestStubRisk_AcceptsValidArbDecision](../src/risk/risk_test.go)`      | Risk interface stub accepts arb      | pass   |
| `[TestStubRisk_RejectsIncompleteHedge](../src/risk/risk_test.go)`       | Hedge with <2 legs rejected          | pass   |
| `[TestStubRisk_RejectsEmptyLegs](../src/risk/risk_test.go)`             | Empty Decision rejected              | pass   |
| `[TestGate_AcceptsPositiveEdge](../src/risk/gate_test.go)`              | Gate OK when net edge > 0            | pass   |
| `[TestGate_RejectsNegativeEdge](../src/risk/gate_test.go)`              | Gate miss when costs wipe edge       | pass   |
| `[TestGate_RejectsStaleBook](../src/risk/gate_test.go)`                 | Stale book reason                    | pass   |
| `[TestGate_RejectsUnhealthyVenue](../src/risk/gate_test.go)`            | Unhealthy venue reason               | pass   |
| `[TestGate_RejectsIncompleteHedge](../src/risk/gate_test.go)`           | HedgeID set but one leg              | pass   |
| `[TestGate_TryAcquire_LockBusy](../src/risk/gate_test.go)`              | Same lock key → `lock_busy`          | pass   |
| `[TestGate_TryAcquire_Overloaded](../src/risk/gate_test.go)`            | Cap → `overloaded`                   | pass   |
| `[TestGate_ConcurrentSameKeySerialized](../src/risk/gate_test.go)`      | Contention produces lock_busy misses | pass   |
| `[TestGate_RateLimited](../src/risk/gate_test.go)`                      | Per-symbol order_interval miss       | pass   |
| `[TestGate_BudgetExceeded](../src/risk/gate_test.go)`                   | Notional budget miss                 | pass   |
| `[TestGate_MaxVolumeExceeded](../src/risk/gate_test.go)`                | max_size defense                 | pass   |
| `[TestGate_HaltBlocksEvaluateUntilResume](../src/risk/gate_test.go)`   | Halt rejects; Resume allows again | pass   |
| `[TestParamsFromConfig_BudgetsAndIntervals](../src/risk/gate_test.go)`  | Config → budgets / interval / max vol | pass  |
| `[TestVenueFee_RateFixedCommission](../src/risk/fee_test.go)`           | Rate, fixed, commission additive     | pass   |
| `[TestExplainDecision_GapAndFees](../src/risk/explain_test.go)`         | Gap formula + per-leg fee breakdown  | pass   |
| `[TestFeeSchedule_PerVenueAndDefault](../src/risk/fee_test.go)`         | Per-venue + default bps fallback     | pass   |
| `[TestGate_UsesPerVenueFees](../src/risk/fee_test.go)`                  | Evaluate uses venue schedule         | pass   |
| `[TestFormulaEstimator_Estimate](../src/risk/estimator_test.go)`        | Formula estimator returns Estimate   | pass   |
| `[TestCompose_Manager](../src/risk/estimator_test.go)`                  | Gate + Estimator Manager compose     | pass   |
| `[TestParamsFromConfig_DefaultsAndAlign](../src/risk/coverage_test.go)` | Params defaults / fee align          | pass   |
| `[TestNewGate_ClampsBadParams](../src/risk/coverage_test.go)`           | Bad fill factor / in-flight clamped  | pass   |
| `[TestLockKey_HedgeAndEmpty](../src/risk/coverage_test.go)`             | Lock keys: hedge / arb / trace       | pass   |
| `[TestFeeSchedule_ZeroInputs](../src/risk/coverage_test.go)`            | Zero price/size → zero fee           | pass   |




### PnL


| Test case                                                             | Test meaning                                  | Status |
| --------------------------------------------------------------------- | --------------------------------------------- | ------ |
| `[TestTracker_RecordFillAndSnapshotByHedge](../src/pnl/pnl_test.go)`  | Tracker interface stub + hedge snapshot       | pass   |
| `[TestMemory_ArbBuySellRealizesGap](../src/pnl/memory_test.go)`       | Buy then sell realizes ~gap                   | pass   |
| `[TestMemory_SnapshotByHedge](../src/pnl/memory_test.go)`             | Hedge-scoped realized after fees              | pass   |
| `[TestMemory_CoverShortAndPartialClose](../src/pnl/coverage_test.go)` | Short cover, flip long/short, bad fill inputs | pass   |




### Admin


| Test case                                                                         | Test meaning                             | Status |
| --------------------------------------------------------------------------------- | ---------------------------------------- | ------ |
| `[TestAuditor_RecordAndListByHedge](../src/admin/admin_test.go)`                  | Auditor stub list by hedge               | pass   |
| `[TestMemory_RecordAndListByTrace](../src/admin/memory_test.go)`                  | Persist order; list by TraceID           | pass   |
| `[TestRecordPaperDecision_ReconstructableArb](../src/admin/memory_test.go)`       | Paper place → orders + fee’d fills + PnL | pass   |
| `[TestRecordPaperDecision_PartialLegStillAuditable](../src/admin/memory_test.go)` | 1-leg fail still auditable               | pass   |
| `[TestRecordPaperDecision_RejectsUnparseablePriceSize](../src/admin/memory_test.go)` | Bad/zero price or size → no fill     | pass   |
| `[TestHandler_TradingPnL](../src/admin/http_test.go)`                             | `/trading/pnl` JSON                      | pass   |
| `[TestHandler_ForensicsHealthHalts](../src/admin/http_test.go)`                    | `/trading/forensics`, `/health`, halt/resume | pass   |
| `[TestHandler_ResumeRequiresSymbolAndHandler](../src/admin/http_test.go)`          | Resume 503/400; forensics 503                    | pass   |
| `[TestHandler_HTMLAcceptAndHedgeQuery](../src/admin/coverage_test.go)`            | HTML PnL/orders; Accept JSON; hedge_id   | pass   |
| `[TestHandler_NilAuditor](../src/admin/coverage_test.go)`                         | Nil auditor → 503                        | pass   |
| `[TestMemory_FiltersPnLByHedgeAndNilTracker](../src/admin/coverage_test.go)`      | Filters, PnLByHedge, nil tracker ctor    | pass   |
| `[TestRecordPaperDecision_NilTracker](../src/admin/coverage_test.go)`             | Nil tracker rejected                     | pass   |




### Sim (backtest)


| Test case                                                         | Test meaning                              | Status |
| ----------------------------------------------------------------- | ----------------------------------------- | ------ |
| `[TestSimulator_RunReturnsSnapshot](../src/sim/sim_test.go)`      | Simulator stub returns PnL snapshot       | pass   |
| `[TestAnalyzer_Contract](../src/sim/analyzer_test.go)`            | Analyzer WinningRate / Distribution shape | pass   |
| `[TestReplay_RunAccumulatesPnL](../src/sim/replay_test.go)`       | Book replay → Strategy/Risk/place → PnL   | pass   |
| `[TestReplay_WinningDistribution](../src/sim/replay_test.go)`     | Distribution dims + win rate              | pass   |
| `[TestReplay_RiskRejectsNegativeEdge](../src/sim/replay_test.go)` | Harsh fees → no win samples               | pass   |
| `[TestInputFromStore](../src/sim/input_test.go)`                  | Warehouse books → `sim.Input`             | pass   |
| `[TestInputFromStore_Ticks](../src/sim/coverage_test.go)`         | Warehouse ticks → `sim.Input`             | pass   |
| `[TestReplay_NilArgsAndFilters](../src/sim/coverage_test.go)`     | Nil args; Filter exclude; bare Risk path  | pass   |




### Warehouse + crawl (P7)


| Test case                                                                   | Test meaning                                    | Status  |
| --------------------------------------------------------------------------- | ----------------------------------------------- | ------- |
| `[TestStore_AppendAndQuery](../src/warehouse/warehouse_test.go)`            | Store interface stub append/query               | pass    |
| `[TestSQLite_AppendAndQuery](../src/warehouse/sqlite_test.go)`              | SQLite books/ticks persist and query            | pass    |
| `[TestSQLite_OpenFailure](../src/warehouse/coverage_test.go)`               | Invalid DB path fails open                      | pass    |
| `[TestSQLite_CloseNilAndCanceled](../src/warehouse/coverage_test.go)`       | Nil Close; canceled ctx on append/query         | pass    |
| `[TestSQLite_EmptyQueryRange](../src/warehouse/coverage_test.go)`           | Wrong symbol → empty results                    | pass    |
| `[TestSampleFileToSimReplay](../src/crawl/crawl_test.go)`                   | Sample NDJSON → warehouse → Replay PnL          | pass    |
| `[TestFakeDualCrawler](../src/crawl/crawl_test.go)`                         | FakeDual writes 4 scripted books                | pass    |
| `[TestSampleFile_TicksAndComments](../src/crawl/coverage_test.go)`          | Tick lines + `#` comments in NDJSON             | pass    |
| `[TestSampleFile_Errors](../src/crawl/coverage_test.go)`                    | Missing path/store/file; bad JSON; unknown type | pass    |
| `[TestFakeDual_FillsDefaultsAndCustomBooks](../src/crawl/coverage_test.go)` | Default symbol/kind; canceled ctx               | pass    |
| Live crawl via WS into warehouse                                            | P8 adapters → `AppendBook`/`AppendTick`         | pending |




### Market (P8.5)


| Test case                                                                  | Test meaning                                      | Status |
| -------------------------------------------------------------------------- | ------------------------------------------------- | ------ |
| `[TestBookStore_SnapshotThenDelta](../src/market/store_test.go)`           | Size `0` removes level; merge by price            | pass   |
| `[TestBookStore_DeltaBeforeSnapshotRejected](../src/market/store_test.go)` | Reject until snapshot for key                     | pass   |
| `[TestRunner_EvaluatesOnEachVenueUpdate](../src/market/runner_test.go)`    | Peer miss then OnBooks on each side update        | pass   |
| `[TestRunner_UnpairedLegHaltsSymbol](../src/market/runner_test.go)`    | 1-leg reject → Halt; no further places    | pass   |
| `[TestRunner_OneLegTimeoutHalts](../src/market/runner_test.go)`        | Order timeout → Halt                      | pass   |
| `[TestBridgeBooks_PublishesSnapshots](../src/market/bridge_test.go)`       | Fake SetBook → Snapshot on bus                    | pass   |
| `[TestBridgeFakeDeltas_AppliesViaBus](../src/market/bridge_test.go)`       | PushDelta → store via bus                         | pass   |
| `[TestBridgeGRVTDeltas_ClearsAndApplies](../src/market/bridge_test.go)`    | book.d bridge Clears on reconnect then snap+delta | pass   |
| `[TestBus_DropsWhenFull](../src/market/runner_test.go)`                    | Bounded queue drops under overload                | pass   |


### Paper live (P9)


| Test case                                                                  | Test meaning                                      | Status |
| -------------------------------------------------------------------------- | ------------------------------------------------- | ------ |
| `[TestStart_PaperPlacesOnLiveGap](../src/paperlive/paperlive_test.go)`     | Fake dual books → paper place → orders + PnL      | pass   |
| `[TestStart_LogsMissingSeedSnapshot](../src/paperlive/paperlive_test.go)`  | Seed SnapshotBook miss is logged                  | pass   |
| `[TestStart_RejectsMissingLiveVenue](../src/paperlive/paperlive_test.go)`  | Both live adapters required                       | pass   |
| `[TestLiveAdapters_DefaultVenues](../src/paperlive/paperlive_test.go)`     | Constructs HL+GRVT adapters (no network)          | pass   |
| `[TestStart_DefaultPaperAndAuditor](../src/paperlive/coverage_test.go)`    | Nil paper/auditor defaults; still places          | pass   |


### Monitor (P10)


| Test case                                                                  | Test meaning                                      | Status |
| -------------------------------------------------------------------------- | ------------------------------------------------- | ------ |
| `[TestInspect_UnpairedHaltAndFlatten](../src/monitor/monitor_test.go)`     | 1-leg fail → halt + flatten plan (not sent)       | pass   |
| `[TestReconcileUnknown_ResolvesViaGetOrder](../src/monitor/monitor_test.go)` | Fake `GetOrder` clears unknown ack              | pass   |
| `[TestReconcileUnknown_StaysUnknown](../src/monitor/monitor_test.go)`      | Missing order stays `ErrUnknownAck` (no retry)    | pass   |


### Ledger (schema; wire in P11)


| Test case                                                                  | Test meaning                                      | Status |
| -------------------------------------------------------------------------- | ------------------------------------------------- | ------ |
| `[TestSQLite_FillsRealizeAndDedupe](../src/ledger/sqlite_test.go)`         | Opposite fills realize; duplicate fill_id ignored | pass   |
| `[TestSQLite_OrdersAndFilters](../src/ledger/sqlite_test.go)`              | Orders persist; env isolation                     | pass   |
| `[TestSQLite_RestartKeepsRealized](../src/ledger/sqlite_test.go)`          | Close/reopen keeps strategy realized              | pass   |


### Secrets / local place (P11 local rung)


| Test case                                                                  | Test meaning                                      | Status |
| -------------------------------------------------------------------------- | ------------------------------------------------- | ------ |
| `[TestRefuseLocal_LiveKey](../src/secrets/secrets_test.go)`                | Local env refuses live keys                       | pass   |
| `[TestEnvPin](../src/secrets/secrets_test.go)`                             | `HHLD_VENUE_ENV` must match `-env`                | pass   |
| `[TestKillEngaged](../src/secrets/secrets_test.go)`                        | `HHLD_KILL=1` or `hhld.kill`                      | pass   |
| `[TestLocal_PlacesAndLedgers](../src/place/place_test.go)`                 | One-leg fake place → GetOrder → ledger            | pass   |
| `[TestWire_LedgerTwoLegTrace](../src/paperlive/paperlive_test.go)`         | `-trade-local` path: two legs, one TraceID        | pass   |
| `[TestMarketEndpoints_EnvPin](../src/exchange/adapter_test.go)`            | Staging → HL testnet + GRVT staging hosts         | pass   |
| `[TestLoadJSON_EnvKnobFiles](../src/config/config_test.go)`                | `default-staging.json` / `default-production.json`| pass   |


## Fake vs live


| Mode                            | Use for QA                                           | Status  |
| ------------------------------- | ---------------------------------------------------- | ------- |
| `exchange/fake` + `ManualClock` | Default unit/pipeline tests (no network)             | pass    |
| Sample NDJSON + SQLite          | Offline backtest input (P7)                          | pass    |
| Live HL/GRVT adapters           | P8+ smoke only; do not gate every PR on venue uptime | pending |




## Definition of done (implementation PR)

- [ ] `go test ./...` passes  
- [ ] New packages / non-trivial logic include tests (add a row to the catalog above)  
- [ ] `src/` statement coverage stays **≥ 80%** (or justify a temporary dip in the PR)  
- [ ] Spec / roadmap phase file updated when a phase finishes  
- [ ] No secrets in fixtures; no live order placement in tests (P11 smokes are manual; unit tests use httptest + dummy keys)  



## Explicitly out of scope (for now)

- Full E2E against mainnet with real capital (P11 production smoke is **manual**, operator-gated)  
- Mutation testing / formal verification  
- Enforcing coverage in CI as a hard red build (recommended later; bar is documented here first)

