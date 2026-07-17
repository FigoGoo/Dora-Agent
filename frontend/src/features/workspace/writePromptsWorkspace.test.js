import { describe, expect, it } from 'vitest';
import {
  inputFixture,
  promptAcceptedEventFixture,
  promptEventFixture,
  promptPreviewCardFixture,
  storyboardEventFixture,
  storyboardPreviewCardFixture,
  WORKSPACE_IDS,
  workspaceSnapshotV4Fixture
} from '../../test/workspaceFixtures.js';
import { parsePersistentWorkspaceEvent, parseWorkspaceSnapshot } from './workspaceContract.js';
import { classifyWorkspaceEvent, classifyWorkspaceSnapshot, createWorkspaceState, workspaceReducer } from './workspaceReducer.js';

const binding = { expectedProjectID: WORKSPACE_IDS.project, expectedSessionID: WORKSPACE_IDS.session };
const messageEvent = (type, seq, value) => ({ type, lastEventId: String(seq), data: JSON.stringify(value) });

describe('Write Prompts Workspace projection', () => {
  it('restores the nullable V4 Card on hard refresh with exact Source and message-less Input binding', () => {
    const parsed = parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      write_prompts_preview: promptPreviewCardFixture(),
      event_high_watermark: 5
    }), binding);
    expect(parsed).toMatchObject({
      schemaVersion: 'session.workspace.v4',
      writePromptsPreview: {
        kind: 'write_prompts_preview',
        status: 'completed',
        promptPreviewID: WORKSPACE_IDS.promptPreview,
        targetCount: 2
      }
    });
    expect(parsed.inputs[1].messageID).toBe('');
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('resolved', { message_id: WORKSPACE_IDS.message })],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      write_prompts_preview: promptPreviewCardFixture()
    }), binding)).toThrow('source_type/message_id');
  });

  it('strictly applies accepted then terminal, while accepted never overwrites a terminal Card', () => {
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('pending')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      event_high_watermark: 3
    }), binding);
    const ready = { ...createWorkspaceState(), kind: 'ready', cursor: 3, snapshot };
    const accepted = parsePersistentWorkspaceEvent(messageEvent(
      'write_prompts.preview.accepted', 4, promptAcceptedEventFixture()
    ), binding);
    const afterAccepted = workspaceReducer(ready, { type: 'event_applied', event: accepted });
    expect(afterAccepted.snapshot.writePromptsPreview).toBeNull();

    const completed = parsePersistentWorkspaceEvent(messageEvent(
      'write_prompts.preview.completed', 5, promptEventFixture()
    ), binding);
    expect(classifyWorkspaceEvent(afterAccepted, completed)).toBe('apply');
    const terminal = workspaceReducer(afterAccepted, { type: 'event_applied', event: completed });
    expect(terminal.snapshot.writePromptsPreview).toMatchObject({ status: 'completed', targetCount: 2 });
    expect(terminal.snapshot.inputs[1].status).toBe('resolved');

    const lateAcceptedWire = promptAcceptedEventFixture({ seq: 6, event_id: WORKSPACE_IDS.request });
    const lateAccepted = parsePersistentWorkspaceEvent(messageEvent('write_prompts.preview.accepted', 6, lateAcceptedWire), binding);
    const afterLateAccepted = workspaceReducer(terminal, { type: 'event_applied', event: lateAccepted });
    expect(afterLateAccepted.snapshot.writePromptsPreview).toEqual(terminal.snapshot.writePromptsPreview);
  });

  it.each([
    ['write_prompts.preview.failed', 'tool', 'resolved'],
    ['write_prompts.preview.runtime_failed', 'runtime', 'dead']
  ])('maps %s to the strict terminal Card and Input state', (eventName, failureKind, inputStatus) => {
    const snapshot = parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('running')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      event_high_watermark: 4
    }), binding);
    const ready = { ...createWorkspaceState(), kind: 'ready', cursor: 4, snapshot };
    const event = parsePersistentWorkspaceEvent(messageEvent(eventName, 5, promptEventFixture(eventName)), binding);
    const applied = workspaceReducer(ready, { type: 'event_applied', event });
    expect(applied.snapshot.writePromptsPreview.failureKind).toBe(failureKind);
    expect(applied.snapshot.inputs[1].status).toBe(inputStatus);
  });

  it('fails closed on unknown fields, Source/target drift and terminal rewrites or disappearance', () => {
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      write_prompts_preview: promptPreviewCardFixture({
        storyboard_preview_ref: { id: WORKSPACE_IDS.storyboardPreview, version: 1, content_digest: 'c'.repeat(64) }
      })
    }), binding)).toThrow('Source Binding');
    expect(() => parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      write_prompts_preview: promptPreviewCardFixture({ unknown: true })
    }), binding)).toThrow('字段集合');

    const first = parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      write_prompts_preview: promptPreviewCardFixture(),
      event_high_watermark: 5
    }), binding);
    const ready = { ...createWorkspaceState(), kind: 'ready', cursor: 5, project: { projectID: WORKSPACE_IDS.project }, snapshot: first };
    const rewritten = parsePersistentWorkspaceEvent(messageEvent(
      'write_prompts.preview.completed', 6,
      promptEventFixture('write_prompts.preview.completed', {
        seq: 6,
        payload: {
          prompts: [
            { ...promptPreviewCardFixture().prompts[0], positive_prompt: '异义重写' },
            promptPreviewCardFixture().prompts[1]
          ]
        }
      })
    ), binding);
    expect(classifyWorkspaceEvent(ready, rewritten)).toBe('reset');
    expect(classifyWorkspaceSnapshot(ready, ready.project, parseWorkspaceSnapshot(workspaceSnapshotV4Fixture({
      inputs: [storyboardInput('resolved'), promptInput('resolved')],
      plan_storyboard_preview: storyboardPreviewCardFixture(),
      write_prompts_preview: null,
      event_high_watermark: 6
    }), binding))).toBe('invalid');

    const replacementStoryboard = parsePersistentWorkspaceEvent(messageEvent(
      'plan_storyboard.preview.completed', 6,
      storyboardEventFixture('plan_storyboard.preview.completed', {
        seq: 6,
        event_id: WORKSPACE_IDS.request,
        payload: { storyboard_preview_id: '019f0000-0000-7000-8000-000000000099' }
      })
    ), binding);
    expect(classifyWorkspaceEvent(ready, replacementStoryboard)).toBe('reset');
  });
});

function storyboardInput(status) {
  return inputFixture({
    id: WORKSPACE_IDS.storyboardInput,
    message_id: null,
    source_type: 'plan_storyboard_preview',
    status
  });
}

function promptInput(status, overrides = {}) {
  return inputFixture({
    id: WORKSPACE_IDS.promptInput,
    message_id: null,
    source_type: 'write_prompts_preview',
    enqueue_seq: 2,
    status,
    ...overrides
  });
}
