// Package analyzematerials 实现获准但默认不注册的 analyze_materials V2 Tool Core 开发预览。
package analyzematerials

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

const (
	// ToolKey 是主 Agent Catalog 中的稳定高层能力标识；本 Profile 不进入可执行 Registry。
	ToolKey = "analyze_materials"
	// ToolDefinitionVersion 是本批唯一获准实现的 Tool Core 版本。
	ToolDefinitionVersion = "analyze_materials.v2preview1"
	// GraphName 是启动阶段编译的开发预览 Graph 名称。
	GraphName = "analyze_materials_graph_v2_preview"
	// StateSchemaName 是本地 Graph State 的稳定诊断名称；当前不启用 Checkpoint。
	StateSchemaName = "dora.agent.graphtool.analyze_materials.state.v2preview1"
	// IntentSchemaVersion 是模型可控 Tool Intent 的严格版本。
	IntentSchemaVersion = "analyze_materials.preview.intent.v1"
	// EvidenceSnapshotSchemaVersion 是未来 Business Adapter 必须返回的快照版本。
	EvidenceSnapshotSchemaVersion = "asset_analysis_inputs.preview.v1"
	// CandidateSchemaVersion 是内部 ChatModel 候选的严格版本。
	CandidateSchemaVersion = "material_analysis.preview.candidate.v1"
	// ResultSchemaVersion 是非权威开发预览结果版本。
	ResultSchemaVersion = "analyze_materials.preview.result.v1"
	// EvidencePolicyVersion 冻结 text/image 的 Evidence requirement 与门槛。
	EvidencePolicyVersion = "analyze_materials.preview.evidence-policy.v1"
	// PromptKey 是素材分析主 Prompt 的稳定键。
	PromptKey = "graph_tool.analyze_materials.preview.primary"
	// PromptVersion 是素材分析主 Prompt 的语义版本。
	PromptVersion = "graph_tool.analyze_materials.preview.v1"
	// ValidatorVersion 是独立候选 Validator 的冻结版本。
	ValidatorVersion = "analyze_materials.preview.validator.v1"

	// ResultCodeCompleted 表示全部目标 requirement 均由 included Evidence 覆盖。
	ResultCodeCompleted = "MATERIAL_ANALYSIS_PREVIEW_COMPLETED"
	// ResultCodePartial 表示达到最低门槛，但仍存在确定性的缺失或预算排除。
	ResultCodePartial = "MATERIAL_ANALYSIS_PREVIEW_PARTIAL"
	// ResultCodeInvalidArgument 表示模型可控 Intent 违反 strict contract。
	ResultCodeInvalidArgument = "MATERIAL_ANALYSIS_INVALID_ARGUMENT"
	// ResultCodeMaterialsNotAvailable 对外折叠不存在与无权限，避免资源枚举。
	ResultCodeMaterialsNotAvailable = "MATERIALS_NOT_AVAILABLE"
	// ResultCodeSnapshotInvalid 表示 Loader 返回的 Asset exact-set 或版本快照不可信。
	ResultCodeSnapshotInvalid = "MATERIAL_ANALYSIS_SNAPSHOT_INVALID"
	// ResultCodeEvidenceConflict 表示 Evidence ID、locator、digest 或归属冲突。
	ResultCodeEvidenceConflict = "MATERIAL_ANALYSIS_EVIDENCE_CONFLICT"
	// ResultCodeDependencyNotReady 表示没有任何达到最低门槛的 included Evidence。
	ResultCodeDependencyNotReady = "MATERIAL_ANALYSIS_DEPENDENCY_NOT_READY"
	// ResultCodePromptRenderFailed 表示版本化 Prompt 无法确定性渲染。
	ResultCodePromptRenderFailed = "MATERIAL_ANALYSIS_PROMPT_RENDER_FAILED"
	// ResultCodeModelFailed 表示开发模型调用失败且未形成候选。
	ResultCodeModelFailed = "MATERIAL_ANALYSIS_MODEL_FAILED"
	// ResultCodeModelOutputInvalid 表示模型候选未通过独立 strict Validator。
	ResultCodeModelOutputInvalid = "MATERIAL_ANALYSIS_MODEL_OUTPUT_INVALID"
	// ResultCodeInternal 表示未归类的安全内部失败。
	ResultCodeInternal = "MATERIAL_ANALYSIS_INTERNAL"
)

const (
	maxIntentJSONBytes     = 64 * 1024
	maxCandidateJSONBytes  = 64 * 1024
	maxAssets              = 8
	maxEvidence            = 32
	maxEvidenceRunes       = 2_000
	maxPromptEvidenceRunes = 12_000
)

