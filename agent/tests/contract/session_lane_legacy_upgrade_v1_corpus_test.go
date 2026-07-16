// Package contract_test 只承载 legacy Session Lane 升级候选契约，不提供生产 Migration、Helper 或 Runtime。
package contract_test

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	agentsession "github.com/FigoGoo/Dora-Agent/agent/internal/session"
	agentskill "github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

const (
	legacyUpgradeManifestPath   = "testdata/w2_r02_upgrade/manifest.json"
	legacyAuthorityCorpusPath   = "testdata/w2_r02_upgrade/legacy_authority_attestation_v1.json"
	legacyBlockerCorpusPath     = "testdata/w2_r02_upgrade/session_lane_upgrade_blocker_v1.json"
	legacyAuthorityDigestDomain = "dora.legacy_ensure_receipt_attestation.v1"
)

//go:embed testdata/w2_r02_upgrade/*.json
var w2R02LegacyUpgradeFS embed.FS

type legacyUpgradeManifestV1 struct {
	SchemaVersion    string                        `json:"schema_version"`
	Files            []legacyUpgradeManifestFileV1 `json:"files"`
	TotalVectorCount int                           `json:"total_vector_count"`
	TargetTests      []string                      `json:"target_tests"`
}

type legacyUpgradeManifestFileV1 struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type legacyAuthorityCorpusV1 struct {
	SchemaVersion string                     `json:"schema_version"`
	ExactSets     legacyAuthorityExactSetsV1 `json:"exact_sets"`
	Fixtures      []legacyAuthorityFixtureV1 `json:"fixtures"`
	Cases         []legacyAuthorityCaseV1    `json:"cases"`
}

type legacyAuthorityExactSetsV1 struct {
	Modes                  []string `json:"modes"`
	Decisions              []string `json:"decisions"`
	AuthorityKinds         []string `json:"authority_kinds"`
	EvidenceLevels         []string `json:"evidence_levels"`
	SourceContractVersions []string `json:"source_contract_versions"`
	CapabilityPolicies     []string `json:"capability_policies"`
	ReasonCodes            []string `json:"reason_codes"`
}

type legacyAuthorityFixtureV1 struct {
	FixtureID           string                       `json:"fixture_id"`
	Canonical           legacyAuthorityCanonicalV1   `json:"canonical"`
	InitialPrompt       string                       `json:"initial_prompt"`
	ReceiptImmutable    bool                         `json:"receipt_immutable"`
	SessionBindingValid bool                         `json:"session_binding_valid"`
	InputBindingValid   bool                         `json:"input_binding_valid"`
	SkillBindingValid   bool                         `json:"skill_binding_valid"`
	OperationalMetadata legacyAuthorityOperationalV1 `json:"operational_metadata"`
}

type legacyAuthorityCanonicalV1 struct {
	SchemaVersion              string `json:"schema_version"`
	AuthorityKind              string `json:"authority_kind"`
	EvidenceLevel              string `json:"evidence_level"`
	SourceContractVersion      string `json:"source_contract_version"`
	SourceCommandID            string `json:"source_command_id"`
	SourceCommandType          string `json:"source_command_type"`
	SourceRequestDigest        string `json:"source_request_digest"`
	SessionID                  string `json:"session_id"`
	ProjectID                  string `json:"project_id"`
	OwnerUserID                string `json:"owner_user_id"`
	MessageID                  string `json:"message_id"`
	InputID                    string `json:"input_id"`
	ContentDigest              string `json:"content_digest"`
	ResultVersion              int64  `json:"result_version"`
	SkillSnapshotSchemaVersion string `json:"skill_snapshot_schema_version"`
	SkillSnapshotKind          string `json:"skill_snapshot_kind"`
	SkillSnapshotDigest        string `json:"skill_snapshot_digest"`
	SkillCount                 int64  `json:"skill_count"`
	ReceiptCompletedAtUnixUS   int64  `json:"receipt_completed_at_unix_us"`
	ExecutionClass             string `json:"execution_class"`
	CapabilityPolicy           string `json:"capability_policy"`
}

type legacyAuthorityOperationalV1 struct {
	AttestationID    string `json:"attestation_id"`
	MigrationRunID   string `json:"migration_run_id"`
	Attempt          int64  `json:"attempt"`
	AttestedAtUnixUS int64  `json:"attested_at_unix_us"`
}

type legacyAuthorityCaseV1 struct {
	ID                    string                    `json:"id"`
	Mode                  string                    `json:"mode"`
	FromFixture           string                    `json:"from_fixture"`
	Mutations             []string                  `json:"mutations"`
	StoredAuthorityDigest string                    `json:"stored_authority_digest"`
	Expected              legacyAuthorityExpectedV1 `json:"expected"`
}

type legacyAuthorityExpectedV1 struct {
	Decision        string   `json:"decision"`
	ReasonCodes     []string `json:"reason_codes"`
	AuthorityDigest string   `json:"authority_digest"`
}

type legacyBlockerCorpusV1 struct {
	SchemaVersion string                   `json:"schema_version"`
	ExactSets     legacyBlockerExactSetsV1 `json:"exact_sets"`
	Cases         []legacyBlockerCaseV1    `json:"cases"`
}

type legacyBlockerExactSetsV1 struct {
	Phases        []string `json:"phases"`
	Scopes        []string `json:"scopes"`
	Decisions     []string `json:"decisions"`
	ContentStates []string `json:"content_states"`
	ReasonCodes   []string `json:"reason_codes"`
}

type legacyBlockerCaseV1 struct {
	ID        string                  `json:"id"`
	Phase     string                  `json:"phase"`
	Scope     string                  `json:"scope"`
	Baseline  string                  `json:"baseline"`
	Mutations []string                `json:"mutations"`
	Expected  legacyBlockerExpectedV1 `json:"expected"`
}

type legacyBlockerExpectedV1 struct {
	Decision         string   `json:"decision"`
	ReasonCodes      []string `json:"reason_codes"`
	AttestationCount int      `json:"attestation_count"`
	TurnCount        int      `json:"turn_count"`
	RunCount         int      `json:"run_count"`
	RowVerified      bool     `json:"row_verified"`
}

type legacyUpgradeSnapshotV1 struct {
	SessionPresent, SessionActive, SessionVersionExpected  bool
	SessionReceiptPresent                                  bool
	SkillSnapshotPresent, SkillSnapshotValid               bool
	SkillItemCountMatches, SkillItemOrderDense             bool
	SequenceCounterPresent, MessageSequenceDense           bool
	InputSequenceDense                                     bool
	EventCounterPresent, EventSequenceDense                bool
	EventMinAvailableSeq, EventLastSeq                     int64
	CreatedEventPresent, CreatedEventMatches               bool
	CreatedEventSeq                                        int64
	RuntimeLeasePresent, RuntimeLeasePristine              bool
	ReceiptMessageTargetPresent, ReceiptInputTargetPresent bool
	UnclaimedMessageCount, UnclaimedInputCount             int
	UnclaimedEventCount                                    int
	InputPresent                                           bool
	InputStatus                                            string
	InputAttempts                                          int64
	InputLeasePresent                                      bool
	InputFence                                             int64
	InputSource                                            string
	InputMessageRefPresent                                 bool
	MessagePresent, MessageSessionMatches                  bool
	MessageSourceMatches, MessageRoleSupported             bool
	MessageEnvelopeValid, MessageDigestValid               bool
	MessageContentState                                    string
	ReceiptPresent, ReceiptTypeSupported                   bool
	ReceiptResultVersionMatches, ReceiptSessionMatches     bool
	ReceiptMessageMatches, ReceiptInputMatches             bool
	ReceiptDigestValid, EnsureCanonicalDigestMatches       bool
	ReceiptSkillMatches, ReceiptImmutable                  bool
	AcceptedEventCount                                     int
	AcceptedEventMatches                                   bool
	AcceptedEventSeq                                       int64
	SkillRuntimeContentState                               string
	AuthorityAttestationCount                              int
	AuthorityAttestationMatches, AuthorityPolicyAllowed    bool
	TurnCount                                              int
	TurnMatches                                            bool
	RunCount                                               int
	UpgradeLedgerCount                                     int
	UpgradeLedgerMatches                                   bool
	OrphanReceiptCount, OrphanMessageCount                 int
	OrphanInputCount, OrphanEventCount                     int
	OrphanSkillSnapshotHeaderCount                         int
	OrphanSkillSnapshotItemCount                           int
	OrphanSequenceCounterCount                             int
	OrphanEventCounterCount                                int
	OrphanRuntimeLeaseCount                                int
	OrphanAuthorityCount                                   int
	OrphanUpgradeLedgerCount                               int
	OrphanTurnCount, OrphanRunCount                        int
	OrphanEnqueueResultCount                               int
}

type legacyBlockerResultV1 struct {
	Decision         string
	ReasonCodes      []string
	AttestationCount int
	TurnCount        int
	RunCount         int
	RowVerified      bool
}

type legacyLaneReadinessV1 struct {
	FoundationReady              bool
	LaneSchemaReady              bool
	CompatibleWriterReady        bool
	LegacyClassificationComplete bool
	LegacyBlockerCount           int
	EligibleUnverifiedCount      int
	ActiveUpgradeClaims          int
	ActivationPoliciesReady      bool
	UpgradeEvidenceApproved      bool
	CapabilityState              string
	ProcessorEnabled             bool
	UpgradeGeneration            int64
	ClaimGeneration              int64
}

type legacyLaneReadinessResultV1 struct {
	FoundationReady     bool
	LaneCapabilityReady bool
	ProcessorReady      bool
	ClaimAllowed        bool
}

type legacyUpgradeLedgerV1 struct {
	LedgerKey              string
	UpgradeGeneration      int64
	State                  string
	FactsDigest            string
	PlanDigest             string
	TargetInputDigest      string
	TurnContextDigest      string
	AuthorityAttestationID string
	AuthorityDigest        string
	TurnID                 string
	ContextMessageSeq      int64
	LockedLastMessageSeq   int64
}

type legacyUpgradeAppliedV1 struct {
	InputPatched           bool
	LedgerKey              string
	UpgradeGeneration      int64
	LedgerState            string
	FactsDigest            string
	PlanDigest             string
	TargetInputDigest      string
	TurnContextDigest      string
	AuthorityAttestationID string
	AuthorityDigest        string
	TurnID                 string
	ContextMessageSeq      int64
	RunCount               int
}

type legacyDownFactsV1 struct {
	CompatibleWritersStopped bool
	ProcessorStopped         bool
	ScannerStopped           bool
	MigrationFenceHeld       bool
	EnqueueHeaderCount       int
	AliasReceiptCount        int
	OriginResultCount        int
	AuthoritySnapshotCount   int
	LegacyTurnCount          int
	RunCount                 int
	NewStateFactCount        int
	ExpandedFactCount        int
	TurnContextCount         int
	UpgradeLedgerCount       int
}

