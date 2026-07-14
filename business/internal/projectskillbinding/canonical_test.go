package projectskillbinding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
)

const (
	testProjectID    = "019f0000-0000-7000-8000-0000000000ab"
	testOwnerID      = "019f0000-0000-7000-8000-0000000000cd"
	testSkillID      = "019f0000-0000-7000-8000-000000000101"
	testBindingID    = "019f0000-0000-7000-8000-000000000104"
	testSnapshotID   = "019f0000-0000-7000-8000-000000000103"
	testCommandID    = "019f0000-0000-7000-8000-000000000105"
	testResolutionID = "019f0000-0000-7000-8000-000000000106"
)

func TestProducerCanonicalGoldenVectors(t *testing.T) {
	selection := []BindingSelectionItemV1{{Priority: 100, Namespace: "user", SkillID: testSkillID}}
	selectionBytes, selectionDigest, err := CanonicalBindingSelectionV1(selection)
	if err != nil {
		t.Fatal(err)
	}
	const expectedSelection = `[{"priority":100,"namespace":"user","skill_id":"019f0000-0000-7000-8000-000000000101"}]`
	if string(selectionBytes) != expectedSelection || selectionDigest.Hex() != "0eafe12d92686ad70c9d55f8cf2963dfe12570a6e31e90a1f28931df7e3e96fd" {
		t.Fatalf("binding selection golden drift: bytes=%s digest=%s", selectionBytes, selectionDigest.Hex())
	}

	permission := PermissionSnapshotV1{
		SchemaVersion: PermissionSnapshotSchemaVersionV1, Decision: "allow", Basis: "owner_private",
		SubjectUserID: testOwnerID, ProjectID: testProjectID, ProjectOwnerUserID: testOwnerID,
		BindingID: testBindingID, BindingVersion: 1, BindingSetVersion: 1, Namespace: "user",
		SkillID: testSkillID, SkillOwnerUserID: testOwnerID, PublishedSnapshotID: testSnapshotID,
		AllowedActions: []string{"session_snapshot"}, PolicyRef: PermissionPolicyRefV1,
	}
	_, permissionDigest, err := CanonicalPermissionSnapshotV1(permission)
	if err != nil {
		t.Fatal(err)
	}
	if permissionDigest.Hex() != "785ae395603deae2c7daf8d183e27b2f2ca21c082a906cb1bab07b2e45c5280e" {
		t.Fatalf("permission golden drift: %s", permissionDigest.Hex())
	}

	runtimeContent, keys, runtimeBytes, runtimeDigest, err := ProjectRuntimeContentV1(runtimeGoldenDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "write_prompts" || runtimeDigest.Hex() != "d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168" {
		t.Fatalf("runtime golden drift: keys=%v digest=%s bytes=%s", keys, runtimeDigest.Hex(), runtimeBytes)
	}

	producerItem := PublishedSkillSnapshotRefV1{
		LoadOrder: 1, Priority: 100, Namespace: "user", SkillID: testSkillID, PublisherUserID: testOwnerID,
		PublishedSnapshotID: testSnapshotID, PublicationRevision: 2, DefinitionSchemaVersion: "skill_definition.v1",
		ContentDigest:               "dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513",
		RuntimeContentSchemaVersion: RuntimeContentSchemaVersionV1, RuntimeContentDigest: runtimeDigest.Hex(), RuntimeContent: runtimeContent,
		AllowedGraphToolKeys: []string{"write_prompts"}, PublicToolRefs: make([]PublicToolSnapshotRefV1, 0),
		PermissionSnapshotDigest: permissionDigest.Hex(), RuntimePolicyRef: RuntimePolicyRefV1,
		GovernanceEpoch: 3, PublishedAtUnixMS: 1784011500123,
	}
	_, snapshotDigest, err := CanonicalSnapshotSetV1([]PublishedSkillSnapshotRefV1{producerItem})
	if err != nil {
		t.Fatal(err)
	}
	if snapshotDigest.Hex() != "6242c4e449a2f2c9aba4880d7d3ea614b48e3fa652f38ab11be7cbde45e8c905" {
		t.Fatalf("producer snapshot golden drift: %s", snapshotDigest.Hex())
	}

	promptDigest := sha256.Sum256([]byte(" é "))
	_, quickDigest, err := CanonicalQuickCreateSemanticV2(true, promptDigest, selectionDigest)
	if err != nil {
		t.Fatal(err)
	}
	if quickDigest.Hex() != "3d2bc7c4c655457d1bcb3df25c31b7c65bf5ad3caad36d68ad1ce54a5b35bba7" {
		t.Fatalf("quick create semantic golden drift: %s", quickDigest.Hex())
	}
	snapshot := SessionSkillSnapshotV1{
		SchemaVersion: SessionSnapshotSchemaVersionV1, SnapshotKind: SnapshotKindPublishedRefs,
		SkillCount: 1, SnapshotSetDigest: snapshotDigest.Hex(), Skills: []PublishedSkillSnapshotRefV1{producerItem},
	}
	_, requestDigest, err := CanonicalEnsureRequestV2(testProjectID, testOwnerID, true, promptDigest, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if requestDigest.Hex() != "2a12a43e1a774216fb3828a9caac6ba55a6c1a02f5f77ec9addeeadf997c4091" {
		t.Fatalf("producer ensure request golden drift: %s", requestDigest.Hex())
	}
}

func TestResolveProjectSkillSnapshotsV1AndPrepareOutbox(t *testing.T) {
	definition := runtimeGoldenDefinition()
	definitionBytes, definitionDigest, err := skill.CanonicalDefinitionV1(definition)
	if err != nil {
		t.Fatal(err)
	}
	var contentDigest Digest
	copy(contentDigest[:], definitionDigest[:])
	selectionBytes, selectionDigest, err := CanonicalBindingSelectionV1([]BindingSelectionItemV1{{
		Priority: BindingPriorityW1, Namespace: SkillNamespaceUser, SkillID: testSkillID,
	}})
	if err != nil || len(selectionBytes) == 0 {
		t.Fatal(err)
	}
	resolvedAt := time.UnixMilli(1784011500123).UTC()
	row := PublishedSkillReadDTO{
		ProjectID: testProjectID, ProjectOwnerUserID: testOwnerID, ProjectLifecycleStatus: "active",
		BindingID: testBindingID, BindingVersion: 1, BindingStatus: "enabled", Namespace: "user", Priority: 100,
		SkillID: testSkillID, SkillOwnerUserID: testOwnerID, CurrentPublishedSnapshotID: testSnapshotID,
		SkillPublicationRevision: 2, GovernanceStatus: "active", GovernanceEpoch: 3,
		PublishedSnapshotID: testSnapshotID, PublishedSkillID: testSkillID,
		SourceContentRevisionID: "019f0000-0000-7000-8000-000000000107", PublishedPublicationRevision: 2,
		DefinitionSchemaVersion: skill.DefinitionSchemaVersionV1, DefinitionJSON: definitionBytes, ContentDigest: contentDigest,
		PublisherUserID: "019f0000-0000-7000-8000-000000000108", PublishedAt: resolvedAt,
		RevisionID: "019f0000-0000-7000-8000-000000000107", RevisionSkillID: testSkillID,
		RevisionDefinitionSchemaVersion: skill.DefinitionSchemaVersionV1,
		RevisionDefinitionJSON:          append([]byte(nil), definitionBytes...), RevisionContentDigest: contentDigest,
	}
	resolution, err := ResolveProjectSkillSnapshotsV1(ResolveInputV1{
		ResolutionID: testResolutionID, ProjectID: testProjectID, OwnerUserID: testOwnerID,
		CommandID: testCommandID, BindingSetVersion: 1, BindingSelectionDigest: selectionDigest, ResolvedAt: resolvedAt,
	}, []PublishedSkillReadDTO{row}, DefaultLimitsV1())
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Header.SkillCount != 1 || resolution.Snapshot.SnapshotKind != SnapshotKindPublishedRefs ||
		len(resolution.Items) != 1 || resolution.Items[0].PublisherUserID != testOwnerID ||
		len(resolution.Items[0].PublicToolRefs) != 0 || resolution.Items[0].PublicToolRefs == nil {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}

	command := QuickCreateV2Command{
		ProjectID: testProjectID, CommandID: testCommandID, ResolutionID: testResolutionID, OwnerUserID: testOwnerID,
		NormalizedPrompt: " é ", PromptDigest: sha256.Sum256([]byte(" é ")), PromptPresent: true,
		SelectionDigest: selectionDigest, OccurredAt: resolvedAt,
	}
	protector := &recordingProtector{}
	prepared, err := PrepareOutboxV2(context.Background(), command, resolution, DefaultLimitsV1(), protector)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.SkillCount != 1 || prepared.SnapshotDigest != resolution.Header.SnapshotSetDigest ||
		len(prepared.Envelope.Nonce) != 12 || bytes.Contains(prepared.Envelope.CiphertextAndTag, []byte("Prompt helper")) {
		t.Fatalf("unexpected prepared outbox: %+v", prepared)
	}
	if !bytes.Contains(protector.plaintext, []byte("Prompt helper")) || !bytes.Contains(protector.plaintext, []byte(" é ")) || len(protector.aad) == 0 {
		t.Fatal("protector did not receive complete canonical plaintext and AAD")
	}
	parsed, err := ParseCanonicalOutboxPayloadV2(protector.plaintext, OutboxExpectedV2{
		CommandID: command.CommandID, ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID,
		RequestDigest: prepared.RequestDigest, SnapshotDigest: prepared.SnapshotDigest,
		PayloadDigest: prepared.PayloadDigest, SkillCount: prepared.SkillCount,
	}, DefaultLimitsV1())
	if err != nil || parsed.RequestDigest != prepared.RequestDigest.Hex() || parsed.SkillSnapshot.SkillCount != 1 {
		t.Fatalf("strict outbox parse failed: payload=%+v err=%v", parsed, err)
	}
	unknownField := append([]byte(nil), protector.plaintext[:len(protector.plaintext)-1]...)
	unknownField = append(unknownField, []byte(`,"unknown":true}`)...)
	if _, err := ParseCanonicalOutboxPayloadV2(unknownField, OutboxExpectedV2{
		CommandID: command.CommandID, ProjectID: command.ProjectID, OwnerUserID: command.OwnerUserID,
		RequestDigest: prepared.RequestDigest, SnapshotDigest: prepared.SnapshotDigest,
		PayloadDigest: prepared.PayloadDigest, SkillCount: prepared.SkillCount,
	}, DefaultLimitsV1()); !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("unknown outbox field must fail closed: %v", err)
	}
}

func TestResolveProjectSkillSnapshotsV1FailsClosed(t *testing.T) {
	_, emptyDigest, err := CanonicalBindingSelectionV1(make([]BindingSelectionItemV1, 0))
	if err != nil {
		t.Fatal(err)
	}
	input := ResolveInputV1{
		ResolutionID: testResolutionID, ProjectID: testProjectID, OwnerUserID: testOwnerID,
		CommandID: testCommandID, BindingSetVersion: 1, BindingSelectionDigest: emptyDigest, ResolvedAt: time.Now().UTC(),
	}
	empty, err := ResolveProjectSkillSnapshotsV1(input, make([]PublishedSkillReadDTO, 0), DefaultLimitsV1())
	if err != nil || empty.Header.SkillCount != 0 || empty.Header.SnapshotSetDigest.Hex() != "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatalf("empty snapshot failed: result=%+v err=%v", empty, err)
	}
	if _, err := ResolveProjectSkillSnapshotsV1(input, nil, DefaultLimitsV1()); !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("nil rows must fail closed: %v", err)
	}
	content := runtimeGoldenDefinition()
	content.PublicToolRefs = []skill.PublicToolReferenceV1{skill.PublicToolReferenceV1(`{"ref":"blocked"}`)}
	if _, _, _, _, err := ProjectRuntimeContentV1(content); !errors.Is(err, ErrPublicToolUnavailable) {
		t.Fatalf("non-empty public refs must fail closed: %v", err)
	}
}

