package generation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type JobStore interface {
	Get(ctx context.Context, jobID string) (GenerationJob, error)
	UpdateStatus(ctx context.Context, jobID string, status string, update StatusUpdate) (GenerationJob, error)
}

type JobQueue interface {
	Dequeue(ctx context.Context, timeout time.Duration) (QueuePayload, bool, error)
}

type AssetStore interface {
	Get(ctx context.Context, assetID string) (asset.Asset, error)
}

type StoryboardStore interface {
	Get(ctx context.Context, storyboardID string) (storyboard.Storyboard, error)
	ApplyPatch(ctx context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error)
}

const (
	EventJobStatus       = "job.status"
	EventStoryboardPatch = "storyboard.patch"
)

type WorkerEvent struct {
	ID           string
	SessionID    string
	Event        string
	SurfaceID    string
	DataModelKey string
	Payload      any
	CreatedAt    time.Time
}

type StoryboardPatchPayload struct {
	StoryboardID string              `json:"storyboard_id"`
	BaseVersion  int                 `json:"base_version"`
	NextVersion  int                 `json:"next_version"`
	Ops          []patch.JSONPatchOp `json:"ops"`
	Source       string              `json:"source"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
}

type EventPublisher interface {
	Publish(ctx context.Context, event WorkerEvent) error
}

type HandlerResult struct {
	AssetIDs []string
	Result   map[string]any
}

type JobHandler interface {
	Handle(ctx context.Context, job GenerationJob) (HandlerResult, error)
}

type JobHandlerFunc func(ctx context.Context, job GenerationJob) (HandlerResult, error)

func (f JobHandlerFunc) Handle(ctx context.Context, job GenerationJob) (HandlerResult, error) {
	return f(ctx, job)
}

type WorkerConfig struct {
	Store       JobStore
	Queue       JobQueue
	Assets      AssetStore
	Storyboards StoryboardStore
	Events      EventPublisher
	Handlers    map[string]JobHandler
	NewID       func() string
	PollTimeout time.Duration
	Concurrency int
}

type Worker struct {
	cfg WorkerConfig
}

func NewWorker(cfg WorkerConfig) *Worker {
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = time.Second
	}
	if cfg.Handlers == nil {
		cfg.Handlers = map[string]JobHandler{}
	}
	if cfg.NewID == nil {
		cfg.NewID = defaultID
	}
	return &Worker{cfg: cfg}
}

func (w *Worker) Run(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("generation worker is required")
	}
	if w.cfg.Concurrency <= 1 {
		return w.runLoop(ctx)
	}
	var wg sync.WaitGroup
	for i := 0; i < w.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.runLoop(ctx)
		}()
	}
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (w *Worker) runLoop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, _ = w.RunOnce(ctx)
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.cfg.Store == nil {
		return false, fmt.Errorf("generation worker store is required")
	}
	if w.cfg.Queue == nil {
		return false, fmt.Errorf("generation worker queue is required")
	}
	payload, ok, err := w.cfg.Queue.Dequeue(ctx, w.cfg.PollTimeout)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	job, err := w.cfg.Store.Get(ctx, payload.JobID)
	if err != nil {
		return true, err
	}
	if IsTerminalJobStatus(job.Status) {
		return true, nil
	}
	running, err := w.cfg.Store.UpdateStatus(ctx, job.ID, StatusRunning, StatusUpdate{})
	if err != nil {
		return true, err
	}
	w.publishJobStatus(ctx, running)
	handler := w.cfg.Handlers[strings.TrimSpace(running.Provider)]
	if handler == nil {
		err := fmt.Errorf("generation handler is not registered for provider %q", running.Provider)
		failed, _ := w.cfg.Store.UpdateStatus(ctx, running.ID, StatusFailed, StatusUpdate{
			ErrorCode:    "handler_not_registered",
			ErrorMessage: err.Error(),
		})
		w.publishJobStatus(ctx, failed)
		return true, err
	}
	result, err := handler.Handle(ctx, running)
	if err != nil {
		failed, _ := w.cfg.Store.UpdateStatus(ctx, running.ID, StatusFailed, StatusUpdate{
			ErrorCode:    "provider_error",
			ErrorMessage: err.Error(),
		})
		w.publishJobStatus(ctx, failed)
		return true, err
	}
	finalizing := running
	finalizing.ResultAssetIDs = append([]string(nil), result.AssetIDs...)
	finalizing.Result = result.Result
	if err := w.syncStoryboardAssets(ctx, finalizing); err != nil {
		failed, _ := w.cfg.Store.UpdateStatus(ctx, running.ID, StatusFailed, StatusUpdate{
			ResultAssetIDs: append([]string(nil), result.AssetIDs...),
			Result:         result.Result,
			ErrorStage:     ErrorStageBinding,
			ErrorCode:      ErrorFinalizeFailed,
			ErrorMessage:   err.Error(),
		})
		w.publishJobStatus(ctx, failed)
		return true, err
	}
	succeeded, err := w.cfg.Store.UpdateStatus(ctx, running.ID, StatusSucceeded, StatusUpdate{
		ResultAssetIDs: append([]string(nil), result.AssetIDs...),
		Result:         result.Result,
	})
	if err != nil {
		return true, err
	}
	w.publishJobStatus(ctx, succeeded)
	return true, nil
}

func (w *Worker) publishJobStatus(ctx context.Context, job GenerationJob) {
	if w.cfg.Events == nil || strings.TrimSpace(job.SessionID) == "" {
		return
	}
	_ = w.cfg.Events.Publish(ctx, WorkerEvent{
		ID:           w.cfg.NewID(),
		SessionID:    job.SessionID,
		Event:        EventJobStatus,
		SurfaceID:    "storyboard",
		DataModelKey: "jobs",
		Payload:      NewJobStatusPayload(job),
		CreatedAt:    time.Now(),
	})
}

func (w *Worker) syncStoryboardAssets(ctx context.Context, job GenerationJob) error {
	if w.cfg.Assets == nil || w.cfg.Storyboards == nil || strings.TrimSpace(job.StoryboardID) == "" || len(job.ResultAssetIDs) == 0 {
		return nil
	}
	board, err := w.cfg.Storyboards.Get(ctx, job.StoryboardID)
	if err != nil {
		return err
	}
	for i, assetID := range job.ResultAssetIDs {
		record, err := w.cfg.Assets.Get(ctx, assetID)
		if err != nil {
			return err
		}
		ops, err := storyboard.AssetBindingOps(board, storyboard.AssetBindingRequest{
			AssetID:    record.ID,
			AssetKind:  record.Kind,
			TargetType: job.TargetType,
			TargetID:   job.TargetID,
			Field:      jobPayloadString(job.Payload, "field"),
		})
		if err != nil {
			return err
		}
		req := storyboard.PatchRequest{
			EventID:      valueOrDefault(w.cfg.NewID(), fmt.Sprintf("%s-storyboard-sync-%d", job.ID, i+1)),
			SessionID:    job.SessionID,
			StoryboardID: job.StoryboardID,
			BaseVersion:  board.Version,
			Source:       "worker",
			ToolCallID:   job.ToolCallID,
			Ops:          ops,
		}
		patched, event, err := w.cfg.Storyboards.ApplyPatch(ctx, req)
		if err != nil {
			return err
		}
		w.publishStoryboardPatch(ctx, event, req)
		board = patched
	}
	return nil
}

func (w *Worker) publishStoryboardPatch(ctx context.Context, event storyboard.EventRecord, req storyboard.PatchRequest) {
	if w.cfg.Events == nil || strings.TrimSpace(req.SessionID) == "" {
		return
	}
	_ = w.cfg.Events.Publish(ctx, WorkerEvent{
		ID:           w.cfg.NewID(),
		SessionID:    req.SessionID,
		Event:        EventStoryboardPatch,
		SurfaceID:    "storyboard",
		DataModelKey: "storyboard",
		Payload: StoryboardPatchPayload{
			StoryboardID: req.StoryboardID,
			BaseVersion:  req.BaseVersion,
			NextVersion:  event.NextVersion,
			Ops:          append([]patch.JSONPatchOp(nil), req.Ops...),
			Source:       req.Source,
			ToolCallID:   req.ToolCallID,
		},
		CreatedAt: time.Now(),
	})
}

func jobPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

type JobStatusPayload struct {
	JobID              string   `json:"job_id"`
	BatchID            string   `json:"batch_id,omitempty"`
	OperationID        string   `json:"operation_id,omitempty"`
	SessionID          string   `json:"session_id"`
	StoryboardID       string   `json:"storyboard_id,omitempty"`
	ToolCallID         string   `json:"tool_call_id,omitempty"`
	StageKey           string   `json:"stage_key,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	TargetType         string   `json:"target_type,omitempty"`
	TargetID           string   `json:"target_id,omitempty"`
	AssetSlot          string   `json:"asset_slot,omitempty"`
	Status             string   `json:"status"`
	Phase              string   `json:"phase,omitempty"`
	StatusVersion      int      `json:"status_version"`
	ResultAssetIDs     []string `json:"result_asset_ids,omitempty"`
	ResultDisposition  string   `json:"result_disposition,omitempty"`
	GrossChargedPoints int64    `json:"gross_charged_points,omitempty"`
	RefundedPoints     int64    `json:"refunded_points,omitempty"`
	NetChargedPoints   int64    `json:"net_charged_points,omitempty"`
	ErrorCode          string   `json:"error_code,omitempty"`
	ErrorMessage       string   `json:"error_message,omitempty"`
}

func NewJobStatusPayload(job GenerationJob) JobStatusPayload {
	return JobStatusPayload{
		JobID:              job.ID,
		BatchID:            job.BatchID,
		OperationID:        job.OperationID,
		SessionID:          job.SessionID,
		StoryboardID:       job.StoryboardID,
		ToolCallID:         job.ToolCallID,
		StageKey:           jobPayloadString(job.Payload, "stage_key"),
		Provider:           job.Provider,
		TargetType:         job.TargetType,
		TargetID:           job.TargetID,
		AssetSlot:          job.AssetSlot,
		Status:             job.Status,
		Phase:              job.Phase,
		StatusVersion:      job.StatusVersion,
		ResultAssetIDs:     append([]string(nil), job.ResultAssetIDs...),
		ResultDisposition:  job.ResultDisposition,
		GrossChargedPoints: job.ChargedPoints,
		RefundedPoints:     job.CompensatedPoints,
		NetChargedPoints:   job.NetChargedPoints,
		ErrorCode:          job.ErrorCode,
		ErrorMessage:       job.ErrorMessage,
	}
}

func defaultID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func valueOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
