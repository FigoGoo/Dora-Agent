CREATE TABLE business.user_account (
    id uuid NOT NULL,
    user_type varchar(32) NOT NULL,
    status varchar(32) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_user_account PRIMARY KEY (id),
    CONSTRAINT ck_user_account__user_type CHECK (user_type IN ('personal', 'enterprise')),
    CONSTRAINT ck_user_account__status CHECK (status IN ('active', 'disabled', 'cancelled')),
    CONSTRAINT ck_user_account__version CHECK (version >= 1)
);

COMMENT ON TABLE business.user_account IS '用户账户表，保存平台个人用户和企业用户的基础状态';
COMMENT ON COLUMN business.user_account.id IS '用户唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.user_account.user_type IS '用户类型：personal-个人用户，enterprise-企业用户';
COMMENT ON COLUMN business.user_account.status IS '用户状态：active-正常，disabled-禁用，cancelled-已注销';
COMMENT ON COLUMN business.user_account.version IS '用户聚合并发版本，从 1 开始并用于乐观并发控制';
COMMENT ON COLUMN business.user_account.created_at IS '用户账户创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.user_account.updated_at IS '用户账户最近更新时间，使用 UTC 时间';

CREATE INDEX idx_user_account__status_created_id
    ON business.user_account (status, created_at DESC, id DESC);

CREATE TABLE business.user_login_identity (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    identity_type varchar(32) NOT NULL,
    normalized_identifier varchar(320) NOT NULL,
    verified boolean NOT NULL DEFAULT false,
    verified_at timestamptz NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_user_login_identity PRIMARY KEY (id),
    CONSTRAINT uq_user_login_identity__type_identifier UNIQUE (identity_type, normalized_identifier),
    CONSTRAINT ck_user_login_identity__type CHECK (identity_type = 'email'),
    CONSTRAINT ck_user_login_identity__normalized_identifier CHECK (length(normalized_identifier) BETWEEN 1 AND 320),
    CONSTRAINT ck_user_login_identity__verified_at CHECK (
        (verified = false AND verified_at IS NULL) OR
        (verified = true AND verified_at IS NOT NULL)
    )
);

COMMENT ON TABLE business.user_login_identity IS '用户登录身份表，W0 保存标准化后的邮箱登录标识';
COMMENT ON COLUMN business.user_login_identity.id IS '登录身份唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.user_login_identity.user_id IS '所属用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.user_login_identity.identity_type IS '登录身份类型，W0 仅允许 email；其他类型必须通过后续 Migration 扩展';
COMMENT ON COLUMN business.user_login_identity.normalized_identifier IS '规范化登录标识，用于唯一匹配，普通日志不得记录完整值';
COMMENT ON COLUMN business.user_login_identity.verified IS '登录身份是否已通过所有权验证';
COMMENT ON COLUMN business.user_login_identity.verified_at IS '登录身份完成验证的时间，未验证时为空，使用 UTC 时间';
COMMENT ON COLUMN business.user_login_identity.created_at IS '登录身份创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.user_login_identity.updated_at IS '登录身份最近更新时间，使用 UTC 时间';

CREATE INDEX idx_user_login_identity__user_id
    ON business.user_login_identity (user_id, id);

CREATE TABLE business.user_password_credential (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    algorithm varchar(32) NOT NULL,
    memory_kib integer NOT NULL,
    iterations integer NOT NULL,
    parallelism smallint NOT NULL,
    salt bytea NOT NULL,
    password_hash bytea NOT NULL,
    credential_version bigint NOT NULL DEFAULT 1,
    password_changed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_user_password_credential PRIMARY KEY (id),
    CONSTRAINT uq_user_password_credential__user_id UNIQUE (user_id),
    CONSTRAINT ck_user_password_credential__algorithm CHECK (algorithm = 'argon2id'),
    CONSTRAINT ck_user_password_credential__memory_kib CHECK (memory_kib > 0),
    CONSTRAINT ck_user_password_credential__iterations CHECK (iterations > 0),
    CONSTRAINT ck_user_password_credential__parallelism CHECK (parallelism > 0),
    CONSTRAINT ck_user_password_credential__salt CHECK (octet_length(salt) >= 16),
    CONSTRAINT ck_user_password_credential__hash CHECK (octet_length(password_hash) >= 16),
    CONSTRAINT ck_user_password_credential__version CHECK (credential_version >= 1)
);

