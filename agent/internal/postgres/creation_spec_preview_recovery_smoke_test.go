package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	previewchatmodel "github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/contentcrypto"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"gorm.io/gorm"
)

const (
	previewRecoverySmokeDSNEnv    = "DORA_PLAN_SPEC_PREVIEW_RECOVERY_SMOKE_DSN"
	previewRecoverySmokeResultEnv = "DORA_PLAN_SPEC_PREVIEW_RECOVERY_SMOKE_RESULT"
	previewRecoverySmokeLimit     = 3
)

// previewRecoverySmokeIDs 保存该真实 PostgreSQL 探针独占的全部逻辑标识；清理只按这些标识执行。
type previewRecoverySmokeIDs struct {
	SessionID           string
	ProjectID           string
	UserID              string
	InputID             string
	MessageID           string
	RequestID           string
	IdempotencyKey      string
	TurnID              string
	RunID               string
	ToolCallID          string
	BusinessCommandID   string
	TerminalEventID     string
	FollowerInputID     string
	FollowerMessageID   string
	FollowerRequestID   string
	FollowerIdempotency string
	FollowerTurnID      string
	FollowerRunID       string
	FollowerToolCallID  string
	FollowerCommandID   string
	FollowerEventID     string
}

// previewRecoverySmokeReceiptFacts 是脱敏权威查询结果；它只保留存在性、格式和计数，不读取密文或 Key Version 值。
type previewRecoverySmokeReceiptFacts struct {
	Stage                string `gorm:"column:stage"`
	CiphertextPresent    bool   `gorm:"column:ciphertext_present"`
	KeyReferencePresent  bool   `gorm:"column:key_reference_present"`
	PayloadDigestValid   bool   `gorm:"column:payload_digest_valid"`
	ResendAttempts       int    `gorm:"column:resend_attempts"`
	ResendLimit          int    `gorm:"column:resend_limit"`
	ExhaustedAtPresent   bool   `gorm:"column:exhausted_at_present"`
	ExhaustedCodeMatches bool   `gorm:"column:exhausted_code_matches"`
	ResultPayloadAbsent  bool   `gorm:"column:result_payload_absent"`
}

// previewRecoverySmokeEvidence 是 canonical Smoke 可消费的严格脱敏结果。
type previewRecoverySmokeEvidence struct {
	SchemaVersion string                                 `json:"schema_version"`
	Status        string                                 `json:"status"`
	Assertions    previewRecoverySmokeEvidenceAssertions `json:"assertions"`
	Counts        previewRecoverySmokeEvidenceCounts     `json:"counts"`
}

// previewRecoverySmokeEvidenceAssertions 固定全部恢复安全门禁，不输出命令正文、密文或密钥引用值。
type previewRecoverySmokeEvidenceAssertions struct {
	AuthoritativeNotFoundReserveCAS bool `json:"authoritative_not_found_reserve_cas"`
	BusinessAdapterCommandExact     bool `json:"business_adapter_command_exact"`
	DurableCiphertextPresent        bool `json:"durable_ciphertext_present"`
	DurableKeyReferencePresent      bool `json:"durable_key_reference_present"`
	DurablePayloadDigestValid       bool `json:"durable_payload_digest_valid"`
	ExhaustedMarkedExplicitly       bool `json:"exhausted_marked_explicitly"`
	ExhaustedNotClaimed             bool `json:"exhausted_not_claimed"`
	FormalGraphRecovery             bool `json:"formal_graph_recovery"`
	HeadOfLineNotSkipped            bool `json:"head_of_line_not_skipped"`
	RealPostgreSQL                  bool `json:"real_postgresql"`
	ResendLimitFrozen               bool `json:"resend_limit_frozen"`
	RestartedOwnerFenceRebuilt      bool `json:"restarted_owner_fence_rebuilt"`
	ResultPayloadAbsent             bool `json:"result_payload_absent"`
	StableBusinessCommandID         bool `json:"stable_business_command_id"`
	StableRequestDigest             bool `json:"stable_request_digest"`
	StaleFenceRejected              bool `json:"stale_fence_rejected"`
	TechnicalFailureNotExhausted    bool `json:"technical_failure_not_exhausted"`
	TechnicalQueryZeroBudgetDelta   bool `json:"technical_query_zero_budget_delta"`
}

