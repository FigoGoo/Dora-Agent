package postgres

import "time"

// textMaterialAssetModel 映射既有素材分析预览 Asset 表中的文本素材头。
type textMaterialAssetModel struct {
	// ID 是由 Idempotency-Key 固定的素材 UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// OwnerUserID 是素材所属用户逻辑关联标识。
	OwnerUserID string `gorm:"column:owner_user_id;type:uuid"`
	// ProjectID 是素材所属 Project 逻辑关联标识。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// AssetVersion 是不可变内容版本，最小文本素材固定为 1。
	AssetVersion int64 `gorm:"column:asset_version"`
	// MediaType 固定为 text。
	MediaType string `gorm:"column:media_type"`
	// Status 固定为 ready，使 analyze_materials 能立即读取。
	Status string `gorm:"column:status"`
	// CreatedAt 是素材创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回既有素材分析预览 Asset 权威表，禁止 GORM 推导或建表。
func (textMaterialAssetModel) TableName() string {
	return "business.asset_analysis_preview_assets"
}

// textMaterialEvidenceModel 映射既有不可变 Evidence 表中的完整文本片段。
type textMaterialEvidenceModel struct {
	// ID 是 Evidence UUIDv7 主键。
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// AssetID 是所属文本素材逻辑关联标识。
	AssetID string `gorm:"column:asset_id;type:uuid"`
	// AssetVersion 固定与 Asset 版本 1 一致。
	AssetVersion int64 `gorm:"column:asset_version"`
	// MediaType 固定为 text。
	MediaType string `gorm:"column:media_type"`
	// EvidenceKind 固定为 text_segment。
	EvidenceKind string `gorm:"column:evidence_kind"`
	// Availability 固定为 ready。
	Availability string `gorm:"column:availability"`
	// ContentDigest 是正文 UTF-8 字节的小写 SHA-256。
	ContentDigest string `gorm:"column:content_digest"`
	// ExtractorSchemaVersion 是人工文本输入的稳定输出结构版本。
	ExtractorSchemaVersion string `gorm:"column:extractor_schema_version"`
	// ExtractorVersion 是 Business 人工文本 Evidence 生成实现版本。
	ExtractorVersion string `gorm:"column:extractor_version"`
	// LocatorKind 固定为 text_range。
	LocatorKind string `gorm:"column:locator_kind"`
	// TextStart 固定为完整正文起点 0。
	TextStart int64 `gorm:"column:text_start"`
	// TextEnd 是完整正文 Unicode 字符数。
	TextEnd int64 `gorm:"column:text_end"`
	// TextSourceLength 与 TextEnd 相同，表示 Evidence 覆盖完整正文。
	TextSourceLength int64 `gorm:"column:text_source_length"`
	// Content 是不可变完整文本正文。
	Content string `gorm:"column:content"`
	// CreatedAt 是 Evidence 与 Asset 同事务创建的 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回既有素材分析预览 Evidence 权威表，表上的触发器拒绝后续修改和删除。
func (textMaterialEvidenceModel) TableName() string {
	return "business.asset_analysis_preview_evidence"
}

// textMaterialReadDTO 承载一次集合读取的 Asset、唯一 Evidence 与完整性计数。
type textMaterialReadDTO struct {
	// AssetID 是文本素材 UUIDv7。
	AssetID string `gorm:"column:asset_id"`
	// OwnerUserID 是素材所属用户 UUIDv7。
	OwnerUserID string `gorm:"column:owner_user_id"`
	// ProjectID 是素材所属 Project UUIDv7。
	ProjectID string `gorm:"column:project_id"`
	// AssetVersion 是不可变素材版本。
	AssetVersion int64 `gorm:"column:asset_version"`
	// MediaType 是 Asset 媒体类型。
	MediaType string `gorm:"column:media_type"`
	// Status 是 Asset 可用状态。
	Status string `gorm:"column:status"`
	// EvidenceID 是唯一 text_segment Evidence UUIDv7；缺失时为空。
	EvidenceID *string `gorm:"column:evidence_id"`
	// EvidenceMediaType 是 Evidence 媒体类型；缺失时为空。
	EvidenceMediaType *string `gorm:"column:evidence_media_type"`
	// EvidenceKind 是 Evidence 种类；缺失时为空。
	EvidenceKind *string `gorm:"column:evidence_kind"`
	// Availability 是 Evidence 可用性；缺失时为空。
	Availability *string `gorm:"column:availability"`
	// ContentDigest 是完整正文摘要；缺失时为空。
	ContentDigest *string `gorm:"column:content_digest"`
	// ExtractorSchemaVersion 是 Evidence 结构版本；缺失时为空。
	ExtractorSchemaVersion *string `gorm:"column:extractor_schema_version"`
	// ExtractorVersion 是 Evidence 实现版本；缺失时为空。
	ExtractorVersion *string `gorm:"column:extractor_version"`
	// LocatorKind 是 Evidence 定位器类型；缺失时为空。
	LocatorKind *string `gorm:"column:locator_kind"`
	// TextStart 是文本起点；缺失时为空。
	TextStart *int64 `gorm:"column:text_start"`
	// TextEnd 是文本终点；缺失时为空。
	TextEnd *int64 `gorm:"column:text_end"`
	// TextSourceLength 是完整正文字符数；缺失时为空。
	TextSourceLength *int64 `gorm:"column:text_source_length"`
	// Content 是完整正文；缺失时为空。
	Content *string `gorm:"column:content"`
	// CreatedAt 是 Asset 创建 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// EvidenceCreatedAt 是 Evidence 创建 UTC 时间；缺失时为空。
	EvidenceCreatedAt *time.Time `gorm:"column:evidence_created_at"`
	// EvidenceCount 是该 Asset 当前版本的全部 Evidence 数量，必须恰好为 1。
	EvidenceCount int64 `gorm:"column:evidence_count"`
}
