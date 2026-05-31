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

	// Populate risk