// previewRecoverySmokeEvidenceCounts 只保存预算和 Claim 数量，不保存任意敏感载荷。
type previewRecoverySmokeEvidenceCounts struct {
	ClaimedAfterExhaustion int `json:"claimed_after_exhaustion"`
	FollowerPending        int `json:"follower_pending"`
	QueryCalls             int `json:"query_calls"`
	ResendAttempts         int `json:"resend_attempts"`
	ResendLimit            int `json:"resend_limit"`
	SaveCalls              int `json:"save_calls"`
}

// previewRecoverySmokeClock 为 Graph 恢复路径提供非零固定 UTC 时间；恢复未完成时不会生成终态 Card。
type previewRecoverySmokeClock struct{ now time.Time }

// Now 返回本轮探针冻结时间。
func (clock previewRecoverySmokeClock) Now() time.Time { return clock.now }

// previewRecoverySmokeBusinessAdapter 只在测试侧模拟 Business 协议结果，并逐次核对原命令没有换键或漂移。
type previewRecoverySmokeBusinessAdapter struct {
	t             *testing.T
	expected      plancreationspec.DraftCommand
	queryOutcomes []string
	queryCalls    int
	saveCalls     int
	commandExact  bool
}

// GetCreationSpecContext 不应被 Recover 调用；命中即失败，防止恢复重跑业务上下文。
func (adapter *previewRecoverySmokeBusinessAdapter) GetCreationSpecContext(
	context.Context,
	string,
	string,
	string,
) (plancreationspec.DomainContext, error) {
	adapter.t.Fatal("Recover 不得重新加载 Business Context")
	return plancreationspec.DomainContext{}, plancreationspec.ErrBusinessTechnical
}

// SaveCreationSpecDraft 模拟请求结果未知；Graph 必须随后 Query 同一命令，不能换键重试。
func (adapter *previewRecoverySmokeBusinessAdapter) SaveCreationSpecDraft(
	_ context.Context,
	command plancreationspec.DraftCommand,
) (plancreationspec.SaveDisposition, plancreationspec.Resource, error) {
	adapter.saveCalls++
	adapter.assertSameCommand(command)
	return "", plancreationspec.Resource{}, plancreationspec.ErrBusinessUnknownOutcome
}

// QueryCreationSpecDraftCommand 按脚本依次返回 technical/not_found，真实预算 CAS 仍由 Graph 调正式 Repository。
func (adapter *previewRecoverySmokeBusinessAdapter) QueryCreationSpecDraftCommand(
	_ context.Context,
	command plancreationspec.DraftCommand,
) (string, *plancreationspec.Resource, error) {
	adapter.queryCalls++
	adapter.assertSameCommand(command)
	if adapter.queryCalls > len(adapter.queryOutcomes) {
		adapter.t.Fatalf("Business adapter Query 超出冻结脚本: call=%d", adapter.queryCalls)
	}
	switch adapter.queryOutcomes[adapter.queryCalls-1] {
	case "technical":
		return "", nil, plancreationspec.ErrBusinessTechnical
	case "not_found":
		return "not_found", nil, nil
	default:
		adapter.t.Fatalf("Business adapter 含未知脚本结果: %q", adapter.queryOutcomes[adapter.queryCalls-1])
		return "", nil, plancreationspec.ErrBusinessTechnical
	}
}

// assertSameCommand 要求 Query/Save 的 command_id、request_digest、内容和全部稳定字段逐值不变。
func (adapter *previewRecoverySmokeBusinessAdapter) assertSameCommand(command plancreationspec.DraftCommand) {
	if !reflect.DeepEqual(command, adapter.expected) {
		adapter.commandExact = false
		adapter.t.Fatalf("恢复 Query/Save 命令发生漂移")
	}
}

