CREATE TABLE business.media_preview_asset (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    asset_version bigint NOT NULL,
    status text NOT NULL,
    media_kind text NOT NULL,
    mime_type text NOT NULL,
    output_profile text NOT NULL,
    source_type text NOT NULL,
    source_id uuid NOT NULL,
    source_version bigint NOT NULL,
    source_digest bytea NOT NULL,
    target_local_key text,
    target_digest bytea,
    object_key text,
    content_digest bytea,
    size_bytes bigint,
    width integer,
    height integer,
    duration_ms bigint,
    codec text,
    pixel_format text,
    finalized_job_id uuid,
    finalized_attempt_id uuid,
    finalized_fence bigint,
    error_code text,
    created_at timestamptz NOT NULL,
    finalized_at timestamptz,
    CONSTRAINT media_preview_asset_version_check CHECK (asset_version = 1),
    CONSTRAINT media_preview_asset_status_check CHECK (status IN ('reserved', 'ready', 'failed')),
    CONSTRAINT media_preview_asset_profile_check CHECK (
        (media_kind = 'image' AND mime_type = 'image/png' AND output_profile = 'png_640x360.v1') OR
        (media_kind = 'video' AND mime_type = 'video/mp4' AND output_profile = 'mp4_h264_640x360_2s.v1')
    ),
    CONSTRAINT media_preview_asset_source_check CHECK (
        (source_type = 'prompt_preview' AND target_local_key IS NOT NULL
            AND char_length(target_local_key) BETWEEN 1 AND 128 AND target_digest IS NOT NULL
            AND octet_length(target_digest) = 32) OR
        (source_type = 'image_asset' AND target_local_key IS NULL AND target_digest IS NULL)
    ),
    CONSTRAINT media_preview_asset_source_version_check CHECK (source_version = 1),
    CONSTRAINT media_preview_asset_source_digest_check CHECK (octet_length(source_digest) = 32),
    CONSTRAINT media_preview_asset_lifecycle_check CHECK (
        (status = 'reserved' AND object_key IS NULL AND content_digest IS NULL AND size_bytes IS NULL
            AND width IS NULL AND height IS NULL AND duration_ms IS NULL AND codec IS NULL
            AND pixel_format IS NULL AND finalized_job_id IS NULL AND finalized_attempt_id IS NULL
            AND finalized_fence IS NULL AND error_code IS NULL AND finalized_at IS NULL) OR
        (status = 'ready' AND object_key IS NOT NULL AND char_length(object_key) BETWEEN 1 AND 1024
            AND content_digest IS NOT NULL AND octet_length(content_digest) = 32 AND size_bytes > 0
            AND width = 640 AND height = 360 AND finalized_job_id IS NOT NULL
            AND finalized_attempt_id IS NOT NULL AND finalized_fence > 0 AND error_code IS NULL
            AND finalized_at IS NOT NULL
            AND ((media_kind = 'image' AND duration_ms IS NULL AND codec IS NULL AND pixel_format IS NULL)
                OR (media_kind = 'video' AND duration_ms BETWEEN 1900 AND 2100
                    AND codec = 'h264' AND pixel_format = 'yuv420p'))) OR
        (status = 'failed' AND object_key IS NULL AND content_digest IS NULL AND size_bytes IS NULL
            AND width IS NULL AND height IS NULL AND duration_ms IS NULL AND codec IS NULL
            AND pixel_format IS NULL AND finalized_job_id IS NOT NULL AND finalized_attempt_id IS NOT NULL
            AND finalized_fence > 0 AND char_length(error_code) BETWEEN 1 AND 64 AND finalized_at IS NOT NULL)
    )
);

