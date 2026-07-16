package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

const (
	testV2ProjectID      = "019f0000-0000-7000-8000-0000000000ab"
	testV2OwnerID        = "019f0000-0000-7000-8000-0000000000cd"
	testV2CommandID      = "019f0000-0000-7000-8000-0000000000ef"
	testV2RuntimeDigest  = "d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168"
	testV2SnapshotDigest = "69ef1ba7ca41c90986204308043cb4587097ce3d4edbcea921b00eafc7cdfcdc"
)

// recordingSkillSnapshotProtector 模拟一次批量加密/解密边界并记录调用，不在普通失败文本中输出明文。
type recordingSkillSnapshotProtector struct {
	mu           sync.Mutex
	protectCalls int
	items        int
	stored       map[string]SkillSnapshotPlaintext
}

// ProtectBatch 返回结构合法且可由同一 Fake 严格恢复的 Envelope，任一输入失败时不保存局部结果。
func (protector *recordingSkillSnapshotProtector) ProtectBatch(
	_ context.Context,
	plaintexts []SkillSnapshotPlaintext,
) ([]SkillSnapshotCiphertext, error) {
	protector.mu.Lock()
	defer protector.mu.Unlock()
	protector.protectCalls++
	protector.items += len(plaintexts)
	if protector.stored == nil {
		protector.stored = make(map[string]SkillSnapshotPlaintext)
	}
	results := make([]SkillSnapshotCiphertext, len(plaintexts))
	for index, plaintext := range plaintexts {
		nonce := make([]byte, 12)
		nonce[len(nonce)-1] = byte(index + protector.protectCalls)
		ciphertextAndTag := append([]byte("fake:"), plaintext.CanonicalBytes...)
		ciphertextAndTag = append(ciphertextAndTag, make([]byte, 16)...)
		envelope, err := BuildEnvelopeV1(EnvelopeAlgorithmAES256GCM, nonce, ciphertextAndTag)
		if err != nil {
			return nil, err
		}
		results[index] = SkillSnapshotCiphertext{
			Identity:  plaintext.Identity,
			Protected: ProtectedContent{Ciphertext: envelope, KeyVersion: "skill-snapshot-test-v1"},
		}
		protector.stored[string(envelope)] = SkillSnapshotPlaintext{
			Identity: plaintext.Identity, CanonicalBytes: append([]byte(nil), plaintext.CanonicalBytes...),
		}
	}
	return results, nil
}

// OpenBatch 只恢复该 Fake 曾按完全相同 Envelope 与 AAD 身份保护的内容；损坏或身份串线整体失败。
func (protector *recordingSkillSnapshotProtector) OpenBatch(
	_ context.Context,
	ciphertexts []SkillSnapshotCiphertext,
) ([][]byte, error) {
	protector.mu.Lock()
	defer protector.mu.Unlock()
	results := make([][]byte, len(ciphertexts))
	for index, ciphertext := range ciphertexts {
		stored, exists := protector.stored[string(ciphertext.Protected.Ciphertext)]
		if !exists || stored.Identity != ciphertext.Identity || ciphertext.Protected.KeyVersion != "skill-snapshot-test-v1" {
			return nil, ErrContentUnavailable
		}
		results[index] = append([]byte(nil), stored.CanonicalBytes...)
	}
	return results, nil
}

// stats 返回批量保护调用次数和总 Item 数。
func (protector *recordingSkillSnapshotProtector) stats() (int, int) {
	protector.mu.Lock()
	defer protector.mu.Unlock()
	return protector.protectCalls, protector.items
}

// newV2TestService 创建同时支持 V1/V2 的内存测试 Service。
func newV2TestService(t *testing.T) (*Service, *memoryRepository, *recordingSkillSnapshotProtector) {
	t.Helper()
	repository := newMemoryRepository()
	snapshotProtector := &recordingSkillSnapshotProtector{}
	service, err := NewServiceWithSkillSnapshot(
		repository, &sequenceIDGenerator{},
		fixedClock{now: time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC)},
		&recordingProtector{}, snapshotProtector, skill.DefaultLimitsProfileV1(),
	)
	if err != nil {
		t.Fatalf("创建 V2 Session Service 失败: %v", err)
	}
	return service, repository, snapshotProtector
}

