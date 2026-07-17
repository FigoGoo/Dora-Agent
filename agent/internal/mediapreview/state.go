package mediapreview

import "time"

const (
	// GenerateMediaGraphName 是 generate_media 开发预览的稳定 Graph 名称。
	GenerateMediaGraphName = "generate_media_graph_v3preview1"
	// AssembleOutputGraphName 是 assemble_output 开发预览的稳定 Graph 名称。
	AssembleOutputGraphName = "assemble_output_graph_v3preview1"
	// ScopeSchemaVersion 是两个 Graph 冻结执行范围的 canonical 编码版本。
	ScopeSchemaVersion = "media_preview.scope.v1"
	// ArtifactRequestSchemaVersion 是 Worker 确定性产物请求摘要版本。
	ArtifactRequestSchemaVersion = "media_preview.artifact_request.v1"
	// DispatchCommandSchemaVersion 是 Agent 原子派发命令摘要版本。
	DispatchCommandSchemaVersion = "media_preview.dispatch_command.v1"
)

// Clock 为 Graph 冻结 Job 与 Result 的 UTC 时间，测试可注入固定实现。
type Clock interface {
	// Now 返回当前时间；零值会使 Graph 失败关闭。
	Now() time.Time
}

// MediaGraphInput 是两个 Preview Graph 共用的严格入口；IntentJSON 仍在首节点重新解码。
type MediaGraphInput struct {
	// TrustedContext 是 Session Lane 注入的身份、Fence 与 Deadline，不可由 Intent 覆盖。
	TrustedContext TrustedContext
	// IntentJSON 是模型或结构化 Dispatcher 产生的原始 exact JSON。
	IntentJSON []byte
}

// GenerateMediaGraphInput 是 generate_media Graph 的有类型入口别名。
type GenerateMediaGraphInput = MediaGraphInput

// AssembleOutputGraphInput 是 assemble_output Graph 的有类型入口别名。
type AssembleOutputGraphInput = MediaGraphInput

// mediaPreviewState 保存一次无环 Graph 调用内的最小技术状态；跨进程权威事实始终在 PostgreSQL。
type mediaPreviewState[Intent any] struct {
	// TrustedContext 是 Runtime 初始化且单写的可信命令身份。
	TrustedContext TrustedContext
	// Intent 是 validate_intent 严格解码后的封闭 Tool 意图。
	Intent Intent
	// ScopeDigest 是 freeze_scope 生成且后续不可变化的执行范围摘要。
	ScopeDigest string
	// Operation 是 ensure_operation first-write-wins 返回的稳定聚合身份。
	Operation Operation
	// PreparationRequest 是进入 Business 副作用前冻结的原命令。
	PreparationRequest PrepareRequest
	// Preparation 是 Business Prepare 或 Query 返回并再次校验的权威预留结果。
	Preparation PrepareResult
	// JobSpec 是 build_job 生成的一 Operation/Batch/Job 固定映射。
	JobSpec JobSpec
	// DispatchReceipt 是 dispatch/query_dispatch 收敛后的 Agent 权威回执。
	DispatchReceipt DispatchReceipt
	// Result 是 emit_accepted/emit_failed 冻结的唯一 Graph Tool 终态。
	Result *GraphToolResult
	// ErrorCode 是分支只读的稳定低基数错误码，不保存内部错误原文。
	ErrorCode string
}

// GenerateMediaPreviewStateV1 是 generate_media.v3preview1 的强类型 Local State。
type GenerateMediaPreviewStateV1 = mediaPreviewState[GenerateMediaIntent]

// AssembleOutputPreviewStateV1 是 assemble_output.v3preview1 的强类型 Local State。
type AssembleOutputPreviewStateV1 = mediaPreviewState[AssembleOutputIntent]

// graphFlow 是 Node/Branch 间的有类型控制值；不作为持久化或外部协议。
type graphFlow[Intent any] struct {
	TrustedContext     TrustedContext
	Intent             Intent
	ScopeDigest        string
	Operation          Operation
	PreparationRequest PrepareRequest
	Preparation        PrepareResult
	Job                JobSpec
	DispatchReceipt    DispatchReceipt
	Status             string
	ErrorCode          string
}