COMMENT ON TABLE business.media_preview_asset IS '本地媒体开发预览的 Business 权威 Asset 状态与受保护对象元数据';
COMMENT ON COLUMN business.media_preview_asset.id IS '媒体预览 Asset UUIDv7 标识，由 Business 应用生成';
COMMENT ON COLUMN business.media_preview_asset.owner_user_id IS '创建时冻结的项目所有者逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.media_preview_asset.project_id IS '所属 Business Project 逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.media_preview_asset.asset_version IS '媒体预览 Asset 不可变版本，当前固定为 1';
COMMENT ON COLUMN business.media_preview_asset.status IS 'Asset 状态，仅允许 reserved、ready 或 failed';
COMMENT ON COLUMN business.media_preview_asset.media_kind IS '冻结的媒体种类，仅允许 image 或 video';
COMMENT ON COLUMN business.media_preview_asset.mime_type IS '冻结的 MIME，仅允许 image/png 或 video/mp4';
COMMENT ON COLUMN business.media_preview_asset.output_profile IS '冻结的本地媒体输出参数组合版本';
COMMENT ON COLUMN business.media_preview_asset.source_type IS '权威来源类型，仅允许 prompt_preview 或 image_asset';
COMMENT ON COLUMN business.media_preview_asset.source_id IS 'Prompt Preview Draft 或来源 PNG Asset 的逻辑标识';
COMMENT ON COLUMN business.media_preview_asset.source_version IS '权威来源不可变版本，当前固定为 1';
COMMENT ON COLUMN business.media_preview_asset.source_digest IS '权威来源完整内容的 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_asset.target_local_key IS 'generate_media 选择的唯一图片 Prompt 目标局部键';
COMMENT ON COLUMN business.media_preview_asset.target_digest IS '所选 Prompt Entry 规范 JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_asset.object_key IS 'ready Asset 的 Business 生成相对对象键，不是 URL 或绝对路径';
COMMENT ON COLUMN business.media_preview_asset.content_digest IS 'ready 产物文件字节的 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_asset.size_bytes IS 'ready 产物文件的精确字节数';
COMMENT ON COLUMN business.media_preview_asset.width IS 'ready PNG 或 MP4 的冻结像素宽度';
COMMENT ON COLUMN business.media_preview_asset.height IS 'ready PNG 或 MP4 的冻结像素高度';
COMMENT ON COLUMN business.media_preview_asset.duration_ms IS 'ready MP4 的探针时长毫秒值，PNG 中为空';
COMMENT ON COLUMN business.media_preview_asset.codec IS 'ready MP4 的视频编码，当前固定 h264，PNG 中为空';
COMMENT ON COLUMN business.media_preview_asset.pixel_format IS 'ready MP4 的像素格式，当前固定 yuv420p，PNG 中为空';
COMMENT ON COLUMN business.media_preview_asset.finalized_job_id IS '首次终结该 Asset 的 Agent Job 逻辑标识';
COMMENT ON COLUMN business.media_preview_asset.finalized_attempt_id IS '首次终结该 Asset 的 Worker Attempt 逻辑标识';
COMMENT ON COLUMN business.media_preview_asset.finalized_fence IS '首次终结该 Asset 的正整数 Fencing Token';
COMMENT ON COLUMN business.media_preview_asset.error_code IS 'failed Asset 的白名单稳定错误码，ready 或 reserved 中为空';
COMMENT ON COLUMN business.media_preview_asset.created_at IS 'Asset 预留时间，使用 UTC';
COMMENT ON COLUMN business.media_preview_asset.finalized_at IS 'Asset 首次进入 ready 或 failed 的时间，使用 UTC';

CREATE INDEX media_preview_asset_owner_project_created_idx
    ON business.media_preview_asset (owner_user_id, project_id, created_at DESC, id DESC);
CREATE INDEX media_preview_asset_owner_project_ready_idx
    ON business.media_preview_asset (owner_user_id, project_id, status, media_kind, id);

