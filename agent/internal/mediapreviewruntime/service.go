package mediapreviewruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
)

// Service 负责两个严格 Intent 的 canonical 化、摘要与可靠入队。
type Service struct {
	repository Repository
	clock      Clock
	wake       func()
}

// NewService 创建媒体 typed ingress Service。
func NewService(repository Repository, clock Clock, wake func()) (*Service, error) {
	if repository == nil || clock == nil || wake == nil {
		return nil, fmt.Errorf("create media preview runtime service: invalid dependency")
	}
	return &Service{repository: repository, clock: clock, wake: wake}, nil
}

// Enqueue 严格验证 Tool/Intent，冻结十分钟 Deadline 并在提交后唤醒共享 Coordinator。
func (s *Service) Enqueue(ctx context.Context, request EnqueueRequest) (EnqueueResponse, error) {
	if !mediapreview.ValidUUIDv7(request.RequestID) || !mediapreview.ValidUUIDv7(request.SessionID) ||
		!mediapreview.ValidUUIDv7(request.UserID) || !mediapreview.ValidUUIDv7(request.ProjectID) ||
		!mediapreview.ValidUUIDv7(request.IdempotencyKey) {
		return EnqueueResponse{}, ErrInvalidInput
	}
	canonical, schemaVersion, err := canonicalIntent(request.ToolKey, request.IntentJSON)
	if err != nil {
		return EnqueueResponse{}, ErrInvalidInput
	}
	intentDigest := digest(canonical)
	requestDigest, err := mediapreview.DigestJSON(struct {
		SchemaVersion string `json:"schema_version"`
		ToolKey       string `json:"tool_key"`
		IntentDigest  string `json:"intent_digest"`
	}{"media_preview.enqueue.digest.v1", request.ToolKey, intentDigest})
	if err != nil {
		return EnqueueResponse{}, ErrPersistence
	}
	now := s.clock.Now().UTC()
	result, err := s.repository.Enqueue(ctx, EnqueueCommand{
		EnqueueRequest: EnqueueRequest{
			RequestID: request.RequestID, SessionID: request.SessionID, UserID: request.UserID,
			ProjectID: request.ProjectID, IdempotencyKey: request.IdempotencyKey,
			ToolKey: request.ToolKey, IntentJSON: canonical,
		},
		IntentSchemaVersion: schemaVersion, IntentDigest: intentDigest,
		RequestDigest: requestDigest, DeadlineAt: now.Add(DefaultRequestTTL),
	})
	if err != nil {
		return EnqueueResponse{}, err
	}
	if !mediapreview.ValidUUIDv7(result.InputID) || !mediapreview.ValidUUIDv7(result.TurnID) ||
		!mediapreview.ValidUUIDv7(result.RunID) || !mediapreview.ValidUUIDv7(result.ToolCallID) ||
		result.ToolKey != request.ToolKey {
		return EnqueueResponse{}, ErrPersistence
	}
	s.wake()
	return EnqueueResponse{
		SchemaVersion: EnqueueSchemaVersion, Status: PendingStatus, InputID: result.InputID,
		TurnID: result.TurnID, RunID: result.RunID, ToolCallID: result.ToolCallID,
		ToolKey: result.ToolKey, Replayed: result.Replayed,
	}, nil
}

func canonicalIntent(toolKey string, raw []byte) ([]byte, string, error) {
	switch toolKey {
	case mediapreview.GenerateMediaToolKey:
		intent, err := mediapreview.DecodeGenerateMediaIntent(raw)
		if err != nil {
			return nil, "", err
		}
		encoded, err := mediapreview.CanonicalJSON(intent)
		return encoded, mediapreview.GenerateMediaIntentVersion, err
	case mediapreview.AssembleOutputToolKey:
		intent, err := mediapreview.DecodeAssembleOutputIntent(raw)
		if err != nil {
			return nil, "", err
		}
		encoded, err := mediapreview.CanonicalJSON(intent)
		return encoded, mediapreview.AssembleOutputIntentVersion, err
	default:
		return nil, "", ErrInvalidInput
	}
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