// TestCreationSpecPreviewDurableRecoveryPostgreSQLSmoke 使用显式独立 PostgreSQL 验证 durable command、Fence、重发预算和 HOL。
// 未提供专用 DSN 时默认跳过，避免普通单元测试误连开发或生产数据库。
func TestCreationSpecPreviewDurableRecoveryPostgreSQLSmoke(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(previewRecoverySmokeDSNEnv))
	if dsn == "" {
		t.Skip("未设置 DORA_PLAN_SPEC_PREVIEW_RECOVERY_SMOKE_DSN，跳过真实 PostgreSQL 恢复探针")
	}
	resultPath := strings.TrimSpace(os.Getenv(previewRecoverySmokeResultEnv))
	if resultPath == "" {
		t.Fatal("真实 PostgreSQL 恢复探针必须设置 DORA_PLAN_SPEC_PREVIEW_RECOVERY_SMOKE_RESULT")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接 Preview 恢复专用 PostgreSQL 失败: %v", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("关闭 Preview 恢复 PostgreSQL 失败: %v", closeErr)
		}
	}()
	if err := client.VerifyCreationSpecPreviewSchema(ctx, 5*time.Second); err != nil {
		t.Fatalf("Preview 恢复 Schema 未就绪: %v", err)
	}

	ids := newPreviewRecoverySmokeIDs(t)
	defer cleanupPreviewRecoverySmokeRows(t, client, ids)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("生成恢复探针临时内容密钥失败: %v", err)
	}
	protector, err := contentcrypto.NewAES256GCMProtector(key, "preview-recovery-smoke-v1")
	if err != nil {
		t.Fatalf("创建恢复探针 AEAD 保护器失败: %v", err)
	}
	repository, err := NewCreationSpecPreviewRepository(client, protector, previewRecoverySmokeLimit)
	if err != nil {
		t.Fatalf("创建正式 Preview Repository 失败: %v", err)
	}

	intent := previewRecoverySmokeIntent()
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		t.Fatalf("编码恢复探针 Intent 失败: %v", err)
	}
	intentDigest, err := plancreationspec.IntentDigest(intent)
	if err != nil {
		t.Fatalf("计算恢复探针 Intent 摘要失败: %v", err)
	}
	protectedIntent, err := protector.Protect(ctx, intentJSON)
	if err != nil {
		t.Fatalf("加密恢复探针 Intent 失败: %v", err)
	}
	ownerV1 := "preview-recovery-smoke-owner-v1"
	ownerV2 := "preview-recovery-smoke-owner-v2"
	now := time.Now().UTC()
	seedPreviewRecoverySmokeRows(t, client, ids, intentDigest, protectedIntent, ownerV1, now)

	trustedV1 := previewRecoverySmokeTrusted(ids, ownerV1, 1)
	command := previewRecoverySmokeCommand(t, trustedV1)
	if err := repository.PrepareCommand(ctx, command); err != nil {
		t.Fatalf("PrepareCommand 未能加密冻结原命令: %v", err)
	}
	preparedFacts := loadPreviewRecoverySmokeReceiptFacts(t, client, ids.ToolCallID)
	if preparedFacts.Stage != "business_prepared" || !preparedFacts.CiphertextPresent ||
		!preparedFacts.KeyReferencePresent || !preparedFacts.PayloadDigestValid ||
		preparedFacts.ResendAttempts != 0 || preparedFacts.ResendLimit != previewRecoverySmokeLimit {
		t.Fatalf("PrepareCommand 权威事实不完整: %+v", preparedFacts)
	}

	// 模拟进程重启：丢弃原保护器/Repository 实例，只复用同一启动密钥材料创建全新实例。
	restartedProtector, err := contentcrypto.NewAES256GCMProtector(key, "preview-recovery-smoke-v1")
	if err != nil {
		t.Fatalf("重建恢复探针 AEAD 保护器失败: %v", err)
	}
	repository, err = NewCreationSpecPreviewRepository(client, restartedProtector, previewRecoverySmokeLimit)
	if err != nil {
		t.Fatalf("重建正式 Preview Repository 失败: %v", err)
	}
	advancePreviewRecoverySmokeFence(t, client, ids, ownerV2, 2)
	trustedV2 := previewRecoverySmokeTrusted(ids, ownerV2, 2)
	staleFenceRejected := false
	if _, err := repository.ReplayRecovery(ctx, trustedV1); errors.Is(err, previewruntime.ErrFenceLost) {
		staleFenceRejected = true
	}
	replayedRecovery, err := repository.ReplayRecovery(ctx, trustedV2)
	if err != nil || replayedRecovery == nil {
		t.Fatalf("新 Owner/Fence 未能重放 durable command: recovery=%+v err=%v", replayedRecovery, err)
	}
	recovery := *replayedRecovery
	restartedOwnerFenceRebuilt := recovery.Command.TrustedContext.Owner == ownerV2 &&
		recovery.Command.TrustedContext.FenceToken == 2
	stableBusinessCommandID := recovery.BusinessCommandID == ids.BusinessCommandID &&
		recovery.Command.TrustedContext.BusinessCommandID == ids.BusinessCommandID
	stableRequestDigest := recovery.RequestDigest == command.RequestDigest &&
		recovery.Command.RequestDigest == command.RequestDigest

	adapter := &previewRecoverySmokeBusinessAdapter{
		t: t, expected: recovery.Command, commandExact: true,
		queryOutcomes: []string{
			"technical",
			"not_found", "technical",
			"not_found", "technical",
			"not_found", "not_found",
		},
	}
	graph, err := plancreationspec.Compile(
		ctx, previewchatmodel.NewFakeProposal(), adapter, repository,
		previewRecoverySmokeClock{now: time.Date(2026, 7, 16, 16, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatalf("编译正式 plan_creation_spec 恢复 Graph 失败: %v", err)
	}

	// 首次 Query 是技术失败：正式 Recover 必须保持 query-only，重发预算仍为零。
	outcome, err := graph.Recover(ctx, trustedV2, recovery)
	if err != nil || outcome.Recovery == nil || outcome.Terminal != nil ||
		outcome.Recovery.ResendAttempts != 0 || outcome.Recovery.ResendExhausted {
		t.Fatalf("技术 Query 被错误解释为可重发或终态: outcome=%+v err=%v", outcome, err)
	}
	if err := repository.MarkRecovery(ctx, trustedV2, *outcome.Recovery); err != nil {
		t.Fatalf("记录技术失败后的 query-only 恢复阶段失败: %v", err)
	}
	technicalFacts := loadPreviewRecoverySmokeReceiptFacts(t, client, ids.ToolCallID)
	technicalFailureNotExhausted := technicalFacts.Stage == "business_unknown" &&
		technicalFacts.ResendAttempts == 0 && !technicalFacts.ExhaustedAtPresent && !technicalFacts.ExhaustedCodeMatches
	technicalQueryZeroBudgetDelta := outcome.Recovery.ResendAttempts == 0 && technicalFacts.ResendAttempts == 0

	// 三轮都由正式 Recover 先收到权威 not_found，再由 Repository CAS 预算并同键 Save；前两轮 post Query
	// 技术失败，最终轮 post Query 权威 not_found 才设置 ResendExhausted。
	for attempt := 1; attempt <= previewRecoverySmokeLimit; attempt++ {
		replayed, replayErr := repository.ReplayRecovery(ctx, trustedV2)
		if replayErr != nil || replayed == nil {
			t.Fatalf("第 %d 轮恢复前读取 durable command 失败: recovery=%+v err=%v", attempt, replayed, replayErr)
		}
		outcome, err = graph.Recover(ctx, trustedV2, *replayed)
		if err != nil || outcome.Recovery == nil || outcome.Terminal != nil ||
			outcome.Recovery.ResendAttempts != attempt ||
			(outcome.Recovery.ResendExhausted != (attempt == previewRecoverySmokeLimit)) {
			t.Fatalf("第 %d 轮正式 Recover 结果不符合预算/权威 Query: outcome=%+v err=%v", attempt, outcome, err)
		}
		if err := repository.MarkRecovery(ctx, trustedV2, *outcome.Recovery); err != nil {
			t.Fatalf("第 %d 轮记录恢复结果失败: %v", attempt, err)
		}
	}
	exhaustedFacts := loadPreviewRecoverySmokeReceiptFacts(t, client, ids.ToolCallID)
	authoritativeNotFoundReserveCAS := adapter.queryCalls == 7 && adapter.saveCalls == previewRecoverySmokeLimit &&
		exhaustedFacts.ResendAttempts == previewRecoverySmokeLimit
	formalGraphRecovery := adapter.queryCalls == 7 && adapter.saveCalls == previewRecoverySmokeLimit

	releasePreviewRecoverySmokeLaneAndSeedFollower(t, client, ids, intentDigest, protectedIntent, now)
	var otherClaimable int64
	if err := client.db.WithContext(ctx).Raw(`
		SELECT count(*)
		FROM agent.creation_spec_preview_run AS run
		JOIN agent.session_input AS input_record ON input_record.id = run.input_id
		WHERE run.session_id <> ? AND input_record.status IN ('pending','retry_wait','recovery_pending','claimed','running')`,
		ids.SessionID,
	).Scan(&otherClaimable).Error; err != nil {
		t.Fatalf("检查专用数据库是否存在其他可领取 Preview 输入失败: %v", err)
	}
	if otherClaimable != 0 {
		t.Fatalf("恢复探针要求独立数据库，发现 %d 条其他未决 Preview 输入", otherClaimable)
	}
	claimed, err := repository.ClaimNext(ctx, "preview-recovery-smoke-claim-after-exhaustion", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatalf("exhausted/HOL Claim 检查失败: %v", err)
	}
	claimedAfterExhaustion := 0
	if claimed != nil {
		claimedAfterExhaustion = 1
		t.Fatalf("exhausted 输入或其 HOL 后继被错误领取: %+v", claimed)
	}
	var followerPending int
	if err := client.db.WithContext(ctx).Raw(`
		SELECT count(*) FROM agent.session_input
		WHERE id = ? AND session_id = ? AND status = 'pending' AND attempts = 0 AND fence_token = 0`,
		ids.FollowerInputID, ids.SessionID,
	).Scan(&followerPending).Error; err != nil {
		t.Fatalf("读取 HOL 后继状态失败: %v", err)
	}

	evidence := previewRecoverySmokeEvidence{
		SchemaVersion: "plan_spec_preview.durable_recovery_postgresql.v1",
		Status:        "passed",
		Assertions: previewRecoverySmokeEvidenceAssertions{
			AuthoritativeNotFoundReserveCAS: authoritativeNotFoundReserveCAS,
			BusinessAdapterCommandExact:     adapter.commandExact,
			DurableCiphertextPresent:        preparedFacts.CiphertextPresent,
			DurableKeyReferencePresent:      preparedFacts.KeyReferencePresent,
			DurablePayloadDigestValid:       preparedFacts.PayloadDigestValid,
			ExhaustedMarkedExplicitly: exhaustedFacts.Stage == "business_resend_exhausted" &&
				exhaustedFacts.ExhaustedAtPresent && exhaustedFacts.ExhaustedCodeMatches,
			ExhaustedNotClaimed:           claimedAfterExhaustion == 0,
			FormalGraphRecovery:           formalGraphRecovery,
			HeadOfLineNotSkipped:          claimedAfterExhaustion == 0 && followerPending == 1,
			RealPostgreSQL:                true,
			ResendLimitFrozen:             preparedFacts.ResendLimit == previewRecoverySmokeLimit,
			RestartedOwnerFenceRebuilt:    restartedOwnerFenceRebuilt,
			ResultPayloadAbsent:           exhaustedFacts.ResultPayloadAbsent,
			StableBusinessCommandID:       stableBusinessCommandID,
			StableRequestDigest:           stableRequestDigest,
			StaleFenceRejected:            staleFenceRejected,
			TechnicalFailureNotExhausted:  technicalFailureNotExhausted,
			TechnicalQueryZeroBudgetDelta: technicalQueryZeroBudgetDelta,
		},
		Counts: previewRecoverySmokeEvidenceCounts{
			ClaimedAfterExhaustion: claimedAfterExhaustion,
			FollowerPending:        followerPending,
			QueryCalls:             adapter.queryCalls,
			ResendAttempts:         exhaustedFacts.ResendAttempts,
			ResendLimit:            exhaustedFacts.ResendLimit,
			SaveCalls:              adapter.saveCalls,
		},
	}
	if !allPreviewRecoverySmokeAssertions(evidence.Assertions) || evidence.Counts.ResendAttempts != previewRecoverySmokeLimit ||
		evidence.Counts.ResendLimit != previewRecoverySmokeLimit || evidence.Counts.FollowerPending != 1 ||
		evidence.Counts.QueryCalls != 7 || evidence.Counts.SaveCalls != previewRecoverySmokeLimit {
		t.Fatalf("恢复探针 Evidence 未闭合: %+v", evidence)
	}
	writePreviewRecoverySmokeEvidence(t, resultPath, evidence)
}

// newPreviewRecoverySmokeIDs 为每次运行生成独立 UUIDv7，重复执行不会复用旧业务身份。
func newPreviewRecoverySmokeIDs(t *testing.T) previewRecoverySmokeIDs {
	t.Helper()
	newID := func() string {
		value, err := (idgen.UUIDv7{}).New()
		if err != nil {
			t.Fatalf("生成恢复探针 UUIDv7 失败: %v", err)
		}
		return value
	}
	return previewRecoverySmokeIDs{
		SessionID: newID(), ProjectID: newID(), UserID: newID(), InputID: newID(), MessageID: newID(),
		RequestID: newID(), IdempotencyKey: newID(), TurnID: newID(), RunID: newID(), ToolCallID: newID(),
		BusinessCommandID: newID(), TerminalEventID: newID(), FollowerInputID: newID(), FollowerMessageID: newID(),
		FollowerRequestID: newID(), FollowerIdempotency: newID(), FollowerTurnID: newID(), FollowerRunID: newID(),
		FollowerToolCallID: newID(), FollowerCommandID: newID(), FollowerEventID: newID(),
	}
}

// previewRecoverySmokeIntent 返回不会写入 Evidence 的最小严格输入。
func previewRecoverySmokeIntent() plancreationspec.Intent {
	return plancreationspec.Intent{
		SchemaVersion: plancreationspec.IntentSchemaVersion,
		Goal:          "验证可恢复创作规格", DeliverableType: "video", Locale: "zh-CN",
		Constraints: []string{"仅验证恢复一致性"},
	}
}

// previewRecoverySmokeTrusted 构造只用于正式 Repository 调用的可信上下文。
func previewRecoverySmokeTrusted(ids previewRecoverySmokeIDs, owner string, fence int64) plancreationspec.TrustedContext {
	return plancreationspec.TrustedContext{
		Owner: owner, RequestID: ids.RequestID, UserID: ids.UserID, ProjectID: ids.ProjectID,
		SessionID: ids.SessionID, InputID: ids.InputID, TurnID: ids.TurnID, RunID: ids.RunID,
		ToolCallID: ids.ToolCallID, BusinessCommandID: ids.BusinessCommandID,
		PromptVersion: plancreationspec.PromptVersion, ValidatorVersion: plancreationspec.ValidatorVersion,
		FenceToken: fence,
	}
}

// previewRecoverySmokeCommand 构造严格 Draft Command，并用生产摘要函数冻结 request digest。
func previewRecoverySmokeCommand(t *testing.T, trusted plancreationspec.TrustedContext) plancreationspec.DraftCommand {
	t.Helper()
	command := plancreationspec.DraftCommand{
		TrustedContext: trusted,
		DomainContext:  plancreationspec.DomainContext{ProjectID: trusted.ProjectID, ProjectVersion: 1},
		Content: plancreationspec.Content{
			Title: "恢复一致性创作规格", Goal: "验证可恢复创作规格", DeliverableType: "video",
			Audience: "", Locale: "zh-CN",
			Phases: []plancreationspec.Phase{{
				Key: "phase_1", Title: "恢复验证", Objective: "验证 durable command", Output: "权威恢复证据",
			}},
			Constraints: []string{"仅验证恢复一致性"}, AcceptanceCriteria: []string{"同键恢复且后继输入不越过 HOL"},
		},
	}
	digest, err := plancreationspec.SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("计算恢复探针 Save 摘要失败: %v", err)
	}
	command.RequestDigest = digest
	return command
}

