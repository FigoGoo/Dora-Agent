import { describe, expect, it } from 'vitest';
import {
  directResponseCardFixture,
  inputFixture,
  messageFixture,
  storyboardAcceptedEventFixture,
  storyboardEventFixture,
  storyboardPreviewCardFixture,
  WORKSPACE_IDS,
  workspaceSnapshotFixture,
  workspaceSnapshotV3Fixture
} from '../../test/workspaceFixtures.js';
import { parsePersistentWorkspaceEvent, parseWorkspaceSnapshot } from './workspaceContract.js';
import { classifyWorkspaceEvent, classifyWorkspaceSnapshot, createWorkspaceState, workspaceReducer } from './workspaceReducer.js';

const binding = { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session };
const messageEvent = (type, seq, value) => ({ type, lastEventId: String(seq), data: JSON.stringify(value) });

describe('Plan Storyboard Workspace projection', () => {
  it('inherits every v2 projection while adding the v3 Storyboard field', () => {
    const parsed = parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({
      messages: [messageFixture()],
      inputs: [inputFixture({ status: 'resolved' })],
      latest_turn_output: directResponseCardFixture(),
      event_high_watermark: 3
    }), binding);
    expect(parsed.latestTurnOutput).toMatchObject({ kind: 'direct_response', inputID: WORKSPACE_IDS.input });
    expect(parsed.analyzeMaterialsPreview).toBeNull();
    expect(parsed.planStoryboardPreview).toBeNull();
  });

  it('adds the nullable Card only in session.workspace.v3 and requires a message-less typed Input', () => {
    const parsed = parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({
      inputs: [storyboardInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      event_high_watermark: 3
    }), binding);
    expect(parsed).toMatchObject({
      schemaVersion: 'session.workspace.v3',
      planStoryboardPreview: {
        kind: 'plan_storyboard_preview',
        status: 'completed',
        inputID: WORKSPACE_IDS.storyboardInput,
        projectID: WORKSPACE_IDS.project
      }
    });
    expect(parsed.inputs[0].messageID).toBe('');

    expect(() => parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({
      inputs: [storyboardInput('resolved', { message_id: WORKSPACE_IDS.message })],
      plan_storyboard_preview: storyboardPreviewCardFixture()
    }), binding)).toThrow('source_type/message_id');
    expect(() => parseWorkspaceSnapshot({
      ...workspaceSnapshotFixture(),
      plan_storyboard_preview: null
    }, binding)).toThrow('字段集合');
  });

  it('strictly consumes accepted then terminal SSE without treating accepted as a completed Card', () => {
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({ inputs: [storyboardInput('pending')] }), binding);
    const ready = { ...createWorkspaceState(), kind: 'ready', cursor: 1, snapshot };
    const accepted = parsePersistentWorkspaceEvent(messageEvent(
      'plan_storyboard.preview.accepted', 2, storyboardAcceptedEventFixture()
    ), binding);
    const afterAccepted = workspaceReducer(ready, { type: 'event_applied', event: accepted });
    expect(afterAccepted.cursor).toBe(2);
    expect(afterAccepted.snapshot.planStoryboardPreview).toBeNull();

    const completed = parsePersistentWorkspaceEvent(messageEvent(
      'plan_storyboard.preview.completed', 3, storyboardEventFixture()
    ), binding);
    expect(classifyWorkspaceEvent(afterAccepted, completed)).toBe('apply');
    const afterCompleted = workspaceReducer(afterAccepted, { type: 'event_applied', event: completed });
    expect(afterCompleted.snapshot.planStoryboardPreview).toMatchObject({
      status: 'completed', storyboardPreviewID: WORKSPACE_IDS.storyboardPreview
    });
    expect(afterCompleted.snapshot.inputs[0].status).toBe('resolved');
  });

  it.each([
    ['plan_storyboard.preview.failed', 'tool', 'resolved'],
    ['plan_storyboard.preview.runtime_failed', 'runtime', 'dead']
  ])('maps %s to the frozen failed Card and correct Input terminal state', (eventName, failureKind, inputStatus) => {
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({ inputs: [storyboardInput('running')] }), binding);
    const ready = { ...createWorkspaceState(), kind: 'ready', cursor: 2, snapshot };
    const terminal = parsePersistentWorkspaceEvent(messageEvent(
      eventName, 3, storyboardEventFixture(eventName)
    ), binding);
    expect(terminal.payload).toMatchObject({ status: 'failed', failureKind });
    const applied = workspaceReducer(ready, { type: 'event_applied', event: terminal });
    expect(applied.snapshot.planStoryboardPreview.failureKind).toBe(failureKind);
    expect(applied.snapshot.inputs[0].status).toBe(inputStatus);
  });

  it('fails closed on Project/Aggregate/status drift and terminal rewrites for the same Turn', () => {
    expect(() => parsePersistentWorkspaceEvent(messageEvent(
      'plan_storyboard.preview.completed', 3,
      storyboardEventFixture('plan_storyboard.preview.completed', { aggregate_id: WORKSPACE_IDS.storyboardTurn })
    ), binding)).toThrow(/Aggregate/);
    expect(() => parsePersistentWorkspaceEvent(messageEvent(
      'plan_storyboard.preview.runtime_failed', 3,
      storyboardEventFixture('plan_storyboard.preview.runtime_failed', { payload: { failure_kind: 'tool' } })
    ), binding)).toThrow(/failure_kind|result_code/);
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({
      inputs: [storyboardInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture({
        project_id: '019f0000-0000-7000-8000-000000000099'
      })
    }), binding)).toThrow('Project Binding');

    const first = parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({
      inputs: [storyboardInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      event_high_watermark: 3
    }), binding);
    const ready = {
      ...createWorkspaceState(), kind: 'ready', cursor: 3,
      project: { projectID: WORKSPACE_IDS.project }, snapshot: first
    };
    const rewritten = parsePersistentWorkspaceEvent(messageEvent(
      'plan_storyboard.preview.completed', 4,
      storyboardEventFixture('plan_storyboard.preview.completed', {
        seq: 4,
        payload: { summary: '同一 Turn 被异义改写。' }
      })
    ), binding);
    expect(classifyWorkspaceEvent(ready, rewritten)).toBe('reset');
    expect(classifyWorkspaceSnapshot(ready, ready.project, parseWorkspaceSnapshot(workspaceSnapshotV3Fixture({
      inputs: [storyboardInput('resolved')],
      plan_storyboard_preview: null,
      event_high_watermark: 4
    }), binding))).toBe('invalid');
  });
});

function storyboardInput(status, overrides = {}) {
  return inputFixture({
    id: WORKSPACE_IDS.storyboardInput,
    message_id: null,
    source_type: 'plan_storyboard_preview',
    status,
    ...overrides
  });
}
