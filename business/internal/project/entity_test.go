package project

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newUUIDv7ForTest 生成测试专用 UUIDv7，随机源失败时立即终止当前测试。
func newUUIDv7ForTest(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate test UUIDv7: %v", err)
	}
	return id.String()
}

// validQuickCreateSeed 返回满足 W0 初始状态不变量的基础 Seed，测试可按场景覆盖字段。
func validQuickCreateSeed(t *testing.T) QuickCreateSeed {
	t.Helper()
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	return QuickCreateSeed{
		ProjectID: newUUIDv7ForTest(t), ReceiptID: newUUIDv7ForTest(t), BindingID: newUUIDv7ForTest(t),
		CommandID: newUUIDv7ForTest(t), OwnerUserID: newUUIDv7ForTest(t),
		KeyDigest:   SHA256Digest([]byte("idempotency-key")),
		MaxAttempts: 5, OccurredAt: now,
	}
}

func TestEnsureSessionCanonicalDigestFrozenVector(t *testing.T) {
	normalized, promptDigest, present, err := NormalizeEnsureSessionPrompt(" e\u0301 ")
	if err != nil {
		t.Fatalf("normalize initial prompt: %v", err)
	}
	if normalized != " é " || !present || promptDigest.Hex() != "273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df" {
		t.Fatalf("unexpected prompt canonicalization: normalized=%q present=%t digest=%s", normalized, present, promptDigest.Hex())
	}

	requestDigest, err := CalculateEnsureSessionRequestDigest(
		"019F0000-0000-7000-8000-0000000000AB",
		"019F0000-0000-7000-8000-0000000000CD",
		present,
		promptDigest,
	)
	if err != nil {
		t.Fatalf("calculate ensure session request digest: %v", err)
	}
	// 该固定向量约束 Canonical JSON 字段顺序、UUID 小写、UTF-8 无空格编码和 SHA-256 小写十六进制表示。
	if requestDigest.Hex() != "35141e4689f43dc9778773f4cf20cd9a6633e22eed18cfde4059f6d5d9841fc4" {
		t.Fatalf("unexpected frozen request digest: %s", requestDigest.Hex())
	}
	quickCreateDigest, err := CalculateQuickCreateSemanticDigest(present, promptDigest)
	if err != nil {
		t.Fatalf("calculate quick create semantic digest: %v", err)
	}
	if quickCreateDigest.Hex() != "dbf7920d5641b2ed5a2564b8d09228e89bbdcd2281085d1fe2c7f59d221457e4" {
		t.Fatalf("unexpected frozen quick create digest: %s", quickCreateDigest.Hex())
	}
}

func TestNormalizeEnsureSessionPromptCollapsesOnlyAllWhitespace(t *testing.T) {
	if normalized, digest, present, err := NormalizeEnsureSessionPrompt(" \n\t　"); err != nil || normalized != "" || present || digest != (Digest{}) {
		t.Fatalf("unexpected blank prompt result: normalized=%q present=%t digest=%s err=%v", normalized, present, digest.Hex(), err)
	}
	normalized, _, present, err := NormalizeEnsureSessionPrompt("  正文  ")
	if err != nil || !present || normalized != "  正文  " {
		t.Fatalf("non-empty prompt whitespace was changed: normalized=%q present=%t err=%v", normalized, present, err)
	}
}

func TestNewQuickCreateAggregateWithoutPrompt(t *testing.T) {
	aggregate, err := NewQuickCreateAggregate(validQuickCreateSeed(t))
	if err != nil {
		t.Fatalf("create aggregate without prompt: %v", err)
	}
	if aggregate.Project.RecentRunStatus != RecentRunStatusIdle || aggregate.Project.InitialPromptStatus != InitialPromptStatusAbsent {
		t.Fatalf("unexpected empty prompt project status: %+v", aggregate.Project)
	}
	if aggregate.Outbox.HasInitialPrompt || aggregate.Outbox.EncryptedPayload != nil {
		t.Fatalf("empty prompt persisted encrypted payload metadata: %+v", aggregate.Outbox)
	}
}

