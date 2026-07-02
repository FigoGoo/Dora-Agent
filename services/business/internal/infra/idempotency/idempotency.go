package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusProcessing = "processing"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"

	DecisionProceed    = "proceed"
	DecisionReplay     = "replay"
	DecisionConflict   = "conflict"
	DecisionProcessing = "processing"
)

type IdempotencyRecord struct {
	ID             string     `gorm:"column:id;primaryKey"`
	TenantID       string     `gorm:"column:tenant_id"`
	SpaceID        *string    `gorm:"column:space_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	RequestHash    string     `gorm:"column:request_hash"`
	Scope          string     `gorm:"column:scope"`
	ActorUserID    string     `gorm:"column:actor_user_id"`
	EnterpriseID   *string    `gorm:"column:enterprise_id"`
	ResultRefType  *string    `gorm:"column:result_ref_type"`
	ResultRefID    *string    `gorm:"column:result_ref_id"`
	Status         string     `gorm:"column:status"`
	ErrorCode      *string    `gorm:"column:error_code"`
	LockedUntil    *time.Time `gorm:"column:locked_until"`
	ExpiresAt      time.Time  `gorm:"column:expires_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

func (IdempotencyRecord) TableName() string { return "idempotency_records" }

type BeginInput struct {
	TenantID       string
	SpaceID        string
	Scope          string
	IdempotencyKey string
	RequestHash    string
	ActorUserID    string
	EnterpriseID   *string
}

type ResultRef struct {
	Type string
	ID   string
}

type Decision struct {
	Mode         string
	Record       IdempotencyRecord
	ReplayResult *ResultRef
}

type IdempotencyGuard struct {
	db           *gorm.DB
	ttl          time.Duration
	lockDuration time.Duration
}

func NewGuard(db *gorm.DB, ttl, lockDuration time.Duration) *IdempotencyGuard {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if lockDuration <= 0 {
		lockDuration = 30 * time.Second
	}
	return &IdempotencyGuard{db: db, ttl: ttl, lockDuration: lockDuration}
}

func (g *IdempotencyGuard) Begin(ctx context.Context, input BeginInput) (Decision, error) {
	if input.TenantID == "" || input.Scope == "" || input.IdempotencyKey == "" || input.RequestHash == "" || input.ActorUserID == "" {
		return Decision{}, bizerrors.New(bizerrors.CodeInvalidArgument, "tenant id, scope, idempotency key, request hash and actor user id are required")
	}

	var decision Decision
	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var record IdempotencyRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND scope = ? AND idempotency_key = ?", input.TenantID, input.Scope, input.IdempotencyKey).
			First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lockUntil := now.Add(g.lockDuration)
			record = IdempotencyRecord{
				ID:             "idem_" + randomHex(16),
				TenantID:       input.TenantID,
				SpaceID:        optionalString(input.SpaceID),
				IdempotencyKey: input.IdempotencyKey,
				RequestHash:    input.RequestHash,
				Scope:          input.Scope,
				ActorUserID:    input.ActorUserID,
				EnterpriseID:   input.EnterpriseID,
				Status:         StatusProcessing,
				LockedUntil:    &lockUntil,
				ExpiresAt:      now.Add(g.ttl),
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			result := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "tenant_id"},
					{Name: "scope"},
					{Name: "idempotency_key"},
				},
				DoNothing: true,
			}).Create(&record)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return g.resolveExistingDecision(tx, input, now, &decision)
			}
			decision = Decision{Mode: DecisionProceed, Record: record}
			return nil
		}
		if err != nil {
			return err
		}
		if record.RequestHash != input.RequestHash {
			decision = Decision{Mode: DecisionConflict, Record: record}
			return nil
		}
		if record.Status == StatusSucceeded {
			decision = Decision{Mode: DecisionReplay, Record: record, ReplayResult: record.ResultRef()}
			return nil
		}
		if record.Status == StatusProcessing && record.LockedUntil != nil && record.LockedUntil.After(now) {
			decision = Decision{Mode: DecisionProcessing, Record: record}
			return nil
		}
		lockUntil := now.Add(g.lockDuration)
		if err := tx.Model(&IdempotencyRecord{}).
			Where("id = ?", record.ID).
			Updates(map[string]any{
				"status":       StatusProcessing,
				"locked_until": lockUntil,
				"updated_at":   now,
			}).Error; err != nil {
			return err
		}
		record.Status = StatusProcessing
		record.LockedUntil = &lockUntil
		record.UpdatedAt = now
		decision = Decision{Mode: DecisionProceed, Record: record}
		return nil
	})
	return decision, err
}

func (g *IdempotencyGuard) resolveExistingDecision(tx *gorm.DB, input BeginInput, now time.Time, decision *Decision) error {
	var record IdempotencyRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND scope = ? AND idempotency_key = ?", input.TenantID, input.Scope, input.IdempotencyKey).
		First(&record).Error; err != nil {
		return err
	}
	if record.RequestHash != input.RequestHash {
		*decision = Decision{Mode: DecisionConflict, Record: record}
		return nil
	}
	if record.Status == StatusSucceeded {
		*decision = Decision{Mode: DecisionReplay, Record: record, ReplayResult: record.ResultRef()}
		return nil
	}
	if record.Status == StatusProcessing && record.LockedUntil != nil && record.LockedUntil.After(now) {
		*decision = Decision{Mode: DecisionProcessing, Record: record}
		return nil
	}
	lockUntil := now.Add(g.lockDuration)
	if err := tx.Model(&IdempotencyRecord{}).
		Where("id = ?", record.ID).
		Updates(map[string]any{
			"status":       StatusProcessing,
			"locked_until": lockUntil,
			"updated_at":   now,
		}).Error; err != nil {
		return err
	}
	record.Status = StatusProcessing
	record.LockedUntil = &lockUntil
	record.UpdatedAt = now
	*decision = Decision{Mode: DecisionProceed, Record: record}
	return nil
}

func (g *IdempotencyGuard) Succeed(ctx context.Context, id string, result ResultRef) error {
	now := time.Now().UTC()
	return g.db.WithContext(ctx).Model(&IdempotencyRecord{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":          StatusSucceeded,
			"result_ref_type": optionalString(result.Type),
			"result_ref_id":   optionalString(result.ID),
			"error_code":      nil,
			"locked_until":    nil,
			"updated_at":      now,
		}).Error
}

func (g *IdempotencyGuard) Fail(ctx context.Context, id, responseCode string) error {
	now := time.Now().UTC()
	return g.db.WithContext(ctx).Model(&IdempotencyRecord{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":       StatusFailed,
			"error_code":   responseCode,
			"locked_until": nil,
			"updated_at":   now,
		}).Error
}

func (r IdempotencyRecord) ResultRef() *ResultRef {
	if r.ResultRefType == nil && r.ResultRefID == nil {
		return nil
	}
	result := &ResultRef{}
	if r.ResultRefType != nil {
		result.Type = *r.ResultRefType
	}
	if r.ResultRefID != nil {
		result.ID = *r.ResultRefID
	}
	return result
}

func randomHex(bytesLen int) string {
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		return hex.EncodeToString(sum[:bytesLen])
	}
	return hex.EncodeToString(data)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
