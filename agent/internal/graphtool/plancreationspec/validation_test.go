package plancreationspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	validationTestUserID       = "019f68e8-0001-7000-8000-000000000001"
	validationTestProjectID    = "019f68e8-0002-7000-8000-000000000002"
	validationTestToolCallID   = "019f68e8-0003-7000-8000-000000000003"
	validationTestResourceID   = "019f68e8-0004-7000-8000-000000000004"
	validationTestIntentPrefix = `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"`
)

type saveDigestVector struct {
	SchemaVersion string `json:"schema_version"`
	Canonical     struct {
		SchemaVersion          string  `json:"schema_version"`
		UserID                 string  `json:"user_id"`
		ProjectID              string  `json:"project_id"`
		ExpectedProjectVersion int64   `json:"expected_project_version"`
		ToolCallID             string  `json:"tool_call_id"`
		PromptVersion          string  `json:"prompt_version"`
		ValidatorVersion       string  `json:"validator_version"`
		Content                Content `json:"content"`
	} `json:"canonical"`
	CanonicalJSON  string `json:"canonical_json"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

// TestSaveRequestDigestConsumesCrossModuleVector 防止 Agent 与 Business 的字段顺序、版本 Pin 或内容摘要静默漂移。
func TestSaveRequestDigestConsumesCrossModuleVector(t *testing.T) {
	vectorPath := filepath.Join("..", "..", "..", "..", "docs", "design", "cross-module", "testdata", "creation_spec_preview_save_digest_v1.json")
	encoded, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatalf("读取跨 Module 固定向量失败: %v", err)
	}
	var vector saveDigestVector
	if err := json.Unmarshal(encoded, &vector); err != nil {
		t.Fatalf("解析跨 Module 固定向量失败: %v", err)
	}
	if vector.SchemaVersion != "creation_spec.preview.save-draft.digest.corpus.v1" ||
		vector.Canonical.SchemaVersion != SaveDigestSchemaVersion ||
		vector.Canonical.PromptVersion != PromptVersion || vector.Canonical.ValidatorVersion != ValidatorVersion {
		t.Fatalf("固定向量版本 Pin 漂移: %+v", vector)
	}
	canonical, err := json.Marshal(vector.Canonical)
	if err != nil {
		t.Fatalf("编码固定向量 canonical 失败: %v", err)
	}
	if string(canonical) != vector.CanonicalJSON {
		t.Fatalf("canonical JSON 漂移:\n got: %s\nwant: %s", canonical, vector.CanonicalJSON)
	}

	command := DraftCommand{
		TrustedContext: TrustedContext{
			UserID: vector.Canonical.UserID, ProjectID: vector.Canonical.ProjectID,
			ToolCallID: vector.Canonical.ToolCallID, PromptVersion: PromptVersion, ValidatorVersion: ValidatorVersion,
		},
		DomainContext: DomainContext{
			ProjectID: vector.Canonical.ProjectID, ProjectVersion: vector.Canonical.ExpectedProjectVersion,
		},
		Content: vector.Canonical.Content,
	}
	digest, err := SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("计算保存请求摘要失败: %v", err)
	}
	if digest != vector.ExpectedSHA256 || digest != sha256Hex([]byte(vector.CanonicalJSON)) {
		t.Fatalf("保存请求摘要=%q want=%q", digest, vector.ExpectedSHA256)
	}
}

// TestIntentProposalAndResourceDigests 验证模型 Proposal 只能形成与固定内容一致的 Draft，且资源必须绑定原命令内容。
func TestIntentProposalAndResourceDigests(t *testing.T) {
	vector := loadSaveDigestVector(t)
	audience := vector.Canonical.Content.Audience
	intent := Intent{
		SchemaVersion: IntentSchemaVersion, Goal: vector.Canonical.Content.Goal,
		DeliverableType: vector.Canonical.Content.DeliverableType, Audience: &audience,
		Locale: vector.Canonical.Content.Locale, Constraints: append([]string(nil), vector.Canonical.Content.Constraints...),
	}
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		t.Fatalf("编码 Intent 失败: %v", err)
	}
	intentDigest, err := IntentDigest(intent)
	if err != nil {
		t.Fatalf("计算 Intent 摘要失败: %v", err)
	}
	if intentDigest != sha256Hex(intentJSON) {
		t.Fatalf("Intent 摘要=%q want=%q", intentDigest, sha256Hex(intentJSON))
	}

	proposal := Proposal{
		SchemaVersion: ProposalSchemaVersion, Title: vector.Canonical.Content.Title,
		Goal: vector.Canonical.Content.Goal, DeliverableType: vector.Canonical.Content.DeliverableType,
		Audience: vector.Canonical.Content.Audience, Phases: append([]Phase(nil), vector.Canonical.Content.Phases...),
		Constraints:        append([]string(nil), vector.Canonical.Content.Constraints...),
		AcceptanceCriteria: append([]string(nil), vector.Canonical.Content.AcceptanceCriteria...),
	}
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("编码 Proposal 失败: %v", err)
	}
	content, decodedProposal, err := DecodeAndValidateProposal(proposalJSON, intent)
	if err != nil {
		t.Fatalf("校验 Proposal 失败: %v", err)
	}
	if !reflect.DeepEqual(decodedProposal, proposal) || !reflect.DeepEqual(content, vector.Canonical.Content) {
		t.Fatalf("Proposal 未确定性映射为固定 Content: proposal=%+v content=%+v", decodedProposal, content)
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("编码 Content 失败: %v", err)
	}
	contentDigest, err := ContentDigest(content)
	if err != nil {
		t.Fatalf("计算 Content 摘要失败: %v", err)
	}
	if contentDigest != sha256Hex(contentJSON) {
		t.Fatalf("Content 摘要=%q want=%q", contentDigest, sha256Hex(contentJSON))
	}

	command := DraftCommand{
		TrustedContext: TrustedContext{
			UserID: validationTestUserID, ProjectID: validationTestProjectID,
			ToolCallID: validationTestToolCallID, PromptVersion: PromptVersion, ValidatorVersion: ValidatorVersion,
		},
		DomainContext: DomainContext{ProjectID: validationTestProjectID, ProjectVersion: 1},
		Content:       content,
	}
	resource := Resource{
		ID: validationTestResourceID, ProjectID: validationTestProjectID, Version: 1,
		Status: "draft", ContentDigest: contentDigest, Content: content,
	}
	if err := ValidateResourceForCommand(resource, command); err != nil {
		t.Fatalf("合法资源未通过原命令绑定校验: %v", err)
	}

	otherContent := content
	otherContent.Title = "另一份自洽草稿"
	otherDigest, err := ContentDigest(otherContent)
	if err != nil {
		t.Fatalf("计算另一份 Content 摘要失败: %v", err)
	}
	other := resource
	other.Content, other.ContentDigest = otherContent, otherDigest
	if err := ValidateResource(other, validationTestProjectID); err != nil {
		t.Fatalf("另一份自洽资源应通过通用资源校验: %v", err)
	}
	if err := ValidateResourceForCommand(other, command); err == nil {
		t.Fatal("自洽但不属于原命令的资源被错误接受")
	}

	tampered := resource
	tampered.Content.Title = "被篡改"
	if err := ValidateResource(tampered, validationTestProjectID); err == nil {
		t.Fatal("内容与摘要不一致的资源被错误接受")
	}
}

// TestIntentAudiencePresenceIsDigestSignificant 验证 audience 省略、显式空字符串和 null 是三个不同协议状态。
func TestIntentAudiencePresenceIsDigestSignificant(t *testing.T) {
	absentJSON := []byte(`{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作品牌短片","deliverable_type":"video","locale":"zh-CN","constraints":[]}`)
	emptyJSON := []byte(`{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作品牌短片","deliverable_type":"video","audience":"","locale":"zh-CN","constraints":[]}`)
	nullJSON := []byte(`{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作品牌短片","deliverable_type":"video","audience":null,"locale":"zh-CN","constraints":[]}`)

	absent, err := DecodeIntent(absentJSON)
	if err != nil || absent.Audience != nil {
		t.Fatalf("省略 audience 解码结果=%+v err=%v", absent, err)
	}
	empty, err := DecodeIntent(emptyJSON)
	if err != nil || empty.Audience == nil || *empty.Audience != "" {
		t.Fatalf("显式空 audience 解码结果=%+v err=%v", empty, err)
	}
	if _, err := DecodeIntent(nullJSON); err == nil {
		t.Fatal("显式 null audience 被错误接受")
	}
	absentDigest, err := IntentDigest(absent)
	if err != nil {
		t.Fatalf("计算省略 audience 摘要失败: %v", err)
	}
	emptyDigest, err := IntentDigest(empty)
	if err != nil {
		t.Fatalf("计算显式空 audience 摘要失败: %v", err)
	}
	if absentDigest == emptyDigest || absentDigest != sha256Hex(absentJSON) || emptyDigest != sha256Hex(emptyJSON) {
		t.Fatalf("audience presence 未进入摘要: absent=%q empty=%q", absentDigest, emptyDigest)
	}
}

// TestIntentRejectsUnsafeUnicode 验证控制字符、行分隔符、非 NFC 和非法 surrogate 在进入摘要前失败关闭。
func TestIntentRejectsUnsafeUnicode(t *testing.T) {
	for name, goalJSON := range map[string]string{
		"control":             `目标\u0001文本`,
		"line separator":      `目标\u2028文本`,
		"paragraph separator": `目标\u2029文本`,
		"unpaired high":       `目标\ud800文本`,
		"unpaired low":        `目标\udc00文本`,
		"invalid pair":        `目标\ud800\u0041文本`,
		"non NFC":             `Cafe\u0301`,
	} {
		t.Run(name, func(t *testing.T) {
			encoded := []byte(validationTestIntentPrefix + goalJSON + `","deliverable_type":"video","locale":"zh-CN","constraints":[]}`)
			if _, err := DecodeIntent(encoded); err == nil {
				t.Fatalf("不安全 Unicode 被接受: %s", encoded)
			}
		})
	}

	validPair := []byte(validationTestIntentPrefix + `目标\ud83c\udf1e文本","deliverable_type":"video","locale":"zh-CN","constraints":[]}`)
	intent, err := DecodeIntent(validPair)
	if err != nil || !strings.Contains(intent.Goal, "🌞") {
		t.Fatalf("合法 surrogate pair 解码结果=%+v err=%v", intent, err)
	}
}

// TestProposalRejectsUnsafeUnicode 验证模型候选不能用 JSON 转义绕过同一文本策略。
func TestProposalRejectsUnsafeUnicode(t *testing.T) {
	vector := loadSaveDigestVector(t)
	audience := vector.Canonical.Content.Audience
	intent := Intent{
		SchemaVersion: IntentSchemaVersion, Goal: vector.Canonical.Content.Goal,
		DeliverableType: vector.Canonical.Content.DeliverableType, Audience: &audience,
		Locale: vector.Canonical.Content.Locale, Constraints: append([]string(nil), vector.Canonical.Content.Constraints...),
	}
	proposal := Proposal{
		SchemaVersion: ProposalSchemaVersion, Title: vector.Canonical.Content.Title,
		Goal: vector.Canonical.Content.Goal, DeliverableType: vector.Canonical.Content.DeliverableType,
		Audience: vector.Canonical.Content.Audience, Phases: append([]Phase(nil), vector.Canonical.Content.Phases...),
		Constraints:        append([]string(nil), vector.Canonical.Content.Constraints...),
		AcceptanceCriteria: append([]string(nil), vector.Canonical.Content.AcceptanceCriteria...),
	}
	for name, title := range map[string]string{
		"control": "标题\u0001", "line separator": "标题\u2028", "paragraph separator": "标题\u2029",
	} {
		t.Run(name, func(t *testing.T) {
			candidate := proposal
			candidate.Title = title
			encoded, err := json.Marshal(candidate)
			if err != nil {
				t.Fatalf("编码候选失败: %v", err)
			}
			if _, _, err := DecodeAndValidateProposal(encoded, intent); err == nil {
				t.Fatalf("不安全 Proposal Unicode 被接受: %q", title)
			}
		})
	}

	encoded, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("编码基准 Proposal 失败: %v", err)
	}
	unsafeSurrogate := strings.Replace(string(encoded), `"title":"夏日品牌短片"`, `"title":"\ud800"`, 1)
	if _, _, err := DecodeAndValidateProposal([]byte(unsafeSurrogate), intent); err == nil {
		t.Fatal("非法 Proposal surrogate 被错误接受")
	}
}

