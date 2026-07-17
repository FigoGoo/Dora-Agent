package mediapreview

import "errors"

// ErrorCode 是跨 Worker 边界可持久化和分类的稳定错误码。
type ErrorCode string

const (
	// ErrorCodeInvalidArgument 表示 Envelope、配置或相对 key 不符合冻结契约。
	ErrorCodeInvalidArgument ErrorCode = "INVALID_ARGUMENT"
	// ErrorCodeNotFound 表示 Business 相对 key 对应的源文件不存在。
	ErrorCodeNotFound ErrorCode = "NOT_FOUND"
	// ErrorCodeUnsupportedProfile 表示 Runtime、JobType 或输出 Profile 未被当前版本授权。
	ErrorCodeUnsupportedProfile ErrorCode = "UNSUPPORTED_PROFILE"
	// ErrorCodeArtifactInvalid 表示文件格式、尺寸、摘要或 MP4 探针结果不符合契约。
	ErrorCodeArtifactInvalid ErrorCode = "ARTIFACT_INVALID"
	// ErrorCodeFFmpegUnavailable 表示 ffmpeg/ffprobe 配置或 readiness 不满足固定执行要求。
	ErrorCodeFFmpegUnavailable ErrorCode = "FFMPEG_UNAVAILABLE"
	// ErrorCodeExecutionTimeout 表示 Context 或 Envelope Deadline 在媒体命令完成前到期。
	ErrorCodeExecutionTimeout ErrorCode = "EXECUTION_TIMEOUT"
	// ErrorCodeUnknownOutcome 表示原子 rename 已发生但目录持久化结果无法确认，必须先核对。
	ErrorCodeUnknownOutcome ErrorCode = "UNKNOWN_OUTCOME"
	// ErrorCodeInternal 表示没有对外暴露底层细节的 Worker 内部失败。
	ErrorCodeInternal ErrorCode = "INTERNAL"
)

// ArtifactError 是媒体预览引擎返回的脱敏错误。
//
// Error 文本只包含稳定 op 与 code；底层原因仅供当前进程通过 errors.Is/As 核对，不应写入 Job Result。
type ArtifactError struct {
	// Code 是允许跨边界持久化的稳定错误码。
	Code ErrorCode
	// Op 是不含路径、命令参数或输入正文的稳定处理阶段。
	Op string
	// cause 只用于进程内错误链判断，Error 文本不会展开它。
	cause error
}

// Error 返回不包含绝对路径、stderr 或 Envelope 内容的稳定错误文本。
func (e *ArtifactError) Error() string {
	if e == nil {
		return "mediapreview: INTERNAL"
	}
	if e.Op == "" {
		return "mediapreview: " + string(e.Code)
	}
	return "mediapreview: " + e.Op + ": " + string(e.Code)
}

// Unwrap 暴露进程内原因供 errors.Is/As 分类；调用方不得把展开后的原因持久化或记录到普通日志。
func (e *ArtifactError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// CodeOf 从任意错误链提取稳定媒体错误码；非媒体错误统一收敛为 INTERNAL。
func CodeOf(err error) ErrorCode {
	var artifactError *ArtifactError
	if errors.As(err, &artifactError) {
		return artifactError.Code
	}
	return ErrorCodeInternal
}

// newArtifactError 创建只对外暴露稳定阶段与错误码的错误。
func newArtifactError(code ErrorCode, op string, cause error) error {
	return &ArtifactError{Code: code, Op: op, cause: cause}
}
