package mediapreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const maxMediaJSONBytes = 64 * 1024

var safeObjectKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,511}$`)
var targetLocalKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

// DecodeGenerateMediaIntent 严格拒绝重复/未知字段、尾随值、Prompt、路径和执行参数。
func DecodeGenerateMediaIntent(encoded []byte) (GenerateMediaIntent, error) {
	var intent GenerateMediaIntent
	if err := strictDecodeJSON(encoded, &intent); err != nil || ValidateGenerateMediaIntent(intent) != nil {
		return GenerateMediaIntent{}, fmt.Errorf("%w: invalid generate_media intent", ErrInvalidArgument)
	}
	return intent, nil
}

// ValidateGenerateMediaIntent 校验 generate_media exact-set。
func ValidateGenerateMediaIntent(intent GenerateMediaIntent) error {
	if intent.SchemaVersion != GenerateMediaIntentVersion || !validUUIDv7(intent.PromptPreviewID) ||
		intent.ExpectedPromptVersion != 1 || !validDigest(intent.ExpectedPromptContentDigest) ||
		!validLocalKey(intent.TargetLocalKey) || intent.OutputProfile != GenerateOutputProfile {
		return ErrInvalidArgument
	}
	return nil
}

// DecodeAssembleOutputIntent 严格拒绝重复/未知字段、尾随值、Object Key 和 ffmpeg 参数。
func DecodeAssembleOutputIntent(encoded []byte) (AssembleOutputIntent, error) {
	var intent AssembleOutputIntent
	if err := strictDecodeJSON(encoded, &intent); err != nil || ValidateAssembleOutputIntent(intent) != nil {
		return AssembleOutputIntent{}, fmt.Errorf("%w: invalid assemble_output intent", ErrInvalidArgument)
	}
	return intent, nil
}

// DecodePrepareRequest 严格恢复已冻结的 Prepare 命令；数据库 JSONB 中出现未知字段也必须失败关闭。
func DecodePrepareRequest(encoded []byte) (PrepareRequest, error) {
	var request PrepareRequest
	if err := strictDecodeJSON(encoded, &request); err != nil || ValidatePrepareRequest(request) != nil {
		return PrepareRequest{}, fmt.Errorf("%w: invalid media prepare request", ErrInvalidArgument)
	}
	return request, nil
}

// DecodePrepareResult 严格恢复 Business Prepare 权威结果，并再次绑定原始请求语义。
func DecodePrepareResult(encoded []byte, request PrepareRequest) (PrepareResult, error) {
	var result PrepareResult
	if err := strictDecodeJSON(encoded, &result); err != nil || ValidatePrepareResult(result, request) != nil {
		return PrepareResult{}, fmt.Errorf("%w: invalid media prepare result", ErrInvalidArgument)
	}
	return result, nil
}

// DecodePrepareQueryResult 严格解码 Prepare Unknown Outcome 查询结果，拒绝不完整联合和未知字段。
func DecodePrepareQueryResult(encoded []byte, query PrepareQuery) (PrepareQueryResult, error) {
	var result PrepareQueryResult
	if err := strictDecodeJSON(encoded, &result); err != nil || ValidatePrepareQueryResult(result, query) != nil {
		return PrepareQueryResult{}, fmt.Errorf("%w: invalid media prepare query result", ErrInvalidArgument)
	}
	return result, nil
}

// DecodeBusinessReadiness 严格解码 Agent 启动探针结果；任一能力未就绪都失败关闭。
func DecodeBusinessReadiness(encoded []byte) (BusinessReadiness, error) {
	var result BusinessReadiness
	if err := strictDecodeJSON(encoded, &result); err != nil ||
		result.SchemaVersion != "media_asset.readiness.preview.v1" || result.Profile != Profile ||
		!result.ObjectRootReady || !result.Prepare || !result.Finalize {
		return BusinessReadiness{}, fmt.Errorf("%w: invalid Business media readiness", ErrDependencyNotReady)
	}
	return result, nil
}

// DecodeGraphToolResult 严格解码媒体 Tool 的 accepted/failed 联合，供共享 Runner 重验。
func DecodeGraphToolResult(encoded []byte) (GraphToolResult, error) {
	var result GraphToolResult
	if err := strictDecodeJSON(encoded, &result); err != nil || ValidateGraphToolResult(result) != nil {
		return GraphToolResult{}, fmt.Errorf("%w: invalid media Graph Tool result", ErrInvalidArgument)
	}
	return result, nil
}

// ValidateGraphToolResult 校验 Tool 输出不泄漏 Job、路径、URL 或内部诊断。
func ValidateGraphToolResult(result GraphToolResult) error {
	if result.SchemaVersion != ToolResultSchemaVersion ||
		(result.ToolKey != GenerateMediaToolKey && result.ToolKey != AssembleOutputToolKey) || result.UpdatedAt.IsZero() {
		return ErrInvalidArgument
	}
	switch result.Status {
	case "accepted":
		if result.ResultCode != ResultCodeAccepted || !validUUIDv7(result.OperationID) ||
			!validUUIDv7(result.BatchID) || !validUUIDv7(result.AssetID) || !validUUIDv7(result.ReceiptID) ||
			result.ErrorCode != "" {
			return ErrInvalidArgument
		}
	case "failed":
		if !validMediaFailureCode(result.ResultCode) || result.ErrorCode != result.ResultCode ||
			result.OperationID != "" || result.BatchID != "" || result.AssetID != "" || result.ReceiptID != "" {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

func validMediaFailureCode(code string) bool {
	switch code {
	case ResultCodeInvalidArgument, ResultCodeNotFound, ResultCodeVersionConflict,
		ResultCodeIdempotencyConflict, ResultCodeDependencyNotReady, ResultCodeUnsupportedProfile,
		ResultCodeUnknownOutcome, ResultCodeInternal:
		return true
	default:
		return false
	}
}

// ValidateAssembleOutputIntent 校验 assemble_output exact-set。
func ValidateAssembleOutputIntent(intent AssembleOutputIntent) error {
	if intent.SchemaVersion != AssembleOutputIntentVersion || !validUUIDv7(intent.SourceAssetID) ||
		intent.ExpectedSourceVersion != 1 || !validDigest(intent.ExpectedSourceContentDigest) ||
		intent.OutputProfile != AssembleOutputProfile {
		return ErrInvalidArgument
	}
	return nil
}

// ValidateTrustedContext 校验所有身份、Fence 与 Deadline 都来自可信 Runtime。
func ValidateTrustedContext(value TrustedContext) error {
	for _, id := range []string{
		value.RequestID, value.IdempotencyKey, value.UserID, value.ProjectID, value.SessionID,
		value.InputID, value.TurnID, value.RunID, value.ToolCallID,
	} {
		if !validUUIDv7(id) {
			return ErrInvalidArgument
		}
	}
	if value.FenceToken < 1 || value.DeadlineAt.IsZero() {
		return ErrInvalidArgument
	}
	return nil
}

// ValidateEnsureOperationCommand 校验 Repository 的 first-write-wins 身份与 Tool/Profile 对应关系。
func ValidateEnsureOperationCommand(command EnsureOperationCommand) error {
	if ValidateTrustedContext(command.TrustedContext) != nil || !validDigest(command.ScopeDigest) {
		return ErrInvalidArgument
	}
	if (command.ToolKey == GenerateMediaToolKey && command.OutputProfile == GenerateOutputProfile) ||
		(command.ToolKey == AssembleOutputToolKey && command.OutputProfile == AssembleOutputProfile) {
		return nil
	}
	return ErrInvalidArgument
}

// ValidatePrepareRequest 校验 source 恰好一个且 Tool/Profile/source exact-match。
func ValidatePrepareRequest(request PrepareRequest) error {
	if request.SchemaVersion != PrepareRequestSchemaVersion || !validUUIDv7(request.RequestID) ||
		!validUUIDv7(request.CommandID) || !validUUIDv7(request.OperationID) || !validDigest(request.RequestDigest) ||
		!validUUIDv7(request.UserID) || !validUUIDv7(request.ProjectID) || !validDigest(request.ScopeDigest) ||
		(request.PromptSource == nil) == (request.ImageAssetSource == nil) {
		return ErrInvalidArgument
	}
	switch request.ToolKey {
	case GenerateMediaToolKey:
		if request.OutputProfile != GenerateOutputProfile || request.PromptSource == nil || request.ImageAssetSource != nil ||
			!validUUIDv7(request.PromptSource.PromptPreviewID) || request.PromptSource.Version != 1 ||
			!validDigest(request.PromptSource.ContentDigest) || !validLocalKey(request.PromptSource.TargetLocalKey) {
			return ErrInvalidArgument
		}
	case AssembleOutputToolKey:
		if request.OutputProfile != AssembleOutputProfile || request.ImageAssetSource == nil || request.PromptSource != nil ||
			!validUUIDv7(request.ImageAssetSource.AssetID) || request.ImageAssetSource.Version != 1 ||
			!validDigest(request.ImageAssetSource.ContentDigest) {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// ValidatePrepareQuery 校验 Query 必须复用原 Prepare command、digest 与可信 Owner 范围。
func ValidatePrepareQuery(query PrepareQuery) error {
	if query.SchemaVersion != PrepareQuerySchemaVersion || !validUUIDv7(query.RequestID) ||
		!validUUIDv7(query.CommandID) || !validDigest(query.RequestDigest) ||
		!validUUIDv7(query.UserID) || !validUUIDv7(query.ProjectID) {
		return ErrInvalidArgument
	}
	return nil
}

// ValidatePrepareQueryResult 校验 not_found/completed/conflict 严格联合及 completed 回执身份。
func ValidatePrepareQueryResult(result PrepareQueryResult, query PrepareQuery) error {
	if ValidatePrepareQuery(query) != nil || result.SchemaVersion != PrepareQueryResultVersion ||
		result.RequestID != query.RequestID {
		return ErrInvalidArgument
	}
	switch result.Status {
	case PreparationStatusNotFound, PreparationStatusConflict:
		if result.Result != nil {
			return ErrInvalidArgument
		}
	case PreparationStatusCompleted:
		if result.Result == nil || result.Result.CommandID != query.CommandID {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// ValidatePrepareResult 校验 Business 权威回执与 generate/assemble source/target 联合。
func ValidatePrepareResult(result PrepareResult, request PrepareRequest) error {
	if ValidatePrepareRequest(request) != nil || result.SchemaVersion != PrepareResultSchemaVersion ||
		result.RequestID != request.RequestID || result.CommandID != request.CommandID ||
		(result.Disposition != "created" && result.Disposition != "replayed") || !validUUIDv7(result.PreparationID) ||
		result.OutputProfile != request.OutputProfile || result.CreatedAt.IsZero() ||
		!validReservedAssetRef(result.AssetRef, request.ToolKey) || !validSourceRef(result.SourceRef, request.ToolKey) ||
		!validObjectKey(result.StagingObjectKey) {
		return ErrInvalidArgument
	}
	if request.ToolKey == GenerateMediaToolKey {
		if result.SourceObjectKey != "" || result.SourceRef.SourceID != request.PromptSource.PromptPreviewID ||
			result.SourceRef.SourceVersion != request.PromptSource.Version ||
			result.SourceRef.SourceDigest != request.PromptSource.ContentDigest ||
			result.SourceRef.TargetLocalKey != request.PromptSource.TargetLocalKey {
			return ErrInvalidArgument
		}
	} else if !validObjectKey(result.SourceObjectKey) || result.SourceRef.SourceObjectKey != result.SourceObjectKey ||
		result.SourceRef.SourceID != request.ImageAssetSource.AssetID ||
		result.SourceRef.SourceVersion != request.ImageAssetSource.Version ||
		result.SourceRef.SourceDigest != request.ImageAssetSource.ContentDigest {
		return ErrInvalidArgument
	}
	return nil
}

// ValidateJobSpec 校验单 Job Envelope 不含路径根、Prompt 或动态执行参数。
func ValidateJobSpec(job JobSpec) error {
	for _, id := range []string{job.JobID, job.BatchID, job.OperationID, job.SessionID, job.UserID, job.ProjectID} {
		if !validUUIDv7(id) {
			return ErrInvalidArgument
		}
	}
	if !validDigest(job.ScopeDigest) || !validDigest(job.ArtifactRequestDigest) || job.CreatedAt.IsZero() ||
		job.DeadlineAt.IsZero() || !job.DeadlineAt.After(job.CreatedAt) || !validTarget(job.Target) {
		return ErrInvalidArgument
	}
	switch job.JobType {
	case JobTypeGeneratePNG:
		if job.DefinitionVersion != GenerateMediaDefinitionVersion || job.OutputProfile != GenerateOutputProfile ||
			!validSourceRef(job.SourceRef, GenerateMediaToolKey) {
			return ErrInvalidArgument
		}
	case JobTypeAssembleMP4:
		if job.DefinitionVersion != AssembleOutputDefinitionVersion || job.OutputProfile != AssembleOutputProfile ||
			!validSourceRef(job.SourceRef, AssembleOutputToolKey) {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	return nil
}

// ValidateDispatchCommand 校验派发只使用 EnsureOperation 预分配身份和同一 Prepare 权威回执。
func ValidateDispatchCommand(command DispatchCommand) error {
	if !validDigest(command.DispatchDigest) || ValidateJobSpec(command.Job) != nil ||
		command.Operation.OperationID != command.Job.OperationID || command.Operation.BatchID != command.Job.BatchID ||
		command.Operation.JobID != command.Job.JobID || command.Operation.ScopeDigest != command.Job.ScopeDigest ||
		command.Operation.OutputProfile != command.Job.OutputProfile ||
		command.Preparation.PreparationID != command.Job.Target.PreparationID ||
		command.Preparation.AssetRef.AssetID != command.Job.Target.AssetID ||
		command.Preparation.AssetRef.Version != command.Job.Target.AssetVersion ||
		command.Preparation.StagingObjectKey != command.Job.Target.StagingObjectKey {
		return ErrInvalidArgument
	}
	return nil
}

// ValidDigest 暴露给 PostgreSQL/HTTP Adapter 的协议级 lowercase SHA-256 校验。
func ValidDigest(value string) bool { return validDigest(value) }

// ValidUUIDv7 暴露给 PostgreSQL/HTTP Adapter 的 canonical UUIDv7 校验。
func ValidUUIDv7(value string) bool { return validUUIDv7(value) }

// ValidObjectKey 暴露给严格 Business Client/Repository 的相对 Object Key 校验。
func ValidObjectKey(value string) bool { return validObjectKey(value) }

// DigestJSON 对已验证 exact DTO 生成稳定小写 SHA-256。
func DigestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// CanonicalJSON 返回 struct 字段顺序冻结的 JSON 副本。
func CanonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func validReservedAssetRef(ref AssetRef, toolKey string) bool {
	if !validUUIDv7(ref.AssetID) || ref.Version != 1 || ref.Status != "reserved" ||
		ref.ContentDigest != "" || ref.SizeBytes != 0 {
		return false
	}
	if toolKey == GenerateMediaToolKey {
		return ref.MediaKind == "image" && ref.MIMEType == "image/png"
	}
	return ref.MediaKind == "video" && ref.MIMEType == "video/mp4"
}

func validSourceRef(ref SourceRef, toolKey string) bool {
	if !validUUIDv7(ref.SourceID) || ref.SourceVersion != 1 || !validDigest(ref.SourceDigest) {
		return false
	}
	if toolKey == GenerateMediaToolKey {
		return ref.SourceType == SourceTypePromptPreview && validLocalKey(ref.TargetLocalKey) &&
			validDigest(ref.TargetDigest) && ref.SourceObjectKey == ""
	}
	return ref.SourceType == SourceTypeImageAsset && ref.TargetLocalKey == "" && ref.TargetDigest == "" &&
		validObjectKey(ref.SourceObjectKey)
}

func validTarget(target Target) bool {
	return validUUIDv7(target.AssetID) && target.AssetVersion == 1 && validUUIDv7(target.PreparationID) &&
		validObjectKey(target.StagingObjectKey)
}

func validUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == 7
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func validLocalKey(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && norm.NFC.IsNormalString(value) &&
		targetLocalKeyPattern.MatchString(value)
}

func validObjectKey(value string) bool {
	return safeObjectKeyPattern.MatchString(value) && !strings.HasPrefix(value, "/") &&
		!strings.Contains(value, "\\") && !strings.Contains(value, "\x00") &&
		!strings.Contains(value, "../") && !strings.Contains(value, "/..") && !strings.Contains(value, "//")
}

func strictDecodeJSON(encoded []byte, target any) error {
	if len(encoded) == 0 || len(encoded) > maxMediaJSONBytes || !utf8.Valid(encoded) {
		return ErrInvalidArgument
	}
	if err := rejectDuplicateJSONKeys(encoded); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				key, keyOK := keyToken.(string)
				if keyErr != nil || !keyOK {
					return ErrInvalidArgument
				}
				if _, duplicate := seen[key]; duplicate {
					return ErrInvalidArgument
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return ErrInvalidArgument
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return ErrInvalidArgument
			}
		default:
			return ErrInvalidArgument
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return ErrInvalidArgument
		}
		return err
	}
	return nil
}
