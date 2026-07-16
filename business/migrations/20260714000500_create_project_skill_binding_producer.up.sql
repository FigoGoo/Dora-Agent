ALTER TABLE business.skill
    ADD COLUMN governance_epoch bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT ck_skill__governance_epoch CHECK (governance_epoch >= 1);

COMMENT ON COLUMN business.skill.governance_epoch IS 'Skill 治理有效性纪元，初始为 1；治理有效性变化时递增，发布内容不递增';

CREATE TABLE business.project_skill_binding_set (
    project_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    schema_version varchar(64) NOT NULL,
    set_version bigint NOT NULL,
    selection_digest bytea NOT NULL,
    enabled_count integer NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_project_skill_binding_set PRIMARY KEY (project_id),
    CONSTRAINT ck_project_skill_binding_set__schema CHECK (schema_version = 'project_skill_binding_set.v1'),
    CONSTRAINT ck_project_skill_binding_set__version CHECK (set_version >= 1),
    CONSTRAINT ck_project_skill_binding_set__selection_digest CHECK (octet_length(selection_digest) = 32),
    CONSTRAINT ck_project_skill_binding_set__enabled_count CHECK (enabled_count BETWEEN 0 AND 32)
);

COMMENT ON TABLE business.project_skill_binding_set IS '项目期望 Skill 集合聚合根表；空集合也保留版本、摘要和可信所有者';
COMMENT ON COLUMN business.project_skill_binding_set.project_id IS '所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding_set.owner_user_id IS '集合冻结的项目所有者用户标识，只来自可信身份与项目事实';
COMMENT ON COLUMN business.project_skill_binding_set.schema_version IS '绑定集合结构版本，W1 固定为 project_skill_binding_set.v1';
COMMENT ON COLUMN business.project_skill_binding_set.set_version IS '绑定集合 CAS 版本，从 1 开始且仅在集合语义变化时递增';
COMMENT ON COLUMN business.project_skill_binding_set.selection_digest IS '规范排序后的启用绑定 Canonical JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.project_skill_binding_set.enabled_count IS '当前启用 Skill 数量，W1 协议硬上限为 32';
COMMENT ON COLUMN business.project_skill_binding_set.created_at IS '绑定集合创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.project_skill_binding_set.updated_at IS '绑定集合最近语义变更时间，使用 UTC 时间';

CREATE INDEX idx_project_skill_binding_set__owner_updated_project
    ON business.project_skill_binding_set (owner_user_id, updated_at DESC, project_id DESC);

CREATE TABLE business.project_skill_binding (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    namespace varchar(16) NOT NULL,
    priority integer NOT NULL,
    status varchar(16) NOT NULL,
    source varchar(32) NOT NULL,
    enabled_by_user_id uuid NOT NULL,
    enabled_at timestamptz NOT NULL,
    disabled_by_user_id uuid NULL,
    disabled_at timestamptz NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_project_skill_binding PRIMARY KEY (id),
    CONSTRAINT uq_project_skill_binding__project_skill UNIQUE (project_id, skill_id),
    CONSTRAINT ck_project_skill_binding__namespace CHECK (namespace = 'user'),
    CONSTRAINT ck_project_skill_binding__priority CHECK (priority = 100),
    CONSTRAINT ck_project_skill_binding__status CHECK (status IN ('enabled', 'disabled')),
    CONSTRAINT ck_project_skill_binding__source CHECK (source IN ('quick_create', 'owner_replace')),
    CONSTRAINT ck_project_skill_binding__version CHECK (version >= 1),
    CONSTRAINT ck_project_skill_binding__status_fields CHECK (
        (status = 'enabled' AND disabled_by_user_id IS NULL AND disabled_at IS NULL) OR
        (status = 'disabled' AND disabled_by_user_id IS NOT NULL AND disabled_at IS NOT NULL)
    )
);

