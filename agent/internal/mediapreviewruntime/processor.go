package mediapreviewruntime

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
)

var stableErrorCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// ProcessorConfig 冻结媒体请求在共享 Coordinator 下的 Lease 与恢复预算。
type ProcessorConfig struct {
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RecoveryDelay     time.Duration
	MaxAttempts       int
}

// Processor 是 generate 或 assemble 的 source-specific 单步 Handler，不拥有 Scanner 生命周期。
type Processor struct {
	repository Repository
	runner     Runner
	clock      Clock
	owner      string
	sourceType string
	config     ProcessorConfig
}

// NewProcessor 创建一个只领取指定媒体请求来源的 Handler。
func NewProcessor(repository Repository, runner Runner, clock Clock, owner, sourceType string, config ProcessorConfig) (*Processor, error) {
	if repository == nil || runner == nil || clock == nil || owner == "" ||
		(sourceType != GenerateSourceType && sourceType != AssembleSourceType) ||
		config.LeaseDuration <= 0 || config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration ||
		config.RecoveryDelay <= 0 || config.MaxAttempts < 1 {
		return nil, fmt.Errorf("create media preview processor: invalid dependency or budget")
	}
	return &Processor{repository: repository, runner: runner, clock: clock, owner: owner, sourceType: sourceType, config: config}, nil
}

// ProcessNext 尝试一次全来源 HOL Claim；命中后同步执行确定性媒体 Graph。
func (p *Processor) ProcessNext(ctx context.Context) (bool, error) {
	claim, err := p.repository.ClaimNext(ctx, p.sourceType, p.owner, p.config.LeaseDuration)
	if err != nil || claim == nil {
		return false, err
	}
	p.process(ctx, *claim)
	return true, nil
}

func (p *Processor) process(ctx context.Context, claim Claim) {
	if validateClaim(claim) != nil || !claim.DeadlineAt.After(p.clock.Now().UTC()) {
		_ = p.repository.CompleteRuntimeFailure(ctx, claim, "MEDIA_PREVIEW_RUNTIME_FAILED")
		return
	}
	if err := p.repository.MarkRunning(ctx, claim); err != nil {
		return
	}
	result, runErr, leaseErr := p.runWithHeartbeat(ctx, claim)
	if leaseErr != nil || ctx.Err() != nil {
		return
	}
	if runErr == nil {
		if err := p.repository.CompleteGraphResult(ctx, claim, result); err != nil &&
			!errors.Is(err, ErrFenceLost) {
			_ = p.repository.DeferInputRecovery(ctx, claim, p.config.RecoveryDelay)
		}
		return
	}
	if errors.Is(runErr, mediapreview.ErrUnknownOutcome) {
		_ = p.repository.DeferInputRecovery(ctx, claim, p.config.RecoveryDelay)
		return
	}
	if errors.Is(runErr, context.DeadlineExceeded) && ctx.Err() == nil {
		_ = p.repository.CompleteRuntimeFailure(ctx, claim, "MEDIA_PREVIEW_RUNTIME_FAILED")
		return
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return
	}
	if claim.Attempts < p.config.MaxAttempts {
		_ = p.repository.DeferInputRecovery(ctx, claim, p.config.RecoveryDelay)
		return
	}
	_ = p.repository.CompleteRuntimeFailure(ctx, claim, "MEDIA_PREVIEW_RUNTIME_FAILED")
}

func (p *Processor) runWithHeartbeat(ctx context.Context, claim Claim) (mediapreview.GraphToolResult, error, error) {
	runCtx, cancel := context.WithDeadline(ctx, claim.DeadlineAt)
	defer cancel()
	type outcome struct {
		result mediapreview.GraphToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := p.runner.Run(runCtx, claim); done <- outcome{result, err} }()
	ticker := time.NewTicker(p.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case value := <-done:
			return value.result, value.err, nil
		case <-ticker.C:
			if err := p.repository.RenewLease(runCtx, claim, p.config.LeaseDuration); err != nil {
				cancel()
				if runCtx.Err() != nil {
					return mediapreview.GraphToolResult{}, runCtx.Err(), nil
				}
				return mediapreview.GraphToolResult{}, nil, err
			}
		case <-runCtx.Done():
			cancel()
			return mediapreview.GraphToolResult{}, runCtx.Err(), nil
		}
	}
}

// TerminalProcessor 先 AppendOnce 桥接一个 Worker 终态，再消费当前 Session HOL 终态 Input。
type TerminalProcessor struct {
	repository    Repository
	owner         string
	leaseDuration time.Duration
}

// NewTerminalProcessor 创建无 ChatModel/Graph 的确定性终态 Handler。
func NewTerminalProcessor(repository Repository, owner string, leaseDuration time.Duration) (*TerminalProcessor, error) {
	if repository == nil || owner == "" || leaseDuration <= 0 {
		return nil, fmt.Errorf("create media terminal processor: invalid dependency")
	}
	return &TerminalProcessor{repository: repository, owner: owner, leaseDuration: leaseDuration}, nil
}