func TestW2R02LegacyUpgradeCorpusManifest(t *testing.T) {
	entries, err := w2R02LegacyUpgradeFS.ReadDir("testdata/w2_r02_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"legacy_authority_attestation_v1.json", "manifest.json", "session_lane_upgrade_blocker_v1.json"}
	if len(entries) != len(wantNames) {
		t.Fatalf("legacy upgrade Corpus 文件数=%d want=%d", len(entries), len(wantNames))
	}
	for index, entry := range entries {
		if entry.Name() != wantNames[index] {
			t.Fatalf("legacy upgrade Corpus 文件[%d]=%q want=%q", index, entry.Name(), wantNames[index])
		}
	}
	manifest := loadLegacyManifestV1(t)
	wantTests := []string{
		"TestW2R02LegacyUpgradeCorpusManifest", "TestLegacyAuthorityAttestationV1Corpus",
		"TestLegacyAuthorityAttestationV1GoldenDigests", "TestLegacyAuthorityAttestationV1CanonicalFields",
		"TestLegacyAuthorityAttestationV1RejectsInvalidCanonicalShape",
		"TestSessionLaneUpgradeBlockerV1Corpus", "TestSessionLaneUpgradeBlockerV1ExactSets",
		"TestSessionLaneUpgradeBlockerV1ReasonOrder", "TestSessionLaneUpgradeBlockerV1RootedAntiJoin",
		"TestLegacyUpgradeClassificationRejectsUnknownDomain", "TestLegacyEventRetentionV1FailsClosed",
		"TestLegacyUpgradeLedgerV1CrashRecovery", "TestLegacyLaneReadinessV1SeparatesFoundation",
		"TestLegacyUpgradeDownGuardV1",
	}
	if manifest.SchemaVersion != "w2_r02_legacy_upgrade_manifest.v1" || manifest.TotalVectorCount != 107 || !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("legacy upgrade manifest Header 错误: %+v", manifest)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].File != "legacy_authority_attestation_v1.json" || manifest.Files[0].VectorCount != 17 ||
		manifest.Files[1].File != "session_lane_upgrade_blocker_v1.json" || manifest.Files[1].VectorCount != 90 {
		t.Fatalf("legacy upgrade manifest files 错误: %+v", manifest.Files)
	}
	for _, item := range manifest.Files {
		if len(item.SHA256) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(item.SHA256, "sha256:") {
			t.Fatalf("%s manifest digest 格式错误", item.File)
		}
		content, readErr := w2R02LegacyUpgradeFS.ReadFile("testdata/w2_r02_upgrade/" + item.File)
		if readErr != nil {
			t.Fatal(readErr)
		}
		actual := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(actual[:]); subtle.ConstantTimeCompare([]byte(got), []byte(item.SHA256)) != 1 {
			t.Fatalf("%s raw digest=%s want=%s", item.File, got, item.SHA256)
		}
	}
}

func TestLegacyAuthorityAttestationV1Corpus(t *testing.T) {
	corpus := loadLegacyAuthorityCorpusV1(t)
	fixtures := make(map[string]legacyAuthorityFixtureV1, len(corpus.Fixtures))
	for _, fixture := range corpus.Fixtures {
		if _, duplicate := fixtures[fixture.FixtureID]; duplicate {
			t.Fatalf("重复 Authority fixture %q", fixture.FixtureID)
		}
		fixtures[fixture.FixtureID] = fixture
	}
	if len(corpus.Cases) != 17 {
		t.Fatalf("Authority vector count=%d want=17", len(corpus.Cases))
	}
	caseIDs := make([]string, len(corpus.Cases))
	for index, fixtureCase := range corpus.Cases {
		caseIDs[index] = fixtureCase.ID
	}
	if want := legacyAuthorityRequiredCaseIDsV1(); !reflect.DeepEqual(caseIDs, want) {
		t.Fatalf("Authority case IDs=%v want=%v", caseIDs, want)
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, fixtureCase := range corpus.Cases {
		fixtureCase := fixtureCase
		t.Run(fixtureCase.ID, func(t *testing.T) {
			if _, duplicate := seen[fixtureCase.ID]; duplicate {
				t.Fatalf("重复 Authority vector %q", fixtureCase.ID)
			}
			seen[fixtureCase.ID] = struct{}{}
			fixture, ok := fixtures[fixtureCase.FromFixture]
			if !ok {
				t.Fatalf("未知 Authority fixture %q", fixtureCase.FromFixture)
			}
			for _, mutation := range fixtureCase.Mutations {
				applyLegacyAuthorityMutationV1(&fixture, &fixtureCase, mutation)
			}
			digest, err := calculateLegacyAuthorityDigestV1(fixture.Canonical)
			if err != nil {
				t.Fatal(err)
			}
			reasons := validateLegacyAuthorityV1(fixture, fixtureCase.Mode, fixtureCase.StoredAuthorityDigest, digest)
			decision := "valid"
			if len(reasons) > 0 {
				decision = "invalid"
			}
			if decision != fixtureCase.Expected.Decision || !reflect.DeepEqual(reasons, fixtureCase.Expected.ReasonCodes) {
				t.Fatalf("Authority result decision=%s reasons=%v want=%+v", decision, reasons, fixtureCase.Expected)
			}
			if fixtureCase.Expected.AuthorityDigest != "" && fixtureCase.Expected.AuthorityDigest != digest {
				t.Fatalf("Authority digest=%s want=%s", digest, fixtureCase.Expected.AuthorityDigest)
			}
		})
	}
}

func TestLegacyAuthorityAttestationV1GoldenDigests(t *testing.T) {
	corpus := loadLegacyAuthorityCorpusV1(t)
	goldenCount := 0
	for _, fixtureCase := range corpus.Cases {
		if !strings.Contains(fixtureCase.ID, "golden") {
			continue
		}
		goldenCount++
		if fixtureCase.Expected.Decision != "valid" || fixtureCase.Expected.AuthorityDigest == "" {
			t.Fatalf("golden %s 缺少冻结 digest", fixtureCase.ID)
		}
	}
	if goldenCount != 3 {
		t.Fatalf("Authority golden count=%d want=3", goldenCount)
	}
}

func TestLegacyAuthorityAttestationV1CanonicalFields(t *testing.T) {
	corpus := loadLegacyAuthorityCorpusV1(t)
	fixture := corpus.Fixtures[0]
	raw, err := json.Marshal(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := `{"schema_version":"legacy_ensure_receipt_attestation.v1","authority_kind":"legacy_ensure_receipt_attestation","evidence_level":"derived_provenance_only"`
	if !strings.HasPrefix(string(raw), wantPrefix) || strings.Contains(string(raw), "attestation_id") || strings.Contains(string(raw), "migration_run_id") {
		t.Fatalf("Authority canonical 字段顺序或排除域错误: %s", raw)
	}
	before, err := calculateLegacyAuthorityDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	fixture.OperationalMetadata.AttestationID = "019f6000-ffff-7fff-8fff-ffffffffffff"
	fixture.OperationalMetadata.MigrationRunID = "retry"
	fixture.OperationalMetadata.Attempt++
	after, err := calculateLegacyAuthorityDigestV1(fixture.Canonical)
	if err != nil || before != after {
		t.Fatalf("运行元数据不得改变 Authority digest: before=%s after=%s err=%v", before, after, err)
	}
	if fixture.Canonical.EvidenceLevel == "authenticated_user" || fixture.Canonical.AuthorityKind == "authenticated_user" {
		t.Fatal("legacy provenance 不得伪装历史 authenticated_user")
	}
}

func TestLegacyAuthorityAttestationV1RejectsInvalidCanonicalShape(t *testing.T) {
	corpus := loadLegacyAuthorityCorpusV1(t)
	base := corpus.Fixtures[0]
	testCases := []struct {
		name   string
		mutate func(*legacyAuthorityCanonicalV1)
	}{
		{name: "source_uuid", mutate: func(v *legacyAuthorityCanonicalV1) { v.SourceCommandID = "not-a-uuid" }},
		{name: "session_uuid_version", mutate: func(v *legacyAuthorityCanonicalV1) { v.SessionID = "550e8400-e29b-41d4-a716-446655440000" }},
		{name: "request_digest", mutate: func(v *legacyAuthorityCanonicalV1) { v.SourceRequestDigest = "ABC" }},
		{name: "content_digest", mutate: func(v *legacyAuthorityCanonicalV1) { v.ContentDigest = strings.Repeat("A", 64) }},
		{name: "result_version", mutate: func(v *legacyAuthorityCanonicalV1) { v.ResultVersion = 2 }},
		{name: "skill_schema", mutate: func(v *legacyAuthorityCanonicalV1) { v.SkillSnapshotSchemaVersion = "future" }},
		{name: "empty_skill_count", mutate: func(v *legacyAuthorityCanonicalV1) { v.SkillCount = 1 }},
		{name: "completed_at", mutate: func(v *legacyAuthorityCanonicalV1) { v.ReceiptCompletedAtUnixUS = 0 }},
		{name: "completed_at_js_overflow", mutate: func(v *legacyAuthorityCanonicalV1) { v.ReceiptCompletedAtUnixUS = 9_007_199_254_740_992 }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := base
			testCase.mutate(&fixture.Canonical)
			digest, err := calculateLegacyAuthorityDigestV1(fixture.Canonical)
			if err != nil {
				t.Fatal(err)
			}
			reasons := validateLegacyAuthorityV1(fixture, "derive", "", digest)
			if !containsLegacyReasonV1(reasons, "AUTHORITY_SCHEMA_INVALID") {
				t.Fatalf("非法 canonical shape 未 fail-closed: %v", reasons)
			}
		})
	}
}

func TestSessionLaneUpgradeBlockerV1Corpus(t *testing.T) {
	corpus := loadLegacyBlockerCorpusV1(t)
	if len(corpus.Cases) != 90 {
		t.Fatalf("upgrade blocker vector count=%d want=90", len(corpus.Cases))
	}
	caseIDs := make([]string, len(corpus.Cases))
	for index, fixtureCase := range corpus.Cases {
		caseIDs[index] = fixtureCase.ID
	}
	if want := legacyBlockerRequiredCaseIDsV1(); !reflect.DeepEqual(caseIDs, want) {
		t.Fatalf("upgrade blocker case IDs=%v want=%v", caseIDs, want)
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, fixtureCase := range corpus.Cases {
		fixtureCase := fixtureCase
		t.Run(fixtureCase.ID, func(t *testing.T) {
			if _, duplicate := seen[fixtureCase.ID]; duplicate {
				t.Fatalf("重复 blocker vector %q", fixtureCase.ID)
			}
			seen[fixtureCase.ID] = struct{}{}
			snapshot := newLegacyUpgradeBaselineV1(fixtureCase.Baseline)
			for _, mutation := range fixtureCase.Mutations {
				applyLegacyBlockerMutationV1(&snapshot, mutation)
			}
			got, classifyErr := classifyLegacyUpgradeV1(snapshot, fixtureCase.Phase, fixtureCase.Scope)
			if classifyErr != nil {
				t.Fatalf("分类合法 Corpus 向量: %v", classifyErr)
			}
			want := legacyBlockerResultV1{
				Decision: fixtureCase.Expected.Decision, ReasonCodes: fixtureCase.Expected.ReasonCodes,
				AttestationCount: fixtureCase.Expected.AttestationCount, TurnCount: fixtureCase.Expected.TurnCount,
				RunCount: fixtureCase.Expected.RunCount, RowVerified: fixtureCase.Expected.RowVerified,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("blocker result=%+v want=%+v", got, want)
			}
			if len(got.ReasonCodes) > 0 && (got.AttestationCount != snapshot.AuthorityAttestationCount || got.TurnCount != snapshot.TurnCount || got.RunCount != snapshot.RunCount) {
				t.Fatal("blocked classifier 不得伪造回填事实")
			}
		})
	}
}

func TestSessionLaneUpgradeBlockerV1ExactSets(t *testing.T) {
	corpus := loadLegacyBlockerCorpusV1(t)
	assertStrings := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s exact-set=%v want=%v", name, got, want)
		}
	}
	assertStrings("phases", corpus.ExactSets.Phases, []string{"preflight", "verify"})
	assertStrings("scopes", corpus.ExactSets.Scopes, []string{"input_row", "session_rooted", "global_orphan", "full"})
	assertStrings("decisions", corpus.ExactSets.Decisions, []string{"diagnostic_clear", "eligible", "legal_no_input", "verified", "blocked"})
	assertStrings("content_states", corpus.ExactSets.ContentStates, []string{"verified_active_key", "verified_previous_key", "unverified", "unreadable_unknown_key", "unreadable_auth_or_digest"})
	assertStrings("reason_codes", corpus.ExactSets.ReasonCodes, legacyBlockerReasonOrderV1())
}