COMMENT ON TABLE business.project_skill_binding IS '项目与 Skill 当前绑定表；历史动作进入追加式审计，不由数据库自动级联';
COMMENT ON COLUMN business.project_skill_binding.id IS '绑定唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_skill_binding.project_id IS '所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding.skill_id IS '被绑定 Skill 标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding.namespace IS 'Skill 命名空间，W1 仅允许 user';
COMMENT ON COLUMN business.project_skill_binding.priority IS 'Skill 加载优先级，W1 固定为 100且不接受客户端覆盖';
COMMENT ON COLUMN business.project_skill_binding.status IS '绑定状态：enabled-启用，disabled-停用';
COMMENT ON COLUMN business.project_skill_binding.source IS '绑定来源：quick_create-项目创建，owner_replace-所有者全量替换';
COMMENT ON COLUMN business.project_skill_binding.enabled_by_user_id IS '最近一次启用该绑定的可信项目所有者标识';
COMMENT ON COLUMN business.project_skill_binding.enabled_at IS '最近一次启用时间，使用 UTC 时间';
COMMENT ON COLUMN business.project_skill_binding.disabled_by_user_id IS '最近一次停用该绑定的可信项目所有者标识；启用时为空';
COMMENT ON COLUMN business.project_skill_binding.disabled_at IS '最近一次停用时间；启用时为空并使用 UTC 时间';
COMMENT ON COLUMN business.project_skill_binding.version IS '绑定行 CAS 与审计版本，从 1 开始';
COMMENT ON COLUMN business.project_skill_binding.created_at IS '绑定首次创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.project_skill_binding.updated_at IS '绑定最近状态变更时间，使用 UTC 时间';

CREATE INDEX idx_project_skill_binding__project_status_order
    ON business.project_skill_binding (project_id, status, priority DESC, namespace, skill_id);
CREATE INDEX idx_project_skill_binding__skill_status_project
    ON business.project_skill_binding (skill_id, status, project_id);

CREATE TABLE business.project_skill_binding_command_receipt (
    id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    project_id uuid NOT NULL,
    command_type varchar(64) NOT NULL,
    key_digest bytea NOT NULL,
    semantic_digest bytea NOT NULL,
    result_set_version bigint NOT NULL,
    result_selection_digest bytea NOT NULL,
    result_enabled_count integer NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_project_skill_binding_command_receipt PRIMARY KEY (id),
    CONSTRAINT uq_project_skill_binding_command_receipt__scope UNIQUE (actor_user_id, project_id, command_type, key_digest),
    CONSTRAINT ck_project_skill_binding_command_receipt__command CHECK (command_type = 'replace_project_skill_bindings'),
    CONSTRAINT ck_project_skill_binding_command_receipt__key_digest CHECK (octet_length(key_digest) = 32),
    CONSTRAINT ck_project_skill_binding_command_receipt__semantic_digest CHECK (octet_length(semantic_digest) = 32),
    CONSTRAINT ck_project_skill_binding_command_receipt__set_version CHECK (result_set_version >= 1),
    CONSTRAINT ck_project_skill_binding_command_receipt__selection_digest CHECK (octet_length(result_selection_digest) = 32),
    CONSTRAINT ck_project_skill_binding_command_receipt__enabled_count CHECK (result_enabled_count BETWEEN 0 AND 32)
);

COMMENT ON TABLE business.project_skill_binding_command_receipt IS '项目 Skill 绑定全量替换命令回执表，冻结首次集合结果并收敛并发幂等';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.id IS '绑定命令回执唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.actor_user_id IS '执行绑定替换命令的可信项目所有者标识';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.project_id IS '命令作用项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.command_type IS '命令类型，W1 固定为 replace_project_skill_bindings';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.key_digest IS '原始幂等键的 SHA-256 摘要，数据库不保存原始键';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.semantic_digest IS '项目、Expected Set Version 与目标集合摘要组成的语义摘要';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.result_set_version IS '首次命令成功后的绑定集合版本';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.result_selection_digest IS '首次命令成功后的绑定集合摘要';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.result_enabled_count IS '首次命令成功后的启用 Skill 数量';
COMMENT ON COLUMN business.project_skill_binding_command_receipt.created_at IS '首次命令提交时间，使用 UTC 时间';

