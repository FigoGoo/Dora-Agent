ALTER TABLE business.skill_governance_audit
    DROP COLUMN request_id;

ALTER TABLE business.skill_command_receipt
    DROP COLUMN request_id;

DROP TRIGGER trg_user_role_assignment__append_only ON business.user_role_assignment;
DROP FUNCTION business.enforce_user_role_assignment_append_only();
DROP TABLE business.user_role_assignment;
