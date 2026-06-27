package auditlog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AuditRecord struct {
	ID                   string         `gorm:"column:id;primaryKey"`
	TraceID              string         `gorm:"column:trace_id"`
	RequestID            string         `gorm:"column:request_id"`
	IdempotencyKey       *string        `gorm:"column:idempotency_key"`
	Source               string         `gorm:"column:source"`
	ActorUserID          *string        `gorm:"column:actor_user_id"`
	AdminID              *string        `gorm:"column:admin_id"`
	LoginIdentityType    string         `gorm:"column:login_identity_type"`
	SpaceID              *string        `gorm:"column:space_id"`
	EnterpriseID         *string        `gorm:"column:enterprise_id"`
	EnterpriseRole       *string        `gorm:"column:enterprise_role"`
	Action               string         `gorm:"column:action"`
	ResourceType         string         `gorm:"column:resource_type"`
	ResourceID           *string        `gorm:"column:resource_id"`
	Result               string         `gorm:"column:result"`
	ErrorCode            *string        `gorm:"column:error_code"`
	BeforeSnapshotDigest *string        `gorm:"column:before_snapshot_digest"`
	AfterSnapshotDigest  *string        `gorm:"column:after_snapshot_digest"`
	MetadataJSON         datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	ClientIPDigest       *string        `gorm:"column:client_ip_digest"`
	UserAgentDigest      *string        `gorm:"column:user_agent_digest"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
}

func (AuditRecord) TableName() string { return "business_audit_logs" }

type Writer interface {
	Write(ctx context.Context, record *AuditRecord) error
}

type GormWriter struct {
	db *gorm.DB
}

func NewGormWriter(db *gorm.DB) *GormWriter {
	return &GormWriter{db: db}
}

func (w *GormWriter) Write(ctx context.Context, record *AuditRecord) error {
	if record.ID == "" {
		record.ID = "audit_" + randomHex(16)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if len(record.MetadataJSON) == 0 {
		record.MetadataJSON = datatypes.JSON([]byte(`{}`))
	}
	return w.db.WithContext(ctx).Create(record).Error
}

func randomHex(bytesLen int) string {
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(data)
}