COMMENT ON TABLE business.user_password_credential IS '用户密码凭证表，保存 Argon2id 参数、盐和不可逆密码摘要';
COMMENT ON COLUMN business.user_password_credential.id IS '密码凭证唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.user_password_credential.user_id IS '所属用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.user_password_credential.algorithm IS '密码摘要算法，W0 固定为 argon2id';
COMMENT ON COLUMN business.user_password_credential.memory_kib IS 'Argon2id 单次计算使用的内存参数，单位为 KiB';
COMMENT ON COLUMN business.user_password_credential.iterations IS 'Argon2id 迭代次数参数';
COMMENT ON COLUMN business.user_password_credential.parallelism IS 'Argon2id 并行度参数';
COMMENT ON COLUMN business.user_password_credential.salt IS '密码摘要随机盐，不得进入普通日志、Trace 或前端 DTO';
COMMENT ON COLUMN business.user_password_credential.password_hash IS '不可逆密码摘要，不得进入普通日志、Trace 或前端 DTO';
COMMENT ON COLUMN business.user_password_credential.credential_version IS '密码凭证版本，用于参数升级和会话撤销判断';
COMMENT ON COLUMN business.user_password_credential.password_changed_at IS '最近一次密码变更时间，使用 UTC 时间';
COMMENT ON COLUMN business.user_password_credential.created_at IS '密码凭证创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.user_password_credential.updated_at IS '密码凭证最近更新时间，使用 UTC 时间';

CREATE TABLE business.auth_web_session (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    token_digest bytea NOT NULL,
    csrf_token_digest bytea NOT NULL,
    status varchar(32) NOT NULL,
    session_version bigint NOT NULL DEFAULT 1,
    last_seen_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at timestamptz NULL,
    revoke_reason varchar(128) NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_auth_web_session PRIMARY KEY (id),
    CONSTRAINT uq_auth_web_session__token_digest UNIQUE (token_digest),
    CONSTRAINT ck_auth_web_session__token_digest CHECK (octet_length(token_digest) = 32),
    CONSTRAINT ck_auth_web_session__csrf_token_digest CHECK (octet_length(csrf_token_digest) = 32),
    CONSTRAINT ck_auth_web_session__token_digest_nonzero CHECK (token_digest <> decode(repeat('00', 32), 'hex')),
    CONSTRAINT ck_auth_web_session__csrf_token_digest_nonzero CHECK (csrf_token_digest <> decode(repeat('00', 32), 'hex')),
    CONSTRAINT ck_auth_web_session__status CHECK (status IN ('active', 'revoked', 'expired')),
    CONSTRAINT ck_auth_web_session__version CHECK (session_version >= 1),
    CONSTRAINT ck_auth_web_session__expiry CHECK (
        last_seen_at <= idle_expires_at AND idle_expires_at <= absolute_expires_at
    ),
    CONSTRAINT ck_auth_web_session__revocation CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL AND revoke_reason IS NOT NULL) OR
        (status <> 'revoked' AND revoked_at IS NULL AND revoke_reason IS NULL)
    )
);

COMMENT ON TABLE business.auth_web_session IS '浏览器安全会话表，保存不透明会话令牌与防跨站令牌的摘要和撤销状态';
COMMENT ON COLUMN business.auth_web_session.id IS 'Web 会话唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.auth_web_session.user_id IS '会话所属用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.auth_web_session.token_digest IS '随机不透明会话令牌的 SHA-256 摘要，数据库不保存原始令牌';
COMMENT ON COLUMN business.auth_web_session.csrf_token_digest IS '会话绑定防跨站令牌的 SHA-256 摘要，数据库不保存原始令牌';
COMMENT ON COLUMN business.auth_web_session.status IS '会话状态：active-有效，revoked-已撤销，expired-已过期';
COMMENT ON COLUMN business.auth_web_session.session_version IS '会话版本，用于撤销与内部身份断言失效判断';
COMMENT ON COLUMN business.auth_web_session.last_seen_at IS '会话最近一次有效访问时间，使用 UTC 时间';
COMMENT ON COLUMN business.auth_web_session.idle_expires_at IS '会话空闲过期时间，使用 UTC 时间';
COMMENT ON COLUMN business.auth_web_session.absolute_expires_at IS '会话绝对过期时间，使用 UTC 时间';
COMMENT ON COLUMN business.auth_web_session.revoked_at IS '会话撤销时间，仅 revoked 状态非空，使用 UTC 时间';
COMMENT ON COLUMN business.auth_web_session.revoke_reason IS '会话撤销稳定原因代码，不保存用户输入或敏感信息';
COMMENT ON COLUMN business.auth_web_session.created_at IS 'Web 会话创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.auth_web_session.updated_at IS 'Web 会话最近更新时间，使用 UTC 时间';

