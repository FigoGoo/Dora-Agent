package modelreceipt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type slot struct {
	turnID  string
	ordinal int
}

// MemoryStore is primarily useful for tests and single-process demos. Its
// mutex makes concurrent PutOnce calls observe the same first writer.
type MemoryStore struct {
	mu       sync.RWMutex
	receipts map[slot]Receipt
	now      func() time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{receipts: make(map[slot]Receipt), now: time.Now}
}

func (s *MemoryStore) Get(_ context.Context, turnID string, ordinal int) (Receipt, error) {
	if s == nil {
		return Receipt{}, fmt.Errorf("memory model receipt store is required")
	}
	key, err := normalizeSlot(turnID, ordinal)
	if err != nil {
		return Receipt{}, err
	}
	s.mu.RLock()
	receipt, ok := s.receipts[key]
	s.mu.RUnlock()
	if !ok {
		return Receipt{}, ErrNotFound
	}
	return clone(receipt), nil
}

func (s *MemoryStore) PutOnce(_ context.Context, receipt Receipt) (Receipt, error) {
	if s == nil {
		return Receipt{}, fmt.Errorf("memory model receipt store is required")
	}
	receipt, err := normalize(receipt)
	if err != nil {
		return Receipt{}, err
	}
	key := slot{turnID: receipt.TurnID, ordinal: receipt.Ordinal}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.receipts[key]; ok {
		return clone(existing), nil
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = s.now().UTC()
	} else {
		receipt.CreatedAt = receipt.CreatedAt.UTC()
	}
	s.receipts[key] = clone(receipt)
	return clone(receipt), nil
}

func normalizeSlot(turnID string, ordinal int) (slot, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return slot{}, fmt.Errorf("model receipt turn id is required")
	}
	if ordinal <= 0 {
		return slot{}, fmt.Errorf("model receipt ordinal must be positive")
	}
	return slot{turnID: turnID, ordinal: ordinal}, nil
}

var _ Store = (*MemoryStore)(nil)
