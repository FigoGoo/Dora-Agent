package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

const (
	serviceTestRequestID      = "019f68e8-5001-7000-8000-000000000001"
	serviceTestIdempotencyKey = "019f68e8-5002-7000-8000-000000000002"
	serviceTestUserID         = "019f68e8-5003-7000-8000-000000000003"
	serviceTestProjectID      = "019f68e8-5004-7000-8000-000000000004"
	serviceTestSessionID      = "019f68e8-5005-7000-8000-000000000005"
	serviceTestStableInputID  = "019f68e8-5006-7000-8000-000000000006"
)

// TestServiceIdempotentReplaySkipsKMSIDsAndClock 验证已接受同义重放只依赖 PostgreSQL first-write-wins 真源；
// 即使 KMS、随机源与时钟在该时刻均不可用，也必须返回冻结 InputID。
func TestServiceIdempotentReplaySkipsKMSIDsAndClock(t *testing.T) {
	order := make([]string, 0, 2)
	repository := &serviceTestEnqueueRepository{
		lookupResult: &EnqueueResult{SessionID: serviceTestSessionID, InputID: serviceTestStableInputID, Status: "pending"},
		order:        &order,
	}
	protector := &serviceTestProtector{err: errors.New("KMS unavailable"), order: &order}
	ids := &serviceTestIDGenerator{err: errors.New("random source unavailable"), order: &order}
	clock := &serviceTestClock{order: &order}
	wakeCalls := 0
	service := newServiceForTest(t, repository, protector, ids, clock, func() {
		order = append(order, "wake")
		wakeCalls++
	})

	result, err := service.Enqueue(context.Background(), serviceTestCommand())
	if err != nil {
		t.Fatalf("已接受同义重放被临时依赖故障阻断: %v", err)
	}
	if result.RequestID != serviceTestRequestID || result.SessionID != serviceTestSessionID ||
		result.InputID != serviceTestStableInputID || result.Status != "pending" {
		t.Fatalf("同义重放未返回冻结回执: %+v", result)
	}
	if protector.calls != 0 || ids.calls != 0 || clock.calls != 0 || repository.enqueueCalls != 0 {
		t.Fatalf("同义重放产生了不必要副作用: protect=%d ids=%d clock=%d enqueue=%d",
			protector.calls, ids.calls, clock.calls, repository.enqueueCalls)
	}
	if repository.lookupCalls != 1 || wakeCalls != 1 || strings.Join(order, ",") != "lookup,wake" {
		t.Fatalf("同义重放调用顺序错误: lookup=%d wake=%d order=%v", repository.lookupCalls, wakeCalls, order)
	}
}

// TestServiceIdempotencyConflictHasNoSideEffects 验证异义幂等键在预检阶段稳定冲突，
// 不得调用 KMS、随机源、Clock、最终 Enqueue 或 wake。
func TestServiceIdempotencyConflictHasNoSideEffects(t *testing.T) {
	order := make([]string, 0, 1)
	repository := &serviceTestEnqueueRepository{lookupErr: ErrIdempotencyConflict, order: &order}
	protector := &serviceTestProtector{err: errors.New("must not be called"), order: &order}
	ids := &serviceTestIDGenerator{err: errors.New("must not be called"), order: &order}
	clock := &serviceTestClock{order: &order}
	wakeCalls := 0
	service := newServiceForTest(t, repository, protector, ids, clock, func() { wakeCalls++ })

	result, err := service.Enqueue(context.Background(), serviceTestCommand())
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("异义幂等键未返回稳定 conflict: result=%+v err=%v", result, err)
	}
	if result != (EnqueueResult{}) || protector.calls != 0 || ids.calls != 0 || clock.calls != 0 ||
		repository.enqueueCalls != 0 || wakeCalls != 0 {
		t.Fatalf("conflict 产生副作用: result=%+v protect=%d ids=%d clock=%d enqueue=%d wake=%d",
			result, protector.calls, ids.calls, clock.calls, repository.enqueueCalls, wakeCalls)
	}
	if repository.lookupCalls != 1 || strings.Join(order, ",") != "lookup" {
		t.Fatalf("conflict 预检顺序错误: lookup=%d order=%v", repository.lookupCalls, order)
	}
}