CREATE INDEX idx_auth_web_session__user_status_id
    ON business.auth_web_session (user_id, status, id);
CREATE INDEX idx_auth_web_session__cleanup
    ON business.auth_web_session (status, idle_expires_at, id);

CREATE TABLE business.project (
    id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    title varchar(160) NOT NULL,
    lifecycle_status varchar(32) NOT NULL,
    recent_run_status varchar(32) NOT NULL,
    initial_prompt_status varchar(32) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_project PRIMARY KEY (id),
    CONSTRAINT ck_project__title CHECK (length(title) BETWEEN 1 AND 160),
    CONSTRAINT ck_project__lifecycle_status CHECK (lifecycle_status IN ('active', 'archived', 'trash', 'deleted')),
    CONSTRAINT ck_project__recent_run_status CHECK (
        recent_run_status IN ('idle', 'queued', 'running', 'waiting_user', 'waiting_async', 'succeeded', 'partial_failed', 'failed', 'cancelled')
    ),
    CONSTRAINT ck_project__initial_prompt_status CHECK (initial_prompt_status IN ('absent', 'pending', 'accepted', 'failed')),
    CONSTRAINT ck_project__version CHECK (version >= 1)
);

COMMENT ON TABLE business.project IS '项目表，保存用户创作项目的所有权、生命周期和最近运行摘要';
COMMENT ON COLUMN business.project.id IS '项目唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project.owner_user_id IS '项目所有者用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project.title IS '项目标题，不保存完整首提示词，普通日志不得记录用户敏感内容';
COMMENT ON COLUMN business.project.lifecycle_status IS '项目生命周期：active-活跃，archived-归档，trash-回收站，deleted-已删除';
COMMENT ON COLUMN business.project.recent_run_status IS '最近运行摘要：idle、queued、running、waiting_user、waiting_async、succeeded、partial_failed、failed 或 cancelled';
COMMENT ON COLUMN business.project.initial_prompt_status IS '首提示词状态：absent-不存在，pending-待 Agent 接收，accepted-已接收，failed-处理失败';
COMMENT ON COLUMN business.project.version IS '项目聚合并发版本，从 1 开始并用于乐观并发控制';
COMMENT ON COLUMN business.project.created_at IS '项目创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.project.updated_at IS '项目最近更新时间，使用 UTC 时间';

CREATE INDEX idx_project__owner_lifecycle_updated_id
    ON business.project (owner_user_id, lifecycle_status, updated_at DESC, id DESC);

CREATE TABLE business.project_creation_receipt (
    id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    command_type varchar(64) NOT NULL,
    key_digest bytea NOT NULL,
    semantic_digest bytea NOT NULL,
    project_id uuid NOT NULL,
    lifecycle_status varchar(32) NOT NULL,
    recent_run_status varchar(32) NOT NULL,
    session_provisioning_status varchar(32) NOT NULL,
    initial_prompt_status varchar(32) NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_project_creation_receipt PRIMARY KEY (id),
    CONSTRAINT uq_project_creation_receipt__scope UNIQUE (owner_user_id, command_type, key_digest),
    CONSTRAINT ck_project_creation_receipt__command_type CHECK (command_type = 'quick_create'),
    CONSTRAINT ck_project_creation_receipt__key_digest CHECK (octet_length(key_digest) = 32),
    CONSTRAINT ck_project_creation_receipt__semantic_digest CHECK (octet_length(semantic_digest) = 32),
    CONSTRAINT ck_project_creation_receipt__lifecycle_status CHECK (lifecycle_status = 'active'),
    CONSTRAINT ck_project_creation_receipt__recent_run_status CHECK (recent_run_status IN ('idle', 'queued')),
    CONSTRAINT ck_project_creation_receipt__provisioning_status CHECK (session_provisioning_status = 'pending'),
    CONSTRAINT ck_project_creation_receipt__initial_prompt_status CHECK (initial_prompt_status IN ('absent', 'pending'))
);