CREATE TABLE business.media_preview_preparation_receipt (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    command_id uuid NOT NULL,
    request_digest bytea NOT NULL,
    operation_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    tool_key text NOT NULL,
    scope_digest bytea NOT NULL,
    output_profile text NOT NULL,
    source_type text NOT NULL,
    source_id uuid NOT NULL,
    source_version bigint NOT NULL,
    source_digest bytea NOT NULL,
    target_local_key text,
    target_digest bytea,
    source_object_key text,
    asset_id uuid NOT NULL,
    asset_version bigint NOT NULL,
    asset_status text NOT NULL,
    media_kind text NOT NULL,
    mime_type text NOT NULL,
    staging_object_key text NOT NULL,
    final_object_key text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT media_preview_preparation_command_unique UNIQUE (command_id),
    CONSTRAINT media_preview_preparation_operation_unique UNIQUE (operation_id),
    CONSTRAINT media_preview_preparation_asset_unique UNIQUE (asset_id),
    CONSTRAINT media_preview_preparation_request_digest_check CHECK (octet_length(request_digest) = 32),
    CONSTRAINT media_preview_preparation_scope_digest_check CHECK (octet_length(scope_digest) = 32),
    CONSTRAINT media_preview_preparation_tool_profile_check CHECK (
        (tool_key = 'generate_media' AND output_profile = 'png_640x360.v1'
            AND media_kind = 'image' AND mime_type = 'image/png') OR
        (tool_key = 'assemble_output' AND output_profile = 'mp4_h264_640x360_2s.v1'
            AND media_kind = 'video' AND mime_type = 'video/mp4')
    ),
    CONSTRAINT media_preview_preparation_source_check CHECK (
        (source_type = 'prompt_preview' AND target_local_key IS NOT NULL
            AND char_length(target_local_key) BETWEEN 1 AND 128 AND target_digest IS NOT NULL
            AND octet_length(target_digest) = 32 AND source_object_key IS NULL) OR
        (source_type = 'image_asset' AND target_local_key IS NULL AND target_digest IS NULL
            AND source_object_key IS NOT NULL AND char_length(source_object_key) BETWEEN 1 AND 1024)
    ),
    CONSTRAINT media_preview_preparation_source_version_check CHECK (source_version = 1),
    CONSTRAINT media_preview_preparation_source_digest_check CHECK (octet_length(source_digest) = 32),
    CONSTRAINT media_preview_preparation_asset_version_check CHECK (asset_version = 1),
    CONSTRAINT media_preview_preparation_asset_status_check CHECK (asset_status = 'reserved'),
    CONSTRAINT media_preview_preparation_key_check CHECK (
        char_length(staging_object_key) BETWEEN 1 AND 1024
        AND char_length(final_object_key) BETWEEN 1 AND 1024
        AND staging_object_key <> final_object_key
    )
);