// seedPreviewRecoverySmokeRows 只创建 Prepare/Replay 所需的本 Session 最小权威行。
func seedPreviewRecoverySmokeRows(
	t *testing.T,
	client *Client,
	ids previewRecoverySmokeIDs,
	intentDigest string,
	protected session.ProtectedContent,
	owner string,
	now time.Time,
) {
	t.Helper()
	leaseUntil := now.Add(10 * time.Minute)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent.session (id,project_id,user_id,status,version,created_at,updated_at) VALUES (?,?,?,'active',1,?,?)`, []any{ids.SessionID, ids.ProjectID, ids.UserID, now, now}},
		{`INSERT INTO agent.session_runtime_lease (session_id,lease_owner,lease_until,fence_token,version,updated_at) VALUES (?,?,?,?,1,?)`, []any{ids.SessionID, owner, leaseUntil, 1, now}},
		{`INSERT INTO agent.session_message (id,session_id,message_seq,role,content_ciphertext,content_key_version,content_digest,source_kind,source_id,created_at) VALUES (?,?,1,'user',?,?,?,'creation_spec_preview',?,?)`, []any{ids.MessageID, ids.SessionID, protected.Ciphertext, protected.KeyVersion, intentDigest, ids.IdempotencyKey, now}},
		{`INSERT INTO agent.session_input (id,session_id,source_type,source_id,message_id,status,enqueue_seq,attempts,available_at,lease_owner,lease_until,fence_token,created_at,updated_at) VALUES (?,?, 'creation_spec_preview', ?, ?, 'running', 1, 1, ?, ?, ?, 1, ?, ?)`, []any{ids.InputID, ids.SessionID, ids.IdempotencyKey, ids.MessageID, now, owner, leaseUntil, now, now}},
		{`INSERT INTO agent.creation_spec_preview_run (input_id,request_id,idempotency_key,request_digest,session_id,user_id,project_id,message_id,turn_id,run_id,tool_call_id,business_command_id,terminal_event_id,prompt_version,validator_version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, []any{ids.InputID, ids.RequestID, ids.IdempotencyKey, intentDigest, ids.SessionID, ids.UserID, ids.ProjectID, ids.MessageID, ids.TurnID, ids.RunID, ids.ToolCallID, ids.BusinessCommandID, ids.TerminalEventID, plancreationspec.PromptVersion, plancreationspec.ValidatorVersion, now, now}},
		{`INSERT INTO agent.creation_spec_preview_tool_receipt (tool_call_id,request_digest,stage,business_command_id,created_at,updated_at) VALUES (?,?,'pending',?,?,?)`, []any{ids.ToolCallID, intentDigest, ids.BusinessCommandID, now, now}},
	}
	for _, statement := range statements {
		if execErr := client.db.Exec(statement.query, statement.args...).Error; execErr != nil {
			t.Fatalf("写入恢复探针最小权威行失败: %v", execErr)
		}
	}
}

