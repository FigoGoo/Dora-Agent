package modelreceipt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db, now: time.Now}
}

// Migrate creates or updates the immutable model receipt table.
func Migrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("model receipt postgres db is required")
	}
	return db.WithContext(ctx).AutoMigrate(&Receipt{})
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("model receipt postgres db is required")
	}
	return Migrate(ctx, s.db)
}

// Migrate is the store-bound alias used by applications that prefer explicit
// migration naming over AutoMigrate.
func (s *PostgresStore) Migrate(ctx context.Context) error { return s.AutoMigrate(ctx) }

func (s *PostgresStore) Get(ctx context.Context, turnID string, ordinal int) (Receipt, error) {
	if s == nil || s.db == nil {
		return Receipt{}, fmt.Errorf("model receipt postgres db is required")
	}
	key, err := normalizeSlot(turnID, ordinal)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	err = s.db.WithContext(ctx).
		Where("turn_id = ? AND ordinal = ?", key.turnID, key.ordinal).
		First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("get model receipt %s/%d: %w", key.turnID, key.ordinal, err)
	}
	receipt, err = normalize(receipt)
	if err != nil {
		return Receipt{}, fmt.Errorf("validate stored model receipt %s/%d: %w", key.turnID, key.ordinal, err)
	}
	return clone(receipt), nil
}

func (s *PostgresStore) PutOnce(ctx context.Context, receipt Receipt) (Receipt, error) {
	if s == nil || s.db == nil {
		return Receipt{}, fmt.Errorf("model receipt postgres db is required")
	}
	receipt, err := normalize(receipt)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = s.now().UTC()
	} else {
		receipt.CreatedAt = receipt.CreatedAt.UTC()
	}

	// PostgreSQL serializes contenders on the composite primary key. The
	// losing insert does not update any column; both callers then read the
	// committed first writer as the authoritative output.
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "turn_id"}, {Name: "ordinal"}},
		DoNothing: true,
	}).Create(&receipt)
	if result.Error != nil {
		return Receipt{}, fmt.Errorf("put model receipt %s/%d: %w", receipt.TurnID, receipt.Ordinal, result.Error)
	}
	authoritative, err := s.Get(ctx, receipt.TurnID, receipt.Ordinal)
	if err != nil {
		return Receipt{}, fmt.Errorf("read authoritative model receipt %s/%d: %w", receipt.TurnID, receipt.Ordinal, err)
	}
	return authoritative, nil
}

var _ Store = (*PostgresStore)(nil)
