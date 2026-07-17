package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxIdempotencyKeyBytes = 128

// Clock 为 Project 应用服务提供可测试的 UTC 当前时间。
type Clock interface {
	// Now 返回当前时间；应用服务在写入领域事实前统一转换为 UTC。
	Now() time.Time
}

// IDGenerator 为一次 QuickCreate 生成 Project、Receipt、Binding 与 Command 的 UUIDv7。
type IDGenerator interface {
	// New 返回新的 UUIDv7；生成失败时 QuickCreate 不开始持久化事务。
	New() (string, error)
}

// PromptProtector 对规范化后的非空首提示词执行认证加密。
// 实现不得记录正文，返回的 PayloadDigest 必须等于调用方提供的明文摘要。
type PromptProtector interface {
	// Protect 保护一段已经完成 NFC 与长度校验的正文，并返回完整 AES-256-GCM 元数据。
	Protect(ctx context.Context, normalizedPrompt string, promptDigest Digest) (*EncryptedPayload, error)
}

// QuickCreateCommand 是 HTTP Handler 传入 Project 应用层的可信命令。
type QuickCreateCommand struct {
	// OwnerUserID 只来自 Business Auth Context，不接受请求体覆盖。
	OwnerUserID string
	// IdempotencyKey 是同一次用户创建意图复用的原始键，只在构造栈中计算摘要。
	IdempotencyKey string
	// InitialPrompt 是可选首提示词，只在构造和保护阶段短暂存在。
	InitialPrompt string
}

// Service 编排 Project QuickCreate 与 Bootstrap，不在数据库事务中执行外部 RPC。
type Service struct {
	repository       Repository
	clock            Clock
	idGenerator      IDGenerator
	promptProtector  PromptProtector
	maxOutboxAttempt int32
}

// NewService 创建 Project 应用服务并校验所有必需依赖与版本化 Outbox 尝试预算。
func NewService(repository Repository, clock Clock, idGenerator IDGenerator, promptProtector PromptProtector, maxOutboxAttempt int32) (*Service, error) {
	if repository == nil || clock == nil || idGenerator == nil || promptProtector == nil || maxOutboxAttempt <= 0 {
		return nil, errors.New("create project service: required dependency is missing")
	}
	return &Service{
		repository: repository, clock: clock, idGenerator: idGenerator,
		promptProtector: promptProtector, maxOutboxAttempt: maxOutboxAttempt,
	}, nil
}

// QuickCreate 先冻结幂等语义和密文，再调用 Repository 原子提交四类本地事实。
// 成功只代表 Business 已可靠接受命令；本方法不会等待 Agent RPC 或模型执行。
func (s *Service) QuickCreate(ctx context.Context, command QuickCreateCommand) (QuickCreateResult, error) {
	keyDigest, err := idempotencyKeyDigest(command.IdempotencyKey)
	if err != nil {
		return QuickCreateResult{}, err
	}
	normalizedPrompt, promptDigest, promptPresent, err := NormalizeEnsureSessionPrompt(command.InitialPrompt)
	if err != nil {
		return QuickCreateResult{}, err
	}

	var encryptedPayload *EncryptedPayload
	if promptPresent {
		encryptedPayload, err = s.promptProtector.Protect(ctx, normalizedPrompt, promptDigest)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return QuickCreateResult{}, context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return QuickCreateResult{}, context.DeadlineExceeded
			}
			// 保护器错误可能携带密钥服务地址或 Secret，只在此边界收敛为稳定错误。
			return QuickCreateResult{}, ErrPromptProtection
		}
		if encryptedPayload == nil || encryptedPayload.PayloadDigest != promptDigest {
			return QuickCreateResult{}, ErrPromptProtection
		}
	}

	ids := make([]string, 4)
	for index := range ids {
		ids[index], err = s.idGenerator.New()
		if err != nil {
			return QuickCreateResult{}, fmt.Errorf("generate quick create UUIDv7: %w", err)
		}
	}
	// 领域构造器会再次从真实 Prompt 重算摘要并核对密文，避免应用层或保护器伪造语义。
	aggregate, err := NewQuickCreateAggregate(QuickCreateSeed{
		ProjectID: ids[0], ReceiptID: ids[1], BindingID: ids[2], CommandID: ids[3],
		OwnerUserID: command.OwnerUserID, InitialPrompt: command.InitialPrompt, KeyDigest: keyDigest,
		EncryptedPayload: encryptedPayload, MaxAttempts: s.maxOutboxAttempt, OccurredAt: s.clock.Now().UTC(),
	})
	if err != nil {
		return QuickCreateResult{}, err
	}
	return s.repository.CreateQuick(ctx, aggregate)
}

// Bootstrap 读取当前可信用户拥有的 Project 与默认 Session 绑定；越权与不存在保持相同错误。
func (s *Service) Bootstrap(ctx context.Context, projectID string, ownerUserID string) (BootstrapResult, error) {
	if !isUUIDv7(projectID) || !isUUIDv7(ownerUserID) {
		return BootstrapResult{}, ErrProjectNotFound
	}
	return s.repository.FindBootstrapOwnedByID(ctx, projectID, ownerUserID)
}

// ListOwned 校验可信 owner 与有界 Keyset 查询后，返回当前用户可见的项目单页读模型。
// 非法 owner、limit 或 after 游标会在进入 Repository 前返回 ErrInvalidProjectListQuery。
func (s *Service) ListOwned(ctx context.Context, query ProjectListQuery) (ProjectListResult, error) {
	if err := query.Validate(); err != nil {
		return ProjectListResult{}, err
	}
	return s.repository.ListOwned(ctx, query)
}

// idempotencyKeyDigest 校验代理可安全转发的可见 ASCII 意图键，并计算不落原文的 SHA-256 摘要。
func idempotencyKeyDigest(value string) (Digest, error) {
	if value == "" || len(value) > maxIdempotencyKeyBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return Digest{}, ErrInvalidIdempotencyKey
	}
	for _, character := range value {
		if character > unicode.MaxASCII || unicode.IsControl(character) || unicode.IsSpace(character) {
			return Digest{}, ErrInvalidIdempotencyKey
		}
	}
	return SHA256Digest([]byte(value)), nil
}

// CalculateIdempotencyKeyDigest 为同属 Business 的版本化 Project 创建用例提供统一幂等键规范化。
// 返回值只包含摘要，原始 Header 不得进入持久化、日志、Trace 或跨 Module DTO。
func CalculateIdempotencyKeyDigest(value string) (Digest, error) {
	return idempotencyKeyDigest(value)
}
