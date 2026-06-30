package idempotency

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
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

type RequestHashInput struct {
	TenantID    string
	SpaceID     string
	ActorUserID string
	AdminID     string
	Body        []byte
	Extra       map[string]any
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

func HashRequest(input RequestHashInput) (string, error) {
	if input.TenantID == "" {
		return "", bizerrors.New(bizerrors.CodeInvalidArgument, "tenant id is required for request hash")
	}
	if input.ActorUserID == "" && input.AdminID == "" {
		return "", bizerrors.New(bizerrors.CodeInvalidArgument, "actor user id or admin id is required for request hash")
	}
	body, err := canonicalBody(input.Body)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"tenant_id": input.TenantID,
		"body":      body,
	}
	if input.SpaceID != "" {
		payload["space_id"] = input.SpaceID
	}
	if input.ActorUserID != "" {
		payload["actor_user_id"] = input.ActorUserID
	}
	if input.AdminID != "" {
		payload["admin_id"] = input.AdminID
	}
	if len(input.Extra) > 0 {
		payload["extra"] = normalizeValue(input.Extra)
	}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalBody(body []byte) (any, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("canonicalize request body: %w", err)
	}
	return normalizeValue(value), nil
}

func canonicalJSON(value any) ([]byte, error) {
	normalized := normalizeValue(value)
	var buf bytes.Buffer
	if err := writeCanonical(&buf, normalized); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if shouldIgnoreHashField(key) {
				continue
			}
			out[key] = normalizeValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeValue(item))
		}
		return out
	default:
		return typed
	}
}

func shouldIgnoreHashField(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch normalized {
	case "request_id", "trace_id", "x_client_request_id", "client_request_id", "request_hash":
		return true
	default:
		return false
	}
}

func writeCanonical(buf *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(keyBytes)
			buf.WriteByte(':')
			if err := writeCanonical(buf, typed[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case json.Number:
		buf.WriteString(typed.String())
	default:
		valueBytes, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		buf.Write(valueBytes)
	}
	return nil
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
