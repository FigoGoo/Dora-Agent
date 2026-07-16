CREATE TABLE agent.session (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status varchar(16) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz NULL,
    CONSTRAINT pk_session PRIMARY KEY (id),
    CONSTRAINT uq_session__project_id UNIQUE (project_id),
    CONSTRAINT ck_session__status CHECK (status IN ('active', 'archived')),
    CONSTRAINT ck_session__version CHECK (version > 0),
    CONSTRAINT ck_session__archived_at CHECK (
        (status = 'active' AND archived_at IS NULL)
        OR (status = 'archived' AND archived_at IS NOT NULL)
    )
);

COMMENT ON TABLE agent.session IS 'Agent 会话聚合根表，保存一个 Business Project 对应的默认会话生命周期';
COMMENT ON COLUMN agent.session.id IS '会话唯一标识，由 Agent 应用生成 UUIDv7';
COMMENT ON COLUMN agent.session.project_id IS '关联 Business Project 的逻辑外键标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session.user_id IS '关联 Business User 的逻辑外键标识，仅用于可信授权校验，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session.status IS '会话状态：active-可接收输入，archived-已归档';
COMMENT ON COLUMN agent.session.version IS '会话聚合乐观锁版本，从 1 开始单调递增';
COMMENT ON COLUMN agent.session.created_at IS '会话创建时间，使用 UTC 时间';
COMMENT ON COLUMN agent.session.updated_at IS '会话最近更新时间，使用 UTC 时间';
COMMENT ON COLUMN agent.session.archived_at IS '会话归档时间，active 状态为空，使用 UTC 时间';

CREATE INDEX idx_session__user_status_created
    ON agent.session (user_id, status, created_at DESC, id DESC);

CREATE TABLE agent.session_skill_snapshot (
    session_id uuid NOT NULL,
    snapshot_kind varchar(16) NOT NULL,
    snapshot_digest char(64) NOT NULL,
    published_snapshot_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_session_skill_snapshot PRIMARY KEY (session_id),
    CONSTRAINT ck_session_skill_snapshot__kind CHECK (snapshot_kind IN ('empty')),
    CONSTRAINT ck_session_skill_snapshot__digest CHECK (snapshot_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_skill_snapshot__refs_array CHECK (jsonb_typeof(published_snapshot_refs) = 'array'),
    CONSTRAINT ck_session_skill_snapshot__empty_refs CHECK (
        snapshot_kind <> 'empty' OR published_snapshot_refs = '[]'::jsonb
    )
);

COMMENT ON TABLE agent.session_skill_snapshot IS '会话 Skill 冻结快照表，W0 显式记录空快照并保证后续发布不改变既有会话';
COMMENT ON COLUMN agent.session_skill_snapshot.session_id IS '关联 Agent 会话的逻辑外键标识，同时作为快照主键，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_skill_snapshot.snapshot_kind IS '快照类型，W0 仅允许 empty-显式空快照';
COMMENT ON COLUMN agent.session_skill_snapshot.snapshot_digest IS '快照规范化内容的 SHA-256 小写十六进制摘要，不保存 Skill 原文';
COMMENT ON COLUMN agent.session_skill_snapshot.published_snapshot_refs IS '冻结的 Business Published Skill 引用数组，W0 必须为空数组';
COMMENT ON COLUMN agent.session_skill_snapshot.created_at IS 'Skill 快照冻结时间，使用 UTC 时间';

CREATE TABLE agent.session_sequence_counter (
    session_id uuid NOT NULL,
    last_message_seq bigint NOT NULL DEFAULT 0,
    last_input_enqueue_seq bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_session_sequence_counter PRIMARY KEY (session_id),
    CONSTRAINT ck_session_sequence_counter__message_seq CHECK (last_message_seq >= 0),
    CONSTRAINT ck_session_sequence_counter__input_seq CHECK (last_input_enqueue_seq >= 0)
);

COMMENT ON TABLE agent.session_sequence_counter IS '会话 Message 与 Input 序号计数器表，后续追加必须在本地事务中锁行后分配序号';
COMMENT ON COLUMN agent.session_sequence_counter.session_id IS '关联 Agent 会话的逻辑外键标识，同时作为计数器主键，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_sequence_counter.last_message_seq IS '会话最近已提交的 Message 单调序号，0 表示尚无 Message';
COMMENT ON COLUMN agent.session_sequence_counter.last_input_enqueue_seq IS '会话最近已提交的 Input 入队单调序号，0 表示尚无 Input';
COMMENT ON COLUMN agent.session_sequence_counter.updated_at IS '序号计数器最近更新时间，使用 UTC 时间';

CREATE TABLE agent.session_runtime_lease (
    session_id uuid NOT NULL,
    lease_owner varchar(128) NULL,
    lease_until timestamptz NULL,
    fence_token bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_session_runtime_lease PRIMARY KEY (session_id),
    CONSTRAINT ck_session_runtime_lease__owner_until CHECK (
        (lease_owner IS NULL AND lease_until IS NULL)
        OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL)
    ),
    CONSTRAINT ck_session_runtime_lease__fence CHECK (fence_token >= 0),
    CONSTRAINT ck_session_runtime_lease__version CHECK (version > 0)
);

COMMENT ON TABLE agent.session_runtime_lease IS '会话 Runtime 串行处理租约表，W0 仅初始化空租约，后续 Claim 使用 Fence 防并发提交';
COMMENT ON COLUMN agent.session_runtime_lease.session_id IS '关联 Agent 会话的逻辑外键标识，同时作为租约主键，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_runtime_lease.lease_owner IS '当前会话处理实例标识，未持有租约时为空，不包含 Secret';
COMMENT ON COLUMN agent.session_runtime_lease.lease_until IS '当前租约到期时间，未持有租约时为空，使用 UTC 时间';
COMMENT ON COLUMN agent.session_runtime_lease.fence_token IS '会话处理 Fence Token，每次成功领取时单调递增，0 表示从未领取';
COMMENT ON COLUMN agent.session_runtime_lease.version IS '租约记录乐观锁版本，从 1 开始单调递增';
COMMENT ON COLUMN agent.session_runtime_lease.updated_at IS '租约记录最近更新时间，使用 UTC 时间';
