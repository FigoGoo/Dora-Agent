package planstoryboard

import "errors"

var (
	// ErrBusinessUnknownOutcome 表示 Save 响应未知且当前 Graph 的一次 Query 未消除歧义。
	ErrBusinessUnknownOutcome = errors.New("storyboard preview business outcome is unknown")
	// ErrBusinessNotFound 对外折叠项目、CreationSpec 不存在与无权限。
	ErrBusinessNotFound = errors.New("storyboard preview dependency is not found")
	// ErrBusinessConflict 表示版本、摘要、Fence 或幂等命令冲突。
	ErrBusinessConflict = errors.New("storyboard preview business conflict")
	// ErrBusinessCreationSpecConflict 表示可信 CreationSpec 的版本或内容摘要已变化。
	ErrBusinessCreationSpecConflict = errors.New("storyboard preview CreationSpec conflict")
	// ErrBusinessDisabled 表示 Business 明确关闭本地 Preview 能力。
	ErrBusinessDisabled = errors.New("storyboard preview is disabled")
	// ErrBusinessTechnical 表示不应伪装成确定 Tool Result 的技术故障。
	ErrBusinessTechnical = errors.New("storyboard preview business technical failure")
)