// newV2Command 使用冻结 golden fixture 构造 Runtime/set/request 摘要全部自洽的 Ensure V2 命令。
func newV2Command(t *testing.T, commandID string, snapshot skill.SessionSkillSnapshotV1) EnsureCommandV2 {
	t.Helper()
	input := skill.EnsureProjectSessionInputV2{
		SchemaVersion: skill.EnsureProjectSessionSchemaVersionV2,
		ProjectID:     testV2ProjectID, OwnerUserID: testV2OwnerID,
		CreationSource: skill.CreationSourceQuickCreate, InitialPrompt: " e\u0301 ", SkillSnapshot: snapshot,
	}
	canonical, err := skill.CanonicalEnsureProjectSessionV2(input, skill.DefaultLimitsProfileV1())
	if err != nil {
		t.Fatalf("构造 V2 命令 canonical 失败: %v", err)
	}
	return EnsureCommandV2{
		SchemaVersion: input.SchemaVersion, RequestID: "019f0000-0000-7000-8000-0000000000aa",
		CommandID: commandID, RequestDigest: canonical.RequestDigest.Hex(),
		ProjectID: input.ProjectID, OwnerUserID: input.OwnerUserID, CreationSource: input.CreationSource,
		InitialPrompt: input.InitialPrompt, PromptDigest: canonical.PromptDigest, SkillSnapshot: snapshot,
		RequestedAt: time.Date(2026, 7, 14, 6, 59, 0, 0, time.UTC),
	}
}

// nonEmptySkillSnapshotFixture 返回与设计文档 golden vector 一致的单 Skill Snapshot。
func nonEmptySkillSnapshotFixture() skill.SessionSkillSnapshotV1 {
	notApplicable := skill.CapabilityGuidanceV1{
		Applicability: skill.SkillGuidanceNotApplicableV1, NotApplicableReason: "not used",
	}
	runtime := skill.SkillRuntimeContentV1{
		SchemaVersion: skill.RuntimeContentSchemaVersionV1, Name: "Prompt helper",
		InputDescription: "text", OutputDescription: "prompt", InvocationRules: "Use for prompt writing.",
		PlanCreationSpec: notApplicable, AnalyzeMaterials: notApplicable, PlanStoryboard: notApplicable,
		GenerateMedia: notApplicable,
		WritePrompts: skill.CapabilityGuidanceV1{
			Applicability: skill.SkillGuidanceEnabledV1, Guidance: "Write concise prompts.",
		},
		AssembleOutput: notApplicable, Examples: make([]skill.SkillExampleV1, 0),
		StarterPrompts: []string{"Improve this prompt."},
	}
	return skill.SessionSkillSnapshotV1{
		SchemaVersion: skill.SnapshotSchemaVersionV1,
		SnapshotKind:  skill.SessionSkillSnapshotKindPublishedRefsV1, SkillCount: 1,
		SnapshotSetDigest: testV2SnapshotDigest,
		Skills: []skill.PublishedSkillSnapshotRefV1{{
			LoadOrder: 1, Priority: 100, Namespace: skill.SkillNamespaceUserV1,
			SkillID:             "019f0000-0000-7000-8000-000000000101",
			PublisherUserID:     "019f0000-0000-7000-8000-000000000102",
			PublishedSnapshotID: "019f0000-0000-7000-8000-000000000103",
			PublicationRevision: 2, DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
			ContentDigest:               "dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513",
			RuntimeContentSchemaVersion: skill.RuntimeContentSchemaVersionV1,
			RuntimeContentDigest:        testV2RuntimeDigest, RuntimeContent: runtime,
			AllowedGraphToolKeys: []string{"write_prompts"}, PublicToolRefs: make([]skill.PublicToolSnapshotRefV1, 0),
			PermissionSnapshotDigest: "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25",
			RuntimePolicyRef:         skill.RuntimePolicyRefV1, GovernanceEpoch: 3, PublishedAtUnixMS: 1784011500123,
		}},
	}
}

// emptySkillSnapshotFixture 返回 V2 显式空 Snapshot；它仍必须写 V2 Receipt，不能转换为 V1。
func emptySkillSnapshotFixture() skill.SessionSkillSnapshotV1 {
	return skill.SessionSkillSnapshotV1{
		SchemaVersion: skill.SnapshotSchemaVersionV1, SnapshotKind: skill.SessionSkillSnapshotKindEmptyV1,
		SkillCount: 0, SnapshotSetDigest: skill.EmptySnapshotSetDigestHex,
		Skills: make([]skill.PublishedSkillSnapshotRefV1, 0),
	}
}