func TestLegacyUpgradeClassificationRejectsUnknownDomain(t *testing.T) {
	baseline := newLegacyUpgradeBaselineV1("legacy.v1.prompt")
	for _, fixture := range []struct {
		name, phase, scope string
		mutate             func(*legacyUpgradeSnapshotV1)
	}{
		{name: "phase", phase: "future", scope: "full", mutate: func(*legacyUpgradeSnapshotV1) {}},
		{name: "scope", phase: "preflight", scope: "future", mutate: func(*legacyUpgradeSnapshotV1) {}},
		{name: "message_content", phase: "preflight", scope: "full", mutate: func(v *legacyUpgradeSnapshotV1) { v.MessageContentState = "future" }},
		{name: "skill_content", phase: "preflight", scope: "full", mutate: func(v *legacyUpgradeSnapshotV1) { v.SkillRuntimeContentState = "" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value := baseline
			fixture.mutate(&value)
			if _, err := classifyLegacyUpgradeV1(value, fixture.phase, fixture.scope); err == nil {
				t.Fatal("未知 phase/scope/content state 必须 fail-closed")
			}
		})
	}
	partial, err := classifyLegacyUpgradeV1(baseline, "preflight", "session_rooted")
	if err != nil || partial.Decision != "diagnostic_clear" || partial.RowVerified {
		t.Fatalf("部分 scope 只允许诊断清除，不得授权升级: result=%+v err=%v", partial, err)
	}
}

func TestLegacyEventRetentionV1FailsClosed(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		baseline string
		mutate   func(*legacyUpgradeSnapshotV1)
		want     string
	}{
		{name: "created_pruned", baseline: "legacy.v1.empty_prompt", mutate: func(v *legacyUpgradeSnapshotV1) { v.EventMinAvailableSeq = v.CreatedEventSeq + 1 }, want: "SESSION_CREATED_EVENT_MISSING"},
		{name: "accepted_pruned", baseline: "legacy.v1.prompt", mutate: func(v *legacyUpgradeSnapshotV1) { v.EventMinAvailableSeq = v.AcceptedEventSeq + 1 }, want: "ACCEPTED_EVENT_MISSING"},
		{name: "online_range_gap", baseline: "legacy.v1.prompt", mutate: func(v *legacyUpgradeSnapshotV1) { v.EventSequenceDense = false }, want: "SESSION_EVENT_SEQUENCE_GAP"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value := newLegacyUpgradeBaselineV1(fixture.baseline)
			fixture.mutate(&value)
			got, err := classifyLegacyUpgradeV1(value, "preflight", "full")
			if err != nil || !containsLegacyReasonV1(got.ReasonCodes, fixture.want) || got.RowVerified {
				t.Fatalf("Retention 异常必须阻断: result=%+v err=%v", got, err)
			}
		})
	}
}

func TestSessionLaneUpgradeBlockerV1ReasonOrder(t *testing.T) {
	corpus := loadLegacyBlockerCorpusV1(t)
	rank := make(map[string]int, len(corpus.ExactSets.ReasonCodes))
	for index, code := range corpus.ExactSets.ReasonCodes {
		rank[code] = index
	}
	for _, fixtureCase := range corpus.Cases {
		for index := 1; index < len(fixtureCase.Expected.ReasonCodes); index++ {
			if rank[fixtureCase.Expected.ReasonCodes[index-1]] >= rank[fixtureCase.Expected.ReasonCodes[index]] {
				t.Fatalf("%s reason_codes 未按固定 rank 递增: %v", fixtureCase.ID, fixtureCase.Expected.ReasonCodes)
			}
		}
	}
}

func TestSessionLaneUpgradeBlockerV1RootedAntiJoin(t *testing.T) {
	corpus := loadLegacyBlockerCorpusV1(t)
	required := map[string][]string{
		"UPG-03-P01-empty-prompt-root-legal":         {},
		"UPG-03-N04-receipt-message-target-missing":  {"SESSION_RECEIPT_MESSAGE_TARGET_MISSING"},
		"UPG-03-N05-receipt-input-target-missing":    {"SESSION_RECEIPT_INPUT_TARGET_MISSING"},
		"UPG-03-N09-orphan-receipt-session":          {"ORPHAN_RECEIPT_SESSION"},
		"UPG-03-N10-orphan-message-session":          {"ORPHAN_MESSAGE_SESSION"},
		"UPG-03-N11-orphan-input-session":            {"ORPHAN_INPUT_SESSION"},
		"UPG-03-N12-orphan-event-session":            {"ORPHAN_EVENT_SESSION"},
		"UPG-03-N14-orphan-snapshot-header-session":  {"ORPHAN_SKILL_SNAPSHOT_HEADER_SESSION"},
		"UPG-03-N15-orphan-snapshot-item-session":    {"ORPHAN_SKILL_SNAPSHOT_ITEM_SESSION"},
		"UPG-03-N16-orphan-sequence-counter-session": {"ORPHAN_SEQUENCE_COUNTER_SESSION"},
		"UPG-03-N17-orphan-event-counter-session":    {"ORPHAN_EVENT_COUNTER_SESSION"},
		"UPG-03-N18-orphan-runtime-lease-session":    {"ORPHAN_RUNTIME_LEASE_SESSION"},
		"UPG-03-N19-orphan-authority-session":        {"ORPHAN_AUTHORITY_SESSION"},
		"UPG-03-N20-orphan-upgrade-ledger-session":   {"ORPHAN_UPGRADE_LEDGER_SESSION"},
		"UPG-03-N21-orphan-turn-session":             {"ORPHAN_TURN_SESSION"},
		"UPG-03-N22-orphan-run-session":              {"ORPHAN_RUN_SESSION"},
		"UPG-03-N23-orphan-enqueue-result-session":   {"ORPHAN_ENQUEUE_RESULT_SESSION"},
		"UPG-03-N24-empty-prompt-receipt-invalid":    {"RECEIPT_TYPE_UNSUPPORTED", "ENSURE_CANONICAL_DIGEST_MISMATCH", "RECEIPT_NOT_IMMUTABLE"},
		"UPG-03-N25-empty-prompt-existing-facts":     {"AUTHORITY_ATTESTATION_CONFLICT", "TURN_CONFLICT", "RUN_ALREADY_EXISTS", "UPGRADE_LEDGER_CONFLICT"},
		"UPG-03-N26-created-event-above-high-water":  {"SESSION_CREATED_EVENT_MISSING"},
	}
	for _, fixtureCase := range corpus.Cases {
		want, ok := required[fixtureCase.ID]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(fixtureCase.Expected.ReasonCodes, want) {
			t.Fatalf("%s rooted anti-join=%v want=%v", fixtureCase.ID, fixtureCase.Expected.ReasonCodes, want)
		}
		delete(required, fixtureCase.ID)
	}
	if len(required) != 0 {
		t.Fatalf("缺少 rooted anti-join vectors: %v", required)
	}
}

func TestLegacyUpgradeLedgerV1CrashRecovery(t *testing.T) {
	const (
		authorityID = "019f6000-0101-7000-8000-000000000101"
		turnID      = "019f6000-0102-7000-8000-000000000102"
	)
	prepared := legacyUpgradeLedgerV1{
		LedgerKey: "generation-7:019f6000-0100-7000-8000-000000000100", UpgradeGeneration: 7,
		State: "prepared", FactsDigest: strings.Repeat("1", 64), PlanDigest: strings.Repeat("2", 64),
		TargetInputDigest: strings.Repeat("4", 64), TurnContextDigest: strings.Repeat("5", 64),
		AuthorityAttestationID: authorityID, AuthorityDigest: strings.Repeat("3", 64),
		TurnID: turnID, ContextMessageSeq: 7, LockedLastMessageSeq: 7,
	}
	if !legacyUpgradeLedgerTransitionV1("", "blocked") || !legacyUpgradeLedgerTransitionV1("", "prepared") ||
		!legacyUpgradeLedgerTransitionV1("prepared", "applied") || !legacyUpgradeLedgerTransitionV1("applied", "verified") {
		t.Fatal("legacy upgrade ledger 合法迁移未冻结")
	}
	for _, transition := range [][2]string{{"blocked", "prepared"}, {"verified", "applied"}, {"prepared", "verified"}, {"applied", "prepared"}} {
		if legacyUpgradeLedgerTransitionV1(transition[0], transition[1]) {
			t.Fatalf("非法 ledger 迁移被接受: %q -> %q", transition[0], transition[1])
		}
	}

	t.Run("before_prepare", func(t *testing.T) {
		before := legacyUpgradeAppliedV1{}
		after, committed, err := applyLegacyUpgradeTransactionV1(before, legacyUpgradeLedgerV1{}, "")
		if err == nil || committed || after != before {
			t.Fatalf("prepare 前崩溃必须零事实: after=%+v committed=%v err=%v", after, committed, err)
		}
	})
	for _, crashPoint := range []string{"after_input_patch", "after_turn_insert"} {
		t.Run(crashPoint+"_rolls_back", func(t *testing.T) {
			before := legacyUpgradeAppliedV1{}
			after, committed, err := applyLegacyUpgradeTransactionV1(before, prepared, crashPoint)
			if err == nil || committed || after != before {
				t.Fatalf("%s 必须整体回滚: after=%+v committed=%v err=%v", crashPoint, after, committed, err)
			}
		})
	}

	store, committed, err := applyLegacyUpgradeTransactionV1(legacyUpgradeAppliedV1{}, prepared, "after_apply_commit")
	if err == nil || !committed {
		t.Fatalf("apply commit 响应丢失应保留已提交事实: committed=%v err=%v", committed, err)
	}
	if store.LedgerState != "applied" || store.AuthorityAttestationID != authorityID || store.TurnID != turnID || store.ContextMessageSeq != 7 || store.RunCount != 0 {
		t.Fatalf("回填必须复用冻结身份、Message cutoff 且不创建 Run: %+v", store)
	}
	replayed, committed, err := applyLegacyUpgradeTransactionV1(store, prepared, "")
	if err != nil || committed || replayed != store {
		t.Fatalf("apply replay 必须零增量: replayed=%+v committed=%v err=%v", replayed, committed, err)
	}
	tampered := prepared
	tampered.PlanDigest = strings.Repeat("4", 64)
	if after, _, conflictErr := applyLegacyUpgradeTransactionV1(store, tampered, ""); conflictErr == nil || after != store {
		t.Fatalf("同 ledger key 异义必须冲突且原样保留: after=%+v err=%v", after, conflictErr)
	}
	preexistingRun := legacyUpgradeAppliedV1{RunCount: 1}
	if after, _, conflictErr := applyLegacyUpgradeTransactionV1(preexistingRun, prepared, ""); conflictErr == nil || after != preexistingRun {
		t.Fatalf("已有 Run 的 fresh apply 必须拒绝: after=%+v err=%v", after, conflictErr)
	}
	badCutoff := prepared
	badCutoff.ContextMessageSeq++
	if after, _, cutoffErr := applyLegacyUpgradeTransactionV1(legacyUpgradeAppliedV1{}, badCutoff, ""); cutoffErr == nil || after != (legacyUpgradeAppliedV1{}) {
		t.Fatalf("Turn cutoff 必须精确等于锁定 last_message_seq: after=%+v err=%v", after, cutoffErr)
	}
	verified, verifyCommitted, verifyErr := verifyLegacyUpgradeV1(store, prepared, "after_verify_commit")
	if verifyErr == nil || !verifyCommitted || verified.LedgerState != "verified" {
		t.Fatalf("verify commit 响应丢失必须保留 verified: result=%+v committed=%v err=%v", verified, verifyCommitted, verifyErr)
	}
	verifyReplay, replayCommitted, replayErr := verifyLegacyUpgradeV1(verified, prepared, "")
	if replayErr != nil || replayCommitted || verifyReplay != verified {
		t.Fatalf("verify 响应丢失后的同义重放必须零增量成功: result=%+v committed=%v err=%v", verifyReplay, replayCommitted, replayErr)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*legacyUpgradeAppliedV1, *legacyUpgradeLedgerV1)
	}{
		{name: "authority_id", mutate: func(value *legacyUpgradeAppliedV1, _ *legacyUpgradeLedgerV1) {
			value.AuthorityAttestationID = "019f6000-0103-7000-8000-000000000103"
		}},
		{name: "authority_digest", mutate: func(value *legacyUpgradeAppliedV1, _ *legacyUpgradeLedgerV1) {
			value.AuthorityDigest = strings.Repeat("6", 64)
		}},
		{name: "turn_id", mutate: func(value *legacyUpgradeAppliedV1, _ *legacyUpgradeLedgerV1) {
			value.TurnID = "019f6000-0104-7000-8000-000000000104"
		}},
		{name: "ledger_state", mutate: func(_ *legacyUpgradeAppliedV1, value *legacyUpgradeLedgerV1) { value.State = "applied" }},
	} {
		t.Run("verify_rejects_"+testCase.name+"_tamper", func(t *testing.T) {
			before, ledger := store, prepared
			testCase.mutate(&before, &ledger)
			after, committed, integrityErr := verifyLegacyUpgradeV1(before, ledger, "")
			if integrityErr == nil || committed || after != before {
				t.Fatalf("verify 篡改必须原样失败: after=%+v committed=%v err=%v", after, committed, integrityErr)
			}
		})
	}
}

