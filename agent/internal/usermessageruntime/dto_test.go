package usermessageruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
)

// TestValidateClaimRecomputesMessageAndContextDigests 验证 Claim 不能只携带格式正确但语义不匹配的摘要。
func TestValidateClaimRecomputesMessageAndContextDigests(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	if err := ValidateClaim(claim); err != nil {
		t.Fatalf("合法 Claim 被拒绝: %v", err)
	}
	mutated := claim
	mutated.MessagePlaintext += "篡改"
	if !errors.Is(ValidateClaim(mutated), ErrInvalidClaim) {
		t.Fatal("消息明文与 digest 不一致未失败关闭")
	}
	mutated = claim
	mutated.Context.PromptRef = "drifted"
	if !errors.Is(ValidateClaim(mutated), ErrInvalidClaim) {
		t.Fatal("Context pin 与 context_digest 不一致未失败关闭")
	}
	mutated = claim
	mutated.Context.ToolRegistryRef = "analyze_materials"
	if !errors.Is(ValidateClaim(mutated), ErrInvalidClaim) {
		t.Fatal("非空 Tool Registry 未失败关闭")
	}
	mutated = claim
	mutated.Context.PromptDigest = strings.Repeat("c", 64)
	mutated.Context.ContextDigest = runtimeTestContextDigest(t, mutated.Context)
	if !errors.Is(ValidateClaim(mutated), ErrInvalidClaim) {
		t.Fatal("自洽但未批准的 Profile pin 未失败关闭")
	}
}

// TestDecodeDirectResponseCardRejectsNonExactJSON 验证未知、重复、缺失、尾随与超限均完整失败。
func TestDecodeDirectResponseCardRejectsNonExactJSON(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	card := NewDirectResponse(claim)
	encoded, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("编码 fixture 失败: %v", err)
	}
	decoded, err := DecodeDirectResponseCard(string(encoded))
	if err != nil || ValidateDirectResponse(decoded, claim) != nil {
		t.Fatalf("合法 Direct Response 被拒绝: decoded=%+v err=%v", decoded, err)
	}

	invalid := []string{
		strings.Replace(string(encoded), `"summary":`, `"unknown":true,"summary":`, 1),
		strings.Replace(string(encoded), `"summary":`, `"status":"completed","summary":`, 1),
		strings.Replace(string(encoded), `,"summary":"`+DirectResponseSummary+`"`, "", 1),
		string(encoded) + `{}`,
		strings.Repeat("x", MaxModelOutputBytes+1),
	}
	for index, content := range invalid {
		if _, err := DecodeDirectResponseCard(content); !errors.Is(err, ErrOutputContract) {
			t.Fatalf("非法 JSON[%d] err=%v want output contract", index, err)
		}
	}
}

// TestOutputReceiptStageMatchesUnion 验证 open/completed/failed 不能互相伪装。
func TestOutputReceiptStageMatchesUnion(t *testing.T) {
	claim := validRuntimeTestClaim(t)
	direct := NewDirectResponse(claim)
	failure := NewFailure(claim, false)
	valid := []OutputReceiptSnapshot{
		{Stage: OutputReceiptOpen},
		{Stage: OutputReceiptCompleted, Output: &Output{DirectResponse: &direct}},
		{Stage: OutputReceiptFailed, Output: &Output{Failure: &failure}},
	}
	for _, snapshot := range valid {
		if err := ValidateOutputReceipt(snapshot, claim); err != nil {
			t.Fatalf("合法 Output Receipt 被拒绝: %+v err=%v", snapshot, err)
		}
	}
	invalid := []OutputReceiptSnapshot{
		{Stage: OutputReceiptOpen, Output: &Output{DirectResponse: &direct}},
		{Stage: OutputReceiptCompleted, Output: &Output{Failure: &failure}},
		{Stage: OutputReceiptFailed, Output: &Output{DirectResponse: &direct}},
		{Stage: "unknown"},
	}
	for _, snapshot := range invalid {
		if !errors.Is(ValidateOutputReceipt(snapshot, claim), ErrOutputContract) {
			t.Fatalf("非法 Output Receipt 未失败关闭: %+v", snapshot)
		}
	}
}

