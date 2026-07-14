package project

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrAgentSessionNotFound 表示 Agent Session 不存在、不属于当前用户、未 ready 或 Project 已不可读。
	ErrAgentSessionNotFound = errors.New("agent session not found or inaccessible")
)

// AgentSessionAccess 是 Business 所有权与 ready Binding JOIN 形成的最小内部读取授权事实。
type AgentSessionAccess struct {
	// ProjectID 是当前用户拥有且仍可读取的 Business Project UUIDv7。
	ProjectID string
	// AgentSessionID 是该 Project ready 默认绑定确认的 Agent Session UUIDv7。
	AgentSessionID string
}

// Validate 校验授权事实同时包含规范 UUIDv7；失败表示持久化投影不可信。
func (access AgentSessionAccess) Validate() error {
	if !isUUIDv7(access.ProjectID) || !isUUIDv7(access.AgentSessionID) {
		return ErrAgentSessionNotFound
	}
	return nil
}

// AgentSessionAccessRepository 定义 BFF 按可信用户与 Agent Session 查询 ready 绑定的最小持久化能力。
type AgentSessionAccessRepository interface {
	// FindReadyAgentSessionAccess 用一次集合查询核对 owner、可读 Project、ready Binding 和完整 Session 匹配。
	FindReadyAgentSessionAccess(ctx context.Context, ownerUserID string, agentSessionID string) (AgentSessionAccess, error)
}

// AgentSessionAccessService 负责在 BFF 签发身份断言前收敛资源级授权语义。
type AgentSessionAccessService struct {
	repository AgentSessionAccessRepository
}

// NewAgentSessionAccessService 校验 Repository 后创建只读资源授权服务。
func NewAgentSessionAccessService(repository AgentSessionAccessRepository) (*AgentSessionAccessService, error) {
	if repository == nil {
		return nil, fmt.Errorf("create agent session access service: repository is nil")
	}
	return &AgentSessionAccessService{repository: repository}, nil
}

// Resolve 按可信 Principal 与路由 Session 查询读取授权；非法、缺失、越权、未 ready 和不可读统一返回 ErrAgentSessionNotFound。
func (service *AgentSessionAccessService) Resolve(ctx context.Context, ownerUserID string, agentSessionID string) (AgentSessionAccess, error) {
	if !isUUIDv7(ownerUserID) || !isUUIDv7(agentSessionID) {
		return AgentSessionAccess{}, ErrAgentSessionNotFound
	}
	access, err := service.repository.FindReadyAgentSessionAccess(ctx, ownerUserID, agentSessionID)
	if err != nil {
		return AgentSessionAccess{}, err
	}
	if err := access.Validate(); err != nil || access.AgentSessionID != agentSessionID {
		// Repository 投影与路由不一致时失败关闭，不允许为另一个 Session 签发断言。
		return AgentSessionAccess{}, ErrAgentSessionNotFound
	}
	return access, nil
}
