// Package generationruntime wires the durable generation state machine to
// storyboard, asset, approval and billing business services.
package generationruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/billing"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type AssetStore interface {
	Get(context.Context, string) (asset.Asset, error)
	Save(context.Context, asset.Asset) (asset.Asset, error)
}

type SourceJobAssetStore interface {
	ListBySourceJob(context.Context, string) ([]asset.Asset, error)
}

type ConfirmedSpecSource interface {
	GetConfirmedBySession(context.Context, string) (spec.FinalVideoSpec, error)
}

// PendingAssetStore ensures provider handlers cannot expose an asset before
// finalization and billing have committed.
type PendingAssetStore struct{ AssetStore }

func (s PendingAssetStore) Save(ctx context.Context, record asset.Asset) (asset.Asset, error) {
	record.Availability = asset.AvailabilityPendingBilling
	return s.AssetStore.Save(ctx, record)
}

func (s PendingAssetStore) Get(ctx context.Context, id string) (asset.Asset, error) {
	return s.AssetStore.Get(ctx, id)
}

func (s PendingAssetStore) RecoverProviderResult(ctx context.Context, job generation.GenerationJob) (generation.ProviderResult, bool, error) {
	source, ok := s.AssetStore.(SourceJobAssetStore)
	if !ok {
		return generation.ProviderResult{}, false, nil
	}
	items, err := source.ListBySourceJob(ctx, job.ID)
	if err != nil {
		return generation.ProviderResult{}, false, err
	}
	expected := 1
	if strings.EqualFold(job.MediaKind, "image") || strings.EqualFold(job.MediaKind, "illustration") || strings.EqualFold(job.MediaKind, "keyframe") {
		switch value := job.Payload["n"].(type) {
		case int:
			if value > 0 {
				expected = value
			}
		case float64:
			if value > 0 {
				expected = int(value)
			}
		}
	}
	byOutput := make(map[int]string, len(items))
	for _, item := range items {
		if item.Availability != asset.AvailabilityPendingBilling && item.Availability != asset.AvailabilityAvailable {
			continue
		}
		if item.OutputIndex < 0 || item.OutputIndex >= expected {
			continue
		}
		if _, exists := byOutput[item.OutputIndex]; !exists {
			byOutput[item.OutputIndex] = item.ID
		}
	}
	assetIDs := make([]string, 0, len(byOutput))
	complete := true
	for index := 0; index < expected; index++ {
		assetID, exists := byOutput[index]
		if !exists {
			complete = false
			continue
		}
		assetIDs = append(assetIDs, assetID)
	}
	if len(assetIDs) == 0 {
		return generation.ProviderResult{}, false, nil
	}
	return generation.ProviderResult{AssetIDs: assetIDs, Payload: map[string]any{"asset_ids": assetIDs, "recovered_receipt": true, "receipt_complete": complete}}, complete, nil
}

type StoryboardBindingAdapter struct {
	Repository storyboard.AggregateRepository
	Commands   *storyboard.CommandService
	Assets     AssetStore
	Approvals  approval.Store
	Specs      ConfirmedSpecSource
	Events     a2ui.EventPublisher
}

