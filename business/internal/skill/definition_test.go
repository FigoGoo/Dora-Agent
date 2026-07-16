package skill

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// validDefinitionForTest 返回六字段完整、数组非 nil 且无公共 Tool 引用的最小合法定义。
func validDefinitionForTest() SkillDefinitionV1 {
	enabled := CapabilityGuidanceV1{Applicability: "enabled", Guidance: "按稳定步骤处理"}
	notApplicable := CapabilityGuidanceV1{Applicability: "not_applicable", NotApplicableReason: "当前场景不需要"}
	return SkillDefinitionV1{
		SchemaVersion: DefinitionSchemaVersionV1,
		Name:          "创作策划", Summary: "整理创作需求", Category: "video", Tags: []string{"视频", "策划"},
		InputDescription: "目标与素材", OutputDescription: "创作方案", InvocationRules: "匹配策划目标时使用",
		PlanCreationSpec: enabled, AnalyzeMaterials: enabled, PlanStoryboard: enabled,
		GenerateMedia: notApplicable, WritePrompts: enabled, AssembleOutput: notApplicable,
		Examples:       []SkillExampleV1{{Input: "制作介绍视频", Output: "输出结构化方案"}},
		StarterPrompts: []string{"帮我策划介绍视频"}, MarketListing: MarketListingV1{Detail: "创作策划说明"},
		PublicToolRefs: []PublicToolReferenceV1{},
	}
}

func TestNormalizeDefinitionV1CanonicalDigestStableAcrossNFCAndArrayOrder(t *testing.T) {
	left := validDefinitionForTest()
	left.Name = " Cafe\u0301 "
	left.Tags = []string{"视频", "策划", "视频"}
	left.StarterPrompts = []string{"第二条", "第一条", "第二条"}
	right := validDefinitionForTest()
	right.Name = "Café"
	right.Tags = []string{"策划", "视频"}
	right.StarterPrompts = []string{"第一条", "第二条"}

	leftNormalized, err := NormalizeDefinitionV1(left)
	if err != nil {
		t.Fatalf("normalize left definition: %v", err)
	}
	rightNormalized, err := NormalizeDefinitionV1(right)
	if err != nil {
		t.Fatalf("normalize right definition: %v", err)
	}
	leftJSON, leftDigest, err := CanonicalDefinitionV1(leftNormalized)
	if err != nil {
		t.Fatal(err)
	}
	rightJSON, rightDigest, err := CanonicalDefinitionV1(rightNormalized)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftJSON, rightJSON) || leftDigest != rightDigest || leftNormalized.Name != "Café" {
		t.Fatalf("canonicalization diverged:\nleft=%s\nright=%s", leftJSON, rightJSON)
	}
	if !bytes.Contains(leftJSON, []byte(`"cover_asset_id":null`)) {
		t.Fatalf("missing Asset reference must canonicalize to JSON null: %s", leftJSON)
	}
	if len(leftNormalized.Tags) != 2 || leftNormalized.Tags[0] != "策划" || len(leftNormalized.StarterPrompts) != 2 {
		t.Fatalf("arrays were not deduplicated and sorted: tags=%v prompts=%v", leftNormalized.Tags, leftNormalized.StarterPrompts)
	}
}

func TestNormalizeDefinitionV1RejectsCapabilityMutualExclusion(t *testing.T) {
	definition := validDefinitionForTest()
	definition.WritePrompts = CapabilityGuidanceV1{
		Applicability: "enabled", Guidance: "需要提示词", NotApplicableReason: "又声明不适用",
	}
	_, err := NormalizeDefinitionV1(definition)
	var validation *ValidationError
	if !errors.As(err, &validation) || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("expected validation error, got %v", err)
	}
	found := false
	for _, fieldError := range validation.FieldErrors {
		if fieldError.Field == "write_prompts.not_applicable_reason" && fieldError.Code == "MUST_BE_EMPTY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stable mutual exclusion field error: %+v", validation.FieldErrors)
	}
}

func TestNormalizeDefinitionV1FailsClosedForNonEmptyPublicToolRefs(t *testing.T) {
	definition := validDefinitionForTest()
	definition.PublicToolRefs = []PublicToolReferenceV1{PublicToolReferenceV1(json.RawMessage(`{"tool_key":"forbidden"}`))}
	_, err := NormalizeDefinitionV1(definition)
	var validation *ValidationError
	if !errors.As(err, &validation) || !errors.Is(err, ErrToolReferenceUnavailable) {
		t.Fatalf("expected unavailable public tool error, got %v", err)
	}
	if len(validation.FieldErrors) != 1 || validation.FieldErrors[0].Field != "public_tool_refs" ||
		validation.FieldErrors[0].Code != "SKILL_TOOL_REFERENCE_UNAVAILABLE" {
		t.Fatalf("unexpected field errors: %+v", validation.FieldErrors)
	}
}

