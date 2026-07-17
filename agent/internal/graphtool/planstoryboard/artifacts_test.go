package planstoryboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestArtifactDigestsBindRealCanonicalContent(t *testing.T) {
	info, err := CanonicalToolInfo(context.Background())
	if err != nil || ValidateToolInfo(info) != nil {
		t.Fatalf("canonical Tool Definition 无效: info=%+v err=%v", info, err)
	}
	canonicalSHA := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for name, value := range map[string]string{
		"tool": ToolDefinitionDigest(), "prompt": PromptArtifactDigest(),
		"validator": ValidatorArtifactDigest(), "dag": DAGValidatorArtifactDigest(),
	} {
		if !canonicalSHA.MatchString(value) {
			t.Fatalf("%s artifact digest=%q 非 canonical SHA-256", name, value)
		}
	}
	for name, pair := range map[string][2]string{
		"tool":      {ToolDefinitionDigest(), "c9160b4e45e67e18d4d0df926bf9c901780af73006ae5e9ac4c0705816d2c6a6"},
		"prompt":    {PromptArtifactDigest(), "ad67c45314180374bf35b621786406ae237b0333c0bb12b63cd9706ae641d446"},
		"validator": {ValidatorArtifactDigest(), "b827fd37ffd3c9d069e20162d51c907f2dd902d029dcafed452905778d5f7be8"},
		"dag":       {DAGValidatorArtifactDigest(), "a59c4fd281970ddabbfb607fbae2e5e1baf6907e887a10124c2b7875639dc63c"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s artifact approval digest 漂移: got=%s want=%s", name, pair[0], pair[1])
		}
	}
	for ref, digest := range map[string]string{
		ToolDefinitionVersion: ToolDefinitionDigest(), PromptVersion: PromptArtifactDigest(),
		ValidatorVersion: ValidatorArtifactDigest(), DAGValidatorVersion: DAGValidatorArtifactDigest(),
	} {
		refSHA := sha256.Sum256([]byte(ref))
		if digest == hex.EncodeToString(refSHA[:]) {
			t.Fatalf("artifact digest 仍只是 Ref %q 的摘要", ref)
		}
	}
}

func TestValidateToolInfoRejectsDescriptionAndStrictSchemaDrift(t *testing.T) {
	descriptionDrift, _ := CanonicalToolInfo(context.Background())
	descriptionDrift.Desc += "漂移"
	if err := ValidateToolInfo(descriptionDrift); err == nil {
		t.Fatal("Tool 描述漂移未失败关闭")
	}
	relaxed := &schema.ToolInfo{
		Name: ToolKey, Desc: descriptionDrift.Desc[:len(descriptionDrift.Desc)-len("漂移")],
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {Type: schema.String, Required: true},
		}),
	}
	if err := ValidateToolInfo(relaxed); err == nil {
		t.Fatal("同名但放宽的 Tool JSON Schema 未失败关闭")
	}
}
