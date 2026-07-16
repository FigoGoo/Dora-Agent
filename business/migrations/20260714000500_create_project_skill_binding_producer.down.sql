DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM business.project_creation_receipt WHERE request_schema_version = 'project_quick_create.v2')
       OR EXISTS (SELECT 1 FROM business.project_session_binding WHERE request_schema_version = 'ensure_project_session.v2')
       OR EXISTS (SELECT 1 FROM business.project_session_outbox WHERE schema_version = 'session_bootstrap_outbox_payload.v2')
       OR EXISTS (SELECT 1 FROM business.project_skill_binding_set)
       OR EXISTS (SELECT 1 FROM business.project_skill_binding)
       OR EXISTS (SELECT 1 FROM business.project_skill_binding_audit)
       OR EXISTS (SELECT 1 FROM business.project_skill_binding_command_receipt)
       OR EXISTS (SELECT 1 FROM business.project_session_skill_resolution)
       OR EXISTS (SELECT 1 FROM business.project_session_skill_resolution_item) THEN
        RAISE EXCEPTION 'cannot rollback Project Skill Binding producer migration while v2 or binding facts exist'
            USING ERRCODE = '55000';
    END IF;
    IF EXISTS (SELECT 1 FROM business.skill WHERE governance_epoch <> 1) THEN
        RAISE EXCEPTION 'cannot rollback Project Skill Binding producer migration after governance epoch changed'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE business.project_session_outbox
    DROP CONSTRAINT ck_project_session_outbox__encrypted_payload,
    DROP CONSTRAINT ck_project_session_outbox__version_projection,
    DROP CONSTRAINT ck_project_session_outbox__skill_count,
    DROP CONSTRAINT ck_project_session_outbox__snapshot_digest,
    DROP CONSTRAINT ck_project_session_outbox__schema_version,
    DROP COLUMN resolution_id,
    DROP COLUMN binding_set_version,
    DROP COLUMN skill_count,
    DROP COLUMN skill_snapshot_digest,
    ALTER COLUMN payload_key_version TYPE varchar(64),
    ADD CONSTRAINT ck_project_session_outbox__schema_version CHECK (schema_version = 'agent.session-bootstrap.v1'),
    ADD CONSTRAINT ck_project_session_outbox__encrypted_payload CHECK (
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
            payload_cleared_at IS NOT NULL AND delivered_at IS NOT NULL AND payload_cleared_at >= delivered_at
        )
    );

ALTER TABLE business.project_session_binding
    DROP CONSTRAINT ck_project_session_binding__version_projection,
    DROP CONSTRAINT ck_project_session_binding__skill_count,
    DROP CONSTRAINT ck_project_session_binding__snapshot_digest,
    DROP CONSTRAINT ck_project_session_binding__request_schema,
    DROP COLUMN resolution_id,
    DROP COLUMN binding_set_version,
    DROP COLUMN skill_count,
    DROP COLUMN skill_snapshot_digest,
    DROP COLUMN request_schema_version;

ALTER TABLE business.project_creation_receipt
    DROP CONSTRAINT ck_project_creation_receipt__version_projection,
    DROP CONSTRAINT ck_project_creation_receipt__skill_count,
    DROP CONSTRAINT ck_project_creation_receipt__snapshot_digest,
    DROP CONSTRAINT ck_project_creation_receipt__request_schema,
    DROP COLUMN resolution_id,
    DROP COLUMN binding_set_version,
    DROP COLUMN skill_count,
    DROP COLUMN skill_snapshot_digest,
    DROP COLUMN request_schema_version;

DROP TRIGGER trg_project_session_skill_resolution_item__immutable ON business.project_session_skill_resolution_item;
DROP TRIGGER trg_project_session_skill_resolution__immutable ON business.project_session_skill_resolution;
DROP TRIGGER trg_project_skill_binding_audit__immutable ON business.project_skill_binding_audit;
DROP FUNCTION business.reject_project_skill_binding_immutable_fact_change();

DROP TABLE business.project_session_skill_resolution_item;
DROP TABLE business.project_session_skill_resolution;
DROP TABLE business.project_skill_binding_audit;
DROP TABLE business.project_skill_binding_command_receipt;
DROP TABLE business.project_skill_binding;
DROP TABLE business.project_skill_binding_set;

ALTER TABLE business.skill
    DROP CONSTRAINT ck_skill__governance_epoch,
    DROP COLUMN governance_epoch;
