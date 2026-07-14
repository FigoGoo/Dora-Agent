package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testCommandID = "0190f4d4-0000-7000-8000-000000000001"
	testProjectID = "0190f4d4-0000-7000-8000-000000000002"
	testUserID    = "0190f4d4-0000-7000-8000-000000000003"
)

// fixedClock 为 Session Service 测试冻结单次 UTC 时间。
type fixedClock struct{ now time.Time }

// Now 返回测试冻结时间。
func (clock fixedClock) Now() time.Time { return clock.now }

// sequenceIDGenerator 并发安全地生成可预测 UUIDv7，便于验证同键竞争结果。
type sequenceIDGenerator struct {
	mu   sync.Mutex
	next uint64
}

// New 返回版本位和 Variant 均合法的测试 UUIDv7。
func (generator *sequenceIDGenerator) New() (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.next++
	return fmt.Sprintf("0190f4d4-0001-7000-8000-%012x", generator.next), nil
}

// recordingProtector 记录被保护的 NFC 正文并返回测试用版本化自描述 AEAD Envelope。
type recordingProtector struct {
	mu        sync.Mutex
	plaintext [][]byte
}

// fixedEnvelopeProtector 返回指定保护结果，用于验证 Service 的 Envelope 非空边界。
type fixedEnvelopeProtector struct {
	// content 是测试指定的 Envelope 与 KeyVersion 组合。
	content ProtectedContent
	// err 是模拟 KMS/加密器返回且不得外泄的原始错误。
	err error
}

// failingIDGenerator 模拟首次命令已落库后随机源暂时不可用。
type failingIDGenerator struct{ err error }

// New 始终返回测试指定错误。
func (generator failingIDGenerator) New() (string, error) { return "", generator.err }

// Protect 返回测试指定结果，不执行真实加密。
func (protector fixedEnvelopeProtector) Protect(_ context.Context, _ []byte) (ProtectedContent, error) {
	return protector.content, protector.err
}

// Protect 复制正文并返回模拟包含版本/算法/Nonce/认证标签的非空 Envelope 与密钥版本。
func (protector *recordingProtector) Protect(_ context.Context, plaintext []byte) (ProtectedContent, error) {
	protector.mu.Lock()
	defer protector.mu.Unlock()
	protector.plaintext = append(protector.plaintext, append([]byte(nil), plaintext...))
	ciphertextAndTag := append([]byte("test-ciphertext:"), plaintext...)
	ciphertextAndTag = append(ciphertextAndTag, make([]byte, protectedEnvelopeV1TagSize)...)
	envelope, err := BuildEnvelopeV1(EnvelopeAlgorithmAES256GCM, make([]byte, protectedEnvelopeV1NonceSize), ciphertextAndTag)
	if err != nil {
		return ProtectedContent{}, err
	}
	return ProtectedContent{Ciphertext: envelope, KeyVersion: "test-key-v1"}, nil
}

// callCount 返回测试正文保护调用次数。
func (protector *recordingProtector) callCount() int {
	protector.mu.Lock()
	defer protector.mu.Unlock()
	return len(protector.plaintext)
}

// memoryRepository 用互斥区模拟 PostgreSQL command/project 一致性边界，用于无网络并发单元测试。
type memoryRepository struct {
	mu             sync.Mutex
	receipts       map[string]CommandReceipt
	projectCommand map[string]string
	createdPlans   []EnsurePlan
}

// newMemoryRepository 创建空的 first-write-wins 内存 Repository。
func newMemoryRepository() *memoryRepository {
	return &memoryRepository{receipts: make(map[string]CommandReceipt), projectCommand: make(map[string]string)}
}