func validRuntimeTestClaim(t *testing.T) Claim {
	t.Helper()
	message := "请帮我规划一个短视频"
	messageDigest := sha256.Sum256([]byte(message))
	contextValue := turncontext.UserMessageTurnContext{
		SchemaVersion:    turncontext.UserMessageTurnContextSchemaVersion,
		TurnID:           "019f68e8-7201-7000-8000-000000000001",
		SessionID:        "019f68e8-7202-7000-8000-000000000002",
		InputID:          "019f68e8-7203-7000-8000-000000000003",
		MessageID:        "019f68e8-7204-7000-8000-000000000004",
		UserID:           "019f68e8-7205-7000-8000-000000000005",
		ProjectID:        "019f68e8-7206-7000-8000-000000000006",
		MessageCutoffSeq: 1, MessageContentDigest: hex.EncodeToString(messageDigest[:]),
		SkillSnapshotRef:    "session_skill_snapshot:019f68e8-7202-7000-8000-000000000002",
		SkillSnapshotDigest: strings.Repeat("a", 64),
		PromptRef:           PromptRef, PromptDigest: PromptDigest,
		ToolRegistryRef: EmptyToolRegistryRef, ToolRegistryDigest: EmptyToolRegistryDigest,
		RuntimePolicyRef: RuntimePolicyRef, RuntimePolicyDigest: RuntimePolicyDigest,
		ModelRouteRef: LocalFakeModelRouteRef, ModelRouteDigest: LocalFakeModelRouteDigest,
		BudgetRef: BudgetRef, BudgetDigest: BudgetDigest,
		AccessScopeRef:    "ensure_command:019f68e8-7210-7000-8000-000000000010",
		AccessScopeDigest: strings.Repeat("b", 64),
	}
	contextValue.ContextDigest = runtimeTestContextDigest(t, contextValue)
	return Claim{
		Profile: Profile, Owner: "processor-test",
		RunID:           "019f68e8-7207-7000-8000-000000000007",
		ModelCallID:     "019f68e8-7208-7000-8000-000000000008",
		OutputID:        "019f68e8-7209-7000-8000-000000000009",
		RecoveryEventID: "019f68e8-7211-7000-8000-000000000011",
		TerminalEventID: "019f68e8-7212-7000-8000-000000000012",
		FenceToken:      7, Attempts: 1, EnqueueSeq: 1, Context: contextValue, MessagePlaintext: message,
	}
}

func runtimeTestContextDigest(t *testing.T, contextValue turncontext.UserMessageTurnContext) string {
	t.Helper()
	digest, err := session.DigestUserMessageContext(session.UserMessageContext{
		TurnID: contextValue.TurnID, SchemaVersion: contextValue.SchemaVersion,
		SessionID: contextValue.SessionID, InputID: contextValue.InputID, MessageID: contextValue.MessageID,
		UserID: contextValue.UserID, ProjectID: contextValue.ProjectID,
		MessageCutoffSeq: contextValue.MessageCutoffSeq, MessageContentDigest: contextValue.MessageContentDigest,
		SkillSnapshotRef: contextValue.SkillSnapshotRef, SkillSnapshotDigest: contextValue.SkillSnapshotDigest,
		PromptRef: contextValue.PromptRef, PromptDigest: contextValue.PromptDigest,
		ToolRegistryRef: contextValue.ToolRegistryRef, ToolRegistryDigest: contextValue.ToolRegistryDigest,
		RuntimePolicyRef: contextValue.RuntimePolicyRef, RuntimePolicyDigest: contextValue.RuntimePolicyDigest,
		ModelRouteRef: contextValue.ModelRouteRef, ModelRouteDigest: contextValue.ModelRouteDigest,
		BudgetRef: contextValue.BudgetRef, BudgetDigest: contextValue.BudgetDigest,
		AccessScopeRef: contextValue.AccessScopeRef, AccessScopeDigest: contextValue.AccessScopeDigest,
	})
	if err != nil {
		t.Fatalf("计算 fixture Context digest 失败: %v", err)
	}
	return digest
}