func TestNormalizeDefinitionV1RejectsMissingOrNullArrays(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(*SkillDefinitionV1)
	}{
		{name: "tags", field: "tags", mutate: func(definition *SkillDefinitionV1) { definition.Tags = nil }},
		{name: "examples", field: "examples", mutate: func(definition *SkillDefinitionV1) { definition.Examples = nil }},
		{name: "starter prompts", field: "starter_prompts", mutate: func(definition *SkillDefinitionV1) { definition.StarterPrompts = nil }},
		{name: "public tool refs", field: "public_tool_refs", mutate: func(definition *SkillDefinitionV1) { definition.PublicToolRefs = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validDefinitionForTest()
			test.mutate(&definition)
			_, err := NormalizeDefinitionV1(definition)
			var validation *ValidationError
			if !errors.As(err, &validation) || !hasDefinitionFieldError(validation.FieldErrors, test.field, "REQUIRED") {
				t.Fatalf("nil array %s must fail closed with REQUIRED: %v", test.field, err)
			}
		})
	}
}

func TestDefinitionFromCanonicalV1RejectsMissingOrNullArrays(t *testing.T) {
	canonical, _, err := CanonicalDefinitionV1(validDefinitionForTest())
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"null", "__missing__"} {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(canonical, &raw); err != nil {
			t.Fatal(err)
		}
		if replacement == "__missing__" {
			delete(raw, "public_tool_refs")
		} else {
			raw["public_tool_refs"] = json.RawMessage(replacement)
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := DefinitionFromCanonicalV1(encoded); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("persisted public_tool_refs=%s must fail closed, got %v", replacement, err)
		}
	}
}

func hasDefinitionFieldError(errorsFound []FieldError, field string, code string) bool {
	for _, item := range errorsFound {
		if item.Field == field && item.Code == code {
			return true
		}
	}
	return false
}

func TestNormalizeDefinitionV1FailsClosedForAnyNonNullCoverAssetID(t *testing.T) {
	definition := validDefinitionForTest()
	assetID := "019f0000-0000-7000-8000-000000000123"
	definition.MarketListing.CoverAssetID = &assetID
	_, err := NormalizeDefinitionV1(definition)
	var validation *ValidationError
	if !errors.As(err, &validation) || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("expected unavailable Asset reference error, got %v", err)
	}
	found := false
	for _, item := range validation.FieldErrors {
		if item.Field == "market_listing.cover_asset_id" && item.Code == "ASSET_REFERENCE_UNAVAILABLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing Asset fail-closed field error: %+v", validation.FieldErrors)
	}
}

func TestDefinitionFromCanonicalV1RejectsTrailingSecondJSONValue(t *testing.T) {
	definition, err := NormalizeDefinitionV1(validDefinitionForTest())
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := CanonicalDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	trailing := append(append([]byte(nil), canonical...), []byte(` {}`)...)
	if _, _, err := DefinitionFromCanonicalV1(trailing); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("expected trailing JSON rejection, got %v", err)
	}
}

func TestDefinitionFromCanonicalV1AcceptsPostgresJSONBKeyOrder(t *testing.T) {
	// PostgreSQL jsonb 读取文本时可能改变对象键顺序；摘要必须基于重新 Canonicalize 后的语义，而不是数据库文本顺序。
	definition, err := NormalizeDefinitionV1(validDefinitionForTest())
	if err != nil {
		t.Fatal(err)
	}
	canonical, expectedDigest, err := CanonicalDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, digest, err := DefinitionFromCanonicalV1(canonical); err != nil || digest != expectedDigest {
		t.Fatalf("canonical read failed: digest=%s err=%v", digest.Hex(), err)
	}
}

func TestDefinitionFromCanonicalV1ReturnsNormalizedDefinition(t *testing.T) {
	definition := validDefinitionForTest()
	definition.Name = "  Cafe\u0301  "
	definition.Tags = []string{"视频", "策划", "视频"}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := DefinitionFromCanonicalV1(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != "Café" || len(restored.Tags) != 2 || restored.Tags[0] != "策划" {
		t.Fatalf("database definition was not normalized: %+v", restored)
	}
}