// TestServiceMissCreatesPlanOnlyAfterPreflight 验证真正 miss 才调用 KMS、生成全部稳定 ID、读取一次 Clock，
// 并把预分配 InputID 原样交给最终 first-write-wins 事务。
func TestServiceMissCreatesPlanOnlyAfterPreflight(t *testing.T) {
	order := make([]string, 0, 13)
	generatedIDs := []string{
		"019f68e8-5011-7000-8000-000000000011", "019f68e8-5012-7000-8000-000000000012",
		"019f68e8-5013-7000-8000-000000000013", "019f68e8-5014-7000-8000-000000000014",
		"019f68e8-5015-7000-8000-000000000015", "019f68e8-5016-7000-8000-000000000016",
		"019f68e8-5017-7000-8000-000000000017", "019f68e8-5018-7000-8000-000000000018",
	}
	now := time.Date(2026, 7, 16, 15, 30, 0, 0, time.UTC)
	protected := serviceTestProtectedContent(t)
	repository := &serviceTestEnqueueRepository{
		// 模拟 preflight miss 后另一请求先赢得最终事务；Repository 必须返回胜者冻结的稳定 InputID。
		enqueueResult: EnqueueResult{SessionID: serviceTestSessionID, InputID: serviceTestStableInputID, Status: "pending"},
		order:         &order,
	}
	protector := &serviceTestProtector{protected: protected, order: &order}
	ids := &serviceTestIDGenerator{ids: generatedIDs, order: &order}
	clock := &serviceTestClock{now: now, order: &order}
	wakeCalls := 0
	service := newServiceForTest(t, repository, protector, ids, clock, func() {
		order = append(order, "wake")
		wakeCalls++
	})

	command := serviceTestCommand()
	result, err := service.Enqueue(context.Background(), command)
	if err != nil {
		t.Fatalf("Preview miss 入队失败: %v", err)
	}
	if result.RequestID != command.RequestID || result.InputID != serviceTestStableInputID || result.Status != "pending" {
		t.Fatalf("miss 入队回执错误: %+v", result)
	}
	wantOrder := "lookup,protect,id,id,id,id,id,id,id,id,clock,enqueue,wake"
	if strings.Join(order, ",") != wantOrder {
		t.Fatalf("miss 副作用顺序=%v want=%s", order, wantOrder)
	}
	if repository.lookupCalls != 1 || repository.enqueueCalls != 1 || protector.calls != 1 ||
		ids.calls != 8 || clock.calls != 1 || wakeCalls != 1 {
		t.Fatalf("miss 调用次数错误: lookup=%d enqueue=%d protect=%d ids=%d clock=%d wake=%d",
			repository.lookupCalls, repository.enqueueCalls, protector.calls, ids.calls, clock.calls, wakeCalls)
	}
	digest, digestErr := plancreationspec.IntentDigest(command.Intent)
	if digestErr != nil {
		t.Fatalf("计算测试 Intent digest 失败: %v", digestErr)
	}
	if repository.lookupDigest != digest || repository.plan.RequestDigest != digest {
		t.Fatalf("preflight/final digest 漂移: lookup=%q plan=%q want=%q",
			repository.lookupDigest, repository.plan.RequestDigest, digest)
	}
	if repository.plan.InputID != generatedIDs[1] || repository.plan.MessageID != generatedIDs[0] ||
		repository.plan.EventID != generatedIDs[6] || repository.plan.TerminalEventID != generatedIDs[7] ||
		!repository.plan.CreatedAt.Equal(now) || repository.plan.Content.KeyVersion != protected.KeyVersion {
		t.Fatalf("miss EnqueuePlan 未保留预分配身份/时间/密文: %+v", repository.plan)
	}
	if result.InputID == repository.plan.InputID {
		t.Fatalf("最终 Enqueue 竞态重放未采用 Repository 冻结 InputID: result=%q plan=%q",
			result.InputID, repository.plan.InputID)
	}
}

