CREATE TABLE business.skill (
    id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    current_draft_revision_id uuid NOT NULL,
    current_published_snapshot_id uuid NULL,
    publication_revision bigint NOT NULL DEFAULT 0,
    governance_status varchar(32) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_skill PRIMARY KEY (id),
    CONSTRAINT ck_skill__publication_revision CHECK (publication_revision >= 0),
    CONSTRAINT ck_skill__publication_pointer CHECK (
        (publication_revision = 0 AND current_published_snapshot_id IS NULL) OR
        (publication_revision > 0 AND current_published_snapshot_id IS NOT NULL)
    ),
    CONSTRAINT ck_skill__governance_status CHECK (governance_status IN ('active', 'suspended', 'offline')),
    CONSTRAINT ck_skill__version CHECK (version >= 1)
);

COMMENT ON TABLE business.skill IS 'Skill 聚合根表，保存所有权、当前草稿与发布指针、治理状态和并发版本';
COMMENT ON COLUMN business.skill.id IS 'Skill 唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.skill.owner_user_id IS 'Skill 所有者用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill.current_draft_revision_id IS '当前草稿内容修订标识，为同聚合逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill.current_published_snapshot_id IS '当前生效发布快照标识，尚未发布时为空，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill.publication_revision IS '内部发布修订序号，从零开始且不得投影给普通用户';
COMMENT ON COLUMN business.skill.governance_status IS '治理可用性：active-可用，suspended-暂停，offline-下线';
COMMENT ON COLUMN business.skill.version IS 'Skill 聚合并发版本，从 1 开始并用于条件更新';
COMMENT ON COLUMN business.skill.created_at IS 'Skill 创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.skill.updated_at IS 'Skill 聚合最近更新时间，使用 UTC 时间';

CREATE INDEX idx_skill__owner_updated_id
    ON business.skill (owner_user_id, updated_at DESC, id DESC);
CREATE INDEX idx_skill__governance_updated_id
    ON business.skill (governance_status, updated_at DESC, id DESC);

CREATE TABLE business.skill_content_revision (
    id uuid NOT NULL,
    skill_id uuid NOT NULL,
    revision_no bigint NOT NULL,
    definition_schema_version varchar(64) NOT NULL,
    definition_json jsonb NOT NULL,
    content_digest bytea NOT NULL,
    created_by_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_skill_content_revision PRIMARY KEY (id),
    CONSTRAINT uq_skill_content_revision__skill_revision UNIQUE (skill_id, revision_no),
    CONSTRAINT ck_skill_content_revision__revision_no CHECK (revision_no >= 1),
    CONSTRAINT ck_skill_content_revision__schema CHECK (definition_schema_version = 'skill_definition.v1'),
    CONSTRAINT ck_skill_content_revision__definition_json CHECK (jsonb_typeof(definition_json) = 'object'),
    CONSTRAINT ck_skill_content_revision__content_digest CHECK (octet_length(content_digest) = 32)
);

COMMENT ON TABLE business.skill_content_revision IS 'Skill 不可变内容修订表，记录每次完整草稿替换后的结构化定义';
COMMENT ON COLUMN business.skill_content_revision.id IS '内容修订唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.skill_content_revision.skill_id IS '所属 Skill 标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_content_revision.revision_no IS '同一 Skill 内部递增内容修订序号，不向普通用户展示';
COMMENT ON COLUMN business.skill_content_revision.definition_schema_version IS '结构化 Skill 定义版本，W1 固定为 skill_definition.v1';
COMMENT ON COLUMN business.skill_content_revision.definition_json IS '完成 NFC、去重、排序和字段校验后的结构化 Skill 定义';
COMMENT ON COLUMN business.skill_content_revision.content_digest IS 'skill_definition.v1 Canonical JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.skill_content_revision.created_by_user_id IS '创建该内容修订的可信用户标识，不接受客户端覆盖';
COMMENT ON COLUMN business.skill_content_revision.created_at IS '内容修订创建时间，使用 UTC 时间且写入后禁止更新';

CREATE INDEX idx_skill_content_revision__skill_created_id
    ON business.skill_content_revision (skill_id, created_at DESC, id DESC);

