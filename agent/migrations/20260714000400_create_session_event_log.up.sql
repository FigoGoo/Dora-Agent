CREATE TABLE agent.session_event_counter (
    session_id uuid NOT NULL,
    last_seq bigint NOT NULL DEFAULT 0,
    min_available_seq bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_session_event_counter PRIMARY KEY (session_id),
    CONSTRAINT ck_session_event_counter__last_seq CHECK (last_seq >= 0),
    CONSTRAINT ck_session_event_counter__min_seq CHECK (
        min_available_seq >= 1 AND min_available_seq <= last_seq + 1
    )
);

COMMENT ON TABLE agent.session_event_counter IS '会话 EventLog 序号与在线保留水位计数器表，追加和保留水位推进均需事务锁行';
COMMENT ON COLUMN agent.session_event_counter.session_id IS '关联 Agent 会话的逻辑外键标识，同时作为事件计数器主键，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_event_counter.last_seq IS '会话最近已提交的 EventLog 单调序号，0 表示尚无事件';
COMMENT ON COLUMN agent.session_event_counter.min_available_seq IS '当前在线可重放的最小事件序号，Cursor 更早时必须要求 Snapshot Reset';
COMMENT ON COLUMN agent.session_event_counter.updated_at IS '事件计数器最近更新时间，使用 UTC 时间';

CREATE TABLE agent.session_event_log (
    event_id uuid NOT NULL,
    session_id uuid NOT NULL,
    seq bigint NOT NULL,
    event_type varchar(64) NOT NULL,
    schema_version varchar(32) NOT NULL,
    source_kind varchar(64) NOT NULL,
    source_id uuid NOT NULL,
    projection_index integer NOT NULL,
    aggregate_type varchar(32) NOT NULL,
    aggregate_id uuid NOT NULL,
    aggregate_version bigint NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pk_session_event_log PRIMARY KEY (event_id),
    CONSTRAINT uq_session_event_log__session_seq UNIQUE (session_id, seq),
    CONSTRAINT uq_session_event_log__source_projection UNIQUE (
        session_id,
        source_kind,
        source_id,
        event_type,
        projection_index
    ),
    CONSTRAINT ck_session_event_log__seq CHECK (seq > 0),
    CONSTRAINT ck_session_event_log__type CHECK (event_type IN ('session.created', 'session.input.accepted')),
    CONSTRAINT ck_session_event_log__schema_version CHECK (schema_version IN ('session.event.v1')),
    CONSTRAINT ck_session_event_log__projection_index CHECK (projection_index >= 0),
    CONSTRAINT ck_session_event_log__aggregate_type CHECK (aggregate_type IN ('session', 'session_input')),
    CONSTRAINT ck_session_event_log__aggregate_version CHECK (aggregate_version > 0),
    CONSTRAINT ck_session_event_log__payload_object CHECK (jsonb_typeof(payload) = 'object')
);

COMMENT ON TABLE agent.session_event_log IS '会话追加式前端投影事件真源表，按 Session 单调序号支持 Snapshot 后补读与 SSE 重连';
COMMENT ON COLUMN agent.session_event_log.event_id IS '事件唯一标识，由 Agent 应用生成 UUIDv7';
COMMENT ON COLUMN agent.session_event_log.session_id IS '关联 Agent 会话的逻辑外键标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_event_log.seq IS '事件在会话内的单调递增序号，从 1 开始且提交后不可复用';
COMMENT ON COLUMN agent.session_event_log.event_type IS '事件类型，W0 仅允许 session.created 与 session.input.accepted';
COMMENT ON COLUMN agent.session_event_log.schema_version IS '前端投影事件 Schema 版本，W0 固定为 session.event.v1';
COMMENT ON COLUMN agent.session_event_log.source_kind IS '事件来源类型，W0 为 ensure_project_session';
COMMENT ON COLUMN agent.session_event_log.source_id IS '事件来源稳定 UUIDv7，用于 AppendOnce 去重，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_event_log.projection_index IS '同一业务来源下投影事件的固定顺序索引，从 0 开始';
COMMENT ON COLUMN agent.session_event_log.aggregate_type IS '事件关联聚合类型：session 或 session_input';
COMMENT ON COLUMN agent.session_event_log.aggregate_id IS '事件关联聚合逻辑标识，不设置数据库物理外键约束';
COMMENT ON COLUMN agent.session_event_log.aggregate_version IS '事件对应的聚合版本，用于拒绝乱序投影回滚';
COMMENT ON COLUMN agent.session_event_log.payload IS '严格版本化的安全前端投影 JSON，不包含 Prompt、Checkpoint、Graph State 或 Provider 原文';
COMMENT ON COLUMN agent.session_event_log.created_at IS '事件创建时间，使用 UTC 时间';

CREATE INDEX idx_session_event_log__aggregate
    ON agent.session_event_log (aggregate_type, aggregate_id, aggregate_version, event_id);
