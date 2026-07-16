package skill

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// GovernanceCapability 是受信 Governor Principal 必须持有的正式治理能力键。
	GovernanceCapability = "skill.govern"
	// GovernanceRoleKey 是治理审计和事务内 assignment 复核使用的固定角色键。
	GovernanceRoleKey = "skill_governor"
	// CommandTypeGovernanceTransition 是治理命令回执使用的稳定命令类型。
	CommandTypeGovernanceTransition = "governance_transition"
	// defaultGovernancePageSize 是治理工作队列固定页大小，禁止客户端放大单次查询。
	defaultGovernancePageSize = 20
)

var (
	// ErrInvalidGovernanceRequest 表示治理路径、查询、Body、来源地址或 Strong ETag 格式非法。
	ErrInvalidGovernanceRequest = errors.New("invalid skill governance request")
	// ErrGovernanceCapabilityRequired 表示当前权威 Principal 不持有 skill.govern。
	ErrGovernanceCapabilityRequired = errors.New("skill governance capability required")
	// ErrGovernanceNotFound 表示合法 Skill 不存在或从未发布。
	ErrGovernanceNotFound = errors.New("skill governance target not found")
	// ErrGovernanceConflict 表示当前发布指针、治理状态、治理纪元或 ETag 已不允许请求迁移。
	ErrGovernanceConflict = errors.New("skill governance conflict")
)

// GovernanceAction 是 Skill 治理状态机允许的命令动作闭集。
type GovernanceAction string

const (
	// GovernanceActionSuspend 将 active Skill 暂停为 suspended。
	GovernanceActionSuspend GovernanceAction = "suspend"
	// GovernanceActionResume 将 suspended Skill 恢复为 active。
	GovernanceActionResume GovernanceAction = "resume"
	// GovernanceActionOffline 将 active 或 suspended Skill 永久下架为 offline。
	GovernanceActionOffline GovernanceAction = "offline"
)

var governanceReasonCodes = map[GovernanceAction]map[string]struct{}{
	GovernanceActionSuspend: {
		"content_safety": {}, "copyright_risk": {}, "privacy_risk": {}, "fraud_or_abuse": {},
		"tool_dependency_risk": {}, "policy_violation": {}, "incident_containment": {},
	},
	GovernanceActionResume: {
		"risk_cleared": {}, "appeal_approved": {}, "incident_resolved": {},
		"dependency_restored": {}, "policy_remediated": {},
	},
	GovernanceActionOffline: {
		"content_safety": {}, "copyright_risk": {}, "privacy_risk": {}, "fraud_or_abuse": {},
		"tool_dependency_risk": {}, "policy_violation": {}, "owner_request": {}, "repeated_violation": {},
	},
}

var governanceApprovalReferencePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,31}-[A-Za-z0-9][A-Za-z0-9._-]{0,126}$`)

// GovernancePrincipal 是动态 Session Resolver 交给治理应用服务的最小可信身份。
type GovernancePrincipal struct {
	// UserID 是 Governor 用户 UUIDv7。
	UserID string
	// Capabilities 是本次请求重新解析的权威能力集合。
	Capabilities []string
}

// GovernanceQueueBoundary 是治理列表按当前发布时间倒序分页的内部边界。
type GovernanceQueueBoundary struct {
	// PublishedAt 是上一页最后一项当前发布快照的 UTC 时间。
	PublishedAt time.Time
	// PublishedSnapshotID 是同一发布时间下的稳定 UUIDv7 次级排序键。
	PublishedSnapshotID string
}

// GovernanceQueueItem 是 Repository 从当前发布快照派生的最小治理列表行。
type GovernanceQueueItem struct {
	// SkillID 是已发布 Skill UUIDv7。
	SkillID string
	// PublishedSnapshotID 是当前发布快照 UUIDv7，仅用于分页和完整性校验。
	PublishedSnapshotID string
	// Name 是当前发布 Definition 的名称。
	Name string
	// Summary 是当前发布 Definition 的摘要。
	Summary string
	// Category 是当前发布 Definition 的分类。
	Category string
	// PublishedAt 是当前发布快照 UTC 发布时间。
	PublishedAt time.Time
	// GovernanceStatus 是当前治理状态。
	GovernanceStatus GovernanceStatus
	// GovernanceEpoch 是当前治理有效性纪元。
	GovernanceEpoch int64
}

// GovernanceQueuePage 是 Repository 使用一次集合查询返回的治理列表页。
type GovernanceQueuePage struct {
	// Items 是当前页列表行，必须按发布时间和快照 ID 倒序排列。
	Items []GovernanceQueueItem
	// HasMore 表示 Repository 读取到 limit+1 行。
	HasMore bool
}

// GovernanceState 是管理详情和治理事务使用的当前发布权威投影。
type GovernanceState struct {
	// SkillID 是 Skill 聚合 UUIDv7。
	SkillID string
	// CurrentPublishedSnapshotID 是聚合当前发布逻辑指针。
	CurrentPublishedSnapshotID string
	// PublicationRevision 是聚合当前内部发布修订。
	PublicationRevision int64
	// GovernanceStatus 是当前治理状态。
	GovernanceStatus GovernanceStatus
	// GovernanceEpoch 是当前治理有效性纪元。
	GovernanceEpoch int64
	// SkillVersion 是 Skill 聚合 CAS 版本。
	SkillVersion int64
	// Published 是 current pointer 指向的不可变发布快照。
	Published PublishedSnapshot
}

// GovernanceTransitionRepositoryCommand 是应用服务校验后交给 Repository 的原子治理命令。
type GovernanceTransitionRepositoryCommand struct {
	// GovernorUserID 是来自可信 Principal 的 Governor UUIDv7。
	GovernorUserID string
	// SkillID 是待治理 Skill UUIDv7。
	SkillID string
	// Action 是 suspend、resume 或 offline。
	Action GovernanceAction
	// ReasonCode 是与 Action 匹配的稳定闭集原因代码。
	ReasonCode string
	// ApprovalReference 是规范外部工单引用。
	ApprovalReference string
	// SourceAddress 是 HTTP 直连 peer 的规范 IPv4/IPv6 地址。
	SourceAddress string
	// IfMatch 是客户端原样提交的单个 Strong Governance ETag。
	IfMatch string
	// RequestID 是本次 HTTP 请求的服务端 UUIDv7。
	RequestID string
	// ReceiptID 是应用服务在事务前生成的治理回执 UUIDv7。
	ReceiptID string
	// AuditID 是应用服务在事务前生成的追加审计 UUIDv7。
	AuditID string
	// KeyDigest 是原始 Idempotency-Key 的 SHA-256 摘要。
	KeyDigest Digest
	// SemanticDigest 是治理业务语义的版本化 SHA-256 摘要。
	SemanticDigest Digest
	// TransitionedAt 是首次迁移冻结的 UTC 时间。
	TransitionedAt time.Time
}

// GovernanceTransitionRepositoryResult 是首次治理迁移或同义回执重放的冻结结果。
type GovernanceTransitionRepositoryResult struct {
	// SkillID 是被治理 Skill UUIDv7。
	SkillID string
	// PublishedSnapshotID 是首次响应冻结的 current published snapshot UUIDv7。
	PublishedSnapshotID string
	// GovernanceStatus 是首次响应冻结的迁移后状态。
	GovernanceStatus GovernanceStatus
	// GovernanceEpoch 是首次响应冻结的迁移后纪元。
	GovernanceEpoch int64
	// TransitionedAt 是首次可靠提交时间。
	TransitionedAt time.Time
	// IdempotentReplay 表示本次没有新增状态、回执或审计事实。
	IdempotentReplay bool
}

// GovernanceRepository 定义独立于 Owner/Reviewer 大接口的最小治理持久化边界。
type GovernanceRepository interface {
	// ListGovernance 使用一次集合查询读取指定治理状态的已发布 Skill。
	ListGovernance(ctx context.Context, status GovernanceStatus, boundary *GovernanceQueueBoundary, limit int) (GovernanceQueuePage, error)
	// FindGovernanceDetail 使用一次集合查询读取 Skill 与 current published snapshot。
	FindGovernanceDetail(ctx context.Context, skillID string) (GovernanceState, error)
	// TransitionGovernance 在固定锁序事务中完成授权复核、回执重放、迁移和追加审计。
	TransitionGovernance(ctx context.Context, command GovernanceTransitionRepositoryCommand) (GovernanceTransitionRepositoryResult, error)
}

// GovernanceQueueItemDTO 是治理列表对管理前端公开的安全投影。
type GovernanceQueueItemDTO struct {
	// SkillID 是 Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// Name 是当前发布名称。
	Name string `json:"name"`
	// Summary 是当前发布摘要。
	Summary string `json:"summary"`
	// Category 是当前发布分类。
	Category string `json:"category"`
	// PublishedAt 是当前发布 UTC RFC3339Nano 时间。
	PublishedAt string `json:"published_at"`
	// GovernanceStatus 是 active、suspended 或 offline。
	GovernanceStatus GovernanceStatus `json:"governance_status"`
	// GovernanceEpoch 是当前治理有效性纪元。
	GovernanceEpoch int64 `json:"governance_epoch"`
	// AllowedActions 是当前状态允许的固定顺序治理动作。
	AllowedActions []string `json:"allowed_actions"`
}

// GovernanceQueueResult 是治理列表应用服务返回的 keyset 分页结果。
type GovernanceQueueResult struct {
	// Items 是当前页安全列表投影。
	Items []GovernanceQueueItemDTO
	// NextCursor 是下一页 opaque cursor，无更多数据时为空。
	NextCursor string
}

// GovernanceDetailDTO 是 Governor 可读取的当前发布完整治理详情。
type GovernanceDetailDTO struct {
	// SkillID 是 Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// Definition 是 current published snapshot 的完整结构化定义。
	Definition SkillDefinitionV1 `json:"definition"`
	// PublishedAt 是当前发布 UTC RFC3339Nano 时间。
	PublishedAt string `json:"published_at"`
	// GovernanceStatus 是当前治理状态。
	GovernanceStatus GovernanceStatus `json:"governance_status"`
	// GovernanceEpoch 是当前治理有效性纪元。
	GovernanceEpoch int64 `json:"governance_epoch"`
	// GovernanceETag 是可原样传入 If-Match 的 Strong opaque ETag。
	GovernanceETag string `json:"governance_etag"`
	// AllowedActions 是当前状态允许的固定顺序治理动作。
	AllowedActions []string `json:"allowed_actions"`
}

// GovernanceDecisionCommand 是治理 HTTP 边界交给应用服务的可信命令。
type GovernanceDecisionCommand struct {
	// Governor 是本次 Session 动态解析的权威 Principal。
	Governor GovernancePrincipal
	// SkillID 是路径中的 Skill UUIDv7。
	SkillID string
	// Action 是 Body 中的 suspend、resume 或 offline。
	Action string
	// ReasonCode 是 Body 中与 Action 匹配的闭集原因代码。
	ReasonCode string
	// ApprovalReference 是 Body 中的规范外部工单引用。
	ApprovalReference string
	// SourceAddress 是 Handler 从 RemoteAddr 解析的规范直连 peer 地址。
	SourceAddress string
	// IfMatch 是客户端原样提交的单个 Strong Governance ETag。
	IfMatch string
	// IdempotencyKey 是请求头原值，只在应用层计算摘要。
	IdempotencyKey string
	// RequestID 是 Handler 生成的本次服务端 UUIDv7。
	RequestID string
}

// GovernanceDecisionDTO 是首次迁移和同义重放共用的安全结果投影。
type GovernanceDecisionDTO struct {
	// SkillID 是被治理 Skill UUIDv7。
	SkillID string `json:"skill_id"`
	// GovernanceStatus 是迁移后的治理状态。
	GovernanceStatus GovernanceStatus `json:"governance_status"`
	// GovernanceEpoch 是迁移后的治理有效性纪元。
	GovernanceEpoch int64 `json:"governance_epoch"`
	// TransitionedAt 是首次迁移 UTC RFC3339Nano 时间。
	TransitionedAt string `json:"transitioned_at"`
	// GovernanceETag 是迁移后新的 Strong opaque ETag。
	GovernanceETag string `json:"governance_etag"`
	// AllowedActions 是迁移后状态允许的固定顺序治理动作。
	AllowedActions []string `json:"allowed_actions"`
}

// GovernanceDecisionResult 是应用服务返回的治理决定与重放标识。
type GovernanceDecisionResult struct {
	// Skill 是首次冻结的安全治理结果。
	Skill GovernanceDecisionDTO
	// IdempotentReplay 表示本次命中了同义既有回执。
	IdempotentReplay bool
}

// GovernanceService 编排治理列表、详情和原子状态迁移，不复用 Owner/Reviewer Repository 大接口。
type GovernanceService struct {
	repository GovernanceRepository
	clock      Clock
	ids        IDGenerator
}

// NewGovernanceService 校验独立治理 Repository、Clock 和 UUIDv7 Generator 后创建应用服务。
func NewGovernanceService(repository GovernanceRepository, clock Clock, ids IDGenerator) (*GovernanceService, error) {
	if repository == nil || clock == nil || ids == nil {
		return nil, errors.New("create skill governance service: required dependency is missing")
	}
	return &GovernanceService{repository: repository, clock: clock, ids: ids}, nil
}

// ListGovernance 返回指定状态的已发布治理工作队列；无 capability 时不会调用 Repository。
func (service *GovernanceService) ListGovernance(ctx context.Context, principal GovernancePrincipal, status string, cursor string) (GovernanceQueueResult, error) {
	if !validGovernancePrincipal(principal) {
		return GovernanceQueueResult{}, ErrGovernanceCapabilityRequired
	}
	governanceStatus := GovernanceStatus(status)
	if !validGovernanceStatus(governanceStatus) {
		return GovernanceQueueResult{}, ErrInvalidGovernanceRequest
	}
	boundary, err := decodeGovernanceQueueCursor(governanceStatus, cursor)
	if err != nil {
		return GovernanceQueueResult{}, err
	}
	page, err := service.repository.ListGovernance(ctx, governanceStatus, boundary, defaultGovernancePageSize)
	if err != nil {
		return GovernanceQueueResult{}, normalizeGovernanceRepositoryError(err)
	}
	if page.Items == nil || len(page.Items) > defaultGovernancePageSize {
		return GovernanceQueueResult{}, ErrPersistence
	}
	result := GovernanceQueueResult{Items: make([]GovernanceQueueItemDTO, 0, len(page.Items))}
	seenSkills := make(map[string]struct{}, len(page.Items))
	seenSnapshots := make(map[string]struct{}, len(page.Items))
	for index, item := range page.Items {
		if err := validateGovernanceQueueItem(item, governanceStatus); err != nil {
			return GovernanceQueueResult{}, err
		}
		if index > 0 && !governanceQueueItemBefore(page.Items[index-1], item) {
			return GovernanceQueueResult{}, ErrPersistence
		}
		if _, duplicate := seenSkills[item.SkillID]; duplicate {
			return GovernanceQueueResult{}, ErrPersistence
		}
		if _, duplicate := seenSnapshots[item.PublishedSnapshotID]; duplicate {
			return GovernanceQueueResult{}, ErrPersistence
		}
		seenSkills[item.SkillID] = struct{}{}
		seenSnapshots[item.PublishedSnapshotID] = struct{}{}
		result.Items = append(result.Items, GovernanceQueueItemDTO{
			SkillID: item.SkillID, Name: item.Name, Summary: item.Summary, Category: item.Category,
			PublishedAt: item.PublishedAt.UTC().Format(time.RFC3339Nano), GovernanceStatus: item.GovernanceStatus,
			GovernanceEpoch: item.GovernanceEpoch, AllowedActions: governanceAllowedActions(item.GovernanceStatus),
		})
	}
	if page.HasMore {
		if len(page.Items) == 0 {
			return GovernanceQueueResult{}, ErrPersistence
		}
		last := page.Items[len(page.Items)-1]
		result.NextCursor, err = encodeGovernanceQueueCursor(governanceStatus, GovernanceQueueBoundary{
			PublishedAt: last.PublishedAt, PublishedSnapshotID: last.PublishedSnapshotID,
		})
		if err != nil {
			return GovernanceQueueResult{}, ErrPersistence
		}
	}
	return result, nil
}

// FindGovernanceDetail 返回 current published Definition 和治理状态；从未发布与不存在统一返回稳定 Not Found。
func (service *GovernanceService) FindGovernanceDetail(ctx context.Context, principal GovernancePrincipal, skillID string) (GovernanceDetailDTO, error) {
	if !validGovernancePrincipal(principal) {
		return GovernanceDetailDTO{}, ErrGovernanceCapabilityRequired
	}
	if !isUUIDv7(skillID) {
		return GovernanceDetailDTO{}, ErrInvalidGovernanceRequest
	}
	state, err := service.repository.FindGovernanceDetail(ctx, skillID)
	if err != nil {
		return GovernanceDetailDTO{}, normalizeGovernanceRepositoryError(err)
	}
	if err := validateGovernanceState(state); err != nil {
		return GovernanceDetailDTO{}, err
	}
	etag, err := GovernanceETag(state.SkillID, state.CurrentPublishedSnapshotID, state.GovernanceStatus, state.GovernanceEpoch)
	if err != nil {
		return GovernanceDetailDTO{}, ErrPersistence
	}
	return GovernanceDetailDTO{
		SkillID: state.SkillID, Definition: cloneDefinition(state.Published.Definition),
		PublishedAt:      state.Published.PublishedAt.UTC().Format(time.RFC3339Nano),
		GovernanceStatus: state.GovernanceStatus, GovernanceEpoch: state.GovernanceEpoch,
		GovernanceETag: etag, AllowedActions: governanceAllowedActions(state.GovernanceStatus),
	}, nil
}

// DecideGovernance 校验治理语义并生成回执/审计 ID，再由 Repository 原子复核授权和迁移状态。
func (service *GovernanceService) DecideGovernance(ctx context.Context, command GovernanceDecisionCommand) (GovernanceDecisionResult, error) {
	if !validGovernancePrincipal(command.Governor) {
		return GovernanceDecisionResult{}, ErrGovernanceCapabilityRequired
	}
	action := GovernanceAction(command.Action)
	if !isUUIDv7(command.SkillID) || !isUUIDv7(command.RequestID) || !validGovernanceAction(action) ||
		!validGovernanceReason(action, command.ReasonCode) || !validGovernanceApprovalReference(command.ApprovalReference) ||
		!validGovernanceSourceAddress(command.SourceAddress) || ValidateStrongGovernanceETag(command.IfMatch) != nil {
		return GovernanceDecisionResult{}, ErrInvalidGovernanceRequest
	}
	keyDigest, err := idempotencyKeyDigest(command.IdempotencyKey)
	if err != nil {
		return GovernanceDecisionResult{}, err
	}
	semanticDigest := governanceSemanticDigest(command.SkillID, action, command.ReasonCode, command.ApprovalReference, command.IfMatch)
	ids, err := service.newGovernanceIDs(2)
	if err != nil {
		return GovernanceDecisionResult{}, err
	}
	transitionedAt := service.clock.Now().UTC()
	if transitionedAt.IsZero() || transitionedAt.UnixNano() <= 0 {
		return GovernanceDecisionResult{}, ErrPersistence
	}
	result, err := service.repository.TransitionGovernance(ctx, GovernanceTransitionRepositoryCommand{
		GovernorUserID: command.Governor.UserID, SkillID: command.SkillID, Action: action,
		ReasonCode: command.ReasonCode, ApprovalReference: command.ApprovalReference, SourceAddress: command.SourceAddress,
		IfMatch: command.IfMatch, RequestID: command.RequestID, ReceiptID: ids[0], AuditID: ids[1],
		KeyDigest: keyDigest, SemanticDigest: semanticDigest, TransitionedAt: transitionedAt,
	})
	if err != nil {
		return GovernanceDecisionResult{}, normalizeGovernanceRepositoryError(err)
	}
	if err := validateGovernanceTransitionResult(action, result); err != nil {
		return GovernanceDecisionResult{}, err
	}
	etag, err := GovernanceETag(result.SkillID, result.PublishedSnapshotID, result.GovernanceStatus, result.GovernanceEpoch)
	if err != nil {
		return GovernanceDecisionResult{}, ErrPersistence
	}
	return GovernanceDecisionResult{Skill: GovernanceDecisionDTO{
		SkillID: result.SkillID, GovernanceStatus: result.GovernanceStatus, GovernanceEpoch: result.GovernanceEpoch,
		TransitionedAt: result.TransitionedAt.UTC().Format(time.RFC3339Nano), GovernanceETag: etag,
		AllowedActions: governanceAllowedActions(result.GovernanceStatus),
	}, IdempotentReplay: result.IdempotentReplay}, nil
}

// GovernanceETag 使用发布指针、治理状态和纪元生成域分离的单个 Strong opaque ETag。
func GovernanceETag(skillID string, publishedSnapshotID string, status GovernanceStatus, epoch int64) (string, error) {
	if !isUUIDv7(skillID) || !isUUIDv7(publishedSnapshotID) || !validGovernanceStatus(status) || epoch < 1 {
		return "", ErrPersistence
	}
	value := "skill_governance_etag.v1\x00" + skillID + "\x00" + publishedSnapshotID + "\x00" + string(status) + "\x00" + fmt.Sprintf("%d", epoch)
	digest := sha256.Sum256([]byte(value))
	return `"sg1-` + base64.RawURLEncoding.EncodeToString(digest[:]) + `"`, nil
}

// ValidateStrongGovernanceETag 拒绝通配符、weak/list 标签、非规范空白和非规范 Base64URL。
func ValidateStrongGovernanceETag(value string) error {
	const prefix = `"sg1-`
	if len(value) != len(prefix)+43+1 || !strings.HasPrefix(value, prefix) || value[len(value)-1] != '"' ||
		strings.TrimSpace(value) != value || strings.Contains(value, ",") {
		return ErrInvalidGovernanceRequest
	}
	raw := value[len(prefix) : len(value)-1]
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return ErrInvalidGovernanceRequest
	}
	return nil
}

