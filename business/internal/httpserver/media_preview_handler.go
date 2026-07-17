package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/mediapreview"
	"github.com/gin-gonic/gin"
)

const mediaPreviewInternalBodyBytes int64 = 64 << 10

// MediaPreviewHandler 提供 local-only 内部回执端点和 Owner 鉴权的媒体内容读取端点。
type MediaPreviewHandler struct {
	service    *mediapreview.Service
	store      *mediapreview.LocalObjectStore
	requestIDs auth.IDGenerator
}

// NewMediaPreviewHandler 校验领域服务、对象根和公开请求 ID 生成器后创建 Handler。
func NewMediaPreviewHandler(
	service *mediapreview.Service,
	store *mediapreview.LocalObjectStore,
	requestIDs auth.IDGenerator,
) (*MediaPreviewHandler, error) {
	if service == nil || store == nil || requestIDs == nil || !store.Ready() {
		return nil, errors.New("create media preview HTTP handler: invalid dependency")
	}
	return &MediaPreviewHandler{service: service, store: store, requestIDs: requestIDs}, nil
}

// RegisterInternal 注册只接受 loopback 请求的五个冻结内部端点。
func (handler *MediaPreviewHandler) RegisterInternal(router gin.IRoutes) {
	router.POST("/internal/v1/media-preview-assets/prepare", handler.prepare)
	router.POST("/internal/v1/media-preview-assets/query-preparation", handler.queryPreparation)
	router.POST("/internal/v1/media-preview-assets/finalize", handler.finalize)
	router.POST("/internal/v1/media-preview-assets/query-finalization", handler.queryFinalization)
	router.GET("/internal/v1/media-preview-assets/readiness", handler.readiness)
}

// RegisterContent 注册只按 Project/Asset 标识读取的 GET 与 HEAD 资源端点。
func (handler *MediaPreviewHandler) RegisterContent(router gin.IRoutes, requireSession gin.HandlerFunc) {
	path := "/api/v1/projects/:project_id/media-preview-assets/:asset_id/content"
	router.GET(path, requireSession, handler.content)
	router.HEAD(path, requireSession, handler.content)
}

type prepareMediaPreviewRequestV1 struct {
	SchemaVersion    string                     `json:"schema_version"`
	RequestID        string                     `json:"request_id"`
	CommandID        string                     `json:"command_id"`
	OperationID      string                     `json:"operation_id"`
	RequestDigest    string                     `json:"request_digest"`
	UserID           string                     `json:"user_id"`
	ProjectID        string                     `json:"project_id"`
	ToolKey          string                     `json:"tool_key"`
	ScopeDigest      string                     `json:"scope_digest"`
	OutputProfile    string                     `json:"output_profile"`
	PromptSource     *preparePromptSourceV1     `json:"prompt_source,omitempty"`
	ImageAssetSource *prepareImageAssetSourceV1 `json:"image_asset_source,omitempty"`
}

type preparePromptSourceV1 struct {
	PromptPreviewID string `json:"prompt_preview_id"`
	Version         int64  `json:"version"`
	ContentDigest   string `json:"content_digest"`
	TargetLocalKey  string `json:"target_local_key"`
}

type prepareImageAssetSourceV1 struct {
	AssetID       string `json:"asset_id"`
	Version       int64  `json:"version"`
	ContentDigest string `json:"content_digest"`
}