CREATE TABLE business.skill_review_submission (
    id uuid NOT NULL,
    skill_id uuid NOT NULL,
    content_revision_id uuid NOT NULL,
    content_digest bytea NOT NULL,
    status varchar(32) NOT NULL,
    safe_reason_code varchar(128) NULL,
    version bigint NOT NULL DEFAULT 1,
    submitted_by_user_id uuid NOT NULL,
    decided_by_user_id uuid NULL,
    submitted_at timestamptz NOT NULL,
    decided_at timestamptz NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_skill_review_submission PRIMARY KEY (id),
    CONSTRAINT ck_skill_review_submission__content_digest CHECK (octet_length(content_digest) = 32),
    CONSTRAINT ck_skill_review_submission__status CHECK (status IN ('reviewing', 'approved', 'rejected', 'withdrawn')),
    CONSTRAINT ck_skill_review_submission__version CHECK (version >= 1),
    CONSTRAINT ck_skill_review_submission__decision CHECK (
        (status = 'reviewing' AND decided_by_user_id IS NULL AND decided_at IS NULL) OR
        (status <> 'reviewing' AND decided_by_user_id IS NOT NULL AND decided_at IS NOT NULL)
    )
);

COMMENT ON TABLE business.skill_review_submission IS 'Skill 审核提交表，冻结提交时的精确内容修订、摘要和审核状态';
COMMENT ON COLUMN business.skill_review_submission.id IS '审核提交唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.skill_review_submission.skill_id IS '所属 Skill 标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_review_submission.content_revision_id IS '提交审核的精确不可变内容修订标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_review_submission.content_digest IS '提交审核内容的 SHA-256 摘要，审批时必须重新核对';
COMMENT ON COLUMN business.skill_review_submission.status IS '审核状态：reviewing-审核中，approved-通过，rejected-拒绝，withdrawn-撤回';
COMMENT ON COLUMN business.skill_review_submission.safe_reason_code IS '可安全返回的稳定审核原因代码，不保存内部策略原文';
COMMENT ON COLUMN business.skill_review_submission.version IS '审核提交并发版本，从 1 开始并用于状态条件更新';
COMMENT ON COLUMN business.skill_review_submission.submitted_by_user_id IS '提交审核的可信 Skill 所有者用户标识';
COMMENT ON COLUMN business.skill_review_submission.decided_by_user_id IS '作出终态决定的可信 Reviewer 用户标识，审核中为空';
COMMENT ON COLUMN business.skill_review_submission.submitted_at IS '提交审核时间，使用 UTC 时间';
COMMENT ON COLUMN business.skill_review_submission.decided_at IS '审核终态决定时间，审核中为空并使用 UTC 时间';
COMMENT ON COLUMN business.skill_review_submission.updated_at IS '审核状态最近更新时间，使用 UTC 时间';

CREATE UNIQUE INDEX uq_skill_review_submission__one_reviewing
    ON business.skill_review_submission (skill_id)
    WHERE status = 'reviewing';
CREATE INDEX idx_skill_review_submission__skill_submitted_id
    ON business.skill_review_submission (skill_id, submitted_at DESC, id DESC);
CREATE INDEX idx_skill_review_submission__status_submitted_id
    ON business.skill_review_submission (status, submitted_at, id);

CREATE TABLE business.skill_published_snapshot (
    id uuid NOT NULL,
    skill_id uuid NOT NULL,
    source_content_revision_id uuid NOT NULL,
    review_submission_id uuid NOT NULL,
    publication_revision bigint NOT NULL,
    definition_schema_version varchar(64) NOT NULL,
    definition_json jsonb NOT NULL,
    content_digest bytea NOT NULL,
    published_by_user_id uuid NOT NULL,
    published_at timestamptz NOT NULL,
    CONSTRAINT pk_skill_published_snapshot PRIMARY KEY (id),
    CONSTRAINT uq_skill_published_snapshot__skill_publication UNIQUE (skill_id, publication_revision),
    CONSTRAINT uq_skill_published_snapshot__review UNIQUE (review_submission_id),
    CONSTRAINT ck_skill_published_snapshot__publication_revision CHECK (publication_revision >= 1),
    CONSTRAINT ck_skill_published_snapshot__schema CHECK (definition_schema_version = 'skill_definition.v1'),
    CONSTRAINT ck_skill_published_snapshot__definition_json CHECK (jsonb_typeof(definition_json) = 'object'),
    CONSTRAINT ck_skill_published_snapshot__content_digest CHECK (octet_length(content_digest) = 32)
);

