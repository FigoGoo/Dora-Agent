ALTER TABLE business.user_role_assignment
    DROP CONSTRAINT ck_user_role_assignment__role,
    ADD CONSTRAINT ck_user_role_assignment__role
        CHECK (role_key IN ('skill_reviewer', 'skill_governor'));

COMMENT ON TABLE business.user_role_assignment IS '用户角色分配表，保存 Skill 审核与治理角色的授予、撤销和审批审计事实';
COMMENT ON COLUMN business.user_role_assignment.role_key IS '角色稳定键：skill_reviewer-审核角色，skill_governor-治理角色';

ALTER TABLE business.skill_command_receipt
    DROP CONSTRAINT ck_skill_command_receipt__command_type,
    ADD COLUMN response_governance_epoch bigint NULL,
    ADD CONSTRAINT ck_skill_command_receipt__command_type
        CHECK (command_type IN ('create', 'submit_review', 'approve_and_publish', 'governance_transition')),
    ADD CONSTRAINT ck_skill_command_receipt__governance_branch CHECK (
        (
            command_type = 'governance_transition' AND
            scope_id = result_skill_id AND
            result_content_revision_id IS NULL AND
            result_review_submission_id IS NULL AND
            result_published_snapshot_id IS NOT NULL AND
            response_published_snapshot_id IS NOT NULL AND
            result_published_snapshot_id = response_published_snapshot_id AND
            response_review_submission_id IS NULL AND
            response_review_status IS NULL AND
            response_review_reason_code IS NULL AND
            response_review_updated_at IS NULL AND
            response_governance_epoch IS NOT NULL AND
            response_governance_epoch >= 2 AND
            request_id IS NOT NULL
        ) OR
        (
            command_type <> 'governance_transition' AND
            response_governance_epoch IS NULL
        )
    );

COMMENT ON COLUMN business.skill_command_receipt.command_type IS '命令类型：create、submit_review、approve_and_publish 或 governance_transition';
COMMENT ON COLUMN business.skill_command_receipt.result_published_snapshot_id IS '批准发布产生或治理命令读取的当前发布快照安全结果引用';
COMMENT ON COLUMN business.skill_command_receipt.request_id IS '首次 HTTP 审核或治理决定的服务端 UUIDv7 请求标识，历史及非 HTTP 命令允许为空';
COMMENT ON COLUMN business.skill_command_receipt.response_governance_epoch IS '首次治理命令安全响应冻结的治理纪元，非治理命令固定为空';

