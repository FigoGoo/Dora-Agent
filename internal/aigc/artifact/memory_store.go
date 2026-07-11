package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.Mutex
	now      func() time.Time
	byID     map[string]Revision
	byKey    map[string]string
	receipts map[string]ReviewCommandReceipt
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{now: time.Now, byID: map[string]Revision{}, byKey: map[string]string{}, receipts: map[string]ReviewCommandReceipt{}}
}

func (s *MemoryStore) CreateRevision(_ context.Context, revision Revision) (CreateResult, error) {
	revision = normalizeCreateRequest(revision)
	if err := revision.Validate(); err != nil {
		return CreateResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byKey[revision.IdempotencyKey]; ok {
		existing := clone(s.byID[id])
		if !sameCreateRequest(existing, revision) {
			return CreateResult{}, fmt.Errorf("%w: idempotency_key=%s", ErrIdempotencyConflict, revision.IdempotencyKey)
		}
		return CreateResult{Revision: existing}, nil
	}
	if _, ok := s.byID[revision.ID]; ok {
		return CreateResult{}, fmt.Errorf("artifact id already exists: %s", revision.ID)
	}
	if revision.Version == 0 {
		revision.Version = s.nextVersionLocked(revision.SessionID, revision.Kind)
	}
	now := s.now().UTC()
	revision.CreatedAt, revision.UpdatedAt = now, now
	s.byID[revision.ID] = clone(revision)
	s.byKey[revision.IdempotencyKey] = revision.ID
	return CreateResult{Revision: clone(revision), Created: true}, nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return Revision{}, ErrNotFound
	}
	return clone(revision), nil
}

func (s *MemoryStore) GetByIdempotencyKey(_ context.Context, key string) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byKey[strings.TrimSpace(key)]
	if !ok {
		return Revision{}, ErrNotFound
	}
	return clone(s.byID[id]), nil
}

func (s *MemoryStore) GetLatest(_ context.Context, sessionID, kind string) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Revision, 0)
	for _, revision := range s.byID {
		if revision.SessionID == strings.TrimSpace(sessionID) && revision.Kind == strings.TrimSpace(kind) {
			items = append(items, revision)
		}
	}
	if len(items) == 0 {
		return Revision{}, ErrNotFound
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version > items[j].Version })
	return clone(items[0]), nil
}

func (s *MemoryStore) ApplyReview(_ context.Context, command ReviewCommand) (ReviewResult, error) {
	command = command.normalize()
	if err := command.Validate(); err != nil {
		return ReviewResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if receipt, ok := s.receipts[command.IdempotencyKey]; ok {
		if !sameReviewCommand(receipt, command) {
			return ReviewResult{}, fmt.Errorf("%w: artifact review command key=%s", ErrIdempotencyConflict, command.IdempotencyKey)
		}
		receipt = cloneReceipt(receipt)
		return ReviewResult{Revision: clone(receipt.Result), Receipt: receipt}, nil
	}
	revision, ok := s.byID[command.ArtifactID]
	if !ok {
		return ReviewResult{}, ErrNotFound
	}
	if revision.SessionID != command.SessionID || revision.Kind != command.ArtifactKind || revision.Version != command.ArtifactVersion {
		return ReviewResult{}, fmt.Errorf("%w: artifact review target changed", ErrIdempotencyConflict)
	}
	if revision.Status != command.ExpectedStatus {
		return ReviewResult{}, fmt.Errorf("%w: status=%s", ErrNotReviewable, revision.Status)
	}
	if command.RequireLatest {
		latestVersion := 0
		latestID := ""
		for _, candidate := range s.byID {
			if candidate.SessionID == command.SessionID && candidate.Kind == command.ArtifactKind && candidate.Version > latestVersion {
				latestVersion, latestID = candidate.Version, candidate.ID
			}
		}
		if latestVersion != command.ArtifactVersion || latestID != command.ArtifactID {
			return ReviewResult{}, fmt.Errorf("%w: latest=%s/v%d target=%s/v%d", ErrStale, latestID, latestVersion, command.ArtifactID, command.ArtifactVersion)
		}
	}
	now := s.now().UTC()
	if command.Decision == ReviewDecisionApprove {
		for key, candidate := range s.byID {
			if candidate.SessionID == revision.SessionID && candidate.Kind == revision.Kind && candidate.Status == StatusActive && candidate.ID != revision.ID {
				candidate.Status, candidate.UpdatedAt = StatusSuperseded, now
				s.byID[key] = candidate
			}
		}
		revision.Status = StatusActive
		revision.ActivatedAt = &now
	} else {
		revision.Status = StatusRejected
	}
	revision.UpdatedAt = now
	s.byID[revision.ID] = revision
	receipt := ReviewCommandReceipt{
		IdempotencyKey: command.IdempotencyKey, SessionID: revision.SessionID, ArtifactID: revision.ID, ArtifactKind: revision.Kind,
		ArtifactVersion: revision.Version, ExpectedStatus: command.ExpectedStatus, Decision: command.Decision,
		RequireLatest: command.RequireLatest, Result: clone(revision), CreatedAt: now,
	}
	s.receipts[receipt.IdempotencyKey] = cloneReceipt(receipt)
	return ReviewResult{Revision: clone(revision), Receipt: cloneReceipt(receipt), Applied: true}, nil
}

func (s *MemoryStore) GetReviewReceipt(_ context.Context, idempotencyKey string) (ReviewCommandReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, ok := s.receipts[strings.TrimSpace(idempotencyKey)]
	if !ok {
		return ReviewCommandReceipt{}, ErrNotFound
	}
	return cloneReceipt(receipt), nil
}

func (s *MemoryStore) Activate(_ context.Context, id string, expectedVersion int) (Revision, error) {
	return s.transition(id, expectedVersion, StatusActive)
}

func (s *MemoryStore) Reject(_ context.Context, id string, expectedVersion int) (Revision, error) {
	return s.transition(id, expectedVersion, StatusRejected)
}

func (s *MemoryStore) transition(id string, expectedVersion int, status string) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return Revision{}, ErrNotFound
	}
	if expectedVersion > 0 && revision.Version != expectedVersion {
		return Revision{}, fmt.Errorf("artifact version conflict: current=%d expected=%d", revision.Version, expectedVersion)
	}
	now := s.now().UTC()
	if status == StatusActive {
		for key, candidate := range s.byID {
			if candidate.SessionID == revision.SessionID && candidate.Kind == revision.Kind && candidate.Status == StatusActive && candidate.ID != revision.ID {
				candidate.Status, candidate.UpdatedAt = StatusSuperseded, now
				s.byID[key] = candidate
			}
		}
		revision.ActivatedAt = &now
	}
	revision.Status, revision.UpdatedAt = status, now
	s.byID[revision.ID] = revision
	return clone(revision), nil
}

func (s *MemoryStore) nextVersionLocked(sessionID, kind string) int {
	version := 0
	for _, revision := range s.byID {
		if revision.SessionID == sessionID && revision.Kind == kind && revision.Version > version {
			version = revision.Version
		}
	}
	return version + 1
}

func clone(revision Revision) Revision {
	raw, _ := json.Marshal(revision)
	_ = json.Unmarshal(raw, &revision)
	return revision
}

func cloneReceipt(receipt ReviewCommandReceipt) ReviewCommandReceipt {
	raw, _ := json.Marshal(receipt)
	_ = json.Unmarshal(raw, &receipt)
	return receipt
}

var _ Store = (*MemoryStore)(nil)