COMMENT ON TABLE business.skill_published_snapshot IS 'Skill 不可变发布快照表，保存审核通过后精确内容和内部发布修订';
COMMENT ON COLUMN business.skill_published_snapshot.id IS '发布快照唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.skill_published_snapshot.skill_id IS '所属 Skill 标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_published_snapshot.source_content_revision_id IS '发布来源的不可变内容修订标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_published_snapshot.review_submission_id IS '批准本次发布的审核提交标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_published_snapshot.publication_revision IS '同一 Skill 内部递增发布修订，仅用于服务端冻结和并发控制';
COMMENT ON COLUMN business.skill_published_snapshot.definition_schema_version IS '发布内容结构版本，W1 固定为 skill_definition.v1';
COMMENT ON COLUMN business.skill_published_snapshot.definition_json IS '审核通过且完成权威重校验的不可变结构化 Skill 定义';
COMMENT ON COLUMN business.skill_published_snapshot.content_digest IS '发布定义 Canonical JSON 的 SHA-256 摘要';
COMMENT ON COLUMN business.skill_published_snapshot.published_by_user_id IS '执行批准并发布的可信 Reviewer 用户标识';
COMMENT ON COLUMN business.skill_published_snapshot.published_at IS '发布快照创建时间，使用 UTC 时间且写入后禁止更新';

CREATE INDEX idx_skill_published_snapshot__skill_published_id
    ON business.skill_published_snapshot (skill_id, published_at DESC, id DESC);

CREATE TABLE business.skill_command_receipt (
    id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    command_type varchar(64) NOT NULL,
    scope_id uuid NOT NULL,
    key_digest bytea NOT NULL,
    semantic_digest bytea NOT NULL,
    result_skill_id uuid NOT NULL,
    result_content_revision_id uuid NULL,
    result_review_submission_id uuid NULL,
    result_published_snapshot_id uuid NULL,
    response_draft_revision_id uuid NOT NULL,
    response_published_snapshot_id uuid NULL,
    response_review_submission_id uuid NULL,
    response_review_status varchar(32) NULL,
    response_review_reason_code varchar(128) NULL,
    response_review_updated_at timestamptz NULL,
    response_governance_status varchar(32) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_skill_command_receipt PRIMARY KEY (id),
    CONSTRAINT uq_skill_command_receipt__scope UNIQUE (actor_user_id, command_type, scope_id, key_digest),
    CONSTRAINT ck_skill_command_receipt__command_type CHECK (command_type IN ('create', 'submit_review', 'approve_and_publish')),
    CONSTRAINT ck_skill_command_receipt__key_digest CHECK (octet_length(key_digest) = 32),
    CONSTRAINT ck_skill_command_receipt__semantic_digest CHECK (octet_length(semantic_digest) = 32),
    CONSTRAINT ck_skill_command_receipt__response_review_status CHECK (response_review_status IS NULL OR response_review_status IN ('reviewing', 'approved', 'rejected', 'withdrawn')),
    CONSTRAINT ck_skill_command_receipt__response_review CHECK (
        (response_review_submission_id IS NULL AND response_review_status IS NULL AND response_review_reason_code IS NULL AND response_review_updated_at IS NULL) OR
        (response_review_submission_id IS NOT NULL AND response_review_status IS NOT NULL AND response_review_updated_at IS NOT NULL)
    ),
    CONSTRAINT ck_skill_command_receipt__response_governance CHECK (response_governance_status IN ('active', 'suspended', 'offline'))
);

COMMENT ON TABLE business.skill_command_receipt IS 'Skill 命令幂等回执表，保存键摘要、语义摘要和安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.id IS '命令回执唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.skill_command_receipt.actor_user_id IS '发起命令的可信用户或 Reviewer 标识';
COMMENT ON COLUMN business.skill_command_receipt.command_type IS '命令类型：create、submit_review 或 approve_and_publish';
COMMENT ON COLUMN business.skill_command_receipt.scope_id IS '命令幂等作用域标识，创建时为 Owner，其他命令为 Skill 或 Review 标识';
COMMENT ON COLUMN business.skill_command_receipt.key_digest IS '客户端或内部调用方幂等键的 SHA-256 摘要，不保存原始键';
COMMENT ON COLUMN business.skill_command_receipt.semantic_digest IS '命令稳定业务语义的 SHA-256 摘要，用于识别同键异义';
COMMENT ON COLUMN business.skill_command_receipt.result_skill_id IS '首次命令产生或影响的 Skill 安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.result_content_revision_id IS '首次命令产生或冻结的内容修订安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.result_review_submission_id IS '首次命令产生或决定的审核提交安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.result_published_snapshot_id IS '批准发布命令产生的发布快照安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.response_draft_revision_id IS '首次安全响应使用的不可变草稿修订引用，用于后续精确重放而不复制完整正文';
COMMENT ON COLUMN business.skill_command_receipt.response_published_snapshot_id IS '首次安全响应使用的当前发布快照引用，未发布时为空';
COMMENT ON COLUMN business.skill_command_receipt.response_review_submission_id IS '首次安全响应使用的审核提交引用，从未审核时为空';
COMMENT ON COLUMN business.skill_command_receipt.response_review_status IS '首次安全响应冻结的审核状态，不跟随审核后续演进';
COMMENT ON COLUMN business.skill_command_receipt.response_review_reason_code IS '首次安全响应冻结的可安全审核原因代码';
COMMENT ON COLUMN business.skill_command_receipt.response_review_updated_at IS '首次安全响应冻结的审核状态时间，未审核时为空';
COMMENT ON COLUMN business.skill_command_receipt.response_governance_status IS '首次安全响应冻结的治理状态，不跟随后续治理演进';
COMMENT ON COLUMN business.skill_command_receipt.created_at IS '首次命令可靠提交时间，使用 UTC 时间';