// advancePreviewRecoverySmokeFence 模拟进程重启后新 Owner/Fence 接管同一 running 输入。
func advancePreviewRecoverySmokeFence(t *testing.T, client *Client, ids previewRecoverySmokeIDs, owner string, fence int64) {
	t.Helper()
	leaseUntil := time.Now().UTC().Add(10 * time.Minute)
	if err := client.db.Transaction(func(tx *gorm.DB) error {
		if result := tx.Exec(`UPDATE agent.session_input SET lease_owner=?,lease_until=?,fence_token=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND session_id=?`, owner, leaseUntil, fence, ids.InputID, ids.SessionID); result.Error != nil || result.RowsAffected != 1 {
			return fmt.Errorf("update recovery input fence")
		}
		if result := tx.Exec(`UPDATE agent.session_runtime_lease SET lease_owner=?,lease_until=?,fence_token=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE session_id=?`, owner, leaseUntil, fence, ids.SessionID); result.Error != nil || result.RowsAffected != 1 {
			return fmt.Errorf("update recovery lane fence")
		}
		return nil
	}); err != nil {
		t.Fatalf("模拟恢复进程新 Fence 失败: %v", err)
	}
}

// loadPreviewRecoverySmokeReceiptFacts 只投影脱敏列，不把密文或 Key Version 值读入测试内存。
func loadPreviewRecoverySmokeReceiptFacts(t *testing.T, client *Client, toolCallID string) previewRecoverySmokeReceiptFacts {
	t.Helper()
	var facts previewRecoverySmokeReceiptFacts
	if err := client.db.Raw(`
		SELECT stage,
		       business_command_ciphertext IS NOT NULL AS ciphertext_present,
		       business_command_key_version IS NOT NULL AS key_reference_present,
		       COALESCE(business_command_payload_digest ~ '^[0-9a-f]{64}$', false) AS payload_digest_valid,
		       business_resend_attempts AS resend_attempts,
		       COALESCE(business_resend_limit, 0) AS resend_limit,
		       business_resend_exhausted_at IS NOT NULL AS exhausted_at_present,
		       COALESCE(error_code = ?, false) AS exhausted_code_matches,
		       result_ciphertext IS NULL AND result_key_version IS NULL AND result_digest IS NULL AS result_payload_absent
		FROM agent.creation_spec_preview_tool_receipt WHERE tool_call_id = ?`,
		plancreationspec.RecoveryCodeBusinessResendExhausted, toolCallID,
	).Scan(&facts).Error; err != nil {
		t.Fatalf("读取脱敏恢复 Receipt 事实失败: %v", err)
	}
	return facts
}

