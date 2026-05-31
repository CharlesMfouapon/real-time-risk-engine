package rules

import (
	"fmt"
	"sync"

	"github.com/CharlesMfouapon/real-time-risk-engine/internal/model"
)

// NotionalTracker monitors total notional exposure per account.
// Prevents over-concentration of risk in a single account.
type NotionalTracker struct {
	mu sync.RWMutex

	// currentNotional[accountID] = total notional value of open positions
	currentNotional map[string]float64

	// limits[accountID] = maximum notional value allowed
	limits map[string]float64
}

// NewNotionalTracker creates a new notional value tracker.
func NewNotionalTracker() *NotionalTracker {
	return &NotionalTracker{
		currentNotional: make(map[string]float64),
		limits:          make(map[string]float64),
	}
}

// SetLimit configures the maximum notional value for an account.
func (nt *NotionalTracker) SetLimit(accountID string, maxNotional float64) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	nt.limits[accountID] = maxNotional
}

// Evaluate checks if the proposed order would breach notional limits.
func (nt *NotionalTracker) Evaluate(order *model.Order) *model.RejectReason {
	nt.mu.RLock()
	defer nt.mu.RUnlock()

	limit := nt.getLimit(order.AccountID)
	if limit <= 0 {
		return nil // No limit configured
	}

	current := nt.currentNotional[order.AccountID]
	orderValue := order.NotionalValue()
	newNotional := current + orderValue

	if newNotional > limit {
		return &model.RejectReason{
			Code:            model.RejectCodeNotionalLimitExceeded,
			Message:         fmt.Sprintf("Notional limit exceeded: %.2f > %.2f", newNotional, limit),
			CurrentExposure: current,
			LimitValue:      limit,
		}
	}

	return nil
}

// Apply updates notional exposure after an accepted order.
func (nt *NotionalTracker) Apply(order *model.Order) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	nt.currentNotional[order.AccountID] += order.NotionalValue()
}

func (nt *NotionalTracker) getLimit(accountID string) float64 {
	if limit, ok := nt.limits[accountID]; ok {
		return limit
	}
	return 100_000_000.00 // Default: $100M notional
}

// GetCurrentNotional returns current notional exposure for an account.
func (nt *NotionalTracker) GetCurrentNotional(accountID string) float64 {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	return nt.currentNotional[accountID]
}
