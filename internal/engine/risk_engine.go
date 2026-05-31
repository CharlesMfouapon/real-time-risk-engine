package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/CharlesMfouapon/real-time-risk-engine/internal/engine/rules"
	"github.com/CharlesMfouapon/real-time-risk-engine/internal/model"
)

// RiskEngine evaluates orders against all configured risk rules.
// This is the core component. Every order passes through here.
type RiskEngine struct {
	// Risk rules (evaluated in order)
	positionTracker  *rules.PositionTracker
	notionalTracker  *rules.NotionalTracker
	fatFingerDetector *rules.FatFingerDetector
	circuitBreaker   *rules.CircuitBreaker

	// Duplicate detection
	recentOrderIDs sync.Map // map[string]time.Time

	// Statistics
	evaluationsTotal   atomic.Int64
	rejectionsTotal    atomic.Int64
	evaluationLatency  atomic.Int64 // Cumulative nanoseconds

	// Logging
	logger *zap.Logger
}

// NewRiskEngine creates a new risk engine with default rules.
func NewRiskEngine(logger *zap.Logger) *RiskEngine {
	return &RiskEngine{
		positionTracker:   rules.NewPositionTracker(),
		notionalTracker:   rules.NewNotionalTracker(),
		fatFingerDetector: rules.NewFatFingerDetector(),
		circuitBreaker:    rules.NewCircuitBreaker(),
		logger:            logger,
	}
}

// EvaluateOrder runs an order through all risk checks.
// Rules are evaluated in order of severity. First rejection stops evaluation.
func (re *RiskEngine) EvaluateOrder(ctx context.Context, order *model.Order) *model.EvaluationResult {
	start := time.Now()
	re.evaluationsTotal.Add(1)

	result := &model.EvaluationResult{
		EvaluationID: fmt.Sprintf("EVAL-%d", re.evaluationsTotal.Load()),
		RiskMetrics:  make(map[string]float64),
	}

	// Check 0: Basic validation
	if err := order.Validate(); err != nil {
		result.Decision = model.DecisionRejected
		result.RejectReason = &model.RejectReason{
			Code:    model.RejectCodeUnspecified,
			Message: err.Error(),
		}
		re.recordResult(result, start)
		return result
	}

	// Check 1: Duplicate order detection
	if re.isDuplicate(order.ClOrdID) {
		result.Decision = model.DecisionRejected
		result.RejectReason = &model.RejectReason{
			Code:    model.RejectCodeDuplicateOrder,
			Message: fmt.Sprintf("Duplicate order ID: %s", order.ClOrdID),
		}
		re.recordResult(result, start)
		return result
	}

	// Check 2: Circuit breaker (system-level halt)
	if reason := re.circuitBreaker.Evaluate(order); reason != nil {
		result.Decision = model.DecisionRejected
		result.RejectReason = reason
		re.recordResult(result, start)
		return result
	}

	// Check 3: Fat-finger detection (anomalous patterns)
	if reason := re.fatFingerDetector.Evaluate(order); reason != nil {
		result.Decision = model.DecisionRejected
		result.RejectReason = reason
		re.recordResult(result, start)
		return result
	}

	// Check 4: Notional value limit
	if reason := re.notionalTracker.Evaluate(order); reason != nil {
		result.Decision = model.DecisionRejected
		result.RejectReason = reason
		re.recordResult(result, start)
		return result
	}

	// Check 5: Position limit
	if reason := re.positionTracker.Evaluate(order); reason != nil {
		result.Decision = model.DecisionRejected
		result.RejectReason = reason
		re.recordResult(result, start)
		return result
	}

	// All checks passed — accept the order
	result.Decision = model.DecisionAccepted

	// Apply state changes
	re.positionTracker.Apply(order)
	re.notionalTracker.Apply(order)
	re.fatFingerDetector.RecordPrice(order.Symbol, order.Price)
	re.recordOrderID(order.ClOrdID)

	// Populate risk metrics
	result.RiskMetrics["position"] = float64(re.positionTracker.GetPosition(order.AccountID, order.Symbol))
	result.RiskMetrics["notional"] = re.notionalTracker.GetCurrentNotional(order.AccountID)
	result.RiskMetrics["circuit_state"] = 0 // Closed

	re.recordResult(result, start)
	return result
}

// GetAccountLimits returns current limit status for an account.
func (re *RiskEngine) GetAccountLimits(accountID string) *model.AccountLimits {
	limits := &model.AccountLimits{
		AccountID:             accountID,
		CircuitBreakerTripped: re.circuitBreaker.IsTripped(accountID),
		OrdersEvaluatedToday:  re.evaluationsTotal.Load(),
		OrdersRejectedToday:   re.rejectionsTotal.Load(),
	}

	// Collect position limits (simplified — in production, iterate all symbols)
	// For demo, we return a snapshot of common symbols
	symbols := []string{"AAPL", "GOOG", "MSFT", "EUR/USD"}
	for _, symbol := range symbols {
		pos := re.positionTracker.GetPosition(accountID, symbol)
		maxLong, maxShort, _ := re.positionTracker.GetPositionLimit(accountID, symbol)

		utilization := 0.0
		if pos > 0 && maxLong > 0 {
			utilization = float64(pos) / float64(maxLong) * 100
		} else if pos < 0 && maxShort > 0 {
			utilization = float64(-pos) / float64(maxShort) * 100
		}

		limits.PositionLimits = append(limits.PositionLimits, model.PositionLimit{
			Symbol:           symbol,
			MaxLongPosition:  maxLong,
			MaxShortPosition: maxShort,
			CurrentPosition:  pos,
			UtilizationPct:   utilization,
		})
	}

	// Notional limit
	currentNotional := re.notionalTracker.GetCurrentNotional(accountID)
	maxNotional := 100_000_000.00 // Default
	limits.NotionalLimit = &model.NotionalLimit{
		MaxNotionalValue:     maxNotional,
		CurrentNotionalValue: currentNotional,
		UtilizationPct:       currentNotional / maxNotional * 100,
	}

	return limits
}

// ResetCircuitBreaker resets the breaker for an account.
func (re *RiskEngine) ResetCircuitBreaker(accountID string) error {
	return re.circuitBreaker.Reset(accountID)
}

// isDuplicate checks if an order ID was recently processed.
func (re *RiskEngine) isDuplicate(clOrdID string) bool {
	_, exists := re.recentOrderIDs.Load(clOrdID)
	return exists
}

// recordOrderID marks an order ID as processed.
func (re *RiskEngine) recordOrderID(clOrdID string) {
	re.recentOrderIDs.Store(clOrdID, time.Now())
}

// recordResult updates statistics and logs the evaluation.
func (re *RiskEngine) recordResult(result *model.EvaluationResult, start time.Time) {
	latency := time.Since(start)
	result.LatencyNs = latency.Nanoseconds()
	result.EvaluatedAt = time.Now()

	re.evaluationLatency.Add(latency.Nanoseconds())

	if result.Decision == model.DecisionRejected {
		re.rejectionsTotal.Add(1)
		re.logger.Warn("Order rejected",
			zap.String("evaluation_id", result.EvaluationID),
			zap.String("code", result.RejectReason.Code.String()),
			zap.String("message", result.RejectReason.Message),
			zap.Duration("latency", latency),
		)
	} else {
		re.logger.Debug("Order accepted",
			zap.String("evaluation_id", result.EvaluationID),
			zap.Duration("latency", latency),
		)
	}
}
