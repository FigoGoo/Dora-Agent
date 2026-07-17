CREATE TABLE business.asset_analysis_preview_assets (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    asset_version bigint NOT NULL CHECK (asset_version >= 1),
    media_type varchar(16) NOT NULL CHECK (media_type IN ('text', 'image')),
    status varchar(16) NOT NULL CHECK (status = 'ready'),
    created_at timestamptz NOT NULL
);

COMMENT ON TABLE business.asset_analysis_preview_assets IS '素材分析输入开发预览的素材权威快照';
COMMENT ON COLUMN business.asset_analysis_preview_assets.id IS '素材 UUIDv7 主键';
COMMENT ON COLUMN business.asset_analysis_preview_assets.owner_user_id IS '素材所属用户 UUIDv7';
COMMENT ON COLUMN business.asset_analysis_preview_assets.project_id IS '素材所属项目 UUIDv7';
COMMENT ON COLUMN business.asset_analysis_preview_assets.asset_version IS '素材不可变内容版本';
COMMENT ON COLUMN business.asset_analysis_preview_assets.media_type IS '素材媒体类型，仅允许文本或图片';
COMMENT ON COLUMN business.asset_analysis_preview_assets.status IS '素材可用状态，预览仅允许就绪';
COMMENT ON COLUMN business.asset_analysis_preview_assets.created_at IS '素材快照创建时间';

CREATE INDEX idx_asset_analysis_preview_assets_owner_project
    ON business.asset_analysis_preview_assets (owner_user_id, project_id, status, id);

CREATE TABLE business.asset_analysis_preview_evidence (
    id uuid PRIMARY KEY,
    asset_id uuid NOT NULL,
    asset_version bigint NOT NULL CHECK (asset_version >= 1),
    media_type varchar(16) NOT NULL CHECK (media_type IN ('text', 'image')),
    evidence_kind varchar(32) NOT NULL CHECK (evidence_kind IN ('text_segment', 'visual_description', 'safety_label')),
    availability varchar(16) NOT NULL CHECK (availability IN ('ready', 'missing', 'failed', 'redacted', 'unsupported')),
    reason_code varchar(128),
    content_digest varchar(64),
    extractor_schema_version varchar(128),
    extractor_version varchar(128),
    locator_kind varchar(32),
    text_start bigint,
    text_end bigint,
    text_source_length bigint,
    image_x integer,
    image_y integer,
    image_width integer,
    image_height integer,
    content text,
    created_at timestamptz NOT NULL,
    CONSTRAINT ck_asset_analysis_preview_evidence_media_kind CHECK (
        (media_type = 'text' AND evidence_kind = 'text_segment') OR
        (media_type = 'image' AND evidence_kind IN ('visual_description', 'safety_label'))
    ),
    CONSTRAINT ck_asset_analysis_preview_evidence_payload CHECK (
        (
            availability = 'ready'
            AND reason_code IS NULL
            AND content_digest ~ '^[0-9a-f]{64}$'
            AND length(extractor_schema_version) BETWEEN 1 AND 128
            AND length(extractor_version) BETWEEN 1 AND 128
            AND length(content) BETWEEN 1 AND 2000
            AND locator_kind IN ('text_range', 'image_whole', 'image_region')
        ) OR (
            availability <> 'ready'
            AND length(reason_code) BETWEEN 1 AND 128
            AND content_digest IS NULL
            AND extractor_schema_version IS NULL
            AND extractor_version IS NULL
            AND locator_kind IS NULL
            AND text_start IS NULL
            AND text_end IS NULL
            AND text_source_length IS NULL
            AND image_x IS NULL
            AND image_y IS NULL
            AND image_width IS NULL
            AND image_height IS NULL
            AND content IS NULL
        )
    ),
    CONSTRAINT ck_asset_analysis_preview_evidence_locator CHECK (
        availability <> 'ready' OR
        (
            locator_kind = 'text_range'
            AND media_type = 'text'
            AND text_start IS NOT NULL AND text_end IS NOT NULL AND text_source_length IS NOT NULL
            AND text_start >= 0 AND text_start < text_end AND text_end <= text_source_length
            AND image_x IS NULL AND image_y IS NULL AND image_width IS NULL AND image_height IS NULL
        ) OR (
            locator_kind = 'image_whole'
            AND media_type = 'image'
            AND text_start IS NULL AND text_end IS NULL AND text_source_length IS NULL
            AND image_x IS NULL AND image_y IS NULL AND image_width IS NULL AND image_height IS NULL
        ) OR (
            locator_kind = 'image_region'
            AND media_type = 'image'
            AND text_start IS NULL AND text_end IS NULL AND text_source_length IS NULL
            AND image_x BETWEEN 0 AND 10000 AND image_y BETWEEN 0 AND 10000
            AND image_width BETWEEN 1 AND 10000 AND image_height BETWEEN 1 AND 10000
            AND image_x + image_width <= 10000 AND image_y + image_height <= 10000
        )
    )
);

COMMENT ON TABLE business.asset_analysis_preview_evidence IS '素材分析输入开发预览的不可变证据事实';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.id IS '证据 UUIDv7 主键';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.asset_id IS '证据所属素材 UUIDv7';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.asset_version IS '证据对应的素材版本';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.media_type IS '证据对应的素材媒体类型';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.evidence_kind IS '证据种类稳定代码';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.availability IS '证据可用性稳定代码';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.reason_code IS '不可用证据的稳定原因代码';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.content_digest IS '就绪证据原始内容 UTF-8 字节的小写 SHA256';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.extractor_schema_version IS '提取器输出结构版本';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.extractor_version IS '提取器实现版本';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.locator_kind IS '证据定位器类型';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.text_start IS '文本区间起始字符偏移';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.text_end IS '文本区间结束字符偏移';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.text_source_length IS '文本来源总字符长度';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.image_x IS '图片区域左边界基点';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.image_y IS '图片区域上边界基点';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.image_width IS '图片区域宽度基点';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.image_height IS '图片区域高度基点';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.content IS '就绪证据的最小规范化内容';
COMMENT ON COLUMN business.asset_analysis_preview_evidence.created_at IS '证据事实创建时间';

CREATE INDEX idx_asset_analysis_preview_evidence_asset_version
    ON business.asset_analysis_preview_evidence (asset_id, asset_version, evidence_kind, id);

CREATE FUNCTION business.reject_asset_analysis_preview_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION 'asset analysis preview evidence is immutable';
END;
$function$;

COMMENT ON FUNCTION business.reject_asset_analysis_preview_evidence_mutation() IS '拒绝修改或删除不可变素材分析预览证据';

CREATE TRIGGER trg_asset_analysis_preview_evidence_immutable
BEFORE UPDATE OR DELETE ON business.asset_analysis_preview_evidence
FOR EACH ROW EXECUTE FUNCTION business.reject_asset_analysis_preview_evidence_mutation();
