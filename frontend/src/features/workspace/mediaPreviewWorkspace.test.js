import { describe, expect, it } from 'vitest';
import {
  inputFixture,
  WORKSPACE_IDS,
  workspaceSnapshotV5Fixture
} from '../../test/workspaceFixtures.js';
import { parseMediaPreviewCard } from '../media/mediaPreviewContract.js';
import {
  parsePersistentWorkspaceEvent,
  parseWorkspaceSnapshot
} from './workspaceContract.js';
import {
  classifyWorkspaceEvent,
  classifyWorkspaceSnapshot,
  createWorkspaceState,
  workspaceReducer
} from './workspaceReducer.js';

describe('Workspace V5 media projection', () => {
  it('strictly binds the append-only media projection to Project and original request Inputs', () => {
    const wire = workspaceSnapshotV5Fixture({
      inputs: mediaInputs(),
      media_previews: [acceptedCard(), completedCard()],
      event_high_watermark: 8
    });
    const parsed = parseWorkspaceSnapshot(wire, expectedBinding());

    expect(parsed.schemaVersion).toBe('session.workspace.v5');
    expect(parsed.mediaPreviews).toHaveLength(2);
    expect(parsed.mediaPreviews[1]).toMatchObject({
      status: 'completed',
      contentURL: `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${WORKSPACE_IDS.mediaAsset}/content`
    });

    expect(() => parseWorkspaceSnapshot({
      ...wire,
      media_previews: [completedCard({
        content_url: `/api/v1/projects/${WORKSPACE_IDS.session}/media-preview-assets/${WORKSPACE_IDS.mediaAsset}/content`
      })]
    }, expectedBinding())).toThrow('Workspace Binding');
    expect(() => parseWorkspaceSnapshot({ ...wire, inputs: [mediaInputs()[1]] }, expectedBinding()))
      .toThrow('原始请求 Input');
    const missingProjection = { ...wire };
    delete missingProjection.media_previews;
    expect(() => parseWorkspaceSnapshot(missingProjection, expectedBinding())).toThrow('字段集合');
  });

  it('accepts only the frozen original-input and terminal-input aggregate variants', () => {
    const accepted = parsePersistentWorkspaceEvent(
      messageEvent('media.preview.accepted', '7', mediaEvent('media.preview.accepted', acceptedCard())),
      expectedBinding()
    );
    const completed = parsePersistentWorkspaceEvent(
      messageEvent('media.preview.completed', '8', mediaEvent('media.preview.completed', completedCard())),
      expectedBinding()
    );
    expect(accepted.payload.status).toBe('accepted');
    expect(completed).toMatchObject({ aggregateID: WORKSPACE_IDS.mediaTerminalInput });

    expect(() => parsePersistentWorkspaceEvent(messageEvent('media.preview.completed', '8', mediaEvent(
      'media.preview.completed', completedCard(), { aggregate_id: WORKSPACE_IDS.mediaInput }
    )), expectedBinding())).toThrow('变体不一致');
    expect(() => parsePersistentWorkspaceEvent(messageEvent('media.preview.runtime_failed', '8', mediaEvent(
      'media.preview.runtime_failed', terminalFailedCard()
    )), expectedBinding())).toThrow('变体不一致');
    expect(parsePersistentWorkspaceEvent(messageEvent('media.preview.runtime_failed', '7', mediaEvent(
      'media.preview.runtime_failed', earlyFailedCard(), { seq: 7, aggregate_id: WORKSPACE_IDS.mediaInput }
    )), expectedBinding()).payload).toMatchObject({ status: 'failed', operationID: '' });
  });

  it('appends accepted then terminal Cards and rejects projection deletion, mutation or reordering', () => {
    const ready = {
      ...createWorkspaceState(),
      kind: 'ready',
      cursor: 6,
      project: { projectID: WORKSPACE_IDS.project },
      snapshot: {
        session: { id: WORKSPACE_IDS.session },
        inputs: mediaInputs().map((input) => ({
          id: input.id,
          enqueueSeq: input.enqueue_seq,
          status: 'running'
        })),
        mediaPreviews: []
      }
    };
    const accepted = mappedMediaEvent('media.preview.accepted', 7, acceptedCard(), WORKSPACE_IDS.mediaInput);
    expect(classifyWorkspaceEvent(ready, accepted)).toBe('apply');
    const afterAccepted = workspaceReducer(ready, { type: 'event_applied', event: accepted });
    expect(afterAccepted.snapshot.mediaPreviews).toHaveLength(1);
    expect(afterAccepted.snapshot.inputs[0].status).toBe('resolved');

    const terminal = mappedMediaEvent('media.preview.completed', 8, completedCard(), WORKSPACE_IDS.mediaTerminalInput);
    expect(classifyWorkspaceEvent(afterAccepted, terminal)).toBe('apply');
    const completed = workspaceReducer(afterAccepted, { type: 'event_applied', event: terminal });
    expect(completed.snapshot.mediaPreviews.map((card) => card.status)).toEqual(['accepted', 'completed']);
    expect(completed.snapshot.inputs.map((input) => input.status)).toEqual(['resolved', 'resolved']);

    const snapshot = {
      ...completed.snapshot,
      eventHighWatermark: 8,
      mediaPreviews: [...completed.snapshot.mediaPreviews]
    };
    expect(classifyWorkspaceSnapshot(completed, ready.project, snapshot)).toBe('apply');
    expect(classifyWorkspaceSnapshot(completed, ready.project, {
      ...snapshot,
      mediaPreviews: snapshot.mediaPreviews.slice(1)
    })).toBe('invalid');
    expect(classifyWorkspaceSnapshot(completed, ready.project, {
      ...snapshot,
      mediaPreviews: [{ ...snapshot.mediaPreviews[0], resultCode: 'MEDIA_PREVIEW_CHANGED' }, snapshot.mediaPreviews[1]]
    })).toBe('invalid');
  });
});

