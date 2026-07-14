package skill

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// governanceTestClock 为治理应用测试返回固定时间。
type governanceTestClock struct {
	// now 是测试冻结时间。
	now time.Time
}

// Now 返回测试冻结时间。
func (clock governanceTestClock) Now() time.Time { return clock.now }

// governanceTestIDs 按顺序返回预置 UUIDv7 或注入生成错误。
type governanceTestIDs struct {
	// values 是待返回的 UUIDv7 序列。
	values []string
	// err 是需要注入的生成错误。
	err error
	// calls 记录生成调用次数。
	calls int
}

// New 返回下一个预置 UUIDv7，序列耗尽时返回错误。
func (generator *governanceTestIDs) New() (string, error) {
	generator.calls++
	if generator.err != nil {
		return "", generator.err
	}
	if len(generator.values) == 0 {
		return "", errors.New("governance test ID sequence exhausted")
	}
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

// governanceRepositoryStub 捕获治理应用服务交给独立 Repository 的参数。
type governanceRepositoryStub struct {
	// listStatus 是最近一次列表状态。
	listStatus GovernanceStatus
	// listBoundary 是最近一次列表边界。
	listBoundary *GovernanceQueueBoundary
	// listLimit 是最近一次列表上限。
	listLimit int
	// listPage 是列表预置结果。
	listPage GovernanceQueuePage
	// listErr 是列表注入错误。
	listErr error
	// listCalls 是列表调用次数。
	listCalls int
	// detailSkillID 是最近一次详情 Skill ID。
	detailSkillID string
	// detailState 是详情预置结果。
	detailState GovernanceState
	// detailErr 是详情注入错误。
	detailErr error
	// detailCalls 是详情调用次数。
	detailCalls int
	// transitionCommand 是最近一次治理迁移命令。
	transitionCommand GovernanceTransitionRepositoryCommand
	// transitionResult 是迁移预置结果。
	transitionResult GovernanceTransitionRepositoryResult
	// transitionErr 是迁移注入错误。
	transitionErr error
	// transitionCalls 是迁移调用次数。
	transitionCalls int
}

// ListGovernance 捕获列表参数并返回预置页面。
func (repository *governanceRepositoryStub) ListGovernance(_ context.Context, status GovernanceStatus, boundary *GovernanceQueueBoundary, limit int) (GovernanceQueuePage, error) {
	repository.listCalls++
	repository.listStatus = status
	repository.listBoundary = boundary
	repository.listLimit = limit
	return repository.listPage, repository.listErr
}

// FindGovernanceDetail 捕获详情 ID 并返回预置权威状态。
func (repository *governanceRepositoryStub) FindGovernanceDetail(_ context.Context, skillID string) (GovernanceState, error) {
	repository.detailCalls++
	repository.detailSkillID = skillID
	return repository.detailState, repository.detailErr
}

// TransitionGovernance 捕获原子命令并返回预置冻结结果。
func (repository *governanceRepositoryStub) TransitionGovernance(_ context.Context, command GovernanceTransitionRepositoryCommand) (GovernanceTransitionRepositoryResult, error) {
	repository.transitionCalls++
	repository.transitionCommand = command
	return repository.transitionResult, repository.transitionErr
}

// newGovernanceTestUUIDv7 创建一个规范小写 UUIDv7。
func newGovernanceTestUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// newGovernanceTestState 构造完成 Canonical 校验的当前发布治理状态。
func newGovernanceTestState(t *testing.T, status GovernanceStatus, epoch int64) GovernanceState {
	t.Helper()
	definition, err := NormalizeDefinitionV1(validDefinitionForTest())
	if err != nil {
		t.Fatal(err)
	}
	canonical, digest, err := CanonicalDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	publishedAt := time.Date(2026, 7, 14, 9, 30, 0, 123, time.UTC)
	return GovernanceState{
		SkillID: skillID, CurrentPublishedSnapshotID: snapshotID, PublicationRevision: 2,
		GovernanceStatus: status, GovernanceEpoch: epoch, SkillVersion: 5,
		Published: PublishedSnapshot{
			ID: snapshotID, SkillID: skillID, SourceContentRevisionID: newGovernanceTestUUIDv7(t),
			ReviewSubmissionID: newGovernanceTestUUIDv7(t), PublicationRevision: 2,
			Definition: definition, CanonicalJSON: canonical, ContentDigest: digest,
			PublishedByUserID: newGovernanceTestUUIDv7(t), PublishedAt: publishedAt,
		},
	}
}

// newGovernanceTestPrincipal 构造只持有正式治理 capability 的可信 Principal。
func newGovernanceTestPrincipal(t *testing.T) GovernancePrincipal {
	t.Helper()
	return GovernancePrincipal{UserID: newGovernanceTestUUIDv7(t), Capabilities: []string{GovernanceCapability}}
}

// newGovernanceTestService 创建使用两个预置决定 ID 的治理应用服务。
func newGovernanceTestService(t *testing.T, repository GovernanceRepository, now time.Time) (*GovernanceService, *governanceTestIDs) {
	t.Helper()
	ids := &governanceTestIDs{values: []string{newGovernanceTestUUIDv7(t), newGovernanceTestUUIDv7(t)}}
	service, err := NewGovernanceService(repository, governanceTestClock{now: now}, ids)
	if err != nil {
		t.Fatal(err)
	}
	return service, ids
}

func TestNewGovernanceServiceRequiresAllDependencies(t *testing.T) {
	repository := &governanceRepositoryStub{}
	clock := governanceTestClock{now: time.Now()}
	ids := &governanceTestIDs{values: []string{newGovernanceTestUUIDv7(t)}}
	for _, create := range []func() (*GovernanceService, error){
		func() (*GovernanceService, error) { return NewGovernanceService(nil, clock, ids) },
		func() (*GovernanceService, error) { return NewGovernanceService(repository, nil, ids) },
		func() (*GovernanceService, error) { return NewGovernanceService(repository, clock, nil) },
	} {
		if _, err := create(); err == nil {
			t.Fatal("NewGovernanceService accepted a missing dependency")
		}
	}
}

func TestGovernanceListUsesKeysetAndSafeProjection(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	first := GovernanceQueueItem{
		SkillID: newGovernanceTestUUIDv7(t), PublishedSnapshotID: newGovernanceTestUUIDv7(t),
		Name: "第一项", Summary: "摘要一", Category: "视频", PublishedAt: now,
		GovernanceStatus: GovernanceStatusActive, GovernanceEpoch: 1,
	}
	second := GovernanceQueueItem{
		SkillID: newGovernanceTestUUIDv7(t), PublishedSnapshotID: newGovernanceTestUUIDv7(t),
		Name: "第二项", Summary: "摘要二", Category: "图像", PublishedAt: now.Add(-time.Second),
		GovernanceStatus: GovernanceStatusActive, GovernanceEpoch: 3,
	}
	repository := &governanceRepositoryStub{listPage: GovernanceQueuePage{Items: []GovernanceQueueItem{first, second}, HasMore: true}}
	service, _ := newGovernanceTestService(t, repository, now)

	result, err := service.ListGovernance(context.Background(), newGovernanceTestPrincipal(t), "active", "")
	if err != nil {
		t.Fatalf("ListGovernance() error = %v", err)
	}
	if repository.listStatus != GovernanceStatusActive || repository.listBoundary != nil || repository.listLimit != defaultGovernancePageSize {
		t.Fatalf("unexpected repository list input: status=%s boundary=%+v limit=%d", repository.listStatus, repository.listBoundary, repository.listLimit)
	}
	if len(result.Items) != 2 || result.Items[0].PublishedAt != first.PublishedAt.Format(time.RFC3339Nano) ||
		strings.Join(result.Items[0].AllowedActions, ",") != "suspend,offline" || result.NextCursor == "" {
		t.Fatalf("unexpected governance list result: %+v", result)
	}
	boundary, err := decodeGovernanceQueueCursor(GovernanceStatusActive, result.NextCursor)
	if err != nil || boundary.PublishedSnapshotID != second.PublishedSnapshotID || !boundary.PublishedAt.Equal(second.PublishedAt) {
		t.Fatalf("next cursor did not freeze last item: boundary=%+v err=%v", boundary, err)
	}
}

func TestGovernanceListRejectsCapabilityStatusCursorAndBrokenPage(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	item := GovernanceQueueItem{
		SkillID: newGovernanceTestUUIDv7(t), PublishedSnapshotID: newGovernanceTestUUIDv7(t), Name: "合法",
		PublishedAt: now, GovernanceStatus: GovernanceStatusActive, GovernanceEpoch: 1,
	}
	repository := &governanceRepositoryStub{listPage: GovernanceQueuePage{Items: []GovernanceQueueItem{item}}}
	service, _ := newGovernanceTestService(t, repository, now)
	withoutCapability := GovernancePrincipal{UserID: newGovernanceTestUUIDv7(t), Capabilities: []string{ReviewCapability}}
	if _, err := service.ListGovernance(context.Background(), withoutCapability, "active", ""); !errors.Is(err, ErrGovernanceCapabilityRequired) {
		t.Fatalf("missing capability error = %v", err)
	}
	if _, err := service.ListGovernance(context.Background(), newGovernanceTestPrincipal(t), "unknown", ""); !errors.Is(err, ErrInvalidGovernanceRequest) {
		t.Fatalf("unknown status error = %v", err)
	}
	cursor, err := encodeGovernanceQueueCursor(GovernanceStatusSuspended, GovernanceQueueBoundary{PublishedAt: now, PublishedSnapshotID: item.PublishedSnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListGovernance(context.Background(), newGovernanceTestPrincipal(t), "active", cursor); !errors.Is(err, ErrInvalidGovernanceRequest) {
		t.Fatalf("cross-status cursor error = %v", err)
	}
	if repository.listCalls != 0 {
		t.Fatalf("invalid list input reached repository %d times", repository.listCalls)
	}

	repository.listPage = GovernanceQueuePage{Items: []GovernanceQueueItem{item, item}}
	if _, err := service.ListGovernance(context.Background(), newGovernanceTestPrincipal(t), "active", ""); !errors.Is(err, ErrPersistence) {
		t.Fatalf("duplicate list row error = %v", err)
	}
	repository.listPage = GovernanceQueuePage{Items: []GovernanceQueueItem{}, HasMore: true}
	if _, err := service.ListGovernance(context.Background(), newGovernanceTestPrincipal(t), "active", ""); !errors.Is(err, ErrPersistence) {
		t.Fatalf("empty has-more page error = %v", err)
	}
}

func TestGovernanceDetailReturnsPublishedCloneAndStrongETag(t *testing.T) {
	state := newGovernanceTestState(t, GovernanceStatusSuspended, 4)
	repository := &governanceRepositoryStub{detailState: state}
	service, _ := newGovernanceTestService(t, repository, time.Now())
	result, err := service.FindGovernanceDetail(context.Background(), newGovernanceTestPrincipal(t), state.SkillID)
	if err != nil {
		t.Fatalf("FindGovernanceDetail() error = %v", err)
	}
	if result.SkillID != state.SkillID || result.Definition.Name != state.Published.Definition.Name ||
		strings.Join(result.AllowedActions, ",") != "resume,offline" || ValidateStrongGovernanceETag(result.GovernanceETag) != nil {
		t.Fatalf("unexpected governance detail: %+v", result)
	}
	if err := VerifyGovernanceETag(result.GovernanceETag, state.SkillID, state.CurrentPublishedSnapshotID, GovernanceStatusSuspended, 4); err != nil {
		t.Fatalf("detail ETag did not verify: %v", err)
	}
	result.Definition.Tags[0] = "changed"
	if state.Published.Definition.Tags[0] == "changed" {
		t.Fatal("detail definition shared mutable slices with repository state")
	}
}

func TestGovernanceDetailRejectsInvalidInputAndCorruption(t *testing.T) {
	state := newGovernanceTestState(t, GovernanceStatusActive, 1)
	repository := &governanceRepositoryStub{detailState: state}
	service, _ := newGovernanceTestService(t, repository, time.Now())
	if _, err := service.FindGovernanceDetail(context.Background(), GovernancePrincipal{UserID: newGovernanceTestUUIDv7(t)}, state.SkillID); !errors.Is(err, ErrGovernanceCapabilityRequired) {
		t.Fatalf("missing detail capability error = %v", err)
	}
	if _, err := service.FindGovernanceDetail(context.Background(), newGovernanceTestPrincipal(t), "not-a-uuid"); !errors.Is(err, ErrInvalidGovernanceRequest) {
		t.Fatalf("invalid detail ID error = %v", err)
	}
	if repository.detailCalls != 0 {
		t.Fatalf("invalid detail input reached repository %d times", repository.detailCalls)
	}
	repository.detailState.CurrentPublishedSnapshotID = newGovernanceTestUUIDv7(t)
	if _, err := service.FindGovernanceDetail(context.Background(), newGovernanceTestPrincipal(t), state.SkillID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("corrupt pointer error = %v", err)
	}
	repository.detailErr = ErrGovernanceNotFound
	if _, err := service.FindGovernanceDetail(context.Background(), newGovernanceTestPrincipal(t), state.SkillID); !errors.Is(err, ErrGovernanceNotFound) {
		t.Fatalf("not found was not preserved: %v", err)
	}
}

func TestGovernanceETagCoversAllFrozenInputs(t *testing.T) {
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	base, err := GovernanceETag(skillID, snapshotID, GovernanceStatusActive, 1)
	if err != nil || ValidateStrongGovernanceETag(base) != nil {
		t.Fatalf("base ETag invalid: %q %v", base, err)
	}
	variants := []struct {
		skillID  string
		snapshot string
		status   GovernanceStatus
		epoch    int64
	}{
		{newGovernanceTestUUIDv7(t), snapshotID, GovernanceStatusActive, 1},
		{skillID, newGovernanceTestUUIDv7(t), GovernanceStatusActive, 1},
		{skillID, snapshotID, GovernanceStatusSuspended, 1},
		{skillID, snapshotID, GovernanceStatusActive, 2},
	}
	for _, variant := range variants {
		changed, changedErr := GovernanceETag(variant.skillID, variant.snapshot, variant.status, variant.epoch)
		if changedErr != nil || changed == base {
			t.Fatalf("ETag input was not covered: variant=%+v etag=%q err=%v", variant, changed, changedErr)
		}
	}
	for _, invalid := range []string{"", "*", `W/` + base, base + "," + base, " " + base, `"sg1-not-base64"`} {
		if !errors.Is(ValidateStrongGovernanceETag(invalid), ErrInvalidGovernanceRequest) {
			t.Fatalf("invalid Strong ETag accepted: %q", invalid)
		}
	}
	other, _ := GovernanceETag(skillID, snapshotID, GovernanceStatusActive, 2)
	if !errors.Is(VerifyGovernanceETag(other, skillID, snapshotID, GovernanceStatusActive, 1), ErrGovernanceConflict) {
		t.Fatal("stale governance ETag was not rejected as conflict")
	}
}

func TestGovernanceDecisionBuildsFrozenRepositoryCommand(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 321, time.FixedZone("CST", 8*60*60))
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	ifMatch, _ := GovernanceETag(skillID, snapshotID, GovernanceStatusActive, 1)
	repository := &governanceRepositoryStub{transitionResult: GovernanceTransitionRepositoryResult{
		SkillID: skillID, PublishedSnapshotID: snapshotID, GovernanceStatus: GovernanceStatusSuspended,
		GovernanceEpoch: 2, TransitionedAt: now.UTC(),
	}}
	service, ids := newGovernanceTestService(t, repository, now)
	command := GovernanceDecisionCommand{
		Governor: newGovernanceTestPrincipal(t), SkillID: skillID, Action: "suspend", ReasonCode: "content_safety",
		ApprovalReference: "TICKET-123", SourceAddress: "127.0.0.1", IfMatch: ifMatch,
		IdempotencyKey: "governance-intent-1", RequestID: newGovernanceTestUUIDv7(t),
	}
	result, err := service.DecideGovernance(context.Background(), command)
	if err != nil {
		t.Fatalf("DecideGovernance() error = %v", err)
	}
	stored := repository.transitionCommand
	if repository.transitionCalls != 1 || stored.GovernorUserID != command.Governor.UserID || stored.SkillID != skillID ||
		stored.Action != GovernanceActionSuspend || stored.ReasonCode != command.ReasonCode ||
		stored.ApprovalReference != command.ApprovalReference || stored.SourceAddress != command.SourceAddress ||
		stored.IfMatch != ifMatch || stored.RequestID != command.RequestID || !isUUIDv7(stored.ReceiptID) ||
		!isUUIDv7(stored.AuditID) || stored.ReceiptID == stored.AuditID || !stored.TransitionedAt.Equal(now.UTC()) || ids.calls != 2 {
		t.Fatalf("repository command was not frozen correctly: %+v id_calls=%d", stored, ids.calls)
	}
	if stored.KeyDigest != sha256DigestForTest(command.IdempotencyKey) ||
		stored.SemanticDigest != governanceSemanticDigest(skillID, GovernanceActionSuspend, command.ReasonCode, command.ApprovalReference, ifMatch) {
		t.Fatal("governance key or semantic digest mismatch")
	}
	if result.Skill.GovernanceStatus != GovernanceStatusSuspended || result.Skill.GovernanceEpoch != 2 ||
		strings.Join(result.Skill.AllowedActions, ",") != "resume,offline" || result.IdempotentReplay ||
		ValidateStrongGovernanceETag(result.Skill.GovernanceETag) != nil {
		t.Fatalf("unexpected decision projection: %+v", result)
	}
}

// sha256DigestForTest 返回字符串的 SHA-256，避免测试依赖未导出的幂等实现细节以外的校验逻辑。
func sha256DigestForTest(value string) Digest { return sha256Sum([]byte(value)) }

// sha256Sum 把字节编码为固定 Digest。
func sha256Sum(value []byte) Digest {
	var digest Digest
	calculated := sha256Bytes(value)
	copy(digest[:], calculated)
	return digest
}

// sha256Bytes 使用生产标准库路径计算测试摘要。
func sha256Bytes(value []byte) []byte {
	calculated := sha256.Sum256(value)
	return calculated[:]
}

func TestGovernanceDecisionRejectsInvalidBoundaryInputs(t *testing.T) {
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	etag, _ := GovernanceETag(skillID, snapshotID, GovernanceStatusActive, 1)
	valid := GovernanceDecisionCommand{
		Governor: newGovernanceTestPrincipal(t), SkillID: skillID, Action: "suspend", ReasonCode: "content_safety",
		ApprovalReference: "TICKET-123", SourceAddress: "2001:db8::1", IfMatch: etag,
		IdempotencyKey: "intent", RequestID: newGovernanceTestUUIDv7(t),
	}
	tests := []struct {
		name   string
		mutate func(*GovernanceDecisionCommand)
	}{
		{"invalid skill", func(command *GovernanceDecisionCommand) { command.SkillID = "bad" }},
		{"invalid request", func(command *GovernanceDecisionCommand) { command.RequestID = "bad" }},
		{"unknown action", func(command *GovernanceDecisionCommand) { command.Action = "delete" }},
		{"cross-action reason", func(command *GovernanceDecisionCommand) { command.ReasonCode = "risk_cleared" }},
		{"free reason", func(command *GovernanceDecisionCommand) { command.ReasonCode = "free text" }},
		{"lowercase approval prefix", func(command *GovernanceDecisionCommand) { command.ApprovalReference = "ticket-123" }},
		{"approval whitespace", func(command *GovernanceDecisionCommand) { command.ApprovalReference = " TICKET-123" }},
		{"mapped address", func(command *GovernanceDecisionCommand) { command.SourceAddress = "::ffff:127.0.0.1" }},
		{"address zone", func(command *GovernanceDecisionCommand) { command.SourceAddress = "fe80::1%eth0" }},
		{"weak etag", func(command *GovernanceDecisionCommand) { command.IfMatch = "W/" + etag }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &governanceRepositoryStub{}
			service, _ := newGovernanceTestService(t, repository, time.Now())
			command := valid
			testCase.mutate(&command)
			if _, err := service.DecideGovernance(context.Background(), command); !errors.Is(err, ErrInvalidGovernanceRequest) {
				t.Fatalf("invalid command error = %v", err)
			}
			if repository.transitionCalls != 0 {
				t.Fatal("invalid governance command reached repository")
			}
		})
	}

	repository := &governanceRepositoryStub{}
	service, _ := newGovernanceTestService(t, repository, time.Now())
	invalidKey := valid
	invalidKey.IdempotencyKey = "contains whitespace"
	if _, err := service.DecideGovernance(context.Background(), invalidKey); !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("invalid idempotency key error = %v", err)
	}
	missingCapability := valid
	missingCapability.Governor.Capabilities = []string{ReviewCapability}
	if _, err := service.DecideGovernance(context.Background(), missingCapability); !errors.Is(err, ErrGovernanceCapabilityRequired) {
		t.Fatalf("reviewer obtained governance access: %v", err)
	}
}

