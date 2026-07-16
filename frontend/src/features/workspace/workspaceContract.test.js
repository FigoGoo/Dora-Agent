import { describe, expect, it } from 'vitest';
import {
  inputAcceptedEventFixture,
  inputFixture,
  messageFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture
} from '../../test/workspaceFixtures.js';
import {
  parsePersistentWorkspaceEvent,
  parseStreamReady,
  parseStreamReset,
  parseWorkspaceSnapshot,
  WORKSPACE_INPUT_STATUSES
} from './workspaceContract.js';

describe('Workspace frozen contract', () => {
  it('maps a complete typed Snapshot and preserves empty arrays', () => {
    const empty = parseWorkspaceSnapshot(workspaceSnapshotFixture(), expectedBinding());
    expect(empty.messages).toEqual([]);
    expect(empty.inputs).toEqual([]);
    expect(empty.eventHighWatermark).toBe(1);

    const populated = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      messages: [messageFixture()],
      inputs: [inputFixture()],
      event_high_watermark: 2
    }), expectedBinding());
    expect(populated.messages[0]).toMatchObject({ messageSeq: 1, content: '创建一支短片' });
    expect(populated.inputs[0]).toMatchObject({ enqueueSeq: 1, status: 'pending' });
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
    expect(parseStreamReady(messageEvent('stream.ready', '', {
      schema_version: 'workspace.stream-control.v1', event: 'stream.ready', session_id: WORKSPACE_IDS.session,
      cursor: 2, min_available_seq: 1, latest_seq: 2
    }), WORKSPACE_IDS.session)).toMatchObject({ cursor: 2, latestSeq: 2 });
    expect(parseStreamReset(messageEvent('stream.reset', '', {
      schema_version: 'workspace.stream-control.v1', event: 'stream.reset', session_id: WORKSPACE_IDS.session,
      reason: 'cursor_expired', snapshot_required: true, min_available_seq: 3, latest_seq: 5
    }), WORKSPACE_IDS.session)).toMatchObject({ reason: 'cursor_expired', snapshotRequired: true });
    expect(() => parseStreamReset(messageEvent('stream.reset', '2', {
      schema_version: 'workspace.stream-control.v1', event: 'stream.reset', session_id: WORKSPACE_IDS.session,
      reason: 'cursor_expired', snapshot_required: true, min_available_seq: 3, latest_seq: 5
    }), WORKSPACE_IDS.session)).toThrow('不得设置 SSE id');
  });
});

function expectedBinding() {
  return { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session };
}

function messageEvent(type, lastEventId, value) {
  return { type, lastEventId, data: JSON.stringify(value) };
}
