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
	AuditID         string         `gorm:"column:audit_id;primaryKey"`
	TraceID         string         `gorm:"column:trace_id"`
	OperatorType    string         `gorm:"column:operator_type"`
	OperatorID      *string        `gorm:"column:operator_id"`
	TenantID        string         `gorm:"column:tenant_id"`
	SpaceID         *string        `gorm:"column:space_id"`
	BusinessAction  string         `gorm:"column:business_action"`
	ResourceType    string         `gorm:"column:resource_type"`
	ResourceID      *string        `gorm:"column:resource_id"`
	BeforeStatus    *string        `gorm:"column:before_status"`
	AfterStatus     *string        `gorm:"column:after_status"`
	Reason          *string        `gorm:"column:reason"`
	Result          string         `gorm:"column:result"`
	ErrorCode       *string        `gorm:"column:error_code"`
	MetadataSummary datatypes.JSON `gorm:"column:metadata_summary;type:jsonb"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
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
	if record.AuditID == "" {
		record.AuditID = "audit_" + randomHex(16)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if len(record.MetadataSummary) == 0 {
		record.MetadataSummary = datatypes.JSON([]byte(`{}`))
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
