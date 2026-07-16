package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/authorization"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"gorm.io/gorm"
)

// skillGovernanceIntegrationFacts 使用一条固定 SQL 读取 Skill、治理回执和治理审计的联合权威事实。
type skillGovernanceIntegrationFacts struct {
	// GovernanceStatus 是 Skill 当前治理状态。
	GovernanceStatus string `gorm:"column:governance_status"`
	// GovernanceEpoch 是 Skill 当前治理纪元。
	GovernanceEpoch int64 `gorm:"column:governance_epoch"`
	// SkillVersion 是 Skill 聚合 CAS 版本。
	SkillVersion int64 `gorm:"column:skill_version"`
	// CurrentPublishedSnapshotID 是治理迁移不得修改的当前发布指针。
	CurrentPublishedSnapshotID string `gorm:"column:current_published_snapshot_id"`
	// ReceiptCount 是当前 Skill 已提交的治理回执数量。
	ReceiptCount int64 `gorm:"column:receipt_count"`
	// AuditCount 是当前 Skill 已提交的治理审计数量。
	AuditCount int64 `gorm:"column:audit_count"`
}

// skillGovernanceIntegrationJointFact 核对一次治理迁移的回执与审计是否保存相同的安全事实。
type skillGovernanceIntegrationJointFact struct {
	// ReceiptID 是首次治理命令回执标识。
	ReceiptID string `gorm:"column:receipt_id"`
	// AuditReceiptID 是审计关联的治理回执标识。
	AuditReceiptID string `gorm:"column:audit_receipt_id"`
	// ReceiptActorUserID 是回执冻结的 Governor 标识。
	ReceiptActorUserID string `gorm:"column:receipt_actor_user_id"`
	// AuditActorUserID 是审计记录的 Governor 标识。
	AuditActorUserID string `gorm:"column:audit_actor_user_id"`
	// ReceiptSkillID 是回执治理作用域中的 Skill 标识。
	ReceiptSkillID string `gorm:"column:receipt_skill_id"`
	// AuditSkillID 是审计目标 Skill 标识。
	AuditSkillID string `gorm:"column:audit_skill_id"`
	// ReceiptSnapshotID 是回执冻结的当前发布快照标识。
	ReceiptSnapshotID string `gorm:"column:receipt_snapshot_id"`
	// ResponseSnapshotID 是安全响应冻结的发布快照标识。
	ResponseSnapshotID string `gorm:"column:response_snapshot_id"`
	// ReceiptStatus 是回执冻结的迁移后治理状态。
	ReceiptStatus string `gorm:"column:receipt_status"`
	// AuditToStatus 是审计记录的迁移后治理状态。
	AuditToStatus string `gorm:"column:audit_to_status"`
	// ReceiptEpoch 是回执冻结的迁移后治理纪元。
	ReceiptEpoch int64 `gorm:"column:receipt_epoch"`
	// AuditEpoch 是审计记录的迁移后治理纪元。
	AuditEpoch int64 `gorm:"column:audit_epoch"`
	// ReceiptRequestID 是回执冻结的服务端请求标识。
	ReceiptRequestID string `gorm:"column:receipt_request_id"`
	// AuditRequestID 是审计记录的服务端请求标识。
	AuditRequestID string `gorm:"column:audit_request_id"`
	// AuditAction 是与状态边对应的稳定治理动作。
	AuditAction string `gorm:"column:audit_action"`
	// AuditFromStatus 是审计记录的迁移前治理状态。
	AuditFromStatus string `gorm:"column:audit_from_status"`
	// ActorRoleKey 是事务内复核并冻结的治理角色键。
	ActorRoleKey string `gorm:"column:actor_role_key"`
	// SafeReasonCode 是动作对应闭集中的安全原因代码。
	SafeReasonCode string `gorm:"column:safe_reason_code"`
	// ApprovalReference 是规范外部审批引用。
	ApprovalReference string `gorm:"column:approval_reference"`
	// SourceAddress 是 HTTP 直连 peer 的规范地址。
	SourceAddress string `gorm:"column:source_address"`
	// ReceiptCreatedAt 是首次回执冻结时间。
	ReceiptCreatedAt time.Time `gorm:"column:receipt_created_at"`
	// AuditOccurredAt 是追加治理审计发生时间。
	AuditOccurredAt time.Time `gorm:"column:audit_occurred_at"`
}

