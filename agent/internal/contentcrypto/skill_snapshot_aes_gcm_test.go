package contentcrypto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
)

// TestSkillSnapshotProtectorBatchRoundTrip 验证专用 purpose/AAD 可批量保护、保持顺序、使用独立 Nonce 且数据库密文不含 Fixture 明文。
func TestSkillSnapshotProtectorBatchRoundTrip(t *testing.T) {
	rootKey := []byte("0123456789abcdef0123456789abcdef")
	random := bytes.NewReader([]byte("123456789012abcdefghijkl"))
	protector, err := newSkillSnapshotAES256GCMProtector(rootKey, "skill-snapshot-key-v1", random)
	if err != nil {
		t.Fatalf("创建专用保护器失败: %v", err)
	}
	fixtures := []session.SkillSnapshotPlaintext{
		newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000101", []byte(`{"name":"敏感 Skill A"}`)),
		newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000102", []byte(`{"name":"敏感 Skill B"}`)),
	}
	protected, err := protector.ProtectBatch(context.Background(), fixtures)
	if err != nil {
		t.Fatalf("批量保护失败: %v", err)
	}
	if len(protected) != 2 || protected[0].Identity != fixtures[0].Identity || protected[1].Identity != fixtures[1].Identity {
		t.Fatalf("批量保护顺序或身份漂移: %+v", protected)
	}
	for index, item := range protected {
		if err := session.ValidateEnvelopeV1(item.Protected.Ciphertext); err != nil {
			t.Fatalf("Item %d Envelope 无效: %v", index, err)
		}
		if bytes.Contains(item.Protected.Ciphertext, fixtures[index].CanonicalBytes) {
			t.Fatalf("Item %d 密文包含 Fixture 明文", index)
		}
	}
	if bytes.Equal(protected[0].Protected.Ciphertext[7:19], protected[1].Protected.Ciphertext[7:19]) {
		t.Fatal("两个 Item 重用了 GCM Nonce")
	}
	opened, err := protector.OpenBatch(context.Background(), protected)
	if err != nil || !bytes.Equal(opened[0], fixtures[0].CanonicalBytes) || !bytes.Equal(opened[1], fixtures[1].CanonicalBytes) {
		t.Fatalf("批量解密=%q err=%v", opened, err)
	}
}

// TestSkillSnapshotProtectorRejectsAADTamperCiphertextAndUnknownKey 验证身份串线、密文损坏与 key 不可用统一失败且不返回局部明文。
func TestSkillSnapshotProtectorRejectsAADTamperCiphertextAndUnknownKey(t *testing.T) {
	protector, err := newSkillSnapshotAES256GCMProtector(
		[]byte("0123456789abcdef0123456789abcdef"),
		"skill-snapshot-key-v1",
		bytes.NewReader([]byte("123456789012abcdefghijklmnopqrstuvwx")),
	)
	if err != nil {
		t.Fatalf("创建保护器失败: %v", err)
	}
	fixtures := []session.SkillSnapshotPlaintext{
		newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000101", []byte(`{"name":"first"}`)),
		newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000102", []byte(`{"name":"second"}`)),
	}
	protected, err := protector.ProtectBatch(context.Background(), fixtures)
	if err != nil {
		t.Fatalf("准备密文失败: %v", err)
	}

	testCases := []struct {
		name   string
		mutate func([]session.SkillSnapshotCiphertext)
	}{
		{name: "AAD skill_id 串线", mutate: func(values []session.SkillSnapshotCiphertext) {
			values[1].Identity.SkillID = "019f0000-0000-7000-8000-000000000103"
		}},
		{name: "密文损坏", mutate: func(values []session.SkillSnapshotCiphertext) {
			values[1].Protected.Ciphertext[len(values[1].Protected.Ciphertext)-1] ^= 1
		}},
		{name: "key unavailable", mutate: func(values []session.SkillSnapshotCiphertext) {
			values[1].Protected.KeyVersion = "missing-key-v2"
		}},
		{name: "runtime digest 串线", mutate: func(values []session.SkillSnapshotCiphertext) {
			values[1].Identity.RuntimeContentDigest = values[0].Identity.RuntimeContentDigest
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			mutated := cloneSkillSnapshotCiphertexts(protected)
			testCase.mutate(mutated)
			opened, err := protector.OpenBatch(context.Background(), mutated)
			if !errors.Is(err, session.ErrContentUnavailable) || opened != nil {
				t.Fatalf("损坏读取=%q err=%v，want nil/ErrContentUnavailable", opened, err)
			}
		})
	}
}