CREATE TABLE business.project_skill_binding_audit (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    binding_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    binding_set_version bigint NOT NULL,
    action varchar(32) NOT NULL,
    from_status varchar(16) NULL,
    to_status varchar(16) NOT NULL,
    source varchar(32) NOT NULL,
    actor_user_id uuid NOT NULL,
    command_receipt_id uuid NOT NULL,
    reason_code varchar(128) NULL,
    occurred_at timestamptz NOT NULL,
    CONSTRAINT pk_project_skill_binding_audit PRIMARY KEY (id),
    CONSTRAINT ck_project_skill_binding_audit__set_version CHECK (binding_set_version >= 1),
    CONSTRAINT ck_project_skill_binding_audit__action CHECK (action IN ('enabled', 'disabled', 're_enabled', 'replaced')),
    CONSTRAINT ck_project_skill_binding_audit__from_status CHECK (from_status IS NULL OR from_status IN ('enabled', 'disabled')),
    CONSTRAINT ck_project_skill_binding_audit__to_status CHECK (to_status IN ('enabled', 'disabled')),
    CONSTRAINT ck_project_skill_binding_audit__source CHECK (source IN ('quick_create', 'owner_replace'))
);

COMMENT ON TABLE business.project_skill_binding_audit IS '项目 Skill 绑定追加式审计表，只记录稳定动作与安全原因，不保存 Skill 正文';
COMMENT ON COLUMN business.project_skill_binding_audit.id IS '绑定审计唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_skill_binding_audit.project_id IS '审计所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding_audit.binding_id IS '审计涉及的绑定标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding_audit.skill_id IS '审计涉及的 Skill 标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding_audit.binding_set_version IS '动作完成后的绑定集合版本';
COMMENT ON COLUMN business.project_skill_binding_audit.action IS '稳定审计动作：enabled、disabled、re_enabled 或 replaced';
COMMENT ON COLUMN business.project_skill_binding_audit.from_status IS '动作前绑定状态；首次启用时为空';
COMMENT ON COLUMN business.project_skill_binding_audit.to_status IS '动作后绑定状态：enabled 或 disabled';
COMMENT ON COLUMN business.project_skill_binding_audit.source IS '动作来源：quick_create 或 owner_replace';
COMMENT ON COLUMN business.project_skill_binding_audit.actor_user_id IS '执行动作的可信项目所有者标识';
COMMENT ON COLUMN business.project_skill_binding_audit.command_receipt_id IS '关联本批命令回执或项目创建回执标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_skill_binding_audit.reason_code IS '可选稳定安全原因代码，不保存用户 Prompt、Skill 正文或权限策略原文';
COMMENT ON COLUMN business.project_skill_binding_audit.occurred_at IS '审计动作发生时间，使用 UTC 时间且写入后禁止修改';

CREATE INDEX idx_project_skill_binding_audit__project_occurred_id
    ON business.project_skill_binding_audit (project_id, occurred_at DESC, id DESC);
CREATE INDEX idx_project_skill_binding_audit__command_id
    ON business.project_skill_binding_audit (command_receipt_id, id);

CREATE TABLE business.project_session_skill_resolution (
    id uuid NOT NULL,
    command_id uuid NOT NULL,
    project_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    binding_set_version bigint NOT NULL,
    binding_selection_digest bytea NOT NULL,
    snapshot_schema_version varchar(64) NOT NULL,
    snapshot_kind varchar(32) NOT NULL,
    skill_count integer NOT NULL,
    snapshot_set_digest bytea NOT NULL,
    runtime_policy_ref varchar(256) NOT NULL,
    resolved_at timestamptz NOT NULL,
    CONSTRAINT pk_project_session_skill_resolution PRIMARY KEY (id),
    CONSTRAINT uq_project_session_skill_resolution__command UNIQUE (command_id),
    CONSTRAINT ck_project_session_skill_resolution__binding_version CHECK (binding_set_version >= 1),
    CONSTRAINT ck_project_session_skill_resolution__binding_digest CHECK (octet_length(binding_selection_digest) = 32),
    CONSTRAINT ck_project_session_skill_resolution__schema CHECK (snapshot_schema_version = 'session_skill_snapshot.v1'),
    CONSTRAINT ck_project_session_skill_resolution__kind CHECK (snapshot_kind IN ('empty', 'published_refs')),
    CONSTRAINT ck_project_session_skill_resolution__count CHECK (skill_count BETWEEN 0 AND 32),
    CONSTRAINT ck_project_session_skill_resolution__set_digest CHECK (octet_length(snapshot_set_digest) = 32),
    CONSTRAINT ck_project_session_skill_resolution__policy CHECK (runtime_policy_ref = 'skill-runtime-policy:v1'),
    CONSTRAINT ck_project_session_skill_resolution__kind_count CHECK (
        (snapshot_kind = 'empty' AND skill_count = 0) OR
        (snapshot_kind = 'published_refs' AND skill_count > 0)
    )
);

