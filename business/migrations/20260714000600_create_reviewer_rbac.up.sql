CREATE TABLE business.user_role_assignment (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    role_key varchar(64) NOT NULL,
    status varchar(32) NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    assigned_by_user_id uuid NOT NULL,
    assignment_reason_code varchar(128) NOT NULL,
    approval_reference varchar(160) NOT NULL,
    assigned_at timestamptz NOT NULL,
    revoked_by_user_id uuid NULL,
    revoke_reason_code varchar(128) NULL,
    revocation_approval_reference varchar(160) NULL,
    revoked_at timestamptz NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT pk_user_role_assignment PRIMARY KEY (id),
    CONSTRAINT ck_user_role_assignment__role CHECK (role_key = 'skill_reviewer'),
    CONSTRAINT ck_user_role_assignment__status CHECK (status IN ('active', 'revoked')),
    CONSTRAINT ck_user_role_assignment__version CHECK (version >= 1),
    CONSTRAINT ck_user_role_assignment__grant_actor CHECK (assigned_by_user_id <> user_id),
    CONSTRAINT ck_user_role_assignment__reason CHECK (
        length(assignment_reason_code) BETWEEN 1 AND 128
    ),
    CONSTRAINT ck_user_role_assignment__approval_reference CHECK (
        length(approval_reference) BETWEEN 1 AND 160
    ),
    CONSTRAINT ck_user_role_assignment__time CHECK (
        assigned_at <= updated_at AND
        (revoked_at IS NULL OR revoked_at >= assigned_at)
    ),
    CONSTRAINT ck_user_role_assignment__revocation CHECK (
        (
            status = 'active' AND
            revoked_by_user_id IS NULL AND
            revoke_reason_code IS NULL AND
            revocation_approval_reference IS NULL AND
            revoked_at IS NULL
        ) OR
        (
            status = 'revoked' AND
            revoked_by_user_id IS NOT NULL AND
            revoked_by_user_id <> user_id AND
            revoke_reason_code IS NOT NULL AND
            length(revoke_reason_code) BETWEEN 1 AND 128 AND
            revocation_approval_reference IS NOT NULL AND
            length(revocation_approval_reference) BETWEEN 1 AND 160 AND
            revoked_at IS NOT NULL
        )
    )
);

COMMENT ON TABLE business.user_role_assignment IS '用户角色分配表，保存最小 Reviewer RBAC 的授予、撤销和审批审计事实';
COMMENT ON COLUMN business.user_role_assignment.id IS '角色分配唯一标识，由 Business 应用生成 UUIDv7';
COMMENT ON COLUMN business.user_role_assignment.user_id IS '被分配角色的用户标识，为 Business 内逻辑关联，不设置数据库物理外键约束';
COMMENT ON COLUMN business.user_role_assignment.role_key IS '角色稳定键，W1-C2 固定为 skill_reviewer';
COMMENT ON COLUMN business.user_role_assignment.status IS '角色分配状态：active-生效，revoked-已撤销';
COMMENT ON COLUMN business.user_role_assignment.version IS '角色分配并发版本，从 1 开始且撤销时递增';
COMMENT ON COLUMN business.user_role_assignment.assigned_by_user_id IS '执行授予的独立 active 操作人用户标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.user_role_assignment.assignment_reason_code IS '授予角色的稳定安全原因代码，不保存自由文本或敏感信息';
COMMENT ON COLUMN business.user_role_assignment.approval_reference IS '授予操作对应的外部部署审批或工单稳定引用，不保存 Secret';
COMMENT ON COLUMN business.user_role_assignment.assigned_at IS '角色首次授予时间，使用 UTC 时间';
COMMENT ON COLUMN business.user_role_assignment.revoked_by_user_id IS '执行撤权的独立 active 操作人用户标识，仅 revoked 状态存在且不设置数据库物理外键约束';
COMMENT ON COLUMN business.user_role_assignment.revoke_reason_code IS '撤销角色的稳定安全原因代码，仅 revoked 状态存在';
COMMENT ON COLUMN business.user_role_assignment.revocation_approval_reference IS '撤权操作对应的外部部署审批或工单稳定引用，仅 revoked 状态存在且不保存 Secret';
COMMENT ON COLUMN business.user_role_assignment.revoked_at IS '角色撤销时间，仅 revoked 状态存在并使用 UTC 时间';
COMMENT ON COLUMN business.user_role_assignment.updated_at IS '角色分配最近状态更新时间，使用 UTC 时间';

CREATE UNIQUE INDEX uq_user_role_assignment__active_user_role
    ON business.user_role_assignment (user_id, role_key)
    WHERE status = 'active';

CREATE INDEX idx_user_role_assignment__resolver
    ON business.user_role_assignment (user_id, status, role_key, id);

CREATE OR REPLACE FUNCTION business.enforce_user_role_assignment_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'user role assignment cannot be deleted';
    END IF;

    IF OLD.status <> 'active' OR NEW.status <> 'revoked' OR
       NEW.version <> OLD.version + 1 OR
       NEW.id IS DISTINCT FROM OLD.id OR
       NEW.user_id IS DISTINCT FROM OLD.user_id OR
       NEW.role_key IS DISTINCT FROM OLD.role_key OR
       NEW.assigned_by_user_id IS DISTINCT FROM OLD.assigned_by_user_id OR
       NEW.assignment_reason_code IS DISTINCT FROM OLD.assignment_reason_code OR
       NEW.approval_reference IS DISTINCT FROM OLD.approval_reference OR
       NEW.assigned_at IS DISTINCT FROM OLD.assigned_at OR
       NEW.revoked_by_user_id IS NULL OR
       NEW.revoke_reason_code IS NULL OR
       NEW.revocation_approval_reference IS NULL OR
       NEW.revoked_at IS NULL THEN
        RAISE EXCEPTION 'user role assignment transition is invalid';
    END IF;

    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION business.enforce_user_role_assignment_append_only() IS '限制角色分配只能从 active 单向撤销，并禁止删除或改写授予历史';

CREATE TRIGGER trg_user_role_assignment__append_only
    BEFORE UPDATE OR DELETE ON business.user_role_assignment
    FOR EACH ROW
    EXECUTE FUNCTION business.enforce_user_role_assignment_append_only();

ALTER TABLE business.skill_command_receipt
    ADD COLUMN request_id uuid NULL;

COMMENT ON COLUMN business.skill_command_receipt.request_id IS '首次 HTTP 审核决定的服务端 UUIDv7 请求标识，历史及非 HTTP 命令允许为空';

ALTER TABLE business.skill_governance_audit
    ADD COLUMN request_id uuid NULL;

COMMENT ON COLUMN business.skill_governance_audit.request_id IS '首次 HTTP 审核决定的服务端 UUIDv7 请求标识，历史及非 HTTP 审核允许为空';