// TestEnsureProjectSessionV2NonEmptyAndLoad 验证非空 Snapshot 在事务前一次批量保护、原子计划保存，并可从密文重验加载。
func TestEnsureProjectSessionV2NonEmptyAndLoad(t *testing.T) {
	service, repository, snapshotProtector := newV2TestService(t)
	result, err := service.EnsureProjectSessionV2(
		context.Background(), newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture()),
	)
	if err != nil {
		t.Fatalf("Ensure V2 非空失败: %v", err)
	}
	if result.Disposition != EnsureDispositionCreated || result.ResultVersion != ResultVersionV2 ||
		result.SkillSnapshotDigest != testV2SnapshotDigest || result.SkillCount != 1 {
		t.Fatalf("V2 Receipt 不完整: %+v", result)
	}
	plan := repository.createdPlan()
	if plan.SkillSnapshot.Kind != SkillSnapshotKindPublishedRefs || len(plan.SkillSnapshotItems) != 1 ||
		plan.Receipt.CommandType != CommandTypeEnsureProjectSessionV2 {
		t.Fatalf("V2 原子计划不完整: %+v", plan)
	}
	if bytes.Contains([]byte(plan.SkillSnapshot.PublishedSnapshotRefsJSON), []byte("Write concise prompts")) {
		t.Fatal("Header JSONB 泄漏 Runtime Content 明文")
	}
	protectCalls, itemCount := snapshotProtector.stats()
	if protectCalls != 1 || itemCount != 1 {
		t.Fatalf("Snapshot 加密调用=(%d,%d)，want 一次批量/一个 Item", protectCalls, itemCount)
	}
	loaded, err := service.LoadSessionSkillSnapshotV1(context.Background(), result.SessionID)
	if err != nil || loaded.Snapshot.SnapshotSetDigest != testV2SnapshotDigest ||
		len(loaded.Snapshot.Skills) != 1 || loaded.Snapshot.Skills[0].RuntimeContent.Name != "Prompt helper" {
		t.Fatalf("加载冻结 Snapshot=%+v err=%v", loaded, err)
	}
}

// TestEnsureProjectSessionV2EmptyKeepsV2Receipt 验证 V2 empty 写 Header/Receipt 但不加密、不写 Item，且不能退化成 V1 command token。
func TestEnsureProjectSessionV2EmptyKeepsV2Receipt(t *testing.T) {
	service, repository, snapshotProtector := newV2TestService(t)
	result, err := service.EnsureProjectSessionV2(
		context.Background(), newV2Command(t, testV2CommandID, emptySkillSnapshotFixture()),
	)
	if err != nil {
		t.Fatalf("Ensure V2 empty 失败: %v", err)
	}
	plan := repository.createdPlan()
	if result.ResultVersion != ResultVersionV2 || result.SkillCount != 0 ||
		plan.Receipt.CommandType != CommandTypeEnsureProjectSessionV2 || len(plan.SkillSnapshotItems) != 0 {
		t.Fatalf("V2 empty 被错误降级: result=%+v plan=%+v", result, plan)
	}
	protectCalls, itemCount := snapshotProtector.stats()
	if protectCalls != 0 || itemCount != 0 {
		t.Fatalf("empty Snapshot 仍调用保护器=(%d,%d)", protectCalls, itemCount)
	}
	loaded, err := service.LoadSessionSkillSnapshotV1(context.Background(), result.SessionID)
	if err != nil || loaded.Snapshot.SnapshotKind != skill.SessionSkillSnapshotKindEmptyV1 {
		t.Fatalf("加载 V2 empty=%+v err=%v", loaded, err)
	}
}

