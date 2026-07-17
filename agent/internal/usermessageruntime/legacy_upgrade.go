package usermessageruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

const (
	LegacyUpgradeGeneration int64 = 1

	LegacyUpgradeStageAbsent   = "absent"
	LegacyUpgradeStagePrepared = "prepared"
	LegacyUpgradeStageApplied  = "applied"
	LegacyUpgradeStageVerified = "verified"
)

var (
	ErrLegacyUpgradeBlocked  = errors.New("user message legacy upgrade is blocked")
	ErrLegacyUpgradeConflict = errors.New("user message legacy upgrade conflict")
)

// LegacyUpgradeBlocker 是只包含稳定代码和不敏感逻辑标识的 Preview 诊断结果。
type LegacyUpgradeBlocker struct {
	InputID string
	Code    string
}

// LegacyUpgradeLedger 是 Preview 专用升级 Ledger 的领域投影。
type LegacyUpgradeLedger struct {
	InputID           string
	SessionID         string
	Stage             string
	TurnID            string
	ContextDigest     string
	UpgradeGeneration int64
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// LegacyUpgradeCandidate 是从 Ensure Receipt 聚合根派生的 legacy 首 Input；布尔字段均由同一只读快照计算。
type LegacyUpgradeCandidate struct {
	CommandID          string
	CommandType        string
	RequestDigest      string
	ReceiptSessionID   string
	ReceiptMessageID   string
	ReceiptInputID     string
	ReceiptSkillDigest string
	ReceiptSkillCount  int

	SessionID string
	ProjectID string
	UserID    string
	Status    string
	Archived  bool

	InputID        string
	InputSource    string
	InputSourceID  string
	InputMessageID string
	InputStatus    string
	EnqueueSeq     int64
	Attempts       int
	InputLeaseIdle bool
	InputFence     int64

	LeaseIdle  bool
	LeaseFence int64

	MessageID         string
	MessageSeq        int64
	MessageRole       string
	MessageSourceKind string
	MessageSourceID   string
	MessageDigest     string
	MessageProtected  session.ProtectedContent

	SnapshotKind       session.SkillSnapshotKind
	SnapshotDigest     string
	SnapshotCount      int
	AcceptedEventValid bool

	TurnPresent    bool
	ContextPresent bool
	Ledger         *LegacyUpgradeLedger
}

// LegacyUpgradePreview 是 Helper 写入前的完整批次判定。只要 Blockers 非空，UpgradeBatch 必须零写。
type LegacyUpgradePreview struct {
	Candidates []LegacyUpgradeCandidate
	Blockers   []LegacyUpgradeBlocker
}

type LegacyUpgradePreparePlan struct {
	Candidate     LegacyUpgradeCandidate
	TurnID        string
	ContextDigest string
}

type LegacyUpgradeApplyPlan struct {
	Candidate LegacyUpgradeCandidate
	Ledger    LegacyUpgradeLedger
	Turn      session.UserMessageTurn
	Context   session.UserMessageContext
}

type LegacyUpgradeVerifyPlan struct {
	Candidate LegacyUpgradeCandidate
	Ledger    LegacyUpgradeLedger
	Turn      session.UserMessageTurn
	Context   session.UserMessageContext
}

// LegacyUpgradeRepository 将 Preview 与三个单向状态转换分开，便于证明 blocker 分支没有写事务。
type LegacyUpgradeRepository interface {
	Preview(context.Context, int) (LegacyUpgradePreview, error)
	Prepare(context.Context, LegacyUpgradePreparePlan) (LegacyUpgradeLedger, error)
	Apply(context.Context, LegacyUpgradeApplyPlan) (LegacyUpgradeLedger, error)
	Verify(context.Context, LegacyUpgradeVerifyPlan) (LegacyUpgradeLedger, error)
}

type LegacyUpgradeContentOpener interface {
	Open(context.Context, session.ProtectedContent, string) ([]byte, error)
}

type LegacyUpgradeSnapshotLoader interface {
	LoadSessionSkillSnapshotsV1(context.Context, []string) (map[string]session.LoadedSkillSnapshotV1, error)
}

type LegacyUpgradeIDGenerator interface{ New() (string, error) }

// LegacyUpgradeService 只服务 user_message.runtime.v2preview1，不复用生产 Session Lane 的通用升级器。
type LegacyUpgradeService struct {
	repository LegacyUpgradeRepository
	opener     LegacyUpgradeContentOpener
	snapshots  LegacyUpgradeSnapshotLoader
	ids        LegacyUpgradeIDGenerator
	limits     skill.LimitsProfileV1
}

func NewLegacyUpgradeService(repository LegacyUpgradeRepository, opener LegacyUpgradeContentOpener,
	snapshots LegacyUpgradeSnapshotLoader, ids LegacyUpgradeIDGenerator, limits skill.LimitsProfileV1,
) (*LegacyUpgradeService, error) {
	if repository == nil || opener == nil || snapshots == nil || ids == nil {
		return nil, fmt.Errorf("create user message legacy upgrade service: dependency is nil")
	}
	return &LegacyUpgradeService{repository: repository, opener: opener, snapshots: snapshots, ids: ids, limits: limits}, nil
}

// Preview 执行专用、只读的 legacy 分类。它不会调用任何写端口。
func (s *LegacyUpgradeService) Preview(ctx context.Context, limit int) (LegacyUpgradePreview, error) {
	if limit <= 0 {
		return LegacyUpgradePreview{}, fmt.Errorf("preview user message legacy upgrade: invalid limit")
	}
	preview, err := s.repository.Preview(ctx, limit)
	if err != nil {
		return LegacyUpgradePreview{}, err
	}
	structurallyValidSessionIDs := make([]string, 0, len(preview.Candidates))
	for i := range preview.Candidates {
		if structuralLegacyUpgradeBlocker(preview.Candidates[i]) == "" {
			structurallyValidSessionIDs = append(structurallyValidSessionIDs, preview.Candidates[i].SessionID)
		}
	}
	loadedSnapshots := map[string]session.LoadedSkillSnapshotV1{}
	var snapshotLoadErr error
	if len(structurallyValidSessionIDs) != 0 {
		loadedSnapshots, snapshotLoadErr = s.snapshots.LoadSessionSkillSnapshotsV1(ctx, structurallyValidSessionIDs)
	}
	for i := range preview.Candidates {
		candidate := preview.Candidates[i]
		code := structuralLegacyUpgradeBlocker(candidate)
		if code == "" {
			loaded, exists := loadedSnapshots[candidate.SessionID]
			if snapshotLoadErr != nil || !exists {
				code = "message_digest_mismatch"
			} else {
				code = s.cryptographicLegacyUpgradeBlocker(ctx, candidate, loaded)
			}
		}
		if code != "" {
			preview.Blockers = append(preview.Blockers, LegacyUpgradeBlocker{InputID: preview.Candidates[i].InputID, Code: code})
		}
	}
	if len(preview.Blockers) == 0 && len(preview.Candidates) > limit {
		preview.Candidates = preview.Candidates[:limit]
	}
	return preview, nil
}

// UpgradeBatch 在任何 blocker 存在时返回 ErrLegacyUpgradeBlocked 且保证没有调用写端口。
// 正常路径优先恢复 applied/prepared，最后处理 absent，并可在一次调用中收敛到 verified。
func (s *LegacyUpgradeService) UpgradeBatch(ctx context.Context, limit int) (LegacyUpgradePreview, error) {
	preview, err := s.Preview(ctx, limit)
	if err != nil {
		return LegacyUpgradePreview{}, err
	}
	if len(preview.Blockers) != 0 {
		return preview, ErrLegacyUpgradeBlocked
	}
	for _, stage := range []string{LegacyUpgradeStageApplied, LegacyUpgradeStagePrepared, LegacyUpgradeStageAbsent} {
		for i := range preview.Candidates {
			candidate := preview.Candidates[i]
			if legacyCandidateStage(candidate) != stage {
				continue
			}
			if err := s.upgradeCandidate(ctx, candidate); err != nil {
				return preview, err
			}
		}
	}
	return preview, nil
}

func (s *LegacyUpgradeService) upgradeCandidate(ctx context.Context, candidate LegacyUpgradeCandidate) error {
	ledger := candidate.Ledger
	if ledger == nil {
		turnID, err := s.ids.New()
		if err != nil {
			return err
		}
		contextValue, err := buildLegacyContext(candidate, turnID)
		if err != nil {
			return err
		}
		prepared, err := s.repository.Prepare(ctx, LegacyUpgradePreparePlan{Candidate: candidate, TurnID: turnID, ContextDigest: contextValue.ContextDigest})
		if err != nil {
			return err
		}
		ledger = &prepared
	}
	contextValue, err := buildLegacyContext(candidate, ledger.TurnID)
	if err != nil || contextValue.ContextDigest != ledger.ContextDigest {
		return ErrLegacyUpgradeConflict
	}
	if ledger.Stage == LegacyUpgradeStagePrepared {
		values := make([]string, 4)
		for i := range values {
			values[i], err = s.ids.New()
			if err != nil {
				return err
			}
		}
		turn := buildLegacyTurn(candidate, ledger.TurnID, values)
		applied, applyErr := s.repository.Apply(ctx, LegacyUpgradeApplyPlan{Candidate: candidate, Ledger: *ledger, Turn: turn, Context: contextValue})
		if applyErr != nil {
			return applyErr
		}
		ledger = &applied
	}
	if ledger.Stage == LegacyUpgradeStageApplied {
		turn := buildLegacyTurn(candidate, ledger.TurnID, nil)
		verified, verifyErr := s.repository.Verify(ctx, LegacyUpgradeVerifyPlan{Candidate: candidate, Ledger: *ledger, Turn: turn, Context: contextValue})
		if verifyErr != nil {
			return verifyErr
		}
		ledger = &verified
	}
	if ledger.Stage != LegacyUpgradeStageVerified {
		return ErrLegacyUpgradeConflict
	}
	return nil
}

// cryptographicLegacyUpgradeBlocker 在任何写事务开始前批量完成正文、Snapshot 与 V1/V2 请求摘要重验。
// Message、Receipt 与 Skill Snapshot 由 Migration 的不可变 Guard 冻结；写事务仍会重验全部结构事实。
func (s *LegacyUpgradeService) cryptographicLegacyUpgradeBlocker(
	ctx context.Context,
	candidate LegacyUpgradeCandidate,
	loaded session.LoadedSkillSnapshotV1,
) string {
	plaintext, err := s.opener.Open(ctx, candidate.MessageProtected, candidate.MessageDigest)
	if err != nil || digestLegacyPlaintext(plaintext) != candidate.MessageDigest {
		return "message_digest_mismatch"
	}
	if loaded.SessionID != candidate.SessionID {
		return "message_digest_mismatch"
	}
	if err := validateLegacyRequest(candidate, string(plaintext), loaded.Snapshot, s.limits); err != nil {
		return "message_digest_mismatch"
	}
	return ""
}

func structuralLegacyUpgradeBlocker(c LegacyUpgradeCandidate) string {
	switch {
	case c.Status != "active" || c.Archived:
		return "session_not_active"
	case c.InputSource != "user_message":
		return "source_type_not_user_message"
	case c.EnqueueSeq != 1:
		return "not_first_input"
	case c.ReceiptSessionID != c.SessionID || c.ReceiptInputID != c.InputID || c.ReceiptMessageID != c.MessageID ||
		c.InputSourceID != c.CommandID || c.InputMessageID != c.MessageID || c.MessageSourceID != c.CommandID:
		return "provenance_mismatch"
	case c.MessageRole != "user" || c.MessageSeq != 1 || c.MessageSourceKind != "ensure_project_session":
		return "provenance_mismatch"
	case c.ReceiptSkillDigest != c.SnapshotDigest || c.ReceiptSkillCount != c.SnapshotCount:
		return "message_digest_mismatch"
	case c.InputStatus != "pending" || c.Attempts != 0 || !c.InputLeaseIdle || c.InputFence != 0:
		return "input_not_pristine"
	case !c.LeaseIdle || c.LeaseFence != 0:
		return "lease_not_idle"
	case !c.AcceptedEventValid:
		return "accepted_event_unverifiable"
	case !validLegacyShape(c):
		return "legacy_upgrade_incomplete"
	default:
		return ""
	}
}

func validLegacyShape(c LegacyUpgradeCandidate) bool {
	stage := legacyCandidateStage(c)
	switch stage {
	case LegacyUpgradeStageAbsent, LegacyUpgradeStagePrepared:
		return !c.TurnPresent && !c.ContextPresent
	case LegacyUpgradeStageApplied, LegacyUpgradeStageVerified:
		return c.TurnPresent && c.ContextPresent
	default:
		return false
	}
}

func legacyCandidateStage(c LegacyUpgradeCandidate) string {
	if c.Ledger == nil {
		return LegacyUpgradeStageAbsent
	}
	return c.Ledger.Stage
}

func validateLegacyRequest(c LegacyUpgradeCandidate, prompt string, snapshot skill.SessionSkillSnapshotV1, limits skill.LimitsProfileV1) error {
	if snapshot.SnapshotSetDigest != c.SnapshotDigest || int(snapshot.SkillCount) != c.SnapshotCount {
		return fmt.Errorf("%w: skill_snapshot_unverifiable", ErrLegacyUpgradeBlocked)
	}
	switch c.CommandType {
	case session.CommandTypeEnsureProjectSessionV1:
		request, promptDigest, present, err := session.CalculateRequestDigest(c.ProjectID, c.UserID, prompt, c.SnapshotKind)
		if err != nil || !present || promptDigest != c.MessageDigest || request != c.RequestDigest {
			return fmt.Errorf("%w: message_digest_mismatch", ErrLegacyUpgradeBlocked)
		}
	case session.CommandTypeEnsureProjectSessionV2:
		canonical, err := skill.CanonicalEnsureProjectSessionV2(skill.EnsureProjectSessionInputV2{
			SchemaVersion: skill.EnsureProjectSessionSchemaVersionV2, ProjectID: c.ProjectID, OwnerUserID: c.UserID,
			CreationSource: skill.CreationSourceQuickCreate, InitialPrompt: prompt, SkillSnapshot: snapshot,
		}, limits)
		if err != nil || canonical.PromptDigest != c.MessageDigest || canonical.RequestDigest.Hex() != c.RequestDigest {
			return fmt.Errorf("%w: message_digest_mismatch", ErrLegacyUpgradeBlocked)
		}
	default:
		return fmt.Errorf("%w: provenance_mismatch", ErrLegacyUpgradeBlocked)
	}
	return nil
}

func buildLegacyContext(c LegacyUpgradeCandidate, turnID string) (session.UserMessageContext, error) {
	profile := ApprovedSessionProfile()
	value := session.UserMessageContext{
		TurnID: turnID, SchemaVersion: profile.ContextSchema, SessionID: c.SessionID, InputID: c.InputID,
		MessageID: c.MessageID, UserID: c.UserID, ProjectID: c.ProjectID, MessageCutoffSeq: c.MessageSeq,
		MessageContentDigest: c.MessageDigest, SkillSnapshotRef: "session_skill_snapshot:" + c.SessionID,
		SkillSnapshotDigest: c.SnapshotDigest, PromptRef: profile.PromptRef, PromptDigest: profile.PromptDigest,
		ToolRegistryRef: profile.ToolRegistryRef, ToolRegistryDigest: profile.ToolRegistryDigest,
		RuntimePolicyRef: profile.RuntimePolicyRef, RuntimePolicyDigest: profile.RuntimePolicyDigest,
		ModelRouteRef: profile.ModelRouteRef, ModelRouteDigest: profile.ModelRouteDigest,
		BudgetRef: profile.BudgetRef, BudgetDigest: profile.BudgetDigest,
		AccessScopeRef: "ensure_command:" + c.CommandID, AccessScopeDigest: c.RequestDigest,
	}
	digest, err := session.DigestUserMessageContext(value)
	if err != nil {
		return session.UserMessageContext{}, err
	}
	value.ContextDigest = digest
	return value, nil
}

func buildLegacyTurn(c LegacyUpgradeCandidate, turnID string, ids []string) session.UserMessageTurn {
	turn := session.UserMessageTurn{TurnID: turnID, InputID: c.InputID, SessionID: c.SessionID, MessageID: c.MessageID,
		UserID: c.UserID, ProjectID: c.ProjectID, Status: "created", Version: 1}
	if len(ids) == 4 {
		turn.OutputID, turn.ModelCallID, turn.RecoveryEventID, turn.TerminalEventID = ids[0], ids[1], ids[2], ids[3]
	}
	return turn
}

func digestLegacyPlaintext(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
