package analyzematerials

import (
	"errors"
	"fmt"
)

// ContractError 是 analyze_materials Preview 在 Tool 边界可安全公开的稳定错误。
// Error 只返回冻结的中文摘要；底层 cause 仅供 errors.Is/As 与内部诊断使用，禁止直接投影给用户。
type ContractError struct {
	resultCode string
	summary    string
	cause      error
}

// Error 返回不含 Loader、Provider、Prompt 或 Evidence 原文的安全摘要。
func (e *ContractError) Error() string {
	if e == nil {
		return ""
	}
	return e.summary
}

// Unwrap 保留内部错误分类能力；调用方不得把展开后的原文写入 Result、日志或 Trace。
func (e *ContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// ResultCode 返回 Tool Result 使用的稳定错误码。
func (e *ContractError) ResultCode() string {
	if e == nil {
		return ResultCodeInternal
	}
	return e.resultCode
}

// Summary 返回可安全写入 failed Preview Result 的中文摘要。
func (e *ContractError) Summary() string {
	if e == nil {
		return safeSummaryForCode(ResultCodeInternal)
	}
	return e.summary
}

// ErrorResultCode 从错误链提取稳定 Result Code；未知错误统一失败关闭为 INTERNAL。
func ErrorResultCode(err error) string {
	var contractError *ContractError
	if errors.As(err, &contractError) && contractError.resultCode != "" {
		return contractError.resultCode
	}
	return ResultCodeInternal
}

// ErrorSummary 从错误链提取安全中文摘要；未知错误不得泄漏其 Error 文本。
func ErrorSummary(err error) string {
	var contractError *ContractError
	if errors.As(err, &contractError) && contractError.summary != "" {
		return contractError.summary
	}
	return safeSummaryForCode(ResultCodeInternal)
}

// NewContractError 把节点内部错误包装为固定 Result Code 与安全摘要。
// 调用方只选择已冻结 code，cause 原文不会由 Error 或 Summary 暴露。
func NewContractError(resultCode string, cause error) error {
	return newContractError(resultCode, cause)
}

// failureFromError 把内部错误压缩为 Graph State 可保存的稳定失败事实。
func failureFromError(err error) *failure {
	return &failure{Code: ErrorResultCode(err), Summary: ErrorSummary(err)}
}

// newContractError 创建安全错误并保留内部 cause；summary 由稳定 code 唯一决定。
func newContractError(resultCode string, cause error) error {
	if !validFailureResultCode(resultCode) {
		resultCode = ResultCodeInternal
	}
	return &ContractError{
		resultCode: resultCode,
		summary:    safeSummaryForCode(resultCode),
		cause:      cause,
	}
}

// contractErrorf 只用于构造内部 cause；格式化内容不会进入 ContractError.Error。
func contractErrorf(resultCode, format string, arguments ...any) error {
	return newContractError(resultCode, fmt.Errorf(format, arguments...))
}

// safeSummaryForCode 冻结所有 Preview 失败码的安全用户摘要。
func safeSummaryForCode(resultCode string) string {
	switch resultCode {
	case ResultCodeInvalidArgument:
		return "素材分析参数不符合要求"
	case ResultCodeMaterialsNotAvailable:
		return "所选素材不可用或无权访问"
	case ResultCodeSnapshotInvalid:
		return "素材证据快照不完整或已失效"
	case ResultCodeEvidenceConflict:
		return "素材证据存在冲突"
	case ResultCodeDependencyNotReady:
		return "素材证据尚不足以生成可信分析"
	case ResultCodePromptRenderFailed:
		return "素材分析请求暂时无法构造"
	case ResultCodeModelFailed:
		return "素材分析模型调用失败"
	case ResultCodeModelOutputInvalid:
		return "素材分析结果未通过结构校验"
	default:
		return "素材分析暂时无法完成"
	}
}