COMMENT ON TABLE business.project_session_skill_resolution IS 'Session Bootstrap 命令冻结的 Skill 解析头；它不是当前绑定读模型且写入后不可变';
COMMENT ON COLUMN business.project_session_skill_resolution.id IS '解析头唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_session_skill_resolution.command_id IS '对应 Session Bootstrap Outbox 与 Agent command_id，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution.project_id IS '解析所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution.owner_user_id IS '解析时冻结的可信项目所有者标识';
COMMENT ON COLUMN business.project_session_skill_resolution.binding_set_version IS '解析时冻结的绑定集合 CAS 版本';
COMMENT ON COLUMN business.project_session_skill_resolution.binding_selection_digest IS '解析时冻结的绑定集合 SHA-256 摘要';
COMMENT ON COLUMN business.project_session_skill_resolution.snapshot_schema_version IS 'Session Skill Snapshot 结构版本，W1 固定为 session_skill_snapshot.v1';
COMMENT ON COLUMN business.project_session_skill_resolution.snapshot_kind IS '快照类型：empty-空集合，published_refs-已发布 Skill 引用集合';
COMMENT ON COLUMN business.project_session_skill_resolution.skill_count IS '本次不可变快照冻结的 Skill 数量';
COMMENT ON COLUMN business.project_session_skill_resolution.snapshot_set_digest IS '按稳定顺序编码的 Snapshot Item 元数据集合 SHA-256 摘要';
COMMENT ON COLUMN business.project_session_skill_resolution.runtime_policy_ref IS '运行时安全策略引用，W1 固定为 skill-runtime-policy:v1';
COMMENT ON COLUMN business.project_session_skill_resolution.resolved_at IS '同一 Project 创建事务内冻结解析结果的 UTC 时间';

CREATE INDEX idx_project_session_skill_resolution__project_resolved_id
    ON business.project_session_skill_resolution (project_id, resolved_at DESC, id DESC);

