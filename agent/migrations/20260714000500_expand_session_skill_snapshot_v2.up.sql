-- W1-B1 只能在 W0 空快照事实保持完整时扩展 Schema；异常历史数据必须阻止发布，不能静默回填为另一语义。
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM agent.session_skill_snapshot
        WHERE snapshot_kind <> 'empty'
           OR snapshot_digest <> '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945'
           OR published_snapshot_refs <> '[]'::jsonb
    ) THEN
        RAISE EXCEPTION 'cannot expand session skill snapshot v2: incompatible W0 snapshot data exists';
    END IF;
END
$$;

ALTER TABLE agent.session_skill_snapshot
    ADD COLUMN schema_version varchar(64) NULL,
    ADD COLUMN skill_count integer NULL;

UPDATE agent.session_skill_snapshot
SET schema_version = 'session_skill_snapshot.v1',
    skill_count = 0
WHERE schema_version IS NULL OR skill_count IS NULL;

ALTER TABLE agent.session_skill_snapshot
    ALTER COLUMN schema_version SET DEFAULT 'session_skill_snapshot.v1',
    ALTER COLUMN schema_version SET NOT NULL,
    ALTER COLUMN skill_count SET DEFAULT 0,
    ALTER COLUMN skill_count SET NOT NULL,
    DROP CONSTRAINT ck_session_skill_snapshot__kind,
    DROP CONSTRAINT ck_session_skill_snapshot__empty_refs,
    ADD CONSTRAINT ck_session_skill_snapshot__schema_version
        CHECK (schema_version = 'session_skill_snapshot.v1'),
    ADD CONSTRAINT ck_session_skill_snapshot__kind
        CHECK (snapshot_kind IN ('empty', 'published_refs')),
    ADD CONSTRAINT ck_session_skill_snapshot__skill_count
        CHECK (skill_count BETWEEN 0 AND 32),
    ADD CONSTRAINT ck_session_skill_snapshot__kind_content
        CHECK (
            (snapshot_kind = 'empty'
                AND skill_count = 0
                AND published_snapshot_refs = '[]'::jsonb
                AND snapshot_digest = '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945')
            OR
            (snapshot_kind = 'published_refs'
                AND skill_count > 0
                AND jsonb_array_length(published_snapshot_refs) = skill_count)
        );

COMMENT ON TABLE agent.session_skill_snapshot IS '会话 Skill 冻结快照 Header 表，保存不可变集合摘要及轻量审计引用，运行时内容由 Item 表加密保存';
COMMENT ON COLUMN agent.session_skill_snapshot.snapshot_kind IS '快照类型：empty-显式空集合，published_refs-冻结一个或多个已发布 Skill 引用';
COMMENT ON COLUMN agent.session_skill_snapshot.published_snapshot_refs IS '按 load_order 排列的已发布 Skill 轻量审计引用数组，不保存 Runtime Content 明文且不是运行时读取真源';
COMMENT ON COLUMN agent.session_skill_snapshot.schema_version IS '快照 Header Schema 版本，W1 固定为 session_skill_snapshot.v1';
COMMENT ON COLUMN agent.session_skill_snapshot.skill_count IS '快照 Item 数量，协议硬上限 32，必须与同 Session 的 Item 行数一致';