COMMENT ON TABLE business.media_preview_preparation_receipt IS '媒体预览 Prepare 命令首次写入回执，用于响应丢失查询和 first-write-wins';
COMMENT ON COLUMN business.media_preview_preparation_receipt.id IS 'Preparation 回执 UUIDv7 标识，由 Business 应用生成';
COMMENT ON COLUMN business.media_preview_preparation_receipt.request_id IS '首次 Prepare HTTP 调用的追踪 UUIDv7，不参与幂等判断';
COMMENT ON COLUMN business.media_preview_preparation_receipt.command_id IS 'Prepare first-write-wins 命令 UUIDv7，全局唯一';
COMMENT ON COLUMN business.media_preview_preparation_receipt.request_digest IS '调用方冻结的 Prepare 完整请求语义 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_preparation_receipt.operation_id IS 'Agent 媒体 Operation 逻辑标识，一个 Operation 只允许一个 Preparation';
COMMENT ON COLUMN business.media_preview_preparation_receipt.owner_user_id IS 'Prepare 中的可信用户逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.media_preview_preparation_receipt.project_id IS 'Prepare 中的 Business Project 逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.media_preview_preparation_receipt.tool_key IS '冻结的媒体 Graph Tool，仅允许 generate_media 或 assemble_output';
COMMENT ON COLUMN business.media_preview_preparation_receipt.scope_digest IS 'Agent 冻结 Tool Scope 的 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_preparation_receipt.output_profile IS '与 Tool 配套的固定本地输出 Profile';
COMMENT ON COLUMN business.media_preview_preparation_receipt.source_type IS 'Business 复核后的权威来源类型';
COMMENT ON COLUMN business.media_preview_preparation_receipt.source_id IS 'Business 复核后的 Prompt Draft 或 PNG Asset 标识';
COMMENT ON COLUMN business.media_preview_preparation_receipt.source_version IS 'Business 复核后的权威来源版本，当前固定为 1';
COMMENT ON COLUMN business.media_preview_preparation_receipt.source_digest IS 'Business 复核后的权威来源 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_preparation_receipt.target_local_key IS 'generate_media 选择的唯一图片 Prompt 目标键';
COMMENT ON COLUMN business.media_preview_preparation_receipt.target_digest IS 'Business 从 Prompt Entry 规范 JSON 计算的 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_preparation_receipt.source_object_key IS 'assemble_output 使用的 ready PNG 相对对象键，不是路径根或 URL';
COMMENT ON COLUMN business.media_preview_preparation_receipt.asset_id IS '本次 Prepare 创建的输出 Asset 逻辑标识';
COMMENT ON COLUMN business.media_preview_preparation_receipt.asset_version IS '输出 Asset 版本，当前固定为 1';
COMMENT ON COLUMN business.media_preview_preparation_receipt.asset_status IS 'Prepare 响应冻结状态，当前固定为 reserved';
COMMENT ON COLUMN business.media_preview_preparation_receipt.media_kind IS '输出媒体种类，与 Tool 和 Profile 联合冻结';
COMMENT ON COLUMN business.media_preview_preparation_receipt.mime_type IS '输出 MIME，与 Tool 和 Profile 联合冻结';
COMMENT ON COLUMN business.media_preview_preparation_receipt.staging_object_key IS 'Business 生成、Worker 可写的 staging 相对对象键';
COMMENT ON COLUMN business.media_preview_preparation_receipt.final_object_key IS 'Business 内部保留的 objects 相对目标键，不进入 Prepare HTTP 响应';
COMMENT ON COLUMN business.media_preview_preparation_receipt.created_at IS '首次 Prepare 命令提交时间，使用 UTC';

CREATE INDEX media_preview_preparation_owner_project_created_idx
    ON business.media_preview_preparation_receipt (owner_user_id, project_id, created_at DESC);

CREATE TABLE business.media_preview_finalization_receipt (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL,
    command_id uuid NOT NULL,
    request_digest bytea NOT NULL,
    preparation_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    batch_id uuid NOT NULL,
    job_id uuid NOT NULL,
    attempt_id uuid NOT NULL,
    fence bigint NOT NULL,
    terminal_status text NOT NULL,
    asset_id uuid NOT NULL,
    asset_version bigint NOT NULL,
    asset_status text NOT NULL,
    media_kind text NOT NULL,
    mime_type text NOT NULL,
    content_digest bytea,
    size_bytes bigint,
    width integer,
    height integer,
    duration_ms bigint,
    codec text,
    pixel_format text,
    error_code text,
    completed_at timestamptz NOT NULL,
    CONSTRAINT media_preview_finalization_command_unique UNIQUE (command_id),
    CONSTRAINT media_preview_finalization_preparation_unique UNIQUE (preparation_id),
    CONSTRAINT media_preview_finalization_job_unique UNIQUE (job_id),
    CONSTRAINT media_preview_finalization_request_digest_check CHECK (octet_length(request_digest) = 32),
    CONSTRAINT media_preview_finalization_fence_check CHECK (fence > 0),
    CONSTRAINT media_preview_finalization_terminal_check CHECK (terminal_status IN ('ready', 'failed')),
    CONSTRAINT media_preview_finalization_asset_version_check CHECK (asset_version = 1),
    CONSTRAINT media_preview_finalization_profile_check CHECK (
        (media_kind = 'image' AND mime_type = 'image/png') OR
        (media_kind = 'video' AND mime_type = 'video/mp4')
    ),
    CONSTRAINT media_preview_finalization_result_check CHECK (
        (terminal_status = 'ready' AND asset_status = 'ready'
            AND content_digest IS NOT NULL AND octet_length(content_digest) = 32
            AND size_bytes > 0 AND width = 640 AND height = 360 AND error_code IS NULL
            AND ((media_kind = 'image' AND duration_ms IS NULL AND codec IS NULL AND pixel_format IS NULL)
                OR (media_kind = 'video' AND duration_ms BETWEEN 1900 AND 2100
                    AND codec = 'h264' AND pixel_format = 'yuv420p'))) OR
        (terminal_status = 'failed' AND asset_status = 'failed' AND content_digest IS NULL
            AND size_bytes IS NULL AND width IS NULL AND height IS NULL AND duration_ms IS NULL
            AND codec IS NULL AND pixel_format IS NULL AND char_length(error_code) BETWEEN 1 AND 64)
    )
);