func TestNewQuickCreateV2CommandStrictSchemaAndSkillIDs(t *testing.T) {
	seed := QuickCreateV2Seed{
		SchemaVersion: QuickCreateSchemaVersionV2,
		ProjectID:     "019f0000-0000-7000-8000-000000000201", ReceiptID: "019f0000-0000-7000-8000-000000000202",
		SessionBindingID: "019f0000-0000-7000-8000-000000000203", CommandID: "019f0000-0000-7000-8000-000000000204",
		ResolutionID: "019f0000-0000-7000-8000-000000000205", OwnerUserID: "019f0000-0000-7000-8000-000000000206",
		InitialPrompt: " e\u0301 ", KeyDigest: SHA256Digest([]byte("quick-v2-strict-key")),
		Bindings: []BindingSeed{
			{ID: "019f0000-0000-7000-8000-000000000207", SkillID: "019f0000-0000-7000-8000-000000000209", AuditID: "019f0000-0000-7000-8000-000000000208"},
			{ID: "019f0000-0000-7000-8000-000000000210", SkillID: "019f0000-0000-7000-8000-000000000101", AuditID: "019f0000-0000-7000-8000-000000000211"},
		},
		MaxAttempts: 5, OccurredAt: time.UnixMilli(1784011500123).UTC(),
	}
	command, err := NewQuickCreateV2Command(seed, DefaultLimitsV1())
	if err != nil {
		t.Fatal(err)
	}
	if command.NormalizedPrompt != " é " || len(command.Bindings) != 2 ||
		command.Bindings[0].SkillID != "019f0000-0000-7000-8000-000000000101" ||
		command.Bindings[1].SkillID != "019f0000-0000-7000-8000-000000000209" {
		t.Fatalf("v2 command did not normalize prompt and sort skill IDs: %+v", command)
	}
	if err := command.Validate(DefaultLimitsV1()); err != nil {
		t.Fatal(err)
	}
	nilBindings := seed
	nilBindings.Bindings = nil
	if _, err := NewQuickCreateV2Command(nilBindings, DefaultLimitsV1()); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("nil enabled_skill_ids must fail: %v", err)
	}
	duplicate := seed
	duplicate.Bindings = append([]BindingSeed(nil), seed.Bindings...)
	duplicate.Bindings[1].SkillID = duplicate.Bindings[0].SkillID
	if _, err := NewQuickCreateV2Command(duplicate, DefaultLimitsV1()); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("duplicate skill ID must fail: %v", err)
	}
	unknownSchema := seed
	unknownSchema.SchemaVersion = "project_quick_create.v3"
	if _, err := NewQuickCreateV2Command(unknownSchema, DefaultLimitsV1()); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("unknown quick create schema must fail: %v", err)
	}
}

