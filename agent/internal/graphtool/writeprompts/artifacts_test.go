package writeprompts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestArtifactDigestsBindCanonicalContentInsteadOfVersionRefs(t *testing.T) {
	t.Parallel()
	info, err := CanonicalToolInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolInfo(info); err != nil {
		t.Fatalf("ValidateToolInfo() error=%v", err)
	}
	for _, test := range []struct {
		name    string
		version string
		digest  string
	}{
		{name: "tool", version: ToolDefinitionVersion, digest: ToolDefinitionDigest()},
		{name: "prompt", version: PromptVersion, digest: PromptArtifactDigest()},
		{name: "candidate validator", version: ValidatorVersion, digest: ValidatorArtifactDigest()},
		{name: "exact-set validator", version: ExactSetValidatorVersion, digest: ExactSetValidatorArtifactDigest()},
	} {
		if !validLowerSHA256(test.digest) {
			t.Fatalf("%s artifact digest=%q 非 canonical SHA-256", test.name, test.digest)
		}
		versionSHA := sha256.Sum256([]byte(test.version))
		if test.digest == hex.EncodeToString(versionSHA[:]) {
			t.Fatalf("%s artifact digest 仍只是版本 Ref %q 的摘要", test.name, test.version)
		}
	}

	zhDigest, err := RuntimePolicyDigest(Policy{Version: RuntimePolicyVersion, MaxTargets: 8, DefaultOutputLanguage: "zh-CN", MaxCommandResends: 1})
	if err != nil {
		t.Fatal(err)
	}
	enDigest, err := RuntimePolicyDigest(Policy{Version: RuntimePolicyVersion, MaxTargets: 8, DefaultOutputLanguage: "en-US", MaxCommandResends: 1})
	if err != nil {
		t.Fatal(err)
	}
	if zhDigest == enDigest || !validLowerSHA256(zhDigest) || !validLowerSHA256(enDigest) {
		t.Fatalf("Runtime Policy 摘要未绑定具体策略: zh=%s en=%s", zhDigest, enDigest)
	}
}

func TestValidateToolInfoRejectsDescriptionAndSchemaDrift(t *testing.T) {
	t.Parallel()
	descriptionDrift, err := CanonicalToolInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	descriptionDrift.Desc += "漂移"
	if err := ValidateToolInfo(descriptionDrift); err == nil {
		t.Fatal("Tool 描述漂移未失败关闭")
	}
	relaxed := &schema.ToolInfo{
		Name: ToolKey,
		Desc: "基于 Runtime 已绑定的 Storyboard Preview Draft 全部 Slot 编写严格结构化 Prompt 开发预览草稿。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {Type: schema.String, Required: true},
		}),
	}
	if err := ValidateToolInfo(relaxed); err == nil {
		t.Fatal("同名但放宽的 Tool JSON Schema 未失败关闭")
	}
}