func (a StoryboardBindingAdapter) Check(ctx context.Context, token generation.BindingToken) (generation.BindingCheck, error) {
	if token.NormalizedKind() == generation.TargetKindSessionDeliverable {
		// session_deliverable 无可漂移的绑定目标（无 storyboard 结构可比对），
		// 目标恒存在、恒匹配；漂移防护由 job 级 request_fingerprint 承担。
		return generation.BindingCheck{TargetExists: true, Matches: true, Current: token}, nil
	}
	if strings.HasPrefix(token.TargetID, "assembly:") {
		if a.Repository == nil {
			return generation.BindingCheck{}, fmt.Errorf("storyboard repository is required")
		}
		aggregate, err := a.Repository.GetAggregate(ctx, token.StoryboardID)
		if err != nil {
			return generation.BindingCheck{}, err
		}
		revision, err := aggregate.ActiveRevision()
		if err != nil {
			return generation.BindingCheck{}, err
		}
		current := token
		current.SpecVersion = revision.DerivedFromSpecVersion
		current.AggregateVersion = aggregate.Version
		specMatches, err := a.specMatchesStoryboard(ctx, aggregate)
		if err != nil {
			return generation.BindingCheck{}, err
		}
		return generation.BindingCheck{TargetExists: true, Matches: specMatches && token.Equal(current), Current: current}, nil
	}
	if a.Repository == nil {
		return generation.BindingCheck{}, fmt.Errorf("storyboard repository is required")
	}
	aggregate, err := a.Repository.GetAggregate(ctx, token.StoryboardID)
	if err != nil {
		if errors.Is(err, storyboard.ErrAggregateNotFound) {
			return generation.BindingCheck{}, nil
		}
		return generation.BindingCheck{}, err
	}
	current, exists := currentBindingToken(aggregate, token.TargetID, token.AssetSlot)
	matches := exists && token.Equal(current)
	if matches {
		specMatches, specErr := a.specMatchesStoryboard(ctx, aggregate)
		if specErr != nil {
			return generation.BindingCheck{}, specErr
		}
		matches = specMatches
	}
	return generation.BindingCheck{TargetExists: exists, Matches: matches, Current: current}, nil
}

func (a StoryboardBindingAdapter) Commit(ctx context.Context, input generation.FinalizationCommit) error {
	assetPG, assetOK := a.Assets.(*asset.PostgresStore)
	storyboardPG, storyboardOK := a.Repository.(*storyboard.PostgresStore)
	approvalPG, approvalOK := a.Approvals.(*approval.PostgresStore)
	specPG, specOK := a.Specs.(*spec.PostgresStore)
	if assetOK && storyboardOK && storyboardPG.DB() != nil && (!needsApproval(input) || approvalOK) {
		err := storyboardPG.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			commands, err := storyboard.NewCommandService(storyboardPG.WithTx(tx))
			if err != nil {
				return err
			}
			local := a
			local.Assets = assetPG.WithTx(tx)
			local.Repository = storyboardPG.WithTx(tx)
			local.Commands = commands
			if approvalOK {
				local.Approvals = approvalPG.WithTx(tx)
			}
			if specOK {
				local.Specs = specPG.WithTx(tx)
			}
			return local.commit(ctx, input)
		})
		if err != nil {
			return err
		}
		return nil
	}
	return a.commit(ctx, input)
}

// PublishFinalized projects an already committed job result. It is invoked by
// the durable generation outbox, never by Commit: a transient SSE/event-log
// failure must not roll a successful asset binding into quarantine/refund.
func (a StoryboardBindingAdapter) PublishFinalized(ctx context.Context, job generation.GenerationJob) error {
	return a.publishChanges(ctx, generation.FinalizationCommit{
		Job:            job,
		AssetIDs:       append([]string(nil), job.ResultAssetIDs...),
		BindingToken:   job.BindingToken,
		BindingMode:    job.DeliveryPolicy.Normalize().BindingMode,
		ApprovalPolicy: job.DeliveryPolicy.Normalize().ApprovalPolicy,
	})
}

