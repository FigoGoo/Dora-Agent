// Package assetanalysis 定义素材分析输入开发预览的严格领域契约。
package assetanalysis

import (
	"context"
	"errors"
	"time"
)

const (
	// RPCSchemaVersion 是 Business 与 Agent 预览 RPC 的冻结协议版本。
	RPCSchemaVersion = "asset_analysis_inputs.preview.rpc.v1"
	// MaxAssets 是一次批量读取允许的素材上限。
	MaxAssets = 8
	// MaxEvidence 是一次完整响应允许的证据上限。
	MaxEvidence = 32
)

var (
	// ErrInvalidArgument 表示请求结构、标识或排序不符合冻结契约。
	ErrInvalidArgument = errors.New("asset analysis preview invalid argument")
	// ErrNotFound 统一隐藏 Project、Owner、Asset 与可用状态差异。
	ErrNotFound = errors.New("asset analysis preview resource not found")
	// ErrVersionConflict 表示授权完成后发现调用方期望的素材版本已过期。
	ErrVersionConflict = errors.New("asset analysis preview asset version conflict")
	// ErrLimitExceeded 表示完整响应会超过冻结资源上限。
	ErrLimitExceeded = errors.New("asset analysis preview limit exceeded")
	// ErrEvidenceConflict 表示权威证据违反内容、摘要、定位器或版本不变量。
	ErrEvidenceConflict = errors.New("asset analysis preview evidence conflict")
	// ErrPersistence 表示权威存储暂时不可用或返回了无法安全解释的数据。
	ErrPersistence = errors.New("asset analysis preview persistence unavailable")
)

// MediaType 是冻结的素材媒体类型。
type MediaType string

const (
	MediaTypeText  MediaType = "text"
	MediaTypeImage MediaType = "image"
)

// EvidenceKind 是冻结的证据种类。
type EvidenceKind string

const (
	EvidenceKindTextSegment       EvidenceKind = "text_segment"
	EvidenceKindVisualDescription EvidenceKind = "visual_description"
	EvidenceKindSafetyLabel       EvidenceKind = "safety_label"
)

// Availability 是冻结的证据可用性。
type Availability string

const (
	AvailabilityReady       Availability = "ready"
	AvailabilityMissing     Availability = "missing"
	AvailabilityFailed      Availability = "failed"
	AvailabilityRedacted    Availability = "redacted"
	AvailabilityUnsupported Availability = "unsupported"
)

// LocatorKind 是冻结的强类型证据定位器。
type LocatorKind string

const (
	LocatorKindTextRange   LocatorKind = "text_range"
	LocatorKindImageWhole  LocatorKind = "image_whole"
	LocatorKindImageRegion LocatorKind = "image_region"
)

// Target 是请求中的单个规范化素材目标。
type Target struct {
	AssetID              string
	ExpectedAssetVersion *int64
}

// Query 是一次授权批量读取命令。
type Query struct {
	SchemaVersion string
	RequestID     string
	UserID        string
	ProjectID     string
	Targets       []Target
}

// RepositoryQuery 是授权查询所需的最小不可变参数，不包含版本断言。
type RepositoryQuery struct {
	UserID    string
	ProjectID string
	AssetIDs  []string
}

// Locator 保存三种定位器的互斥字段；指针用于保留合法零值。
type Locator struct {
	Kind             LocatorKind `json:"kind"`
	TextStart        *int64      `json:"text_start,omitempty"`
	TextEnd          *int64      `json:"text_end,omitempty"`
	TextSourceLength *int64      `json:"text_source_length,omitempty"`
	ImageX           *int32      `json:"image_x,omitempty"`
	ImageY           *int32      `json:"image_y,omitempty"`
	ImageWidth       *int32      `json:"image_width,omitempty"`
	ImageHeight      *int32      `json:"image_height,omitempty"`
}

// Evidence 是与特定素材版本绑定的不可变证据事实。
type Evidence struct {
	ID                     string
	AssetID                string
	AssetVersion           int64
	MediaType              MediaType
	Kind                   EvidenceKind
	Availability           Availability
	ReasonCode             string
	ContentDigest          string
	ExtractorSchemaVersion string
	ExtractorVersion       string
	Locator                *Locator
	Content                string
	CreatedAt              time.Time
}

// Asset 是已授权 Project 下的单个就绪素材及其完整证据集合。
type Asset struct {
	ID        string
	Version   int64
	MediaType MediaType
	Evidence  []Evidence
	CreatedAt time.Time
}

// Snapshot 是严格完整、稳定排序的预览读取结果。
type Snapshot struct {
	SnapshotToken    string
	ResponseComplete bool
	Assets           []Asset
}

// Repository 定义必须由单次集合 SQL 实现的授权读取边界。
type Repository interface {
	BatchGetAuthorized(ctx context.Context, query RepositoryQuery) ([]Asset, error)
}
