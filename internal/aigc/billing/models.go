package billing

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAccountNotFound     = errors.New("billing account not found")
	ErrTransactionNotFound = errors.New("billing transaction not found")
	ErrIdempotencyConflict = errors.New("billing idempotency conflict")
	ErrInsufficientPoints  = errors.New("insufficient points")
	ErrRefundExceedsCharge = errors.New("refund exceeds charged points")
)

const (
	KindCredit = "credit"
	KindCharge = "charge"
	KindRefund = "refund"
)

type Account struct {
	UserID    string    `json:"user_id" gorm:"primaryKey;size:128"`
	Balance   int64     `json:"balance" gorm:"not null"`
	Version   int64     `json:"version" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Account) TableName() string { return "aigc_point_accounts" }

type Transaction struct {
	ID             string           `json:"id" gorm:"primaryKey;size:160"`
	UserID         string           `json:"user_id" gorm:"size:128;index"`
	Kind           string           `json:"kind" gorm:"size:32;index"`
	IdempotencyKey string           `json:"idempotency_key" gorm:"size:256;uniqueIndex"`
	ReferenceID    string           `json:"reference_id,omitempty" gorm:"size:160;index"`
	OperationID    string           `json:"operation_id,omitempty" gorm:"size:128;index"`
	BatchID        string           `json:"batch_id,omitempty" gorm:"size:128;index"`
	JobID          string           `json:"job_id,omitempty" gorm:"size:128;index"`
	Points         int64            `json:"points"`
	Delta          int64            `json:"delta"`
	BalanceAfter   int64            `json:"balance_after"`
	Breakdown      map[string]int64 `json:"breakdown,omitempty" gorm:"type:jsonb;serializer:json"`
	Metadata       map[string]any   `json:"metadata,omitempty" gorm:"type:jsonb;serializer:json"`
	CreatedAt      time.Time        `json:"created_at"`
}

func (Transaction) TableName() string { return "aigc_point_transactions" }

type MutationRequest struct {
	TransactionID  string
	UserID         string
	IdempotencyKey string
	ReferenceID    string
	OperationID    string
	BatchID        string
	JobID          string
	Points         int64
	Breakdown      map[string]int64
	Metadata       map[string]any
}

type Result struct {
	Transaction Transaction `json:"transaction"`
	Duplicate   bool        `json:"duplicate"`
}

func (r MutationRequest) validate(kind string) error {
	if strings.TrimSpace(r.TransactionID) == "" {
		return fmt.Errorf("billing transaction id is required")
	}
	if strings.TrimSpace(r.UserID) == "" {
		return fmt.Errorf("billing user id is required")
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("billing idempotency key is required")
	}
	if r.Points <= 0 {
		return fmt.Errorf("billing points must be positive")
	}
	if kind == KindRefund && strings.TrimSpace(r.ReferenceID) == "" {
		return fmt.Errorf("refund reference transaction id is required")
	}
	return nil
}

func normalizeMutationRequest(request MutationRequest) MutationRequest {
	request.TransactionID = strings.TrimSpace(request.TransactionID)
	request.UserID = strings.TrimSpace(request.UserID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.ReferenceID = strings.TrimSpace(request.ReferenceID)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.BatchID = strings.TrimSpace(request.BatchID)
	request.JobID = strings.TrimSpace(request.JobID)
	request.Breakdown = cloneBreakdown(request.Breakdown)
	request.Metadata = cloneMetadata(request.Metadata)
	return request
}

// validateIdempotentReplay makes an idempotency key an immutable binding to
// the billing mutation that first claimed it. TransactionID and Metadata are
// deliberately excluded: they are storage/audit details rather than the
// monetary operation identified by the key.
func validateIdempotentReplay(existing Transaction, kind string, request MutationRequest) error {
	request = normalizeMutationRequest(request)
	field := ""
	switch {
	case strings.TrimSpace(existing.Kind) != strings.TrimSpace(kind):
		field = "kind"
	case strings.TrimSpace(existing.UserID) != request.UserID:
		field = "user_id"
	case existing.Points != request.Points:
		field = "points"
	case strings.TrimSpace(existing.ReferenceID) != request.ReferenceID:
		field = "reference_id"
	case strings.TrimSpace(existing.OperationID) != request.OperationID:
		field = "operation_id"
	case strings.TrimSpace(existing.BatchID) != request.BatchID:
		field = "batch_id"
	case strings.TrimSpace(existing.JobID) != request.JobID:
		field = "job_id"
	case !equalBreakdown(existing.Breakdown, request.Breakdown):
		field = "breakdown"
	}
	if field == "" {
		return nil
	}
	return fmt.Errorf("%w: idempotency key %q is already bound to a mutation with different %s", ErrIdempotencyConflict, request.IdempotencyKey, field)
}

func equalBreakdown(left, right map[string]int64) bool {
	// Treat nil and an empty object as the same canonical breakdown.
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if other, ok := right[key]; !ok || other != value {
			return false
		}
	}
	return true
}
