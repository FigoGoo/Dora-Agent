package skill

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const (
	fixtureProjectID       = "019f0000-0000-7000-8000-0000000000ab"
	fixtureOwnerID         = "019f0000-0000-7000-8000-0000000000cd"
	fixtureRuntimeDigest   = "d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168"
	fixtureOpaqueSetDigest = "69ef1ba7ca41c90986204308043cb4587097ce3d4edbcea921b00eafc7cdfcdc"
	fixtureProducerDigest  = "6242c4e449a2f2c9aba4880d7d3ea614b48e3fa652f38ab11be7cbde45e8c905"
)

// fixtureRuntimeContent 返回 Snapshot v2 设计冻结的语义 fixture，不从 canonical JSON 反序列化。
func fixtureRuntimeContent() SkillRuntimeContentV1 {
	notApplicable := CapabilityGuidanceV1{
		Applicability:       SkillGuidanceNotApplicableV1,
		NotApplicableReason: "not used",
	}
	return SkillRuntimeContentV1{
		SchemaVersion:     RuntimeContentSchemaVersionV1,
		Name:              "Prompt helper",
		InputDescription:  "text",
		OutputDescription: "prompt",
		InvocationRules:   "Use for prompt writing.",
		PlanCreationSpec:  notApplicable,
		AnalyzeMaterials:  notApplicable,
		PlanStoryboard:    notApplicable,
		GenerateMedia:     notApplicable,
		WritePrompts: CapabilityGuidanceV1{
			Applicability: SkillGuidanceEnabledV1,
			Guidance:      "Write concise prompts.",
		},
		AssembleOutput: notApplicable,
		Examples:       []SkillExampleV1{},
		StarterPrompts: []string{"Improve this prompt."},
	}
}

// fixtureSnapshotItem 返回可替换发布者与权限摘要的固定 Snapshot Item。
func fixtureSnapshotItem(publisherID, permissionDigest string) PublishedSkillSnapshotRefV1 {
	return PublishedSkillSnapshotRefV1{
		LoadOrder: 1, Priority: 100, Namespace: SkillNamespaceUserV1,
		SkillID:                     "019f0000-0000-7000-8000-000000000101",
		PublisherUserID:             publisherID,
		PublishedSnapshotID:         "019f0000-0000-7000-8000-000000000103",
		PublicationRevision:         2,
		DefinitionSchemaVersion:     DefinitionSchemaVersionV1,
		ContentDigest:               "dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513",
		RuntimeContentSchemaVersion: RuntimeContentSchemaVersionV1,
		RuntimeContentDigest:        fixtureRuntimeDigest,
		RuntimeContent:              fixtureRuntimeContent(),
		AllowedGraphToolKeys:        []string{"write_prompts"},
		PublicToolRefs:              []PublicToolSnapshotRefV1{},
		PermissionSnapshotDigest:    permissionDigest,
		RuntimePolicyRef:            RuntimePolicyRefV1,
		GovernanceEpoch:             3,
		PublishedAtUnixMS:           1784011500123,
	}
}

// fixtureSnapshot 返回带调用方声明 digest 的固定 Snapshot Set。
func fixtureSnapshot(item PublishedSkillSnapshotRefV1, setDigest string) SessionSkillSnapshotV1 {
	return SessionSkillSnapshotV1{
		SchemaVersion:     SnapshotSchemaVersionV1,
		SnapshotKind:      SessionSkillSnapshotKindPublishedRefsV1,
		SkillCount:        1,
		SnapshotSetDigest: setDigest,
		Skills:            []PublishedSkillSnapshotRefV1{item},
	}
}

// emptySnapshot 返回非 nil 空数组和 W0 冻结 digest，供 V2 empty 语义复用。
func emptySnapshot() SessionSkillSnapshotV1 {
	return SessionSkillSnapshotV1{
		SchemaVersion:     SnapshotSchemaVersionV1,
		SnapshotKind:      SessionSkillSnapshotKindEmptyV1,
		SkillCount:        0,
		SnapshotSetDigest: EmptySnapshotSetDigestHex,
		Skills:            []PublishedSkillSnapshotRefV1{},
	}
}

