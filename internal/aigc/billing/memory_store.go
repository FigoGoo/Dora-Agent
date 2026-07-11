package billing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu           sync.Mutex
	accounts     map[string]Account
	transactions map[string]Transaction
	byKey        map[string]string
	now          func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts:     map[string]Account{},
		transactions: map[string]Transaction{},
		byKey:        map[string]string{},
		now:          time.Now,
	}
}

func (s *MemoryStore) Migrate(context.Context) error { return nil }

func (s *MemoryStore) EnsureAccount(_ context.Context, userID string, initialBalance int64) (Account, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || initialBalance < 0 {
		return Account{}, fmt.Errorf("valid user id and non-negative initial balance are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if account, ok := s.accounts[userID]; ok {
		return account, nil
	}
	now := s.now().UTC()
	account := Account{UserID: userID, Balance: initialBalance, Version: 1, CreatedAt: now, UpdatedAt: now}
	s.accounts[userID] = account
	return account, nil
}

func (s *MemoryStore) GetAccount(_ context.Context, userID string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[strings.TrimSpace(userID)]
	if !ok {
		return Account{}, ErrAccountNotFound
	}
	return account, nil
}

func (s *MemoryStore) Credit(_ context.Context, request MutationRequest) (Result, error) {
	return s.mutate(request, KindCredit)
}

func (s *MemoryStore) Charge(_ context.Context, request MutationRequest) (Result, error) {
	return s.mutate(request, KindCharge)
}

func (s *MemoryStore) Refund(_ context.Context, request MutationRequest) (Result, error) {
	return s.mutate(request, KindRefund)
}

func (s *MemoryStore) mutate(request MutationRequest, kind string) (Result, error) {
	request = normalizeMutationRequest(request)
	if err := request.validate(kind); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if transactionID, ok := s.byKey[request.IdempotencyKey]; ok {
		existing := s.transactions[transactionID]
		if err := validateIdempotentReplay(existing, kind, request); err != nil {
			return Result{}, err
		}
		return Result{Transaction: existing, Duplicate: true}, nil
	}
	account, ok := s.accounts[request.UserID]
	if !ok {
		return Result{}, ErrAccountNotFound
	}
	delta := request.Points
	if kind == KindCharge {
		delta = -request.Points
		if account.Balance < request.Points {
			return Result{}, ErrInsufficientPoints
		}
	}
	if kind == KindRefund {
		charged, ok := s.transactions[request.ReferenceID]
		if !ok || charged.Kind != KindCharge || charged.UserID != request.UserID {
			return Result{}, ErrTransactionNotFound
		}
		var refunded int64
		for _, transaction := range s.transactions {
			if transaction.Kind == KindRefund && transaction.ReferenceID == request.ReferenceID {
				refunded += transaction.Points
			}
		}
		if refunded+request.Points > charged.Points {
			return Result{}, ErrRefundExceedsCharge
		}
	}
	account.Balance += delta
	account.Version++
	account.UpdatedAt = s.now().UTC()
	transaction := newTransaction(request, kind, delta, account.Balance, account.UpdatedAt)
	s.accounts[request.UserID] = account
	s.transactions[transaction.ID] = transaction
	s.byKey[transaction.IdempotencyKey] = transaction.ID
	return Result{Transaction: transaction}, nil
}

func newTransaction(request MutationRequest, kind string, delta int64, balanceAfter int64, now time.Time) Transaction {
	return Transaction{
		ID:             strings.TrimSpace(request.TransactionID),
		UserID:         strings.TrimSpace(request.UserID),
		Kind:           kind,
		IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		ReferenceID:    strings.TrimSpace(request.ReferenceID),
		OperationID:    strings.TrimSpace(request.OperationID),
		BatchID:        strings.TrimSpace(request.BatchID),
		JobID:          strings.TrimSpace(request.JobID),
		Points:         request.Points,
		Delta:          delta,
		BalanceAfter:   balanceAfter,
		Breakdown:      cloneBreakdown(request.Breakdown),
		Metadata:       cloneMetadata(request.Metadata),
		CreatedAt:      now,
	}
}

func cloneBreakdown(value map[string]int64) map[string]int64 {
	if value == nil {
		return nil
	}
	clone := make(map[string]int64, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneMetadata(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

var _ Store = (*MemoryStore)(nil)
