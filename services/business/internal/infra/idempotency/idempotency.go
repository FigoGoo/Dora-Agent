package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"gorm.io/datatypes"
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
	ID                 string         `gorm:"column:id;primaryKey"`
	IdempotencyKey     string         `gorm:"column:idempotency_key"`
	RequestHash        string         `gorm:"column:request_hash"`
	Scope              string         `gorm:"column:scope"`
	ActorUserID        string         `gorm:"column:actor_user_id"`
	SpaceID            *string        `gorm:"column:space_id"`
	EnterpriseID       *string        `gorm:"column:enterprise_id"`
	ResourceType       *string        `gorm:"column:resource_type"`
	ResourceID         *string        `gorm:"column:resource_id"`
	Status             string         `gorm:"column:status"`
	ResponseCode       *string        `gorm:"column:response_code"`
	ResponseBodyDigest *string        `gorm:"column:response_body_digest"`
	ResponseBodyJSON   datatypes.JSON `gorm:"column:response_body_json;type:jsonb"`
	LockedUntil        *time.Time     `gorm:"column:locked_until"`
	ExpiresAt          time.Time      `gorm:"column:expires_at"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
}

func (IdempotencyRecord) TableName() string { return "idempotency_records" }

type BeginInput struct {
	Scope          string
	IdempotencyKey string
	RequestHash    string
	ActorUserID    string
	SpaceID        *string
	EnterpriseID   *string
	ResourceType   *string
	ResourceID     *string
}

type Decision struct {
	Mode   string
	Record IdempotencyRecord
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

func HashRequest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (g *IdempotencyGuard) Begin(ctx context.Context, input BeginInput) (Decision, error) {
	if input.Scope == "" || input.IdempotencyKey == "" || input.RequestHash == "" || input.ActorUserID == "" {
		return Decision{}, bizerrors.New(bizerrors.CodeInvalidArgument, "scope, idempotency key, request hash and actor user id are required")
	}

	var decision Decision
	err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var record IdempotencyRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope = ? AND idempotency_key = ?", input.Scope, input.IdempotencyKey).
			First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lockUntil := now.Add(g.lockDuration)
			record = IdempotencyRecord{
				ID:               "idem_" + randomHex(16),
				IdempotencyKey:   input.IdempotencyKey,
				RequestHash:      input.RequestHash,
				Scope:            input.Scope,
				ActorUserID:      input.ActorUserID,
				SpaceID:          input.SpaceID,
				EnterpriseID:     input.EnterpriseID,
				ResourceType:     input.ResourceType,
				ResourceID:       input.ResourceID,
				Status:           StatusProcessing,
				ResponseBodyJSON: datatypes.JSON([]byte(`{}`)),
				LockedUntil:      &lockUntil,
				ExpiresAt:        now.Add(g.ttl),
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
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
			decision = Decision{Mode: DecisionReplay, Record: record}
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

func (g *IdempotencyGuard) Succeed(ctx context.Context, id, responseCode string, responseBody []byte) error {
	body := datatypes.JSON([]byte(`{}`))
	if json.Valid(responseBody) {
		body = datatypes.JSON(responseBody)
	}
	digest := HashRequest(responseBody)
	now := time.Now().UTC()
	return g.db.WithContext(ctx).Model(&IdempotencyRecord{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":               StatusSucceeded,
			"response_code":        responseCode,
			"response_body_digest": digest,
			"response_body_json":   body,
			"locked_until":         nil,
			"updated_at":           now,
		}).Error
}

func (g *IdempotencyGuard) Fail(ctx context.Context, id, responseCode string) error {
	now := time.Now().UTC()
	return g.db.WithContext(ctx).Model(&IdempotencyRecord{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        StatusFailed,
			"response_code": responseCode,
			"locked_until":  nil,
			"updated_at":    now,
		}).Error
}

func randomHex(bytesLen int) string {
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		return hex.EncodeToString(sum[:bytesLen])
	}
	return hex.EncodeToString(data)
}
