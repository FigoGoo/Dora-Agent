package writeprompts

import "errors"

var (
	// ErrBusinessUnknownOutcome 表示 Save 响应未知且当前 Graph 的一次 Query 未消除歧义。
	ErrBusinessUnknownOutcome = errors.New("prompt preview business outcome is unknown")
	// ErrBusinessNotFound 对外折叠项目、Storyboard 不存在与无权限。
	ErrBusinessNotFound = errors.New("prompt preview dependency is not found")
	// ErrBusinessConflict 表示版本、摘要、Fence 或幂等命令冲突。
	ErrBusinessConflict = errors.New("prompt preview business conflict")
	// ErrBusinessStoryboardConflict 表示可信 Storyboard 的版本或内容摘要已变化。
	ErrBusinessStoryboardConflict = errors.New("prompt preview Storyboard conflict")
	// ErrBusinessDisabled 表示 Business 明确关闭本地 Preview 能力。
	ErrBusinessDisabled = errors.New("prompt preview is disabled")
	// ErrBusinessTechnical 表示不应伪装成确定 Tool Result 的技术故障。
	ErrBusinessTechnical = errors.New("prompt preview business technical failure")
)
