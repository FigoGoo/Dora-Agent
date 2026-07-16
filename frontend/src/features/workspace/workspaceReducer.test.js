import { describe, expect, it } from 'vitest';
import { inputAcceptedEventFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { classifyWorkspaceEvent, createWorkspaceState, workspaceReducer } from './workspaceReducer.js';

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
