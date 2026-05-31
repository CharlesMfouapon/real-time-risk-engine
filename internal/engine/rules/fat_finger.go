package rules

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/CharlesMfouapon/real-time-risk-engine/internal/model"
)

// FatFingerDetector identifies anomalous orders that deviate significantly
// from recent trading patterns for the same symbol.
type FatFingerDetector struct {
	mu sync.RWMutex

	// recentPrices[symbol] tracks recent trade prices for deviation checks
	recentPrices map[string][]float64

	// configurable thresholds
	priceDeviationPct float64 // Max allowed deviation from recent price (e.g., 0.10 = 10%)
	maxOrderQuantity  int64   // Absolute maximum order quantity
	maxWindowSize     int     // Number of recent prices to track

	// rate limiting: max orders per account per second
	orderTimestamps map[string][]time.Time
	maxOrdersPerSecond int
}

// NewFatFingerDetector creates a detector with sensible defaults.
func NewFatFingerDetector() *FatFingerDetector {
	return &FatFingerDetector{
		recentPrices:       make(map[string][]float64),
		priceDeviationPct:  0.15,     // 15% deviation triggers check
		maxOrderQuantity:   10_000_000, // 10M shares
		maxWindowSize:      20,
		orderTimestamps:    make(map[string][]time.Time),
		maxOrdersPerSecond: 1000,
	}
}

// Evaluate checks the order for fat-finger characteristics.
func (ff *FatFingerDetector) Evaluate(order *model.Order) *model.RejectReason {
	// Check 1: Absolute quantity cap
	if order.Quantity > ff.maxOrderQuantity {
		return &model.RejectReason{
			Code:            model.RejectCodeFatFingerDetected,
			Message:         fmt.Sprintf("Order quantity %d exceeds absolute maximum %d", order.Quantity, ff.maxOrderQuantity),
			CurrentExposure: float64(order.Quantity),
			LimitValue:      float64(ff.maxOrderQuantity),
		}
	}

	// Check 2: Price deviation from recent trading range
	if reason := ff.checkPriceDeviation(order); reason != nil {
		return reason
	}

	// Check 3: Rate limiting
	if reason := ff.checkRateLimit(order); reason != nil {
		return reason
	}

	return nil
}

func (ff *FatFingerDetector) checkPriceDeviation(order *model.Order) *model.RejectReason {
	if order.OrdType != model.OrdTypeLimit {
		return nil // Market orders don't have a limit price to check
	}

	ff.mu.RLock()
	prices, ok := ff.recentPrices[order.Symbol]
	ff.mu.RUnlock()

	if !ok || len(prices) == 0 {
		return nil // No recent prices to compare against
	}

	// Calculate median of recent prices
	median := calculateMedian(prices)
	deviation := math.Abs(order.Price-median) / median

	if deviation > ff.priceDeviationPct {
		return &model.RejectReason{
			Code:            model.RejectCodeFatFingerDetected,
			Message:         fmt.Sprintf("Price %.2f deviates %.1f%% from recent median %.2f", order.Price, deviation*100, median),
			CurrentExposure: order.Price,
			LimitValue:      median,
		}
	}

	return nil
}

func (ff *FatFingerDetector) checkRateLimit(order *model.Order) *model.RejectReason {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	now := time.Now()
	timestamps := ff.orderTimestamps[order.AccountID]

	// Prune old timestamps
	cutoff := now.Add(-time.Second)
	valid := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= ff.maxOrdersPerSecond {
		return &model.RejectReason{
			Code:            model.RejectCodeRateLimitExceeded,
			Message:         fmt.Sprintf("Rate limit exceeded: %d orders/second for account %s", ff.maxOrdersPerSecond, order.AccountID),
			CurrentExposure: float64(len(valid)),
			LimitValue:      float64(ff.maxOrdersPerSecond),
		}
	}

	valid = append(valid, now)
	ff.orderTimestamps[order.AccountID] = valid
	return nil
}

// RecordPrice adds a trade price to the recent price window.
func (ff *FatFingerDetector) RecordPrice(symbol string, price float64) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	prices := ff.recentPrices[symbol]
	prices = append(prices, price)
	if len(prices) > ff.maxWindowSize {
		prices = prices[len(prices)-ff.maxWindowSize:]
	}
	ff.recentPrices[symbol] = prices
}

func calculateMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Create a copy to avoid modifying the original
	sorted := make([]float64, len(values))
	copy(sorted, values)

	// Simple insertion sort (acceptable for small windows)
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}
