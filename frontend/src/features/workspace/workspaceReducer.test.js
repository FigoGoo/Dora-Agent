import { describe, expect, it } from 'vitest';
import {
  creationSpecPreviewCardFixture,
  creationSpecPreviewFailedEventFixture,
  directResponseCardFixture,
  inputAcceptedEventFixture,
  turnFailureCardFixture,
  WORKSPACE_IDS
} from '../../test/workspaceFixtures.js';
import { parseCreationSpecPreviewCard, parseCreationSpecPreviewFailure } from '../aigc/creationSpecPreviewContract.js';
import { parseDirectResponseCard, parseTurnFailureCard } from './turnOutputContract.js';
import {
  classifyWorkspaceEvent,
  classifyWorkspaceSnapshot,
  createWorkspaceState,
  workspaceReducer
} from './workspaceReducer.js';

describe('Workspace reducer', () => {
  it('advances only an exactly continuous accepted event and detects unsafe duplicates/gaps', () => {
    const ready = {
      ...createWorkspaceState(),
      kind: 'ready',
      cursor: 1,
      eventIDsBySeq: {},
      snapshot: { inputs: [{ id: WORKSPACE_IDS.input, status: 'pending' }] }
    };
    const event = mappedEvent();
    expect(classifyWorkspaceEvent(ready, event)).toBe('apply');
    const applied = workspaceReducer(ready, { type: 'event_applied', event });
    expect(applied.cursor).toBe(2);
    expect(classifyWorkspaceEvent(applied, event)).toBe('duplicate');
    expect(classifyWorkspaceEvent(applied, { ...event, eventID: '019f0000-0000-7000-8000-000000000099' })).toBe('reset');
    expect(classifyWorkspaceEvent(ready, { ...event, seq: 4 })).toBe('reset');
  });

  it('clears verified data on unauthorized/not_found but retains it read-only during reset/offline', () => {
    const ready = { ...createWorkspaceState(), kind: 'ready', project: { projectID: WORKSPACE_IDS.project }, snapshot: { session: {} } };
    expect(workspaceReducer(ready, { type: 'reset' }).snapshot).toBe(ready.snapshot);
    expect(workspaceReducer(ready, { type: 'offline' }).snapshot).toBe(ready.snapshot);
    expect(workspaceReducer(ready, { type: 'unauthorized' }).snapshot).toBeNull();
    expect(workspaceReducer(ready, { type: 'not_found' }).project).toBeNull();
  });

  it('applies completed/failed Preview events and resets before a same-resource rollback', () => {
    const v2 = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({ version: 2, content_digest: 'b'.repeat(64) }));
    const v3 = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({ version: 3, content_digest: 'c'.repeat(64) }));
    const ready = {
      ...createWorkspaceState(),
      kind: 'ready',
      cursor: 10,
      snapshot: { inputs: [], creationSpecPreview: v2, creationSpecPreviewFailure: null }
    };
    const upgraded = workspaceReducer(ready, { type: 'event_applied', event: {
      eventID: WORKSPACE_IDS.event, seq: 11, event: 'creation_spec.preview.completed', payload: v3
    } });
    expect(upgraded.snapshot.creationSpecPreview.version).toBe(3);

    const staleEvent = {
      eventID: '019f0000-0000-7000-8000-000000000019', seq: 12,
      event: 'creation_spec.preview.completed', payload: v2
    };
    expect(classifyWorkspaceEvent(upgraded, staleEvent)).toBe('reset');

    const failure = parseCreationSpecPreviewFailure(creationSpecPreviewFailedEventFixture().payload);
    const failed = workspaceReducer(upgraded, { type: 'event_applied', event: {
      eventID: '019f0000-0000-7000-8000-000000000020', seq: 12,
      event: 'creation_spec.preview.failed', payload: failure
    } });
    expect(failed.snapshot.creationSpecPreview.version).toBe(3);
    expect(failed.snapshot.creationSpecPreviewFailure).toMatchObject({ inputID: WORKSPACE_IDS.previewInput });
  });

  it('accepts a new v1 resource and rejects ambiguous same-resource Snapshot projections', () => {
    const oldResource = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      version: 3,
      content_digest: 'c'.repeat(64)
    }));
    const newResource = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      creation_spec_id: '019f0000-0000-7000-8000-000000000091',
      version: 1,
      content_digest: 'd'.repeat(64)
    }));
    const ready = {
      ...createWorkspaceState(),
      kind: 'ready',
      cursor: 10,
      project: { projectID: WORKSPACE_IDS.project },
      snapshot: {
        session: { id: WORKSPACE_IDS.session },
        creationSpecPreview: oldResource,
        creationSpecPreviewFailure: null
      }
    };

    const replaced = workspaceReducer(ready, { type: 'event_applied', event: {
      eventID: WORKSPACE_IDS.event,
      seq: 11,
      event: 'creation_spec.preview.completed',
      payload: newResource
    } });
    expect(replaced.snapshot.creationSpecPreview).toBe(newResource);

    const sameVersionDifferentDigest = {
      session: { id: WORKSPACE_IDS.session },
      creationSpecPreview: parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
        creation_spec_id: newResource.creationSpecID,
        version: 1,
        content_digest: 'e'.repeat(64)
      })),
      creationSpecPreviewFailure: null,
      eventHighWatermark: 12
    };
    expect(classifyWorkspaceSnapshot(replaced, ready.project, sameVersionDifferentDigest)).toBe('invalid');
    expect(classifyWorkspaceSnapshot(replaced, ready.project, {
      ...sameVersionDifferentDigest,
      creationSpecPreview: null,
      eventHighWatermark: 13
    })).toBe('invalid');

    const v2 = parseCreationSpecPreviewCard(creationSpecPreviewCardFixture({
      creation_spec_id: newResource.creationSpecID,
      version: 2,
      content_digest: 'f'.repeat(64)
    }));
    const advanced = workspaceReducer(replaced, { type: 'event_applied', event: {
      eventID: '019f0000-0000-7000-8000-000000000092',
      seq: 12,
      event: 'creation_spec.preview.completed',
      payload: v2
    } });
    expect(classifyWorkspaceSnapshot(advanced, ready.project, {
      session: { id: WORKSPACE_IDS.session },
      creationSpecPreview: newResource,
      creationSpecPreviewFailure: null,
      eventHighWatermark: 13
    })).toBe('invalid');
  });

  it('applies completed/failed/recovery Turn events to the Card and matching Input status', () => {
    const direct = parseDirectResponseCard(directResponseCardFixture());
    const failedCard = parseTurnFailureCard(turnFailureCardFixture());
    const recoveryCard = parseTurnFailureCard(turnFailureCardFixture({
      status: 'recovery_pending', error_code: 'MODEL_RESULT_UNKNOWN', retryable: true
    }));
    const ready = {
      ...createWorkspaceState(),
      kind: 'ready',
      cursor: 2,
      snapshot: {
        eventHighWatermark: 2,
        latestTurnOutput: null,
        inputs: [{ id: WORKSPACE_IDS.input, enqueueSeq: 1, status: 'running' }]
      }
    };
    const recovered = workspaceReducer(ready, { type: 'event_applied', event: {
      eventID: WORKSPACE_IDS.turnEvent, seq: 3, event: 'session.turn.recovery_pending', payload: recoveryCard
    } });
    expect(recovered.snapshot.latestTurnOutput).toBe(recoveryCard);
    expect(recovered.snapshot.inputs[0].status).toBe('recovery_pending');

    const completed = workspaceReducer(recovered, { type: 'event_applied', event: {
      eventID: '019f0000-0000-7000-8000-000000000015', seq: 4,
      event: 'session.turn.completed', payload: direct
    } });
    expect(completed.snapshot.latestTurnOutput).toBe(direct);
    expect(completed.snapshot.inputs[0].status).toBe('resolved');

    const fresh = { ...ready, snapshot: { ...ready.snapshot, inputs: [{ ...ready.snapshot.inputs[0] }] } };
    const failed = workspaceReducer(fresh, { type: 'event_applied', event: {
      eventID: '019f0000-0000-7000-8000-000000000016', seq: 3,
      event: 'session.turn.failed', payload: failedCard
    } });
    expect(failed.snapshot.inputs[0].status).toBe('dead');
  });

  it('resets on an unbound or terminally ambiguous Turn Output', () => {
    const direct = parseDirectResponseCard(directResponseCardFixture());
    const failed = parseTurnFailureCard(turnFailureCardFixture());
    const ready = {
      ...createWorkspaceState(),
      kind: 'ready', cursor: 3,
      project: { projectID: WORKSPACE_IDS.project },
      snapshot: {
        session: { id: WORKSPACE_IDS.session },
        eventHighWatermark: 3,
        latestTurnOutput: direct,
        inputs: [{ id: WORKSPACE_IDS.input, enqueueSeq: 1, status: 'resolved' }]
      }
    };
    expect(classifyWorkspaceEvent(ready, {
      eventID: WORKSPACE_IDS.turnEvent, seq: 4, event: 'session.turn.failed', payload: failed
    })).toBe('reset');
    expect(classifyWorkspaceEvent(ready, {
      eventID: WORKSPACE_IDS.turnEvent, seq: 4, event: 'session.turn.completed',
      payload: { ...direct, inputID: '019f0000-0000-7000-8000-000000000099' }
    })).toBe('reset');
    expect(classifyWorkspaceSnapshot(ready, { projectID: WORKSPACE_IDS.project }, {
      eventHighWatermark: 4,
      latestTurnOutput: null,
      inputs: ready.snapshot.inputs,
      session: { id: WORKSPACE_IDS.session }
    })).toBe('invalid');
  });
});

function mappedEvent() {
  const event = inputAcceptedEventFixture();
  return {
    eventID: event.event_id,
    seq: event.seq,
    event: event.event,
    payload: event.payload
  };
}