func TestGoldenRuntimeContentV1(t *testing.T) {
	wantCanonical := `{"schema_version":"skill_runtime_content.v1","name":"Prompt helper","input_description":"text","output_description":"prompt","invocation_rules":"Use for prompt writing.","plan_creation_spec":{"applicability":"not_applicable","guidance":"","not_applicable_reason":"not used"},"analyze_materials":{"applicability":"not_applicable","guidance":"","not_applicable_reason":"not used"},"plan_storyboard":{"applicability":"not_applicable","guidance":"","not_applicable_reason":"not used"},"generate_media":{"applicability":"not_applicable","guidance":"","not_applicable_reason":"not used"},"write_prompts":{"applicability":"enabled","guidance":"Write concise prompts.","not_applicable_reason":""},"assemble_output":{"applicability":"not_applicable","guidance":"","not_applicable_reason":"not used"},"examples":[],"starter_prompts":["Improve this prompt."]}`
	_, canonical, digest, err := CanonicalRuntimeContentV1(fixtureRuntimeContent(), DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != wantCanonical || digest.Hex() != fixtureRuntimeDigest {
		t.Fatalf("runtime golden changed:\ncanonical=%s\ndigest=%s", canonical, digest.Hex())
	}
	parsed, parsedDigest, err := ParseCanonicalRuntimeContentV1(canonical, DefaultLimitsProfileV1())
	if err != nil || parsed.Name != "Prompt helper" || parsedDigest != digest {
		t.Fatalf("strict canonical parse failed: parsed=%+v digest=%s err=%v", parsed, parsedDigest.Hex(), err)
	}
}

func TestGoldenOpaquePermissionSnapshotSetV1(t *testing.T) {
	wantCanonical := `[{"load_order":1,"priority":100,"namespace":"user","skill_id":"019f0000-0000-7000-8000-000000000101","publisher_user_id":"019f0000-0000-7000-8000-000000000102","published_snapshot_id":"019f0000-0000-7000-8000-000000000103","publication_revision":2,"definition_schema_version":"skill_definition.v1","content_digest":"dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513","runtime_content_schema_version":"skill_runtime_content.v1","runtime_content_digest":"d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168","allowed_graph_tool_keys":["write_prompts"],"public_tool_refs":[],"permission_snapshot_digest":"3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25","runtime_policy_ref":"skill-runtime-policy:v1","governance_epoch":3,"published_at_unix_ms":1784011500123}]`
	snapshot := fixtureSnapshot(
		fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25"),
		fixtureOpaqueSetDigest,
	)
	_, canonical, digest, err := CanonicalSnapshotSetV1(snapshot, DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != wantCanonical || digest.Hex() != fixtureOpaqueSetDigest {
		t.Fatalf("opaque snapshot golden changed:\ncanonical=%s\ndigest=%s", canonical, digest.Hex())
	}
}

func TestGoldenEmptyAndNonEmptyEnsureProjectSessionV2(t *testing.T) {
	tests := []struct {
		name          string
		snapshot      SessionSkillSnapshotV1
		wantCanonical string
		wantDigest    string
	}{
		{
			name:          "empty",
			snapshot:      emptySnapshot(),
			wantCanonical: `{"schema_version":"ensure_project_session.v2","project_id":"019f0000-0000-7000-8000-0000000000ab","owner_user_id":"019f0000-0000-7000-8000-0000000000cd","creation_source":"quick_create","prompt_present":true,"prompt_digest":"273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df","skill_snapshot_schema_version":"session_skill_snapshot.v1","skill_snapshot_kind":"empty","skill_count":0,"skill_snapshot_digest":"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"}`,
			wantDigest:    "904b88d91a452522b95b0925e61ac94d93e89def4af29944ff563a4ff9ffc1b5",
		},
		{
			name: "published refs",
			snapshot: fixtureSnapshot(
				fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25"),
				fixtureOpaqueSetDigest,
			),
			wantCanonical: `{"schema_version":"ensure_project_session.v2","project_id":"019f0000-0000-7000-8000-0000000000ab","owner_user_id":"019f0000-0000-7000-8000-0000000000cd","creation_source":"quick_create","prompt_present":true,"prompt_digest":"273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df","skill_snapshot_schema_version":"session_skill_snapshot.v1","skill_snapshot_kind":"published_refs","skill_count":1,"skill_snapshot_digest":"69ef1ba7ca41c90986204308043cb4587097ce3d4edbcea921b00eafc7cdfcdc"}`,
			wantDigest:    "2dcc22f80c546ff992c2f3d82a9252adc338deb1d4805b14f9477f66bdab52f1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := CanonicalEnsureProjectSessionV2(EnsureProjectSessionInputV2{
				SchemaVersion: EnsureProjectSessionSchemaVersionV2,
				ProjectID:     fixtureProjectID, OwnerUserID: fixtureOwnerID,
				CreationSource: CreationSourceQuickCreate,
				InitialPrompt:  " e\u0301 ", SkillSnapshot: test.snapshot,
			}, DefaultLimitsProfileV1())
			if err != nil {
				t.Fatal(err)
			}
			if string(result.CanonicalJSON) != test.wantCanonical || result.RequestDigest.Hex() != test.wantDigest ||
				result.PromptDigest != "273f7787225c057d3b40cecfdad67cefd35e4b0fa95eacff5668011fc44497df" || result.NormalizedPrompt != " é " {
				t.Fatalf("ensure golden changed: result=%+v canonical=%s", result, result.CanonicalJSON)
			}
		})
	}
}