CREATE TABLE business.project_session_skill_resolution_item (
    resolution_id uuid NOT NULL,
    project_id uuid NOT NULL,
    command_id uuid NOT NULL,
    load_order integer NOT NULL,
    priority integer NOT NULL,
    namespace varchar(16) NOT NULL,
    binding_id uuid NOT NULL,
    binding_version bigint NOT NULL,
    skill_id uuid NOT NULL,
    publisher_user_id uuid NOT NULL,
    published_snapshot_id uuid NOT NULL,
    publication_revision bigint NOT NULL,
    definition_schema_version varchar(64) NOT NULL,
    content_digest bytea NOT NULL,
    runtime_content_schema_version varchar(64) NOT NULL,
    runtime_content_digest bytea NOT NULL,
    allowed_graph_tool_keys jsonb NOT NULL,
    public_tool_refs jsonb NOT NULL,
    permission_snapshot_digest bytea NOT NULL,
    runtime_policy_ref varchar(256) NOT NULL,
    governance_epoch bigint NOT NULL,
    published_at_unix_ms bigint NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_project_session_skill_resolution_item PRIMARY KEY (resolution_id, load_order),
    CONSTRAINT uq_project_session_skill_resolution_item__skill UNIQUE (resolution_id, skill_id),
    CONSTRAINT uq_project_session_skill_resolution_item__snapshot UNIQUE (resolution_id, published_snapshot_id),
    CONSTRAINT ck_project_session_skill_resolution_item__load_order CHECK (load_order > 0),
    CONSTRAINT ck_project_session_skill_resolution_item__priority CHECK (priority = 100),
    CONSTRAINT ck_project_session_skill_resolution_item__namespace CHECK (namespace = 'user'),
    CONSTRAINT ck_project_session_skill_resolution_item__binding_version CHECK (binding_version >= 1),
    CONSTRAINT ck_project_session_skill_resolution_item__publication_revision CHECK (publication_revision >= 1),
    CONSTRAINT ck_project_session_skill_resolution_item__definition_schema CHECK (definition_schema_version = 'skill_definition.v1'),
    CONSTRAINT ck_project_session_skill_resolution_item__content_digest CHECK (octet_length(content_digest) = 32),
    CONSTRAINT ck_project_session_skill_resolution_item__runtime_schema CHECK (runtime_content_schema_version = 'skill_runtime_content.v1'),
    CONSTRAINT ck_project_session_skill_resolution_item__runtime_digest CHECK (octet_length(runtime_content_digest) = 32),
    CONSTRAINT ck_project_session_skill_resolution_item__graph_keys CHECK (jsonb_typeof(allowed_graph_tool_keys) = 'array' AND jsonb_array_length(allowed_graph_tool_keys) <= 6),
    CONSTRAINT ck_project_session_skill_resolution_item__public_refs CHECK (jsonb_typeof(public_tool_refs) = 'array' AND public_tool_refs = '[]'::jsonb),
    CONSTRAINT ck_project_session_skill_resolution_item__permission_digest CHECK (octet_length(permission_snapshot_digest) = 32),
    CONSTRAINT ck_project_session_skill_resolution_item__policy CHECK (runtime_policy_ref = 'skill-runtime-policy:v1'),
    CONSTRAINT ck_project_session_skill_resolution_item__governance_epoch CHECK (governance_epoch >= 1),
    CONSTRAINT ck_project_session_skill_resolution_item__published_at CHECK (published_at_unix_ms > 0)
);

COMMENT ON TABLE business.project_session_skill_resolution_item IS 'Session Bootstrap 冻结的 Skill 解析项元数据；正文仍由不可变发布事实承载且本表写入后不可变';
COMMENT ON COLUMN business.project_session_skill_resolution_item.resolution_id IS '所属解析头标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution_item.project_id IS '所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution_item.command_id IS '所属 Session Bootstrap command_id，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution_item.load_order IS 'Session 内稠密加载顺序，从 1 开始';
COMMENT ON COLUMN business.project_session_skill_resolution_item.priority IS '冻结的 Skill 加载优先级，W1 固定为 100';
COMMENT ON COLUMN business.project_session_skill_resolution_item.namespace IS '冻结的 Skill 命名空间，W1 固定为 user';
COMMENT ON COLUMN business.project_session_skill_resolution_item.binding_id IS '解析来源绑定标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution_item.binding_version IS '解析时绑定行的 CAS 与审计版本';
COMMENT ON COLUMN business.project_session_skill_resolution_item.skill_id IS '冻结的 Business Skill 标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution_item.publisher_user_id IS '发布者用户标识，W1 owner-private 等于项目所有者';
COMMENT ON COLUMN business.project_session_skill_resolution_item.published_snapshot_id IS '冻结的不可变发布快照标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_skill_resolution_item.publication_revision IS '冻结的 Skill 内部发布修订序号';
COMMENT ON COLUMN business.project_session_skill_resolution_item.definition_schema_version IS '完整发布定义版本，W1 固定为 skill_definition.v1';
COMMENT ON COLUMN business.project_session_skill_resolution_item.content_digest IS '完整发布定义 Canonical JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.project_session_skill_resolution_item.runtime_content_schema_version IS '运行时内容投影版本，W1 固定为 skill_runtime_content.v1';
COMMENT ON COLUMN business.project_session_skill_resolution_item.runtime_content_digest IS '运行时内容 Canonical JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.project_session_skill_resolution_item.allowed_graph_tool_keys IS '六能力适用性导出的声明键数组，不证明 Tool 已注册、编译或可执行';
COMMENT ON COLUMN business.project_session_skill_resolution_item.public_tool_refs IS '公共 Tool 引用数组，W1 必须为空数组';
COMMENT ON COLUMN business.project_session_skill_resolution_item.permission_snapshot_digest IS 'owner-private 权限快照 Canonical JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.project_session_skill_resolution_item.runtime_policy_ref IS '运行时安全策略引用，W1 固定为 skill-runtime-policy:v1';
COMMENT ON COLUMN business.project_session_skill_resolution_item.governance_epoch IS '解析时冻结的 Skill 治理有效性纪元';
COMMENT ON COLUMN business.project_session_skill_resolution_item.published_at_unix_ms IS '发布时刻的 Unix 毫秒整数，保留跨 Module Canonical 原值';
COMMENT ON COLUMN business.project_session_skill_resolution_item.created_at IS '解析项创建时间，使用 UTC 时间且写入后禁止修改';