COMMENT ON TABLE business.project_creation_receipt IS '项目创建幂等回执表，保存快速创建首次提交的安全响应快照';
COMMENT ON COLUMN business.project_creation_receipt.id IS '创建回执唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_creation_receipt.owner_user_id IS '发起创建的可信用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_creation_receipt.command_type IS '幂等命令类型，W0 固定为 quick_create';
COMMENT ON COLUMN business.project_creation_receipt.key_digest IS '客户端幂等键的 SHA-256 摘要，不保存原始幂等键';
COMMENT ON COLUMN business.project_creation_receipt.semantic_digest IS '规范化快速创建语义的 SHA-256 摘要，用于同键异义冲突判断';
COMMENT ON COLUMN business.project_creation_receipt.project_id IS '首次创建的项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_creation_receipt.lifecycle_status IS '首次响应中的项目生命周期快照，W0 固定为 active';
COMMENT ON COLUMN business.project_creation_receipt.recent_run_status IS '首次响应中的最近运行摘要，空提示词为 idle，非空提示词为 queued';
COMMENT ON COLUMN business.project_creation_receipt.session_provisioning_status IS '首次响应中的 Session 初始化状态，W0 固定为 pending';
COMMENT ON COLUMN business.project_creation_receipt.initial_prompt_status IS '首次响应中的首提示词状态：absent-不存在，pending-待接收';
COMMENT ON COLUMN business.project_creation_receipt.created_at IS '首次创建命令提交时间，使用 UTC 时间';

CREATE INDEX idx_project_creation_receipt__project_id
    ON business.project_creation_receipt (project_id);

CREATE TABLE business.project_session_binding (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    command_id uuid NOT NULL,
    request_digest bytea NOT NULL,
    agent_session_id uuid NULL,
    agent_input_id uuid NULL,
    provisioning_status varchar(32) NOT NULL,
    last_error_code varchar(128) NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_project_session_binding PRIMARY KEY (id),
    CONSTRAINT uq_project_session_binding__project_id UNIQUE (project_id),
    CONSTRAINT uq_project_session_binding__command_id UNIQUE (command_id),
    CONSTRAINT ck_project_session_binding__request_digest CHECK (octet_length(request_digest) = 32),
    CONSTRAINT ck_project_session_binding__provisioning_status CHECK (provisioning_status IN ('pending', 'reconciling', 'ready', 'blocked')),
    CONSTRAINT ck_project_session_binding__ready_session CHECK (
        (provisioning_status = 'ready' AND agent_session_id IS NOT NULL) OR
        (provisioning_status <> 'ready')
    ),
    CONSTRAINT ck_project_session_binding__version CHECK (version >= 1)
);

COMMENT ON TABLE business.project_session_binding IS '项目与 Agent 默认 Session 的初始化绑定表，保存跨数据库最终一致状态';
COMMENT ON COLUMN business.project_session_binding.id IS 'Session 绑定唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_session_binding.project_id IS '所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_binding.command_id IS 'Agent Session 初始化命令标识，与同事务 Outbox 标识一致';
COMMENT ON COLUMN business.project_session_binding.request_digest IS '按 ensure_project_session.v1 Canonical Schema 计算的请求摘要，与 QuickCreate HTTP 语义摘要相互独立';
COMMENT ON COLUMN business.project_session_binding.agent_session_id IS 'Agent Module 权威 Session 标识，跨数据库逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_binding.agent_input_id IS 'Agent Module 权威首 Input 标识，空提示词时为空，跨数据库逻辑关联且不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_binding.provisioning_status IS '初始化状态：pending-待派发，reconciling-核对未知结果，ready-已就绪，blocked-需恢复';
COMMENT ON COLUMN business.project_session_binding.last_error_code IS '最近一次稳定错误代码，不保存内部堆栈、地址或用户内容';
COMMENT ON COLUMN business.project_session_binding.version IS '绑定并发版本，从 1 开始并用于状态条件更新';
COMMENT ON COLUMN business.project_session_binding.created_at IS '绑定创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.project_session_binding.updated_at IS '绑定最近更新时间，使用 UTC 时间';