// Ensure 在一个互斥区模拟命令级事务锁、Project 唯一约束和冻结回执重放。
func (repository *memoryRepository) Ensure(_ context.Context, plan EnsurePlan) (EnsureResult, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.receipts[plan.Receipt.CommandID]; ok {
		if existing.RequestDigest != plan.Receipt.RequestDigest {
			return EnsureResult{}, ErrCommandConflict
		}
		return resultFromTestReceipt(existing, EnsureDispositionReplayed), nil
	}
	if existingCommand, ok := repository.projectCommand[plan.Session.ProjectID]; ok && existingCommand != plan.Receipt.CommandID {
		return EnsureResult{}, ErrProjectSessionConflict
	}
	repository.receipts[plan.Receipt.CommandID] = plan.Receipt
	repository.projectCommand[plan.Session.ProjectID] = plan.Receipt.CommandID
	repository.createdPlans = append(repository.createdPlans, plan)
	return resultFromTestReceipt(plan.Receipt, EnsureDispositionCreated), nil
}

// Query 在互斥区模拟 PostgreSQL Receipt 只读核对，并确保冲突状态不泄漏既有结果。
func (repository *memoryRepository) Query(_ context.Context, command QueryCommand) (QueryCommandResult, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	receipt, exists := repository.receipts[command.CommandID]
	if !exists {
		return QueryCommandResult{Status: QueryCommandStatusNotFound}, nil
	}
	if receipt.RequestDigest != command.ExpectedRequestDigest {
		return QueryCommandResult{Status: QueryCommandStatusConflict}, nil
	}
	result := resultFromTestReceipt(receipt, EnsureDispositionReplayed)
	return QueryCommandResult{Status: QueryCommandStatusCompleted, Receipt: &result}, nil
}

// resultFromTestReceipt 将内存冻结回执映射为测试结果 DTO。
func resultFromTestReceipt(receipt CommandReceipt, disposition EnsureDisposition) EnsureResult {
	return EnsureResult{
		CommandID: receipt.CommandID, SessionID: receipt.SessionID,
		MessageID: receipt.MessageID, InputID: receipt.InputID,
		Disposition: disposition, ResultVersion: receipt.ResultVersion, AcceptedAt: receipt.CompletedAt,
	}
}

// createdPlan 返回唯一创建计划；调用方应先断言只创建一次。
func (repository *memoryRepository) createdPlan() EnsurePlan {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.createdPlans[0]
}

// createdCount 返回真正提交的新计划数量。
func (repository *memoryRepository) createdCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return len(repository.createdPlans)
}

// newTestService 创建具有冻结时钟、并发安全 ID 和记录型正文保护器的 Service。
func newTestService(t *testing.T) (*Service, *memoryRepository, *recordingProtector) {
	t.Helper()
	repository := newMemoryRepository()
	protector := &recordingProtector{}
	service, err := NewService(
		repository,
		&sequenceIDGenerator{},
		fixedClock{now: time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)},
		protector,
	)
	if err != nil {
		t.Fatalf("创建 Session Service 失败: %v", err)
	}
	return service, repository, protector
}

// newTestCommand 按生产 Canonical Schema 构造摘要一致的测试命令。
func newTestCommand(t *testing.T, commandID, prompt string) EnsureCommand {
	t.Helper()
	requestDigest, promptDigest, _, err := CalculateRequestDigest(testProjectID, testUserID, prompt, SkillSnapshotKindEmpty)
	if err != nil {
		t.Fatalf("计算测试命令摘要失败: %v", err)
	}
	return EnsureCommand{
		SchemaVersion: EnsureCommandSchemaVersionV1,
		RequestID:     "0190f4d4-0000-7000-8000-000000000004",
		CommandID:     commandID, RequestDigest: requestDigest, ProjectID: testProjectID, OwnerUserID: testUserID,
		CreationSource: CreationSourceQuickCreate,
		InitialPrompt:  prompt, PromptDigest: promptDigest, SkillSnapshotMode: SkillSnapshotKindEmpty,
		RequestedAt: time.Date(2026, 7, 14, 3, 59, 0, 0, time.UTC),
	}
}

