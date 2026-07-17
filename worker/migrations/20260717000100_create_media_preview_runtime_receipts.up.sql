CREATE TABLE worker.media_preview_attempts (
    attempt_id uuid PRIMARY KEY,
    claim_request_id uuid NOT NULL UNIQUE,
    job_id uuid NOT NULL,
    worker_id text NOT NULL,
    fence bigint,
    job_type text,
    artifact_request_digest char(64),
    status text NOT NULL,
    finalize_command_id uuid,
    finalize_request_digest char(64),
    finalize_error_code text,
    terminal_event_id uuid,
    terminal_status text,
    terminal_result_digest char(64),
    error_code text,
    started_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    finished_at timestamptz,
    CONSTRAINT media_preview_attempt_fence_positive CHECK (fence IS NULL OR fence > 0),
    CONSTRAINT media_preview_attempt_status_check CHECK (status IN (
        'claim_pending', 'claim_unknown', 'running', 'artifact_ready', 'finalize_unknown',
        'reconciling', 'terminal_unknown', 'retry_scheduled', 'completed', 'failed',
        'lease_lost', 'claim_rejected'
    )),
    CONSTRAINT media_preview_attempt_terminal_status_check CHECK (
        terminal_status IS NULL OR terminal_status IN ('succeeded', 'failed')
    )
);

CREATE INDEX media_preview_attempt_recovery_idx
    ON worker.media_preview_attempts (status, updated_at, attempt_id);

CREATE INDEX media_preview_attempt_job_idx
    ON worker.media_preview_attempts (job_id, started_at DESC, attempt_id DESC);

COMMENT ON TABLE worker.media_preview_attempts IS '媒体开发预览任务的 Worker 尝试、稳定命令标识与恢复状态，不保存任务载荷或文件路径';
COMMENT ON COLUMN worker.media_preview_attempts.attempt_id IS 'Agent Claim 使用的 UUIDv7 Attempt 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_attempts.claim_request_id IS 'Agent Claim first-write-wins 的 UUIDv7 请求标识，用于响应未知后的原键重放';
COMMENT ON COLUMN worker.media_preview_attempts.job_id IS 'Agent-owned Media Job UUIDv7 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_attempts.worker_id IS '持有本次尝试的 Worker 实例标识';
COMMENT ON COLUMN worker.media_preview_attempts.fence IS 'Agent Claim 返回的正整数 Fencing Token，领取前为空';
COMMENT ON COLUMN worker.media_preview_attempts.job_type IS '冻结任务类型 generate_png 或 assemble_mp4，领取前为空';
COMMENT ON COLUMN worker.media_preview_attempts.artifact_request_digest IS '产物请求的无前缀 lowercase SHA-256，领取前为空';
COMMENT ON COLUMN worker.media_preview_attempts.status IS 'Worker 恢复状态：claim_pending、claim_unknown、running、artifact_ready、finalize_unknown、reconciling、terminal_unknown、retry_scheduled、completed、failed、lease_lost、claim_rejected';
COMMENT ON COLUMN worker.media_preview_attempts.finalize_command_id IS 'Business Finalize first-write-wins UUIDv7 命令标识，产物成功前为空';
COMMENT ON COLUMN worker.media_preview_attempts.finalize_request_digest IS 'Business Finalize 规范请求的 lowercase SHA-256，产物成功前为空';
COMMENT ON COLUMN worker.media_preview_attempts.finalize_error_code IS 'failed Finalize first-write-wins 的稳定错误码，ready Finalize 为空';
COMMENT ON COLUMN worker.media_preview_attempts.terminal_event_id IS 'Agent Terminal Outbox AppendOnce 使用的 UUIDv7 事件标识，提交前为空';
COMMENT ON COLUMN worker.media_preview_attempts.terminal_status IS 'Agent 终态 succeeded 或 failed，提交前为空';
COMMENT ON COLUMN worker.media_preview_attempts.terminal_result_digest IS 'Agent Terminal Result 规范 JSON 的 lowercase SHA-256，提交前为空';
COMMENT ON COLUMN worker.media_preview_attempts.error_code IS '白名单稳定错误码，不包含 stderr、路径或堆栈';
COMMENT ON COLUMN worker.media_preview_attempts.started_at IS 'Worker 创建 Claim Intent 的 UTC 时间';
COMMENT ON COLUMN worker.media_preview_attempts.updated_at IS 'Worker 最近一次状态投影更新时间';
COMMENT ON COLUMN worker.media_preview_attempts.finished_at IS 'Worker 确认终态、重试或租约丢失的 UTC 时间，未结束时为空';

CREATE TABLE worker.media_preview_artifact_receipts (
    receipt_id uuid PRIMARY KEY,
    attempt_id uuid NOT NULL UNIQUE,
    job_id uuid NOT NULL,
    fence bigint NOT NULL,
    schema_version text NOT NULL,
    job_type text NOT NULL,
    generator_version text,
    artifact_request_digest char(64) NOT NULL,
    content_digest char(64) NOT NULL,
    size_bytes bigint NOT NULL,
    mime_type text NOT NULL,
    width integer NOT NULL,
    height integer NOT NULL,
    duration_ms bigint,
    codec text,
    pixel_format text,
    created_at timestamptz NOT NULL,
    CONSTRAINT media_preview_artifact_fence_positive CHECK (fence > 0),
    CONSTRAINT media_preview_artifact_size_positive CHECK (size_bytes > 0),
    CONSTRAINT media_preview_artifact_dimensions_positive CHECK (width > 0 AND height > 0),
    CONSTRAINT media_preview_artifact_identity_unique UNIQUE (job_id, fence, artifact_request_digest)
);