func TestGoldenBusinessProductionSnapshotV1(t *testing.T) {
	snapshot := fixtureSnapshot(
		fixtureSnapshotItem(fixtureOwnerID, "785ae395603deae2c7daf8d183e27b2f2ca21c082a906cb1bab07b2e45c5280e"),
		fixtureProducerDigest,
	)
	_, _, digest, err := CanonicalSnapshotSetV1(snapshot, DefaultLimitsProfileV1())
	if err != nil || digest.Hex() != fixtureProducerDigest {
		t.Fatalf("production snapshot golden changed: digest=%s err=%v", digest.Hex(), err)
	}
	result, err := CanonicalEnsureProjectSessionV2(EnsureProjectSessionInputV2{
		SchemaVersion: EnsureProjectSessionSchemaVersionV2,
		ProjectID:     fixtureProjectID, OwnerUserID: fixtureOwnerID,
		CreationSource: CreationSourceQuickCreate,
		InitialPrompt:  " e\u0301 ", SkillSnapshot: snapshot,
	}, DefaultLimitsProfileV1())
	if err != nil || result.RequestDigest.Hex() != "2a12a43e1a774216fb3828a9caac6ba55a6c1a02f5f77ec9addeeadf997c4091" {
		t.Fatalf("production request golden changed: digest=%s err=%v", result.RequestDigest.Hex(), err)
	}
}

func TestCanonicalRejectsNilListsAndUnknownEnums(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SessionSkillSnapshotV1)
	}{
		{"nil skills", func(snapshot *SessionSkillSnapshotV1) { snapshot.Skills = nil }},
		{"unknown snapshot kind", func(snapshot *SessionSkillSnapshotV1) { snapshot.SnapshotKind = "future" }},
		{"nil allowed keys", func(snapshot *SessionSkillSnapshotV1) { snapshot.Skills[0].AllowedGraphToolKeys = nil }},
		{"nil public refs", func(snapshot *SessionSkillSnapshotV1) { snapshot.Skills[0].PublicToolRefs = nil }},
		{"unknown namespace", func(snapshot *SessionSkillSnapshotV1) { snapshot.Skills[0].Namespace = "future" }},
		{"unknown guidance enum", func(snapshot *SessionSkillSnapshotV1) {
			snapshot.Skills[0].RuntimeContent.WritePrompts.Applicability = "future"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := fixtureSnapshot(
				fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25"),
				fixtureOpaqueSetDigest,
			)
			test.mutate(&snapshot)
			if _, _, _, err := CanonicalSnapshotSetV1(snapshot, DefaultLimitsProfileV1()); err == nil {
				t.Fatal("expected fail-closed validation")
			}
		})
	}
	content := fixtureRuntimeContent()
	content.Examples = nil
	if _, _, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1()); err == nil {
		t.Fatal("nil examples must fail")
	}
	content = fixtureRuntimeContent()
	content.StarterPrompts = nil
	if _, _, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1()); err == nil {
		t.Fatal("nil starter_prompts must fail")
	}
}