func TestGovernanceDecisionPreservesReplayAndStableErrors(t *testing.T) {
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	etag, _ := GovernanceETag(skillID, snapshotID, GovernanceStatusSuspended, 4)
	command := GovernanceDecisionCommand{
		Governor: newGovernanceTestPrincipal(t), SkillID: skillID, Action: "resume", ReasonCode: "risk_cleared",
		ApprovalReference: "TICKET-RESUME_1", SourceAddress: "127.0.0.1", IfMatch: etag,
		IdempotencyKey: "resume-intent", RequestID: newGovernanceTestUUIDv7(t),
	}
	repository := &governanceRepositoryStub{transitionResult: GovernanceTransitionRepositoryResult{
		SkillID: skillID, PublishedSnapshotID: snapshotID, GovernanceStatus: GovernanceStatusActive,
		GovernanceEpoch: 5, TransitionedAt: now.Add(-time.Hour), IdempotentReplay: true,
	}}
	service, _ := newGovernanceTestService(t, repository, now)
	result, err := service.DecideGovernance(context.Background(), command)
	if err != nil || !result.IdempotentReplay || result.Skill.TransitionedAt != now.Add(-time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("frozen replay result = %+v err=%v", result, err)
	}

	for _, stable := range []error{ErrGovernanceCapabilityRequired, ErrGovernanceNotFound, ErrGovernanceConflict, ErrIdempotencyConflict, context.Canceled, context.DeadlineExceeded} {
		repository.transitionErr = stable
		service, _ = newGovernanceTestService(t, repository, now)
		if _, got := service.DecideGovernance(context.Background(), command); !errors.Is(got, stable) {
			t.Fatalf("stable repository error %v became %v", stable, got)
		}
	}
	repository.transitionErr = errors.New("sql details")
	service, _ = newGovernanceTestService(t, repository, now)
	if _, err := service.DecideGovernance(context.Background(), command); !errors.Is(err, ErrPersistence) {
		t.Fatalf("unknown repository error leaked: %v", err)
	}
}