// TestSkillGovernanceRepositoryPostgreSQLTransitions 使用真实 PostgreSQL 16 验证治理状态机、并发幂等、原子审计和动态撤权。
func TestSkillGovernanceRepositoryPostgreSQLTransitions(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewSkillRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	authorizationRepository, err := NewAuthorizationRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}

	published := seedPublishedProjectSkill(t, db)
	initialState, err := repository.FindGovernanceDetail(context.Background(), published.Skill.ID)
	if err != nil {
		t.Fatalf("read initial governance fixture: %v", err)
	}
	if initialState.GovernanceStatus != skill.GovernanceStatusActive || initialState.GovernanceEpoch != 1 {
		t.Fatalf("unexpected initial governance state: %+v", initialState)
	}

	governorID := newSkillRepositoryUUIDv7(t)
	provisionerID := newSkillRepositoryUUIDv7(t)
	for _, account := range []struct {
		id   string
		name string
	}{{governorID, "Governance integration governor"}, {provisionerID, "Governance integration provisioner"}} {
		if err := db.Create(&userAccountModel{
			ID: account.id, DisplayName: account.name, UserType: "personal", Status: "active", Version: 1,
			CreatedAt: initialState.Published.PublishedAt, UpdatedAt: initialState.Published.PublishedAt,
		}).Error; err != nil {
			t.Fatalf("create governance authority account: %v", err)
		}
	}

	// active 账户没有 skill_governor assignment 时必须在读取回执和 Skill 前失败关闭。
	unauthorized := newSkillGovernanceIntegrationCommand(
		t, provisionerID, initialState, skill.GovernanceActionSuspend, "content_safety", "TEST-GOVERNANCE-UNAUTHORIZED", "unauthorized",
	)
	if _, err := repository.TransitionGovernance(context.Background(), unauthorized); !errors.Is(err, skill.ErrGovernanceCapabilityRequired) {
		t.Fatalf("non-governor crossed governance boundary: %v", err)
	}

	assignmentID := newSkillRepositoryUUIDv7(t)
	if _, err := authorizationRepository.Grant(context.Background(), authorization.Assignment{
		ID: assignmentID, UserID: governorID, Role: authorization.RoleSkillGovernor,
		Status: authorization.StatusActive, Version: 1, AssignedByUserID: provisionerID,
		AssignmentReasonCode: "governance_integration", ApprovalReference: "TEST-GOVERNANCE-GRANT",
		AssignedAt: initialState.Published.PublishedAt, UpdatedAt: initialState.Published.PublishedAt,
	}); err != nil {
		t.Fatalf("grant governance integration role: %v", err)
	}

	// 相同 Governor、Key 和语义的 100 并发必须只提交一次暂停，其余从同一回执冻结重放。
	suspend := newSkillGovernanceIntegrationCommand(
		t, governorID, initialState, skill.GovernanceActionSuspend, "content_safety", "TEST-GOVERNANCE-SUSPEND", "suspend",
	)
	const concurrency = 100
	results := make(chan skill.GovernanceTransitionRepositoryResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, transitionErr := repository.TransitionGovernance(context.Background(), suspend)
			if transitionErr != nil {
				errorsChannel <- transitionErr
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for transitionErr := range errorsChannel {
		t.Errorf("concurrent governance suspend failed: %v", transitionErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	createdCount := 0
	replayCount := 0
	for result := range results {
		if result.IdempotentReplay {
			replayCount++
		} else {
			createdCount++
		}
		if result.SkillID != initialState.SkillID || result.PublishedSnapshotID != initialState.CurrentPublishedSnapshotID ||
			result.GovernanceStatus != skill.GovernanceStatusSuspended || result.GovernanceEpoch != 2 ||
			!result.TransitionedAt.Equal(suspend.TransitionedAt) {
			t.Fatalf("concurrent suspend returned drifting frozen result: %+v", result)
		}
	}
	if createdCount != 1 || replayCount != concurrency-1 {
		t.Fatalf("unexpected concurrent governance dispositions: created=%d replayed=%d", createdCount, replayCount)
	}
	assertSkillGovernanceIntegrationFacts(t, db, published.Skill.ID, skillGovernanceIntegrationFacts{
		GovernanceStatus: string(skill.GovernanceStatusSuspended), GovernanceEpoch: 2,
		SkillVersion: initialState.SkillVersion + 1, CurrentPublishedSnapshotID: initialState.CurrentPublishedSnapshotID,
		ReceiptCount: 1, AuditCount: 1,
	})
	assertSkillGovernanceIntegrationJointFact(t, db, suspend, initialState, "governance_suspended", "active", "suspended", 2)

	suspendedState, err := repository.FindGovernanceDetail(context.Background(), published.Skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume := newSkillGovernanceIntegrationCommand(
		t, governorID, suspendedState, skill.GovernanceActionResume, "risk_cleared", "TEST-GOVERNANCE-RESUME", "resume",
	)
	resumed, err := repository.TransitionGovernance(context.Background(), resume)
	if err != nil || resumed.IdempotentReplay || resumed.GovernanceStatus != skill.GovernanceStatusActive || resumed.GovernanceEpoch != 3 {
		t.Fatalf("resume governance result=%+v err=%v", resumed, err)
	}
	assertSkillGovernanceIntegrationJointFact(t, db, resume, suspendedState, "governance_resumed", "suspended", "active", 3)

	resumedState, err := repository.FindGovernanceDetail(context.Background(), published.Skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 复用已经存在的 audit ID 在事务最后一步制造失败，CAS、回执和审计必须一起回滚。
	failedOffline := newSkillGovernanceIntegrationCommand(
		t, governorID, resumedState, skill.GovernanceActionOffline, "repeated_violation", "TEST-GOVERNANCE-OFFLINE-ROLLBACK", "offline-rollback",
	)
	failedOffline.AuditID = resume.AuditID
	if _, err := repository.TransitionGovernance(context.Background(), failedOffline); !errors.Is(err, skill.ErrPersistence) {
		t.Fatalf("expected duplicate audit to roll back governance transaction, got %v", err)
	}
	assertSkillGovernanceIntegrationFacts(t, db, published.Skill.ID, skillGovernanceIntegrationFacts{
		GovernanceStatus: string(skill.GovernanceStatusActive), GovernanceEpoch: 3,
		SkillVersion: initialState.SkillVersion + 2, CurrentPublishedSnapshotID: initialState.CurrentPublishedSnapshotID,
		ReceiptCount: 2, AuditCount: 2,
	})

	offline := newSkillGovernanceIntegrationCommand(
		t, governorID, resumedState, skill.GovernanceActionOffline, "repeated_violation", "TEST-GOVERNANCE-OFFLINE", "offline",
	)
	offlined, err := repository.TransitionGovernance(context.Background(), offline)
	if err != nil || offlined.IdempotentReplay || offlined.GovernanceStatus != skill.GovernanceStatusOffline || offlined.GovernanceEpoch != 4 {
		t.Fatalf("offline governance result=%+v err=%v", offlined, err)
	}
	assertSkillGovernanceIntegrationJointFact(t, db, offline, resumedState, "governance_offlined", "active", "offline", 4)
	assertSkillGovernanceIntegrationFacts(t, db, published.Skill.ID, skillGovernanceIntegrationFacts{
		GovernanceStatus: string(skill.GovernanceStatusOffline), GovernanceEpoch: 4,
		SkillVersion: initialState.SkillVersion + 3, CurrentPublishedSnapshotID: initialState.CurrentPublishedSnapshotID,
		ReceiptCount: 3, AuditCount: 3,
	})

	// 后续已进入 offline 时，旧暂停 Key 仍先于当前状态校验返回首次 suspended/epoch=2 冻结结果。
	replayedSuspend, err := repository.TransitionGovernance(context.Background(), suspend)
	if err != nil || !replayedSuspend.IdempotentReplay || replayedSuspend.GovernanceStatus != skill.GovernanceStatusSuspended ||
		replayedSuspend.GovernanceEpoch != 2 || !replayedSuspend.TransitionedAt.Equal(suspend.TransitionedAt) {
		t.Fatalf("old governance receipt did not replay frozen response: result=%+v err=%v", replayedSuspend, err)
	}

	offlineState, err := repository.FindGovernanceDetail(context.Background(), published.Skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	terminalResume := newSkillGovernanceIntegrationCommand(
		t, governorID, offlineState, skill.GovernanceActionResume, "risk_cleared", "TEST-GOVERNANCE-OFFLINE-TERMINAL", "offline-terminal",
	)
	if _, err := repository.TransitionGovernance(context.Background(), terminalResume); !errors.Is(err, skill.ErrGovernanceConflict) {
		t.Fatalf("offline terminal accepted resume: %v", err)
	}
	assertSkillGovernanceIntegrationFacts(t, db, published.Skill.ID, skillGovernanceIntegrationFacts{
		GovernanceStatus: string(skill.GovernanceStatusOffline), GovernanceEpoch: 4,
		SkillVersion: initialState.SkillVersion + 3, CurrentPublishedSnapshotID: initialState.CurrentPublishedSnapshotID,
		ReceiptCount: 3, AuditCount: 3,
	})

	revokedAt := offline.TransitionedAt.Add(time.Minute)
	if _, err := authorizationRepository.Revoke(context.Background(), authorization.RevokeCommand{
		AssignmentID: assignmentID, TargetUserID: governorID, ActorUserID: provisionerID,
		Role: authorization.RoleSkillGovernor, ExpectedVersion: 1,
		ReasonCode: "governance_integration_complete", ApprovalReference: "TEST-GOVERNANCE-REVOKE",
	}, revokedAt); err != nil {
		t.Fatalf("revoke governance integration role: %v", err)
	}
	// 撤权提交后即使同一 actor 和合法 Skill/ETag 再请求，也必须先于回执与状态读取失败关闭。
	afterRevocation := newSkillGovernanceIntegrationCommand(
		t, governorID, offlineState, skill.GovernanceActionResume, "risk_cleared", "TEST-GOVERNANCE-AFTER-REVOKE", "after-revoke",
	)
	if _, err := repository.TransitionGovernance(context.Background(), afterRevocation); !errors.Is(err, skill.ErrGovernanceCapabilityRequired) {
		t.Fatalf("revoked governor crossed governance boundary: %v", err)
	}
	assertSkillGovernanceIntegrationFacts(t, db, published.Skill.ID, skillGovernanceIntegrationFacts{
		GovernanceStatus: string(skill.GovernanceStatusOffline), GovernanceEpoch: 4,
		SkillVersion: initialState.SkillVersion + 3, CurrentPublishedSnapshotID: initialState.CurrentPublishedSnapshotID,
		ReceiptCount: 3, AuditCount: 3,
	})
}

// newSkillGovernanceIntegrationCommand 根据当前权威状态生成一个治理 Repository 命令，所有 UUIDv7 在事务外完成。
func newSkillGovernanceIntegrationCommand(
	t *testing.T,
	governorID string,
	state skill.GovernanceState,
	action skill.GovernanceAction,
	reasonCode string,
	approvalReference string,
	digestSeed string,
) skill.GovernanceTransitionRepositoryCommand {
	t.Helper()
	ifMatch, err := skill.GovernanceETag(
		state.SkillID, state.CurrentPublishedSnapshotID, state.GovernanceStatus, state.GovernanceEpoch,
	)
	if err != nil {
		t.Fatalf("create governance integration ETag: %v", err)
	}
	return skill.GovernanceTransitionRepositoryCommand{
		GovernorUserID: governorID, SkillID: state.SkillID, Action: action,
		ReasonCode: reasonCode, ApprovalReference: approvalReference, SourceAddress: "192.0.2.10",
		IfMatch: ifMatch, RequestID: newSkillRepositoryUUIDv7(t),
		ReceiptID: newSkillRepositoryUUIDv7(t), AuditID: newSkillRepositoryUUIDv7(t),
		KeyDigest:      sha256.Sum256([]byte("governance-key-" + digestSeed)),
		SemanticDigest: sha256.Sum256([]byte("governance-semantic-" + digestSeed)),
		TransitionedAt: state.Published.PublishedAt.Add(time.Duration(state.GovernanceEpoch) * time.Minute),
	}
}

// readSkillGovernanceIntegrationFacts 使用固定子查询一次读取治理聚合、回执和审计数量。
func readSkillGovernanceIntegrationFacts(t *testing.T, db *gorm.DB, skillID string) skillGovernanceIntegrationFacts {
	t.Helper()
	var facts skillGovernanceIntegrationFacts
	if err := db.Raw(`
SELECT
    skill_record.governance_status,
    skill_record.governance_epoch,
    skill_record.version AS skill_version,
    skill_record.current_published_snapshot_id,
    (SELECT COUNT(*)
       FROM business.skill_command_receipt AS receipt
      WHERE receipt.command_type = 'governance_transition'
        AND receipt.scope_id = skill_record.id) AS receipt_count,
    (SELECT COUNT(*)
       FROM business.skill_governance_audit AS audit
      WHERE audit.skill_id = skill_record.id
        AND audit.action IN ('governance_suspended', 'governance_resumed', 'governance_offlined')) AS audit_count
FROM business.skill AS skill_record
WHERE skill_record.id = ?`, skillID).Scan(&facts).Error; err != nil {
		t.Fatalf("read governance integration facts: %v", err)
	}
	return facts
}

// assertSkillGovernanceIntegrationFacts 核对失败、重放或成功路径只产生预期数量的治理事实。
func assertSkillGovernanceIntegrationFacts(t *testing.T, db *gorm.DB, skillID string, expected skillGovernanceIntegrationFacts) {
	t.Helper()
	if actual := readSkillGovernanceIntegrationFacts(t, db, skillID); actual != expected {
		t.Fatalf("unexpected governance facts: actual=%+v expected=%+v", actual, expected)
	}
}

// assertSkillGovernanceIntegrationJointFact 使用一次 JOIN 核对回执和审计在同一事务中冻结完全一致的身份与迁移事实。
func assertSkillGovernanceIntegrationJointFact(
	t *testing.T,
	db *gorm.DB,
	command skill.GovernanceTransitionRepositoryCommand,
	before skill.GovernanceState,
	action string,
	fromStatus string,
	toStatus string,
	epoch int64,
) {
	t.Helper()
	var fact skillGovernanceIntegrationJointFact
	if err := db.Raw(`
SELECT
    receipt.id AS receipt_id,
    audit.command_receipt_id AS audit_receipt_id,
    receipt.actor_user_id AS receipt_actor_user_id,
    audit.actor_user_id AS audit_actor_user_id,
    receipt.scope_id AS receipt_skill_id,
    audit.skill_id AS audit_skill_id,
    receipt.result_published_snapshot_id AS receipt_snapshot_id,
    receipt.response_published_snapshot_id AS response_snapshot_id,
    receipt.response_governance_status AS receipt_status,
    audit.to_status AS audit_to_status,
    receipt.response_governance_epoch AS receipt_epoch,
    audit.governance_epoch AS audit_epoch,
    receipt.request_id AS receipt_request_id,
    audit.request_id AS audit_request_id,
    audit.action AS audit_action,
    audit.from_status AS audit_from_status,
    audit.actor_role_key,
    audit.safe_reason_code,
    audit.approval_reference,
    host(audit.source_address) AS source_address,
    receipt.created_at AS receipt_created_at,
    audit.occurred_at AS audit_occurred_at
FROM business.skill_command_receipt AS receipt
JOIN business.skill_governance_audit AS audit
  ON audit.command_receipt_id = receipt.id
WHERE receipt.id = ?`, command.ReceiptID).Scan(&fact).Error; err != nil {
		t.Fatalf("read governance receipt/audit joint fact: %v", err)
	}
	if fact.ReceiptID != command.ReceiptID || fact.AuditReceiptID != command.ReceiptID ||
		fact.ReceiptActorUserID != command.GovernorUserID || fact.AuditActorUserID != command.GovernorUserID ||
		fact.ReceiptSkillID != command.SkillID || fact.AuditSkillID != command.SkillID ||
		fact.ReceiptSnapshotID != before.CurrentPublishedSnapshotID || fact.ResponseSnapshotID != before.CurrentPublishedSnapshotID ||
		fact.ReceiptStatus != toStatus || fact.AuditToStatus != toStatus || fact.ReceiptEpoch != epoch || fact.AuditEpoch != epoch ||
		fact.ReceiptRequestID != command.RequestID || fact.AuditRequestID != command.RequestID ||
		fact.AuditAction != action || fact.AuditFromStatus != fromStatus || fact.ActorRoleKey != skill.GovernanceRoleKey ||
		fact.SafeReasonCode != command.ReasonCode || fact.ApprovalReference != command.ApprovalReference ||
		fact.SourceAddress != command.SourceAddress || !fact.ReceiptCreatedAt.Equal(command.TransitionedAt) ||
		!fact.AuditOccurredAt.Equal(command.TransitionedAt) {
		t.Fatalf("governance receipt/audit fact drifted: %+v", fact)
	}
}