CREATE INDEX idx_project_session_skill_resolution_item__project_command_order
    ON business.project_session_skill_resolution_item (project_id, command_id, load_order);
CREATE INDEX idx_project_session_skill_resolution_item__skill_snapshot
    ON business.project_session_skill_resolution_item (skill_id, published_snapshot_id);

CREATE FUNCTION business.reject_project_skill_binding_immutable_fact_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Project Skill Binding immutable fact cannot be updated or deleted'
        USING ERRCODE = '55000';
END;
$$;

COMMENT ON FUNCTION business.reject_project_skill_binding_immutable_fact_change() IS '拒绝修改或删除项目 Skill 绑定审计与 Session Skill 解析不可变事实';

CREATE TRIGGER trg_project_skill_binding_audit__immutable
BEFORE UPDATE OR DELETE ON business.project_skill_binding_audit
FOR EACH ROW EXECUTE FUNCTION business.reject_project_skill_binding_immutable_fact_change();

CREATE TRIGGER trg_project_session_skill_resolution__immutable
BEFORE UPDATE OR DELETE ON business.project_session_skill_resolution
FOR EACH ROW EXECUTE FUNCTION business.reject_project_skill_binding_immutable_fact_change();

CREATE TRIGGER trg_project_session_skill_resolution_item__immutable
BEFORE UPDATE OR DELETE ON business.project_session_skill_resolution_item
FOR EACH ROW EXECUTE FUNCTION business.reject_project_skill_binding_immutable_fact_change();

ALTER TABLE business.project_creation_receipt
    ADD COLUMN request_schema_version varchar(64) NOT NULL DEFAULT 'project_quick_create.v1',
    ADD COLUMN skill_snapshot_digest bytea NOT NULL DEFAULT decode('4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945', 'hex'),
    ADD COLUMN skill_count integer NOT NULL DEFAULT 0,
    ADD COLUMN binding_set_version bigint NULL,
    ADD COLUMN resolution_id uuid NULL,
    ADD CONSTRAINT ck_project_creation_receipt__request_schema CHECK (request_schema_version IN ('project_quick_create.v1', 'project_quick_create.v2')),
    ADD CONSTRAINT ck_project_creation_receipt__snapshot_digest CHECK (octet_length(skill_snapshot_digest) = 32),
    ADD CONSTRAINT ck_project_creation_receipt__skill_count CHECK (skill_count BETWEEN 0 AND 32),
    ADD CONSTRAINT ck_project_creation_receipt__version_projection CHECK (
        (request_schema_version = 'project_quick_create.v1' AND skill_count = 0 AND binding_set_version IS NULL AND resolution_id IS NULL) OR
        (request_schema_version = 'project_quick_create.v2' AND binding_set_version >= 1 AND resolution_id IS NOT NULL)
    );

COMMENT ON COLUMN business.project_creation_receipt.request_schema_version IS '快速创建请求语义版本；存量固定为 project_quick_create.v1，显式 v2 才进入 Skill 冻结路径';
COMMENT ON COLUMN business.project_creation_receipt.skill_snapshot_digest IS '首次命令冻结的 Session Skill Snapshot 集合摘要；v1 为固定空摘要';
COMMENT ON COLUMN business.project_creation_receipt.skill_count IS '首次命令冻结的 Skill 数量；v1 固定为 0';
COMMENT ON COLUMN business.project_creation_receipt.binding_set_version IS '显式 v2 冻结的项目 Skill 绑定集合版本；v1 为空';
COMMENT ON COLUMN business.project_creation_receipt.resolution_id IS '显式 v2 冻结的 Session Skill 解析头标识；v1 为空且不设置数据库物理外键约束';

