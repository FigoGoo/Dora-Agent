CREATE TABLE business.prompt_preview_draft (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    storyboard_preview_id uuid NOT NULL,
    storyboard_preview_version bigint NOT NULL,
    storyboard_preview_content_digest bytea NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    schema_version text NOT NULL,
    content_json jsonb NOT NULL,
    content_digest bytea NOT NULL,
    exact_target_set_digest bytea NOT NULL,
    source_tool_call_id uuid NOT NULL,
    source_prompt_version text NOT NULL,
    source_validator_version text NOT NULL,
    source_exact_set_validator_version text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT prompt_preview_draft_storyboard_version_check CHECK (storyboard_preview_version = 1),
    CONSTRAINT prompt_preview_draft_storyboard_digest_check CHECK (octet_length(storyboard_preview_content_digest) = 32),
    CONSTRAINT prompt_preview_draft_status_check CHECK (status = 'draft'),
    CONSTRAINT prompt_preview_draft_version_check CHECK (version = 1),
    CONSTRAINT prompt_preview_draft_schema_version_check CHECK (schema_version = 'prompt.preview.draft.v1'),
    CONSTRAINT prompt_preview_draft_content_object_check CHECK (jsonb_typeof(content_json) = 'object'),
    CONSTRAINT prompt_preview_draft_content_digest_check CHECK (octet_length(content_digest) = 32),
    CONSTRAINT prompt_preview_draft_exact_target_digest_check CHECK (octet_length(exact_target_set_digest) = 32),
    CONSTRAINT prompt_preview_draft_prompt_version_check CHECK (char_length(source_prompt_version) BETWEEN 1 AND 64),
    CONSTRAINT prompt_preview_draft_validator_version_check CHECK (char_length(source_validator_version) BETWEEN 1 AND 64),
    CONSTRAINT prompt_preview_draft_exact_validator_version_check CHECK (char_length(source_exact_set_validator_version) BETWEEN 1 AND 64),
    CONSTRAINT prompt_preview_draft_timestamp_order_check CHECK (updated_at = created_at)
);

COMMENT ON TABLE business.prompt_preview_draft IS 'Prompt Development Preview 不可变草稿权威表，仅保存从 Storyboard Preview 全部 Slot 确定性派生且经双 Validator 复核的 draft JSON';
COMMENT ON COLUMN business.prompt_preview_draft.id IS 'Prompt Preview 草稿根标识，由 Business 应用生成 UUIDv7；不是生产 PromptArtifact 或 PromptRevision 标识';
COMMENT ON COLUMN business.prompt_preview_draft.project_id IS '所属 Business Project 逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_draft.user_id IS '创建时冻结的项目所有者逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_draft.storyboard_preview_id IS '生成该草稿的 Storyboard Preview Draft 逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_draft.storyboard_preview_version IS '生成该草稿时冻结的 Storyboard Preview Draft 版本，当前固定为 1';
COMMENT ON COLUMN business.prompt_preview_draft.storyboard_preview_content_digest IS '生成该草稿时冻结的 Storyboard Preview Canonical Content SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_draft.status IS 'Prompt Preview 草稿状态，当前固定为 draft，不表示 reviewing、ready 或 active';
COMMENT ON COLUMN business.prompt_preview_draft.version IS 'Prompt Preview 草稿资源版本，当前不可变 Draft 固定为 1';
COMMENT ON COLUMN business.prompt_preview_draft.schema_version IS 'Prompt Preview 严格内容 JSON 的版本，当前固定为 prompt.preview.draft.v1';
COMMENT ON COLUMN business.prompt_preview_draft.content_json IS '严格校验后的 Prompt Preview 内容 JSON，覆盖 Source Storyboard Preview 的全部 Slot，不含 Provider、价格、Asset 或 Job';
COMMENT ON COLUMN business.prompt_preview_draft.content_digest IS '按冻结字段顺序并使用 HTML escape 规范编码 content_json 后计算的 SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_draft.exact_target_set_digest IS 'Agent 冻结并由保存请求摘要绑定的全部 Source Slot 目标集合 SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_draft.source_tool_call_id IS '产生该草稿的 Agent write_prompts Graph Tool 调用 UUIDv7';
COMMENT ON COLUMN business.prompt_preview_draft.source_prompt_version IS '产生该草稿时冻结的 Prompt 版本';
COMMENT ON COLUMN business.prompt_preview_draft.source_validator_version IS '产生该草稿时冻结的候选协议 Validator 版本';
COMMENT ON COLUMN business.prompt_preview_draft.source_exact_set_validator_version IS '产生该草稿时冻结的目标全集 Validator 版本';
COMMENT ON COLUMN business.prompt_preview_draft.created_at IS 'Prompt Preview 草稿创建时间，使用 UTC';
COMMENT ON COLUMN business.prompt_preview_draft.updated_at IS 'Prompt Preview 草稿更新时间；不可变 Draft 中与创建时间相同';

CREATE UNIQUE INDEX prompt_preview_draft_source_tool_call_uidx
    ON business.prompt_preview_draft (source_tool_call_id);
CREATE INDEX prompt_preview_draft_project_user_created_idx
    ON business.prompt_preview_draft (project_id, user_id, created_at DESC);
