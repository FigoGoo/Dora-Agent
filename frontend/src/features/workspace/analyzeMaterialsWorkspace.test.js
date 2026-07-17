import { describe, expect, it } from 'vitest';
import {
  analyzeMaterialsAcceptedEventFixture,
  analyzeMaterialsEventFixture,
  analyzeMaterialsPreviewCardFixture,
  inputFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture
} from '../../test/workspaceFixtures.js';
import { parseWorkspaceSnapshot, parsePersistentWorkspaceEvent } from './workspaceContract.js';
import { classifyWorkspaceEvent, createWorkspaceState, workspaceReducer } from './workspaceReducer.js';

const binding = { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session };
const messageEvent = (type, seq, value) => ({ type, lastEventId: String(seq), data: JSON.stringify(value) });

describe('Analyze Materials Workspace projection', () => {
  it('parses nullable Snapshot and requires analyze Input message_id=null', () => {
    const parsed = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      inputs: [inputFixture({ source_type: 'analyze_materials_preview', message_id: null, status: 'resolved' })],
      analyze_materials_preview: analyzeMaterialsPreviewCardFixture(), event_high_watermark: 3
    }), binding);
    expect(parsed.analyzeMaterialsPreview).toMatchObject({ status: 'completed', inputID: WORKSPACE_IDS.input });
    expect(parsed.inputs[0].messageID).toBe('');
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotFixture({
      inputs: [inputFixture({ source_type: 'analyze_materials_preview' })]
    }), binding)).toThrow('source_type/message_id');
  });

  it('accepted only advances cursor and terminal events create the read-only Card', () => {
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotFixture({
      inputs: [inputFixture({ source_type: 'analyze_materials_preview', message_id: null })]
    }), binding);
    const ready = { ...createWorkspaceState(), kind: 'ready', cursor: 1, snapshot };
    const accepted = parsePersistentWorkspaceEvent(messageEvent(
      'analyze_materials.preview.accepted', 2, analyzeMaterialsAcceptedEventFixture()
    ), binding);
    const afterAccepted = workspaceReducer(ready, { type: 'event_applied', event: accepted });
    expect(afterAccepted.cursor).toBe(2);
    expect(afterAccepted.snapshot.analyzeMaterialsPreview).toBeNull();

    const completed = parsePersistentWorkspaceEvent(messageEvent(
      'analyze_materials.preview.completed', 3, analyzeMaterialsEventFixture()
    ), binding);
    expect(classifyWorkspaceEvent(afterAccepted, completed)).toBe('apply');
    const afterCompleted = workspaceReducer(afterAccepted, { type: 'event_applied', event: completed });
    expect(afterCompleted.snapshot.analyzeMaterialsPreview.status).toBe('completed');
    expect(afterCompleted.snapshot.inputs[0].status).toBe('resolved');
  });

  it('enforces event/status/failure_kind mapping and same-turn immutability', () => {
    const invalid = analyzeMaterialsEventFixture('analyze_materials.preview.runtime_failed', {
      payload: { failure_kind: 'tool' }
    });
    expect(() => parsePersistentWorkspaceEvent(messageEvent('analyze_materials.preview.runtime_failed', 3, invalid), binding))
      .toThrow(/failure_kind/);
  });
});
