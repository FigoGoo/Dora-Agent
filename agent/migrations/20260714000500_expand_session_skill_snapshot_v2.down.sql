-- Down 只允许尚未产生任何 V2 数据时执行；拒绝条件先于 DROP，保证非空 Snapshot 和 V2 Receipt 不丢失。
DO $$
BEGIN
    IF to_regclass('agent.session_skill_snapshot_item') IS NOT NULL
       AND EXISTS (SELECT 1 FROM agent.session_skill_snapshot_item) THEN
        RAISE EXCEPTION 'cannot rollback session skill snapshot v2: snapshot items exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM agent.session_skill_snapshot WHERE snapshot_kind = 'published_refs'
    ) THEN
        RAISE EXCEPTION 'cannot rollback session skill snapshot v2: published_refs headers exist';
    END IF;
    IF EXISTS (
        SELECT 1 FROM agent.session_command_receipt WHERE command_type = 'ensure_project_session_v2'
    ) THEN
        RAISE EXCEPTION 'cannot rollback session skill snapshot v2: v2 command receipts exist';
    END IF;
END
$$;

DROP TRIGGER IF EXISTS trg_session_skill_snapshot_item__immutable
    ON agent.session_skill_snapshot_item;
DROP TRIGGER IF EXISTS trg_session_skill_snapshot__immutable
    ON agent.session_skill_snapshot;
DROP FUNCTION IF EXISTS agent.reject_session_skill_snapshot_mutation();

DROP TABLE agent.session_skill_snapshot_item;

ALTER TABLE agent.session_command_receipt
    DROP CONSTRAINT ck_session_command_receipt__type,
    DROP CONSTRAINT ck_session_command_receipt__skill_snapshot_digest,
    DROP CONSTRAINT ck_session_command_receipt__skill_count,
    DROP COLUMN skill_snapshot_digest,
    DROP COLUMN skill_count,
    ADD CONSTRAINT ck_session_command_receipt__type
        CHECK (command_type IN ('ensure_project_session_v1'));

COMMENT ON COLUMN agent.session_command_receipt.command_type IS '稳定命令类型，W0 为 ensure_project_session_v1';

ALTER TABLE agent.session_skill_snapshot
    DROP CONSTRAINT ck_session_skill_snapshot__schema_version,
    DROP CONSTRAINT ck_session_skill_snapshot__kind,
    DROP CONSTRAINT ck_session_skill_snapshot__skill_count,
    DROP CONSTRAINT ck_session_skill_snapshot__kind_content,
    DROP COLUMN schema_version,
    DROP COLUMN skill_count,
    ADD CONSTRAINT ck_session_skill_snapshot__kind CHECK (snapshot_kind IN ('empty')),
    ADD CONSTRAINT ck_session_skill_snapshot__empty_refs CHECK (
        snapshot_kind <> 'empty' OR published_snapshot_refs = '[]'::jsonb
    );

COMMENT ON TABLE agent.session_skill_snapshot IS '会话 Skill 冻结快照表，W0 显式记录空快照并保证后续发布不改变既有会话';
COMMENT ON COLUMN agent.session_skill_snapshot.snapshot_kind IS '快照类型，W0 仅允许 empty-显式空快照';
COMMENT ON COLUMN agent.session_skill_snapshot.published_snapshot_refs IS '冻结的 Business Published Skill 引用数组，W0 必须为空数组';
