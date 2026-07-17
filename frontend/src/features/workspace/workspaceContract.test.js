import { describe, expect, it } from 'vitest';
import {
  creationSpecPreviewCardFixture,
  creationSpecPreviewCompletedEventFixture,
  creationSpecPreviewFailedEventFixture,
  directResponseCardFixture,
  inputAcceptedEventFixture,
  inputFixture,
  messageFixture,
  turnEventFixture,
  turnFailureCardFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture,
  workspaceSnapshotV1Fixture
} from '../../test/workspaceFixtures.js';
import {
  parsePersistentWorkspaceEvent,
  parseStreamReady,
  parseStreamReset,
  parseWorkspaceSnapshot,
  WORKSPACE_INPUT_SOURCE_TYPES,
  WORKSPACE_INPUT_STATUSES
} from './workspaceContract.js';

describe('Workspace frozen contract', () => {
  it('maps a complete typed Snapshot and preserves empty arrays', () => {
    const empty = parseWorkspaceSnapshot(workspaceSnapshotFixture(), expectedBinding());
    expect(empty.messages).toEqual([]);
    expect(empty.inputs).toEqual([]);
    expect(empty.creationSpecPreview).toBeNull();
    expect(empty.latestTurnOutput).toBeNull();
    expect(empty.schemaVersion).toBe('session.workspace.v2');
    expect(empty.eventHighWatermark).toBe(1);

    const populated = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      messages: [messageFixture()],
      inputs: [inputFixture()],
      event_high_watermark: 2
    }), expectedBinding());
    expect(populated.messages[0]).toMatchObject({ messageSeq: 1, content: '创建一支短片' });
    expect(populated.inputs[0]).toMatchObject({ enqueueSeq: 1, status: 'pending' });
  });

  it('accepts exact legacy v1 without Turn Output and requires the nullable v2 field', () => {
    const legacy = parseWorkspaceSnapshot(workspaceSnapshotV1Fixture(), expectedBinding());
    expect(legacy.schemaVersion).toBe('session.workspace.v1');
    expect(legacy.latestTurnOutput).toBeNull();

    const missing = workspaceSnapshotFixture();
    delete missing.latest_turn_output;
    expect(() => parseWorkspaceSnapshot(missing, expectedBinding())).toThrow('字段集合');
    expect(() => parseWorkspaceSnapshot({
      ...workspaceSnapshotV1Fixture(),
      latest_turn_output: null
    }, expectedBinding())).toThrow('字段集合');
  });

  it('parses v2 Direct Response and Failure projections bound to a Snapshot Input', () => {
    for (const output of [directResponseCardFixture(), turnFailureCardFixture({ status: 'recovery_pending' })]) {
      const parsed = parseWorkspaceSnapshot(workspaceSnapshotFixture({
        messages: [messageFixture()],
        inputs: [inputFixture()],
        latest_turn_output: output,
        event_high_watermark: 3
      }), expectedBinding());
      expect(parsed.latestTurnOutput.inputID).toBe(WORKSPACE_IDS.input);
    }
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
      latest_turn_output: directResponseCardFixture()
    }), expectedBinding())).toThrow('不存在的 Input');
  });

  it('parses a nullable Creation Spec projection and safely degrades an unknown card version', () => {
    const card = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      creation_spec_preview: creationSpecPreviewCardFixture()
    }), expectedBinding());
    expect(card.creationSpecPreview).toMatchObject({
      kind: 'card', creationSpecID: WORKSPACE_IDS.creationSpec, version: 1
    });
    const unsupported = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      creation_spec_preview: {
        ...creationSpecPreviewCardFixture({ schema_version: 'creation_spec.preview.card.v2' }),
        future_field: { opaque: true }
      }
    }), expectedBinding());
    expect(unsupported.creationSpecPreview).toEqual({
      kind: 'unsupported', schemaVersion: 'creation_spec.preview.card.v2'
    });
    const missing = workspaceSnapshotFixture();
    delete missing.creation_spec_preview;
    expect(() => parseWorkspaceSnapshot(missing, expectedBinding())).toThrow('字段集合');
  });

  it('fails closed on extra, missing and null fields at every Snapshot layer', () => {
    strictObjectVariants(workspaceSnapshotFixture(), 'request_id').forEach((snapshot) => {
      expect(() => parseWorkspaceSnapshot(snapshot, expectedBinding())).toThrow();
    });

    strictObjectVariants(workspaceSnapshotFixture().session, 'status').forEach((session) => {
      expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({ session }), expectedBinding())).toThrow();
    });

    strictObjectVariants(messageFixture(), 'role').forEach((message) => {
      expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
        messages: [message],
        inputs: [inputFixture()],
        event_high_watermark: 2
      }), expectedBinding())).toThrow();
    });

    strictObjectVariants(inputFixture(), 'status').forEach((input) => {
      expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
        messages: [messageFixture()],
        inputs: [input],
        event_high_watermark: 2
      }), expectedBinding())).toThrow();
    });
  });

  it('fails closed on null arrays, binding drift, unsafe seq and non-RFC3339 timestamps', () => {
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({ messages: null }), expectedBinding())).toThrow('必须为数组');
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
      session: { ...workspaceSnapshotFixture().session, project_id: '019f0000-0000-7000-8000-000000000099' }
    }), expectedBinding())).toThrow('Binding 不一致');
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({ event_high_watermark: Number.MAX_SAFE_INTEGER + 1 }), expectedBinding()))
      .toThrow('安全整数');
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
      session: { ...workspaceSnapshotFixture().session, updated_at: 'July 14, 2026' }
    }), expectedBinding())).toThrow('RFC3339');
  });

  it.each(WORKSPACE_INPUT_STATUSES)('accepts frozen Snapshot Input status %s', (status) => {
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      messages: [messageFixture()],
      inputs: [inputFixture({ status })],
      event_high_watermark: 2
    }), expectedBinding());

    expect(snapshot.inputs[0].status).toBe(status);
  });

  it.each(WORKSPACE_INPUT_SOURCE_TYPES)('accepts frozen Snapshot Input source type %s', (sourceType) => {
    const messageLess = sourceType === 'analyze_materials_preview' || sourceType === 'plan_storyboard_preview'
      || sourceType === 'write_prompts_preview' || sourceType === 'generate_media_preview_request'
      || sourceType === 'assemble_output_preview_request' || sourceType === 'media_job_preview_terminal';
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      messages: messageLess ? [] : [messageFixture()],
      inputs: [inputFixture({ source_type: sourceType, message_id: messageLess ? null : WORKSPACE_IDS.message })],
      event_high_watermark: 2
    }), expectedBinding());

    expect(snapshot.inputs[0].sourceType).toBe(sourceType);
  });

  it.each(['preview', 'assistant_message', '', null])('rejects unknown Snapshot Input source type %s', (sourceType) => {
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
      messages: [messageFixture()],
      inputs: [inputFixture({ source_type: sourceType })],
      event_high_watermark: 2
    }), expectedBinding())).toThrow(/input\.source_type/);
  });

  it.each(['accepted', 'queued', 'failed', '', null])('rejects unknown Snapshot Input status %s', (status) => {
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
      messages: [messageFixture()],
      inputs: [inputFixture({ status })],
      event_high_watermark: 2
    }), expectedBinding())).toThrow(/input\.status/);
  });

  it('requires SSE id, event name, envelope and typed payload to agree', () => {
    const parsed = parsePersistentWorkspaceEvent(messageEvent('session.input.accepted', '2', inputAcceptedEventFixture()), expectedBinding());
    expect(parsed).toMatchObject({ seq: 2, eventID: WORKSPACE_IDS.event, aggregateID: WORKSPACE_IDS.input });

    expect(() => parsePersistentWorkspaceEvent(
      messageEvent('session.input.accepted', '3', inputAcceptedEventFixture()), expectedBinding()
    )).toThrow('SSE id 与 seq 不一致');
    expect(() => parsePersistentWorkspaceEvent(
      messageEvent('session.created', '2', inputAcceptedEventFixture()), expectedBinding()
    )).toThrow('SSE event');
    expect(() => parsePersistentWorkspaceEvent({ type: 'session.input.accepted', lastEventId: '2', data: '{' }, expectedBinding()))
      .toThrow('有效 JSON');
  });

  it('fails closed on extra, missing and null Workspace Event envelope fields', () => {
    strictObjectVariants(inputAcceptedEventFixture(), 'event_id').forEach((event) => {
      expect(() => parsePersistentWorkspaceEvent(
        messageEvent('session.input.accepted', '2', event), expectedBinding()
      )).toThrow();
    });
  });

  it('fails closed on extra, missing and null session.created/session.input.accepted payload fields', () => {
    const created = sessionCreatedEventFixture();
    strictObjectVariants(created.payload, 'status').forEach((payload) => {
      expect(() => parsePersistentWorkspaceEvent(
        messageEvent('session.created', '1', { ...created, payload }), expectedBinding()
      )).toThrow();
    });

    const accepted = inputAcceptedEventFixture();
    strictObjectVariants(accepted.payload, 'status').forEach((payload) => {
      expect(() => parsePersistentWorkspaceEvent(
        messageEvent('session.input.accepted', '2', { ...accepted, payload }), expectedBinding()
      )).toThrow();
    });
  });

  it('strictly parses Creation Spec completed/failed events and aggregate bindings', () => {
    const completed = parsePersistentWorkspaceEvent(
      messageEvent('creation_spec.preview.completed', '2', creationSpecPreviewCompletedEventFixture()),
      expectedBinding()
    );
    expect(completed.payload).toMatchObject({ kind: 'card', creationSpecID: WORKSPACE_IDS.creationSpec });

    const failed = parsePersistentWorkspaceEvent(
      messageEvent('creation_spec.preview.failed', '2', creationSpecPreviewFailedEventFixture()),
      expectedBinding()
    );
    expect(failed.payload).toMatchObject({ kind: 'failure', inputID: WORKSPACE_IDS.previewInput });

    expect(() => parsePersistentWorkspaceEvent(
      messageEvent('creation_spec.preview.completed', '2', creationSpecPreviewCompletedEventFixture({
        aggregate_id: WORKSPACE_IDS.previewInput
      })), expectedBinding()
    )).toThrow('Aggregate 不一致');
    expect(() => parsePersistentWorkspaceEvent(
      messageEvent('creation_spec.preview.failed', '2', creationSpecPreviewFailedEventFixture({
        payload: {
          input_id: WORKSPACE_IDS.previewInput,
          result_code: 'CREATION_SPEC_PREVIEW_INVALID',
          summary: '失败',
          retryable: false,
          debug: 'secret'
        }
      })), expectedBinding()
    )).toThrow('字段集合');

    expect(() => parsePersistentWorkspaceEvent(
      messageEvent('creation_spec.preview.completed', '2', creationSpecPreviewCompletedEventFixture({
        envelope_future_field: true
      })), expectedBinding()
    )).toThrow('Workspace Event 字段集合');
    expect(() => parsePersistentWorkspaceEvent(
      messageEvent('creation_spec.preview.failed', '2', creationSpecPreviewFailedEventFixture({
        envelope_future_field: true
      })), expectedBinding()
    )).toThrow('Workspace Event 字段集合');
  });

  it.each([
    ['session.turn.completed', 'completed'],
    ['session.turn.failed', 'failed'],
    ['session.turn.recovery_pending', 'recovery_pending']
  ])('strictly parses %s with session_turn aggregate binding', (eventName, status) => {
    const wire = turnEventFixture(eventName);
    const parsed = parsePersistentWorkspaceEvent(messageEvent(eventName, '3', wire), expectedBinding());
    expect(parsed.payload).toMatchObject({ turnID: WORKSPACE_IDS.turn, inputID: WORKSPACE_IDS.input, status });

    expect(() => parsePersistentWorkspaceEvent(messageEvent(eventName, '3', {
      ...wire,
      aggregate_id: WORKSPACE_IDS.input
    }), expectedBinding())).toThrow(/Aggregate|status/);
  });

  it('does not reinterpret a failed/recovery mismatch or unknown Turn Card field as success', () => {
    expect(() => parsePersistentWorkspaceEvent(messageEvent(
      'session.turn.failed', '3', turnEventFixture('session.turn.failed', {
        payload: { status: 'recovery_pending' }
      })
    ), expectedBinding())).toThrow(/Aggregate|status/);
    expect(() => parsePersistentWorkspaceEvent(messageEvent(
      'session.turn.completed', '3', turnEventFixture('session.turn.completed', {
        payload: { provider_payload: 'secret' }
      })
    ), expectedBinding())).toThrow('字段集合');
  });

  it.each(WORKSPACE_INPUT_STATUSES.filter((status) => status !== 'pending'))(
    'keeps session.input.accepted payload status closed against %s',
    (status) => {
      const event = inputAcceptedEventFixture();
      event.payload.status = status;
      expect(() => parsePersistentWorkspaceEvent(
        messageEvent('session.input.accepted', '2', event), expectedBinding()
      )).toThrow('payload.status');
    }
  );

  it('accepts no-id stream controls and rejects reset ids or unknown reasons', () => {
    expect(parseStreamReady(messageEvent('stream.ready', '', streamReadyFixture()), WORKSPACE_IDS.session))
      .toMatchObject({ cursor: 2, latestSeq: 2 });
    expect(parseStreamReset(messageEvent('stream.reset', '', streamResetFixture()), WORKSPACE_IDS.session))
      .toMatchObject({ reason: 'cursor_expired', snapshotRequired: true });
    expect(() => parseStreamReset(
      messageEvent('stream.reset', '2', streamResetFixture()), WORKSPACE_IDS.session
    )).toThrow('不得设置 SSE id');
  });

  it('fails closed on extra, missing and null stream.ready/stream.reset control fields', () => {
    strictObjectVariants(streamReadyFixture(), 'cursor').forEach((control) => {
      expect(() => parseStreamReady(
        messageEvent('stream.ready', '', control), WORKSPACE_IDS.session
      )).toThrow();
    });
    strictObjectVariants(streamResetFixture(), 'reason').forEach((control) => {
      expect(() => parseStreamReset(
        messageEvent('stream.reset', '', control), WORKSPACE_IDS.session
      )).toThrow();
    });
  });
});