type mediaPreviewAssetRefV1 struct {
	AssetID       string `json:"asset_id"`
	Version       int64  `json:"version"`
	Status        string `json:"status"`
	MediaKind     string `json:"media_kind"`
	MIMEType      string `json:"mime_type"`
	ContentDigest string `json:"content_digest,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
}

type mediaPreviewSourceRefV1 struct {
	SourceType      string `json:"source_type"`
	SourceID        string `json:"source_id"`
	SourceVersion   int64  `json:"source_version"`
	SourceDigest    string `json:"source_digest"`
	TargetLocalKey  string `json:"target_local_key,omitempty"`
	TargetDigest    string `json:"target_digest,omitempty"`
	SourceObjectKey string `json:"source_object_key,omitempty"`
}

type prepareMediaPreviewResultV1 struct {
	SchemaVersion    string                  `json:"schema_version"`
	RequestID        string                  `json:"request_id"`
	CommandID        string                  `json:"command_id"`
	Disposition      string                  `json:"disposition"`
	PreparationID    string                  `json:"preparation_id"`
	AssetRef         mediaPreviewAssetRefV1  `json:"asset_ref"`
	SourceRef        mediaPreviewSourceRefV1 `json:"source_ref"`
	OutputProfile    string                  `json:"output_profile"`
	StagingObjectKey string                  `json:"staging_object_key"`
	SourceObjectKey  string                  `json:"source_object_key,omitempty"`
	CreatedAt        string                  `json:"created_at"`
}

type queryPreparationRequestV1 struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	CommandID     string `json:"command_id"`
	RequestDigest string `json:"request_digest"`
	UserID        string `json:"user_id"`
	ProjectID     string `json:"project_id"`
}

type queryPreparationResultV1 struct {
	SchemaVersion string                       `json:"schema_version"`
	RequestID     string                       `json:"request_id"`
	Status        string                       `json:"status"`
	Result        *prepareMediaPreviewResultV1 `json:"result,omitempty"`
}

type finalizeOutputV1 struct {
	ContentDigest string `json:"content_digest"`
	SizeBytes     int64  `json:"size_bytes"`
	MIMEType      string `json:"mime_type"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Codec         string `json:"codec,omitempty"`
	PixelFormat   string `json:"pixel_format,omitempty"`
}

type finalizeMediaPreviewRequestV1 struct {
	SchemaVersion  string            `json:"schema_version"`
	RequestID      string            `json:"request_id"`
	CommandID      string            `json:"command_id"`
	RequestDigest  string            `json:"request_digest"`
	PreparationID  string            `json:"preparation_id"`
	OperationID    string            `json:"operation_id"`
	BatchID        string            `json:"batch_id"`
	JobID          string            `json:"job_id"`
	AttemptID      string            `json:"attempt_id"`
	Fence          int64             `json:"fence"`
	TerminalStatus string            `json:"terminal_status"`
	Output         *finalizeOutputV1 `json:"output,omitempty"`
	ErrorCode      string            `json:"error_code,omitempty"`
}

type finalizeMediaPreviewResultV1 struct {
	SchemaVersion         string                 `json:"schema_version"`
	RequestID             string                 `json:"request_id"`
	CommandID             string                 `json:"command_id"`
	Disposition           string                 `json:"disposition"`
	AssetRef              mediaPreviewAssetRefV1 `json:"asset_ref"`
	FinalizationReceiptID string                 `json:"finalization_receipt_id"`
	CompletedAt           string                 `json:"completed_at"`
}

type queryFinalizationRequestV1 struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	CommandID     string `json:"command_id"`
	RequestDigest string `json:"request_digest"`
	PreparationID string `json:"preparation_id"`
}

type queryFinalizationResultV1 struct {
	SchemaVersion string                        `json:"schema_version"`
	RequestID     string                        `json:"request_id"`
	Status        string                        `json:"status"`
	Result        *finalizeMediaPreviewResultV1 `json:"result,omitempty"`
	ErrorCode     string                        `json:"error_code,omitempty"`
}

func (handler *MediaPreviewHandler) prepare(c *gin.Context) {
	if !handler.requireLoopback(c) {
		return
	}
	var request prepareMediaPreviewRequestV1
	if !handler.decodeInternalJSON(c, &request, projectEmergencyRequestID) {
		return
	}
	command, err := request.command()
	if err != nil {
		handler.writeInternalError(c, err, safeMediaRequestID(request.RequestID))
		return
	}
	result, err := handler.service.Prepare(c.Request.Context(), command)
	if err != nil {
		handler.writeInternalError(c, err, request.RequestID)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, prepareResultResponse(result, request.RequestID, string(result.Disposition)))
}