// TestEnsureProjectSessionWithPrompt 验证非空 Prompt 原子计划包含密文 Message、pending Input、Receipt 和两个安全事件。
func TestEnsureProjectSessionWithPrompt(t *testing.T) {
	service, repository, protector := newTestService(t)
	command := newTestCommand(t, testCommandID, "Cafe\u0301")
	result, err := service.EnsureProjectSession(context.Background(), command)
	if err != nil {
		t.Fatalf("Ensure 非空 Prompt 失败: %v", err)
	}
	if result.Disposition != EnsureDispositionCreated || result.MessageID == nil || result.InputID == nil {
		t.Fatalf("非空 Prompt 结果不完整: %+v", result)
	}
	if repository.createdCount() != 1 {
		t.Fatalf("实际创建计划数=%d，want 1", repository.createdCount())
	}
	plan := repository.createdPlan()
	if plan.Message == nil || plan.Input == nil || plan.Input.Status != InputStatusPending {
		t.Fatalf("Message/Input 计划不完整: %+v", plan)
	}
	if err := ValidateEnvelopeV1(plan.Message.Content.Ciphertext); err != nil {
		t.Fatalf("Message 未保存合法的自描述 AEAD Envelope: %v", err)
	}
	if plan.SequenceCounter.LastMessageSeq != 1 || plan.SequenceCounter.LastInputEnqueueSeq != 1 || len(plan.Events) != 2 {
		t.Fatalf("初始序号或事件数量错误: counter=%+v events=%d", plan.SequenceCounter, len(plan.Events))
	}
	if got := string(protector.plaintext[0]); got != "Café" {
		t.Fatalf("正文未按 NFC 保护: %q", got)
	}
}

// TestEnsureProjectSessionReplayShortCircuitsPreparation 验证冻结 Receipt 重放不再依赖正文保护器、时钟或随机 ID。
func TestEnsureProjectSessionReplayShortCircuitsPreparation(t *testing.T) {
	service, repository, protector := newTestService(t)
	command := newTestCommand(t, testCommandID, "already committed")
	created, err := service.EnsureProjectSession(context.Background(), command)
	if err != nil {
		t.Fatalf("首次 Ensure 失败: %v", err)
	}
	if protector.callCount() != 1 {
		t.Fatalf("首次正文保护调用=%d，want 1", protector.callCount())
	}

	replayService, err := NewService(
		repository,
		failingIDGenerator{err: errors.New("entropy temporarily unavailable")},
		fixedClock{now: time.Time{}},
		fixedEnvelopeProtector{err: errors.New("kms temporarily unavailable")},
	)
	if err != nil {
		t.Fatalf("创建重放 Service 失败: %v", err)
	}
	replayed, err := replayService.EnsureProjectSession(context.Background(), command)
	if err != nil {
		t.Fatalf("冻结 Receipt 重放仍依赖准备阶段: %v", err)
	}
	if replayed.Disposition != EnsureDispositionReplayed || replayed.SessionID != created.SessionID ||
		replayed.MessageID == nil || created.MessageID == nil || *replayed.MessageID != *created.MessageID {
		t.Fatalf("重放结果未保持冻结 Receipt: created=%+v replayed=%+v", created, replayed)
	}
	if protector.callCount() != 1 || repository.createdCount() != 1 {
		t.Fatalf("重放产生额外准备或写入: protect=%d created=%d", protector.callCount(), repository.createdCount())
	}

	conflict := command
	conflict.RequestDigest = strings.Repeat("a", 64)
	if _, err := replayService.EnsureProjectSession(context.Background(), conflict); !errors.Is(err, ErrInvalidCommand) {
		// 调用方伪造摘要会先在 canonicalizeCommand 被拒绝，不能借预检探测其他命令。
		t.Fatalf("伪造异义摘要错误=%v，want ErrInvalidCommand", err)
	}

	differentPrompt := newTestCommand(t, testCommandID, "different semantics")
	if _, err := replayService.EnsureProjectSession(context.Background(), differentPrompt); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("合法异义重放错误=%v，want ErrCommandConflict", err)
	}
}

