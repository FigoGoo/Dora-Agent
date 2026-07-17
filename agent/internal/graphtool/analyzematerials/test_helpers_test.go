package analyzematerials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	graphTestAssetID    = "019b78a0-22b3-7b10-8a10-101010101010"
	graphTestEvidenceID = "019b78a0-22b3-7b11-8a11-111111111110"
)

type graphTestLoader struct {
	mu       sync.Mutex
	snapshot EvidenceSnapshot
	err      error
	calls    int
	query    EvidenceQuery
}

func (loader *graphTestLoader) BatchGetAssetAnalysisInputs(_ context.Context, query EvidenceQuery) (EvidenceSnapshot, error) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.calls++
	loader.query = query
	return cloneEvidenceSnapshot(loader.snapshot), loader.err
}

func (loader *graphTestLoader) callCount() int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	return loader.calls
}

type graphTestModel struct {
	mu       sync.Mutex
	content  string
	role     schema.RoleType
	response *schema.Message
	err      error
	calls    int
	messages [][]*schema.Message
}

func (fake *graphTestModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls++
	fake.messages = append(fake.messages, cloneMessages(messages))
	if fake.err != nil {
		return nil, fake.err
	}
	if fake.response != nil {
		response := *fake.response
		return &response, nil
	}
	role := fake.role
	if role == "" {
		role = schema.Assistant
	}
	return &schema.Message{Role: role, Content: fake.content}, nil
}

func (fake *graphTestModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := fake.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (fake *graphTestModel) callCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}

func graphTestTrustedContext() TrustedContext {
	return TrustedContext{
		Owner: "analyze-materials-graph-test", UserID: toolTestUserID, ProjectID: toolTestProjectID,
		SessionID: toolTestSessionID, InputID: toolTestInputID, TurnID: toolTestTurnID,
		RunID: toolTestRunID, ToolCallID: toolTestToolCallID, FenceToken: 7,
		PromptVersion: PromptVersion, ValidatorVersion: ValidatorVersion,
		EvidencePolicyVersion: EvidencePolicyVersion,
	}
}

func graphTestIntent(focus ...string) Intent {
	return Intent{
		SchemaVersion: IntentSchemaVersion, AssetIDs: []string{graphTestAssetID},
		AnalysisGoal: "识别素材中的核心主题与可复用元素", FocusDimensions: append([]string(nil), focus...),
		OutputLanguage: "zh-CN",
	}
}

func graphTestInput(t *testing.T, intent Intent) GraphInput {
	t.Helper()
	encoded, err := json.Marshal(intent)
	if err != nil {
		t.Fatalf("编码 Graph Intent 失败: %v", err)
	}
	return GraphInput{TrustedContext: graphTestTrustedContext(), IntentJSON: encoded}
}

func graphTestSnapshot(availability string) EvidenceSnapshot {
	content := "画面展示一辆红色自行车停在城市街角"
	evidence := EvidenceInput{
		EvidenceID: graphTestEvidenceID, AssetID: graphTestAssetID, AssetVersion: 1,
		MediaType: "image", EvidenceKind: "visual_description", Availability: availability,
		ExtractorSchemaVersion: "visual-description.v1", ExtractorVersion: "extractor.v1",
	}
	if availability == "ready" {
		evidence.Content = content
		evidence.ContentDigest = graphTestDigest(content)
		evidence.Locator = EvidenceLocator{Kind: "image_whole"}
	} else {
		evidence.ReasonCode = "EVIDENCE_NOT_READY"
	}
	return EvidenceSnapshot{
		SchemaVersion: EvidenceSnapshotSchemaVersion, SnapshotToken: "snapshot-v1", ResponseComplete: true,
		Assets: []AssetAnalysisInput{{
			AssetID: graphTestAssetID, AssetVersion: 1, MediaType: "image", Evidence: []EvidenceInput{evidence},
		}},
	}
}

func graphTestCandidate() Candidate {
	return Candidate{
		SchemaVersion: CandidateSchemaVersion,
		AssetSummaries: []AssetSummary{{
			AssetID: graphTestAssetID, Summary: "素材以城市中的红色自行车为主体。",
			Observations: []Observation{{
				ObservationID: "observation_1", Text: "画面主体是一辆红色自行车。",
				EvidenceIDs: []string{graphTestEvidenceID},
			}},
			Inferences: []Inference{},
		}},
		CrossAssetFindings: []CrossAssetFinding{},
		UsableElements: []UsableElement{{
			ElementID: "element_1", Label: "红色自行车", Description: "可作为城市出行主题的视觉锚点。",
			EvidenceIDs: []string{graphTestEvidenceID}, Constraints: []string{},
		}},
		Risks:             []Risk{},
		OpenQuestions:     []OpenQuestion{},
		UnusedEvidenceIDs: []string{},
	}
}

func graphTestCandidateJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.Marshal(graphTestCandidate())
	if err != nil {
		t.Fatalf("编码 Candidate 失败: %v", err)
	}
	return string(encoded)
}

func graphTestDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func requireContractCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || ErrorResultCode(err) != code {
		t.Fatalf("错误=%v code=%q，期望 code=%q", err, ErrorResultCode(err), code)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("契约错误不应伪装为取消: %v", err)
	}
}

var _ EvidenceLoader = (*graphTestLoader)(nil)
var _ model.BaseChatModel = (*graphTestModel)(nil)