// TestEnsureProjectSessionV2ReplayDoesNotRequireProtector 验证已提交 Receipt 重放先于 protector 检查，Unknown Outcome 不因 key 临时不可用而失去收敛能力。
func TestEnsureProjectSessionV2ReplayDoesNotRequireProtector(t *testing.T) {
	service, repository, _ := newV2TestService(t)
	command := newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture())
	created, err := service.EnsureProjectSessionV2(context.Background(), command)
	if err != nil {
		t.Fatalf("首次 Ensure V2 失败: %v", err)
	}
	replayService, err := NewService(
		repository, failingIDGenerator{err: errors.New("entropy unavailable")}, fixedClock{},
		fixedEnvelopeProtector{err: errors.New("kms unavailable")},
	)
	if err != nil {
		t.Fatalf("创建无 V2 protector Service 失败: %v", err)
	}
	replayed, err := replayService.EnsureProjectSessionV2(context.Background(), command)
	if err != nil || replayed.Disposition != EnsureDispositionReplayed || replayed.SessionID != created.SessionID {
		t.Fatalf("无 key 重放=%+v err=%v", replayed, err)
	}
}

// TestEnsureProjectSessionV2ReplayRejectsSemanticallyTamperedReceipt 验证结构仍合法但 version/digest/count 已漂移的 Receipt 不能绕过事务内核对直接重放。
func TestEnsureProjectSessionV2ReplayRejectsSemanticallyTamperedReceipt(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*CommandReceipt)
	}{
		{name: "result version", mutate: func(receipt *CommandReceipt) {
			receipt.ResultVersion = ResultVersionV2 + 1
		}},
		{name: "snapshot digest", mutate: func(receipt *CommandReceipt) {
			receipt.SkillSnapshotDigest = fmt.Sprintf("%064x", 42)
		}},
		{name: "skill count", mutate: func(receipt *CommandReceipt) {
			receipt.SkillCount = 2
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, repository, _ := newV2TestService(t)
			command := newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture())
			if _, err := service.EnsureProjectSessionV2(context.Background(), command); err != nil {
				t.Fatalf("首次 Ensure V2 失败: %v", err)
			}
			repository.mu.Lock()
			tampered := repository.receipts[command.CommandID]
			testCase.mutate(&tampered)
			// 篡改值仍位于字段的基础取值范围内；Service 必须与本次 canonical Snapshot 逐字段比较，不能只信结构校验。
			repository.receipts[command.CommandID] = tampered
			repository.mu.Unlock()
			_, err := service.EnsureProjectSessionV2(context.Background(), command)
			if !errors.Is(err, ErrSnapshotIntegrity) || repository.createdCount() != 1 {
				t.Fatalf("语义篡改 Receipt 重放 err=%v created=%d", err, repository.createdCount())
			}
		})
	}
}

// TestEnsureProjectSessionV2FailsClosedWithoutProtector 验证未装配专用 protector 的新 V2 命令稳定失败，不 panic、不创建事实也不降级调用 V1。
func TestEnsureProjectSessionV2FailsClosedWithoutProtector(t *testing.T) {
	repository := newMemoryRepository()
	service, err := NewService(repository, &sequenceIDGenerator{}, fixedClock{now: time.Now()}, &recordingProtector{})
	if err != nil {
		t.Fatalf("创建 W0 Service 失败: %v", err)
	}
	_, err = service.EnsureProjectSessionV2(
		context.Background(), newV2Command(t, testV2CommandID, emptySkillSnapshotFixture()),
	)
	if !errors.Is(err, ErrContentProtection) || repository.createdCount() != 0 {
		t.Fatalf("未装配 V2 protector err=%v created=%d", err, repository.createdCount())
	}
}

// TestEnsureProjectSessionV2RejectsCrossVersionCommand 验证同一 CommandID 在 V1/V2 之间全局唯一，版本不匹配不是 replay。
func TestEnsureProjectSessionV2RejectsCrossVersionCommand(t *testing.T) {
	repository := newMemoryRepository()
	service, err := NewServiceWithSkillSnapshot(
		repository, &sequenceIDGenerator{}, fixedClock{now: time.Now()}, &recordingProtector{},
		&recordingSkillSnapshotProtector{}, skill.DefaultLimitsProfileV1(),
	)
	if err != nil {
		t.Fatalf("创建双版本 Service 失败: %v", err)
	}
	v1 := newTestCommand(t, testV2CommandID, "V1 first")
	if _, err := service.EnsureProjectSession(context.Background(), v1); err != nil {
		t.Fatalf("首次 V1 Ensure 失败: %v", err)
	}
	_, err = service.EnsureProjectSessionV2(
		context.Background(), newV2Command(t, testV2CommandID, emptySkillSnapshotFixture()),
	)
	if !errors.Is(err, ErrCommandVersionConflict) || repository.createdCount() != 1 {
		t.Fatalf("跨版本命令 err=%v created=%d", err, repository.createdCount())
	}
}

