package model

import (
	"fmt"
	"time"
)

// Side represents order direction.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrdType represents order type.
type OrdType string

const (
	OrdTypeLimit  OrdType = "LIMIT"
	OrdTypeMarket OrdType = "MARKET"
	OrdTypeStop   OrdType = "STOP"
)

// Order represents an incoming order to be risk-checked.
type Order struct {
	ClOrdID     string
	AccountID   string
	Symbol      string
	Side        Side
	Price       float64
	Quantity    int64
	OrdType     OrdType
	TimestampNs int64
}

// Validate performs basic field validation.
func (o *Order) Validate() error {
	if o.ClOrdID == "" {
		return fmt.Errorf("ClOrdID is required")
	}
	if o.AccountID == "" {
		return fmt.Errorf("AccountID is required")
	}
	if o.Symbol == "" {
		return fmt.Errorf("Symbol is required")
	}
	if o.Side != SideBuy && o.Side != SideSell {
		return fmt.Errorf("invalid Side: %s", o.Side)
	}
	if o.Quantity <= 0 {
		return fmt.Errorf("Quantity must be positive: %d", o.Quantity)
	}
	if o.Price <= 0 && o.OrdType == OrdTypeLimit {
		return fmt.Errorf("Price must be positive for limit orders")
	}
	if o.TimestampNs <= 0 {
		return fmt.Errorf("TimestampNs is required")
	}
	return nil
}

// NotionalValue returns the total notional value of this order.
func (o *Order) NotionalValue() float64 {
	return o.Price * float64(o.Quantity)
}

// String returns a FIX-style representation.
func (o *Order) String() string {
	return fmt.Sprintf("Order{ClOrdID=%s, Account=%s, %s %d %s @ %.2f}",
		o.ClOrdID, o.AccountID, o.Side, o.Quantity, o.Symbol, o.Price)
}

// Decision represents the result of risk evaluation.
type Decision int

const (
	DecisionAccepted Decision = iota
	DecisionRejected
	DecisionError
)

func (d Decision) String() string {
	switch d {
	case DecisionAccepted:
		return "ACCEPTED"
	case DecisionRejected:
		return "REJECTED"
	case DecisionError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// RejectCode categorizes rejection reasons.
type RejectCode int

const (
	RejectCodeUnspecified            RejectCode = iota
	RejectCodePositionLimitExceeded
	RejectCodeNotionalLimitExceeded
	RejectCodeFatFingerDetected
	RejectCodeCircuitBreakerOpen
	RejectCodeDuplicateOrder
	RejectCodeInvalidSymbol
	RejectCodeAccountFrozen
	RejectCodeRateLimitExceeded
)

func (c RejectCode) String() string {
	switch c {
	case RejectCodePositionLimitExceeded:
		return "POSITION_LIMIT_EXCEEDED"
	case RejectCodeNotionalLimitExceeded:
		return "NOTIONAL_LIMIT_EXCEEDED"
	case RejectCodeFatFingerDetected:
		return "FAT_FINGER_DETECTED"
	case RejectCodeCircuitBreakerOpen:
		return "CIRCUIT_BREAKER_OPEN"
	case RejectCodeDuplicateOrder:
		return "DUPLICATE_ORDER"
	case RejectCodeInvalidSymbol:
		return "INVALID_SYMBOL"
	case RejectCodeAccountFrozen:
		return "ACCOUNT_FROZEN"
	case RejectCodeRateLimitExceeded:
		return "RATE_LIMIT_EXCEEDED"
	default:
		return "UNSPECIFIED"
	}
}

// RejectReason provides detailed rejection information.
type RejectReason struct {
	Code            RejectCode
	Message         string
	CurrentExposure float64
	LimitValue      float64
}

// EvaluationResult is the complete risk evaluation output.
type EvaluationResult struct {
	Decision      Decision
	EvaluationID  string
	LatencyNs     int64
	RejectReason  *RejectReason
	RiskMetrics   map[string]float64
	EvaluatedAt   time.Time
}

// AccountLimits summarizes current limit status for an account.
type AccountLimits struct {
	AccountID              string
	PositionLimits         []PositionLimit
	NotionalLimit          *NotionalLimit
	CircuitBreakerTripped  bool
	OrdersEvaluatedToday   int64
	OrdersRejectedToday    int64
}

// PositionLimit tracks position for a single symbol.
type PositionLimit struct {
	Symbol           string
	MaxLongPosition  int64
	MaxShortPosition int64
	CurrentPosition  int64
	UtilizationPct   float64
}

// NotionalLimit tracks total notional exposure.
type NotionalLimit struct {
	MaxNotionalValue    float64
	CurrentNotionalValue float64
	UtilizationPct      float64
}