function mediaInputs() {
  return [
    inputFixture({
      id: WORKSPACE_IDS.mediaInput,
      message_id: null,
      source_type: 'generate_media_preview_request',
      status: 'resolved',
      enqueue_seq: 1
    }),
    inputFixture({
      id: WORKSPACE_IDS.mediaTerminalInput,
      message_id: null,
      source_type: 'media_job_preview_terminal',
      status: 'resolved',
      enqueue_seq: 2
    })
  ];
}

function acceptedCard(overrides = {}) {
  return {
    schema_version: 'media_preview.card.v1',
    input_id: WORKSPACE_IDS.mediaInput,
    turn_id: WORKSPACE_IDS.mediaTurn,
    run_id: WORKSPACE_IDS.mediaRun,
    tool_call_id: WORKSPACE_IDS.mediaToolCall,
    tool_key: 'generate_media',
    status: 'accepted',
    result_code: 'MEDIA_PREVIEW_ACCEPTED',
    updated_at: '2026-07-17T12:00:00Z',
    operation_id: WORKSPACE_IDS.mediaOperation,
    batch_id: WORKSPACE_IDS.mediaBatch,
    asset_ref: {
      id: WORKSPACE_IDS.mediaAsset,
      version: 1,
      status: 'reserved',
      media_kind: 'image',
      mime_type: 'image/png'
    },
    ...overrides
  };
}

function completedCard(overrides = {}) {
  return {
    ...acceptedCard(),
    status: 'completed',
    result_code: 'MEDIA_PREVIEW_COMPLETED',
    job_id: WORKSPACE_IDS.mediaJob,
    asset_ref: {
      id: WORKSPACE_IDS.mediaAsset,
      version: 1,
      status: 'ready',
      media_kind: 'image',
      mime_type: 'image/png',
      content_digest: 'a'.repeat(64),
      size_bytes: 8192
    },
    content_url: `/api/v1/projects/${WORKSPACE_IDS.project}/media-preview-assets/${WORKSPACE_IDS.mediaAsset}/content`,
    ...overrides
  };
}

function earlyFailedCard() {
  return {
    schema_version: 'media_preview.card.v1',
    input_id: WORKSPACE_IDS.mediaInput,
    turn_id: WORKSPACE_IDS.mediaTurn,
    run_id: WORKSPACE_IDS.mediaRun,
    tool_call_id: WORKSPACE_IDS.mediaToolCall,
    tool_key: 'generate_media',
    status: 'failed',
    result_code: 'MEDIA_PREVIEW_FAILED',
    updated_at: '2026-07-17T12:00:00Z',
    error_code: 'INTERNAL'
  };
}

function terminalFailedCard() {
  return {
    ...earlyFailedCard(),
    operation_id: WORKSPACE_IDS.mediaOperation,
    batch_id: WORKSPACE_IDS.mediaBatch,
    job_id: WORKSPACE_IDS.mediaJob,
    asset_ref: {
      id: WORKSPACE_IDS.mediaAsset,
      version: 1,
      status: 'failed',
      media_kind: 'image',
      mime_type: 'image/png'
    },
    error_code: 'ARTIFACT_INVALID'
  };
}

function mediaEvent(event, payload, overrides = {}) {
  const terminal = payload.status !== 'accepted' && payload.operation_id;
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: terminal ? WORKSPACE_IDS.mediaTerminalEvent : WORKSPACE_IDS.mediaAcceptedEvent,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: terminal ? 8 : 7,
    event,
    occurred_at: '2026-07-17T12:00:01Z',
    aggregate_type: 'session_input',
    aggregate_id: terminal ? WORKSPACE_IDS.mediaTerminalInput : WORKSPACE_IDS.mediaInput,
    aggregate_version: 1,
    payload,
    ...overrides
  };
}

function mappedMediaEvent(event, seq, payload, aggregateID) {
  return {
    eventID: seq === 7 ? WORKSPACE_IDS.mediaAcceptedEvent : WORKSPACE_IDS.mediaTerminalEvent,
    seq,
    event,
    aggregateID,
    payload: parseMediaPreviewCard(payload, { expectedProjectID: WORKSPACE_IDS.project })
  };
}

function expectedBinding() {
  return { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session };
}

function messageEvent(type, lastEventId, value) {
  return { type, lastEventId, data: JSON.stringify(value) };
}