CREATE TABLE agent.session_skill_snapshot_item (
    session_id uuid NOT NULL,
    load_order integer NOT NULL,
    priority integer NOT NULL,
    namespace varchar(16) NOT NULL,
    skill_id uuid NOT NULL,
    publisher_user_id uuid NOT NULL,
    published_snapshot_id uuid NOT NULL,
    publication_revision bigint NOT NULL,
    definition_schema_version varchar(64) NOT NULL,
    content_digest char(64) NOT NULL,
    runtime_content_schema_version varchar(64) NOT NULL,
    runtime_content_digest char(64) NOT NULL,
    runtime_content_ciphertext bytea NOT NULL,
    runtime_content_key_version varchar(128) NOT NULL,
    allowed_graph_tool_keys jsonb NOT NULL,
    public_tool_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    permission_snapshot_digest char(64) NOT NULL,
    runtime_policy_ref varchar(256) NOT NULL,
    governance_epoch bigint NOT NULL,
    published_at_unix_ms bigint NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_session_skill_snapshot_item PRIMARY KEY (session_id, load_order),
    CONSTRAINT uq_session_skill_snapshot_item__session_skill UNIQUE (session_id, skill_id),
    CONSTRAINT uq_session_skill_snapshot_item__session_published UNIQUE (session_id, published_snapshot_id),
    CONSTRAINT ck_session_skill_snapshot_item__load_order CHECK (load_order > 0),
    CONSTRAINT ck_session_skill_snapshot_item__priority CHECK (priority >= 0),
    CONSTRAINT ck_session_skill_snapshot_item__namespace CHECK (namespace IN ('system', 'user')),
    CONSTRAINT ck_session_skill_snapshot_item__publication_revision CHECK (publication_revision > 0),
    CONSTRAINT ck_session_skill_snapshot_item__definition_schema_version CHECK (definition_schema_version = 'skill_definition.v1'),
    CONSTRAINT ck_session_skill_snapshot_item__content_digest CHECK (content_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_skill_snapshot_item__runtime_schema_version CHECK (runtime_content_schema_version = 'skill_runtime_content.v1'),
    CONSTRAINT ck_session_skill_snapshot_item__runtime_digest CHECK (runtime_content_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_skill_snapshot_item__runtime_envelope CHECK (
        CASE
            WHEN octet_length(runtime_content_ciphertext) < 36 THEN false
            ELSE
                substring(runtime_content_ciphertext FROM 1 FOR 4) = decode('44524145', 'hex') AND
                get_byte(runtime_content_ciphertext, 4) = 1 AND
                get_byte(runtime_content_ciphertext, 5) = 1 AND
                get_byte(runtime_content_ciphertext, 6) = 12
        END
    ),
    CONSTRAINT ck_session_skill_snapshot_item__key_version CHECK (length(runtime_content_key_version) BETWEEN 1 AND 128),
    CONSTRAINT ck_session_skill_snapshot_item__graph_keys_array CHECK (jsonb_typeof(allowed_graph_tool_keys) = 'array'),
    CONSTRAINT ck_session_skill_snapshot_item__public_refs_empty CHECK (public_tool_refs = '[]'::jsonb),
    CONSTRAINT ck_session_skill_snapshot_item__permission_digest CHECK (permission_snapshot_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_skill_snapshot_item__runtime_policy_ref CHECK (length(runtime_policy_ref) > 0),
    CONSTRAINT ck_session_skill_snapshot_item__governance_epoch CHECK (governance_epoch >= 0),
    CONSTRAINT ck_session_skill_snapshot_item__published_at CHECK (published_at_unix_ms > 0)
);

COMMENT ON TABLE agent.session_skill_snapshot_item IS '会话冻结 Skill Item 表，保存不可变元数据和使用 Agent 专用 AAD 加密的 Runtime Content';
COMMENT ON COLUMN agent.session_skill_snapshot_item.session_id IS '关联 Agent 会话的逻辑外键标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_skill_snapshot_item.load_order IS 'Skill 在会话中的稠密加载顺序，从 1 开始并与 session_id 组成主键';
COMMENT ON COLUMN agent.session_skill_snapshot_item.priority IS 'Business 冻结的 Skill 优先级，非负整数，Agent 不重新排序';
COMMENT ON COLUMN agent.session_skill_snapshot_item.namespace IS 'Skill 命名空间：system-系统，user-用户';
COMMENT ON COLUMN agent.session_skill_snapshot_item.skill_id IS 'Business Skill 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_skill_snapshot_item.publisher_user_id IS 'Business 发布者 User 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_skill_snapshot_item.published_snapshot_id IS 'Business 不可变 Published Snapshot 逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_skill_snapshot_item.publication_revision IS 'Business 冻结的发布修订号，从 1 开始';
COMMENT ON COLUMN agent.session_skill_snapshot_item.definition_schema_version IS 'Published Skill Definition Schema 版本，W1 固定为 skill_definition.v1';
COMMENT ON COLUMN agent.session_skill_snapshot_item.content_digest IS '完整 Published Definition 的 SHA-256 小写十六进制摘要，仅作不透明审计事实';
COMMENT ON COLUMN agent.session_skill_snapshot_item.runtime_content_schema_version IS 'Runtime Content Schema 版本，W1 固定为 skill_runtime_content.v1';
COMMENT ON COLUMN agent.session_skill_snapshot_item.runtime_content_digest IS 'Runtime Content canonical 明文的 SHA-256 小写十六进制摘要，读取解密后必须重验';
COMMENT ON COLUMN agent.session_skill_snapshot_item.runtime_content_ciphertext IS 'Agent Skill Snapshot 专用 AES-256-GCM DRAE v1 Envelope，使用 Item 身份与摘要作为 AAD，禁止保存明文';
COMMENT ON COLUMN agent.session_skill_snapshot_item.runtime_content_key_version IS 'Agent Skill Snapshot 专用加密密钥版本引用，不包含密钥材料';
COMMENT ON COLUMN agent.session_skill_snapshot_item.allowed_graph_tool_keys IS '冻结的 Graph Tool 能力声明数组，仅是声明快照，不证明 Tool 已注册或可执行';
COMMENT ON COLUMN agent.session_skill_snapshot_item.public_tool_refs IS '冻结的 Public Tool 引用数组，W1 强制为空数组';
COMMENT ON COLUMN agent.session_skill_snapshot_item.permission_snapshot_digest IS 'Business 权限快照 SHA-256 小写十六进制摘要，Agent 仅作不透明校验和审计';
COMMENT ON COLUMN agent.session_skill_snapshot_item.runtime_policy_ref IS '冻结的 Runtime Policy 版本引用，不包含策略正文';
COMMENT ON COLUMN agent.session_skill_snapshot_item.governance_epoch IS '冻结时治理纪元，非负整数，历史 Snapshot 不回写';
COMMENT ON COLUMN agent.session_skill_snapshot_item.published_at_unix_ms IS 'Published Snapshot 发布时间的 Unix 毫秒整数，保持 Canonical 原值';
COMMENT ON COLUMN agent.session_skill_snapshot_item.created_at IS 'Snapshot Item 冻结时间，使用 UTC 时间';

CREATE INDEX idx_session_skill_snapshot_item__skill_published
    ON agent.session_skill_snapshot_item (skill_id, published_snapshot_id);

ALTER TABLE agent.session_command_receipt
    ADD COLUMN skill_snapshot_digest char(64) NULL,
    ADD COLUMN skill_count integer NULL;

UPDATE agent.session_command_receipt
SET skill_snapshot_digest = '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945',
    skill_count = 0
WHERE skill_snapshot_digest IS NULL OR skill_count IS NULL;

ALTER TABLE agent.session_command_receipt
    ALTER COLUMN skill_snapshot_digest SET DEFAULT '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945',
    ALTER COLUMN skill_snapshot_digest SET NOT NULL,
    ALTER COLUMN skill_count SET DEFAULT 0,
    ALTER COLUMN skill_count SET NOT NULL,
    DROP CONSTRAINT ck_session_command_receipt__type,
    ADD CONSTRAINT ck_session_command_receipt__type
        CHECK (command_type IN ('ensure_project_session_v1', 'ensure_project_session_v2')),
    ADD CONSTRAINT ck_session_command_receipt__skill_snapshot_digest
        CHECK (skill_snapshot_digest ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT ck_session_command_receipt__skill_count
        CHECK (skill_count BETWEEN 0 AND 32);

COMMENT ON COLUMN agent.session_command_receipt.command_type IS '稳定命令类型：ensure_project_session_v1 或 ensure_project_session_v2；同 command_id 跨版本命中必须冲突';
COMMENT ON COLUMN agent.session_command_receipt.skill_snapshot_digest IS '命令冻结的 Session Skill Snapshot set digest；V1 回填规范空集合摘要';
COMMENT ON COLUMN agent.session_command_receipt.skill_count IS '命令冻结的 Session Skill Item 数量；V1 固定为 0，协议硬上限 32';

-- 所有回填必须在同一 Migration 事务内完成；断言失败会整体回滚，避免出现半扩展 Schema。
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM agent.session_skill_snapshot
        WHERE schema_version <> 'session_skill_snapshot.v1'
           OR skill_count <> 0
           OR snapshot_kind <> 'empty'
           OR snapshot_digest <> '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945'
    ) THEN
        RAISE EXCEPTION 'session skill snapshot v2 backfill verification failed';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM agent.session_command_receipt
        WHERE command_type = 'ensure_project_session_v1'
          AND (skill_count <> 0 OR skill_snapshot_digest <> '4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945')
    ) THEN
        RAISE EXCEPTION 'session command receipt v2 backfill verification failed';
    END IF;
END
$$;

-- Header 与 Item 是 Session 创建时一次冻结的 append-only 事实。即使调用方绕过 Repository，
-- 数据库也必须拒绝覆盖或删除，避免历史会话在 Skill 重新发布或运维误操作后发生语义漂移。
CREATE FUNCTION agent.reject_session_skill_snapshot_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'session Skill Snapshot facts are immutable: %.% does not allow %',
        TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP;
END
$$;

COMMENT ON FUNCTION agent.reject_session_skill_snapshot_mutation() IS
    '拒绝修改或删除会话 Skill 冻结 Header/Item，保证历史会话快照 append-only';

CREATE TRIGGER trg_session_skill_snapshot__immutable
    BEFORE UPDATE OR DELETE ON agent.session_skill_snapshot
    FOR EACH ROW
    EXECUTE FUNCTION agent.reject_session_skill_snapshot_mutation();

CREATE TRIGGER trg_session_skill_snapshot_item__immutable
    BEFORE UPDATE OR DELETE ON agent.session_skill_snapshot_item
    FOR EACH ROW
    EXECUTE FUNCTION agent.reject_session_skill_snapshot_mutation();
