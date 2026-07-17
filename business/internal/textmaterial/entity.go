// Package textmaterial 定义 Project 内最小文本素材的创建、幂等与列表契约。
package textmaterial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

const (
	// AssetVersion 是最小文本素材固定使用的不可变素材版本。
	AssetVersion int64 = 1
	// MaxContentCharacters 是文本素材允许的 Unicode 字符上限。
	MaxContentCharacters = 2000
	// MaxListItems 是 Project 工作台一次最多读取的文本素材数量。
	MaxListItems = 100
	// ExtractorSchemaVersion 标识人工文本输入生成 Evidence 的稳定结构版本。
	ExtractorSchemaVersion = "text_material.manual.v1"
	// ExtractorVersion 标识当前 Business 文本 Evidence 生成实现版本。
	ExtractorVersion = "business-text-material.v1"
)

var (
	// ErrInvalidArgument 表示身份、Project、幂等键或正文不符合最小文本素材契约。
	ErrInvalidArgument = errors.New("text material invalid argument")
	// ErrProjectNotFound 表示 Project 不存在或不属于可信用户，二者对外保持相同语义。
	ErrProjectNotFound = errors.New("text material project not found")
	// ErrIdempotencyConflict 表示同一 asset_id 已绑定到不同 Project、用户或正文语义。
	ErrIdempotencyConflict = errors.New("text material idempotency conflict")
	// ErrPersistence 表示 PostgreSQL 暂不可用或已有数据违反文本素材不变量。
	ErrPersistence = errors.New("text material persistence unavailable")
)

// CreateCommand 是 HTTP Handler 传入应用服务的可信文本素材创建命令。
type CreateCommand struct {
	// OwnerUserID 只来自认证 Principal，不接受请求体覆盖。
	OwnerUserID string
	// ProjectID 来自受保护资源路径，并在事务内重新校验所有权。
	ProjectID string
	// IdempotencyKey 必须是规范 UUIDv7，并直接成为素材 asset_id。
	IdempotencyKey string
	// Content 是已经为 NFC、长度有界且不含非法控制字符的完整文本正文。
	Content string
}

// TextMaterial 是 Project 内可直接供 analyze_materials 读取的不可变文本素材。
type TextMaterial struct {
	// AssetID 是素材 UUIDv7，也是创建请求的 Idempotency-Key。
	AssetID string
	// EvidenceID 是唯一 text_segment Evidence UUIDv7。
	EvidenceID string
	// OwnerUserID 是素材所属可信用户 UUIDv7。
	OwnerUserID string
	// ProjectID 是素材所属 Project UUIDv7。
	ProjectID string
	// AssetVersion 固定为 1；编辑和新版本不属于本最小纵切。
	AssetVersion int64
	// ContentDigest 是 NFC 正文 UTF-8 字节的小写 SHA-256。
	ContentDigest string
	// Content 是列表返回并供当前 Project 内选择的完整文本正文。
	Content string
	// CreatedAt 是素材与 Evidence 同事务创建的 UTC 时间。
	CreatedAt time.Time
}

// Validate 校验素材、Evidence 与正文的完整不可变不变量。
// 失败表示该对象不得进入持久化或 HTTP 响应。
func (material TextMaterial) Validate() error {
	if !CanonicalUUIDv7(material.AssetID) || !CanonicalUUIDv7(material.EvidenceID) ||
		!CanonicalUUIDv7(material.OwnerUserID) || !CanonicalUUIDv7(material.ProjectID) ||
		material.AssetVersion != AssetVersion || !ValidContent(material.Content) ||
		material.ContentDigest != ContentDigest(material.Content) || material.CreatedAt.IsZero() ||
		material.CreatedAt.Location() != time.UTC {
		return ErrInvalidArgument
	}
	return nil
}

// CreateResult 是首次创建和同义重放共用的安全结果。
type CreateResult struct {
	// Material 是首次事务提交或幂等回放得到的原始不可变素材。
	Material TextMaterial
	// Replayed 表示 asset_id 已存在且正文、Owner 与 Project 语义完全一致。
	Replayed bool
}

// ListQuery 是按可信 Owner 与 Project 读取文本素材的固定上限查询。
type ListQuery struct {
	// OwnerUserID 只来自认证 Principal。
	OwnerUserID string
	// ProjectID 来自当前工作台路径。
	ProjectID string
	// Limit 是一至 MaxListItems 的固定有界结果数量。
	Limit int
}

// Validate 校验列表查询不会形成越权或无界扫描。
func (query ListQuery) Validate() error {
	if !CanonicalUUIDv7(query.OwnerUserID) || !CanonicalUUIDv7(query.ProjectID) ||
		query.Limit < 1 || query.Limit > MaxListItems {
		return ErrInvalidArgument
	}
	return nil
}

// Repository 定义文本素材事务创建与固定数量集合列表查询边界。
type Repository interface {
	// CreateOrReplay 在一个 GORM 事务中校验 Project Owner、占有 asset_id 并写入 Asset 与 Evidence。
	CreateOrReplay(ctx context.Context, material TextMaterial) (CreateResult, error)
	// ListOwned 先固定查询 Project Owner，再用一次有界集合 SQL 返回 created_at DESC、id DESC 的文本素材。
	ListOwned(ctx context.Context, query ListQuery) ([]TextMaterial, error)
}

// CanonicalUUIDv7 只接受小写连字符格式 UUIDv7，避免同一幂等键出现多种文本表示。
func CanonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// ValidContent 校验正文为 NFC、1..2000 个 Unicode 字符且可安全作为文本 Evidence。
func ValidContent(value string) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) == "" {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < 1 || length > MaxContentCharacters {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return false
		}
	}
	return true
}

// ContentDigest 计算规范正文 UTF-8 字节的小写 SHA-256。
func ContentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}