type serviceTestEnqueueRepository struct {
	lookupResult  *EnqueueResult
	lookupErr     error
	enqueueResult EnqueueResult
	enqueueErr    error
	lookupCalls   int
	enqueueCalls  int
	lookupDigest  string
	plan          EnqueuePlan
	order         *[]string
}

func (repository *serviceTestEnqueueRepository) LookupEnqueue(
	_ context.Context,
	_ string,
	requestDigest string,
	_ string,
	_ string,
	_ string,
) (*EnqueueResult, error) {
	repository.lookupCalls++
	repository.lookupDigest = requestDigest
	repository.record("lookup")
	if repository.lookupResult == nil {
		return nil, repository.lookupErr
	}
	result := *repository.lookupResult
	return &result, repository.lookupErr
}

func (repository *serviceTestEnqueueRepository) Enqueue(_ context.Context, plan EnqueuePlan) (EnqueueResult, error) {
	repository.enqueueCalls++
	repository.plan = plan
	repository.record("enqueue")
	return repository.enqueueResult, repository.enqueueErr
}

func (repository *serviceTestEnqueueRepository) record(value string) {
	if repository.order != nil {
		*repository.order = append(*repository.order, value)
	}
}

type serviceTestProtector struct {
	protected session.ProtectedContent
	err       error
	calls     int
	order     *[]string
}

func (protector *serviceTestProtector) Protect(context.Context, []byte) (session.ProtectedContent, error) {
	protector.calls++
	if protector.order != nil {
		*protector.order = append(*protector.order, "protect")
	}
	return protector.protected, protector.err
}

type serviceTestIDGenerator struct {
	ids   []string
	err   error
	calls int
	order *[]string
}

func (generator *serviceTestIDGenerator) New() (string, error) {
	generator.calls++
	if generator.order != nil {
		*generator.order = append(*generator.order, "id")
	}
	if generator.err != nil || generator.calls > len(generator.ids) {
		return "", generator.err
	}
	return generator.ids[generator.calls-1], nil
}

type serviceTestClock struct {
	now   time.Time
	calls int
	order *[]string
}

func (clock *serviceTestClock) Now() time.Time {
	clock.calls++
	if clock.order != nil {
		*clock.order = append(*clock.order, "clock")
	}
	return clock.now
}

func newServiceForTest(
	t *testing.T,
	repository EnqueueRepository,
	protector ContentProtector,
	ids IDGenerator,
	clock Clock,
	wake func(),
) *Service {
	t.Helper()
	service, err := NewService(repository, protector, ids, clock, wake)
	if err != nil {
		t.Fatalf("创建 Preview Service 失败: %v", err)
	}
	return service
}

func serviceTestCommand() EnqueueCommand {
	return EnqueueCommand{
		RequestID: serviceTestRequestID, IdempotencyKey: serviceTestIdempotencyKey,
		UserID: serviceTestUserID, ProjectID: serviceTestProjectID, SessionID: serviceTestSessionID,
		Intent: plancreationspec.Intent{
			SchemaVersion: plancreationspec.IntentSchemaVersion, Goal: "制作品牌短片",
			DeliverableType: "video", Locale: "zh-CN", Constraints: []string{},
		},
	}
}

func serviceTestProtectedContent(t *testing.T) session.ProtectedContent {
	t.Helper()
	ciphertextAndTag := append([]byte("service-test-ciphertext"), make([]byte, 16)...)
	envelope, err := session.BuildEnvelopeV1(
		session.EnvelopeAlgorithmAES256GCM,
		make([]byte, 12),
		ciphertextAndTag,
	)
	if err != nil {
		t.Fatalf("构造 Service 测试 Envelope 失败: %v", err)
	}
	return session.ProtectedContent{Ciphertext: envelope, KeyVersion: "service-test-key-v1"}
}

var _ EnqueueRepository = (*serviceTestEnqueueRepository)(nil)
var _ ContentProtector = (*serviceTestProtector)(nil)
var _ IDGenerator = (*serviceTestIDGenerator)(nil)
var _ Clock = (*serviceTestClock)(nil)