func TestLegacyLaneReadinessV1SeparatesFoundation(t *testing.T) {
	ready := legacyLaneReadinessV1{
		FoundationReady: true, LaneSchemaReady: true, CompatibleWriterReady: true,
		LegacyClassificationComplete: true, ActivationPoliciesReady: true, UpgradeEvidenceApproved: true,
		CapabilityState: "ready", ProcessorEnabled: true, UpgradeGeneration: 7, ClaimGeneration: 7,
	}
	testCases := []struct {
		name   string
		mutate func(*legacyLaneReadinessV1)
		want   legacyLaneReadinessResultV1
	}{
		{name: "READY-01-all-gates", mutate: func(*legacyLaneReadinessV1) {}, want: legacyLaneReadinessResultV1{true, true, true, true}},
		{name: "READY-02-schema-only", mutate: func(v *legacyLaneReadinessV1) {
			v.CompatibleWriterReady = false
			v.LegacyClassificationComplete = false
			v.CapabilityState = "upgrading"
		}, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-03-disabled", mutate: func(v *legacyLaneReadinessV1) { v.CapabilityState = "disabled"; v.ProcessorEnabled = false }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-04-verifying", mutate: func(v *legacyLaneReadinessV1) { v.CapabilityState = "verifying" }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-05-blocked-foundation-stays-ready", mutate: func(v *legacyLaneReadinessV1) { v.CapabilityState = "blocked"; v.LegacyBlockerCount = 1 }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-06-unverified", mutate: func(v *legacyLaneReadinessV1) { v.EligibleUnverifiedCount = 1 }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-07-active-helper-claim", mutate: func(v *legacyLaneReadinessV1) { v.ActiveUpgradeClaims = 1 }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-08-activation-policy-missing", mutate: func(v *legacyLaneReadinessV1) { v.ActivationPoliciesReady = false }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-09-real-pg-evidence-missing", mutate: func(v *legacyLaneReadinessV1) { v.UpgradeEvidenceApproved = false }, want: legacyLaneReadinessResultV1{true, false, false, false}},
		{name: "READY-10-processor-off", mutate: func(v *legacyLaneReadinessV1) { v.ProcessorEnabled = false }, want: legacyLaneReadinessResultV1{true, true, false, false}},
		{name: "READY-11-stale-generation", mutate: func(v *legacyLaneReadinessV1) { v.ClaimGeneration = 6 }, want: legacyLaneReadinessResultV1{true, true, true, false}},
		{name: "READY-12-foundation-failure", mutate: func(v *legacyLaneReadinessV1) { v.FoundationReady = false }, want: legacyLaneReadinessResultV1{false, false, false, false}},
		{name: "READY-13-current-repository", mutate: func(v *legacyLaneReadinessV1) {
			v.LaneSchemaReady = false
			v.CompatibleWriterReady = false
			v.LegacyClassificationComplete = false
			v.ActivationPoliciesReady = false
			v.UpgradeEvidenceApproved = false
			v.CapabilityState = "disabled"
			v.ProcessorEnabled = false
		}, want: legacyLaneReadinessResultV1{true, false, false, false}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := ready
			testCase.mutate(&fixture)
			if got := calculateLegacyLaneReadinessV1(fixture); got != testCase.want {
				t.Fatalf("readiness=%+v want=%+v", got, testCase.want)
			}
		})
	}
}

func TestLegacyUpgradeDownGuardV1(t *testing.T) {
	pristine := legacyDownFactsV1{
		CompatibleWritersStopped: true, ProcessorStopped: true, ScannerStopped: true, MigrationFenceHeld: true,
	}
	testCases := []struct {
		name   string
		mutate func(*legacyDownFactsV1)
		allow  bool
	}{
		{name: "DOWN-01-expand-only-pristine", mutate: func(*legacyDownFactsV1) {}, allow: true},
		{name: "DOWN-02-enqueue-header", mutate: func(v *legacyDownFactsV1) { v.EnqueueHeaderCount = 1 }},
		{name: "DOWN-03-alias-receipt", mutate: func(v *legacyDownFactsV1) { v.AliasReceiptCount = 1 }},
		{name: "DOWN-04-origin-result", mutate: func(v *legacyDownFactsV1) { v.OriginResultCount = 1 }},
		{name: "DOWN-05-authority-snapshot", mutate: func(v *legacyDownFactsV1) { v.AuthoritySnapshotCount = 1 }},
		{name: "DOWN-06-legacy-turn", mutate: func(v *legacyDownFactsV1) { v.LegacyTurnCount = 1 }},
		{name: "DOWN-07-run", mutate: func(v *legacyDownFactsV1) { v.RunCount = 1 }},
		{name: "DOWN-08-new-state", mutate: func(v *legacyDownFactsV1) { v.NewStateFactCount = 1 }},
		{name: "DOWN-09-any-upgrade-ledger", mutate: func(v *legacyDownFactsV1) { v.UpgradeLedgerCount = 1 }},
		{name: "DOWN-10-compatible-writer-running", mutate: func(v *legacyDownFactsV1) { v.CompatibleWritersStopped = false }},
		{name: "DOWN-11-processor-running", mutate: func(v *legacyDownFactsV1) { v.ProcessorStopped = false }},
		{name: "DOWN-12-scanner-running", mutate: func(v *legacyDownFactsV1) { v.ScannerStopped = false }},
		{name: "DOWN-13-migration-fence-missing", mutate: func(v *legacyDownFactsV1) { v.MigrationFenceHeld = false }},
		{name: "DOWN-14-turn-context", mutate: func(v *legacyDownFactsV1) { v.TurnContextCount = 1 }},
		{name: "DOWN-15-expanded-fact", mutate: func(v *legacyDownFactsV1) { v.ExpandedFactCount = 1 }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := pristine
			testCase.mutate(&fixture)
			before := fixture
			if got := legacyUpgradeDownAllowedV1(fixture); got != testCase.allow {
				t.Fatalf("Down allowed=%v want=%v facts=%+v", got, testCase.allow, fixture)
			}
			if fixture != before {
				t.Fatal("Down guard 必须在任何 DROP/DELETE/ALTER 前纯读拒绝")
			}
		})
	}
}

