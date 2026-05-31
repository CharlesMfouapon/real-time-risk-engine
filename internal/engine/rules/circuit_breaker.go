package rules

import (
	"fmt"
	"sync"
	"time"

	"github.com/CharlesMfouapon/real-time-risk-engine/internal/model"
)

// CircuitState represents the breaker's current state.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Trading halted
	CircuitHalfOpen                     // Testing if safe to close
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker halts trading for an account when loss thresholds are exceeded.
// Implements the standard three-state circuit breaker pattern.
type CircuitBreaker struct {
	mu sync.RWMutex

	// state[accountID] = current circuit state
	states map[string]CircuitState

	// losses[accountID] = cumulative realized losses
	losses map[string]float64

	// Configuration
	maxLossPerAccount float64       // Maximum loss before tripping
	maxOpenDuration   time.Duration // How long the circuit stays open
	openSince         map[string]time.Time
}

// NewCircuitBreaker creates a breaker with default thresholds.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		states:            make(map[string]CircuitState),
		losses:            make(map[string]float64),
		maxLossPerAccount: 1_000_000.00, // $1M max loss
		maxOpenDuration:   5 * time.Minute,
		openSince:         make(map[string]time.Time),
	}
}

// Evaluate checks if the circuit breaker allows trading for this account.
func (cb *CircuitBreaker) Evaluate(order *model.Order) *model.RejectReason {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state := cb.states[order.AccountID] // Defaults to CircuitClosed (0)

	switch state {
	case CircuitClosed:
		return nil // Allow trading

	case CircuitOpen:
		// Check if cool-down period has elapsed
		if openTime, ok := cb.openSince[order.AccountID]; ok {
			if time.Since(openTime) > cb.maxOpenDuration {
				// Transition to half-open on next evaluation
				return nil // Will be handled by the caller
			}
		}
		return &model.RejectReason{
			Code:            model.RejectCodeCircuitBreakerOpen,
			Message:         fmt.Sprintf("Circuit breaker open for account %s", order.AccountID),
			CurrentExposure: cb.losses[order.AccountID],
			LimitValue:      cb.maxLossPerAccount,
		}

	case CircuitHalfOpen:
		return nil // Allow limited testing

	default:
		return nil
	}
}

// RecordLoss accumulates losses and may trip the breaker.
func (cb *CircuitBreaker) RecordLoss(accountID string, lossAmount float64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.losses[accountID] += lossAmount

	if cb.losses[accountID] >= cb.maxLossPerAccount {
		cb.states[accountID] = CircuitOpen
		cb.openSince[accountID] = time.Now()
	}
}

// Trip manually opens the circuit breaker for an account.
func (cb *CircuitBreaker) Trip(accountID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.states[accountID] = CircuitOpen
	cb.openSince[accountID] = time.Now()
}

// Reset manually closes the circuit breaker.
func (cb *CircuitBreaker) Reset(accountID string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.states[accountID] != CircuitOpen && cb.states[accountID] != CircuitHalfOpen {
		return fmt.Errorf("circuit breaker for %s is not open", accountID)
	}

	cb.states[accountID] = CircuitClosed
	cb.losses[accountID] = 0
	delete(cb.openSince, accountID)
	return nil
}

// IsTripped returns whether the circuit is open for an account.
func (cb *CircuitBreaker) IsTripped(accountID string) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.states[accountID] == CircuitOpen
}
