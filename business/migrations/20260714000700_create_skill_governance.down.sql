DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM business.user_role_assignment
        WHERE role_key = 'skill_governor'
    ) OR EXISTS (
        SELECT 1
        FROM business.skill_command_receipt
        WHERE command_type = 'governance_transition'
           OR response_governance_epoch IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM business.skill_governance_audit
        WHERE action IN ('governance_suspended', 'governance_resumed', 'governance_offlined')
           OR actor_role_key IS NOT NULL
           OR governance_epoch IS NOT NULL
           OR approval_reference IS NOT NULL
           OR source_address IS NOT NULL
           OR command_receipt_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM business.skill
        WHERE governance_status <> 'active'
           OR governance_epoch <> 1
    ) THEN
        RAISE EXCEPTION 'cannot rollback Skill governance migration after governance facts exist'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP INDEX business.idx_skill_published_snapshot__published_id;
DROP INDEX business.uq_skill_governance_audit__command_receipt;

ALTER TABLE business.skill_governance_audit
    DROP CONSTRAINT ck_skill_governance_audit__payload,
    DROP CONSTRAINT ck_skill_governance_audit__transition,
    DROP CONSTRAINT ck_skill_governance_audit__action,
    DROP COLUMN command_receipt_id,
    DROP COLUMN source_address,
    DROP COLUMN approval_reference,
    DROP COLUMN governance_epoch,
    DROP COLUMN actor_role_key,
    ADD CONSTRAINT ck_skill_governance_audit__action
        CHECK (action = 'review_approved_and_published'),
    ADD CONSTRAINT ck_skill_governance_audit__transition
        CHECK (from_status = 'reviewing' AND to_status = 'approved');

COMMENT ON TABLE business.skill_governance_audit IS 'Skill 审核与治理追加审计表，不保存完整 Skill 正文或内部策略原文';
COMMENT ON COLUMN business.skill_governance_audit.review_submission_id IS '审计涉及的审核提交标识，不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_governance_audit.action IS '稳定审计动作，W1 当前仅记录审核通过并发布';
COMMENT ON COLUMN business.skill_governance_audit.from_status IS '动作前审核状态，W1 批准发布固定为 reviewing';
COMMENT ON COLUMN business.skill_governance_audit.to_status IS '动作后审核状态，W1 批准发布固定为 approved';
COMMENT ON COLUMN business.skill_governance_audit.safe_reason_code IS '可安全审计的稳定原因代码，不保存策略原文或完整正文';
COMMENT ON COLUMN business.skill_governance_audit.actor_user_id IS '执行动作的可信 Reviewer 用户标识';
COMMENT ON COLUMN business.skill_governance_audit.request_id IS '首次 HTTP 审核决定的服务端 UUIDv7 请求标识，历史及非 HTTP 审核允许为空';

ALTER TABLE business.skill_command_receipt
    DROP CONSTRAINT ck_skill_command_receipt__governance_branch,
    DROP CONSTRAINT ck_skill_command_receipt__command_type,
    DROP COLUMN response_governance_epoch,
    ADD CONSTRAINT ck_skill_command_receipt__command_type
        CHECK (command_type IN ('create', 'submit_review', 'approve_and_publish'));

COMMENT ON COLUMN business.skill_command_receipt.command_type IS '命令类型：create、submit_review 或 approve_and_publish';
COMMENT ON COLUMN business.skill_command_receipt.result_published_snapshot_id IS '批准发布命令产生的发布快照安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.request_id IS '首次 HTTP 审核决定的服务端 UUIDv7 请求标识，历史及非 HTTP 命令允许为空';

ALTER TABLE business.user_role_assignment
    DROP CONSTRAINT ck_user_role_assignment__role,
    ADD CONSTRAINT ck_user_role_assignment__role
        CHECK (role_key = 'skill_reviewer');

COMMENT ON TABLE business.user_role_assignment IS '用户角色分配表，保存最小 Reviewer RBAC 的授予、撤销和审批审计事实';
COMMENT ON COLUMN business.user_role_assignment.role_key IS '角色稳定键，W1-C2 固定为 skill_reviewer';