// TestEnsureProjectSessionBlankPrompt 验证各种纯 Unicode 空白均不创建 Message/Input 且不调用正文保护器。
func TestEnsureProjectSessionBlankPrompt(t *testing.T) {
	for index, prompt := range []string{"", "   ", "\t\n", "\u00a0\u3000"} {
		t.Run(fmt.Sprintf("blank_%d", index), func(t *testing.T) {
			service, repository, protector := newTestService(t)
			commandID := fmt.Sprintf("0190f4d4-0000-7000-8000-%012x", index+10)
			result, err := service.EnsureProjectSession(context.Background(), newTestCommand(t, commandID, prompt))
			if err != nil {
				t.Fatalf("Ensure 空 Prompt 失败: %v", err)
			}
			plan := repository.createdPlan()
			if result.MessageID != nil || result.InputID != nil || plan.Message != nil || plan.Input != nil {
				t.Fatalf("空 Prompt 产生输入副作用: result=%+v plan=%+v", result, plan)
			}
			if len(plan.Events) != 1 || protector.callCount() != 0 {
				t.Fatalf("空 Prompt 事件或保护调用错误: events=%d protect=%d", len(plan.Events), protector.callCount())
			}
		})
	}
}

// TestEnsureProjectSessionRejectsDigestMismatch 验证 Agent 独立重算摘要并在事务前拒绝调用方伪造值。
func TestEnsureProjectSessionRejectsDigestMismatch(t *testing.T) {
	service, repository, _ := newTestService(t)
	command := newTestCommand(t, testCommandID, "hello")
	command.RequestDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := service.EnsureProjectSession(context.Background(), command)
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("摘要不一致错误=%v，want ErrInvalidCommand", err)
	}
	if repository.createdCount() != 0 {
		t.Fatalf("摘要不一致仍创建了领域事实")
	}
}

// TestEnsureProjectSessionRejectsIncompleteEnvelope 验证非空 Prompt 不接受空 Envelope 或缺失 KeyVersion 的持久化结果。
func TestEnsureProjectSessionRejectsIncompleteEnvelope(t *testing.T) {
	testCases := []struct {
		name    string
		content ProtectedContent
	}{
		{name: "空 Envelope", content: ProtectedContent{KeyVersion: "key-v1"}},
		{name: "裸明文伪装密文", content: ProtectedContent{Ciphertext: []byte("plaintext"), KeyVersion: "key-v1"}},
		{name: "缺失 KeyVersion", content: ProtectedContent{Ciphertext: mustTestEnvelope(t)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newMemoryRepository()
			service, err := NewService(
				repository,
				&sequenceIDGenerator{},
				fixedClock{now: time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)},
				fixedEnvelopeProtector{content: testCase.content},
			)
			if err != nil {
				t.Fatalf("创建 Session Service 失败: %v", err)
			}
			if _, err := service.EnsureProjectSession(context.Background(), newTestCommand(t, testCommandID, "敏感正文")); !errors.Is(err, ErrContentProtection) {
				t.Fatalf("不完整 Envelope 错误=%v，want ErrContentProtection", err)
			}
			if repository.createdCount() != 0 {
				t.Fatalf("不完整 Envelope 仍进入数据库事务")
			}
		})
	}
}

// TestEnsureProjectSessionHidesProtectorError 验证 KMS/算法/地址详情被截断，同时保留 Context 控制错误。
func TestEnsureProjectSessionHidesProtectorError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want error
	}{
		{name: "KMS Secret 错误", err: errors.New("kms https://secret.internal key=top-secret"), want: ErrContentProtection},
		{name: "请求取消", err: fmt.Errorf("kms stopped: %w", context.Canceled), want: context.Canceled},
		{name: "Deadline", err: fmt.Errorf("kms stopped: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newMemoryRepository()
			service, err := NewService(
				repository,
				&sequenceIDGenerator{},
				fixedClock{now: time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)},
				fixedEnvelopeProtector{err: testCase.err},
			)
			if err != nil {
				t.Fatalf("创建 Session Service 失败: %v", err)
			}
			_, ensureErr := service.EnsureProjectSession(context.Background(), newTestCommand(t, testCommandID, "敏感正文"))
			if !errors.Is(ensureErr, testCase.want) {
				t.Fatalf("保护器错误映射=%v，want %v", ensureErr, testCase.want)
			}
			if testCase.want == ErrContentProtection && ensureErr.Error() != ErrContentProtection.Error() {
				t.Fatalf("保护器错误泄漏内部详情: %v", ensureErr)
			}
		})
	}
}