func (handler *MediaPreviewHandler) queryPreparation(c *gin.Context) {
	if !handler.requireLoopback(c) {
		return
	}
	var request queryPreparationRequestV1
	if !handler.decodeInternalJSON(c, &request, projectEmergencyRequestID) {
		return
	}
	digest, err := mediapreview.ParseDigest(request.RequestDigest)
	if err != nil || request.SchemaVersion != mediapreview.QueryPreparationRequestSchemaVersion ||
		!mediapreview.CanonicalUUIDv7(request.RequestID) {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, safeMediaRequestID(request.RequestID))
		return
	}
	result, err := handler.service.QueryPreparation(c.Request.Context(), mediapreview.PreparationQuery{
		CommandID: request.CommandID, RequestDigest: digest, OwnerUserID: request.UserID, ProjectID: request.ProjectID,
	})
	if err != nil {
		handler.writeInternalError(c, err, request.RequestID)
		return
	}
	response := queryPreparationResultV1{
		SchemaVersion: mediapreview.QueryPreparationResultSchemaVersion,
		RequestID:     request.RequestID,
		Status:        string(result.Status),
	}
	if result.Status == mediapreview.QueryStatusCompleted && result.Preparation != nil {
		mapped := prepareResponseFromPreparation(*result.Preparation, request.RequestID, "replayed")
		response.Result = &mapped
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func (handler *MediaPreviewHandler) finalize(c *gin.Context) {
	if !handler.requireLoopback(c) {
		return
	}
	var request finalizeMediaPreviewRequestV1
	if !handler.decodeInternalJSON(c, &request, projectEmergencyRequestID) {
		return
	}
	command, err := request.command()
	if err != nil {
		handler.writeInternalError(c, err, safeMediaRequestID(request.RequestID))
		return
	}
	result, err := handler.service.Finalize(c.Request.Context(), command)
	if err != nil {
		handler.writeInternalError(c, err, request.RequestID)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, finalizeResultResponse(result.Finalization, request.RequestID, string(result.Disposition)))
}

func (handler *MediaPreviewHandler) queryFinalization(c *gin.Context) {
	if !handler.requireLoopback(c) {
		return
	}
	var request queryFinalizationRequestV1
	if !handler.decodeInternalJSON(c, &request, projectEmergencyRequestID) {
		return
	}
	digest, err := mediapreview.ParseDigest(request.RequestDigest)
	if err != nil || request.SchemaVersion != mediapreview.QueryFinalizationRequestSchemaVersion ||
		!mediapreview.CanonicalUUIDv7(request.RequestID) {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, safeMediaRequestID(request.RequestID))
		return
	}
	result, err := handler.service.QueryFinalization(c.Request.Context(), mediapreview.FinalizationQuery{
		CommandID: request.CommandID, RequestDigest: digest, PreparationID: request.PreparationID,
	})
	if err != nil {
		handler.writeInternalError(c, err, request.RequestID)
		return
	}
	response := queryFinalizationResultV1{
		SchemaVersion: mediapreview.QueryFinalizationResultSchemaVersion,
		RequestID:     request.RequestID,
		Status:        string(result.Status),
	}
	if result.Status == mediapreview.QueryStatusCompleted && result.Finalization != nil {
		mapped := finalizeResultResponse(*result.Finalization, request.RequestID, "replayed")
		response.Result = &mapped
	} else if result.Status == mediapreview.QueryStatusConflict {
		response.ErrorCode = "IDEMPOTENCY_CONFLICT"
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func (handler *MediaPreviewHandler) readiness(c *gin.Context) {
	if !handler.requireLoopback(c) {
		return
	}
	if c.Request.URL.RawQuery != "" || !handler.store.Ready() {
		status := http.StatusBadRequest
		if !handler.store.Ready() {
			status = http.StatusServiceUnavailable
		}
		c.Header("Cache-Control", "no-store")
		c.Status(status)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"schema_version":    mediapreview.ReadinessSchemaVersion,
		"profile":           mediapreview.RuntimeProfile,
		"object_root_ready": true,
		"prepare":           true,
		"finalize":          true,
	})
}

func (handler *MediaPreviewHandler) content(c *gin.Context) {
	requestID, err := handler.requestIDs.New()
	if err != nil || !mediapreview.CanonicalUUIDv7(requestID) {
		handler.writeContentError(c, http.StatusServiceUnavailable, projectEmergencyRequestID)
		return
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		handler.writeContentError(c, http.StatusNotFound, requestID)
		return
	}
	if c.Request.URL.RawQuery != "" {
		handler.writeContentError(c, http.StatusBadRequest, requestID)
		return
	}
	content, file, err := handler.service.OpenReadyContent(c.Request.Context(), mediapreview.ContentQuery{
		OwnerUserID: principal.ID, ProjectID: c.Param("project_id"), AssetID: c.Param("asset_id"),
	})
	if err != nil {
		if errors.Is(err, mediapreview.ErrPersistence) || errors.Is(err, mediapreview.ErrDependencyNotReady) ||
			errors.Is(err, mediapreview.ErrUnknownOutcome) {
			handler.writeContentError(c, http.StatusServiceUnavailable, requestID)
		} else {
			handler.writeContentError(c, http.StatusNotFound, requestID)
		}
		return
	}
	defer file.Close()

	start, end, partial, ok := parseSingleMediaRange(c.Request.Header.Values("Range"), content.AssetRef.SizeBytes)
	if !ok {
		setMediaContentHeaders(c, content)
		c.Header("Content-Range", "bytes */"+strconv.FormatInt(content.AssetRef.SizeBytes, 10))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	length := content.AssetRef.SizeBytes
	status := http.StatusOK
	if partial {
		length = end - start + 1
		status = http.StatusPartialContent
		c.Header("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+
			strconv.FormatInt(content.AssetRef.SizeBytes, 10))
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			handler.writeContentError(c, http.StatusServiceUnavailable, requestID)
			return
		}
	}
	setMediaContentHeaders(c, content)
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	c.Status(status)
	if c.Request.Method == http.MethodHead {
		return
	}
	_, _ = io.CopyN(c.Writer, file, length)
}

func (request prepareMediaPreviewRequestV1) command() (mediapreview.PrepareCommand, error) {
	requestDigest, err := mediapreview.ParseDigest(request.RequestDigest)
	if err != nil || request.SchemaVersion != mediapreview.PrepareRequestSchemaVersion {
		return mediapreview.PrepareCommand{}, mediapreview.ErrInvalidArgument
	}
	scopeDigest, err := mediapreview.ParseDigest(request.ScopeDigest)
	if err != nil {
		return mediapreview.PrepareCommand{}, mediapreview.ErrInvalidArgument
	}
	command := mediapreview.PrepareCommand{
		RequestID: request.RequestID, CommandID: request.CommandID, OperationID: request.OperationID,
		RequestDigest: requestDigest, OwnerUserID: request.UserID, ProjectID: request.ProjectID,
		ToolKey: request.ToolKey, ScopeDigest: scopeDigest, OutputProfile: request.OutputProfile,
	}
	if request.PromptSource != nil {
		digest, parseErr := mediapreview.ParseDigest(request.PromptSource.ContentDigest)
		if parseErr != nil {
			return mediapreview.PrepareCommand{}, mediapreview.ErrInvalidArgument
		}
		command.PromptSource = &mediapreview.PromptSource{
			ID: request.PromptSource.PromptPreviewID, Version: request.PromptSource.Version,
			ContentDigest: digest, TargetLocalKey: request.PromptSource.TargetLocalKey,
		}
	}
	if request.ImageAssetSource != nil {
		digest, parseErr := mediapreview.ParseDigest(request.ImageAssetSource.ContentDigest)
		if parseErr != nil {
			return mediapreview.PrepareCommand{}, mediapreview.ErrInvalidArgument
		}
		command.ImageAssetSource = &mediapreview.ImageAssetSource{
			ID: request.ImageAssetSource.AssetID, Version: request.ImageAssetSource.Version, ContentDigest: digest,
		}
	}
	if command.Validate() != nil {
		return mediapreview.PrepareCommand{}, mediapreview.ErrInvalidArgument
	}
	return command, nil
}

func (request finalizeMediaPreviewRequestV1) command() (mediapreview.FinalizeCommand, error) {
	requestDigest, err := mediapreview.ParseDigest(request.RequestDigest)
	if err != nil || request.SchemaVersion != mediapreview.FinalizeRequestSchemaVersion {
		return mediapreview.FinalizeCommand{}, mediapreview.ErrInvalidArgument
	}
	command := mediapreview.FinalizeCommand{
		RequestID: request.RequestID, CommandID: request.CommandID, RequestDigest: requestDigest,
		PreparationID: request.PreparationID, OperationID: request.OperationID, BatchID: request.BatchID,
		JobID: request.JobID, AttemptID: request.AttemptID, Fence: request.Fence,
		TerminalStatus: request.TerminalStatus, ErrorCode: request.ErrorCode,
	}
	if request.Output != nil {
		digest, parseErr := mediapreview.ParseDigest(request.Output.ContentDigest)
		if parseErr != nil {
			return mediapreview.FinalizeCommand{}, mediapreview.ErrInvalidArgument
		}
		command.Output = &mediapreview.OutputMetadata{
			ContentDigest: digest, SizeBytes: request.Output.SizeBytes, MIMEType: request.Output.MIMEType,
			Width: request.Output.Width, Height: request.Output.Height, DurationMS: request.Output.DurationMS,
			Codec: request.Output.Codec, PixelFormat: request.Output.PixelFormat,
		}
	}
	if command.Validate() != nil {
		return mediapreview.FinalizeCommand{}, mediapreview.ErrInvalidArgument
	}
	return command, nil
}

func prepareResultResponse(result mediapreview.PrepareResult, requestID string, disposition string) prepareMediaPreviewResultV1 {
	return prepareResponseFromPreparation(result.Preparation, requestID, disposition)
}

func prepareResponseFromPreparation(
	preparation mediapreview.Preparation,
	requestID string,
	disposition string,
) prepareMediaPreviewResultV1 {
	return prepareMediaPreviewResultV1{
		SchemaVersion: mediapreview.PrepareResultSchemaVersion,
		RequestID:     requestID, CommandID: preparation.CommandID, Disposition: disposition,
		PreparationID: preparation.PreparationID, AssetRef: assetRefResponse(preparation.AssetRef),
		SourceRef: sourceRefResponse(preparation.SourceRef), OutputProfile: preparation.OutputProfile,
		StagingObjectKey: preparation.StagingObjectKey, SourceObjectKey: preparation.SourceRef.SourceObjectKey,
		CreatedAt: preparation.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

func finalizeResultResponse(
	finalization mediapreview.Finalization,
	requestID string,
	disposition string,
) finalizeMediaPreviewResultV1 {
	return finalizeMediaPreviewResultV1{
		SchemaVersion: mediapreview.FinalizeResultSchemaVersion,
		RequestID:     requestID, CommandID: finalization.CommandID, Disposition: disposition,
		AssetRef: assetRefResponse(finalization.AssetRef), FinalizationReceiptID: finalization.ReceiptID,
		CompletedAt: finalization.CompletedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

func assetRefResponse(reference mediapreview.AssetRef) mediaPreviewAssetRefV1 {
	response := mediaPreviewAssetRefV1{
		AssetID: reference.AssetID, Version: reference.Version, Status: reference.Status,
		MediaKind: reference.MediaKind, MIMEType: reference.MIMEType, SizeBytes: reference.SizeBytes,
	}
	if reference.ContentDigest != (mediapreview.Digest{}) {
		response.ContentDigest = reference.ContentDigest.Hex()
	}
	return response
}

func sourceRefResponse(reference mediapreview.SourceRef) mediaPreviewSourceRefV1 {
	response := mediaPreviewSourceRefV1{
		SourceType: reference.SourceType, SourceID: reference.SourceID, SourceVersion: reference.SourceVersion,
		SourceDigest: reference.SourceDigest.Hex(), TargetLocalKey: reference.TargetLocalKey,
		SourceObjectKey: reference.SourceObjectKey,
	}
	if reference.TargetDigest != (mediapreview.Digest{}) {
		response.TargetDigest = reference.TargetDigest.Hex()
	}
	return response
}

func (handler *MediaPreviewHandler) decodeInternalJSON(c *gin.Context, output any, requestID string) bool {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, requestID)
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" || len(parameters) != 0 || c.Request.URL.RawQuery != "" {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, requestID)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mediaPreviewInternalBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(raw) == 0 || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, requestID)
		return false
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, requestID)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil || ensureJSONEOF(decoder) != nil {
		handler.writeInternalError(c, mediapreview.ErrInvalidArgument, requestID)
		return false
	}
	return true
}

func (handler *MediaPreviewHandler) requireLoopback(c *gin.Context) bool {
	if !isLoopbackRemote(c.Request.RemoteAddr) {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusNotFound)
		return false
	}
	return true
}

func isLoopbackRemote(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	parsed := net.ParseIP(strings.Trim(host, "[]"))
	return parsed != nil && parsed.IsLoopback()
}

func safeMediaRequestID(value string) string {
	if mediapreview.CanonicalUUIDv7(value) {
		return value
	}
	return projectEmergencyRequestID
}

func (handler *MediaPreviewHandler) writeInternalError(c *gin.Context, err error, requestID string) {
	status, code, retryable := http.StatusServiceUnavailable, "PERSISTENCE_UNAVAILABLE", true
	switch {
	case errors.Is(err, mediapreview.ErrInvalidArgument):
		status, code, retryable = http.StatusBadRequest, "INVALID_ARGUMENT", false
	case errors.Is(err, mediapreview.ErrNotFound):
		status, code, retryable = http.StatusNotFound, "NOT_FOUND", false
	case errors.Is(err, mediapreview.ErrVersionConflict):
		status, code, retryable = http.StatusConflict, "VERSION_CONFLICT", false
	case errors.Is(err, mediapreview.ErrIdempotencyConflict):
		status, code, retryable = http.StatusConflict, "IDEMPOTENCY_CONFLICT", false
	case errors.Is(err, mediapreview.ErrFenceStale):
		status, code, retryable = http.StatusConflict, "FENCE_STALE", false
	case errors.Is(err, mediapreview.ErrArtifactInvalid):
		status, code, retryable = http.StatusUnprocessableEntity, "ARTIFACT_INVALID", false
	case errors.Is(err, mediapreview.ErrDependencyNotReady):
		status, code, retryable = http.StatusServiceUnavailable, "DEPENDENCY_NOT_READY", true
	case errors.Is(err, mediapreview.ErrUnknownOutcome):
		status, code, retryable = http.StatusServiceUnavailable, "UNKNOWN_OUTCOME", true
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: "媒体预览请求未完成", RequestID: safeMediaRequestID(requestID),
		Retryable: retryable, Details: ErrorDetails{},
	}})
}

