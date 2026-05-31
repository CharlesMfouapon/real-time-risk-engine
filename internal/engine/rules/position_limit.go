package rules

import (
	"fmt"
	"sync"

	"github.com/CharlesMfouapon/real-time-risk-engine/internal/model"
)

// PositionTracker maintains real-time position exposure per account and symbol.
// Thread-safe for concurrent order evaluation.
type PositionTracker struct {
	mu sync.RWMutex

	// positions[accountID][symbol] = net position (positive = long, negative = short)
	positions map[string]map[string]int64

	// limits[accountID][symbol] = (maxLong, maxShort)
	limits map[string]map[string][2]int64
}

// NewPositionTracker creates a new position tracker with default limits.
func NewPositionTracker() *PositionTracker {
	return &PositionTracker{
		positions: make(map[string]map[string]int64),
		limits:    make(map[string]map[string][2]int64),
	}
}

// SetLimit configures the position limit for an account and symbol.
func (pt *PositionTracker) SetLimit(accountID, symbol string, maxLong, maxShort int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.limits[accountID] == nil {
		pt.limits[accountID] = make(map[string][2]int64)
	}
	pt.limits[accountID][symbol] = [2]int64{maxLong, maxShort}
}

// Evaluate checks if the proposed order would breach position limits.
// Returns nil if accepted, or a RejectReason if rejected.
func (pt *PositionTracker) Evaluate(order *model.Order) *model.RejectReason {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	maxLong, maxShort := pt.getLimits(order.AccountID, order.Symbol)

	// Calculate proposed new position
	sign := int64(1)
	if order.Side == model.SideSell {
		sign = -1
	}
	currentPos := pt.getPosition(order.AccountID, order.Symbol)
	newPosition := currentPos + (sign * order.Quantity)

	// Check long limit
	if newPosition > 0 && newPosition > maxLong {
		return &model.RejectReason{
			Code:            model.RejectCodePositionLimitExceeded,
			Message:         fmt.Sprintf("Long position limit exceeded: %d > %d for %s", newPosition, maxLong, order.Symbol),
			CurrentExposure: float64(currentPos),
			LimitValue:      float64(maxLong),
		}
	}

	// Check short limit
	if newPosition < 0 && (-newPosition) > maxShort {
		return &model.RejectReason{
			Code:            model.RejectCodePositionLimitExceeded,
			Message:         fmt.Sprintf("Short position limit exceeded: %d > %d for %s", -newPosition, maxShort, order.Symbol),
			CurrentExposure: float64(currentPos),
			LimitValue:      float64(maxShort),
		}
	}

	return nil
}

// Apply updates the position state after an order is accepted.
func (pt *PositionTracker) Apply(order *model.Order) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	sign := int64(1)
	if order.Side == model.SideSell {
		sign = -1
	}

	if pt.positions[order.AccountID] == nil {
		pt.positions[order.AccountID] = make(map[string]int64)
	}
	pt.positions[order.AccountID][order.Symbol] += sign * order.Quantity
}

// GetPosition returns the current net position.
func (pt *PositionTracker) GetPosition(accountID, symbol string) int64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.getPosition(accountID, symbol)
}

// GetPositionLimit returns the configured limit for an account and symbol.
func (pt *PositionTracker) GetPositionLimit(accountID, symbol string) (maxLong, maxShort int64, exists bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	limits, ok := pt.limits[accountID]
	if !ok {
		return 0, 0, false
	}
	limit, ok := limits[symbol]
	if !ok {
		return 0, 0, false
	}
	return limit[0], limit[1], true
}

func (pt *PositionTracker) getPosition(accountID, symbol string) int64 {
	if pt.positions[accountID] == nil {
		return 0
	}
	return pt.positions[accountID][symbol]
}

func (pt *PositionTracker) getLimits(accountID, symbol string) (maxLong, maxShort int64) {
	maxLong = 1_000_000 // Default: 1M shares long
	maxShort = 1_000_000 // Default: 1M shares short

	if pt.limits[accountID] != nil {
		if lim, ok := pt.limits[accountID][symbol]; ok {
			maxLong = lim[0]
			maxShort = lim[1]
		}
	}
	return
}
