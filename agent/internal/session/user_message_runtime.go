package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// UserMessageRuntimeProfileV2Preview1 是本地方案 A 的精确启用 Profile。
	UserMessageRuntimeProfileV2Preview1 = "user_message.runtime.v2preview1"
	// UserMessageContextSchemaV2Preview1 是方案 A 的最小上下文版本，不等同完整生产 Context。
	UserMessageContextSchemaV2Preview1 = "user_message.turn_context.v2preview1"
)

// UserMessageRuntimeProfile 冻结方案 A 在入队事务可引用的全部版本与摘要。
type UserMessageRuntimeProfile struct {
	Enabled             bool
	Profile             string
	ContextSchema       string
	PromptRef           string
	PromptDigest        string
	ToolRegistryRef     string
	ToolRegistryDigest  string
	RuntimePolicyRef    string
	RuntimePolicyDigest string
	ModelRouteRef       string
	ModelRouteDigest    string
	BudgetRef           string
	BudgetDigest        string
}

// Validate 拒绝任何非方案 A、非空工具集或不完整摘要的静默扩权。
func (profile UserMessageRuntimeProfile) Validate() error {
	if !profile.Enabled {
		return nil
	}
	if profile.Profile != UserMessageRuntimeProfileV2Preview1 ||
		profile.ContextSchema != UserMessageContextSchemaV2Preview1 ||
		profile.PromptRef == "" || profile.ToolRegistryRef != "user_message.empty_tools@v1" ||
		profile.RuntimePolicyRef == "" || profile.ModelRouteRef != "local.fake.user_message@v1" ||
		profile.BudgetRef == "" {
		return fmt.Errorf("%w: invalid user message runtime profile", ErrInvalidCommand)
	}
	for _, digest := range []string{
		profile.PromptDigest, profile.ToolRegistryDigest, profile.RuntimePolicyDigest,
		profile.ModelRouteDigest, profile.BudgetDigest,
	} {
		if !validSHA256Hex(digest) {
			return fmt.Errorf("%w: invalid user message runtime profile digest", ErrInvalidCommand)
		}
	}
	return nil
}