// TestDurableDraftCommandExcludesLeaseAndRebuildsCurrentFence 验证恢复密文覆盖完整 Save 语义但不固化旧 Owner/Fence。
func TestDurableDraftCommandExcludesLeaseAndRebuildsCurrentFence(t *testing.T) {
	command := recoveryTestCommand(t)
	encoded, payloadDigest, contentDigest, err := EncodeDurableDraftCommand(command)
	if err != nil {
		t.Fatalf("编码 durable Business Command 失败: %v", err)
	}
	if !validLowerSHA256(payloadDigest) || !validLowerSHA256(contentDigest) ||
		strings.Contains(string(encoded), command.TrustedContext.Owner) ||
		strings.Contains(string(encoded), "fence_token") || strings.Contains(string(encoded), "owner") {
		t.Fatalf("durable 命令摘要或易变 Lease 字段错误: %s", encoded)
	}

	current := command.TrustedContext
	current.Owner = "restarted-preview-owner"
	current.FenceToken = command.TrustedContext.FenceToken + 11
	restored, err := DecodeDurableDraftCommand(encoded, current)
	if err != nil {
		t.Fatalf("用当前 Fence 重建 durable 命令失败: %v", err)
	}
	if restored.TrustedContext.Owner != current.Owner || restored.TrustedContext.FenceToken != current.FenceToken ||
		restored.RequestDigest != command.RequestDigest || restored.DomainContext.ProjectVersion != command.DomainContext.ProjectVersion {
		t.Fatalf("重建命令未保持稳定语义/当前 Fence: %+v", restored)
	}

	tampered := strings.Replace(string(encoded), `"expected_project_version":1`, `"expected_project_version":2`, 1)
	if _, err := DecodeDurableDraftCommand([]byte(tampered), current); err == nil {
		t.Fatal("篡改 Project version 的恢复明文被接受")
	}
	unknown := strings.Replace(string(encoded), `{"schema_version":`, `{"unknown":true,"schema_version":`, 1)
	if _, err := DecodeDurableDraftCommand([]byte(unknown), current); err == nil {
		t.Fatal("含未知字段的恢复明文被接受")
	}
}

func loadSaveDigestVector(t *testing.T) saveDigestVector {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "docs", "design", "cross-module", "testdata", "creation_spec_preview_save_digest_v1.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取跨 Module 固定向量失败: %v", err)
	}
	var vector saveDigestVector
	if err := json.Unmarshal(encoded, &vector); err != nil {
		t.Fatalf("解析跨 Module 固定向量失败: %v", err)
	}
	return vector
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