ALTER TABLE business.project_session_binding
    ADD COLUMN request_schema_version varchar(64) NOT NULL DEFAULT 'ensure_project_session.v1',
    ADD COLUMN skill_snapshot_digest bytea NOT NULL DEFAULT decode('4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945', 'hex'),
    ADD COLUMN skill_count integer NOT NULL DEFAULT 0,
    ADD COLUMN binding_set_version bigint NULL,
    ADD COLUMN resolution_id uuid NULL,
    ADD CONSTRAINT ck_project_session_binding__request_schema CHECK (request_schema_version IN ('ensure_project_session.v1', 'ensure_project_session.v2')),
    ADD CONSTRAINT ck_project_session_binding__snapshot_digest CHECK (octet_length(skill_snapshot_digest) = 32),
    ADD CONSTRAINT ck_project_session_binding__skill_count CHECK (skill_count BETWEEN 0 AND 32),
    ADD CONSTRAINT ck_project_session_binding__version_projection CHECK (
        (request_schema_version = 'ensure_project_session.v1' AND skill_count = 0 AND binding_set_version IS NULL AND resolution_id IS NULL) OR
        (request_schema_version = 'ensure_project_session.v2' AND binding_set_version >= 1 AND resolution_id IS NOT NULL)
    );

COMMENT ON COLUMN business.project_session_binding.request_schema_version IS 'Agent Session 初始化请求版本：ensure_project_session.v1 或 ensure_project_session.v2';
COMMENT ON COLUMN business.project_session_binding.skill_snapshot_digest IS 'Session 创建时冻结的 Skill Snapshot 摘要；v1 为固定空摘要';
COMMENT ON COLUMN business.project_session_binding.skill_count IS 'Session 创建时冻结的 Skill 数量；v1 固定为 0';
COMMENT ON COLUMN business.project_session_binding.binding_set_version IS 'v2 Session 创建时冻结的项目 Skill 绑定集合版本；v1 为空';
COMMENT ON COLUMN business.project_session_binding.resolution_id IS 'v2 Session 创建时冻结的解析头标识；v1 为空且不设置数据库物理外键约束';