// mustTestEnvelope 构造结构合法的测试 Envelope；失败说明固定测试向量自身无效。
func mustTestEnvelope(t *testing.T) []byte {
	t.Helper()
	ciphertextAndTag := make([]byte, protectedEnvelopeV1TagSize+1)
	envelope, err := BuildEnvelopeV1(EnvelopeAlgorithmAES256GCM, make([]byte, protectedEnvelopeV1NonceSize), ciphertextAndTag)
	if err != nil {
		t.Fatalf("构建测试 Envelope 失败: %v", err)
	}
	return envelope
}

// TestEnsureProjectSessionConcurrentReplay 验证同一 Command 并发 100 次只冻结一个 Session/Input 结果。
func TestEnsureProjectSessionConcurrentReplay(t *testing.T) {
	service, repository, _ := newTestService(t)
	command := newTestCommand(t, testCommandID, "并发创建")
	const concurrent = 100
	results := make(chan EnsureResult, concurrent)
	errorsChannel := make(chan error, concurrent)
	var waitGroup sync.WaitGroup
	for range concurrent {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, err := service.EnsureProjectSession(context.Background(), command)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	close(results)
	for err := range errorsChannel {
		t.Fatalf("并发 Ensure 失败: %v", err)
	}
	if repository.createdCount() != 1 {
		t.Fatalf("并发真正创建数=%d，want 1", repository.createdCount())
	}
	var frozenSessionID string
	for result := range results {
		if frozenSessionID == "" {
			frozenSessionID = result.SessionID
		}
		if result.SessionID != frozenSessionID {
			t.Fatalf("并发重放返回不同 Session: got=%s want=%s", result.SessionID, frozenSessionID)
		}
	}
}

// TestEnsureProjectSessionSameCommandDifferentSemantics 验证同一 CommandID 的新语义不能覆盖旧 Receipt。
func TestEnsureProjectSessionSameCommandDifferentSemantics(t *testing.T) {
	service, _, _ := newTestService(t)
	if _, err := service.EnsureProjectSession(context.Background(), newTestCommand(t, testCommandID, "first")); err != nil {
		t.Fatalf("首次 Ensure 失败: %v", err)
	}
	_, err := service.EnsureProjectSession(context.Background(), newTestCommand(t, testCommandID, "second"))
	if !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("同键异义错误=%v，want ErrCommandConflict", err)
	}
}

// TestQueryProjectSessionCommandThreeStates 验证 Service 对原命令提供 not_found/completed/conflict 权威三态。
func TestQueryProjectSessionCommandThreeStates(t *testing.T) {
	service, _, _ := newTestService(t)
	command := newTestCommand(t, testCommandID, "first")
	notFound, err := service.QueryProjectSessionCommand(context.Background(), QueryCommand{
		SchemaVersion: QueryCommandSchemaVersionV1,
		RequestID:     "0190f4d4-0000-7000-8000-000000000004",
		CommandID:     command.CommandID, ExpectedRequestDigest: command.RequestDigest,
	})
	if err != nil || notFound.Status != QueryCommandStatusNotFound || notFound.Receipt != nil {
		t.Fatalf("Query not_found=%+v err=%v", notFound, err)
	}
	created, err := service.EnsureProjectSession(context.Background(), command)
	if err != nil {
		t.Fatalf("首次 Ensure 失败: %v", err)
	}
	completed, err := service.QueryProjectSessionCommand(context.Background(), QueryCommand{
		SchemaVersion: QueryCommandSchemaVersionV1,
		RequestID:     "0190f4d4-0000-7000-8000-000000000004",
		CommandID:     command.CommandID, ExpectedRequestDigest: command.RequestDigest,
	})
	if err != nil || completed.Status != QueryCommandStatusCompleted || completed.Receipt == nil ||
		completed.Receipt.SessionID != created.SessionID {
		t.Fatalf("Query completed=%+v err=%v", completed, err)
	}
	conflict, err := service.QueryProjectSessionCommand(context.Background(), QueryCommand{
		SchemaVersion: QueryCommandSchemaVersionV1,
		RequestID:     "0190f4d4-0000-7000-8000-000000000004",
		CommandID:     command.CommandID, ExpectedRequestDigest: strings.Repeat("a", 64),
	})
	if err != nil || conflict.Status != QueryCommandStatusConflict || conflict.Receipt != nil {
		t.Fatalf("Query conflict=%+v err=%v", conflict, err)
	}
}

