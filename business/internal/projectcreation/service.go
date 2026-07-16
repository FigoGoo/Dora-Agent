// Package projectcreation 编排显式 QuickCreate v2 应用用例，并保持 W0 Project v1 服务不变。
package projectcreation

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/google/uuid"
)

var (
	// ErrV2Disabled 表示部署尚未确认 Agent v2 capability，调用方不得降级到 v1。
	ErrV2Disabled = errors.New("project Skill Snapshot v2 is disabled")
)

// QuickCreateV2Command 是 HTTP 边界传入显式 v2 应用服务的可信命令。
type QuickCreateV2Command struct {
	// OwnerUserID 只来自认证 Principal。
	OwnerUserID string
	// IdempotencyKey 来自严格 Header，服务只把摘要交给 Repository。
	IdempotencyKey string
	// InitialPrompt 是可选首提示词，仅在规范化和完整 Outbox 加密前短暂存在。
	InitialPrompt string
	// EnabledSkillIDs 是客户端显式非 nil 集合；顺序不构成优先级。
	EnabledSkillIDs []string
}

// Service 在事务外冻结 ID、时间、limits 与加密器，然后调用 Repository 原子入口。
type Service struct {
	repository       projectskillbinding.QuickCreateV2Repository
	clock            project.Clock
	idGenerator      project.IDGenerator
	protector        projectskillbinding.OutboxPayloadProtectorV2
	limits           projectskillbinding.LimitsV1
	maxOutboxAttempt int32
	enabled          bool
}

// NewService 创建 QuickCreate v2 应用服务；无论 feature 是否开启都校验依赖和 limits，避免运行期半配置。
func NewService(
	repository projectskillbinding.QuickCreateV2Repository,
	clock project.Clock,
	idGenerator project.IDGenerator,
	protector projectskillbinding.OutboxPayloadProtectorV2,
	limits projectskillbinding.LimitsV1,
	maxOutboxAttempt int32,
	enabled bool,
) (*Service, error) {
	if repository == nil || clock == nil || idGenerator == nil || protector == nil || maxOutboxAttempt <= 0 {
		return nil, errors.New("create QuickCreate v2 service: required dependency is missing")
	}
	if err := limits.Validate(); err != nil {
		return nil, fmt.Errorf("create QuickCreate v2 service: invalid limits: %w", err)
	}
	return &Service{
		repository: repository, clock: clock, idGenerator: idGenerator, protector: protector,
		limits: limits, maxOutboxAttempt: maxOutboxAttempt, enabled: enabled,
	}, nil
}

// QuickCreateV2 冻结显式 Skill 选择并可靠接受 v2 Session Bootstrap；失败绝不转调 QuickCreate v1。
func (service *Service) QuickCreateV2(ctx context.Context, command QuickCreateV2Command) (projectskillbinding.QuickCreateV2Result, error) {
	if !service.enabled {
		return projectskillbinding.QuickCreateV2Result{}, ErrV2Disabled
	}
	if command.EnabledSkillIDs == nil {
		return projectskillbinding.QuickCreateV2Result{}, projectskillbinding.ErrInvalidBinding
	}
	if len(command.EnabledSkillIDs) > service.limits.MaxItems {
		return projectskillbinding.QuickCreateV2Result{}, projectskillbinding.ErrSnapshotLimitExceeded
	}
	seenSkillIDs := make(map[string]struct{}, len(command.EnabledSkillIDs))
	for _, skillID := range command.EnabledSkillIDs {
		parsed, parseErr := uuid.Parse(skillID)
		if parseErr != nil || parsed.Version() != 7 || parsed.String() != skillID {
			return projectskillbinding.QuickCreateV2Result{}, projectskillbinding.ErrInvalidBinding
		}
		if _, duplicate := seenSkillIDs[skillID]; duplicate {
			return projectskillbinding.QuickCreateV2Result{}, projectskillbinding.ErrInvalidBinding
		}
		seenSkillIDs[skillID] = struct{}{}
	}
	keyDigestV1, err := project.CalculateIdempotencyKeyDigest(command.IdempotencyKey)
	if err != nil {
		return projectskillbinding.QuickCreateV2Result{}, err
	}
	var keyDigest projectskillbinding.Digest
	copy(keyDigest[:], keyDigestV1[:])

	// Project、Receipt、Session Binding、Command、Resolution 各一个 ID；每个 Skill 再生成 Binding 与 Audit ID。
	ids := make([]string, 5+2*len(command.EnabledSkillIDs))
	for index := range ids {
		ids[index], err = service.idGenerator.New()
		if err != nil {
			return projectskillbinding.QuickCreateV2Result{}, fmt.Errorf("generate QuickCreate v2 UUIDv7: %w", err)
		}
	}
	bindings := make([]projectskillbinding.BindingSeed, len(command.EnabledSkillIDs))
	for index, skillID := range command.EnabledSkillIDs {
		bindings[index] = projectskillbinding.BindingSeed{
			ID: ids[5+2*index], SkillID: skillID, AuditID: ids[5+2*index+1],
		}
	}
	prepared, err := projectskillbinding.NewQuickCreateV2Command(projectskillbinding.QuickCreateV2Seed{
		SchemaVersion: projectskillbinding.QuickCreateSchemaVersionV2,
		ProjectID:     ids[0], ReceiptID: ids[1], SessionBindingID: ids[2], CommandID: ids[3], ResolutionID: ids[4],
		OwnerUserID: command.OwnerUserID, InitialPrompt: command.InitialPrompt, KeyDigest: keyDigest,
		Bindings: bindings, MaxAttempts: service.maxOutboxAttempt, OccurredAt: service.clock.Now().UTC(),
	}, service.limits)
	if err != nil {
		return projectskillbinding.QuickCreateV2Result{}, err
	}
	return service.repository.CreateQuickV2(ctx, prepared, service.limits, service.protector)
}
