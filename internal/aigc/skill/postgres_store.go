package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrSkillNotFound = errors.New("skill not found")

type SkillRecord struct {
	ID          string    `json:"id" gorm:"primaryKey;size:128"`
	Name        string    `json:"name" gorm:"size:256;index"`
	Description string    `json:"description,omitempty"`
	Version     string    `json:"version,omitempty" gorm:"size:64"`
	Content     string    `json:"content"`
	Enabled     bool      `json:"enabled" gorm:"index"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (SkillRecord) TableName() string {
	return "aigc_skills"
}

type PostgresSkillStore struct {
	db *gorm.DB
}

func NewPostgresSkillStore(db *gorm.DB) *PostgresSkillStore {
	return &PostgresSkillStore{db: db}
}

func (s *PostgresSkillStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres skill store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&SkillRecord{}); err != nil {
		return fmt.Errorf("migrate skill table: %w", err)
	}
	return nil
}

func (s *PostgresSkillStore) Save(ctx context.Context, record SkillRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres skill store db is required")
	}
	record.ID = strings.TrimSpace(record.ID)
	record.Content = strings.TrimSpace(record.Content)
	if record.ID == "" {
		return fmt.Errorf("skill id is required")
	}
	if record.Content == "" {
		return fmt.Errorf("skill content is required")
	}
	if record.Name == "" || record.Description == "" {
		plan, err := ParseSkill(record.Content)
		if err != nil {
			return fmt.Errorf("parse skill content: %w", err)
		}
		if record.Name == "" {
			record.Name = plan.Name
		}
		if record.Description == "" {
			record.Description = plan.Description
		}
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&record).Error; err != nil {
		return fmt.Errorf("save skill %s: %w", record.ID, err)
	}
	return nil
}

func (s *PostgresSkillStore) Get(ctx context.Context, skillID string) (SkillRecord, error) {
	if s == nil || s.db == nil {
		return SkillRecord{}, fmt.Errorf("postgres skill store db is required")
	}
	var record SkillRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", skillID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SkillRecord{}, fmt.Errorf("%w: %s", ErrSkillNotFound, skillID)
		}
		return SkillRecord{}, fmt.Errorf("get skill %s: %w", skillID, err)
	}
	return record, nil
}

func (s *PostgresSkillStore) ListEnabled(ctx context.Context) ([]SkillRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres skill store db is required")
	}
	var records []SkillRecord
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("updated_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list enabled skills: %w", err)
	}
	return records, nil
}

func (s *PostgresSkillStore) GetEnabledByName(ctx context.Context, name string) (SkillRecord, error) {
	if s == nil || s.db == nil {
		return SkillRecord{}, fmt.Errorf("postgres skill store db is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return SkillRecord{}, fmt.Errorf("skill name is required")
	}
	var record SkillRecord
	err := s.db.WithContext(ctx).
		Where("enabled = ? AND (name = ? OR id = ?)", true, name, name).
		First(&record).Error
	if err != nil {
		return SkillRecord{}, fmt.Errorf("%w: %s", ErrSkillNotFound, name)
	}
	return record, nil
}

func (s *PostgresSkillStore) LoadPlan(ctx context.Context, skillID string) (*SkillPlan, error) {
	record, err := s.Get(ctx, skillID)
	if err != nil {
		return nil, err
	}
	if !record.Enabled {
		return nil, fmt.Errorf("skill %s is disabled", skillID)
	}
	plan, err := ParseSkill(record.Content)
	if err != nil {
		return nil, fmt.Errorf("parse skill %s: %w", skillID, err)
	}
	plan.SkillID = record.ID
	return plan, nil
}