func runtimeGoldenDefinition() skill.SkillDefinitionV1 {
	notApplicable := skill.CapabilityGuidanceV1{Applicability: "not_applicable", NotApplicableReason: "not used"}
	return skill.SkillDefinitionV1{
		SchemaVersion: skill.DefinitionSchemaVersionV1, Name: "Prompt helper", Tags: make([]string, 0),
		InputDescription: "text", OutputDescription: "prompt", InvocationRules: "Use for prompt writing.",
		PlanCreationSpec: notApplicable, AnalyzeMaterials: notApplicable, PlanStoryboard: notApplicable,
		GenerateMedia:  notApplicable,
		WritePrompts:   skill.CapabilityGuidanceV1{Applicability: "enabled", Guidance: "Write concise prompts."},
		AssembleOutput: notApplicable, Examples: make([]skill.SkillExampleV1, 0),
		StarterPrompts: []string{"Improve this prompt."}, PublicToolRefs: make([]skill.PublicToolReferenceV1, 0),
	}
}

type recordingProtector struct {
	plaintext []byte
	aad       []byte
}

func (protector *recordingProtector) Protect(_ context.Context, plaintext []byte, aad []byte) (EncryptedEnvelopeV2, error) {
	protector.plaintext = append([]byte(nil), plaintext...)
	protector.aad = append([]byte(nil), aad...)
	return EncryptedEnvelopeV2{
		Algorithm: OutboxEncryptionAlgorithm, KeyVersion: "bootstrap-v2-test",
		Nonce: bytes.Repeat([]byte{1}, 12), CiphertextAndTag: bytes.Repeat([]byte{2}, 32),
	}, nil
}