// releasePreviewRecoverySmokeLaneAndSeedFollower 将 exhausted 输入保持为 recovery_pending，并创建同 Session 的后继 Input。
func releasePreviewRecoverySmokeLaneAndSeedFollower(
	t *testing.T,
	client *Client,
	ids previewRecoverySmokeIDs,
	intentDigest string,
	protected session.ProtectedContent,
	now time.Time,
) {
	t.Helper()
	if err := client.db.Transaction(func(tx *gorm.DB) error {
		if result := tx.Exec(`UPDATE agent.session_input SET status='recovery_pending',lease_owner=NULL,lease_until=NULL,available_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=?`, ids.InputID); result.Error != nil || result.RowsAffected != 1 {
			return fmt.Errorf("release exhausted input")
		}
		if result := tx.Exec(`UPDATE agent.session_runtime_lease SET lease_owner=NULL,lease_until=NULL,updated_at=CURRENT_TIMESTAMP WHERE session_id=?`, ids.SessionID); result.Error != nil || result.RowsAffected != 1 {
			return fmt.Errorf("release exhausted lane")
		}
		if err := tx.Exec(`INSERT INTO agent.session_message (id,session_id,message_seq,role,content_ciphertext,content_key_version,content_digest,source_kind,source_id,created_at) VALUES (?,?,2,'user',?,?,?,'creation_spec_preview',?,?)`, ids.FollowerMessageID, ids.SessionID, protected.Ciphertext, protected.KeyVersion, intentDigest, ids.FollowerIdempotency, now).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO agent.session_input (id,session_id,source_type,source_id,message_id,status,enqueue_seq,attempts,available_at,fence_token,created_at,updated_at) VALUES (?,?,'creation_spec_preview',?,?,'pending',2,0,CURRENT_TIMESTAMP,0,?,?)`, ids.FollowerInputID, ids.SessionID, ids.FollowerIdempotency, ids.FollowerMessageID, now, now).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO agent.creation_spec_preview_run (input_id,request_id,idempotency_key,request_digest,session_id,user_id,project_id,message_id,turn_id,run_id,tool_call_id,business_command_id,terminal_event_id,prompt_version,validator_version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, ids.FollowerInputID, ids.FollowerRequestID, ids.FollowerIdempotency, intentDigest, ids.SessionID, ids.UserID, ids.ProjectID, ids.FollowerMessageID, ids.FollowerTurnID, ids.FollowerRunID, ids.FollowerToolCallID, ids.FollowerCommandID, ids.FollowerEventID, plancreationspec.PromptVersion, plancreationspec.ValidatorVersion, now, now).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO agent.creation_spec_preview_tool_receipt (tool_call_id,request_digest,stage,business_command_id,created_at,updated_at) VALUES (?,?,'pending',?,?,?)`, ids.FollowerToolCallID, intentDigest, ids.FollowerCommandID, now, now).Error
	}); err != nil {
		t.Fatalf("创建 exhausted HOL 后继失败: %v", err)
	}
}