CREATE INDEX idx_skill_command_receipt__skill_created_id
    ON business.skill_command_receipt (result_skill_id, created_at DESC, id DESC);

CREATE TABLE business.skill_governance_audit (
    id uuid NOT NULL,
    skill_id uuid NOT NULL,
    review_submission_id uuid NULL,
    action varchar(64) NOT NULL,
    from_status varchar(32) NOT NULL,
    to_status varchar(32) NOT NULL,
    safe_reason_code varchar(128) NULL,
    actor_user_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL,
    CONSTRAINT pk_skill_governance_audit PRIMARY KEY (id),
    CONSTRAINT ck_skill_governance_audit__action CHECK (action = 'review_approved_and_published'),
    CONSTRAINT ck_skill_governance_audit__transition CHECK (from_status = 'reviewing' AND to_status = 'approved')
);

COMMENT ON TABLE business.skill_governance_audit IS 'Skill 审核与治理追加审计表，不保存完整 Skill 正文或内部策略原文';
COMMENT ON COLUMN business.skill_governance_audit.id IS '审计记录唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.skill_governance_audit.skill_id IS '审计涉及的 Skill 标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_governance_audit.review_submission_id IS '审计涉及的审核提交标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_governance_audit.action IS '稳定审计动作，W1 当前仅记录审核通过并发布';
COMMENT ON COLUMN business.skill_governance_audit.from_status IS '动作前审核状态，W1 批准发布固定为 reviewing';
COMMENT ON COLUMN business.skill_governance_audit.to_status IS '动作后审核状态，W1 批准发布固定为 approved';
COMMENT ON COLUMN business.skill_governance_audit.safe_reason_code IS '可安全审计的稳定原因代码，不保存策略原文或完整正文';
COMMENT ON COLUMN business.skill_governance_audit.actor_user_id IS '执行动作的可信 Reviewer 用户标识';
COMMENT ON COLUMN business.skill_governance_audit.occurred_at IS '审计动作发生时间，使用 UTC 时间且记录写入后禁止更新';

CREATE INDEX idx_skill_governance_audit__skill_occurred_id
    ON business.skill_governance_audit (skill_id, occurred_at DESC, id DESC);

CREATE FUNCTION business.reject_w1_immutable_skill_fact_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'W1 immutable Skill fact cannot be updated or deleted'
        USING ERRCODE = '55000';
END;
$$;

COMMENT ON FUNCTION business.reject_w1_immutable_skill_fact_change() IS '拒绝修改或删除 W1 Skill 不可变内容修订、发布快照和治理审计事实';

CREATE TRIGGER trg_skill_content_revision__immutable
BEFORE UPDATE OR DELETE ON business.skill_content_revision
FOR EACH ROW EXECUTE FUNCTION business.reject_w1_immutable_skill_fact_change();

CREATE TRIGGER trg_skill_published_snapshot__immutable
BEFORE UPDATE OR DELETE ON business.skill_published_snapshot
FOR EACH ROW EXECUTE FUNCTION business.reject_w1_immutable_skill_fact_change();

CREATE TRIGGER trg_skill_governance_audit__immutable
BEFORE UPDATE OR DELETE ON business.skill_governance_audit
FOR EACH ROW EXECUTE FUNCTION business.reject_w1_immutable_skill_fact_change();