COMMENT ON TABLE worker.media_preview_artifact_receipts IS '媒体开发预览产物的不可变摘要回执，不保存相对对象键、对象根或绝对路径';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.receipt_id IS 'Worker 应用生成的 UUIDv7 产物回执标识';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.attempt_id IS 'Worker Attempt UUIDv7 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.job_id IS 'Agent-owned Media Job UUIDv7 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.fence IS '生成产物时生效的 Agent Fencing Token';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.schema_version IS '媒体产物回执 Schema Version';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.job_type IS '产物任务类型 generate_png 或 assemble_mp4';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.generator_version IS '确定性 PNG 算法版本，MP4 为空';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.artifact_request_digest IS '产物请求的无前缀 lowercase SHA-256';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.content_digest IS '产物字节的无前缀 lowercase SHA-256';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.size_bytes IS '产物精确字节数';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.mime_type IS '验证后的 image/png 或 video/mp4';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.width IS '验证后的像素宽度';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.height IS '验证后的像素高度';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.duration_ms IS 'MP4 探针时长毫秒数，PNG 为空';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.codec IS 'MP4 探针 codec，PNG 为空';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.pixel_format IS 'MP4 探针 pixel format，PNG 为空';
COMMENT ON COLUMN worker.media_preview_artifact_receipts.created_at IS 'Worker 完成产物验证并冻结回执的 UTC 时间';

CREATE TABLE worker.media_preview_finalization_observations (
    observation_id uuid PRIMARY KEY,
    attempt_id uuid NOT NULL,
    job_id uuid NOT NULL,
    fence bigint NOT NULL,
    command_id uuid NOT NULL,
    request_digest char(64) NOT NULL,
    preparation_id uuid NOT NULL,
    query_status text NOT NULL,
    finalization_receipt_id uuid,
    asset_id uuid,
    asset_version bigint,
    asset_status text,
    media_kind text,
    content_digest char(64),
    size_bytes bigint,
    mime_type text,
    error_code text,
    observed_at timestamptz NOT NULL,
    CONSTRAINT media_preview_finalization_fence_positive CHECK (fence > 0),
    CONSTRAINT media_preview_finalization_query_status_check CHECK (query_status IN ('not_found', 'completed', 'conflict')),
    CONSTRAINT media_preview_finalization_identity_unique UNIQUE (command_id, request_digest)
);

CREATE INDEX media_preview_finalization_attempt_idx
    ON worker.media_preview_finalization_observations (attempt_id, observed_at DESC);

COMMENT ON TABLE worker.media_preview_finalization_observations IS 'Business Finalize 或 Query-Finalization 的权威结果摘要，不保存响应正文或文件路径';
COMMENT ON COLUMN worker.media_preview_finalization_observations.observation_id IS 'Worker 应用生成的 UUIDv7 查询观察标识';
COMMENT ON COLUMN worker.media_preview_finalization_observations.attempt_id IS 'Worker Attempt UUIDv7 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_finalization_observations.job_id IS 'Agent-owned Media Job UUIDv7 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_finalization_observations.fence IS 'Finalize 使用的当前 Agent Fencing Token';
COMMENT ON COLUMN worker.media_preview_finalization_observations.command_id IS 'Business Finalize first-write-wins UUIDv7 命令标识';
COMMENT ON COLUMN worker.media_preview_finalization_observations.request_digest IS 'Finalize 规范请求的无前缀 lowercase SHA-256';
COMMENT ON COLUMN worker.media_preview_finalization_observations.preparation_id IS 'Business Prepare 回执 UUIDv7 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN worker.media_preview_finalization_observations.query_status IS '权威查询状态 not_found、completed 或 conflict';
COMMENT ON COLUMN worker.media_preview_finalization_observations.finalization_receipt_id IS 'Business Finalization Receipt UUIDv7 逻辑标识，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.asset_id IS 'Business Media Asset UUIDv7 逻辑标识，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.asset_version IS 'Business Media Asset 正整数版本，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.asset_status IS 'Business Media Asset 权威状态 ready 或 failed，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.media_kind IS 'Business Media Asset 类型 image 或 video，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.content_digest IS 'Business 权威产物 lowercase SHA-256，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.size_bytes IS 'Business 权威产物字节数，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.mime_type IS 'Business 权威产物 MIME，未完成时为空';
COMMENT ON COLUMN worker.media_preview_finalization_observations.error_code IS '冲突或失败的白名单稳定错误码，不含内部诊断';
COMMENT ON COLUMN worker.media_preview_finalization_observations.observed_at IS 'Worker 获得权威 Finalize 或查询结果的 UTC 时间';
