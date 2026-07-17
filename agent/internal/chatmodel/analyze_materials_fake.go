package chatmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// AnalyzeMaterialsFakeRouter 是本地 Profile 唯一允许的确定性 Tool Router。
type AnalyzeMaterialsFakeRouter struct{ tools []*schema.ToolInfo }

// NewAnalyzeMaterialsFakeRouter 创建未绑定能力的本地 Router。
func NewAnalyzeMaterialsFakeRouter() *AnalyzeMaterialsFakeRouter {
	return &AnalyzeMaterialsFakeRouter{}
}

// WithTools 返回恰好绑定 analyze_materials 的不可变副本。
func (m *AnalyzeMaterialsFakeRouter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if err := validateAnalyzeMaterialsTools(tools); err != nil {
		return nil, err
	}
	return &AnalyzeMaterialsFakeRouter{tools: append([]*schema.ToolInfo(nil), tools...)}, nil
}

// Generate 逐字复制可信 canonical Intent，生成唯一稳定 ToolCall；Tool Result 后调用一律失败关闭。
func (m *AnalyzeMaterialsFakeRouter) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tools := model.GetCommonOptions(&model.Options{Tools: append([]*schema.ToolInfo(nil), m.tools...)}, options...).Tools
	if err := validateAnalyzeMaterialsTools(tools); err != nil {
		return nil, fmt.Errorf("run analyze materials fake router: %w", err)
	}
	trusted, ok := turncontext.MaterialAnalysisRuntimeFrom(ctx)
	if !ok || trusted.Context.Profile != turncontext.MaterialAnalysisRuntimeProfile || trusted.Context.ToolCallID == "" || trusted.IntentJSON == "" {
		return nil, fmt.Errorf("run analyze materials fake router: trusted runtime context is invalid")
	}
	var user *schema.Message
	for _, message := range messages {
		if message == nil {
			continue
		}
		if message.Role == schema.Tool {
			return nil, fmt.Errorf("run analyze materials fake router: ReturnDirectly forbids a second router call")
		}
		if message.Role == schema.User {
			user = message
		}
	}
	if user == nil || user.Content != trusted.IntentJSON || len(user.ToolCalls) != 0 {
		return nil, fmt.Errorf("run analyze materials fake router: exact frozen Intent user message is required")
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: trusted.Context.ToolCallID, Type: "function", Function: schema.FunctionCall{Name: analyzematerials.ToolKey, Arguments: trusted.IntentJSON}}}), nil
}

// Stream 返回单块确定性 ToolCall。
func (m *AnalyzeMaterialsFakeRouter) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func validateAnalyzeMaterialsTools(tools []*schema.ToolInfo) error {
	if len(tools) != 1 || tools[0] == nil || tools[0].Name != analyzematerials.ToolKey || tools[0].ParamsOneOf == nil {
		return fmt.Errorf("exact analyze_materials Tool Registry is required")
	}
	return nil
}

// AnalyzeMaterialsFakeModel 是 Graph 内部一次候选生成的本地确定性模型。
// 它只读取版本化 Prompt 中的 included Evidence 标识，不调用外部 Provider。
type AnalyzeMaterialsFakeModel struct{}

// NewAnalyzeMaterialsFakeModel 创建无外部依赖的 Graph ChatModel。
func NewAnalyzeMaterialsFakeModel() *AnalyzeMaterialsFakeModel { return &AnalyzeMaterialsFakeModel{} }

type analyzePromptEvidence struct {
	EvidenceID string `json:"evidence_id"`
	AssetID    string `json:"asset_id"`
}

// Generate 为每个 included Evidence 生成一个直接观察，使 strict complement 可由 Validator 复核。
func (m *AnalyzeMaterialsFakeModel) Generate(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(model.GetCommonOptions(&model.Options{}, options...).Tools) != 0 {
		return nil, fmt.Errorf("run analyze materials fake analysis model: tools are forbidden")
	}
	if len(messages) != 2 || messages[0] == nil || messages[0].Role != schema.System || messages[1] == nil || messages[1].Role != schema.User {
		return nil, fmt.Errorf("run analyze materials fake analysis model: exact system/user prompt is required")
	}
	const start = "\nincluded_evidence_json="
	const end = "\nmissing_requirements_json="
	content := messages[1].Content
	startIndex := strings.Index(content, start)
	if startIndex < 0 {
		return nil, fmt.Errorf("run analyze materials fake analysis model: evidence boundary is missing")
	}
	startIndex += len(start)
	endIndex := strings.Index(content[startIndex:], end)
	if endIndex < 0 {
		return nil, fmt.Errorf("run analyze materials fake analysis model: evidence boundary is incomplete")
	}
	var evidence []analyzePromptEvidence
	if err := json.Unmarshal([]byte(content[startIndex:startIndex+endIndex]), &evidence); err != nil || len(evidence) == 0 {
		return nil, fmt.Errorf("run analyze materials fake analysis model: included Evidence is invalid")
	}
	byAsset := make(map[string][]string)
	for _, item := range evidence {
		if item.AssetID == "" || item.EvidenceID == "" {
			return nil, fmt.Errorf("run analyze materials fake analysis model: Evidence identity is invalid")
		}
		byAsset[item.AssetID] = append(byAsset[item.AssetID], item.EvidenceID)
	}
	assetIDs := make([]string, 0, len(byAsset))
	for assetID := range byAsset {
		assetIDs = append(assetIDs, assetID)
	}
	sort.Strings(assetIDs)
	candidate := analyzematerials.Candidate{SchemaVersion: analyzematerials.CandidateSchemaVersion, AssetSummaries: make([]analyzematerials.AssetSummary, 0, len(assetIDs)), CrossAssetFindings: []analyzematerials.CrossAssetFinding{}, UsableElements: []analyzematerials.UsableElement{}, Risks: []analyzematerials.Risk{}, OpenQuestions: []analyzematerials.OpenQuestion{}, UnusedEvidenceIDs: []string{}}
	observationIndex := 0
	for _, assetID := range assetIDs {
		evidenceIDs := byAsset[assetID]
		sort.Strings(evidenceIDs)
		observations := make([]analyzematerials.Observation, 0, len(evidenceIDs))
		for _, evidenceID := range evidenceIDs {
			observationIndex++
			observations = append(observations, analyzematerials.Observation{ObservationID: fmt.Sprintf("observation_%d", observationIndex), Text: "该素材包含已持久化证据支持的可分析内容。", EvidenceIDs: []string{evidenceID}})
		}
		candidate.AssetSummaries = append(candidate.AssetSummaries, analyzematerials.AssetSummary{AssetID: assetID, Summary: "该素材已依据持久化证据生成开发预览摘要。", Observations: observations, Inferences: []analyzematerials.Inference{}})
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return nil, fmt.Errorf("run analyze materials fake analysis model: encode candidate: %w", err)
	}
	return schema.AssistantMessage(string(encoded), nil), nil
}

// Stream 返回单块候选响应。
func (m *AnalyzeMaterialsFakeModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

var _ model.ToolCallingChatModel = (*AnalyzeMaterialsFakeRouter)(nil)
var _ model.BaseChatModel = (*AnalyzeMaterialsFakeModel)(nil)