// ProcessNext 将 pending Outbox 追加到 Lane，并投影一个真正 HOL Terminal Input。
func (p *TerminalProcessor) ProcessNext(ctx context.Context) (bool, error) {
	bridged, err := p.repository.BridgeNextTerminal(ctx)
	if err != nil {
		return false, err
	}
	claim, err := p.repository.ClaimNextTerminal(ctx, p.owner, p.leaseDuration)
	if err != nil || claim == nil {
		return bridged, err
	}
	result, err := DecodeTerminalResult(claim.ResultJSON, claim.ResultDigest, claim.TerminalStatus)
	if err == nil {
		err = ValidateTerminalResultBinding(*claim, result)
	}
	if err != nil {
		// 冻结 Outbox 契约损坏或成功资产与 Job target 不一致时，只投影安全的 runtime_failed Card。
		result = TerminalResult{SchemaVersion: mediapreview.JobResultSchemaVersion, Status: "failed", ErrorCode: "MEDIA_PREVIEW_RUNTIME_FAILED"}
	}
	if err := p.repository.CompleteTerminal(ctx, *claim, result); err != nil {
		return true, err
	}
	return true, nil
}

// DecodeTerminalResult 通过 canonical struct 复算 Worker digest 并校验 succeeded/failed exact union。
func DecodeTerminalResult(encoded []byte, expectedDigest, expectedStatus string) (TerminalResult, error) {
	var result TerminalResult
	if err := strictDecode(encoded, &result); err != nil || result.SchemaVersion != mediapreview.JobResultSchemaVersion ||
		result.Status != expectedStatus || !mediapreview.ValidDigest(expectedDigest) {
		return TerminalResult{}, ErrOutputContract
	}
	switch result.Status {
	case "succeeded":
		if result.AssetRef == nil || !mediapreview.ValidUUIDv7(result.AssetRef.AssetID) ||
			result.AssetRef.Version != 1 || result.AssetRef.Status != "ready" ||
			!validMediaPair(result.AssetRef.MediaKind, result.AssetRef.MIMEType) ||
			!mediapreview.ValidDigest(result.AssetRef.ContentDigest) || result.AssetRef.SizeBytes <= 0 ||
			!mediapreview.ValidUUIDv7(result.FinalizationReceiptID) || result.ErrorCode != "" {
			return TerminalResult{}, ErrOutputContract
		}
	case "failed":
		if result.AssetRef != nil || result.FinalizationReceiptID != "" || !stableErrorCode.MatchString(result.ErrorCode) {
			return TerminalResult{}, ErrOutputContract
		}
	default:
		return TerminalResult{}, ErrOutputContract
	}
	canonical, err := mediapreview.CanonicalJSON(result)
	if err != nil || digest(canonical) != expectedDigest {
		return TerminalResult{}, ErrOutputContract
	}
	return result, nil
}

// ValidateTerminalResultBinding 把 Worker 成功终态绑定到 Agent Job 冻结的 target、执行类型与 Tool。
// 该校验必须在任何 Card/Event 投影前执行，避免合法形状但属于其他 Job 或媒体类型的资产越权串单。
func ValidateTerminalResultBinding(claim TerminalClaim, result TerminalResult) error {
	expectedToolKey, expectedMediaKind, expectedMIMEType, ok := terminalContractForJobType(claim.JobType)
	if !ok || claim.ToolKey != expectedToolKey || !mediapreview.ValidUUIDv7(claim.AssetID) || claim.AssetVersion != 1 {
		return ErrOutputContract
	}
	if result.Status != "succeeded" {
		return nil
	}
	if result.AssetRef == nil || result.AssetRef.AssetID != claim.AssetID || result.AssetRef.Version != claim.AssetVersion ||
		result.AssetRef.MediaKind != expectedMediaKind || result.AssetRef.MIMEType != expectedMIMEType {
		return ErrOutputContract
	}
	return nil
}

// terminalContractForJobType 返回已批准 Job 类型唯一对应的 Tool 与媒体类型；未知类型失败关闭。
func terminalContractForJobType(jobType string) (toolKey, mediaKind, mimeType string, ok bool) {
	switch jobType {
	case mediapreview.JobTypeGeneratePNG:
		return mediapreview.GenerateMediaToolKey, "image", "image/png", true
	case mediapreview.JobTypeAssembleMP4:
		return mediapreview.AssembleOutputToolKey, "video", "video/mp4", true
	default:
		return "", "", "", false
	}
}

func validMediaPair(kind, mime string) bool {
	return (kind == "image" && mime == "image/png") || (kind == "video" && mime == "video/mp4")
}
