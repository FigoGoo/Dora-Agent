package storyboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
)

var (
	ErrNotFound        = errors.New("storyboard not found")
	ErrVersionConflict = errors.New("storyboard version conflict")
	ErrInvalidPatch    = errors.New("invalid storyboard patch")
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

type storyboardRecord struct {
	ID          string `gorm:"primaryKey;size:128"`
	SessionID   string `gorm:"size:128;index"`
	SpecID      string `gorm:"size:128;index"`
	Version     int    `gorm:"index"`
	Status      string `gorm:"size:64;index"`
	KeyElements []byte `gorm:"type:jsonb"`
	Shots       []byte `gorm:"type:jsonb"`
	AudioLayers []byte `gorm:"type:jsonb"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (storyboardRecord) TableName() string {
	return "aigc_storyboards"
}

type storyboardEventRecord struct {
	ID           string `gorm:"primaryKey;size:128"`
	SessionID    string `gorm:"size:128;index"`
	StoryboardID string `gorm:"size:128;index"`
	BaseVersion  int
	NextVersion  int
	Source       string `gorm:"size:64;index"`
	ToolCallID   string `gorm:"size:128"`
	Ops          []byte `gorm:"type:jsonb"`
	CreatedAt    time.Time
}

func (storyboardEventRecord) TableName() string {
	return "aigc_storyboard_events"
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres storyboard store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(&storyboardRecord{}, &storyboardEventRecord{}); err != nil {
		return fmt.Errorf("migrate storyboard tables: %w", err)
	}
	return nil
}

func (s *PostgresStore) SaveSnapshot(ctx context.Context, board Storyboard) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres storyboard store db is required")
	}
	record, err := boardToRecord(board)
	if err != nil {
		return err
	}
	if record.Version <= 0 {
		record.Version = 1
	}
	if record.Status == "" {
		record.Status = StatusDraft
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := s.db.WithContext(ctx).Save(&record).Error; err != nil {
		return fmt.Errorf("save storyboard %s: %w", record.ID, err)
	}
	return nil
}

func (s *PostgresStore) Get(ctx context.Context, storyboardID string) (Storyboard, error) {
	if s == nil || s.db == nil {
		return Storyboard{}, fmt.Errorf("postgres storyboard store db is required")
	}
	var record storyboardRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", storyboardID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Storyboard{}, fmt.Errorf("%w: %s", ErrNotFound, storyboardID)
		}
		return Storyboard{}, fmt.Errorf("get storyboard %s: %w", storyboardID, err)
	}
	return recordToBoard(record)
}

func (s *PostgresStore) GetLatestBySession(ctx context.Context, sessionID string) (Storyboard, error) {
	if s == nil || s.db == nil {
		return Storyboard{}, fmt.Errorf("postgres storyboard store db is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Storyboard{}, fmt.Errorf("session id is required")
	}

	var record storyboardRecord
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("updated_at DESC").
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Storyboard{}, fmt.Errorf("%w: session=%s", ErrNotFound, sessionID)
		}
		return Storyboard{}, fmt.Errorf("get latest storyboard for session %s: %w", sessionID, err)
	}
	return recordToBoard(record)
}

func (s *PostgresStore) ApplyPatch(ctx context.Context, req PatchRequest) (Storyboard, EventRecord, error) {
	if s == nil || s.db == nil {
		return Storyboard{}, EventRecord{}, fmt.Errorf("postgres storyboard store db is required")
	}
	if strings.TrimSpace(req.StoryboardID) == "" {
		return Storyboard{}, EventRecord{}, fmt.Errorf("%w: storyboard id is required", ErrInvalidPatch)
	}
	if len(req.Ops) == 0 {
		return Storyboard{}, EventRecord{}, fmt.Errorf("%w: ops are required", ErrInvalidPatch)
	}

	var patched Storyboard
	var event EventRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record storyboardRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&record, "id = ?", req.StoryboardID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrNotFound, req.StoryboardID)
			}
			return fmt.Errorf("get storyboard %s for patch: %w", req.StoryboardID, err)
		}
		if record.Version != req.BaseVersion {
			return fmt.Errorf("%w: current=%d expected=%d", ErrVersionConflict, record.Version, req.BaseVersion)
		}

		board, err := recordToBoard(record)
		if err != nil {
			return err
		}
		board, err = applyJSONPatch(board, req.Ops)
		if err != nil {
			return err
		}
		board.Version = req.BaseVersion + 1
		board.UpdatedAt = time.Now()

		nextRecord, err := boardToRecord(board)
		if err != nil {
			return err
		}
		if err := tx.Save(&nextRecord).Error; err != nil {
			return fmt.Errorf("save patched storyboard %s: %w", req.StoryboardID, err)
		}

		event = EventRecord{
			ID:           valueOrDefault(req.EventID, fmt.Sprintf("%s-%d", req.StoryboardID, board.Version)),
			SessionID:    valueOrDefault(req.SessionID, record.SessionID),
			StoryboardID: req.StoryboardID,
			BaseVersion:  req.BaseVersion,
			NextVersion:  board.Version,
			Source:       valueOrDefault(req.Source, "unknown"),
			ToolCallID:   req.ToolCallID,
			Ops:          append([]patch.JSONPatchOp(nil), req.Ops...),
			CreatedAt:    time.Now(),
		}
		eventRecord, err := eventToRecord(event)
		if err != nil {
			return err
		}
		if err := tx.Create(&eventRecord).Error; err != nil {
			return fmt.Errorf("create storyboard event %s: %w", event.ID, err)
		}
		patched = board
		return nil
	})
	if err != nil {
		return Storyboard{}, EventRecord{}, err
	}
	return patched, event, nil
}

func boardToRecord(board Storyboard) (storyboardRecord, error) {
	board.ID = strings.TrimSpace(board.ID)
	board.SessionID = strings.TrimSpace(board.SessionID)
	if board.ID == "" {
		return storyboardRecord{}, fmt.Errorf("storyboard id is required")
	}
	if board.SessionID == "" {
		return storyboardRecord{}, fmt.Errorf("session id is required")
	}
	keyElements, err := json.Marshal(board.KeyElements)
	if err != nil {
		return storyboardRecord{}, fmt.Errorf("marshal key elements: %w", err)
	}
	shots, err := json.Marshal(board.Shots)
	if err != nil {
		return storyboardRecord{}, fmt.Errorf("marshal shots: %w", err)
	}
	audioLayers, err := json.Marshal(board.AudioLayers)
	if err != nil {
		return storyboardRecord{}, fmt.Errorf("marshal audio layers: %w", err)
	}
	return storyboardRecord{
		ID:          board.ID,
		SessionID:   board.SessionID,
		SpecID:      strings.TrimSpace(board.SpecID),
		Version:     board.Version,
		Status:      board.Status,
		KeyElements: keyElements,
		Shots:       shots,
		AudioLayers: audioLayers,
		CreatedAt:   board.CreatedAt,
		UpdatedAt:   board.UpdatedAt,
	}, nil
}

func recordToBoard(record storyboardRecord) (Storyboard, error) {
	board := Storyboard{
		ID:        record.ID,
		SessionID: record.SessionID,
		SpecID:    record.SpecID,
		Version:   record.Version,
		Status:    record.Status,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
	if len(record.KeyElements) > 0 {
		if err := json.Unmarshal(record.KeyElements, &board.KeyElements); err != nil {
			return Storyboard{}, fmt.Errorf("unmarshal key elements: %w", err)
		}
	}
	if len(record.Shots) > 0 {
		if err := json.Unmarshal(record.Shots, &board.Shots); err != nil {
			return Storyboard{}, fmt.Errorf("unmarshal shots: %w", err)
		}
	}
	if len(record.AudioLayers) > 0 {
		if err := json.Unmarshal(record.AudioLayers, &board.AudioLayers); err != nil {
			return Storyboard{}, fmt.Errorf("unmarshal audio layers: %w", err)
		}
	}
	return board, nil
}

func eventToRecord(event EventRecord) (storyboardEventRecord, error) {
	ops, err := json.Marshal(event.Ops)
	if err != nil {
		return storyboardEventRecord{}, fmt.Errorf("marshal storyboard event ops: %w", err)
	}
	return storyboardEventRecord{
		ID:           strings.TrimSpace(event.ID),
		SessionID:    strings.TrimSpace(event.SessionID),
		StoryboardID: strings.TrimSpace(event.StoryboardID),
		BaseVersion:  event.BaseVersion,
		NextVersion:  event.NextVersion,
		Source:       strings.TrimSpace(event.Source),
		ToolCallID:   strings.TrimSpace(event.ToolCallID),
		Ops:          ops,
		CreatedAt:    event.CreatedAt,
	}, nil
}

func applyJSONPatch(board Storyboard, ops []patch.JSONPatchOp) (Storyboard, error) {
	raw, err := json.Marshal(board)
	if err != nil {
		return Storyboard{}, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Storyboard{}, err
	}
	for _, op := range ops {
		if err := applyOne(doc, op); err != nil {
			return Storyboard{}, err
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return Storyboard{}, err
	}
	var patched Storyboard
	if err := json.Unmarshal(out, &patched); err != nil {
		return Storyboard{}, err
	}
	return patched, nil
}

func applyOne(doc any, op patch.JSONPatchOp) error {
	parts, err := parsePatchPath(op.Path)
	if err != nil {
		return err
	}
	parent, key, setParent, err := locateParent(doc, parts)
	if err != nil {
		return err
	}
	switch p := parent.(type) {
	case map[string]any:
		switch op.Op {
		case "add", "replace":
			p[key] = op.Value
		case "remove":
			delete(p, key)
		default:
			return fmt.Errorf("%w: unsupported op %s", ErrInvalidPatch, op.Op)
		}
		return nil
	case []any:
		idx, err := patchIndexForOp(key, len(p), op.Op)
		if err != nil {
			return err
		}
		switch op.Op {
		case "replace":
			p[idx] = op.Value
		case "add":
			if setParent == nil {
				return fmt.Errorf("%w: cannot update root array", ErrInvalidPatch)
			}
			updated := append(p[:idx:idx], append([]any{op.Value}, p[idx:]...)...)
			setParent(updated)
		case "remove":
			if setParent == nil {
				return fmt.Errorf("%w: cannot update root array", ErrInvalidPatch)
			}
			updated := append(p[:idx:idx], p[idx+1:]...)
			setParent(updated)
		default:
			return fmt.Errorf("%w: unsupported op %s", ErrInvalidPatch, op.Op)
		}
		return nil
	default:
		return fmt.Errorf("%w: path %s does not resolve to a container", ErrInvalidPatch, op.Path)
	}
}

func locateParent(doc any, parts []string) (any, string, func(any), error) {
	if len(parts) == 0 {
		return nil, "", nil, fmt.Errorf("%w: root path cannot be patched", ErrInvalidPatch)
	}
	current := doc
	var setCurrent func(any)
	for _, part := range parts[:len(parts)-1] {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, "", nil, fmt.Errorf("%w: missing path segment %s", ErrInvalidPatch, part)
			}
			key := part
			setCurrent = func(value any) {
				node[key] = value
			}
			current = next
		case []any:
			idx, err := patchIndex(part, len(node))
			if err != nil {
				return nil, "", nil, err
			}
			setCurrent = func(value any) {
				node[idx] = value
			}
			current = node[idx]
		default:
			return nil, "", nil, fmt.Errorf("%w: non-container segment %s", ErrInvalidPatch, part)
		}
	}
	return current, parts[len(parts)-1], setCurrent, nil
}

func parsePatchPath(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("%w: path must start with /", ErrInvalidPatch)
	}
	raw := strings.Split(strings.TrimPrefix(path, "/"), "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if part == "" {
			return nil, fmt.Errorf("%w: empty path segment", ErrInvalidPatch)
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func patchIndex(raw string, length int) (int, error) {
	idx, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid array index %s", ErrInvalidPatch, raw)
	}
	if idx < 0 || idx >= length {
		return 0, fmt.Errorf("%w: array index %d out of range", ErrInvalidPatch, idx)
	}
	return idx, nil
}

func patchIndexForOp(raw string, length int, op string) (int, error) {
	if op == "add" && raw == "-" {
		return length, nil
	}
	idx, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid array index %s", ErrInvalidPatch, raw)
	}
	if idx < 0 {
		return 0, fmt.Errorf("%w: array index %d out of range", ErrInvalidPatch, idx)
	}
	if op == "add" {
		if idx > length {
			return 0, fmt.Errorf("%w: array index %d out of range", ErrInvalidPatch, idx)
		}
		return idx, nil
	}
	if idx >= length {
		return 0, fmt.Errorf("%w: array index %d out of range", ErrInvalidPatch, idx)
	}
	return idx, nil
}

func valueOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