ALTER TABLE business.project_session_outbox
    DROP CONSTRAINT ck_project_session_outbox__schema_version,
    DROP CONSTRAINT ck_project_session_outbox__encrypted_payload,
    ALTER COLUMN payload_key_version TYPE varchar(128),
    ADD COLUMN skill_snapshot_digest bytea NOT NULL DEFAULT decode('4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945', 'hex'),
    ADD COLUMN skill_count integer NOT NULL DEFAULT 0,
    ADD COLUMN binding_set_version bigint NULL,
    ADD COLUMN resolution_id uuid NULL,
    ADD CONSTRAINT ck_project_session_outbox__schema_version CHECK (schema_version IN ('agent.session-bootstrap.v1', 'session_bootstrap_outbox_payload.v2')),
    ADD CONSTRAINT ck_project_session_outbox__snapshot_digest CHECK (octet_length(skill_snapshot_digest) = 32),
    ADD CONSTRAINT ck_project_session_outbox__skill_count CHECK (skill_count BETWEEN 0 AND 32),
    ADD CONSTRAINT ck_project_session_outbox__version_projection CHECK (
        (schema_version = 'agent.session-bootstrap.v1' AND skill_count = 0 AND binding_set_version IS NULL AND resolution_id IS NULL) OR
        (schema_version = 'session_bootstrap_outbox_payload.v2' AND binding_set_version >= 1 AND resolution_id IS NOT NULL)
    ),
    ADD CONSTRAINT ck_project_session_outbox__encrypted_payload CHECK (
        (
            schema_version = 'agent.session-bootstrap.v1' AND has_initial_prompt = false AND
            payload_encryption_algorithm IS NULL AND payload_key_version IS NULL AND
            payload_nonce IS NULL AND payload_ciphertext IS NULL AND payload_digest IS NULL AND
            payload_cleared_at IS NULL
        ) OR
        (
            schema_version = 'agent.session-bootstrap.v1' AND has_initial_prompt = true AND
            payload_encryption_algorithm = 'aes-256-gcm' AND
            payload_key_version IS NOT NULL AND length(payload_key_version) BETWEEN 1 AND 64 AND
            payload_nonce IS NOT NULL AND octet_length(payload_nonce) = 12 AND
            payload_ciphertext IS NOT NULL AND octet_length(payload_ciphertext) > 16 AND
            payload_digest IS NOT NULL AND octet_length(payload_digest) = 32 AND
            payload_cleared_at IS NULL
        ) OR
        (
            schema_version = 'agent.session-bootstrap.v1' AND has_initial_prompt = true AND status = 'delivered' AND
            payload_encryption_algorithm IS NULL AND payload_key_version IS NULL AND
            payload_nonce IS NULL AND payload_ciphertext IS NULL AND
            payload_digest IS NOT NULL AND octet_length(payload_digest) = 32 AND
            payload_cleared_at IS NOT NULL AND delivered_at IS NOT NULL AND payload_cleared_at >= delivered_at
        ) OR
        (
            schema_version = 'session_bootstrap_outbox_payload.v2' AND status <> 'delivered' AND
            payload_encryption_algorithm = 'aes-256-gcm' AND
            payload_key_version IS NOT NULL AND length(payload_key_version) BETWEEN 1 AND 128 AND
            payload_nonce IS NOT NULL AND octet_length(payload_nonce) = 12 AND
            payload_ciphertext IS NOT NULL AND octet_length(payload_ciphertext) > 16 AND
            payload_digest IS NOT NULL AND octet_length(payload_digest) = 32 AND
            payload_cleared_at IS NULL
        ) OR
        (
            schema_version = 'session_bootstrap_outbox_payload.v2' AND status = 'delivered' AND
            payload_encryption_algorithm IS NULL AND payload_key_version IS NULL AND
            payload_nonce IS NULL AND payload_ciphertext IS NULL AND
            payload_digest IS NOT NULL AND octet_length(payload_digest) = 32 AND
            payload_cleared_at IS NOT NULL AND delivered_at IS NOT NULL AND payload_cleared_at >= delivered_at
        )
    );

COMMENT ON COLUMN business.project_session_outbox.schema_version IS 'Outbox 负载版本：v1 只保护可选 Prompt，v2 保护完整 Session Bootstrap 明文';
COMMENT ON COLUMN business.project_session_outbox.payload_encryption_algorithm IS '认证加密算法；v1 保护 Prompt，v2 保护完整 Bootstrap 负载，均禁止保存明文';
COMMENT ON COLUMN business.project_session_outbox.payload_key_version IS '用途隔离的负载加密密钥版本引用，不包含密钥材料且不得进入普通日志';
COMMENT ON COLUMN business.project_session_outbox.payload_nonce IS '负载认证加密随机数，不得复用或进入普通日志';
COMMENT ON COLUMN business.project_session_outbox.payload_ciphertext IS '认证加密密文及标签；v2 包含 Prompt 与 Runtime Content，只能在交付确认后清理';
COMMENT ON COLUMN business.project_session_outbox.payload_digest IS 'v1 为 Prompt 明文摘要，v2 为完整版本化 Bootstrap plaintext Canonical 摘要';
COMMENT ON COLUMN business.project_session_outbox.payload_cleared_at IS 'Agent 回执确认后清除密文材料的时间；仅 delivered 可非空且不得早于 delivered_at';
COMMENT ON COLUMN business.project_session_outbox.skill_snapshot_digest IS '冻结的 Session Skill Snapshot 集合摘要；v1 为固定空摘要';
COMMENT ON COLUMN business.project_session_outbox.skill_count IS '冻结的 Skill 数量；v1 固定为 0';
COMMENT ON COLUMN business.project_session_outbox.binding_set_version IS 'v2 冻结的项目 Skill 绑定集合版本；v1 为空';
COMMENT ON COLUMN business.project_session_outbox.resolution_id IS 'v2 冻结的 Session Skill 解析头标识；v1 为空且不设置数据库物理外键约束';