CREATE INDEX prompt_preview_draft_storyboard_created_idx
    ON business.prompt_preview_draft (storyboard_preview_id, created_at DESC);

CREATE TABLE business.prompt_preview_command_receipt (
    id uuid PRIMARY KEY,
    command_id uuid NOT NULL,
    request_digest bytea NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    expected_project_version bigint NOT NULL,
    storyboard_preview_id uuid NOT NULL,
    expected_storyboard_preview_version bigint NOT NULL,
    expected_storyboard_preview_content_digest bytea NOT NULL,
    source_tool_call_id uuid NOT NULL,
    source_prompt_version text NOT NULL,
    source_validator_version text NOT NULL,
    source_exact_set_validator_version text NOT NULL,
    exact_target_set_digest bytea NOT NULL,
    prompt_preview_id uuid NOT NULL,
    result_version bigint NOT NULL,
    result_status text NOT NULL,
    result_content_digest bytea NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT prompt_preview_command_receipt_command_unique UNIQUE (command_id),
    CONSTRAINT prompt_preview_command_receipt_request_digest_check CHECK (octet_length(request_digest) = 32),
    CONSTRAINT prompt_preview_command_receipt_project_version_check CHECK (expected_project_version >= 1),
    CONSTRAINT prompt_preview_command_receipt_storyboard_version_check CHECK (expected_storyboard_preview_version = 1),
    CONSTRAINT prompt_preview_command_receipt_storyboard_digest_check CHECK (octet_length(expected_storyboard_preview_content_digest) = 32),
    CONSTRAINT prompt_preview_command_receipt_exact_target_digest_check CHECK (octet_length(exact_target_set_digest) = 32),
    CONSTRAINT prompt_preview_command_receipt_result_version_check CHECK (result_version = 1),
    CONSTRAINT prompt_preview_command_receipt_result_status_check CHECK (result_status = 'draft'),
    CONSTRAINT prompt_preview_command_receipt_result_digest_check CHECK (octet_length(result_content_digest) = 32),
    CONSTRAINT prompt_preview_command_receipt_prompt_version_check CHECK (char_length(source_prompt_version) BETWEEN 1 AND 64),
    CONSTRAINT prompt_preview_command_receipt_validator_version_check CHECK (char_length(source_validator_version) BETWEEN 1 AND 64),
    CONSTRAINT prompt_preview_command_receipt_exact_validator_version_check CHECK (char_length(source_exact_set_validator_version) BETWEEN 1 AND 64)
);

COMMENT ON TABLE business.prompt_preview_command_receipt IS 'Prompt Preview 保存命令首次写入回执，用于同 command_id 的 first-write-wins 和未知结果查询收敛';
COMMENT ON COLUMN business.prompt_preview_command_receipt.id IS '命令回执标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.prompt_preview_command_receipt.command_id IS 'Agent 提供的 Prompt Preview 保存命令 UUIDv7，全局唯一';
COMMENT ON COLUMN business.prompt_preview_command_receipt.request_digest IS '冻结 save-draft 完整请求语义的 SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_command_receipt.user_id IS '命令中的可信用户逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_command_receipt.project_id IS '命令中的 Business Project 逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_command_receipt.expected_project_version IS '命令冻结的 Project 乐观并发版本';
COMMENT ON COLUMN business.prompt_preview_command_receipt.storyboard_preview_id IS '命令冻结的 Storyboard Preview Draft 逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_command_receipt.expected_storyboard_preview_version IS '命令冻结的 Storyboard Preview Draft 版本，当前固定为 1';
COMMENT ON COLUMN business.prompt_preview_command_receipt.expected_storyboard_preview_content_digest IS '命令冻结的 Storyboard Preview Canonical Content SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_command_receipt.source_tool_call_id IS '命令来源 Agent write_prompts Graph Tool Call UUIDv7';
COMMENT ON COLUMN business.prompt_preview_command_receipt.source_prompt_version IS '命令冻结的 Prompt 版本';
COMMENT ON COLUMN business.prompt_preview_command_receipt.source_validator_version IS '命令冻结的候选协议 Validator 版本';
COMMENT ON COLUMN business.prompt_preview_command_receipt.source_exact_set_validator_version IS '命令冻结的目标全集 Validator 版本';
COMMENT ON COLUMN business.prompt_preview_command_receipt.exact_target_set_digest IS '命令冻结的全部 Source Slot 目标集合 SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_command_receipt.prompt_preview_id IS '首次命令创建的 Prompt Preview Draft 根逻辑标识，不建立数据库物理外键';
COMMENT ON COLUMN business.prompt_preview_command_receipt.result_version IS '首次安全响应冻结的 Prompt Preview Draft 版本';
COMMENT ON COLUMN business.prompt_preview_command_receipt.result_status IS '首次安全响应冻结的 Prompt Preview Draft 状态';
COMMENT ON COLUMN business.prompt_preview_command_receipt.result_content_digest IS '首次安全响应冻结的 Prompt Preview Content SHA-256 摘要';
COMMENT ON COLUMN business.prompt_preview_command_receipt.created_at IS '首次命令提交时间，使用 UTC';

CREATE INDEX prompt_preview_command_receipt_project_user_created_idx
    ON business.prompt_preview_command_receipt (project_id, user_id, created_at DESC);
