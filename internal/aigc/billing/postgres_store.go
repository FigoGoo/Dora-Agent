package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing postgres db is required")
	}
	return s.db.WithContext(ctx).AutoMigrate(&Account{}, &Transaction{})
}

func (s *PostgresStore) EnsureAccount(ctx context.Context, userID string, initialBalance int64) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, fmt.Errorf("billing postgres db is required")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || initialBalance < 0 {
		return Account{}, fmt.Errorf("valid user id and non-negative initial balance are required")
	}
	now := s.now().UTC()
	account := Account{UserID: userID, Balance: initialBalance, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return Account{}, fmt.Errorf("ensure billing account: %w", err)
	}
	return s.GetAccount(ctx, userID)
}

func (s *PostgresStore) GetAccount(ctx context.Context, userID string) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, fmt.Errorf("billing postgres db is required")
	}
	var account Account
	if err := s.db.WithContext(ctx).First(&account, "user_id = ?", strings.TrimSpace(userID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, fmt.Errorf("get billing account: %w", err)
	}
	return account, nil
}

func (s *PostgresStore) Credit(ctx context.Context, request MutationRequest) (Result, error) {
	return s.mutate(ctx, request, KindCredit)
}

func (s *PostgresStore) Charge(ctx context.Context, request MutationRequest) (Result, error) {
	return s.mutate(ctx, request, KindCharge)
}

func (s *PostgresStore) Refund(ctx context.Context, request MutationRequest) (Result, error) {
	return s.mutate(ctx, request, KindRefund)
}

func (s *PostgresStore) mutate(ctx context.Context, request MutationRequest, kind string) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, fmt.Errorf("billing postgres db is required")
	}
	request = normalizeMutationRequest(request)
	if err := request.validate(kind); err != nil {
		return Result{}, err
	}
	var result Result
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		replay, found, err := findIdempotentReplay(tx, request, kind)
		if err != nil {
			return err
		}
		if found {
			result = replay
			return nil
		}

		var account Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "user_id = ?", request.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAccountNotFound
			}
			return err
		}
		delta := request.Points
		if kind == KindCharge {
			delta = -request.Points
			if account.Balance < request.Points {
				return ErrInsufficientPoints
			}
		}
		if kind == KindRefund {
			var charged Transaction
			if err := tx.First(&charged, "id = ? AND kind = ? AND user_id = ?", request.ReferenceID, KindCharge, request.UserID).Error; err != nil {
				return ErrTransactionNotFound
			}
			var refunded int64
			if err := tx.Model(&Transaction{}).
				Where("reference_id = ? AND kind = ?", request.ReferenceID, KindRefund).
				Select("COALESCE(SUM(points), 0)").Scan(&refunded).Error; err != nil {
				return err
			}
			if refunded+request.Points > charged.Points {
				return ErrRefundExceedsCharge
			}
		}

		account.Balance += delta
		account.Version++
		account.UpdatedAt = s.now().UTC()
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		transaction := newTransaction(request, kind, delta, account.Balance, account.UpdatedAt)
		if err := tx.Create(&transaction).Error; err != nil {
			return err
		}
		result = Result{Transaction: transaction}
		return nil
	})
	if err != nil {
		// A concurrent transaction can claim the unique idempotency key after
		// this transaction's initial read. PostgreSQL then aborts this
		// transaction at INSERT; resolve the winner outside the aborted
		// transaction and apply the same immutable replay validation.
		replay, found, replayErr := findIdempotentReplay(s.db.WithContext(ctx), request, kind)
		if found {
			if replayErr != nil {
				return Result{}, replayErr
			}
			return replay, nil
		}
		return Result{}, fmt.Errorf("%s points: %w", kind, err)
	}
	return result, nil
}

func findIdempotentReplay(db *gorm.DB, request MutationRequest, kind string) (Result, bool, error) {
	var existing Transaction
	if err := db.First(&existing, "idempotency_key = ?", request.IdempotencyKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	if err := validateIdempotentReplay(existing, kind, request); err != nil {
		return Result{}, true, err
	}
	return Result{Transaction: existing, Duplicate: true}, true, nil
}

var _ Store = (*PostgresStore)(nil)
