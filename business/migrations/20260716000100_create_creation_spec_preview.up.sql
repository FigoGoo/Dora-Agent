CREATE TABLE business.creation_spec (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    schema_version text NOT NULL,
    content_json jsonb NOT NULL,
    content_digest bytea NOT NULL,
    source_tool_call_id uuid NOT NULL,
    source_prompt_version text NOT NULL,
    source_validator_version text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT creation_spec_status_check CHECK (status = 'draft'),
    CONSTRAINT creation_spec_version_check CHECK (version = 1),
    CONSTRAINT creation_spec_schema_version_check CHECK (schema_version = 'creation_spec.draft.v1'),
    CONSTRAINT creation_spec_content_object_check CHECK (jsonb_typeof(content_json) = 'object'),
    CONSTRAINT creation_spec_content_digest_check CHECK (octet_length(content_digest) = 32),
    CONSTRAINT creation_spec_source_prompt_version_check CHECK (char_length(source_prompt_version) BETWEEN 1 AND 64),
    CONSTRAINT creation_spec_source_validator_version_check CHECK (char_length(source_validator_version) BETWEEN 1 AND 64),
    CONSTRAINT creation_spec_timestamp_order_check CHECK (updated_at >= created_at)
);

COMMENT ON TABLE business.creation_spec IS 'CreationSpec 草稿权威表，仅保存 Preview 阶段严格校验后的 draft';
COMMENT ON COLUMN business.creation_spec.id IS 'CreationSpec 应用侧生成的 UUIDv7 主键';
COMMENT ON COLUMN business.creation_spec.project_id IS '所属 Business Project 逻辑标识，不建立物理外键';
COMMENT ON COLUMN business.creation_spec.user_id IS '创建时冻结的项目所有者逻辑标识，不建立物理外键';
COMMENT ON COLUMN business.creation_spec.status IS 'CreationSpec 状态，V1 Preview 固定为 draft';
COMMENT ON COLUMN business.creation_spec.version IS 'CreationSpec 乐观并发版本，首次草稿固定为 1';
COMMENT ON COLUMN business.creation_spec.schema_version IS 'CreationSpec 草稿内容契约版本';
COMMENT ON COLUMN business.creation_spec.content_json IS '严格校验后的 CreationSpec 内容 JSON，不含提示词、推理或供应商原文';
COMMENT ON COLUMN business.creation_spec.content_digest IS '按冻结字段顺序规范编码 content_json 后计算的 SHA-256 摘要';
COMMENT ON COLUMN business.creation_spec.source_tool_call_id IS '产生该草稿的 Agent Graph Tool 调用 UUIDv7';
COMMENT ON COLUMN business.creation_spec.source_prompt_version IS '产生该草稿时冻结的 Prompt 版本';
COMMENT ON COLUMN business.creation_spec.source_validator_version IS '产生该草稿时冻结的 Validator 版本';
COMMENT ON COLUMN business.creation_spec.created_at IS 'CreationSpec 草稿创建时间';
COMMENT ON COLUMN business.creation_spec.updated_at IS 'CreationSpec 草稿最近更新时间';

CREATE UNIQUE INDEX creation_spec_source_tool_call_uidx
    ON business.creation_spec (source_tool_call_id);
CREATE INDEX creation_spec_project_user_created_idx
    ON business.creation_spec (project_id, user_id, created_at DESC);

CREATE TABLE business.creation_spec_command_receipt (
    id uuid PRIMARY KEY,
    command_id uuid NOT NULL,
    request_digest bytea NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    expected_project_version bigint NOT NULL,
    source_tool_call_id uuid NOT NULL,
    source_prompt_version text NOT NULL,
    source_validator_version text NOT NULL,
    creation_spec_id uuid NOT NULL,
    result_version bigint NOT NULL,
    result_status text NOT NULL,
    result_content_digest bytea NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT creation_spec_command_receipt_command_unique UNIQUE (command_id),
    CONSTRAINT creation_spec_command_receipt_request_digest_check CHECK (octet_length(request_digest) = 32),
    CONSTRAINT creation_spec_command_receipt_project_version_check CHECK (expected_project_version >= 1),
    CONSTRAINT creation_spec_command_receipt_result_version_check CHECK (result_version = 1),
    CONSTRAINT creation_spec_command_receipt_result_status_check CHECK (result_status = 'draft'),
    CONSTRAINT creation_spec_command_receipt_result_digest_check CHECK (octet_length(result_content_digest) = 32),
    CONSTRAINT creation_spec_command_receipt_prompt_version_check CHECK (char_length(source_prompt_version) BETWEEN 1 AND 64),
    CONSTRAINT creation_spec_command_receipt_validator_version_check CHECK (char_length(source_validator_version) BETWEEN 1 AND 64)
);

COMMENT ON TABLE business.creation_spec_command_receipt IS 'CreationSpec 保存命令首次写入回执，用于同 command_id 的 first-write-wins 收敛';
COMMENT ON COLUMN business.creation_spec_command_receipt.id IS '命令回执应用侧生成的 UUIDv7 主键';
COMMENT ON COLUMN business.creation_spec_command_receipt.command_id IS 'Agent 提供的 CreationSpec 保存命令 UUIDv7，全局唯一';
COMMENT ON COLUMN business.creation_spec_command_receipt.request_digest IS '冻结 save-draft 请求语义的 SHA-256 摘要';
COMMENT ON COLUMN business.creation_spec_command_receipt.user_id IS '命令中的可信用户逻辑标识，不建立物理外键';
COMMENT ON COLUMN business.creation_spec_command_receipt.project_id IS '命令中的 Business Project 逻辑标识，不建立物理外键';
COMMENT ON COLUMN business.creation_spec_command_receipt.expected_project_version IS '命令冻结的 Project 乐观并发版本';
COMMENT ON COLUMN business.creation_spec_command_receipt.source_tool_call_id IS '命令来源 Agent Graph Tool 调用 UUIDv7';
COMMENT ON COLUMN business.creation_spec_command_receipt.source_prompt_version IS '命令冻结的 Prompt 版本';
COMMENT ON COLUMN business.creation_spec_command_receipt.source_validator_version IS '命令冻结的 Validator 版本';
COMMENT ON COLUMN business.creation_spec_command_receipt.creation_spec_id IS '首次命令创建的 CreationSpec 逻辑标识，不建立物理外键';
COMMENT ON COLUMN business.creation_spec_command_receipt.result_version IS '首次安全响应冻结的 CreationSpec 版本';
COMMENT ON COLUMN business.creation_spec_command_receipt.result_status IS '首次安全响应冻结的 CreationSpec 状态';
COMMENT ON COLUMN business.creation_spec_command_receipt.result_content_digest IS '首次安全响应冻结的内容 SHA-256 摘要';
COMMENT ON COLUMN business.creation_spec_command_receipt.created_at IS '首次命令提交时间';

CREATE INDEX creation_spec_command_receipt_project_user_created_idx
    ON business.creation_spec_command_receipt (project_id, user_id, created_at DESC);