// cleanupPreviewRecoverySmokeRows 按本轮唯一 Session/Tool ID 清理，不扫描或删除其他测试数据。
func cleanupPreviewRecoverySmokeRows(t *testing.T, client *Client, ids previewRecoverySmokeIDs) {
	t.Helper()
	if client == nil || client.db == nil {
		return
	}
	for _, cleanup := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM agent.creation_spec_preview_tool_receipt WHERE tool_call_id IN (?,?)`, []any{ids.ToolCallID, ids.FollowerToolCallID}},
		{`DELETE FROM agent.creation_spec_preview_run WHERE session_id=?`, []any{ids.SessionID}},
		{`DELETE FROM agent.session_input WHERE session_id=?`, []any{ids.SessionID}},
		{`DELETE FROM agent.session_message WHERE session_id=?`, []any{ids.SessionID}},
		{`DELETE FROM agent.session_runtime_lease WHERE session_id=?`, []any{ids.SessionID}},
		{`DELETE FROM agent.session WHERE id=?`, []any{ids.SessionID}},
	} {
		if err := client.db.Exec(cleanup.query, cleanup.args...).Error; err != nil {
			t.Errorf("清理恢复探针独占行失败: %v", err)
		}
	}
}

// allPreviewRecoverySmokeAssertions 防止漏写 false 却发布 passed Evidence。
func allPreviewRecoverySmokeAssertions(value previewRecoverySmokeEvidenceAssertions) bool {
	return value.AuthoritativeNotFoundReserveCAS && value.BusinessAdapterCommandExact && value.DurableCiphertextPresent &&
		value.DurableKeyReferencePresent && value.DurablePayloadDigestValid && value.ExhaustedMarkedExplicitly &&
		value.ExhaustedNotClaimed && value.FormalGraphRecovery && value.HeadOfLineNotSkipped && value.RealPostgreSQL &&
		value.ResendLimitFrozen &&
		value.RestartedOwnerFenceRebuilt && value.ResultPayloadAbsent && value.StableBusinessCommandID &&
		value.StableRequestDigest && value.StaleFenceRejected && value.TechnicalFailureNotExhausted &&
		value.TechnicalQueryZeroBudgetDelta
}

// writePreviewRecoverySmokeEvidence 使用同目录原子 rename 发布 0600 strict JSON。
func writePreviewRecoverySmokeEvidence(t *testing.T, target string, evidence previewRecoverySmokeEvidence) {
	t.Helper()
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("编码恢复探针 Evidence 失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("创建恢复探针 Evidence 目录失败: %v", err)
	}
	temporary := fmt.Sprintf("%s.%d.tmp", target, os.Getpid())
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		t.Fatalf("写入恢复探针临时 Evidence 失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(temporary) })
	if err := os.Chmod(temporary, 0o600); err != nil {
		t.Fatalf("收紧恢复探针临时 Evidence 权限失败: %v", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		t.Fatalf("原子发布恢复探针 Evidence 失败: %v", err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatalf("收紧恢复探针 Evidence 权限失败: %v", err)
	}
}