// TestEnsureProjectSessionV2ConcurrentReplay 验证 100 并发相同 command+digest 最终只提交一个 Session/Snapshot/Receipt。
func TestEnsureProjectSessionV2ConcurrentReplay(t *testing.T) {
	service, repository, _ := newV2TestService(t)
	command := newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture())
	const concurrent = 100
	results := make(chan EnsureResult, concurrent)
	errorsChannel := make(chan error, concurrent)
	var waitGroup sync.WaitGroup
	for range concurrent {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, err := service.EnsureProjectSessionV2(context.Background(), command)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatalf("并发 V2 Ensure 失败: %v", err)
	}
	if repository.createdCount() != 1 {
		t.Fatalf("并发 V2 创建数=%d，want 1", repository.createdCount())
	}
	var sessionID string
	for result := range results {
		if sessionID == "" {
			sessionID = result.SessionID
		}
		if result.SessionID != sessionID || result.SkillSnapshotDigest != testV2SnapshotDigest {
			t.Fatalf("并发 V2 Receipt 漂移: %+v", result)
		}
	}
}

// TestEnsureProjectSessionV2ConcurrentDifferentDigest 验证同一 CommandID 的两组自洽语义并发竞争时只有一组胜出，另一组稳定冲突且不能覆盖 Receipt。
func TestEnsureProjectSessionV2ConcurrentDifferentDigest(t *testing.T) {
	service, repository, _ := newV2TestService(t)
	first := newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture())
	second := first
	second.InitialPrompt = "different semantic prompt"
	secondCanonical, err := skill.CanonicalEnsureProjectSessionV2(skill.EnsureProjectSessionInputV2{
		SchemaVersion: second.SchemaVersion, ProjectID: second.ProjectID, OwnerUserID: second.OwnerUserID,
		CreationSource: second.CreationSource, InitialPrompt: second.InitialPrompt, SkillSnapshot: second.SkillSnapshot,
	}, skill.DefaultLimitsProfileV1())
	if err != nil {
		t.Fatalf("构造第二组 V2 语义失败: %v", err)
	}
	second.RequestDigest = secondCanonical.RequestDigest.Hex()
	second.PromptDigest = secondCanonical.PromptDigest

	const concurrent = 100
	results := make(chan EnsureResult, concurrent)
	errorsChannel := make(chan error, concurrent)
	var waitGroup sync.WaitGroup
	for index := range concurrent {
		command := first
		if index%2 == 1 {
			command = second
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, ensureErr := service.EnsureProjectSessionV2(context.Background(), command)
			if ensureErr != nil {
				errorsChannel <- ensureErr
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	successCount := 0
	for range results {
		successCount++
	}
	conflictCount := 0
	for ensureErr := range errorsChannel {
		if !errors.Is(ensureErr, ErrCommandConflict) {
			t.Fatalf("异摘要并发出现非冲突错误: %v", ensureErr)
		}
		conflictCount++
	}
	if repository.createdCount() != 1 || successCount != concurrent/2 || conflictCount != concurrent/2 {
		t.Fatalf("异摘要并发 created=%d success=%d conflict=%d", repository.createdCount(), successCount, conflictCount)
	}
}

// TestEnsureProjectSessionV2RejectsLimitsAndDigestTamper 验证 limits 与任一摘要篡改都在 Repository 前失败，且不截断、不留下部分计划。
func TestEnsureProjectSessionV2RejectsLimitsAndDigestTamper(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*EnsureCommandV2)
		want   error
	}{
		{name: "request digest", mutate: func(command *EnsureCommandV2) {
			command.RequestDigest = fmt.Sprintf("%064x", 1)
		}, want: ErrSnapshotIntegrity},
		{name: "runtime digest", mutate: func(command *EnsureCommandV2) {
			command.SkillSnapshot.Skills[0].RuntimeContentDigest = fmt.Sprintf("%064x", 2)
		}, want: ErrSnapshotIntegrity},
		{name: "item limit", mutate: func(command *EnsureCommandV2) {
			item := command.SkillSnapshot.Skills[0]
			command.SkillSnapshot.Skills = make([]skill.PublishedSkillSnapshotRefV1, 17)
			for index := range command.SkillSnapshot.Skills {
				item.LoadOrder = int32(index + 1)
				item.SkillID = fmt.Sprintf("019f0000-0000-7000-8000-%012x", index+1000)
				item.PublishedSnapshotID = fmt.Sprintf("019f0000-0000-7000-8000-%012x", index+2000)
				command.SkillSnapshot.Skills[index] = item
			}
			command.SkillSnapshot.SkillCount = 17
		}, want: ErrSnapshotLimitExceeded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, repository, _ := newV2TestService(t)
			command := newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture())
			testCase.mutate(&command)
			_, err := service.EnsureProjectSessionV2(context.Background(), command)
			if !errors.Is(err, testCase.want) || repository.createdCount() != 0 {
				t.Fatalf("篡改/超限 err=%v want=%v created=%d", err, testCase.want, repository.createdCount())
			}
		})
	}
}