// TestCalculateRequestDigestNormalizesNFC 验证等价 Unicode 组合形式得到相同 Prompt 与 Request 摘要。
func TestCalculateRequestDigestNormalizesNFC(t *testing.T) {
	requestA, promptA, presentA, err := CalculateRequestDigest(testProjectID, testUserID, "Cafe\u0301", SkillSnapshotKindEmpty)
	if err != nil {
		t.Fatalf("计算分解形式摘要失败: %v", err)
	}
	requestB, promptB, presentB, err := CalculateRequestDigest(testProjectID, testUserID, "Café", SkillSnapshotKindEmpty)
	if err != nil {
		t.Fatalf("计算组合形式摘要失败: %v", err)
	}
	if !presentA || !presentB || requestA != requestB || promptA != promptB {
		t.Fatalf("NFC 摘要不一致: request=(%s,%s) prompt=(%s,%s)", requestA, requestB, promptA, promptB)
	}
}

// TestCalculateRequestDigestCrossModuleVectors 固定 Business 与 Agent 必须共同通过的紧凑 JSON 摘要向量。
// 修改字段顺序、固定版本、UUID 规范化、Prompt NFC 或空白折叠规则都会使本测试失败并要求显式升级契约。
func TestCalculateRequestDigestCrossModuleVectors(t *testing.T) {
	if got := sha256Hex([]byte("[]")); got != EmptySkillSnapshotDigest {
		t.Fatalf("显式空 Skill Snapshot 摘要漂移: got=%s want=%s", got, EmptySkillSnapshotDigest)
	}
	testCases := []struct {
		name              string
		projectID         string
		ownerUserID       string
		prompt            string
		wantPromptDigest  string
		wantRequestDigest string
		wantPresent       bool
	}{
		{
			name:              "Business Agent 共享 NFC 固定向量",
			projectID:         "019F0000-0000-7000-8000-0000000000AB",
			ownerUserID:       "019F0000-0000-7000-8000-0000000000CD",
			prompt:            " e\u0301 ",
			wantPromptDigest:  "273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df",
			wantRequestDigest: "35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4",
			wantPresent:       true,
		},
		{
			name:              "Unicode 空白 Prompt",
			projectID:         testProjectID,
			ownerUserID:       testUserID,
			prompt:            "\u00a0\u3000\n",
			wantPromptDigest:  "",
			wantRequestDigest: "9a98dc10f4240bdab7a7a1769f8e70fe0a5081c68cfcf0377760eb444ee66337",
			wantPresent:       false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requestDigest, promptDigest, present, err := CalculateRequestDigest(
				testCase.projectID, testCase.ownerUserID, testCase.prompt, SkillSnapshotKindEmpty,
			)
			if err != nil {
				t.Fatalf("计算跨 Module 固定向量失败: %v", err)
			}
			if requestDigest != testCase.wantRequestDigest || promptDigest != testCase.wantPromptDigest || present != testCase.wantPresent {
				t.Fatalf(
					"固定向量漂移: request=%s prompt=%s present=%v",
					requestDigest, promptDigest, present,
				)
			}
		})
	}
}