function expectedBinding() {
  return { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session };
}

function messageEvent(type, lastEventId, value) {
  return { type, lastEventId, data: JSON.stringify(value) };
}

function strictObjectVariants(value, requiredField) {
  const missing = { ...value };
  delete missing[requiredField];
  return [
    { ...value, future_field: true },
    missing,
    { ...value, [requiredField]: null }
  ];
}

function sessionCreatedEventFixture() {
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.event,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 1,
    event: 'session.created',
    occurred_at: '2026-07-14T00:00:00.000000Z',
    aggregate_type: 'session',
    aggregate_id: WORKSPACE_IDS.session,
    aggregate_version: 1,
    payload: {
      session_id: WORKSPACE_IDS.session,
      project_id: WORKSPACE_IDS.project,
      status: 'active',
      version: 1
    }
  };
}

function streamReadyFixture() {
  return {
    schema_version: 'workspace.stream-control.v1',
    event: 'stream.ready',
    session_id: WORKSPACE_IDS.session,
    cursor: 2,
    min_available_seq: 1,
    latest_seq: 2
  };
}

function streamResetFixture() {
  return {
    schema_version: 'workspace.stream-control.v1',
    event: 'stream.reset',
    session_id: WORKSPACE_IDS.session,
    reason: 'cursor_expired',
    snapshot_required: true,
    min_available_seq: 3,
    latest_seq: 5
  };
}