ALTER TABLE business.skill_governance_audit
    DROP CONSTRAINT ck_skill_governance_audit__action,
    DROP CONSTRAINT ck_skill_governance_audit__transition,
    ADD COLUMN actor_role_key varchar(64) NULL,
    ADD COLUMN governance_epoch bigint NULL,
    ADD COLUMN approval_reference varchar(160) NULL,
    ADD COLUMN source_address inet NULL,
    ADD COLUMN command_receipt_id uuid NULL,
    ADD CONSTRAINT ck_skill_governance_audit__action CHECK (
        action IN (
            'review_approved_and_published',
            'governance_suspended',
            'governance_resumed',
            'governance_offlined'
        )
    ),
    ADD CONSTRAINT ck_skill_governance_audit__transition CHECK (
        (action = 'review_approved_and_published' AND from_status = 'reviewing' AND to_status = 'approved') OR
        (action = 'governance_suspended' AND from_status = 'active' AND to_status = 'suspended') OR
        (action = 'governance_resumed' AND from_status = 'suspended' AND to_status = 'active') OR
        (action = 'governance_offlined' AND from_status IN ('active', 'suspended') AND to_status = 'offline')
    ),
    ADD CONSTRAINT ck_skill_governance_audit__payload CHECK (
        (
            action = 'review_approved_and_published' AND
            actor_role_key IS NULL AND
            governance_epoch IS NULL AND
            approval_reference IS NULL AND
            source_address IS NULL AND
            command_receipt_id IS NULL
        ) OR
        (
            action IN ('governance_suspended', 'governance_resumed', 'governance_offlined') AND
            review_submission_id IS NULL AND
            actor_role_key IS NOT NULL AND
            actor_role_key = 'skill_governor' AND
            safe_reason_code IS NOT NULL AND
            governance_epoch IS NOT NULL AND
            governance_epoch >= 2 AND
            approval_reference IS NOT NULL AND
            approval_reference ~ '^[A-Z][A-Z0-9_]{1,31}-[A-Za-z0-9][A-Za-z0-9._-]{0,126}$' AND
            source_address IS NOT NULL AND
            request_id IS NOT NULL AND
            command_receipt_id IS NOT NULL AND
            (
                (action = 'governance_suspended' AND safe_reason_code IN (
                    'content_safety', 'copyright_risk', 'privacy_risk', 'fraud_or_abuse',
                    'tool_dependency_risk', 'policy_violation', 'incident_containment'
                )) OR
                (action = 'governance_resumed' AND safe_reason_code IN (
                    'risk_cleared', 'appeal_approved', 'incident_resolved',
                    'dependency_restored', 'policy_remediated'
                )) OR
                (action = 'governance_offlined' AND safe_reason_code IN (
                    'content_safety', 'copyright_risk', 'privacy_risk', 'fraud_or_abuse',
                    'tool_dependency_risk', 'policy_violation', 'owner_request', 'repeated_violation'
                ))
            )
        )
    );

COMMENT ON TABLE business.skill_governance_audit IS 'Skill 审核与治理追加审计表，保存不可变处置事实且不保存完整 Skill 正文';
COMMENT ON COLUMN business.skill_governance_audit.review_submission_id IS '审核审计涉及的提交标识，治理迁移固定为空且不设置数据库物理外键约束';
COMMENT ON COLUMN business.skill_governance_audit.action IS '稳定审计动作：审核批准发布、治理暂停、恢复或永久下架';
COMMENT ON COLUMN business.skill_governance_audit.from_status IS '动作前审核或治理状态，与 action 的合法迁移严格对应';
COMMENT ON COLUMN business.skill_governance_audit.to_status IS '动作后审核或治理状态，与 action 的合法迁移严格对应';
COMMENT ON COLUMN business.skill_governance_audit.safe_reason_code IS '治理动作的闭集安全原因代码；审核批准历史允许为空';
COMMENT ON COLUMN business.skill_governance_audit.actor_user_id IS '执行审核或治理动作的可信内部用户标识';
COMMENT ON COLUMN business.skill_governance_audit.request_id IS '首次 HTTP 审核或治理决定的服务端 UUIDv7 请求标识，历史及非 HTTP 审核允许为空';
COMMENT ON COLUMN business.skill_governance_audit.actor_role_key IS '治理动作使用的权威角色键，审核批准历史固定为空';
COMMENT ON COLUMN business.skill_governance_audit.governance_epoch IS '治理动作提交后的治理纪元，审核批准历史固定为空';
COMMENT ON COLUMN business.skill_governance_audit.approval_reference IS '治理动作对应的规范外部审批或工单引用，不保存自由正文或 Secret';
COMMENT ON COLUMN business.skill_governance_audit.source_address IS '治理 HTTP 请求的规范直连 IPv4 或 IPv6 来源地址';
COMMENT ON COLUMN business.skill_governance_audit.command_receipt_id IS '治理动作对应的命令回执标识，不设置数据库物理外键约束';

CREATE UNIQUE INDEX uq_skill_governance_audit__command_receipt
    ON business.skill_governance_audit (command_receipt_id)
    WHERE command_receipt_id IS NOT NULL;

CREATE INDEX idx_skill_published_snapshot__published_id
    ON business.skill_published_snapshot (published_at DESC, id DESC);
