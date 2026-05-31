# Architecture: Real-Time Risk Engine

## Design Philosophy
Pre-trade risk evaluation must be:
1. **Deterministic** — Same order always produces same decision
2. **Low-latency** — Sub-microsecond evaluation on the critical path
3. **Fail-closed** — Any uncertainty rejects the order

## Rule Evaluation Pipeline
```

Order In
|
v
[Validation] ------> Reject if malformed
|
v
[Duplicate Check] -> Reject if seen recently
|
v
[Circuit Breaker] -> Reject if trading halted
|
v
[Fat Finger Detector] -> Reject if anomalous
|
v
[Notional Limit] --> Reject if exposure breached
|
v
[Position Limit] --> Reject if position breached
|
v
Accepted -> Apply state changes

```

Rules are ordered by severity. Circuit breaker (system-level halt)
takes precedence over position limits (single-account constraint).

## Architectural Decision Records

### ADR-001: Synchronous Inline Evaluation
**Decision:** Risk checks execute synchronously in the order path.
**Rationale:** Financial regulations require pre-trade checks to complete
before routing. Asynchronous evaluation would allow orders to reach
the market before checks complete.

### ADR-002: Ordered Rule Evaluation
**Decision:** Rules evaluated in fixed order, first rejection stops.
**Rationale:** Circuit breakers must halt all trading immediately.
Fat-finger checks prevent erroneous data from polluting position trackers.

### ADR-003: In-Memory State with External Limits
**Decision:** Position and notional state held in memory for speed.
Limits configurable via gRPC at runtime.
**Rationale:** Database lookups on the critical path would add milliseconds.
State can be rebuilt from ledger on restart.

### ADR-004: Circuit Breaker Pattern
**Decision:** Three-state breaker (Closed, Open, Half-Open) per account.
**Rationale:** Prevents cascading losses. Half-open state allows controlled
resumption of trading after cool-down period.

### ADR-005: Go Implementation
**Decision:** Go selected for risk engine over C++ or Rust.
**Rationale:** Go's goroutine model handles concurrent order evaluation
efficiently. Garbage collection pauses are sub-millisecond and acceptable
for pre-trade checks (unlike in the matching engine path).