func TestGovernanceDecisionRejectsInvalidGeneratedIDsAndRepositoryResult(t *testing.T) {
	now := time.Now().UTC()
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	etag, _ := GovernanceETag(skillID, snapshotID, GovernanceStatusActive, 1)
	command := GovernanceDecisionCommand{
		Governor: newGovernanceTestPrincipal(t), SkillID: skillID, Action: "offline", ReasonCode: "owner_request",
		ApprovalReference: "TICKET-OFFLINE", SourceAddress: "127.0.0.1", IfMatch: etag,
		IdempotencyKey: "offline-intent", RequestID: newGovernanceTestUUIDv7(t),
	}
	repository := &governanceRepositoryStub{}
	badIDs := &governanceTestIDs{values: []string{"not-a-uuid"}}
	service, err := NewGovernanceService(repository, governanceTestClock{now: now}, badIDs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecideGovernance(context.Background(), command); !errors.Is(err, ErrPersistence) || repository.transitionCalls != 0 {
		t.Fatalf("invalid generated ID result err=%v calls=%d", err, repository.transitionCalls)
	}
	zeroClockIDs := &governanceTestIDs{values: []string{newGovernanceTestUUIDv7(t), newGovernanceTestUUIDv7(t)}}
	service, err = NewGovernanceService(repository, governanceTestClock{}, zeroClockIDs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecideGovernance(context.Background(), command); !errors.Is(err, ErrPersistence) || repository.transitionCalls != 0 {
		t.Fatalf("zero clock result err=%v calls=%d", err, repository.transitionCalls)
	}

	repository.transitionResult = GovernanceTransitionRepositoryResult{
		SkillID: skillID, PublishedSnapshotID: snapshotID, GovernanceStatus: GovernanceStatusActive,
		GovernanceEpoch: 2, TransitionedAt: now,
	}
	service, _ = newGovernanceTestService(t, repository, now)
	if _, err := service.DecideGovernance(context.Background(), command); !errors.Is(err, ErrPersistence) {
		t.Fatalf("wrong target status result error = %v", err)
	}
}

func TestGovernanceSemanticDigestCoversEverySemanticField(t *testing.T) {
	skillID := newGovernanceTestUUIDv7(t)
	snapshotID := newGovernanceTestUUIDv7(t)
	etag, _ := GovernanceETag(skillID, snapshotID, GovernanceStatusActive, 1)
	base := governanceSemanticDigest(skillID, GovernanceActionSuspend, "content_safety", "TICKET-1", etag)
	variants := []Digest{
		governanceSemanticDigest(newGovernanceTestUUIDv7(t), GovernanceActionSuspend, "content_safety", "TICKET-1", etag),
		governanceSemanticDigest(skillID, GovernanceActionOffline, "content_safety", "TICKET-1", etag),
		governanceSemanticDigest(skillID, GovernanceActionSuspend, "privacy_risk", "TICKET-1", etag),
		governanceSemanticDigest(skillID, GovernanceActionSuspend, "content_safety", "TICKET-2", etag),
		governanceSemanticDigest(skillID, GovernanceActionSuspend, "content_safety", "TICKET-1", strings.Replace(etag, "sg1", "sg2", 1)),
	}
	for index, variant := range variants {
		if variant == base {
			t.Fatalf("semantic field %d was not covered", index)
		}
	}
}

func TestGovernanceCursorRejectsMalformedRepresentations(t *testing.T) {
	snapshotID := newGovernanceTestUUIDv7(t)
	now := time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)
	valid, err := encodeGovernanceQueueCursor(GovernanceStatusActive, GovernanceQueueBoundary{PublishedAt: now, PublishedSnapshotID: snapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeGovernanceQueueCursor(GovernanceStatusActive, valid); err != nil || !decoded.PublishedAt.Equal(now) {
		t.Fatalf("valid cursor failed round trip: %+v %v", decoded, err)
	}
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(`{"schema_version":"skill_governance_queue_cursor.v1","status":"active","published_at_unix_nano":1,"published_snapshot_id":"` + snapshotID + `","extra":true}`))
	trailing := base64.RawURLEncoding.EncodeToString([]byte(`{"schema_version":"skill_governance_queue_cursor.v1","status":"active","published_at_unix_nano":1,"published_snapshot_id":"` + snapshotID + `"}{}`))
	for _, invalid := range []string{"!", strings.Repeat("a", 1025), unknownField, trailing} {
		if _, err := decodeGovernanceQueueCursor(GovernanceStatusActive, invalid); !errors.Is(err, ErrInvalidGovernanceRequest) {
			t.Fatalf("malformed cursor accepted: %q err=%v", invalid, err)
		}
	}
}