// ExpectedAsset 是模型可选填写的 Asset 乐观版本约束。
type ExpectedAsset struct {
	AssetID      string `json:"asset_id"`
	AssetVersion int64  `json:"asset_version"`
}

// Intent 是模型可控的最小严格素材分析意图。
type Intent struct {
	SchemaVersion   string          `json:"schema_version"`
	AssetIDs        []string        `json:"asset_ids"`
	AnalysisGoal    string          `json:"analysis_goal"`
	FocusDimensions []string        `json:"focus_dimensions"`
	OutputLanguage  string          `json:"output_language"`
	ExpectedAssets  []ExpectedAsset `json:"expected_assets,omitempty"`
}

// TrustedContext 是 Runtime 注入且永不暴露到 Tool Schema 的只读调用身份。
type TrustedContext struct {
	Owner                 string
	UserID                string
	ProjectID             string
	SessionID             string
	InputID               string
	TurnID                string
	RunID                 string
	ToolCallID            string
	FenceToken            int64
	PromptVersion         string
	ValidatorVersion      string
	EvidencePolicyVersion string
}

// GraphInput 是编译后 Graph 的有类型入口。
type GraphInput struct {
	TrustedContext TrustedContext
	IntentJSON     []byte
}

// AssetTarget 是 Loader Query 中已规范排序的目标与可选版本约束。
type AssetTarget struct {
	AssetID         string
	ExpectedVersion int64
}

// EvidenceQuery 是 Graph 包向消费方 Loader 发出的最小授权读取请求。
type EvidenceQuery struct {
	UserID    string
	ProjectID string
	Targets   []AssetTarget
}

// EvidenceLocator 是 text/image 开发预览支持的有类型定位器。
type EvidenceLocator struct {
	Kind         string `json:"kind"`
	Start        int    `json:"start,omitempty"`
	End          int    `json:"end,omitempty"`
	SourceLength int    `json:"source_length,omitempty"`
	X            int    `json:"x,omitempty"`
	Y            int    `json:"y,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

// MarshalJSON 按定位器 kind 输出冻结的判别联合字段，并保留 start、x、y 的合法零值。
// 该边界禁止依赖 omitempty 猜测字段存在性，避免后端投影生成前端严格契约无法解析的 Card。
func (locator EvidenceLocator) MarshalJSON() ([]byte, error) {
	switch locator.Kind {
	case "text_range":
		if locator.X != 0 || locator.Y != 0 || locator.Width != 0 || locator.Height != 0 {
			return nil, fmt.Errorf("marshal evidence locator: text_range contains image fields")
		}
		type textRangeWire struct {
			Kind         string `json:"kind"`
			Start        int    `json:"start"`
			End          int    `json:"end"`
			SourceLength int    `json:"source_length"`
		}
		return json.Marshal(textRangeWire{
			Kind: locator.Kind, Start: locator.Start, End: locator.End, SourceLength: locator.SourceLength,
		})
	case "image_whole":
		if locator.Start != 0 || locator.End != 0 || locator.SourceLength != 0 ||
			locator.X != 0 || locator.Y != 0 || locator.Width != 0 || locator.Height != 0 {
			return nil, fmt.Errorf("marshal evidence locator: image_whole contains range fields")
		}
		type imageWholeWire struct {
			Kind string `json:"kind"`
		}
		return json.Marshal(imageWholeWire{Kind: locator.Kind})
	case "image_region":
		if locator.Start != 0 || locator.End != 0 || locator.SourceLength != 0 {
			return nil, fmt.Errorf("marshal evidence locator: image_region contains text fields")
		}
		type imageRegionWire struct {
			Kind   string `json:"kind"`
			X      int    `json:"x"`
			Y      int    `json:"y"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		return json.Marshal(imageRegionWire{
			Kind: locator.Kind, X: locator.X, Y: locator.Y, Width: locator.Width, Height: locator.Height,
		})
	case "":
		if locator.Start == 0 && locator.End == 0 && locator.SourceLength == 0 &&
			locator.X == 0 && locator.Y == 0 && locator.Width == 0 && locator.Height == 0 {
			// unavailable Evidence 允许内部使用空定位器；成功 Result 的 Validator 会拒绝该形态。
			return []byte("{}"), nil
		}
		fallthrough
	default:
		return nil, fmt.Errorf("marshal evidence locator: unsupported kind")
	}
}

