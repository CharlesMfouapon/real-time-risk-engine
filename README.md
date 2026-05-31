# Real-Time Risk Engine

Pre-trade risk evaluation engine. Position limits, notional caps, fat-finger detection, circuit breakers. gRPC service with sub-microsecond evaluation latency.

## Architecture

```

FIX Order
|
v
+------------------+
|   Risk Engine    |  Go - Synchronous pre-trade evaluation
|                  |
|  [Validation]    |  Malformed order rejection
|  [Dup Check]     |  Duplicate order ID detection
|  [Circuit Brkr]  |  Three-state breaker per account
|  [Fat Finger]    |  Price deviation + rate limiting
|  [Notional Lim]  |  Total exposure caps
|  [Position Lim]  |  Per-symbol position limits
+--------+---------+
|
Accepted/Rejected
|
v
Order Book

```

## Risk Rules

| Rule | Description | Default |
|---|---|---|
| Validation | Field format and constraint checks | Always active |
| Duplicate Detection | Rejects repeated ClOrdID | Window: 5 min |
| Circuit Breaker | Halts trading on loss threshold | $1M max loss |
| Fat Finger | Anomalous price/quantity detection | 15% deviation |
| Notional Limit | Total exposure per account | $100M |
| Position Limit | Net position per symbol | 1M shares |

## Performance

- Target: <1 microsecond evaluation latency
- Concurrent: Goroutine-per-evaluation model
- Lock contention minimized with RWMutex per tracker

## Quick Start

```bash
# Requires protocol buffer compiler
brew install protobuf

# Generate gRPC code
buf generate

# Run tests
go test -race ./...

# Run benchmarks
go test -bench=. -benchmem ./internal/engine/...
```

## API

gRPC service defined in proto/risk/v1/risk.proto:

* EvaluateOrder - Primary endpoint for pre-trade checks
* GetAccountLimits - Current limit utilization
* ResetCircuitBreaker - Administrative resets
* HealthCheck - Service health and latency metrics

## Integration

This engine sits between the FIX Engine
and the Order Book:

```
fix-engine (C++) -> risk-engine (Go) -> limit-order-book (Java) -> immutable-ledger (Rust)
```