func TestNewQuickCreateAggregateWithEncryptedPrompt(t *testing.T) {
	seed := validQuickCreateSeed(t)
	seed.InitialPrompt = "normalized prompt"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("ciphertext-with-authentication-tag"), PayloadDigest: SHA256Digest([]byte("normalized prompt")),
	}
	aggregate, err := NewQuickCreateAggregate(seed)
	if err != nil {
		t.Fatalf("create aggregate with encrypted prompt: %v", err)
	}
	if aggregate.Project.RecentRunStatus != RecentRunStatusQueued || aggregate.Project.InitialPromptStatus != InitialPromptStatusPending {
		t.Fatalf("unexpected prompt project status: %+v", aggregate.Project)
	}

	// 聚合必须深拷贝敏感切片，避免调用方在提交前改写密文导致持久化语义漂移。
	seed.EncryptedPayload.Ciphertext[0] = 'X'
	if aggregate.Outbox.EncryptedPayload.Ciphertext[0] == 'X' {
		t.Fatal("encrypted payload ciphertext was not cloned")
	}
}

func TestNewQuickCreateAggregateRejectsPromptPayloadMismatch(t *testing.T) {
	seed := validQuickCreateSeed(t)
	seed.InitialPrompt = "真实用户提示词"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("ciphertext-with-authentication-tag"), PayloadDigest: SHA256Digest([]byte("伪造的另一段提示词")),
	}
	if _, err := NewQuickCreateAggregate(seed); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected real prompt and payload digest mismatch rejection, got %v", err)
	}

	seed = validQuickCreateSeed(t)
	seed.InitialPrompt = " \n\t　"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("ciphertext-with-authentication-tag"), PayloadDigest: SHA256Digest([]byte("blank")),
	}
	if _, err := NewQuickCreateAggregate(seed); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected all-whitespace prompt with payload rejection, got %v", err)
	}
}

func TestNewQuickCreateAggregateUsesRealPromptCanonicalSemantics(t *testing.T) {
	seed := validQuickCreateSeed(t)
	seed.InitialPrompt = "第一段提示词"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("first-ciphertext-with-auth-tag"), PayloadDigest: SHA256Digest([]byte("第一段提示词")),
	}
	first, err := NewQuickCreateAggregate(seed)
	if err != nil {
		t.Fatalf("create first real prompt aggregate: %v", err)
	}
	seed.InitialPrompt = "第二段提示词"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("abcdefghijkl"),
		Ciphertext: []byte("second-ciphertext-with-auth-tag"), PayloadDigest: SHA256Digest([]byte("第二段提示词")),
	}
	second, err := NewQuickCreateAggregate(seed)
	if err != nil {
		t.Fatalf("create second real prompt aggregate: %v", err)
	}
	if first.Receipt.SemanticDigest == second.Receipt.SemanticDigest {
		t.Fatal("different real prompts produced the same QuickCreate semantic digest")
	}
}

func TestNewQuickCreateAggregateTreatsNFCEquivalentPromptsAsSameSemantics(t *testing.T) {
	seed := validQuickCreateSeed(t)
	normalized, digest, present, err := NormalizeEnsureSessionPrompt(" e\u0301 ")
	if err != nil || !present || normalized != " é " {
		t.Fatalf("normalize decomposed prompt: normalized=%q present=%t err=%v", normalized, present, err)
	}
	seed.InitialPrompt = " e\u0301 "
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("first-ciphertext-with-auth-tag"), PayloadDigest: digest,
	}
	decomposed, err := NewQuickCreateAggregate(seed)
	if err != nil {
		t.Fatalf("create decomposed prompt aggregate: %v", err)
	}
	seed.InitialPrompt = " é "
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("abcdefghijkl"),
		Ciphertext: []byte("second-ciphertext-with-auth-tag"), PayloadDigest: digest,
	}
	composed, err := NewQuickCreateAggregate(seed)
	if err != nil {
		t.Fatalf("create composed prompt aggregate: %v", err)
	}
	if decomposed.Receipt.SemanticDigest != composed.Receipt.SemanticDigest || decomposed.Outbox.RequestDigest != composed.Outbox.RequestDigest {
		t.Fatal("NFC-equivalent prompts produced different semantic digests")
	}
}