func TestCanonicalNFCArrayOrderAndHTMLEscape(t *testing.T) {
	content := fixtureRuntimeContent()
	content.Name = "  Cafe\u0301 <&>  "
	normalized, canonical, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Name != "Café <&>" || !bytes.Contains(canonical, []byte(`"name":"Café <&>"`)) || bytes.Contains(canonical, []byte(`\u003c`)) {
		t.Fatalf("NFC or HTML escape contract changed: normalized=%q canonical=%s", normalized.Name, canonical)
	}

	content = fixtureRuntimeContent()
	content.Examples = []SkillExampleV1{{Input: "b", Output: "x"}, {Input: "a", Output: "x"}}
	if _, _, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1()); err == nil {
		t.Fatal("unsorted examples must fail instead of being silently reordered")
	}
	content = fixtureRuntimeContent()
	content.StarterPrompts = []string{"z", "a"}
	if _, _, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1()); err == nil {
		t.Fatal("unsorted starter prompts must fail")
	}

	snapshot := fixtureSnapshot(
		fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25"),
		fixtureOpaqueSetDigest,
	)
	snapshot.Skills[0].RuntimeContent.PlanCreationSpec = CapabilityGuidanceV1{Applicability: SkillGuidanceEnabledV1, Guidance: "plan"}
	snapshot.Skills[0].AllowedGraphToolKeys = []string{"write_prompts", "plan_creation_spec"}
	if _, _, _, err := CanonicalSnapshotSetV1(snapshot, DefaultLimitsProfileV1()); err == nil {
		t.Fatal("allowed graph keys must follow product order and match enabled guidance")
	}
}

func TestCanonicalRejectsIntegerBoundariesAndDigestTampering(t *testing.T) {
	mutations := []func(*PublishedSkillSnapshotRefV1){
		func(item *PublishedSkillSnapshotRefV1) { item.LoadOrder = 0 },
		func(item *PublishedSkillSnapshotRefV1) { item.Priority = -1 },
		func(item *PublishedSkillSnapshotRefV1) { item.PublicationRevision = 0 },
		func(item *PublishedSkillSnapshotRefV1) { item.GovernanceEpoch = -1 },
		func(item *PublishedSkillSnapshotRefV1) { item.PublishedAtUnixMS = 0 },
	}
	for index, mutate := range mutations {
		item := fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25")
		mutate(&item)
		if _, _, _, err := CanonicalSnapshotSetV1(fixtureSnapshot(item, fixtureOpaqueSetDigest), DefaultLimitsProfileV1()); err == nil {
			t.Fatalf("integer mutation %d must fail", index)
		}
	}
	item := fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25")
	item.RuntimeContentDigest = strings.Repeat("0", 64)
	if _, _, _, err := CanonicalSnapshotSetV1(fixtureSnapshot(item, fixtureOpaqueSetDigest), DefaultLimitsProfileV1()); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("runtime digest tamper must be classified: %v", err)
	}
	snapshot := fixtureSnapshot(
		fixtureSnapshotItem("019f0000-0000-7000-8000-000000000102", "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25"),
		strings.Repeat("0", 64),
	)
	if _, _, _, err := CanonicalSnapshotSetV1(snapshot, DefaultLimitsProfileV1()); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("set digest tamper must be classified: %v", err)
	}
}

