CREATE TABLE agent.session_message (
    id uuid NOT NULL,
    session_id uuid NOT NULL,
    message_seq bigint NOT NULL,
    role varchar(16) NOT NULL,
    content_ciphertext bytea NOT NULL,
    content_key_version varchar(64) NOT NULL,
    content_digest char(64) NOT NULL,
    source_kind varchar(32) NOT NULL,
    source_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_session_message PRIMARY KEY (id),
    CONSTRAINT uq_session_message__session_seq UNIQUE (session_id, message_seq),
    CONSTRAINT uq_session_message__session_source UNIQUE (session_id, source_kind, source_id),
    CONSTRAINT ck_session_message__message_seq CHECK (message_seq > 0),
    CONSTRAINT ck_session_message__role CHECK (role IN ('user')),
    CONSTRAINT ck_session_message__aead_envelope CHECK (
        CASE
            WHEN octet_length(content_ciphertext) < 36 THEN false
            ELSE
                substring(content_ciphertext FROM 1 FOR 4) = decode('44524145', 'hex') AND
                get_byte(content_ciphertext, 4) = 1 AND
                get_byte(content_ciphertext, 5) = 1 AND
                get_byte(content_ciphertext, 6) = 12
        END
    ),
    CONSTRAINT ck_session_message__key_version CHECK (length(content_key_version) > 0),
    CONSTRAINT ck_session_message__digest CHECK (content_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_message__source_kind CHECK (source_kind IN ('ensure_project_session'))
);

COMMENT ON TABLE agent.session_message IS '会话追加式消息表，正文仅保存受保护密文，W0 由 Ensure 命令创建首条用户消息';
COMMENT ON COLUMN agent.session_message.id IS '消息唯一标识，由 Agent 应用生成 UUIDv7';
COMMENT ON COLUMN agent.session_message.session_id IS '关联 Agent 会话的逻辑外键标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_message.message_seq IS '消息在会话内的单调递增序号，从 1 开始且提交后不可复用';
COMMENT ON COLUMN agent.session_message.role IS '消息角色，W0 只允许 user；assistant、tool、system 必须经后续设计和前向 Migration 扩展';
COMMENT ON COLUMN agent.session_message.content_ciphertext IS 'W0 二进制 AEAD Envelope：DRAE magic、version=1、algorithm=1(AES-256-GCM)、nonce_length=12、12 字节 Nonce、至少 1 字节密文和 16 字节认证标签；禁止保存裸密文或进入普通日志、Trace、Receipt、Event';
COMMENT ON COLUMN agent.session_message.content_key_version IS 'AEAD Envelope 使用的独立密钥版本引用，不保存密钥材料，也不替代 Envelope 内的算法和 Nonce';
COMMENT ON COLUMN agent.session_message.content_digest IS '消息原文 SHA-256 小写十六进制摘要，用于幂等核对，不可用于还原正文';
COMMENT ON COLUMN agent.session_message.source_kind IS '消息来源类型，W0 为 ensure_project_session';
COMMENT ON COLUMN agent.session_message.source_id IS '消息来源稳定标识，W0 关联 Business Command UUIDv7，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_message.created_at IS '消息创建时间，使用 UTC 时间';

CREATE INDEX idx_session_message__session_created
    ON agent.session_message (session_id, created_at, id);

CREATE TABLE agent.session_input (
    id uuid NOT NULL,
    session_id uuid NOT NULL,
    source_type varchar(48) NOT NULL,
    source_id uuid NOT NULL,
    message_id uuid NULL,
    status varchar(24) NOT NULL,
    enqueue_seq bigint NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL,
    lease_owner varchar(128) NULL,
    lease_until timestamptz NULL,
    fence_token bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_session_input PRIMARY KEY (id),
    CONSTRAINT uq_session_input__session_seq UNIQUE (session_id, enqueue_seq),
    CONSTRAINT uq_session_input__session_source UNIQUE (session_id, source_type, source_id),
    CONSTRAINT uq_session_input__message UNIQUE (message_id),
    CONSTRAINT ck_session_input__source_type CHECK (source_type IN ('user_message')),
    CONSTRAINT ck_session_input__status CHECK (status IN (
        'pending',
        'claimed',
        'running',
        'retry_wait',
        'resolved',
        'dead'
    )),
    CONSTRAINT ck_session_input__enqueue_seq CHECK (enqueue_seq > 0),
    CONSTRAINT ck_session_input__attempts CHECK (attempts >= 0),
    CONSTRAINT ck_session_input__owner_until CHECK (
        (lease_owner IS NULL AND lease_until IS NULL)
        OR (lease_owner IS NOT NULL AND lease_until IS NOT NULL)
    ),
    CONSTRAINT ck_session_input__fence CHECK (fence_token >= 0),
    CONSTRAINT ck_session_input__w0_pending_lease CHECK (
        status <> 'pending' OR (lease_owner IS NULL AND lease_until IS NULL)
    )
);

COMMENT ON TABLE agent.session_input IS '会话持久化输入表，PostgreSQL 为待处理与恢复状态真源，W0 只创建 pending 输入';
COMMENT ON COLUMN agent.session_input.id IS '输入唯一标识，由 Agent 应用生成 UUIDv7，同一业务输入技术重试复用原标识';
COMMENT ON COLUMN agent.session_input.session_id IS '关联 Agent 会话的逻辑外键标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_input.source_type IS '可信输入来源类型，W0 只允许 user_message；恢复、审批和批次继续结果必须经后续设计和前向 Migration 扩展';
COMMENT ON COLUMN agent.session_input.source_id IS '来源系统稳定幂等标识，重复投递不得创建第二个有效输入';
COMMENT ON COLUMN agent.session_input.message_id IS '关联首条用户 Message 的逻辑外键标识，非用户消息输入可为空，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_input.status IS '输入状态：pending、claimed、running、retry_wait、resolved 或 dead；W0 仅写 pending';
COMMENT ON COLUMN agent.session_input.enqueue_seq IS '输入在会话内的单调入队序号，从 1 开始并用于 Head-of-Line 处理';
COMMENT ON COLUMN agent.session_input.attempts IS '输入已开始的处理尝试次数，W0 固定为 0';
COMMENT ON COLUMN agent.session_input.available_at IS '输入最早可领取时间，使用 UTC 时间';
COMMENT ON COLUMN agent.session_input.lease_owner IS '当前输入领取实例标识，未领取时为空，不包含 Secret';
COMMENT ON COLUMN agent.session_input.lease_until IS '当前输入领取到期时间，未领取时为空，使用 UTC 时间';
COMMENT ON COLUMN agent.session_input.fence_token IS '输入 Claim Fence Token，防止过期处理者提交结果，W0 固定为 0';
COMMENT ON COLUMN agent.session_input.created_at IS '输入创建时间，使用 UTC 时间';
COMMENT ON COLUMN agent.session_input.updated_at IS '输入最近更新时间，使用 UTC 时间';

CREATE INDEX idx_session_input__claim
    ON agent.session_input (status, available_at, session_id, enqueue_seq)
    WHERE status IN ('pending', 'retry_wait');

CREATE TABLE agent.session_command_receipt (
    command_id uuid NOT NULL,
    command_type varchar(64) NOT NULL,
    request_digest char(64) NOT NULL,
    session_id uuid NOT NULL,
    message_id uuid NULL,
    input_id uuid NULL,
    result_version integer NOT NULL,
    completed_at timestamptz NOT NULL,
    CONSTRAINT pk_session_command_receipt PRIMARY KEY (command_id),
    CONSTRAINT ck_session_command_receipt__type CHECK (command_type IN ('ensure_project_session_v1')),
    CONSTRAINT ck_session_command_receipt__digest CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_session_command_receipt__message_input_pair CHECK (
        (message_id IS NULL AND input_id IS NULL)
        OR (message_id IS NOT NULL AND input_id IS NOT NULL)
    ),
    CONSTRAINT ck_session_command_receipt__result_version CHECK (result_version > 0)
);

COMMENT ON TABLE agent.session_command_receipt IS 'Session 命令 first-write-wins 回执表，用于同键重放、同键异义冲突和 Unknown Outcome 核对';
COMMENT ON COLUMN agent.session_command_receipt.command_id IS 'Business 命令 UUIDv7，同时作为回执主键，同一技术重试必须复用';
COMMENT ON COLUMN agent.session_command_receipt.command_type IS '稳定命令类型，W0 为 ensure_project_session_v1';
COMMENT ON COLUMN agent.session_command_receipt.request_digest IS 'Agent 按冻结 Canonical Schema 独立重算的请求语义 SHA-256 摘要';
COMMENT ON COLUMN agent.session_command_receipt.session_id IS '命令创建的 Agent 会话逻辑引用，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_command_receipt.message_id IS '非空 Prompt 创建的首条消息逻辑引用，空 Prompt 时为空，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_command_receipt.input_id IS '非空 Prompt 创建的待处理输入逻辑引用，空 Prompt 时为空，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_command_receipt.result_version IS '冻结结果结构版本，W0 固定为 1';
COMMENT ON COLUMN agent.session_command_receipt.completed_at IS 'Agent 本地事务成功提交所使用的冻结 UTC 时间';

CREATE INDEX idx_session_command_receipt__session
    ON agent.session_command_receipt (session_id, completed_at DESC);