func (handler *MediaPreviewHandler) writeContentError(c *gin.Context, status int, requestID string) {
	code, message, retryable := "MEDIA_ASSET_NOT_FOUND", "媒体内容不存在或不可访问", false
	if status == http.StatusBadRequest {
		code, message = "INVALID_ARGUMENT", "媒体内容请求无效"
	} else if status == http.StatusServiceUnavailable {
		code, message, retryable = "MEDIA_CONTENT_UNAVAILABLE", "媒体内容暂时不可用", true
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: safeMediaRequestID(requestID), Retryable: retryable, Details: ErrorDetails{},
	}})
}

func setMediaContentHeaders(c *gin.Context, content mediapreview.ReadyContent) {
	c.Header("Content-Type", content.AssetRef.MIMEType)
	c.Header("ETag", `"`+content.AssetRef.ContentDigest.Hex()+`"`)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "private, no-store")
}

func parseSingleMediaRange(values []string, size int64) (start int64, end int64, partial bool, ok bool) {
	if size <= 0 {
		return 0, 0, false, false
	}
	if len(values) == 0 {
		return 0, size - 1, false, true
	}
	if len(values) != 1 || !strings.HasPrefix(values[0], "bytes=") || strings.Contains(values[0], ",") {
		return 0, 0, false, false
	}
	parts := strings.Split(strings.TrimPrefix(values[0], "bytes="), "-")
	if len(parts) != 2 || parts[0] == "" {
		return 0, 0, false, false
	}
	start, firstErr := strconv.ParseInt(parts[0], 10, 64)
	if firstErr != nil || start < 0 || start >= size {
		return 0, 0, false, false
	}
	end = size - 1
	if parts[1] != "" {
		var secondErr error
		end, secondErr = strconv.ParseInt(parts[1], 10, 64)
		if secondErr != nil || end < start {
			return 0, 0, false, false
		}
	}
	if end >= size {
		end = size - 1
	}
	return start, end, true, true
}