// EvidenceInput 是 Loader 返回的一条已持久化摘要 Evidence。
type EvidenceInput struct {
	EvidenceID             string
	AssetID                string
	AssetVersion           int64
	MediaType              string
	EvidenceKind           string
	ContentDigest          string
	ExtractorSchemaVersion string
	ExtractorVersion       string
	Locator                EvidenceLocator
	Availability           string
	ReasonCode             string
	Content                string
}

// AssetAnalysisInput 是一次快照中某个目标 Asset 的版本化摘要集合。
type AssetAnalysisInput struct {
	AssetID      string
	AssetVersion int64
	MediaType    string
	Evidence     []EvidenceInput
}

// EvidenceSnapshot 是 Loader 必须一次性返回的完整、有界 Asset exact-set。
type EvidenceSnapshot struct {
	SchemaVersion    string
	SnapshotToken    string
	ResponseComplete bool
	Assets           []AssetAnalysisInput
}

// EvidenceLoader 是 Graph 消费方定义的最小只读端口；具体 Kitex Adapter 不属于本批。
type EvidenceLoader interface {
	BatchGetAssetAnalysisInputs(context.Context, EvidenceQuery) (EvidenceSnapshot, error)
}

// EvidenceRef 是 Result 可公开的版本化证据引用，不携带正文。
type EvidenceRef struct {
	EvidenceID    string          `json:"evidence_id"`
	AssetID       string          `json:"asset_id"`
	AssetVersion  int64           `json:"asset_version"`
	MediaType     string          `json:"media_type"`
	EvidenceKind  string          `json:"evidence_kind"`
	ContentDigest string          `json:"content_digest"`
	Locator       EvidenceLocator `json:"locator"`
}

// MissingRequirement 是确定性 Evidence Policy 生成的缺失事实。
type MissingRequirement struct {
	RequirementID  string `json:"requirement_id"`
	AssetID        string `json:"asset_id"`
	AssetVersion   int64  `json:"asset_version"`
	FocusDimension string `json:"focus_dimension"`
	EvidenceKind   string `json:"evidence_kind"`
	ReasonCode     string `json:"reason_code"`
}

// Observation 是由 Evidence 直接支持的模型候选观察。
type Observation struct {
	ObservationID string   `json:"observation_id"`
	Text          string   `json:"text"`
	EvidenceIDs   []string `json:"evidence_ids"`
}

// Inference 是只允许从同 Asset observation 推导的候选推断。
type Inference struct {
	InferenceID           string   `json:"inference_id"`
	Text                  string   `json:"text"`
	BasedOnObservationIDs []string `json:"based_on_observation_ids"`
	Confidence            string   `json:"confidence"`
	Uncertainty           string   `json:"uncertainty"`
}

// AssetSummary 是单个可分析 Asset 的候选摘要。
type AssetSummary struct {
	AssetID      string        `json:"asset_id"`
	Summary      string        `json:"summary"`
	Observations []Observation `json:"observations"`
	Inferences   []Inference   `json:"inferences"`
}

// CrossAssetFinding 是至少覆盖两个 Asset 的候选关联发现。
type CrossAssetFinding struct {
	FindingID   string   `json:"finding_id"`
	FindingType string   `json:"finding_type"`
	Text        string   `json:"text"`
	AssetIDs    []string `json:"asset_ids"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  string   `json:"confidence"`
	Uncertainty string   `json:"uncertainty"`
}

// UsableElement 是可复用但不创建 Business Asset/Storyboard ID 的候选元素。
type UsableElement struct {
	ElementID   string   `json:"element_id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	EvidenceIDs []string `json:"evidence_ids"`
	Constraints []string `json:"constraints"`
}

// Risk 是带 Evidence 的候选风险提示，不构成身份或法律结论。
type Risk struct {
	RiskID      string   `json:"risk_id"`
	Category    string   `json:"category"`
	Statement   string   `json:"statement"`
	EvidenceIDs []string `json:"evidence_ids"`
	Severity    string   `json:"severity"`
	Uncertainty string   `json:"uncertainty"`
}

// OpenQuestion 只能引用目标 Asset 与确定性 missing requirement。
type OpenQuestion struct {
	QuestionID            string   `json:"question_id"`
	Question              string   `json:"question"`
	AssetIDs              []string `json:"asset_ids"`
	MissingRequirementIDs []string `json:"missing_requirement_ids"`
}

// Candidate 是 ChatModel 唯一允许输出的严格分析候选，不含状态或资源引用。
type Candidate struct {
	SchemaVersion      string              `json:"schema_version"`
	AssetSummaries     []AssetSummary      `json:"asset_summaries"`
	CrossAssetFindings []CrossAssetFinding `json:"cross_asset_findings"`
	UsableElements     []UsableElement     `json:"usable_elements"`
	Risks              []Risk              `json:"risks"`
	OpenQuestions      []OpenQuestion      `json:"open_questions"`
	UnusedEvidenceIDs  []string            `json:"unused_evidence_ids"`
}