func TestSessionOutboxAllowsDigestOnlyAfterDelivered(t *testing.T) {
	seed := validQuickCreateSeed(t)
	seed.InitialPrompt = "normalized prompt"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("123456789012"),
		Ciphertext: []byte("ciphertext-with-authentication-tag"), PayloadDigest: SHA256Digest([]byte("normalized prompt")),
	}
	aggregate, err := NewQuickCreateAggregate(seed)
	if err != nil {
		t.Fatalf("create prompt aggregate: %v", err)
	}

	deliveredAt := seed.OccurredAt.Add(time.Minute)
	clearedAt := deliveredAt.Add(time.Second)
	cleared := aggregate.Outbox
	cleared.Status = OutboxStatusDelivered
	cleared.DeliveredAt = &deliveredAt
	cleared.PayloadClearedAt = &clearedAt
	cleared.UpdatedAt = clearedAt
	cleared.EncryptedPayload = &EncryptedPayload{PayloadDigest: seed.EncryptedPayload.PayloadDigest}
	if err := cleared.Validate(); err != nil {
		t.Fatalf("validate delivered and cleared prompt: %v", err)
	}

	notDelivered := cleared
	notDelivered.Status = OutboxStatusPending
	notDelivered.DeliveredAt = nil
	if err := notDelivered.Validate(); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected prompt clear before delivery rejection, got %v", err)
	}

	clearedTooEarly := cleared
	earlyTime := deliveredAt.Add(-time.Second)
	clearedTooEarly.PayloadClearedAt = &earlyTime
	if err := clearedTooEarly.Validate(); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected cleared_at before delivered_at rejection, got %v", err)
	}
}

func TestQuickCreateAggregateRejectsPlaintextShapeAndCrossRecordMismatch(t *testing.T) {
	seed := validQuickCreateSeed(t)
	seed.InitialPrompt = "prompt"
	seed.EncryptedPayload = &EncryptedPayload{
		Algorithm: PromptEncryptionAlgorithm, KeyVersion: "key-v1", Nonce: []byte("too-short"),
		Ciphertext: []byte("plaintext"), PayloadDigest: SHA256Digest([]byte("prompt")),
	}
	if _, err := NewQuickCreateAggregate(seed); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected invalid encrypted payload, got %v", err)
	}

	aggregate, err := NewQuickCreateAggregate(validQuickCreateSeed(t))
	if err != nil {
		t.Fatalf("create baseline aggregate: %v", err)
	}
	aggregate.Binding.CommandID = newUUIDv7ForTest(t)
	if err := aggregate.Validate(); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected cross-record command mismatch, got %v", err)
	}

	aggregate, err = NewQuickCreateAggregate(validQuickCreateSeed(t))
	if err != nil {
		t.Fatalf("create request digest tamper baseline: %v", err)
	}
	tamperedDigest := SHA256Digest([]byte("forged-agent-request"))
	aggregate.Binding.RequestDigest = tamperedDigest
	aggregate.Outbox.RequestDigest = tamperedDigest
	if err := aggregate.Validate(); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected recomputed Agent request digest rejection, got %v", err)
	}

	aggregate, err = NewQuickCreateAggregate(validQuickCreateSeed(t))
	if err != nil {
		t.Fatalf("create HTTP semantic digest tamper baseline: %v", err)
	}
	aggregate.Receipt.SemanticDigest = SHA256Digest([]byte("forged-http-semantic"))
	if err := aggregate.Validate(); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected recomputed QuickCreate semantic digest rejection, got %v", err)
	}

	aggregate, err = NewQuickCreateAggregate(validQuickCreateSeed(t))
	if err != nil {
		t.Fatalf("create title tamper baseline: %v", err)
	}
	aggregate.Project.Title = "用户首提示词"
	if err := aggregate.Validate(); !errors.Is(err, ErrInvalidQuickCreate) {
		t.Fatalf("expected non-default title rejection, got %v", err)
	}
}

func TestResultFromReceiptMarksReplayWithoutChangingSnapshot(t *testing.T) {
	aggregate, err := NewQuickCreateAggregate(validQuickCreateSeed(t))
	if err != nil {
		t.Fatalf("create baseline aggregate: %v", err)
	}
	result := ResultFromReceipt(aggregate.Receipt, true)
	if !result.IdempotentReplay || result.ProjectID != aggregate.Project.ID || result.CreatedAt != aggregate.Project.CreatedAt {
		t.Fatalf("unexpected replay result: %+v", result)
	}
}