func loadLegacyManifestV1(t *testing.T) legacyUpgradeManifestV1 {
	t.Helper()
	raw, err := w2R02LegacyUpgradeFS.ReadFile(legacyUpgradeManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("legacy upgrade manifest JSON 非法: %v", err)
	}
	var manifest legacyUpgradeManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func loadLegacyAuthorityCorpusV1(t *testing.T) legacyAuthorityCorpusV1 {
	t.Helper()
	raw, err := w2R02LegacyUpgradeFS.ReadFile(legacyAuthorityCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("legacy Authority Corpus JSON 非法: %v", err)
	}
	var corpus legacyAuthorityCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "legacy_authority_attestation_v1_corpus.v1" {
		t.Fatalf("Authority corpus schema=%q", corpus.SchemaVersion)
	}
	want := legacyAuthorityExactSetsV1{
		Modes: []string{"derive", "verify_persisted"}, Decisions: []string{"valid", "invalid"},
		AuthorityKinds:         []string{"legacy_ensure_receipt_attestation"},
		EvidenceLevels:         []string{"derived_provenance_only"},
		SourceContractVersions: []string{"legacy.ensure_project_session.v1", "legacy.ensure_project_session.v2"},
		CapabilityPolicies:     []string{"legacy_chat_only"},
		ReasonCodes: []string{
			"AUTHORITY_SCHEMA_INVALID", "AUTHORITY_KIND_FORBIDDEN", "AUTHORITY_EVIDENCE_LEVEL_FORBIDDEN",
			"AUTHORITY_SOURCE_CONTRACT_MISMATCH", "AUTHORITY_RECEIPT_NOT_IMMUTABLE",
			"AUTHORITY_ENSURE_DIGEST_MISMATCH", "AUTHORITY_SESSION_BINDING_MISMATCH",
			"AUTHORITY_INPUT_BINDING_MISMATCH", "AUTHORITY_SKILL_BINDING_MISMATCH",
			"AUTHORITY_CAPABILITY_FORBIDDEN", "AUTHORITY_CANONICAL_DIGEST_MISMATCH",
		},
	}
	if !reflect.DeepEqual(corpus.ExactSets, want) {
		t.Fatalf("Authority exact-set=%+v want=%+v", corpus.ExactSets, want)
	}
	return corpus
}

func loadLegacyBlockerCorpusV1(t *testing.T) legacyBlockerCorpusV1 {
	t.Helper()
	raw, err := w2R02LegacyUpgradeFS.ReadFile(legacyBlockerCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("legacy blocker Corpus JSON 非法: %v", err)
	}
	var corpus legacyBlockerCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "session_lane_upgrade_blocker_v1_corpus.v1" {
		t.Fatalf("blocker corpus schema=%q", corpus.SchemaVersion)
	}
	return corpus
}

func calculateLegacyAuthorityDigestV1(canonical legacyAuthorityCanonicalV1) (string, error) {
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal legacy Authority canonical: %w", err)
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(legacyAuthorityDigestDomain))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(raw)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func calculateLegacyEnsureDigestV1(fixture legacyAuthorityFixtureV1) (string, string, error) {
	canonical := fixture.Canonical
	switch canonical.SourceCommandType {
	case "ensure_project_session_v1":
		requestDigest, promptDigest, _, err := agentsession.CalculateRequestDigest(
			canonical.ProjectID,
			canonical.OwnerUserID,
			fixture.InitialPrompt,
			agentsession.SkillSnapshotKindEmpty,
		)
		return requestDigest, promptDigest, err
	case "ensure_project_session_v2":
		snapshot, err := legacyProductionSkillSnapshotV1(canonical)
		if err != nil {
			return "", "", err
		}
		result, err := agentskill.CanonicalEnsureProjectSessionV2(agentskill.EnsureProjectSessionInputV2{
			SchemaVersion: agentskill.EnsureProjectSessionSchemaVersionV2,
			ProjectID:     canonical.ProjectID, OwnerUserID: canonical.OwnerUserID,
			CreationSource: agentskill.CreationSourceQuickCreate,
			InitialPrompt:  fixture.InitialPrompt, SkillSnapshot: snapshot,
		}, agentskill.DefaultLimitsProfileV1())
		if err != nil {
			return "", "", err
		}
		return result.RequestDigest.Hex(), result.PromptDigest, nil
	default:
		return "", "", fmt.Errorf("unsupported Ensure command type")
	}
}

func legacyProductionSkillSnapshotV1(canonical legacyAuthorityCanonicalV1) (agentskill.SessionSkillSnapshotV1, error) {
	snapshot := agentskill.SessionSkillSnapshotV1{
		SchemaVersion: canonical.SkillSnapshotSchemaVersion,
		SnapshotKind:  agentskill.SessionSkillSnapshotKindV1(canonical.SkillSnapshotKind),
		SkillCount:    int32(canonical.SkillCount), SnapshotSetDigest: canonical.SkillSnapshotDigest,
	}
	switch canonical.SkillSnapshotKind {
	case "empty":
		snapshot.Skills = []agentskill.PublishedSkillSnapshotRefV1{}
		return snapshot, nil
	case "published_refs":
		notApplicable := agentskill.CapabilityGuidanceV1{
			Applicability: agentskill.SkillGuidanceNotApplicableV1, NotApplicableReason: "not used",
		}
		runtimeContent := agentskill.SkillRuntimeContentV1{
			SchemaVersion: agentskill.RuntimeContentSchemaVersionV1,
			Name:          "Prompt helper", InputDescription: "text", OutputDescription: "prompt",
			InvocationRules: "Use for prompt writing.", PlanCreationSpec: notApplicable,
			AnalyzeMaterials: notApplicable, PlanStoryboard: notApplicable, GenerateMedia: notApplicable,
			WritePrompts: agentskill.CapabilityGuidanceV1{
				Applicability: agentskill.SkillGuidanceEnabledV1, Guidance: "Write concise prompts.",
			},
			AssembleOutput: notApplicable, Examples: []agentskill.SkillExampleV1{},
			StarterPrompts: []string{"Improve this prompt."},
		}
		snapshot.Skills = []agentskill.PublishedSkillSnapshotRefV1{{
			LoadOrder: 1, Priority: 100, Namespace: agentskill.SkillNamespaceUserV1,
			SkillID:             "019f0000-0000-7000-8000-000000000101",
			PublisherUserID:     "019f0000-0000-7000-8000-000000000102",
			PublishedSnapshotID: "019f0000-0000-7000-8000-000000000103",
			PublicationRevision: 2, DefinitionSchemaVersion: agentskill.DefinitionSchemaVersionV1,
			ContentDigest:               "dc18b1bbe2824f462cbef7373e48074d609cdd4d57897dd87e1b26c85b96d513",
			RuntimeContentSchemaVersion: agentskill.RuntimeContentSchemaVersionV1,
			RuntimeContentDigest:        "d81700e078c331dc271db6d9c7c169f75f48f9fd89f944671883316044594168",
			RuntimeContent:              runtimeContent, AllowedGraphToolKeys: []string{"write_prompts"},
			PublicToolRefs:           []agentskill.PublicToolSnapshotRefV1{},
			PermissionSnapshotDigest: "3317ba4d31b6b64d9c9248495a225da4ca1c4bd738cb403289d9108fe05d9d25",
			RuntimePolicyRef:         agentskill.RuntimePolicyRefV1, GovernanceEpoch: 3, PublishedAtUnixMS: 1784011500123,
		}}
		return snapshot, nil
	default:
		return agentskill.SessionSkillSnapshotV1{}, fmt.Errorf("unsupported legacy skill snapshot kind")
	}
}

func validLegacyAuthorityCanonicalShapeV1(canonical legacyAuthorityCanonicalV1) bool {
	for _, value := range []string{
		canonical.SourceCommandID, canonical.SessionID, canonical.ProjectID, canonical.OwnerUserID,
		canonical.MessageID, canonical.InputID,
	} {
		if _, err := parseCanonicalUUIDv7(value); err != nil {
			return false
		}
	}
	for _, digest := range []string{canonical.SourceRequestDigest, canonical.ContentDigest, canonical.SkillSnapshotDigest} {
		if !sessionLaneBusinessDigestPatternV1.MatchString(digest) {
			return false
		}
	}
	wantResultVersion := map[string]int64{"ensure_project_session_v1": 1, "ensure_project_session_v2": 2}[canonical.SourceCommandType]
	if wantResultVersion == 0 || canonical.ResultVersion != wantResultVersion || canonical.SkillSnapshotSchemaVersion != "session_skill_snapshot.v1" ||
		canonical.SkillCount < 0 || canonical.SkillCount > 32 || canonical.ReceiptCompletedAtUnixUS <= 0 ||
		canonical.ReceiptCompletedAtUnixUS > 9_007_199_254_740_991 {
		return false
	}
	switch canonical.SkillSnapshotKind {
	case "empty":
		return canonical.SkillCount == 0 && canonical.SkillSnapshotDigest == "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945"
	case "published_refs":
		return canonical.SkillCount > 0
	default:
		return false
	}
}

func validateLegacyAuthorityV1(
	fixture legacyAuthorityFixtureV1,
	mode string,
	storedDigest string,
	calculatedDigest string,
) []string {
	reasons := make([]string, 0, 4)
	canonical := fixture.Canonical
	if (mode != "derive" && mode != "verify_persisted") || canonical.SchemaVersion != "legacy_ensure_receipt_attestation.v1" ||
		!validLegacyAuthorityCanonicalShapeV1(canonical) {
		reasons = append(reasons, "AUTHORITY_SCHEMA_INVALID")
	}
	if canonical.AuthorityKind != "legacy_ensure_receipt_attestation" {
		reasons = append(reasons, "AUTHORITY_KIND_FORBIDDEN")
	}
	if canonical.EvidenceLevel != "derived_provenance_only" {
		reasons = append(reasons, "AUTHORITY_EVIDENCE_LEVEL_FORBIDDEN")
	}
	wantContract := map[string]string{
		"ensure_project_session_v1": "legacy.ensure_project_session.v1",
		"ensure_project_session_v2": "legacy.ensure_project_session.v2",
	}[canonical.SourceCommandType]
	if wantContract == "" || canonical.SourceContractVersion != wantContract {
		reasons = append(reasons, "AUTHORITY_SOURCE_CONTRACT_MISMATCH")
	}
	if !fixture.ReceiptImmutable {
		reasons = append(reasons, "AUTHORITY_RECEIPT_NOT_IMMUTABLE")
	}
	ensureDigest, promptDigest, ensureErr := calculateLegacyEnsureDigestV1(fixture)
	if ensureErr != nil || subtle.ConstantTimeCompare([]byte(ensureDigest), []byte(canonical.SourceRequestDigest)) != 1 {
		reasons = append(reasons, "AUTHORITY_ENSURE_DIGEST_MISMATCH")
	}
	if !fixture.SessionBindingValid {
		reasons = append(reasons, "AUTHORITY_SESSION_BINDING_MISMATCH")
	}
	if !fixture.InputBindingValid || subtle.ConstantTimeCompare([]byte(promptDigest), []byte(canonical.ContentDigest)) != 1 {
		reasons = append(reasons, "AUTHORITY_INPUT_BINDING_MISMATCH")
	}
	if !fixture.SkillBindingValid {
		reasons = append(reasons, "AUTHORITY_SKILL_BINDING_MISMATCH")
	}
	if canonical.ExecutionClass != "chat" || canonical.CapabilityPolicy != "legacy_chat_only" {
		reasons = append(reasons, "AUTHORITY_CAPABILITY_FORBIDDEN")
	}
	if mode == "verify_persisted" && subtle.ConstantTimeCompare([]byte(storedDigest), []byte(calculatedDigest)) != 1 {
		reasons = append(reasons, "AUTHORITY_CANONICAL_DIGEST_MISMATCH")
	}
	return reasons
}

func applyLegacyAuthorityMutationV1(fixture *legacyAuthorityFixtureV1, fixtureCase *legacyAuthorityCaseV1, mutation string) {
	switch mutation {
	case "change_operational_metadata":
		fixture.OperationalMetadata.AttestationID = "019f6000-ffff-7fff-8fff-ffffffffffff"
		fixture.OperationalMetadata.MigrationRunID = "retry"
		fixture.OperationalMetadata.Attempt++
		fixture.OperationalMetadata.AttestedAtUnixUS++
	case "invalid_schema":
		fixture.Canonical.SchemaVersion = "legacy_ensure_receipt_attestation.v2"
	case "source_contract_mismatch":
		fixture.Canonical.SourceContractVersion = "legacy.ensure_project_session.v2"
	case "ensure_digest_mismatch":
		fixture.Canonical.SourceRequestDigest = strings.Repeat("0", sha256.Size*2)
	case "session_binding_mismatch":
		fixture.SessionBindingValid = false
	case "input_binding_mismatch":
		fixture.InputBindingValid = false
	case "skill_binding_mismatch":
		fixture.SkillBindingValid = false
	case "persisted_canonical_tamper":
		fixture.Canonical.ReceiptCompletedAtUnixUS++
	case "authenticated_user_kind":
		fixture.Canonical.AuthorityKind = "authenticated_user"
	case "authenticated_evidence_level":
		fixture.Canonical.EvidenceLevel = "authenticated_user"
	case "receipt_mutable":
		fixture.ReceiptImmutable = false
	case "sensitive_capability":
		fixture.Canonical.CapabilityPolicy = "sensitive_tool"
	default:
		panic("unknown Authority mutation: " + mutation)
	}
}

func newLegacyUpgradeBaselineV1(name string) legacyUpgradeSnapshotV1 {
	snapshot := legacyUpgradeSnapshotV1{
		SessionPresent: true, SessionActive: true, SessionVersionExpected: true,
		SessionReceiptPresent: true, SkillSnapshotPresent: true, SkillSnapshotValid: true,
		SkillItemCountMatches: true, SkillItemOrderDense: true,
		SequenceCounterPresent: true, MessageSequenceDense: true, InputSequenceDense: true,
		EventCounterPresent: true, EventSequenceDense: true, EventMinAvailableSeq: 1, EventLastSeq: 2,
		CreatedEventPresent: true, CreatedEventMatches: true, CreatedEventSeq: 1,
		RuntimeLeasePresent: true, RuntimeLeasePristine: true,
		ReceiptMessageTargetPresent: true, ReceiptInputTargetPresent: true,
		InputPresent: true, InputStatus: "pending", InputSource: "user_message", InputMessageRefPresent: true,
		MessagePresent: true, MessageSessionMatches: true, MessageSourceMatches: true,
		MessageRoleSupported: true, MessageEnvelopeValid: true, MessageDigestValid: true,
		MessageContentState: "verified_active_key",
		ReceiptPresent:      true, ReceiptTypeSupported: true, ReceiptResultVersionMatches: true,
		ReceiptSessionMatches: true, ReceiptMessageMatches: true, ReceiptInputMatches: true,
		ReceiptDigestValid: true, EnsureCanonicalDigestMatches: true, ReceiptSkillMatches: true,
		ReceiptImmutable: true, AcceptedEventCount: 1, AcceptedEventMatches: true, AcceptedEventSeq: 2,
		SkillRuntimeContentState:  "verified_active_key",
		AuthorityAttestationCount: 0, AuthorityAttestationMatches: true, AuthorityPolicyAllowed: true,
		TurnCount: 0, TurnMatches: true, UpgradeLedgerMatches: true,
	}
	switch name {
	case "legacy.v1.prompt", "legacy.v2.empty.prompt", "legacy.v2.published.prompt":
		return snapshot
	case "legacy.v1.empty_prompt":
		snapshot.InputPresent = false
		snapshot.InputMessageRefPresent = false
		snapshot.MessagePresent = false
		snapshot.ReceiptMessageTargetPresent = true
		snapshot.ReceiptInputTargetPresent = true
		snapshot.AcceptedEventCount = 0
		snapshot.AcceptedEventSeq = 0
		snapshot.EventLastSeq = 1
		return snapshot
	case "legacy.v1.prompt.backfilled":
		snapshot.AuthorityAttestationCount = 1
		snapshot.TurnCount = 1
		snapshot.UpgradeLedgerCount = 1
		return snapshot
	default:
		panic("unknown legacy baseline: " + name)
	}
}

func classifyLegacyUpgradeV1(snapshot legacyUpgradeSnapshotV1, phase, scope string) (legacyBlockerResultV1, error) {
	if (phase != "preflight" && phase != "verify") ||
		(scope != "input_row" && scope != "session_rooted" && scope != "global_orphan" && scope != "full") ||
		!validLegacyContentStateV1(snapshot.MessageContentState) || !validLegacyContentStateV1(snapshot.SkillRuntimeContentState) {
		return legacyBlockerResultV1{}, fmt.Errorf("legacy upgrade classification domain invalid")
	}
	reasons := collectLegacyBlockersV1(snapshot, phase, scope)
	decision := "blocked"
	verified := false
	if len(reasons) == 0 {
		switch {
		case scope != "full":
			decision = "diagnostic_clear"
		case !snapshot.InputPresent:
			decision, verified = "legal_no_input", true
		case phase == "verify":
			decision, verified = "verified", true
		default:
			decision = "eligible"
		}
	}
	return legacyBlockerResultV1{
		Decision: decision, ReasonCodes: reasons,
		AttestationCount: snapshot.AuthorityAttestationCount,
		TurnCount:        snapshot.TurnCount, RunCount: snapshot.RunCount, RowVerified: verified,
	}, nil
}

func validLegacyContentStateV1(value string) bool {
	switch value {
	case "verified_active_key", "verified_previous_key", "unverified", "unreadable_unknown_key", "unreadable_auth_or_digest":
		return true
	default:
		return false
	}
}

func collectLegacyBlockersV1(snapshot legacyUpgradeSnapshotV1, phase, scope string) []string {
	set := make(map[string]struct{})
	add := func(condition bool, code string) {
		if condition {
			set[code] = struct{}{}
		}
	}
	inputScope := scope == "input_row" || scope == "full"
	rootScope := scope == "session_rooted" || scope == "full"
	orphanScope := scope == "global_orphan" || scope == "full"
	if inputScope {
		add(!snapshot.SessionPresent, "SESSION_MISSING")
		add(snapshot.SessionPresent && !snapshot.SessionActive, "SESSION_NOT_ACTIVE")
		add(snapshot.SessionPresent && !snapshot.SessionVersionExpected, "SESSION_VERSION_UNEXPECTED")
		add(!snapshot.SkillSnapshotPresent, "SESSION_SKILL_SNAPSHOT_MISSING")
		add(snapshot.SkillSnapshotPresent && !snapshot.SkillSnapshotValid, "SESSION_SKILL_SNAPSHOT_INVALID")
		add(snapshot.SkillSnapshotPresent && !snapshot.SkillItemCountMatches, "SESSION_SKILL_ITEM_COUNT_MISMATCH")
		add(snapshot.SkillSnapshotPresent && !snapshot.SkillItemOrderDense, "SESSION_SKILL_ITEM_ORDER_GAP")
		add(!snapshot.SequenceCounterPresent, "SESSION_SEQUENCE_COUNTER_MISSING")
		add(snapshot.SequenceCounterPresent && !snapshot.MessageSequenceDense, "SESSION_MESSAGE_SEQUENCE_GAP")
		add(snapshot.SequenceCounterPresent && !snapshot.InputSequenceDense, "SESSION_INPUT_SEQUENCE_GAP")
		add(!snapshot.EventCounterPresent, "SESSION_EVENT_COUNTER_MISSING")
		add(snapshot.EventCounterPresent && (!snapshot.EventSequenceDense || snapshot.EventMinAvailableSeq < 1 ||
			snapshot.EventMinAvailableSeq > snapshot.EventLastSeq+1), "SESSION_EVENT_SEQUENCE_GAP")
		add(!snapshot.RuntimeLeasePresent, "SESSION_RUNTIME_LEASE_MISSING")
		add(snapshot.RuntimeLeasePresent && !snapshot.RuntimeLeasePristine, "SESSION_RUNTIME_LEASE_NOT_PRISTINE")
		if snapshot.InputPresent {
			add(snapshot.InputStatus != "pending", "INPUT_STATUS_UNSUPPORTED")
			add(snapshot.InputAttempts != 0, "INPUT_ATTEMPTS_NONZERO")
			add(snapshot.InputLeasePresent, "INPUT_LEASE_PRESENT")
			add(snapshot.InputFence != 0, "INPUT_FENCE_NONZERO")
			add(snapshot.InputSource != "user_message", "INPUT_SOURCE_UNSUPPORTED")
			add(!snapshot.InputMessageRefPresent, "INPUT_MESSAGE_REF_MISSING")
			if snapshot.InputMessageRefPresent {
				add(!snapshot.MessagePresent, "MESSAGE_MISSING")
				if snapshot.MessagePresent {
					add(!snapshot.MessageSessionMatches, "MESSAGE_SESSION_MISMATCH")
					add(!snapshot.MessageSourceMatches, "MESSAGE_SOURCE_MISMATCH")
					add(!snapshot.MessageRoleSupported, "MESSAGE_ROLE_UNSUPPORTED")
					add(!snapshot.MessageEnvelopeValid, "MESSAGE_ENVELOPE_INVALID")
					add(!snapshot.MessageDigestValid, "MESSAGE_DIGEST_INVALID")
					add(snapshot.MessageContentState == "unverified", "MESSAGE_CONTENT_UNVERIFIED")
					add(strings.HasPrefix(snapshot.MessageContentState, "unreadable_"), "MESSAGE_CONTENT_UNREADABLE")
				}
			}
			add(snapshot.AcceptedEventCount == 0 || (snapshot.AcceptedEventCount == 1 &&
				(snapshot.AcceptedEventSeq < snapshot.EventMinAvailableSeq || snapshot.AcceptedEventSeq > snapshot.EventLastSeq)), "ACCEPTED_EVENT_MISSING")
			add(snapshot.AcceptedEventCount > 1, "ACCEPTED_EVENT_AMBIGUOUS")
			add(snapshot.AcceptedEventCount == 1 && !snapshot.AcceptedEventMatches, "ACCEPTED_EVENT_MISMATCH")
		}
		add(snapshot.SkillRuntimeContentState == "unverified", "SKILL_RUNTIME_UNVERIFIED")
		add(strings.HasPrefix(snapshot.SkillRuntimeContentState, "unreadable_"), "SKILL_RUNTIME_UNREADABLE")
	}
	if (inputScope && snapshot.InputPresent) || rootScope {
		add(!snapshot.ReceiptPresent, "RECEIPT_MISSING")
		if snapshot.ReceiptPresent {
			add(!snapshot.ReceiptTypeSupported, "RECEIPT_TYPE_UNSUPPORTED")
			add(!snapshot.ReceiptResultVersionMatches, "RECEIPT_RESULT_VERSION_MISMATCH")
			add(!snapshot.ReceiptSessionMatches, "RECEIPT_SESSION_MISMATCH")
			add(!snapshot.ReceiptMessageMatches, "RECEIPT_MESSAGE_MISMATCH")
			add(!snapshot.ReceiptInputMatches, "RECEIPT_INPUT_MISMATCH")
			add(!snapshot.ReceiptDigestValid, "RECEIPT_DIGEST_INVALID")
			add(!snapshot.EnsureCanonicalDigestMatches, "ENSURE_CANONICAL_DIGEST_MISMATCH")
			add(!snapshot.ReceiptSkillMatches, "RECEIPT_SKILL_MISMATCH")
			add(!snapshot.ReceiptImmutable, "RECEIPT_NOT_IMMUTABLE")
		}
	}
	if rootScope {
		add(!snapshot.SessionReceiptPresent, "SESSION_RECEIPT_MISSING")
		add(!snapshot.SkillSnapshotPresent, "SESSION_SKILL_SNAPSHOT_MISSING")
		add(!snapshot.SequenceCounterPresent, "SESSION_SEQUENCE_COUNTER_MISSING")
		add(!snapshot.EventCounterPresent, "SESSION_EVENT_COUNTER_MISSING")
		add(!snapshot.CreatedEventPresent || (snapshot.CreatedEventPresent &&
			(snapshot.CreatedEventSeq < snapshot.EventMinAvailableSeq || snapshot.CreatedEventSeq > snapshot.EventLastSeq)), "SESSION_CREATED_EVENT_MISSING")
		add(snapshot.CreatedEventPresent && !snapshot.CreatedEventMatches, "SESSION_CREATED_EVENT_MISMATCH")
		add(!snapshot.RuntimeLeasePresent, "SESSION_RUNTIME_LEASE_MISSING")
		add(!snapshot.ReceiptMessageTargetPresent, "SESSION_RECEIPT_MESSAGE_TARGET_MISSING")
		add(!snapshot.ReceiptInputTargetPresent, "SESSION_RECEIPT_INPUT_TARGET_MISSING")
		add(snapshot.UnclaimedMessageCount > 0, "SESSION_UNCLAIMED_MESSAGE")
		add(snapshot.UnclaimedInputCount > 0, "SESSION_UNCLAIMED_INPUT")
		add(snapshot.UnclaimedEventCount > 0, "SESSION_UNCLAIMED_EVENT")
	}
	if scope == "full" {
		add(snapshot.RunCount > 0, "RUN_ALREADY_EXISTS")
		if phase == "preflight" || !snapshot.InputPresent {
			add(snapshot.AuthorityAttestationCount > 0, "AUTHORITY_ATTESTATION_CONFLICT")
			add(snapshot.TurnCount > 0, "TURN_CONFLICT")
			add(snapshot.UpgradeLedgerCount > 0 || !snapshot.UpgradeLedgerMatches, "UPGRADE_LEDGER_CONFLICT")
		} else {
			add(snapshot.AuthorityAttestationCount == 0, "AUTHORITY_ATTESTATION_MISSING")
			add(snapshot.AuthorityAttestationCount > 1 || !snapshot.AuthorityAttestationMatches, "AUTHORITY_ATTESTATION_CONFLICT")
			add(!snapshot.AuthorityPolicyAllowed, "AUTHORITY_POLICY_FORBIDDEN")
			add(snapshot.TurnCount == 0, "TURN_MISSING")
			add(snapshot.TurnCount > 1 || !snapshot.TurnMatches, "TURN_CONFLICT")
			add(snapshot.UpgradeLedgerCount != 1 || !snapshot.UpgradeLedgerMatches, "UPGRADE_LEDGER_CONFLICT")
		}
	}
	if orphanScope {
		add(snapshot.OrphanReceiptCount > 0, "ORPHAN_RECEIPT_SESSION")
		add(snapshot.OrphanMessageCount > 0, "ORPHAN_MESSAGE_SESSION")
		add(snapshot.OrphanInputCount > 0, "ORPHAN_INPUT_SESSION")
		add(snapshot.OrphanEventCount > 0, "ORPHAN_EVENT_SESSION")
		add(snapshot.OrphanSkillSnapshotHeaderCount > 0, "ORPHAN_SKILL_SNAPSHOT_HEADER_SESSION")
		add(snapshot.OrphanSkillSnapshotItemCount > 0, "ORPHAN_SKILL_SNAPSHOT_ITEM_SESSION")
		add(snapshot.OrphanSequenceCounterCount > 0, "ORPHAN_SEQUENCE_COUNTER_SESSION")
		add(snapshot.OrphanEventCounterCount > 0, "ORPHAN_EVENT_COUNTER_SESSION")
		add(snapshot.OrphanRuntimeLeaseCount > 0, "ORPHAN_RUNTIME_LEASE_SESSION")
		add(snapshot.OrphanAuthorityCount > 0, "ORPHAN_AUTHORITY_SESSION")
		add(snapshot.OrphanUpgradeLedgerCount > 0, "ORPHAN_UPGRADE_LEDGER_SESSION")
		add(snapshot.OrphanTurnCount > 0, "ORPHAN_TURN_SESSION")
		add(snapshot.OrphanRunCount > 0, "ORPHAN_RUN_SESSION")
		add(snapshot.OrphanEnqueueResultCount > 0, "ORPHAN_ENQUEUE_RESULT_SESSION")
	}
	ordered := make([]string, 0, len(set))
	for _, code := range legacyBlockerReasonOrderV1() {
		if _, ok := set[code]; ok {
			ordered = append(ordered, code)
		}
	}
	return ordered
}

func applyLegacyBlockerMutationV1(snapshot *legacyUpgradeSnapshotV1, mutation string) {
	switch mutation {
	case "authority_missing":
		snapshot.AuthorityAttestationCount = 0
	case "authority_conflict":
		snapshot.AuthorityAttestationMatches = false
	case "authority_policy_forbidden":
		snapshot.AuthorityPolicyAllowed = false
	case "turn_missing":
		snapshot.TurnCount = 0
	case "turn_conflict":
		snapshot.TurnMatches = false
	case "run_exists":
		snapshot.RunCount = 1
	case "ledger_conflict":
		snapshot.UpgradeLedgerMatches = false
	case "session_missing":
		snapshot.SessionPresent = false
	case "session_not_active":
		snapshot.SessionActive = false
	case "session_version_unexpected":
		snapshot.SessionVersionExpected = false
	case "snapshot_missing":
		snapshot.SkillSnapshotPresent = false
	case "snapshot_invalid":
		snapshot.SkillSnapshotValid = false
	case "item_count_mismatch":
		snapshot.SkillItemCountMatches = false
	case "item_order_gap":
		snapshot.SkillItemOrderDense = false
	case "sequence_counter_missing":
		snapshot.SequenceCounterPresent = false
	case "message_sequence_gap":
		snapshot.MessageSequenceDense = false
	case "input_sequence_gap":
		snapshot.InputSequenceDense = false
	case "event_counter_missing":
		snapshot.EventCounterPresent = false
	case "event_sequence_gap":
		snapshot.EventSequenceDense = false
	case "runtime_lease_missing":
		snapshot.RuntimeLeasePresent = false
	case "runtime_lease_not_pristine":
		snapshot.RuntimeLeasePristine = false
	case "input_status_unsupported":
		snapshot.InputStatus = "claimed"
	case "attempts_nonzero":
		snapshot.InputAttempts = 1
	case "input_lease_present":
		snapshot.InputLeasePresent = true
	case "input_fence_nonzero":
		snapshot.InputFence = 1
	case "source_unsupported":
		snapshot.InputSource = "future_source"
	case "message_ref_missing":
		snapshot.InputMessageRefPresent = false
	case "message_missing":
		snapshot.MessagePresent = false
	case "message_session_mismatch":
		snapshot.MessageSessionMatches = false
	case "message_source_mismatch":
		snapshot.MessageSourceMatches = false
	case "role_unsupported":
		snapshot.MessageRoleSupported = false
	case "envelope_invalid":
		snapshot.MessageEnvelopeValid = false
	case "digest_invalid":
		snapshot.MessageDigestValid = false
	case "receipt_missing":
		snapshot.ReceiptPresent = false
	case "receipt_type_unsupported":
		snapshot.ReceiptTypeSupported = false
	case "receipt_result_version_mismatch":
		snapshot.ReceiptResultVersionMatches = false
	case "receipt_session_mismatch":
		snapshot.ReceiptSessionMatches = false
	case "receipt_message_mismatch":
		snapshot.ReceiptMessageMatches = false
	case "receipt_input_mismatch":
		snapshot.ReceiptInputMatches = false
	case "receipt_digest_invalid":
		snapshot.ReceiptDigestValid = false
	case "ensure_canonical_digest_mismatch":
		snapshot.EnsureCanonicalDigestMatches = false
	case "receipt_skill_mismatch":
		snapshot.ReceiptSkillMatches = false
	case "receipt_not_immutable":
		snapshot.ReceiptImmutable = false
	case "accepted_event_missing":
		snapshot.AcceptedEventCount = 0
	case "accepted_event_ambiguous":
		snapshot.AcceptedEventCount = 2
	case "accepted_event_mismatch":
		snapshot.AcceptedEventMatches = false
	case "accepted_event_above_high_water":
		snapshot.AcceptedEventSeq = snapshot.EventLastSeq + 1
	case "session_receipt_missing":
		snapshot.SessionReceiptPresent = false
	case "created_event_missing":
		snapshot.CreatedEventPresent = false
	case "created_event_mismatch":
		snapshot.CreatedEventMatches = false
	case "created_event_above_high_water":
		snapshot.CreatedEventSeq = snapshot.EventLastSeq + 1
	case "authority_existing":
		snapshot.AuthorityAttestationCount = 1
	case "turn_existing":
		snapshot.TurnCount = 1
	case "ledger_existing":
		snapshot.UpgradeLedgerCount = 1
	case "receipt_message_target_missing":
		snapshot.ReceiptMessageTargetPresent = false
	case "receipt_input_target_missing":
		snapshot.ReceiptInputTargetPresent = false
	case "unclaimed_message":
		snapshot.UnclaimedMessageCount = 1
	case "unclaimed_input":
		snapshot.UnclaimedInputCount = 1
	case "unclaimed_event":
		snapshot.UnclaimedEventCount = 1
	case "orphan_receipt":
		snapshot.OrphanReceiptCount = 1
	case "orphan_message":
		snapshot.OrphanMessageCount = 1
	case "orphan_input":
		snapshot.OrphanInputCount = 1
	case "orphan_event":
		snapshot.OrphanEventCount = 1
	case "orphan_snapshot_header":
		snapshot.OrphanSkillSnapshotHeaderCount = 1
	case "orphan_snapshot_item":
		snapshot.OrphanSkillSnapshotItemCount = 1
	case "orphan_sequence_counter":
		snapshot.OrphanSequenceCounterCount = 1
	case "orphan_event_counter":
		snapshot.OrphanEventCounterCount = 1
	case "orphan_runtime_lease":
		snapshot.OrphanRuntimeLeaseCount = 1
	case "orphan_authority":
		snapshot.OrphanAuthorityCount = 1
	case "orphan_upgrade_ledger":
		snapshot.OrphanUpgradeLedgerCount = 1
	case "orphan_turn":
		snapshot.OrphanTurnCount = 1
	case "orphan_run":
		snapshot.OrphanRunCount = 1
	case "orphan_enqueue_result":
		snapshot.OrphanEnqueueResultCount = 1
	case "message_active_key":
		snapshot.MessageContentState = "verified_active_key"
	case "message_previous_key":
		snapshot.MessageContentState = "verified_previous_key"
	case "skill_mixed_keys":
		snapshot.SkillRuntimeContentState = "verified_previous_key"
	case "message_unverified":
		snapshot.MessageContentState = "unverified"
	case "message_unknown_key":
		snapshot.MessageContentState = "unreadable_unknown_key"
	case "message_auth_or_digest_corrupt":
		snapshot.MessageContentState = "unreadable_auth_or_digest"
	case "skill_unverified":
		snapshot.SkillRuntimeContentState = "unverified"
	case "skill_unknown_key":
		snapshot.SkillRuntimeContentState = "unreadable_unknown_key"
	case "skill_auth_or_digest_corrupt":
		snapshot.SkillRuntimeContentState = "unreadable_auth_or_digest"
	default:
		panic("unknown blocker mutation: " + mutation)
	}
}

func legacyBlockerReasonOrderV1() []string {
	return []string{
		"SESSION_MISSING", "SESSION_NOT_ACTIVE", "SESSION_VERSION_UNEXPECTED", "SESSION_RECEIPT_MISSING",
		"SESSION_SKILL_SNAPSHOT_MISSING", "SESSION_SKILL_SNAPSHOT_INVALID", "SESSION_SKILL_ITEM_COUNT_MISMATCH",
		"SESSION_SKILL_ITEM_ORDER_GAP", "SESSION_SEQUENCE_COUNTER_MISSING", "SESSION_MESSAGE_SEQUENCE_GAP",
		"SESSION_INPUT_SEQUENCE_GAP", "SESSION_EVENT_COUNTER_MISSING", "SESSION_EVENT_SEQUENCE_GAP",
		"SESSION_CREATED_EVENT_MISSING", "SESSION_CREATED_EVENT_MISMATCH", "SESSION_RUNTIME_LEASE_MISSING",
		"SESSION_RUNTIME_LEASE_NOT_PRISTINE", "SESSION_RECEIPT_MESSAGE_TARGET_MISSING",
		"SESSION_RECEIPT_INPUT_TARGET_MISSING", "SESSION_UNCLAIMED_MESSAGE", "SESSION_UNCLAIMED_INPUT",
		"SESSION_UNCLAIMED_EVENT", "INPUT_STATUS_UNSUPPORTED", "INPUT_ATTEMPTS_NONZERO", "INPUT_LEASE_PRESENT",
		"INPUT_FENCE_NONZERO", "INPUT_SOURCE_UNSUPPORTED", "INPUT_MESSAGE_REF_MISSING", "MESSAGE_MISSING",
		"MESSAGE_SESSION_MISMATCH", "MESSAGE_SOURCE_MISMATCH", "MESSAGE_ROLE_UNSUPPORTED", "MESSAGE_ENVELOPE_INVALID",
		"MESSAGE_DIGEST_INVALID", "MESSAGE_CONTENT_UNVERIFIED", "MESSAGE_CONTENT_UNREADABLE", "RECEIPT_MISSING",
		"RECEIPT_TYPE_UNSUPPORTED", "RECEIPT_RESULT_VERSION_MISMATCH", "RECEIPT_SESSION_MISMATCH",
		"RECEIPT_MESSAGE_MISMATCH", "RECEIPT_INPUT_MISMATCH", "RECEIPT_DIGEST_INVALID",
		"ENSURE_CANONICAL_DIGEST_MISMATCH", "RECEIPT_SKILL_MISMATCH", "RECEIPT_NOT_IMMUTABLE",
		"ACCEPTED_EVENT_MISSING", "ACCEPTED_EVENT_AMBIGUOUS", "ACCEPTED_EVENT_MISMATCH", "SKILL_RUNTIME_UNVERIFIED",
		"SKILL_RUNTIME_UNREADABLE", "AUTHORITY_ATTESTATION_MISSING", "AUTHORITY_ATTESTATION_CONFLICT",
		"AUTHORITY_POLICY_FORBIDDEN", "TURN_MISSING", "TURN_CONFLICT", "RUN_ALREADY_EXISTS", "UPGRADE_LEDGER_CONFLICT",
		"ORPHAN_RECEIPT_SESSION", "ORPHAN_MESSAGE_SESSION", "ORPHAN_INPUT_SESSION", "ORPHAN_EVENT_SESSION",
		"ORPHAN_SKILL_SNAPSHOT_HEADER_SESSION", "ORPHAN_SKILL_SNAPSHOT_ITEM_SESSION", "ORPHAN_SEQUENCE_COUNTER_SESSION",
		"ORPHAN_EVENT_COUNTER_SESSION", "ORPHAN_RUNTIME_LEASE_SESSION", "ORPHAN_AUTHORITY_SESSION",
		"ORPHAN_UPGRADE_LEDGER_SESSION", "ORPHAN_TURN_SESSION", "ORPHAN_RUN_SESSION", "ORPHAN_ENQUEUE_RESULT_SESSION",
	}
}

func legacyAuthorityRequiredCaseIDsV1() []string {
	return []string{
		"AUTH-01-P01-v1-golden", "AUTH-01-P02-v2-empty-golden", "AUTH-01-P03-v2-published-golden",
		"AUTH-01-P04-replay-stable-id-excluded", "AUTH-01-N01-invalid-schema",
		"AUTH-01-N02-source-contract-type-version-mismatch", "AUTH-01-N03-ensure-request-digest-mismatch",
		"AUTH-01-N04-session-project-owner-binding-mismatch", "AUTH-01-N05-message-input-content-binding-mismatch",
		"AUTH-01-N06-skill-binding-mismatch", "AUTH-01-N07-persisted-canonical-field-tamper",
		"AUTH-02-P01-provenance-chat-only", "AUTH-02-N01-authenticated-user-kind-forbidden",
		"AUTH-02-N02-authenticated-evidence-level-forbidden", "AUTH-02-N03-receipt-mutable",
		"AUTH-02-N04-sensitive-capability", "AUTH-02-N05-multi-order",
	}
}

func legacyBlockerRequiredCaseIDsV1() []string {
	return []string{
		"UPG-01-P01-v1-prompt-preflight", "UPG-01-P02-v2-empty-prompt-preflight",
		"UPG-01-P03-v2-published-prompt-preflight", "UPG-01-P04-empty-prompt-legal",
		"UPG-01-P05-backfilled-verify", "UPG-01-P06-backfill-replay-stable",
		"UPG-01-N01-authority-missing", "UPG-01-N02-authority-conflict", "UPG-01-N03-authority-policy",
		"UPG-01-N04-turn-missing", "UPG-01-N05-turn-conflict", "UPG-01-N06-run-exists", "UPG-01-N07-ledger-conflict",
		"UPG-02-N01-session-missing", "UPG-02-N02-session-not-active", "UPG-02-N03-session-version-unexpected",
		"UPG-02-N04-snapshot-missing", "UPG-02-N05-snapshot-invalid", "UPG-02-N06-item-count-mismatch",
		"UPG-02-N07-item-order-gap", "UPG-02-N08-sequence-counter-missing", "UPG-02-N09-message-sequence-gap",
		"UPG-02-N10-input-sequence-gap", "UPG-02-N11-event-counter-missing", "UPG-02-N12-event-sequence-gap",
		"UPG-02-N13-runtime-lease-missing", "UPG-02-N14-runtime-lease-not-pristine",
		"UPG-02-N15-input-status-unsupported", "UPG-02-N16-attempts-nonzero", "UPG-02-N17-input-lease-present",
		"UPG-02-N18-input-fence-nonzero", "UPG-02-N19-source-unsupported", "UPG-02-N20-message-ref-missing",
		"UPG-02-N21-message-missing", "UPG-02-N22-message-session-mismatch", "UPG-02-N23-message-source-mismatch",
		"UPG-02-N24-role-unsupported", "UPG-02-N25-envelope-invalid", "UPG-02-N26-digest-invalid",
		"UPG-02-N27-receipt-missing", "UPG-02-N28-receipt-type-unsupported",
		"UPG-02-N29-receipt-result-version-mismatch", "UPG-02-N30-receipt-session-mismatch",
		"UPG-02-N31-receipt-message-mismatch", "UPG-02-N32-receipt-input-mismatch",
		"UPG-02-N33-receipt-digest-invalid", "UPG-02-N34-ensure-canonical-digest-mismatch",
		"UPG-02-N35-receipt-skill-mismatch", "UPG-02-N36-receipt-not-immutable",
		"UPG-02-N37-accepted-event-missing", "UPG-02-N38-accepted-event-ambiguous",
		"UPG-02-N39-accepted-event-mismatch", "UPG-02-N40-multi-row-order",
		"UPG-02-N41-accepted-event-above-high-water",
		"UPG-03-P01-empty-prompt-root-legal", "UPG-03-N01-session-receipt-missing",
		"UPG-03-N02-created-event-missing", "UPG-03-N03-created-event-mismatch",
		"UPG-03-N04-receipt-message-target-missing", "UPG-03-N05-receipt-input-target-missing",
		"UPG-03-N06-unclaimed-message", "UPG-03-N07-unclaimed-input", "UPG-03-N08-unclaimed-event",
		"UPG-03-N09-orphan-receipt-session", "UPG-03-N10-orphan-message-session",
		"UPG-03-N11-orphan-input-session", "UPG-03-N12-orphan-event-session", "UPG-03-N13-multi-root-order",
		"UPG-03-N14-orphan-snapshot-header-session", "UPG-03-N15-orphan-snapshot-item-session",
		"UPG-03-N16-orphan-sequence-counter-session", "UPG-03-N17-orphan-event-counter-session",
		"UPG-03-N18-orphan-runtime-lease-session", "UPG-03-N19-orphan-authority-session",
		"UPG-03-N20-orphan-upgrade-ledger-session", "UPG-03-N21-orphan-turn-session",
		"UPG-03-N22-orphan-run-session", "UPG-03-N23-orphan-enqueue-result-session",
		"UPG-03-N24-empty-prompt-receipt-invalid", "UPG-03-N25-empty-prompt-existing-facts",
		"UPG-03-N26-created-event-above-high-water",
		"UPG-04-P01-active-key-message-valid", "UPG-04-P02-previous-key-message-valid",
		"UPG-04-P03-published-skill-mixed-active-previous-valid", "UPG-04-N01-message-unverified",
		"UPG-04-N02-message-unknown-key", "UPG-04-N03-message-aead-tag-or-digest-corrupt",
		"UPG-04-N04-skill-unverified", "UPG-04-N05-skill-unknown-key", "UPG-04-N06-skill-aad-tag-or-digest-corrupt",
	}
}

func init() {
	// 固定 reason registry 中不允许重复；实际排序只由显式 rank 决定。
	reasons := legacyBlockerReasonOrderV1()
	sorted := append([]string(nil), reasons...)
	sort.Strings(sorted)
	for index := 1; index < len(sorted); index++ {
		if sorted[index] == sorted[index-1] {
			panic("duplicate legacy blocker reason: " + sorted[index])
		}
	}
}

func legacyUpgradeLedgerTransitionV1(from, to string) bool {
	switch from + "->" + to {
	case "->blocked", "->prepared", "prepared->applied", "applied->verified":
		return true
	default:
		return false
	}
}

func applyLegacyUpgradeTransactionV1(
	before legacyUpgradeAppliedV1,
	ledger legacyUpgradeLedgerV1,
	crashPoint string,
) (legacyUpgradeAppliedV1, bool, error) {
	if ledger.LedgerKey == "" || ledger.UpgradeGeneration <= 0 || ledger.State != "prepared" ||
		ledger.FactsDigest == "" || ledger.PlanDigest == "" || ledger.TargetInputDigest == "" || ledger.TurnContextDigest == "" ||
		ledger.AuthorityAttestationID == "" || ledger.AuthorityDigest == "" || ledger.TurnID == "" ||
		ledger.ContextMessageSeq <= 0 || ledger.ContextMessageSeq != ledger.LockedLastMessageSeq {
		return before, false, fmt.Errorf("legacy upgrade prepared ledger invalid")
	}
	if before.InputPatched {
		if (before.LedgerState != "applied" && before.LedgerState != "verified") ||
			before.LedgerKey != ledger.LedgerKey || before.UpgradeGeneration != ledger.UpgradeGeneration ||
			before.FactsDigest != ledger.FactsDigest || before.PlanDigest != ledger.PlanDigest ||
			before.TargetInputDigest != ledger.TargetInputDigest || before.TurnContextDigest != ledger.TurnContextDigest ||
			before.AuthorityAttestationID != ledger.AuthorityAttestationID || before.AuthorityDigest != ledger.AuthorityDigest ||
			before.TurnID != ledger.TurnID || before.ContextMessageSeq != ledger.ContextMessageSeq || before.RunCount != 0 {
			return before, false, fmt.Errorf("legacy upgrade replay conflict")
		}
		return before, false, nil
	}
	if before.RunCount != 0 || before.AuthorityAttestationID != "" || before.TurnID != "" || before.LedgerState != "" {
		return before, false, fmt.Errorf("legacy upgrade fresh apply conflicts with existing facts")
	}
	after := before
	after.InputPatched = true
	after.LedgerKey = ledger.LedgerKey
	after.UpgradeGeneration = ledger.UpgradeGeneration
	after.LedgerState = "applied"
	after.FactsDigest = ledger.FactsDigest
	after.PlanDigest = ledger.PlanDigest
	after.TargetInputDigest = ledger.TargetInputDigest
	after.TurnContextDigest = ledger.TurnContextDigest
	if crashPoint == "after_input_patch" {
		return before, false, fmt.Errorf("injected crash after input patch")
	}
	after.AuthorityAttestationID = ledger.AuthorityAttestationID
	after.AuthorityDigest = ledger.AuthorityDigest
	after.TurnID = ledger.TurnID
	after.ContextMessageSeq = ledger.ContextMessageSeq
	if crashPoint == "after_turn_insert" {
		return before, false, fmt.Errorf("injected crash after turn insert")
	}
	if crashPoint == "after_apply_commit" {
		return after, true, fmt.Errorf("injected response loss after apply commit")
	}
	if crashPoint != "" {
		return before, false, fmt.Errorf("unknown crash point")
	}
	return after, true, nil
}

func verifyLegacyUpgradeV1(
	before legacyUpgradeAppliedV1,
	ledger legacyUpgradeLedgerV1,
	crashPoint string,
) (legacyUpgradeAppliedV1, bool, error) {
	if !before.InputPatched || before.RunCount != 0 || ledger.State != "prepared" ||
		before.LedgerKey != ledger.LedgerKey || before.UpgradeGeneration != ledger.UpgradeGeneration ||
		before.FactsDigest != ledger.FactsDigest || before.PlanDigest != ledger.PlanDigest ||
		before.TargetInputDigest != ledger.TargetInputDigest || before.TurnContextDigest != ledger.TurnContextDigest ||
		before.AuthorityAttestationID != ledger.AuthorityAttestationID || before.AuthorityDigest != ledger.AuthorityDigest ||
		before.TurnID != ledger.TurnID || before.ContextMessageSeq != ledger.ContextMessageSeq ||
		before.ContextMessageSeq != ledger.LockedLastMessageSeq {
		return before, false, fmt.Errorf("legacy upgrade verify integrity violation")
	}
	if before.LedgerState == "verified" {
		if crashPoint != "" {
			return before, false, fmt.Errorf("unknown verify crash point")
		}
		return before, false, nil
	}
	if before.LedgerState != "applied" {
		return before, false, fmt.Errorf("legacy upgrade verify state violation")
	}
	after := before
	after.LedgerState = "verified"
	if crashPoint == "after_verify_commit" {
		return after, true, fmt.Errorf("injected response loss after verify commit")
	}
	if crashPoint != "" {
		return before, false, fmt.Errorf("unknown verify crash point")
	}
	return after, true, nil
}

func calculateLegacyLaneReadinessV1(value legacyLaneReadinessV1) legacyLaneReadinessResultV1 {
	laneReady := value.FoundationReady && value.LaneSchemaReady && value.CompatibleWriterReady &&
		value.LegacyClassificationComplete && value.LegacyBlockerCount == 0 && value.EligibleUnverifiedCount == 0 &&
		value.ActiveUpgradeClaims == 0 && value.ActivationPoliciesReady && value.UpgradeEvidenceApproved &&
		value.CapabilityState == "ready" && value.UpgradeGeneration > 0
	processorReady := laneReady && value.ProcessorEnabled
	return legacyLaneReadinessResultV1{
		FoundationReady: value.FoundationReady, LaneCapabilityReady: laneReady,
		ProcessorReady: processorReady, ClaimAllowed: processorReady && value.ClaimGeneration == value.UpgradeGeneration,
	}
}

func legacyUpgradeDownAllowedV1(value legacyDownFactsV1) bool {
	return value.CompatibleWritersStopped && value.ProcessorStopped && value.ScannerStopped && value.MigrationFenceHeld &&
		value.EnqueueHeaderCount == 0 && value.AliasReceiptCount == 0 &&
		value.OriginResultCount == 0 && value.AuthoritySnapshotCount == 0 && value.LegacyTurnCount == 0 &&
		value.RunCount == 0 && value.NewStateFactCount == 0 && value.ExpandedFactCount == 0 &&
		value.TurnContextCount == 0 && value.UpgradeLedgerCount == 0
}

func containsLegacyReasonV1(reasons []string, target string) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}
