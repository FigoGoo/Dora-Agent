package analyzematerials

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeIntentStrictBoundaries(t *testing.T) {
	t.Parallel()
	valid := `{"schema_version":"` + IntentSchemaVersion + `","asset_ids":["` + graphTestAssetID +
		`"],"analysis_goal":"分析素材主题","focus_dimensions":["content"],"output_language":"zh-CN"}`
	if _, err := DecodeIntent([]byte(valid)); err != nil {
		t.Fatalf("合法 Intent 被拒绝: %v", err)
	}
	multiline := strings.Replace(valid, "分析素材主题", " 分析第一行\\n第二行\\t重点 ", 1)
	if _, err := DecodeIntent([]byte(multiline)); err != nil {
		t.Fatalf("NFC 多行自然文本被拒绝: %v", err)
	}
	for _, testCase := range []struct {
		name string
		raw  string
	}{
		{name: "unknown root", raw: strings.Replace(valid, `"analysis_goal"`, `"forged_user_id":"x","analysis_goal"`, 1)},
		{name: "duplicate root", raw: strings.Replace(valid, `"analysis_goal":"分析素材主题"`, `"analysis_goal":"A","analysis_goal":"B"`, 1)},
		{name: "null", raw: strings.Replace(valid, `"analysis_goal":"分析素材主题"`, `"analysis_goal":null`, 1)},
		{name: "trailing", raw: valid + `{}`},
		{name: "uppercase UUID", raw: strings.Replace(valid, graphTestAssetID, strings.ToUpper(graphTestAssetID), 1)},
		{name: "duplicate asset", raw: strings.Replace(valid, `"`+graphTestAssetID+`"],`, `"`+graphTestAssetID+`","`+graphTestAssetID+`"],`, 1)},
		{name: "unknown nested", raw: strings.TrimSuffix(valid, `}`) + `,"expected_assets":[{"asset_id":"` + graphTestAssetID + `","asset_version":1,"owner":"forged"}]}`},
		{name: "expected set mismatch", raw: strings.TrimSuffix(valid, `}`) + `,"expected_assets":[{"asset_id":"019b78a0-22b3-7b12-8a12-121212121212","asset_version":1}]}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeIntent([]byte(testCase.raw)); err == nil || ErrorResultCode(err) != ResultCodeInvalidArgument {
				t.Fatalf("DecodeIntent(%s) error=%v code=%q", testCase.name, err, ErrorResultCode(err))
			}
		})
	}
}

func TestIntentDigestUsesSetSemantics(t *testing.T) {
	t.Parallel()
	assetB := "019b78a0-22b3-7b12-8a12-121212121212"
	first := graphTestIntent("risk", "content")
	first.AssetIDs = []string{assetB, graphTestAssetID}
	first.ExpectedAssets = []ExpectedAsset{{AssetID: graphTestAssetID, AssetVersion: 1}, {AssetID: assetB, AssetVersion: 2}}
	second := first
	second.AssetIDs = []string{graphTestAssetID, assetB}
	second.FocusDimensions = []string{"content", "risk"}
	second.ExpectedAssets = []ExpectedAsset{{AssetID: assetB, AssetVersion: 2}, {AssetID: graphTestAssetID, AssetVersion: 1}}
	firstDigest, err := IntentDigest(first)
	if err != nil {
		t.Fatalf("IntentDigest(first) error=%v", err)
	}
	secondDigest, err := IntentDigest(second)
	if err != nil {
		t.Fatalf("IntentDigest(second) error=%v", err)
	}
	if firstDigest != secondDigest || len(firstDigest) != 64 {
		t.Fatalf("集合重排 digest=%q/%q", firstDigest, secondDigest)
	}
	second.AnalysisGoal = "不同目标"
	changed, err := IntentDigest(second)
	if err != nil || changed == firstDigest {
		t.Fatalf("语义变化 digest=%q error=%v", changed, err)
	}
}

func TestEvidenceSnapshotExactSetAndLocatorFailClosed(t *testing.T) {
	t.Parallel()
	intent := graphTestIntent("content")
	intent.ExpectedAssets = []ExpectedAsset{{AssetID: graphTestAssetID, AssetVersion: 1}}
	query, err := BuildEvidenceQuery(graphTestTrustedContext(), intent)
	if err != nil {
		t.Fatalf("BuildEvidenceQuery() error=%v", err)
	}
	valid := graphTestSnapshot("ready")
	if err := ValidateEvidenceSnapshot(query, valid); err != nil {
		t.Fatalf("合法 Snapshot 被拒绝: %v", err)
	}
	validRegion := cloneEvidenceSnapshot(valid)
	validRegion.Assets[0].Evidence[0].Locator = EvidenceLocator{Kind: "image_region", X: 5, Y: 10, Width: 20, Height: 30}
	if err := ValidateEvidenceSnapshot(query, validRegion); err != nil {
		t.Fatalf("合法 image_region 被拒绝: %v", err)
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*EvidenceSnapshot)
		code   string
	}{
		{name: "incomplete", code: ResultCodeSnapshotInvalid, mutate: func(value *EvidenceSnapshot) { value.ResponseComplete = false }},
		{name: "missing asset", code: ResultCodeSnapshotInvalid, mutate: func(value *EvidenceSnapshot) { value.Assets = nil }},
		{name: "extra asset", code: ResultCodeSnapshotInvalid, mutate: func(value *EvidenceSnapshot) { value.Assets = append(value.Assets, value.Assets[0]) }},
		{name: "expected version drift", code: ResultCodeSnapshotInvalid, mutate: func(value *EvidenceSnapshot) {
			value.Assets[0].AssetVersion = 2
			value.Assets[0].Evidence[0].AssetVersion = 2
		}},
		{name: "duplicate evidence", code: ResultCodeEvidenceConflict, mutate: func(value *EvidenceSnapshot) {
			value.Assets[0].Evidence = append(value.Assets[0].Evidence, value.Assets[0].Evidence[0])
		}},
		{name: "evidence limit", code: ResultCodeSnapshotInvalid, mutate: func(value *EvidenceSnapshot) {
			for len(value.Assets[0].Evidence) <= maxEvidence {
				value.Assets[0].Evidence = append(value.Assets[0].Evidence, value.Assets[0].Evidence[0])
			}
		}},
		{name: "evidence owner conflict", code: ResultCodeEvidenceConflict, mutate: func(value *EvidenceSnapshot) { value.Assets[0].Evidence[0].AssetID = toolTestAssetID }},
		{name: "digest mismatch", code: ResultCodeEvidenceConflict, mutate: func(value *EvidenceSnapshot) { value.Assets[0].Evidence[0].ContentDigest = strings.Repeat("0", 64) }},
		{name: "invalid image locator", code: ResultCodeEvidenceConflict, mutate: func(value *EvidenceSnapshot) {
			value.Assets[0].Evidence[0].Locator = EvidenceLocator{Kind: "image_region", X: 9999, Y: 0, Width: 2, Height: 1}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			value := cloneEvidenceSnapshot(valid)
			testCase.mutate(&value)
			err := ValidateEvidenceSnapshot(query, value)
			if err == nil || ErrorResultCode(err) != testCase.code {
				t.Fatalf("ValidateEvidenceSnapshot() error=%v code=%q want=%q", err, ErrorResultCode(err), testCase.code)
			}
		})
	}
}

func TestTextEvidenceLocatorAndAllAvailabilityValues(t *testing.T) {
	t.Parallel()
	for _, availability := range []string{"ready", "missing", "failed", "redacted", "unsupported"} {
		availability := availability
		t.Run(availability, func(t *testing.T) {
			content := "一段经过授权的文字素材"
			evidence := EvidenceInput{
				EvidenceID: graphTestEvidenceID, AssetID: graphTestAssetID, AssetVersion: 1,
				MediaType: "text", EvidenceKind: "text_segment", Availability: availability,
				ExtractorSchemaVersion: "text-segment.v1", ExtractorVersion: "extractor.v1",
			}
			if availability == "ready" {
				evidence.Content = content
				evidence.ContentDigest = graphTestDigest(content)
				evidence.Locator = EvidenceLocator{Kind: "text_range", Start: 0, End: 12, SourceLength: 12}
			} else {
				evidence.ReasonCode = "EVIDENCE_" + strings.ToUpper(availability)
			}
			snapshot := EvidenceSnapshot{
				SchemaVersion: EvidenceSnapshotSchemaVersion, SnapshotToken: "snapshot-text", ResponseComplete: true,
				Assets: []AssetAnalysisInput{{AssetID: graphTestAssetID, AssetVersion: 1, MediaType: "text", Evidence: []EvidenceInput{evidence}}},
			}
			intent := graphTestIntent("content")
			query, err := BuildEvidenceQuery(graphTestTrustedContext(), intent)
			if err != nil {
				t.Fatalf("BuildEvidenceQuery() error=%v", err)
			}
			if err := ValidateEvidenceSnapshot(query, snapshot); err != nil {
				t.Fatalf("availability=%s Snapshot error=%v", availability, err)
			}
			_, ready, missing, err := NormalizeEvidence(intent, snapshot)
			if err != nil {
				t.Fatalf("NormalizeEvidence() error=%v", err)
			}
			if availability == "ready" {
				if len(ready) != 1 || len(missing) != 0 {
					t.Fatalf("ready/missing=%d/%d", len(ready), len(missing))
				}
			} else if len(ready) != 0 || len(missing) != 1 {
				t.Fatalf("availability=%s ready/missing=%d/%d", availability, len(ready), len(missing))
			}
		})
	}
}

func TestImageEvidenceSupportsAllAvailabilityValues(t *testing.T) {
	t.Parallel()
	for _, availability := range []string{"ready", "missing", "failed", "redacted", "unsupported"} {
		snapshot := graphTestSnapshot(availability)
		query, err := BuildEvidenceQuery(graphTestTrustedContext(), graphTestIntent("visual"))
		if err != nil {
			t.Fatalf("BuildEvidenceQuery() error=%v", err)
		}
		if err := ValidateEvidenceSnapshot(query, snapshot); err != nil {
			t.Fatalf("availability=%s snapshot error=%v", availability, err)
		}
		_, ready, missing, err := NormalizeEvidence(graphTestIntent("visual"), snapshot)
		if err != nil {
			t.Fatalf("availability=%s NormalizeEvidence() error=%v", availability, err)
		}
		if availability == "ready" {
			if len(ready) != 1 || len(missing) != 0 {
				t.Fatalf("ready/missing=%d/%d", len(ready), len(missing))
			}
		} else if len(ready) != 0 || len(missing) != 1 {
			t.Fatalf("availability=%s ready/missing=%d/%d", availability, len(ready), len(missing))
		}
	}
}

func TestPromptBudgetExclusionIsStableAndForcesPartial(t *testing.T) {
	t.Parallel()
	ids := []string{
		"019b78a0-22b3-7b11-8a11-111111111110",
		"019b78a0-22b3-7b11-8a11-111111111111",
		"019b78a0-22b3-7b11-8a11-111111111112",
		"019b78a0-22b3-7b11-8a11-111111111113",
		"019b78a0-22b3-7b11-8a11-111111111114",
		"019b78a0-22b3-7b11-8a11-111111111115",
		"019b78a0-22b3-7b11-8a11-111111111116",
	}
	content := strings.Repeat("证", maxEvidenceRunes)
	evidence := make([]EvidenceInput, len(ids))
	for index, evidenceID := range ids {
		evidence[index] = EvidenceInput{
			EvidenceID: evidenceID, AssetID: graphTestAssetID, AssetVersion: 1,
			MediaType: "image", EvidenceKind: "visual_description", Availability: "ready",
			ExtractorSchemaVersion: "visual-description.v1", ExtractorVersion: "extractor.v1",
			Content: content, ContentDigest: graphTestDigest(content), Locator: EvidenceLocator{Kind: "image_whole"},
		}
	}
	first := EvidenceSnapshot{
		SchemaVersion: EvidenceSnapshotSchemaVersion, SnapshotToken: "budget-v1", ResponseComplete: true,
		Assets: []AssetAnalysisInput{{AssetID: graphTestAssetID, AssetVersion: 1, MediaType: "image", Evidence: evidence}},
	}
	second := cloneEvidenceSnapshot(first)
	for left, right := 0, len(second.Assets[0].Evidence)-1; left < right; left, right = left+1, right-1 {
		second.Assets[0].Evidence[left], second.Assets[0].Evidence[right] = second.Assets[0].Evidence[right], second.Assets[0].Evidence[left]
	}
	intent := graphTestIntent("visual")
	selectEvidence := func(snapshot EvidenceSnapshot) ([]evidenceUnit, []MissingRequirement, Coverage) {
		assets, ready, missing, err := NormalizeEvidence(intent, snapshot)
		if err != nil {
			t.Fatalf("NormalizeEvidence() error=%v", err)
		}
		included, missing, err := SelectPromptEvidence(intent, assets, ready, missing)
		if err != nil {
			t.Fatalf("SelectPromptEvidence() error=%v", err)
		}
		coverage, err := EvaluateCoverage(intent, assets, included, missing)
		if err != nil {
			t.Fatalf("EvaluateCoverage() error=%v", err)
		}
		return included, missing, coverage
	}
	firstIncluded, firstMissing, firstCoverage := selectEvidence(first)
	secondIncluded, secondMissing, secondCoverage := selectEvidence(second)
	if !reflect.DeepEqual(firstIncluded, secondIncluded) || !reflect.DeepEqual(firstMissing, secondMissing) ||
		!reflect.DeepEqual(firstCoverage, secondCoverage) {
		t.Fatal("预算选择随 Loader 输入顺序漂移")
	}
	if len(firstIncluded) != maxPromptEvidenceRunes/maxEvidenceRunes || len(firstMissing) != 1 ||
		firstMissing[0].ReasonCode != missingReasonBudgetTruncated || firstCoverage.Status != "partial" {
		t.Fatalf("included/missing/coverage=%d/%+v/%+v", len(firstIncluded), firstMissing, firstCoverage)
	}
	for _, unit := range firstIncluded {
		if unit.Content != content {
			t.Fatal("预算选择截断了 Evidence 正文")
		}
	}
}

func TestCoverageStatusIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		focus []string
		want  string
	}{
		{focus: []string{"content"}, want: "completed"},
		{focus: []string{"content", "risk"}, want: "partial"},
	} {
		intent := graphTestIntent(testCase.focus...)
		snapshot := graphTestSnapshot("ready")
		assets, ready, missing, err := NormalizeEvidence(intent, snapshot)
		if err != nil {
			t.Fatalf("NormalizeEvidence() error=%v", err)
		}
		included, missing, err := SelectPromptEvidence(intent, assets, ready, missing)
		if err != nil {
			t.Fatalf("SelectPromptEvidence() error=%v", err)
		}
		coverage, err := EvaluateCoverage(intent, assets, included, missing)
		if err != nil || coverage.Status != testCase.want {
			t.Fatalf("Coverage=%+v error=%v want=%s", coverage, err, testCase.want)
		}
	}
	intent := graphTestIntent("content")
	snapshot := graphTestSnapshot("missing")
	assets, ready, missing, err := NormalizeEvidence(intent, snapshot)
	if err != nil {
		t.Fatalf("NormalizeEvidence(missing) error=%v", err)
	}
	included, missing, err := SelectPromptEvidence(intent, assets, ready, missing)
	if err != nil {
		t.Fatalf("SelectPromptEvidence(missing) error=%v", err)
	}
	coverage, err := EvaluateCoverage(intent, assets, included, missing)
	if err != nil || coverage.Status != "failed" {
		t.Fatalf("missing Coverage=%+v error=%v", coverage, err)
	}
}

func TestCandidateStrictSchemaAndEvidenceComplement(t *testing.T) {
	t.Parallel()
	intent := graphTestIntent("content")
	assets, ready, missing, err := NormalizeEvidence(intent, graphTestSnapshot("ready"))
	if err != nil {
		t.Fatalf("NormalizeEvidence() error=%v", err)
	}
	included, missing, err := SelectPromptEvidence(intent, assets, ready, missing)
	if err != nil {
		t.Fatalf("SelectPromptEvidence() error=%v", err)
	}
	coverage, err := EvaluateCoverage(intent, assets, included, missing)
	if err != nil {
		t.Fatalf("EvaluateCoverage() error=%v", err)
	}
	validJSON := graphTestCandidateJSON(t)
	candidate, err := DecodeAndValidateCandidate([]byte(validJSON), intent, coverage, included, missing)
	if err != nil {
		t.Fatalf("合法 Candidate 被拒绝: %v", err)
	}
	digest, err := CandidateDigest(candidate)
	if err != nil || len(digest) != 64 {
		t.Fatalf("CandidateDigest=%q error=%v", digest, err)
	}

	unknownNested := strings.Replace(validJSON, `"observation_id"`, `"forged_status":"completed","observation_id"`, 1)
	if _, err := DecodeAndValidateCandidate([]byte(unknownNested), intent, coverage, included, missing); err == nil {
		t.Fatal("嵌套 unknown field 应被拒绝")
	}
	bad := graphTestCandidate()
	bad.AssetSummaries[0].Observations[0].EvidenceIDs = []string{"019b78a0-22b3-7b13-8a13-131313131313"}
	encoded, _ := json.Marshal(bad)
	if _, err := DecodeAndValidateCandidate(encoded, intent, coverage, included, missing); err == nil {
		t.Fatal("未知 Evidence 引用应被拒绝")
	}
	bad = graphTestCandidate()
	bad.UnusedEvidenceIDs = []string{graphTestEvidenceID}
	encoded, _ = json.Marshal(bad)
	if _, err := DecodeAndValidateCandidate(encoded, intent, coverage, included, missing); err == nil {
		t.Fatal("referenced 与 unused 交集应被拒绝")
	}
	bad = graphTestCandidate()
	bad.AssetSummaries = nil
	encoded, _ = json.Marshal(bad)
	if _, err := DecodeAndValidateCandidate(encoded, intent, coverage, included, missing); err == nil {
		t.Fatal("asset summary exact-set 缺失应被拒绝")
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*Candidate)
	}{
		{name: "invalid local id", mutate: func(value *Candidate) { value.AssetSummaries[0].Observations[0].ObservationID = "Observation-1" }},
		{name: "duplicate observation id", mutate: func(value *Candidate) {
			value.AssetSummaries[0].Observations = append(value.AssetSummaries[0].Observations, value.AssetSummaries[0].Observations[0])
		}},
		{name: "unknown inference observation", mutate: func(value *Candidate) {
			value.AssetSummaries[0].Inferences = []Inference{{
				InferenceID: "inference_1", Text: "推断", BasedOnObservationIDs: []string{"observation_missing"},
				Confidence: "medium", Uncertainty: "仍需验证",
			}}
		}},
		{name: "invalid risk enum", mutate: func(value *Candidate) {
			value.Risks = []Risk{{
				RiskID: "risk_1", Category: "legal_conclusion", Statement: "风险", EvidenceIDs: []string{graphTestEvidenceID},
				Severity: "critical", Uncertainty: "仍需验证",
			}}
		}},
		{name: "unknown missing requirement", mutate: func(value *Candidate) {
			value.OpenQuestions = []OpenQuestion{{
				QuestionID: "question_1", Question: "还缺什么？", AssetIDs: []string{graphTestAssetID},
				MissingRequirementIDs: []string{strings.Repeat("a", 64)},
			}}
		}},
		{name: "unknown unused evidence", mutate: func(value *Candidate) {
			value.UnusedEvidenceIDs = []string{"019b78a0-22b3-7b13-8a13-131313131313"}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := graphTestCandidate()
			testCase.mutate(&candidate)
			encoded, err := json.Marshal(candidate)
			if err != nil {
				t.Fatalf("json.Marshal() error=%v", err)
			}
			if _, err := DecodeAndValidateCandidate(encoded, intent, coverage, included, missing); err == nil ||
				ErrorResultCode(err) != ResultCodeModelOutputInvalid {
				t.Fatalf("DecodeAndValidateCandidate() error=%v", err)
			}
		})
	}
	for _, malformed := range []string{
		strings.Replace(validJSON, `"observation_id":"observation_1"`, `"observation_id":"observation_1","observation_id":"observation_2"`, 1),
		strings.Replace(validJSON, `"summary":"素材以城市中的红色自行车为主体。"`, `"summary":null`, 1),
		validJSON + `{}`,
	} {
		if _, err := DecodeAndValidateCandidate([]byte(malformed), intent, coverage, included, missing); err == nil {
			t.Fatal("duplicate/null/trailing Candidate 应被拒绝")
		}
	}
}

func TestBuildEvidenceQuerySortsTargetsAndPreservesExpectedVersions(t *testing.T) {
	t.Parallel()
	assetB := "019b78a0-22b3-7b12-8a12-121212121212"
	intent := graphTestIntent("content")
	intent.AssetIDs = []string{assetB, graphTestAssetID}
	intent.ExpectedAssets = []ExpectedAsset{{AssetID: assetB, AssetVersion: 2}, {AssetID: graphTestAssetID, AssetVersion: 1}}
	query, err := BuildEvidenceQuery(graphTestTrustedContext(), intent)
	if err != nil {
		t.Fatalf("BuildEvidenceQuery() error=%v", err)
	}
	want := []AssetTarget{{AssetID: graphTestAssetID, ExpectedVersion: 1}, {AssetID: assetB, ExpectedVersion: 2}}
	if !reflect.DeepEqual(query.Targets, want) || query.UserID != toolTestUserID || query.ProjectID != toolTestProjectID {
		t.Fatalf("EvidenceQuery=%+v want targets=%+v", query, want)
	}
}

func TestRequirementIDsAreStableAcrossSnapshotOrder(t *testing.T) {
	t.Parallel()
	intent := graphTestIntent("risk", "content")
	first := graphTestSnapshot("ready")
	secondEvidence := first.Assets[0].Evidence[0]
	secondEvidence.EvidenceID = "019b78a0-22b3-7b11-8a11-111111111111"
	first.Assets[0].Evidence = append(first.Assets[0].Evidence, secondEvidence)
	second := cloneEvidenceSnapshot(first)
	second.Assets[0].Evidence[0], second.Assets[0].Evidence[1] = second.Assets[0].Evidence[1], second.Assets[0].Evidence[0]
	_, _, firstMissing, err := NormalizeEvidence(intent, first)
	if err != nil {
		t.Fatalf("NormalizeEvidence(first) error=%v", err)
	}
	_, _, secondMissing, err := NormalizeEvidence(intent, second)
	if err != nil {
		t.Fatalf("NormalizeEvidence(second) error=%v", err)
	}
	if !reflect.DeepEqual(firstMissing, secondMissing) || len(firstMissing) != 1 || firstMissing[0].RequirementID == "" {
		t.Fatalf("missing requirement 不稳定: first=%+v second=%+v", firstMissing, secondMissing)
	}
}

func newTestUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("生成 UUIDv7 失败: %v", err)
	}
	return value.String()
}