// UserMessageTurn 保存 Ensure 事务预分配的稳定 Turn 与回执/事件标识。
type UserMessageTurn struct {
	TurnID          string
	InputID         string
	SessionID       string
	MessageID       string
	UserID          string
	ProjectID       string
	OutputID        string
	ModelCallID     string
	RecoveryEventID string
	TerminalEventID string
	Status          string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// UserMessageContext 是与 Message/Input/Turn 同事务冻结的最小方案 A Context。
type UserMessageContext struct {
	TurnID               string
	SchemaVersion        string
	SessionID            string
	InputID              string
	MessageID            string
	UserID               string
	ProjectID            string
	MessageCutoffSeq     int64
	MessageContentDigest string
	SkillSnapshotRef     string
	SkillSnapshotDigest  string
	PromptRef            string
	PromptDigest         string
	ToolRegistryRef      string
	ToolRegistryDigest   string
	RuntimePolicyRef     string
	RuntimePolicyDigest  string
	ModelRouteRef        string
	ModelRouteDigest     string
	BudgetRef            string
	BudgetDigest         string
	AccessScopeRef       string
	AccessScopeDigest    string
	ContextDigest        string
	CreatedAt            time.Time
}

// UserMessageRuntimePlan 聚合入队事务必须同时提交的 Turn 与最小 Context。
type UserMessageRuntimePlan struct {
	Turn    UserMessageTurn
	Context UserMessageContext
}

type userMessageContextWire struct {
	SchemaVersion        string `json:"schema_version"`
	SessionID            string `json:"session_id"`
	InputID              string `json:"input_id"`
	MessageID            string `json:"message_id"`
	TurnID               string `json:"turn_id"`
	UserID               string `json:"user_id"`
	ProjectID            string `json:"project_id"`
	MessageCutoffSeq     int64  `json:"message_cutoff_seq"`
	MessageContentDigest string `json:"message_content_digest"`
	SkillSnapshotRef     string `json:"skill_snapshot_ref"`
	SkillSnapshotDigest  string `json:"skill_snapshot_digest"`
	PromptRef            string `json:"prompt_ref"`
	PromptDigest         string `json:"prompt_digest"`
	ToolRegistryRef      string `json:"tool_registry_ref"`
	ToolRegistryDigest   string `json:"tool_registry_digest"`
	RuntimePolicyRef     string `json:"runtime_policy_ref"`
	RuntimePolicyDigest  string `json:"runtime_policy_digest"`
	ModelRouteRef        string `json:"model_route_ref"`
	ModelRouteDigest     string `json:"model_route_digest"`
	BudgetRef            string `json:"budget_ref"`
	BudgetDigest         string `json:"budget_digest"`
	AccessScopeRef       string `json:"access_scope_ref"`
	AccessScopeDigest    string `json:"access_scope_digest"`
}

// DigestUserMessageContext 对全部具名字段按固定结构编码，禁止依赖 map 顺序。
func DigestUserMessageContext(value UserMessageContext) (string, error) {
	wire := userMessageContextWire{
		SchemaVersion: value.SchemaVersion, SessionID: value.SessionID, InputID: value.InputID,
		MessageID: value.MessageID, TurnID: value.TurnID, UserID: value.UserID, ProjectID: value.ProjectID,
		MessageCutoffSeq: value.MessageCutoffSeq, MessageContentDigest: value.MessageContentDigest,
		SkillSnapshotRef: value.SkillSnapshotRef, SkillSnapshotDigest: value.SkillSnapshotDigest,
		PromptRef: value.PromptRef, PromptDigest: value.PromptDigest,
		ToolRegistryRef: value.ToolRegistryRef, ToolRegistryDigest: value.ToolRegistryDigest,
		RuntimePolicyRef: value.RuntimePolicyRef, RuntimePolicyDigest: value.RuntimePolicyDigest,
		ModelRouteRef: value.ModelRouteRef, ModelRouteDigest: value.ModelRouteDigest,
		BudgetRef: value.BudgetRef, BudgetDigest: value.BudgetDigest,
		AccessScopeRef: value.AccessScopeRef, AccessScopeDigest: value.AccessScopeDigest,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("%w: encode user message context", ErrInvalidCommand)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// attachUserMessageRuntimePlan 只在显式启用 Profile 且存在首 user_message 时创建冻结事实。
func (s *Service) attachUserMessageRuntimePlan(plan *EnsurePlan, now time.Time) error {
	if !s.userMessageRuntimeProfile.Enabled {
		return nil
	}
	if plan == nil || plan.Message == nil || plan.Input == nil {
		return nil
	}
	ids := make([]string, 5)
	for index, kind := range []string{"user message turn", "user message output", "user message model call", "user message recovery event", "user message terminal event"} {
		value, err := s.newUUIDv7(kind)
		if err != nil {
			return err
		}
		ids[index] = value
	}
	contextValue := UserMessageContext{
		TurnID: ids[0], SchemaVersion: s.userMessageRuntimeProfile.ContextSchema,
		SessionID: plan.Session.ID, InputID: plan.Input.ID, MessageID: plan.Message.ID,
		UserID: plan.Session.UserID, ProjectID: plan.Session.ProjectID,
		MessageCutoffSeq: plan.Message.Seq, MessageContentDigest: plan.Message.ContentDigest,
		SkillSnapshotRef:    "session_skill_snapshot:" + plan.Session.ID,
		SkillSnapshotDigest: plan.SkillSnapshot.Digest,
		PromptRef:           s.userMessageRuntimeProfile.PromptRef, PromptDigest: s.userMessageRuntimeProfile.PromptDigest,
		ToolRegistryRef:     s.userMessageRuntimeProfile.ToolRegistryRef,
		ToolRegistryDigest:  s.userMessageRuntimeProfile.ToolRegistryDigest,
		RuntimePolicyRef:    s.userMessageRuntimeProfile.RuntimePolicyRef,
		RuntimePolicyDigest: s.userMessageRuntimeProfile.RuntimePolicyDigest,
		ModelRouteRef:       s.userMessageRuntimeProfile.ModelRouteRef,
		ModelRouteDigest:    s.userMessageRuntimeProfile.ModelRouteDigest,
		BudgetRef:           s.userMessageRuntimeProfile.BudgetRef, BudgetDigest: s.userMessageRuntimeProfile.BudgetDigest,
		AccessScopeRef:    "ensure_command:" + plan.Receipt.CommandID,
		AccessScopeDigest: plan.Receipt.RequestDigest, CreatedAt: now,
	}
	digest, err := DigestUserMessageContext(contextValue)
	if err != nil {
		return err
	}
	contextValue.ContextDigest = digest
	plan.UserMessageRuntime = &UserMessageRuntimePlan{
		Turn: UserMessageTurn{
			TurnID: ids[0], InputID: plan.Input.ID, SessionID: plan.Session.ID, MessageID: plan.Message.ID,
			UserID: plan.Session.UserID, ProjectID: plan.Session.ProjectID,
			OutputID: ids[1], ModelCallID: ids[2], RecoveryEventID: ids[3], TerminalEventID: ids[4],
			Status: "created", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		Context: contextValue,
	}
	return nil
}

// validUserMessageRuntimePlan 是 Repository 入库前的强一致性防线。
func validUserMessageRuntimePlan(plan EnsurePlan) bool {
	runtimePlan := plan.UserMessageRuntime
	if runtimePlan == nil {
		return true
	}
	if plan.Message == nil || plan.Input == nil {
		return false
	}
	turn := runtimePlan.Turn
	contextValue := runtimePlan.Context
	if turn.TurnID == "" || turn.InputID != plan.Input.ID || turn.SessionID != plan.Session.ID ||
		turn.MessageID != plan.Message.ID || turn.UserID != plan.Session.UserID || turn.ProjectID != plan.Session.ProjectID ||
		turn.Status != "created" || turn.Version != 1 || turn.OutputID == "" || turn.ModelCallID == "" ||
		turn.RecoveryEventID == "" || turn.TerminalEventID == "" || turn.CreatedAt.IsZero() || turn.UpdatedAt.IsZero() {
		return false
	}
	if contextValue.TurnID != turn.TurnID || contextValue.SchemaVersion != UserMessageContextSchemaV2Preview1 ||
		contextValue.SessionID != turn.SessionID || contextValue.InputID != turn.InputID ||
		contextValue.MessageID != turn.MessageID || contextValue.UserID != turn.UserID ||
		contextValue.ProjectID != turn.ProjectID || contextValue.MessageCutoffSeq != plan.Message.Seq ||
		contextValue.MessageContentDigest != plan.Message.ContentDigest ||
		contextValue.SkillSnapshotDigest != plan.SkillSnapshot.Digest ||
		contextValue.ToolRegistryRef != "user_message.empty_tools@v1" ||
		!strings.HasPrefix(contextValue.AccessScopeRef, "ensure_command:") || contextValue.AccessScopeDigest != plan.Receipt.RequestDigest {
		return false
	}
	digest, err := DigestUserMessageContext(contextValue)
	return err == nil && digest == contextValue.ContextDigest
}

// ValidUserMessageRuntimePlanForRepository 允许 PostgreSQL Adapter 在事务前复核领域计划，不暴露 canonical wire。
func ValidUserMessageRuntimePlanForRepository(plan EnsurePlan) bool {
	return validUserMessageRuntimePlan(plan)
}