// TestSkillSnapshotProtectorUsesIndependentPurpose 验证相同根密钥和 Nonce 下，Prompt 与 Skill Snapshot 仍因 key purpose/AAD 分域而产生不同密文。
func TestSkillSnapshotProtectorUsesIndependentPurpose(t *testing.T) {
	rootKey := []byte("0123456789abcdef0123456789abcdef")
	nonce := []byte("123456789012")
	messageProtector, err := newAES256GCMProtector(rootKey, "shared-root-v1", bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("创建 Prompt 保护器失败: %v", err)
	}
	snapshotProtector, err := newSkillSnapshotAES256GCMProtector(rootKey, "shared-root-v1", bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("创建 Snapshot 保护器失败: %v", err)
	}
	plaintext := []byte(`{"name":"same plaintext"}`)
	message, err := messageProtector.Protect(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("保护 Prompt 失败: %v", err)
	}
	snapshot, err := snapshotProtector.ProtectBatch(context.Background(), []session.SkillSnapshotPlaintext{
		newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000101", plaintext),
	})
	if err != nil {
		t.Fatalf("保护 Snapshot 失败: %v", err)
	}
	if bytes.Equal(message.Ciphertext, snapshot[0].Protected.Ciphertext) {
		t.Fatal("Prompt 与 Skill Snapshot 未实现密码学 purpose 分域")
	}
}

// TestSkillSnapshotProtectorRejectsPartialBatch 验证批次中任一输入不合法时不返回前序 Item 的局部密文。
func TestSkillSnapshotProtectorRejectsPartialBatch(t *testing.T) {
	protector, err := newSkillSnapshotAES256GCMProtector(
		make([]byte, 32), "skill-snapshot-key-v1", bytes.NewReader(make([]byte, 24)),
	)
	if err != nil {
		t.Fatalf("创建保护器失败: %v", err)
	}
	valid := newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000101", []byte(`{"name":"valid"}`))
	invalid := newSkillSnapshotPlaintext("019f0000-0000-7000-8000-000000000102", nil)
	protected, err := protector.ProtectBatch(context.Background(), []session.SkillSnapshotPlaintext{valid, invalid})
	if err == nil || protected != nil {
		t.Fatalf("部分失败返回=%+v err=%v", protected, err)
	}
}

// TestSkillSnapshotProtectorOpensPreviousKey 验证轮换窗口按明确 previous KeyVersion 读取历史 Item，不会尝试其他 key。
func TestSkillSnapshotProtectorOpensPreviousKey(t *testing.T) {
	activeKey := []byte("0123456789abcdef0123456789abcdef")
	previousKey := []byte("abcdef0123456789abcdef0123456789")
	oldProtector, err := NewSkillSnapshotAES256GCMProtector(previousKey, "skill-key-v1")
	if err != nil {
		t.Fatalf("创建旧 Snapshot 保护器失败: %v", err)
	}
	fixture := newSkillSnapshotPlaintext(
		"019f0000-0000-7000-8000-000000000101", []byte(`{"name":"historical"}`),
	)
	protected, err := oldProtector.ProtectBatch(context.Background(), []session.SkillSnapshotPlaintext{fixture})
	if err != nil {
		t.Fatalf("保护历史 Snapshot 失败: %v", err)
	}
	keyring, err := NewSkillSnapshotAES256GCMProtectorWithPrevious(
		activeKey, "skill-key-v2", previousKey, "skill-key-v1",
	)
	if err != nil {
		t.Fatalf("创建轮换 Snapshot Keyring 失败: %v", err)
	}
	opened, err := keyring.OpenBatch(context.Background(), protected)
	if err != nil || len(opened) != 1 || !bytes.Equal(opened[0], fixture.CanonicalBytes) {
		t.Fatalf("读取 previous key Snapshot=%q err=%v", opened, err)
	}
}

// TestSkillSnapshotProtectorRejectsSameRootKeyRotation 验证不同 KeyVersion 不能复用相同根密钥，避免轮换审计与实际密码材料漂移。
func TestSkillSnapshotProtectorRejectsSameRootKeyRotation(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	if _, err := NewSkillSnapshotAES256GCMProtectorWithPrevious(
		key, "skill-key-v2", append([]byte(nil), key...), "skill-key-v1",
	); err == nil {
		t.Fatal("Snapshot Keyring 接受了不同版本复用同一根密钥")
	}
}

// newSkillSnapshotPlaintext 构造摘要与 canonical bytes 一致的 AAD Fixture。
func newSkillSnapshotPlaintext(skillID string, canonical []byte) session.SkillSnapshotPlaintext {
	digest := sha256.Sum256(canonical)
	return session.SkillSnapshotPlaintext{
		Identity: session.SkillSnapshotContentIdentity{
			SessionID:            "019f0000-0000-7000-8000-000000000001",
			SkillID:              skillID,
			PublishedSnapshotID:  "019f0000-0000-7000-8000-000000000201",
			RuntimeContentDigest: hex.EncodeToString(digest[:]),
		},
		CanonicalBytes: append([]byte(nil), canonical...),
	}
}

// cloneSkillSnapshotCiphertexts 深复制测试密文，避免单个篡改案例污染其他用例。
func cloneSkillSnapshotCiphertexts(values []session.SkillSnapshotCiphertext) []session.SkillSnapshotCiphertext {
	cloned := make([]session.SkillSnapshotCiphertext, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].Protected.Ciphertext = append([]byte(nil), value.Protected.Ciphertext...)
	}
	return cloned
}