// Coverage 是确定性节点产生的有界覆盖事实，模型无权填写。
type Coverage struct {
	Status                    string               `json:"status"`
	EvidencePolicyVersion     string               `json:"evidence_policy_version"`
	TargetAssetIDs            []string             `json:"target_asset_ids"`
	AnalyzableAssetIDs        []string             `json:"analyzable_asset_ids"`
	IncludedEvidenceIDs       []string             `json:"included_evidence_ids"`
	MissingRequirements       []MissingRequirement `json:"missing_requirements"`
	TargetAssetSetDigest      string               `json:"target_asset_set_digest"`
	IncludedEvidenceSetDigest string               `json:"included_evidence_set_digest"`
	MissingRequirementDigest  string               `json:"missing_requirement_set_digest"`
}

// InvocationRef 只提供 Tool Call 关联，不声称存在持久化 ToolReceipt。
type InvocationRef struct {
	ToolCallID string `json:"tool_call_id"`
}

// Result 是 completed/partial/failed 的严格开发预览判别联合。
type Result struct {
	SchemaVersion string        `json:"schema_version"`
	Status        string        `json:"status"`
	ResultCode    string        `json:"result_code"`
	Analysis      *Candidate    `json:"analysis,omitempty"`
	Coverage      *Coverage     `json:"coverage,omitempty"`
	EvidenceRefs  []EvidenceRef `json:"evidence_refs,omitempty"`
	InvocationRef InvocationRef `json:"invocation_ref"`
	Summary       string        `json:"summary,omitempty"`
	Retryable     *bool         `json:"retryable,omitempty"`
}

// Outcome 是 Graph 的有类型输出；当前 Profile 只返回一个严格 Result。
type Outcome struct {
	Result Result
}

// evidenceUnit 把可公开 Ref 与只在调用内存存在的最小正文分开。
type evidenceUnit struct {
	Ref     EvidenceRef
	Content string
}

// validatedInput 是 validate_intent 的有类型输出。
type validatedInput struct {
	TrustedContext TrustedContext
	Intent         Intent
	IntentDigest   string
	Targets        []AssetTarget
}

// loadedInputs 是 load_asset_inputs 的有类型输出。
type loadedInputs struct {
	ValidatedInput validatedInput
	Snapshot       EvidenceSnapshot
}

// normalizedEvidence 是 normalize_evidence 的有类型输出。
type normalizedEvidence struct {
	ValidatedInput validatedInput
	Assets         []AssetAnalysisInput
	Ready          []evidenceUnit
	Missing        []MissingRequirement
}

// selectedEvidence 是 select_prompt_evidence 的有类型输出。
type selectedEvidence struct {
	ValidatedInput validatedInput
	Assets         []AssetAnalysisInput
	Included       []evidenceUnit
	Missing        []MissingRequirement
}

// failure 是可以进入 Result 的稳定、安全错误，不包含内部原文。
type failure struct {
	Code    string
	Summary string
}

// analysisRoute 是两个 Branch 共用的判别值；Candidate 只有校验通过后才存在。
type analysisRoute struct {
	Route           string
	Selected        selectedEvidence
	Coverage        Coverage
	Candidate       *Candidate
	CandidateDigest string
	Failure         *failure
}

// promptEvidence 是 Prompt 数据块使用的有界 DTO。
type promptEvidence struct {
	EvidenceID   string          `json:"evidence_id"`
	AssetID      string          `json:"asset_id"`
	AssetVersion int64           `json:"asset_version"`
	MediaType    string          `json:"media_type"`
	EvidenceKind string          `json:"evidence_kind"`
	Locator      EvidenceLocator `json:"locator"`
	Content      string          `json:"content"`
}

// promptEnvelope 是 Prompt User 数据区的稳定结构。
type promptEnvelope struct {
	SchemaVersion       string               `json:"schema_version"`
	AnalysisGoal        string               `json:"analysis_goal"`
	FocusDimensions     []string             `json:"focus_dimensions"`
	OutputLanguage      string               `json:"output_language"`
	AnalyzableAssetIDs  []string             `json:"analyzable_asset_ids"`
	Evidence            []promptEvidence     `json:"evidence"`
	MissingRequirements []MissingRequirement `json:"missing_requirements"`
}

// modelStateOutput 是 ChatModel State post-handler 的输出别名，用于约束经典 Message。
type modelStateOutput = *schema.Message