// VerifyGovernanceETag 对已锁定权威字段重算 ETag，并以常量时间比较客户端 If-Match。
func VerifyGovernanceETag(candidate string, skillID string, publishedSnapshotID string, status GovernanceStatus, epoch int64) error {
	if err := ValidateStrongGovernanceETag(candidate); err != nil {
		return err
	}
	expected, err := GovernanceETag(skillID, publishedSnapshotID, status, epoch)
	if err != nil {
		return ErrPersistence
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) != 1 {
		return ErrGovernanceConflict
	}
	return nil
}

// governanceQueueCursorV1 是治理列表 opaque cursor 的内部冻结结构。
type governanceQueueCursorV1 struct {
	// SchemaVersion 防止后续分页规则变化时误读旧游标。
	SchemaVersion string `json:"schema_version"`
	// Status 绑定本次列表的治理状态过滤条件。
	Status GovernanceStatus `json:"status"`
	// PublishedAtUnixNano 是上一页最后一项 UTC 发布时间。
	PublishedAtUnixNano int64 `json:"published_at_unix_nano"`
	// PublishedSnapshotID 是稳定次级排序 UUIDv7。
	PublishedSnapshotID string `json:"published_snapshot_id"`
}

// encodeGovernanceQueueCursor 把合法 keyset 边界编码为无填充 Base64URL opaque cursor。
func encodeGovernanceQueueCursor(status GovernanceStatus, boundary GovernanceQueueBoundary) (string, error) {
	if !validGovernanceStatus(status) || boundary.PublishedAt.IsZero() || boundary.PublishedAt.UTC().UnixNano() <= 0 ||
		!isUUIDv7(boundary.PublishedSnapshotID) {
		return "", ErrInvalidGovernanceRequest
	}
	encoded, err := json.Marshal(governanceQueueCursorV1{
		SchemaVersion: "skill_governance_queue_cursor.v1", Status: status,
		PublishedAtUnixNano: boundary.PublishedAt.UTC().UnixNano(), PublishedSnapshotID: boundary.PublishedSnapshotID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// decodeGovernanceQueueCursor 严格解码并核对 cursor 与 Query status；空 cursor 表示第一页。
func decodeGovernanceQueueCursor(status GovernanceStatus, cursor string) (*GovernanceQueueBoundary, error) {
	if cursor == "" {
		return nil, nil
	}
	if len(cursor) > 1024 {
		return nil, ErrInvalidGovernanceRequest
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidGovernanceRequest
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var decoded governanceQueueCursorV1
	if err := decoder.Decode(&decoded); err != nil || ensureDecoderEOF(decoder) != nil ||
		decoded.SchemaVersion != "skill_governance_queue_cursor.v1" || decoded.Status != status ||
		!validGovernanceStatus(decoded.Status) || decoded.PublishedAtUnixNano <= 0 || !isUUIDv7(decoded.PublishedSnapshotID) {
		return nil, ErrInvalidGovernanceRequest
	}
	return &GovernanceQueueBoundary{
		PublishedAt: time.Unix(0, decoded.PublishedAtUnixNano).UTC(), PublishedSnapshotID: decoded.PublishedSnapshotID,
	}, nil
}

// governanceSemanticDigest 固定治理命令字段顺序，并精确覆盖客户端原样 If-Match。
func governanceSemanticDigest(skillID string, action GovernanceAction, reasonCode string, approvalReference string, ifMatch string) Digest {
	encoded, _ := json.Marshal(struct {
		SchemaVersion     string           `json:"schema_version"`
		SkillID           string           `json:"skill_id"`
		Action            GovernanceAction `json:"action"`
		ReasonCode        string           `json:"reason_code"`
		ApprovalReference string           `json:"approval_reference"`
		IfMatch           string           `json:"if_match"`
	}{
		SchemaVersion: "skill_governance_transition.v1", SkillID: skillID, Action: action,
		ReasonCode: reasonCode, ApprovalReference: approvalReference, IfMatch: ifMatch,
	})
	return sha256.Sum256(encoded)
}

// newGovernanceIDs 在任何治理事务开始前生成固定数量 UUIDv7。
func (service *GovernanceService) newGovernanceIDs(count int) ([]string, error) {
	ids := make([]string, count)
	for index := range ids {
		id, err := service.ids.New()
		if err != nil || !isUUIDv7(id) {
			return nil, fmt.Errorf("generate skill governance UUIDv7: %w", ErrPersistence)
		}
		ids[index] = id
	}
	return ids, nil
}

// validGovernancePrincipal 只接受规范 UUIDv7 和权威解析的 exact skill.govern capability。
func validGovernancePrincipal(principal GovernancePrincipal) bool {
	return isUUIDv7(principal.UserID) && hasCapability(principal.Capabilities, GovernanceCapability)
}

// validGovernanceStatus 校验治理状态闭集。
func validGovernanceStatus(status GovernanceStatus) bool {
	return status == GovernanceStatusActive || status == GovernanceStatusSuspended || status == GovernanceStatusOffline
}

// validGovernanceAction 校验治理动作闭集。
func validGovernanceAction(action GovernanceAction) bool {
	return action == GovernanceActionSuspend || action == GovernanceActionResume || action == GovernanceActionOffline
}

// validGovernanceReason 校验原因代码属于当前动作的稳定闭集。
func validGovernanceReason(action GovernanceAction, reasonCode string) bool {
	reasons, ok := governanceReasonCodes[action]
	if !ok {
		return false
	}
	_, ok = reasons[reasonCode]
	return ok
}

// validGovernanceApprovalReference 校验审批引用是无需规范化的稳定 ASCII 工单格式。
func validGovernanceApprovalReference(value string) bool {
	return utf8.ValidString(value) && len(value) <= 160 && governanceApprovalReferencePattern.MatchString(value)
}

// validGovernanceSourceAddress 只接受已经 Unmap 的规范 IPv4/IPv6 地址，不接受 zone 或代理 Header 值。
func validGovernanceSourceAddress(value string) bool {
	address, err := netip.ParseAddr(value)
	return err == nil && address.Zone() == "" && address.Unmap() == address && address.String() == value
}

// governanceAllowedActions 按产品固定顺序派生状态允许的动作，并总是返回非 nil 数组。
func governanceAllowedActions(status GovernanceStatus) []string {
	switch status {
	case GovernanceStatusActive:
		return []string{string(GovernanceActionSuspend), string(GovernanceActionOffline)}
	case GovernanceStatusSuspended:
		return []string{string(GovernanceActionResume), string(GovernanceActionOffline)}
	default:
		return []string{}
	}
}

// governanceTargetStatus 返回动作成功后的唯一目标状态。
func governanceTargetStatus(action GovernanceAction) GovernanceStatus {
	switch action {
	case GovernanceActionSuspend:
		return GovernanceStatusSuspended
	case GovernanceActionResume:
		return GovernanceStatusActive
	case GovernanceActionOffline:
		return GovernanceStatusOffline
	default:
		return ""
	}
}

// validateGovernanceQueueItem 校验 Repository 列表行的状态、分页键和最小展示字段。
func validateGovernanceQueueItem(item GovernanceQueueItem, expectedStatus GovernanceStatus) error {
	if !isUUIDv7(item.SkillID) || !isUUIDv7(item.PublishedSnapshotID) || item.GovernanceStatus != expectedStatus ||
		!validGovernanceStatus(item.GovernanceStatus) || item.GovernanceEpoch < 1 || item.PublishedAt.IsZero() ||
		item.PublishedAt.UTC().UnixNano() <= 0 || !utf8.ValidString(item.Name) || item.Name == "" ||
		!utf8.ValidString(item.Summary) || !utf8.ValidString(item.Category) {
		return ErrPersistence
	}
	return nil
}

// governanceQueueItemBefore 校验 Repository 已按发布时间和快照 ID严格倒序返回。
func governanceQueueItemBefore(previous GovernanceQueueItem, current GovernanceQueueItem) bool {
	if previous.PublishedAt.Equal(current.PublishedAt) {
		return previous.PublishedSnapshotID > current.PublishedSnapshotID
	}
	return previous.PublishedAt.After(current.PublishedAt)
}

// validateGovernanceState 重算 current published Definition 摘要并核对全部逻辑关联。
func validateGovernanceState(state GovernanceState) error {
	if !isUUIDv7(state.SkillID) || !isUUIDv7(state.CurrentPublishedSnapshotID) || state.PublicationRevision < 1 ||
		!validGovernanceStatus(state.GovernanceStatus) || state.GovernanceEpoch < 1 || state.SkillVersion < 1 ||
		state.Published.ID != state.CurrentPublishedSnapshotID || state.Published.SkillID != state.SkillID ||
		state.Published.PublicationRevision != state.PublicationRevision || !isUUIDv7(state.Published.SourceContentRevisionID) ||
		!isUUIDv7(state.Published.ReviewSubmissionID) || !isUUIDv7(state.Published.PublishedByUserID) || state.Published.PublishedAt.IsZero() {
		return ErrPersistence
	}
	_, canonicalDigest, err := DefinitionFromCanonicalV1(state.Published.CanonicalJSON)
	if err != nil || canonicalDigest != state.Published.ContentDigest {
		return ErrPersistence
	}
	_, definitionDigest, err := CanonicalDefinitionV1(state.Published.Definition)
	if err != nil || definitionDigest != state.Published.ContentDigest {
		return ErrPersistence
	}
	return nil
}

// validateGovernanceTransitionResult 拒绝 Repository 返回与动作或冻结响应不一致的结果。
func validateGovernanceTransitionResult(action GovernanceAction, result GovernanceTransitionRepositoryResult) error {
	if !isUUIDv7(result.SkillID) || !isUUIDv7(result.PublishedSnapshotID) ||
		result.GovernanceStatus != governanceTargetStatus(action) || result.GovernanceEpoch < 2 || result.TransitionedAt.IsZero() {
		return ErrPersistence
	}
	return nil
}

// normalizeGovernanceRepositoryError 保留取消、超时和稳定治理错误，并收敛所有底层存储细节。
func normalizeGovernanceRepositoryError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrGovernanceCapabilityRequired):
		return ErrGovernanceCapabilityRequired
	case errors.Is(err, ErrGovernanceNotFound):
		return ErrGovernanceNotFound
	case errors.Is(err, ErrGovernanceConflict):
		return ErrGovernanceConflict
	case errors.Is(err, ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, ErrPersistence):
		return ErrPersistence
	default:
		return ErrPersistence
	}
}