func TestLimitsProfileV1AndBoundaryRejection(t *testing.T) {
	if err := DefaultLimitsProfileV1().Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := ProtocolCeilingsV1()
	invalid.MaxItems++
	if err := invalid.Validate(); err == nil {
		t.Fatal("protocol ceiling +1 must fail")
	}
	producer := DefaultLimitsProfileV1()
	consumer := DefaultLimitsProfileV1()
	consumer.MaxItems--
	if err := ValidateProducerLimitsV1(producer, consumer); err == nil {
		t.Fatal("producer limits larger than consumer must fail rollout validation")
	}
	content := fixtureRuntimeContent()
	content.Name = strings.Repeat("a", maxRuntimeNameBytes+1)
	if _, _, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("name boundary +1 must be a limit error: %v", err)
	}
	content = fixtureRuntimeContent()
	content.Examples = make([]SkillExampleV1, DefaultLimitsProfileV1().MaxExamplesPerItem+1)
	if _, _, _, err := CanonicalRuntimeContentV1(content, DefaultLimitsProfileV1()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("example count +1 must be a limit error: %v", err)
	}
	tooMany := emptySnapshot()
	tooMany.SnapshotKind = SessionSkillSnapshotKindPublishedRefsV1
	tooMany.SkillCount = int32(DefaultLimitsProfileV1().MaxItems + 1)
	tooMany.Skills = make([]PublishedSkillSnapshotRefV1, tooMany.SkillCount)
	if _, _, _, err := CanonicalSnapshotSetV1(tooMany, DefaultLimitsProfileV1()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("skill count +1 must be a limit error: %v", err)
	}
}

func TestParseCanonicalRuntimeContentV1RejectsAliasesAndTrailingData(t *testing.T) {
	_, canonical, _, err := CanonicalRuntimeContentV1(fixtureRuntimeContent(), DefaultLimitsProfileV1())
	if err != nil {
		t.Fatal(err)
	}
	inputs := [][]byte{
		append(append([]byte(nil), canonical...), []byte(` {}`)...),
		bytes.Replace(canonical, []byte(`"name":"Prompt helper"`), []byte(`"name":"Prompt \u0068elper"`), 1),
		[]byte(`null`),
		bytes.Replace(canonical, []byte(`"starter_prompts"`), []byte(`"unknown"`), 1),
	}
	for index, input := range inputs {
		if _, _, err := ParseCanonicalRuntimeContentV1(input, DefaultLimitsProfileV1()); err == nil {
			t.Fatalf("non-canonical input %d must fail", index)
		}
	}
}

func TestEnsureProjectSessionV2PromptAbsentAndUUIDFailClosed(t *testing.T) {
	result, err := CanonicalEnsureProjectSessionV2(EnsureProjectSessionInputV2{
		SchemaVersion: EnsureProjectSessionSchemaVersionV2,
		ProjectID:     fixtureProjectID, OwnerUserID: fixtureOwnerID,
		CreationSource: CreationSourceQuickCreate,
		InitialPrompt:  "\u3000\t", SkillSnapshot: emptySnapshot(),
	}, DefaultLimitsProfileV1())
	if err != nil || result.PromptPresent || result.PromptDigest != "" || result.NormalizedPrompt != "" {
		t.Fatalf("unicode whitespace must collapse to absent: result=%+v err=%v", result, err)
	}
	_, err = CanonicalEnsureProjectSessionV2(EnsureProjectSessionInputV2{
		SchemaVersion: EnsureProjectSessionSchemaVersionV2,
		ProjectID:     strings.ToUpper(fixtureProjectID), OwnerUserID: fixtureOwnerID,
		CreationSource: CreationSourceQuickCreate,
		SkillSnapshot:  emptySnapshot(),
	}, DefaultLimitsProfileV1())
	if err == nil {
		t.Fatal("non-canonical uppercase UUID must fail")
	}
}
