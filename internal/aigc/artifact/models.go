// Package artifact stores versioned non-storyboard creation artifacts such as
// material analyses and assembly plans.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

var (
	ErrNotFound            = errors.New("creation artifact not found")
	ErrIdempotencyConflict = errors.New("creation artifact idempotency conflict")
	ErrNotReviewable       = errors.New("creation artifact is not reviewable")
	ErrStale               = errors.New("creation artifact review target is stale")
)

const (
	KindMaterialAnalysis = "material_analysis"
	KindAssemblyPlan     = "assembly_plan"
	KindExportResult     = "export_result"

	StatusDraft      = "draft"
	StatusReviewing  = "reviewing"
	StatusActive     = "active"
	StatusRejected   = "rejected"
	StatusSuperseded = "superseded"
	StatusStale      = "stale"

	ReviewDecisionApprove = "approve"
	ReviewDecisionReject  = "reject"
)

// Revision is an immutable versioned artifact. Activating a revision only
// changes its lifecycle state; Content is never updated in place.
type Revision struct {
	ID             string         `json:"id" gorm:"primaryKey;size:128"`
	SessionID      string         `json:"session_id" gorm:"size:128;uniqueIndex:uidx_aigc_artifact_version,priority:1;index"`
	Kind           string         `json:"kind" gorm:"size:64;uniqueIndex:uidx_aigc_artifact_version,priority:2;index"`
	Version        int            `json:"version" gorm:"uniqueIndex:uidx_aigc_artifact_version,priority:3;autoIncrement:false"`
	Status         string         `json:"status" gorm:"size:32;index"`
	IdempotencyKey string         `json:"idempotency_key" gorm:"size:256;uniqueIndex"`
	DerivedFrom    map[string]int `json:"derived_from,omitempty" gorm:"type:jsonb;serializer:json"`
	Content        map[string]any `json:"content" gorm:"type:jsonb;serializer:json"`
	CreatedBy      string         `json:"created_by,omitempty" gorm:"size:128"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	ActivatedAt    *time.Time     `json:"activated_at,omitempty"`
}

func (Revision) TableName() string { return "aigc_artifact_revisions" }

func (r Revision) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.SessionID) == "" || strings.TrimSpace(r.Kind) == "" {
		return fmt.Errorf("artifact id, session id and kind are required")
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("artifact idempotency key is required")
	}
	if r.Version < 0 {
		return fmt.Errorf("artifact version cannot be negative")
	}
	if r.Content == nil {
		return fmt.Errorf("artifact content is required")
	}
	return nil
}

func normalizeCreateRequest(revision Revision) Revision {
	revision.ID = strings.TrimSpace(revision.ID)
	revision.SessionID = strings.TrimSpace(revision.SessionID)
	revision.Kind = strings.TrimSpace(revision.Kind)
	revision.Status = strings.TrimSpace(revision.Status)
	revision.IdempotencyKey = strings.TrimSpace(revision.IdempotencyKey)
	revision.CreatedBy = strings.TrimSpace(revision.CreatedBy)
	if revision.Status == "" {
		revision.Status = StatusDraft
	}
	if len(revision.DerivedFrom) == 0 {
		revision.DerivedFrom = nil
	}
	return revision
}

func sameCreateRequest(existing, requested Revision) bool {
	if existing.ID != requested.ID || existing.SessionID != requested.SessionID || existing.Kind != requested.Kind ||
		existing.Status != requested.Status || existing.IdempotencyKey != requested.IdempotencyKey || existing.CreatedBy != requested.CreatedBy {
		return false
	}
	if requested.Version > 0 && existing.Version != requested.Version {
		return false
	}
	return sameJSON(existing.DerivedFrom, requested.DerivedFrom) && sameJSON(existing.Content, requested.Content)
}

func sameJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	var leftValue, rightValue any
	if json.Unmarshal(leftJSON, &leftValue) != nil || json.Unmarshal(rightJSON, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

type CreateResult struct {
	Revision Revision `json:"revision"`
	Created  bool     `json:"created"`
}

// ReviewCommand is the immutable domain command selected by an approval.
// Its receipt is committed in the same transaction as the lifecycle change,
// closing the crash window between an artifact transition and the outer
// approval continuation ledger.
type ReviewCommand struct {
	IdempotencyKey  string `json:"idempotency_key"`
	SessionID       string `json:"session_id"`
	ArtifactID      string `json:"artifact_id"`
	ArtifactKind    string `json:"artifact_kind"`
	ArtifactVersion int    `json:"artifact_version"`
	ExpectedStatus  string `json:"expected_status"`
	Decision        string `json:"decision"`
	RequireLatest   bool   `json:"require_latest"`
}

func (c ReviewCommand) normalize() ReviewCommand {
	c.IdempotencyKey = strings.TrimSpace(c.IdempotencyKey)
	c.SessionID = strings.TrimSpace(c.SessionID)
	c.ArtifactID = strings.TrimSpace(c.ArtifactID)
	c.ArtifactKind = strings.TrimSpace(c.ArtifactKind)
	c.ExpectedStatus = strings.TrimSpace(c.ExpectedStatus)
	if c.ExpectedStatus == "" {
		c.ExpectedStatus = StatusReviewing
	}
	c.Decision = strings.ToLower(strings.TrimSpace(c.Decision))
	return c
}

func (c ReviewCommand) Validate() error {
	c = c.normalize()
	if c.IdempotencyKey == "" || c.SessionID == "" || c.ArtifactID == "" || c.ArtifactKind == "" || c.ArtifactVersion <= 0 {
		return fmt.Errorf("artifact review command requires idempotency key, session, artifact identity, kind and positive version")
	}
	if c.Decision != ReviewDecisionApprove && c.Decision != ReviewDecisionReject {
		return fmt.Errorf("artifact review decision must be %q or %q", ReviewDecisionApprove, ReviewDecisionReject)
	}
	if c.ExpectedStatus != StatusReviewing {
		return fmt.Errorf("artifact review command must require status %q", StatusReviewing)
	}
	return nil
}

// ReviewCommandReceipt is a first-write-wins durable command receipt. Result
// is the immutable post-transition snapshot, so recovery remains correct even
// when a newer artifact later supersedes the reviewed revision.
type ReviewCommandReceipt struct {
	IdempotencyKey  string    `json:"idempotency_key" gorm:"primaryKey;size:256"`
	SessionID       string    `json:"session_id" gorm:"size:128;index"`
	ArtifactID      string    `json:"artifact_id" gorm:"size:128;index"`
	ArtifactKind    string    `json:"artifact_kind" gorm:"size:64;index"`
	ArtifactVersion int       `json:"artifact_version"`
	ExpectedStatus  string    `json:"expected_status" gorm:"size:32"`
	Decision        string    `json:"decision" gorm:"size:32"`
	RequireLatest   bool      `json:"require_latest"`
	Result          Revision  `json:"result" gorm:"type:jsonb;serializer:json"`
	CreatedAt       time.Time `json:"created_at"`
}

func (ReviewCommandReceipt) TableName() string { return "aigc_artifact_command_receipts" }

func (r ReviewCommandReceipt) command() ReviewCommand {
	return ReviewCommand{
		IdempotencyKey:  r.IdempotencyKey,
		SessionID:       r.SessionID,
		ArtifactID:      r.ArtifactID,
		ArtifactKind:    r.ArtifactKind,
		ArtifactVersion: r.ArtifactVersion,
		ExpectedStatus:  r.ExpectedStatus,
		Decision:        r.Decision,
		RequireLatest:   r.RequireLatest,
	}.normalize()
}

func sameReviewCommand(receipt ReviewCommandReceipt, command ReviewCommand) bool {
	left, right := receipt.command(), command.normalize()
	return left.IdempotencyKey == right.IdempotencyKey && left.SessionID == right.SessionID && left.ArtifactID == right.ArtifactID &&
		left.ArtifactKind == right.ArtifactKind && left.ArtifactVersion == right.ArtifactVersion &&
		left.ExpectedStatus == right.ExpectedStatus && left.Decision == right.Decision && left.RequireLatest == right.RequireLatest
}

type ReviewResult struct {
	Revision Revision             `json:"revision"`
	Receipt  ReviewCommandReceipt `json:"receipt"`
	Applied  bool                 `json:"applied"`
}
