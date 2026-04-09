# Work Queue Production Readiness Plan

> Tracking document for productionizing `a2asrv/workqueue` and its integration with `internal/taskexec`.

## P0-1: CallContext Propagation Through Work Queue

**Status**: Not started

**Problem**: In distributed/cluster mode, `CallContext` (auth identity, service params, tenant, extensions) is lost when work crosses the queue boundary. `workqueue.Payload` carries only request data — no `User`, `ServiceParams`, `Tenant`, or `Method`. The backend's `workQueueHandler.handle` passes a bare context to `factory.CreateExecutor`/`CreateCanceler`, so `CallContextFrom(ctx)` returns `(nil, false)` and `ExecutorContext` gets zero values for auth/tenant/extensions.

**Impact**: Any `AgentExecutor` relying on `ExecutorContext.User`, `.ServiceParams`, `.Tenant`, or extensions gets nil/empty values in cluster mode. Authentication, authorization, multi-tenancy, and extension negotiation silently break.

**Key files**:
- `a2asrv/middleware.go:42-53` — `CallContext` definition (unexported fields)
- `a2asrv/workqueue/queue.go:38-48` — `Payload` struct (missing call info)
- `a2asrv/agentexec.go:145-148,274-276` — `CallContext` read sites in `CreateExecutor`/`CreateCanceler`
- `internal/taskexec/work_queue_handler.go:59` — backend handler (no CallContext reconstruction)
- `internal/taskexec/distributed_manager.go:107,151` — frontend payload construction sites

**Approach**:
1. Add a `CallInfo` struct to `a2asrv` carrying the serializable subset of `CallContext`:
   - `User`, `ServiceParams` (headers), `Tenant`, `Method`, `ActivatedExtensions`
2. Add `CallInfo` field to `workqueue.Payload`.
3. Add `CallInfoFrom(ctx) *CallInfo` to `a2asrv` — extracts from `CallContext`.
4. Add `AttachCallInfo(ctx, *CallInfo) context.Context` to `a2asrv` — reconstructs a `CallContext` and attaches to ctx.
5. `distributedManager.Execute`/`Cancel`: populate `Payload.CallInfo` via `CallInfoFrom(ctx)`.
6. `workQueueHandler.handle`: reconstruct via `AttachCallInfo(ctx, payload.CallInfo)` before calling factory.

**Serialization contract**: `ServiceParams` wraps `http.Header` (`map[string][]string`) — serializes fine. `User.Claims` is `map[string]any` — must contain JSON-safe types only. Document this.

**Tasks**:
- [ ] Define `CallInfo` struct in `a2asrv`
- [ ] Add `CallInfoFrom(ctx)` and `AttachCallInfo(ctx, *CallInfo)` to `a2asrv`
- [ ] Add `CallInfo` field to `workqueue.Payload`
- [ ] Populate `CallInfo` in `distributedManager.Execute` and `Cancel`
- [ ] Reconstruct `CallContext` in `workQueueHandler.handle`
- [ ] Add tests: round-trip CallInfo through payload, verify ExecutorContext fields in cluster mode
- [ ] Document serialization contract for `User.Claims`

---

## P0-2: Concurrency Limiting in Work Queue

**Status**: Not started

**Problem**: `HandlerConfig.Limiter` (`ConcurrencyConfig`) is passed to `RegisterHandler` but ignored by both queue implementations. Pull queue spawns unbounded goroutines. Push queue accepts all work. `ErrConcurrencyLimitExceeded` is defined but never returned.

**Impact**: Under load, unbounded goroutine/resource consumption. No backpressure to the broker or external orchestrator.

**Key files**:
- `a2asrv/workqueue/pullqueue.go:77` — TODO: gate Read on concurrency quota
- `a2asrv/workqueue/pushqueue.go:39` — TODO: acquire quota or return ErrConcurrencyLimitExceeded
- `internal/taskexec/limiter.go:29-91` — existing limiter (local mode only)
- `a2asrv/limiter/limiter.go:27-35` — `ConcurrencyConfig` definition