func (a StoryboardBindingAdapter) IsCommitted(ctx context.Context, job generation.GenerationJob, assetIDs []string) (bool, error) {
	if a.Assets == nil || len(assetIDs) == 0 {
		return false, nil
	}
	for _, assetID := range assetIDs {
		stored, err := a.Assets.Get(ctx, assetID)
		if err != nil {
			if errors.Is(err, asset.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if stored.Availability != asset.AvailabilityAvailable {
			return false, nil
		}
		if stored.SourceJobID != "" && stored.SourceJobID != job.ID {
			return false, nil
		}
	}
	if strings.HasPrefix(job.BindingToken.TargetID, "assembly:") ||
		job.BindingToken.NormalizedKind() == generation.TargetKindSessionDeliverable {
		return true, nil
	}
	if a.Repository == nil {
		return false, nil
	}
	aggregate, err := a.Repository.GetAggregate(ctx, job.BindingToken.StoryboardID)
	if err != nil {
		if errors.Is(err, storyboard.ErrAggregateNotFound) {
			return false, nil
		}
		return false, err
	}
	for index, assetID := range assetIDs {
		bindingID := fmt.Sprintf("binding:%s:%d", job.ID, index)
		found := false
		for _, binding := range aggregate.Bindings {
			if binding.ID == bindingID && binding.AssetID == assetID {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
		if job.DeliveryPolicy.Normalize().ApprovalPolicy == generation.ApprovalReviewRequired && a.Approvals != nil {
			if _, err := a.Approvals.Get(ctx, "approval:"+bindingID); err != nil {
				if errors.Is(err, approval.ErrNotFound) {
					return false, nil
				}
				return false, err
			}
		}
	}
	return true, nil
}

func (a StoryboardBindingAdapter) Discard(ctx context.Context, job generation.GenerationJob, assetIDs []string, disposition string) error {
	if a.Assets == nil {
		return fmt.Errorf("asset store is required")
	}
	for _, assetID := range assetIDs {
		stored, err := a.Assets.Get(ctx, assetID)
		if err != nil {
			return err
		}
		stored.Availability = asset.AvailabilityQuarantined
		if stored.Metadata == nil {
			stored.Metadata = map[string]any{}
		}
		stored.Metadata["result_disposition"] = strings.TrimSpace(disposition)
		stored.Metadata["source_job_id"] = job.ID
		if _, err := a.Assets.Save(ctx, stored); err != nil {
			return err
		}
	}
	return nil
}

func needsApproval(input generation.FinalizationCommit) bool {
	return input.ApprovalPolicy == generation.ApprovalReviewRequired
}

func (a StoryboardBindingAdapter) commit(ctx context.Context, input generation.FinalizationCommit) error {
	isDeliverable := input.BindingToken.NormalizedKind() == generation.TargetKindSessionDeliverable
	if strings.HasPrefix(input.BindingToken.TargetID, "assembly:") {
		check, err := a.Check(ctx, input.BindingToken)
		if err != nil {
			return err
		}
		if !check.TargetExists || !check.Matches {
			return generation.NewExecutionError(generation.ErrorStageBinding, generation.ErrorResultSuperseded, false, fmt.Errorf("assembly input changed before commit"))
		}
	}
	if !strings.HasPrefix(input.BindingToken.TargetID, "assembly:") && !isDeliverable {
		if a.Repository == nil || a.Commands == nil {
			return fmt.Errorf("storyboard binding services are required")
		}
		// This read occurs inside the same transaction as asset publication and
		// binding creation for Postgres-backed stores. It closes the window
		// between the worker's pre-charge guard and the final semantic commit.
		aggregate, err := a.Repository.GetAggregate(ctx, input.BindingToken.StoryboardID)
		if err != nil {
			return err
		}
		current, exists := currentBindingToken(aggregate, input.BindingToken.TargetID, input.BindingToken.AssetSlot)
		specMatches, specErr := a.specMatchesStoryboard(ctx, aggregate)
		if specErr != nil {
			return specErr
		}
		if !exists || !specMatches || !input.BindingToken.Equal(current) {
			return generation.NewExecutionError(generation.ErrorStageBinding, generation.ErrorResultSuperseded, false, fmt.Errorf("generation binding token became stale before commit"))
		}
	}
	for _, assetID := range input.AssetIDs {
		stored, err := a.Assets.Get(ctx, assetID)
		if err != nil {
			return err
		}
		stored.Availability = asset.AvailabilityAvailable
		if isDeliverable {
			if stored.Metadata == nil {
				stored.Metadata = map[string]any{}
			}
			stored.Metadata["target_kind"] = generation.TargetKindSessionDeliverable
			stored.Metadata["deliverable_target_id"] = input.BindingToken.TargetID
		}
		if _, err := a.Assets.Save(ctx, stored); err != nil {
			return err
		}
	}
	if strings.HasPrefix(input.BindingToken.TargetID, "assembly:") || isDeliverable {
		return nil
	}
	for index, assetID := range input.AssetIDs {
		aggregate, err := a.Repository.GetAggregate(ctx, input.BindingToken.StoryboardID)
		if err != nil {
			return err
		}
		bindingID := fmt.Sprintf("binding:%s:%d", input.Job.ID, index)
		approvalID := ""
		if input.ApprovalPolicy == generation.ApprovalReviewRequired {
			approvalID = "approval:" + bindingID
		}
		updated, disposition, err := a.Commands.Bind(ctx, storyboard.BindAssetCommand{
			CommandID: "job:" + input.Job.ID + ":bind:" + fmt.Sprint(index), StoryboardID: aggregate.ID,
			BaseVersion: aggregate.Version, BindingID: bindingID, TargetID: input.BindingToken.TargetID,
			AssetSlot: input.BindingToken.AssetSlot, AssetID: assetID, AttemptID: fmt.Sprintf("%s:%d", input.Job.ID, input.Job.Attempt), ApprovalID: approvalID,
			TargetRevision: input.BindingToken.TargetRevision, PromptRevision: input.BindingToken.PromptRevision,
			GenerationEpoch: input.BindingToken.GenerationEpoch, InputFingerprint: input.BindingToken.InputFingerprint,
			Activate: input.BindingMode == generation.BindingModeActive,
		})
		if err != nil {
			return err
		}
		if disposition == storyboard.BindingDispositionSuperseded {
			return generation.NewExecutionError(generation.ErrorStageBinding, generation.ErrorResultSuperseded, false, fmt.Errorf("generation binding token became stale"))
		}
		if input.ApprovalPolicy == generation.ApprovalReviewRequired {
			if a.Approvals == nil {
				return fmt.Errorf("approval store is required for candidate binding")
			}
			var artifactRevision int
			for _, binding := range updated.Bindings {
				if binding.ID == bindingID {
					artifactRevision = binding.ArtifactRevision
					break
				}
			}
			approvePayload, _ := json.Marshal(map[string]any{"storyboard_id": updated.ID, "base_version": updated.Version, "binding_id": bindingID})
			rejectPayload, _ := json.Marshal(map[string]any{"storyboard_id": updated.ID, "base_version": updated.Version, "binding_id": bindingID})
			_, err = a.Approvals.Create(ctx, approval.Approval{
				ID: approvalID, IdempotencyKey: "candidate:" + bindingID,
				SessionID: input.Job.SessionID, UserID: input.Job.UserID, ArtifactType: "candidate_asset",
				Binding:    approval.VersionBinding{ArtifactID: bindingID, ArtifactVersion: max(artifactRevision, 1), StoryboardID: updated.ID, StoryboardVersion: updated.Version, TargetID: input.BindingToken.TargetID, TargetRevision: input.BindingToken.TargetRevision, PromptRevision: input.BindingToken.PromptRevision, GenerationEpoch: input.BindingToken.GenerationEpoch},
				ReviewMode: approval.ReviewModeDurable, ExecutionMode: approval.ExecutionModeDurable,
				ApproveCommand: approval.FrozenCommand{Kind: "ActivateArtifactBinding", IdempotencyKey: "approval:" + bindingID + ":activate", Payload: approvePayload},
				RejectCommand:  approval.FrozenCommand{Kind: "RejectArtifactBinding", IdempotencyKey: "approval:" + bindingID + ":reject", Payload: rejectPayload},
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// publishDeliverable 把已落库的 deliverable 资产投影为 deliverables
// surface 的增量事件（前端按 asset id 合并；断线兜底靠全量资产刷新）。
// 发布失败传播给 outbox 重试，与 storyboard 投影同语义。
func (a StoryboardBindingAdapter) publishDeliverable(ctx context.Context, input generation.FinalizationCommit) error {
	if a.Events == nil || a.Assets == nil || len(input.AssetIDs) == 0 {
		return nil
	}
	views := make([]map[string]any, 0, len(input.AssetIDs))
	for _, assetID := range input.AssetIDs {
		stored, err := a.Assets.Get(ctx, assetID)
		if err != nil {
			return err
		}
		views = append(views, map[string]any{
			"id": stored.ID, "url": stored.URL, "kind": stored.Kind,
			"mime_type": stored.MIMEType, "filename": stored.Filename,
			"target_id": input.BindingToken.TargetID,
		})
	}
	envelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: []a2ui.Action{{
		Type: a2ui.ActionUpdateCard, Surface: "deliverables",
		Target:  &a2ui.ActionTarget{Surface: "deliverables", CardID: "deliverables"},
		Payload: map[string]any{"assets": views},
	}}}
	return a.Events.Publish(ctx, a2ui.SSEEvent{ID: "deliverable:" + input.Job.ID, SessionID: input.Job.SessionID, Event: a2ui.EventAction, Payload: envelope, CreatedAt: time.Now()})
}

func (a StoryboardBindingAdapter) specMatchesStoryboard(ctx context.Context, aggregate storyboard.StoryboardAggregate) (bool, error) {
	if a.Specs == nil {
		return true, nil
	}
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return false, err
	}
	confirmed, err := a.Specs.GetConfirmedBySession(ctx, aggregate.SessionID)
	if err != nil {
		return false, err
	}
	return revision.DerivedFromSpecVersion == 0 || revision.DerivedFromSpecVersion == confirmed.Version, nil
}

func (a StoryboardBindingAdapter) publishChanges(ctx context.Context, input generation.FinalizationCommit) error {
	if input.BindingToken.NormalizedKind() == generation.TargetKindSessionDeliverable {
		return a.publishDeliverable(ctx, input)
	}
	if a.Events == nil || strings.HasPrefix(input.BindingToken.TargetID, "assembly:") {
		return nil
	}
	aggregate, err := a.Repository.GetAggregate(ctx, input.BindingToken.StoryboardID)
	if err != nil {
		return err
	}
	storyboardEnvelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: []a2ui.Action{{Type: a2ui.ActionUpdateCard, Surface: "storyboard", Target: &a2ui.ActionTarget{Surface: "storyboard", CardID: "storyboard"}, Payload: map[string]any{"storyboard": aggregate.PublicView()}}}}
	if err := a.Events.Publish(ctx, a2ui.SSEEvent{ID: fmt.Sprintf("job:%s:storyboard:v%d", input.Job.ID, aggregate.Version), SessionID: input.Job.SessionID, Event: a2ui.EventAction, Payload: storyboardEnvelope, CreatedAt: time.Now()}); err != nil {
		return err
	}
	// Candidate approval cards are intentionally not projected into chat. The
	// durable approval rows and binding approval_id fences are consumed by the
	// storyboard-level batch confirmation endpoint.
	return nil
}

func currentBindingToken(aggregate storyboard.StoryboardAggregate, targetID, slotKey string) (generation.BindingToken, bool) {
	input, err := aggregate.ResolveGenerationInput(targetID, slotKey)
	if err != nil {
		return generation.BindingToken{}, false
	}
	revision, err := aggregate.ActiveRevision()
	if err != nil {
		return generation.BindingToken{}, false
	}
	return generation.BindingToken{
		StoryboardID: aggregate.ID, TargetID: input.TargetID, AssetSlot: input.AssetSlot,
		TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision,
		GenerationEpoch: input.GenerationEpoch, SpecVersion: revision.DerivedFromSpecVersion, InputFingerprint: input.Fingerprint,
	}, true
}

// CheckWithFingerprint compares the independently recomputed semantic input
// fingerprint with the dispatch token. Never copy the dispatch fingerprint
// into the current snapshot: doing so would disable the fence entirely.
func (a StoryboardBindingAdapter) CheckWithFingerprint(ctx context.Context, token generation.BindingToken) (generation.BindingCheck, error) {
	return a.Check(ctx, token)
}

type BindingGuard struct{ StoryboardBindingAdapter }

func (g BindingGuard) Check(ctx context.Context, token generation.BindingToken) (generation.BindingCheck, error) {
	return g.StoryboardBindingAdapter.CheckWithFingerprint(ctx, token)
}

type BillingAdapter struct{ Store billing.Store }

func (a BillingAdapter) Charge(ctx context.Context, request generation.ChargeRequest) (generation.ChargeResult, error) {
	if a.Store == nil {
		return generation.ChargeResult{}, fmt.Errorf("billing store is required")
	}
	result, err := a.Store.Charge(ctx, billing.MutationRequest{TransactionID: "charge:" + request.JobID, UserID: request.UserID, IdempotencyKey: request.IdempotencyKey, OperationID: request.OperationID, BatchID: request.BatchID, JobID: request.JobID, Points: request.Points, Breakdown: request.Breakdown})
	if err != nil {
		retryable := !(errors.Is(err, billing.ErrAccountNotFound) || errors.Is(err, billing.ErrInsufficientPoints) || errors.Is(err, billing.ErrTransactionNotFound) || errors.Is(err, billing.ErrRefundExceedsCharge) || errors.Is(err, billing.ErrIdempotencyConflict))
		return generation.ChargeResult{}, generation.NewExecutionError(generation.ErrorStageBilling, "billing_rejected", retryable, err)
	}
	balance := result.Transaction.BalanceAfter
	return generation.ChargeResult{TransactionID: result.Transaction.ID, ChargedPoints: result.Transaction.Points, Breakdown: result.Transaction.Breakdown, BalanceAfter: &balance}, nil
}

func (a BillingAdapter) Refund(ctx context.Context, request generation.RefundRequest) (generation.RefundResult, error) {
	if a.Store == nil {
		return generation.RefundResult{}, fmt.Errorf("billing store is required")
	}
	result, err := a.Store.Refund(ctx, billing.MutationRequest{TransactionID: "refund:" + request.JobID, UserID: request.UserID, IdempotencyKey: request.IdempotencyKey, ReferenceID: request.BillingTransactionID, OperationID: request.OperationID, BatchID: request.BatchID, JobID: request.JobID, Points: request.Points})
	if err != nil {
		retryable := !(errors.Is(err, billing.ErrAccountNotFound) || errors.Is(err, billing.ErrTransactionNotFound) || errors.Is(err, billing.ErrRefundExceedsCharge) || errors.Is(err, billing.ErrInsufficientPoints) || errors.Is(err, billing.ErrIdempotencyConflict))
		return generation.RefundResult{}, generation.NewExecutionError(generation.ErrorStageBilling, "billing_refund_failed", retryable, err)
	}
	balance := result.Transaction.BalanceAfter
	return generation.RefundResult{TransactionID: result.Transaction.ID, RefundedPoints: result.Transaction.Points, BalanceAfter: &balance}, nil
}

type DefaultCostCalculator struct{ Points map[string]int64 }

func (c DefaultCostCalculator) Calculate(_ context.Context, job generation.GenerationJob, result generation.ProviderResult) (int64, map[string]int64, error) {
	if result.UsageReported || result.ActualPoints > 0 || len(result.CostBreakdown) > 0 {
		return result.ActualPoints, result.CostBreakdown, nil
	}
	points := c.Estimate(job)
	return points, map[string]int64{strings.ToLower(job.MediaKind): points}, nil
}

func (c DefaultCostCalculator) Estimate(job generation.GenerationJob) int64 {
	points, configured := c.Points[strings.ToLower(job.MediaKind)]
	if !configured {
		switch strings.ToLower(job.MediaKind) {
		case "video":
			points = 120
		case "audio", "music", "voice":
			points = 30
		default:
			points = 12
		}
	}
	return points * generation.OutputVariantCount(job)
}

var _ generation.BindingGuard = BindingGuard{}
var _ generation.FinalizationCommitter = StoryboardBindingAdapter{}
var _ generation.FinalizationCommitInspector = StoryboardBindingAdapter{}
var _ generation.ResultDiscarder = StoryboardBindingAdapter{}
var _ generation.BillingGateway = BillingAdapter{}
var _ generation.CostCalculator = DefaultCostCalculator{}