// TestLoadSessionSkillSnapshotV1RejectsCorruption 验证密文、AAD 身份和 Header set digest 任一损坏都使整个读取失败。
func TestLoadSessionSkillSnapshotV1RejectsCorruption(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*EnsurePlan)
		want   error
	}{
		{name: "ciphertext", mutate: func(plan *EnsurePlan) {
			plan.SkillSnapshotItems[0].RuntimeContent.Ciphertext[len(plan.SkillSnapshotItems[0].RuntimeContent.Ciphertext)-1] ^= 1
		}, want: ErrContentUnavailable},
		{name: "AAD identity", mutate: func(plan *EnsurePlan) {
			plan.SkillSnapshotItems[0].SkillID = "019f0000-0000-7000-8000-000000000109"
		}, want: ErrContentUnavailable},
		{name: "set digest", mutate: func(plan *EnsurePlan) {
			plan.SkillSnapshot.Digest = fmt.Sprintf("%064x", 9)
		}, want: ErrSnapshotIntegrity},
		{name: "sparse load order", mutate: func(plan *EnsurePlan) {
			plan.SkillSnapshotItems[0].LoadOrder = 2
		}, want: ErrSnapshotIntegrity},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, repository, _ := newV2TestService(t)
			result, err := service.EnsureProjectSessionV2(
				context.Background(), newV2Command(t, testV2CommandID, nonEmptySkillSnapshotFixture()),
			)
			if err != nil {
				t.Fatalf("准备 Snapshot 失败: %v", err)
			}
			repository.mu.Lock()
			testCase.mutate(&repository.createdPlans[0])
			repository.mu.Unlock()
			_, err = service.LoadSessionSkillSnapshotV1(context.Background(), result.SessionID)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("损坏读取 err=%v，want %v", err, testCase.want)
			}
		})
	}
}

// TestQueryProjectSessionCommandV2RoutesVersion 验证同一 Query 方法按 schema v2 查询 V2 receipt，并对 V1 receipt 返回版本冲突。
func TestQueryProjectSessionCommandV2RoutesVersion(t *testing.T) {
	service, _, _ := newV2TestService(t)
	command := newV2Command(t, testV2CommandID, emptySkillSnapshotFixture())
	if _, err := service.EnsureProjectSessionV2(context.Background(), command); err != nil {
		t.Fatalf("准备 V2 Receipt 失败: %v", err)
	}
	query := QueryCommand{
		SchemaVersion: QueryCommandSchemaVersionV2,
		RequestID:     "019f0000-0000-7000-8000-0000000000bb", CommandID: command.CommandID,
		ExpectedRequestDigest: command.RequestDigest,
	}
	result, err := service.QueryProjectSessionCommand(context.Background(), query)
	if err != nil || result.Status != QueryCommandStatusCompleted || result.Receipt == nil ||
		result.Receipt.ResultVersion != ResultVersionV2 {
		t.Fatalf("V2 Query=%+v err=%v", result, err)
	}
	query.SchemaVersion = QueryCommandSchemaVersionV1
	_, err = service.QueryProjectSessionCommand(context.Background(), query)
	if !errors.Is(err, ErrCommandVersionConflict) {
		t.Fatalf("V1 Query 命中 V2 Receipt err=%v", err)
	}
}