**Approach**:
1. Create a semaphore-based `concurrencyGate` in the workqueue package, initialized from `HandlerConfig.Limiter.MaxExecutions`.
2. **Pull queue**: acquire semaphore before `Read()` — natural backpressure. Release in per-message goroutine's defer.
3. **Push queue**: try-acquire semaphore. If full, return `ErrConcurrencyLimitExceeded`.
4. **Per-scope limiting**: defer to application layer (scope isn't known until payload is deserialized in pull queue). Global limit only at the queue layer.

**Tasks**:
- [ ] Implement semaphore-based concurrency gate
- [ ] Wire into pull queue polling loop
- [ ] Wire into push queue handler invocation
- [ ] Add tests: verify limit enforcement, verify ErrConcurrencyLimitExceeded
- [ ] Document that per-scope limiting is the application layer's responsibility

---

## P0-3: Graceful Shutdown for Pull Queue

**Status**: Not started

**Problem**: Pull queue polling loop (`pullqueue.go:73`) spawns handler goroutines but doesn't wait for them to finish. When the loop exits (on `ErrQueueClosed`), in-flight handlers are abandoned.

**Impact**: Process shutdown abandons in-flight executions. Messages will be redelivered by the broker, but partial state may be left in the task store.

**Key files**:
- `a2asrv/workqueue/pullqueue.go:73-124` — polling loop and handler goroutines

**Shutdown model**: The `ReadWriter` implementation owns its own shutdown lifecycle. When its `Close()` (or equivalent) is called, the next `Read()` returns `ErrQueueClosed`. The pull queue loop already handles this (`pullqueue.go:79-82`) — it logs and returns. No context cancellation check needed in the loop; context is for registration, not shutdown.

The only missing piece is a `sync.WaitGroup` so the loop goroutine waits for in-flight handler goroutines to complete before returning.

**Approach**:
1. Add `sync.WaitGroup` to `pullQueue`.
2. `wg.Add(1)` before spawning each handler goroutine (`pullqueue.go:103`), `wg.Done()` in defer.
3. After the polling loop exits (on `ErrQueueClosed` or fatal error), call `wg.Wait()` to drain in-flight handlers before the `RegisterHandler` goroutine returns.

**Tasks**:
- [ ] Add WaitGroup to pullQueue, track in-flight handler goroutines
- [ ] Wait for drain after loop exit
- [ ] Add tests: verify in-flight handlers complete before RegisterHandler goroutine returns

---

## P1-4: Unit Tests for workqueue Package

**Status**: Not started

**Problem**: No unit tests exist in `a2asrv/workqueue/`. All testing is indirect through `internal/taskexec` integration tests.

**Missing coverage**:
- `NewPushQueue` — constructor, returned HandlerFn delegation
- `ExponentialReadBackoff.NextDelay` — exponential growth, max cap, jitter bounds
- Pull queue `Read` error handling — `ErrQueueClosed` stops loop, `ErrMalformedPayload` completes+continues, read errors trigger backoff
- `PullQueueConfig.ReadRetry` customization
- `Message.Complete`/`Return` error paths in pull queue

**Tasks**:
- [ ] Provide an in-memory implementation of a workqueue.
- [ ] Add `retry_test.go` — test ExponentialReadBackoff
- [ ] Add `pushqueue_test.go` — test NewPushQueue and HandlerFn delegation
- [ ] Add `pullqueue_test.go` — test polling loop with mock ReadWriter (all error paths)
- [ ] Test custom ReadRetryPolicy injection

---

## P1-5: Per-Message Retry Budget / Dead-Letter Semantics

**Status**: Not started

**Problem**: Failed handler → `msg.Return()` → infinite redelivery for consistently-failing messages. Malformed payloads silently discarded (TODO at `pullqueue.go:85`).

**Approach**:
1. Add optional `DeliveryAttempt() int` to `Message` interface (via interface upgrade check).
2. Add `MaxRetries int` and `DeadLetterCallback` to `PullQueueConfig`.
3. If attempts exceeded, `Complete()` + route to dead-letter callback instead of `Return()`.

**Tasks**:
- [ ] Design dead-letter callback API
- [ ] Add delivery attempt tracking
- [ ] Add max retry enforcement
- [ ] Add tests

---

## P2-6: Observability Hooks

**Status**: Not started — deferred until P0/P1 are solid.

Counters for messages processed/failed/retried, latency histograms, queue depth, concurrency utilization.

---

## P2-7: Push Queue Backpressure

**Status**: Not started — deferred until P0/P1 are solid.

Signal callers when handler capacity is saturated beyond `ErrConcurrencyLimitExceeded`.
