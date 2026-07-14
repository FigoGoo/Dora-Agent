// Package tool 提供 Agent Tool 定义目录的稳定 DTO 和静态不可用投影。
// 当前包不包含可执行 Definition、Graph、Prompt 或运行入口。
package tool

const (
	// DefinitionCatalogSchemaVersionV1 是 W1-B2 静态定义目录的唯一 DTO 版本。
	DefinitionCatalogSchemaVersionV1 = "tool_definition_catalog.v1"
)

// Definition 是用户可见的最小 Tool 目录项，只表达稳定名称、顺序和不可用原因。
type Definition struct {
	// ToolKey 是六项产品能力的稳定 snake_case 标识。
	ToolKey string `json:"tool_key"`
	// DisplayName 是前端按照产品基线展示的中文名称。
	DisplayName string `json:"display_name"`
	// Order 是从 1 开始且不得重排的目录顺序。
	Order int `json:"order"`
	// Availability 在 W1-B2 固定为 unavailable，不表示运行就绪状态。
	Availability string `json:"availability"`
	// ReasonCode 在 W1-B2 固定说明六份 Graph Tool 设计尚未获得实现批准。
	ReasonCode string `json:"reason_code"`
}

// DefinitionCatalogResponse 是 Agent 内部 HTTP 成功返回的 exact catalog Envelope。
type DefinitionCatalogResponse struct {
	// SchemaVersion 是目录 DTO 版本，不是 Tool Definition 或 Graph 版本。
	SchemaVersion string `json:"schema_version"`
	// RequestID 是 Business 已签名且 Agent 已校验的 UUIDv7 请求标识。
	RequestID string `json:"request_id"`
	// Items 是固定六项、固定顺序且全部不可用的独立副本。
	Items []Definition `json:"items"`
}

// CatalogProvider 提供不依赖数据库、配置、Skill 或 Graph 的静态定义目录。
type CatalogProvider struct{}

// NewCatalogProvider 创建无外部依赖的静态目录 Provider；它不探测也不注册任何可执行能力。
func NewCatalogProvider() *CatalogProvider {
	return &CatalogProvider{}
}

// ListDefinitions 返回六项 exact-set 的新切片，调用方修改结果不会污染后续请求。
func (*CatalogProvider) ListDefinitions() []Definition {
	return []Definition{
		{ToolKey: "plan_creation_spec", DisplayName: "流程规划", Order: 1, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "analyze_materials", DisplayName: "素材分析", Order: 2, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "plan_storyboard", DisplayName: "故事板设计", Order: 3, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "generate_media", DisplayName: "媒体生成", Order: 4, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "write_prompts", DisplayName: "提示词写法", Order: 5, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "assemble_output", DisplayName: "视频剪辑", Order: 6, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
	}
}