CREATE UNIQUE INDEX uq_project_session_binding__agent_session_id
    ON business.project_session_binding (agent_session_id)
    WHERE agent_session_id IS NOT NULL;
CREATE INDEX idx_project_session_binding__status_updated_id
    ON business.project_session_binding (provisioning_status, updated_at, id);

CREATE TABLE business.project_session_outbox (
    id uuid NOT NULL,
    event_type varchar(128) NOT NULL,
    schema_version varchar(64) NOT NULL,
    aggregate_id uuid NOT NULL,
    owner_user_id uuid NOT NULL,
    request_digest bytea NOT NULL,
    has_initial_prompt boolean NOT NULL,
    payload_encryption_algorithm varchar(32) NULL,
    payload_key_version varchar(64) NULL,
    payload_nonce bytea NULL,
    payload_ciphertext bytea NULL,
    payload_digest bytea NULL,
    payload_cleared_at timestamptz NULL,
    status varchar(32) NOT NULL,
    available_at timestamptz NOT NULL,
    lease_owner varchar(128) NULL,
    lease_version bigint NOT NULL DEFAULT 0,
    lease_expires_at timestamptz NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL,
    delivered_at timestamptz NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_project_session_outbox PRIMARY KEY (id),
    CONSTRAINT uq_project_session_outbox__aggregate UNIQUE (event_type, aggregate_id),
    CONSTRAINT ck_project_session_outbox__event_type CHECK (event_type = 'agent.session.ensure'),
    CONSTRAINT ck_project_session_outbox__schema_version CHECK (schema_version = 'agent.session-bootstrap.v1'),
    CONSTRAINT ck_project_session_outbox__request_digest CHECK (octet_length(request_digest) = 32),
    CONSTRAINT ck_project_session_outbox__encrypted_payload CHECK (
        (
            has_initial_prompt = false AND
            payload_encryption_algorithm IS NULL AND payload_key_version IS NULL AND
            payload_nonce IS NULL AND payload_ciphertext IS NULL AND payload_digest IS NULL AND
            payload_cleared_at IS NULL
        ) OR
        (
            has_initial_prompt = true AND
            payload_encryption_algorithm IN ('aes-256-gcm') AND
            payload_key_version IS NOT NULL AND length(payload_key_version) BETWEEN 1 AND 64 AND
            payload_nonce IS NOT NULL AND octet_length(payload_nonce) = 12 AND
            payload_ciphertext IS NOT NULL AND octet_length(payload_ciphertext) > 16 AND
            payload_digest IS NOT NULL AND octet_length(payload_digest) = 32 AND
            payload_cleared_at IS NULL
        ) OR
        (
            has_initial_prompt = true AND status = 'delivered' AND
            payload_encryption_algorithm IS NULL AND payload_key_version IS NULL AND
            payload_nonce IS NULL AND payload_ciphertext IS NULL AND
            payload_digest IS NOT NULL AND octet_length(payload_digest) = 32 AND
            payload_cleared_at IS NOT NULL AND delivered_at IS NOT NULL AND
            payload_cleared_at >= delivered_at
        )
    ),
    CONSTRAINT ck_project_session_outbox__status CHECK (status IN ('pending', 'processing', 'retry', 'delivered', 'dead')),
    CONSTRAINT ck_project_session_outbox__lease_version CHECK (lease_version >= 0),
    CONSTRAINT ck_project_session_outbox__attempt_count CHECK (attempt_count >= 0),
    CONSTRAINT ck_project_session_outbox__max_attempts CHECK (max_attempts > 0),
    CONSTRAINT ck_project_session_outbox__delivery CHECK (
        (status = 'delivered' AND delivered_at IS NOT NULL) OR
        (status <> 'delivered' AND delivered_at IS NULL)
    ),
    CONSTRAINT ck_project_session_outbox__lease CHECK (
        (status = 'processing' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL) OR
        (status <> 'processing' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    )
);

COMMENT ON TABLE business.project_session_outbox IS '项目 Session 初始化命令 Outbox，和 Project 创建事实处于同一事务并支持可靠派发';
COMMENT ON COLUMN business.project_session_outbox.id IS 'Outbox 事件及 Agent command_id，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.project_session_outbox.event_type IS '事件类型，W0 固定为 agent.session.ensure';
COMMENT ON COLUMN business.project_session_outbox.schema_version IS '事件负载契约版本，W0 固定为 agent.session-bootstrap.v1';
COMMENT ON COLUMN business.project_session_outbox.aggregate_id IS '所属项目标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.project_session_outbox.owner_user_id IS '命令冻结的可信项目所有者用户标识，不接受客户端覆盖';
COMMENT ON COLUMN business.project_session_outbox.request_digest IS '按 ensure_project_session.v1 Canonical Schema 计算的请求摘要，Agent 必须独立重算核对且不包含 command_id 或时间';
COMMENT ON COLUMN business.project_session_outbox.has_initial_prompt IS '命令是否包含首提示词；false 时所有加密负载字段必须为空';
COMMENT ON COLUMN business.project_session_outbox.payload_encryption_algorithm IS '首提示词负载加密算法，当前只允许 aes-256-gcm，不保存明文';
COMMENT ON COLUMN business.project_session_outbox.payload_key_version IS '首提示词负载加密密钥版本，密钥本身不得写入数据库或日志';
COMMENT ON COLUMN business.project_session_outbox.payload_nonce IS '首提示词负载加密随机数，不得复用且不得进入普通日志';
COMMENT ON COLUMN business.project_session_outbox.payload_ciphertext IS '首提示词加密密文，禁止存放明文并按交付后保留策略清理';
COMMENT ON COLUMN business.project_session_outbox.payload_digest IS '首提示词规范化明文的 SHA-256 摘要，仅用于完整性核对且不得反向恢复正文';
COMMENT ON COLUMN business.project_session_outbox.payload_cleared_at IS 'Agent Receipt 确认后清除首提示词密文的时间；仅 delivered 状态可非空，使用 UTC 时间且不得早于 delivered_at';
COMMENT ON COLUMN business.project_session_outbox.status IS '派发状态：pending-待派发，processing-处理中，retry-待重试，delivered-已交付，dead-终止';
COMMENT ON COLUMN business.project_session_outbox.available_at IS '命令允许被 Dispatcher 领取的最早时间，使用 UTC 时间';
COMMENT ON COLUMN business.project_session_outbox.lease_owner IS '当前短租约 Owner 标识，仅 processing 状态非空';
COMMENT ON COLUMN business.project_session_outbox.lease_version IS '租约 Fencing 版本，每次成功领取递增';
COMMENT ON COLUMN business.project_session_outbox.lease_expires_at IS '当前短租约过期时间，仅 processing 状态非空，使用 UTC 时间';
COMMENT ON COLUMN business.project_session_outbox.attempt_count IS '已经开始的派发尝试次数';
COMMENT ON COLUMN business.project_session_outbox.max_attempts IS '允许的最大派发尝试次数，由版本化配置冻结';
COMMENT ON COLUMN business.project_session_outbox.delivered_at IS 'Agent 确认命令回执的时间，仅 delivered 状态非空，使用 UTC 时间';
COMMENT ON COLUMN business.project_session_outbox.created_at IS 'Outbox 创建时间，使用 UTC 时间';
COMMENT ON COLUMN business.project_session_outbox.updated_at IS 'Outbox 最近更新时间，使用 UTC 时间';

CREATE INDEX idx_project_session_outbox__claim
    ON business.project_session_outbox (status, available_at, id);
CREATE INDEX idx_project_session_outbox__aggregate_id
    ON business.project_session_outbox (aggregate_id);