COMMENT ON TABLE business.media_preview_finalization_receipt IS '媒体预览 Finalize 命令首次终态回执，冻结 Job、Fence 与安全结果';
COMMENT ON COLUMN business.media_preview_finalization_receipt.id IS 'Finalization 回执 UUIDv7 标识，由 Business 应用生成';
COMMENT ON COLUMN business.media_preview_finalization_receipt.request_id IS '首次 Finalize HTTP 调用的追踪 UUIDv7，不参与幂等判断';
COMMENT ON COLUMN business.media_preview_finalization_receipt.command_id IS 'Finalize first-write-wins 命令 UUIDv7，全局唯一';
COMMENT ON COLUMN business.media_preview_finalization_receipt.request_digest IS '调用方冻结的 Finalize 完整请求语义 SHA-256 摘要';
COMMENT ON COLUMN business.media_preview_finalization_receipt.preparation_id IS '原 Prepare 回执逻辑标识，一个 Preparation 只能有一个终态';
COMMENT ON COLUMN business.media_preview_finalization_receipt.operation_id IS '原 Agent 媒体 Operation 逻辑标识';
COMMENT ON COLUMN business.media_preview_finalization_receipt.batch_id IS 'Agent 单 Job Batch 逻辑标识';
COMMENT ON COLUMN business.media_preview_finalization_receipt.job_id IS 'Agent 媒体 Job 逻辑标识，一个 Job 只能提交一个终态';
COMMENT ON COLUMN business.media_preview_finalization_receipt.attempt_id IS '当前 Worker Claim Attempt 逻辑标识';
COMMENT ON COLUMN business.media_preview_finalization_receipt.fence IS '当前 Worker 租约的正整数 Fencing Token';
COMMENT ON COLUMN business.media_preview_finalization_receipt.terminal_status IS 'Business Finalize 终态，仅允许 ready 或 failed';
COMMENT ON COLUMN business.media_preview_finalization_receipt.asset_id IS '被终结的 Business 媒体预览 Asset 标识';
COMMENT ON COLUMN business.media_preview_finalization_receipt.asset_version IS '被终结 Asset 的版本，当前固定为 1';
COMMENT ON COLUMN business.media_preview_finalization_receipt.asset_status IS '终态 Asset 状态，与 terminal_status 精确一致';
COMMENT ON COLUMN business.media_preview_finalization_receipt.media_kind IS '终态 Asset 媒体种类';
COMMENT ON COLUMN business.media_preview_finalization_receipt.mime_type IS '终态 Asset MIME 类型';
COMMENT ON COLUMN business.media_preview_finalization_receipt.content_digest IS 'ready 产物文件字节的 SHA-256 摘要，failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.size_bytes IS 'ready 产物文件精确字节数，failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.width IS 'ready 产物像素宽度，failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.height IS 'ready 产物像素高度，failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.duration_ms IS 'ready MP4 时长毫秒值，PNG 或 failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.codec IS 'ready MP4 视频编码，PNG 或 failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.pixel_format IS 'ready MP4 像素格式，PNG 或 failed 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.error_code IS 'failed 终态的白名单稳定错误码，ready 中为空';
COMMENT ON COLUMN business.media_preview_finalization_receipt.completed_at IS '首次 Finalize 命令完成时间，使用 UTC';

CREATE INDEX media_preview_finalization_asset_completed_idx
    ON business.media_preview_finalization_receipt (asset_id, completed_at DESC);
