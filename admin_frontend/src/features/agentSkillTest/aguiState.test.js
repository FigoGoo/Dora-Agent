import { describe, expect, test } from 'vitest';
import { applySnapshot, createAguiState, reduceAguiEvent, reduceAguiEvents } from './aguiState.js';

describe('AG-UI state reducer', () => {
  test('deduplicates events and merges message deltas', () => {
    const event = {
      event_id: 'evt_1',
      type: 'agent.message.delta',
      sequence: 1,
      payload: { message_id: 'msg_1', role: 'assistant', content_type: 'text', text_delta: '你好' }
    };
    const state = reduceAguiEvents(createAguiState(), [
      event,
      event,
      {
        event_id: 'evt_2',
        type: 'agent.message.delta',
        sequence: 2,
        payload: { message_id: 'msg_1', text_delta: ' Dora' }
      },
      {
        event_id: 'evt_3',
        type: 'agent.message.completed',
        sequence: 3,
        payload: { message_id: 'msg_1' }
      }
    ]);

    expect(state.events).toHaveLength(3);
    expect(state.messages[0]).toMatchObject({ message_id: 'msg_1', content: '你好 Dora', final: true });
    expect(state.lastSequence).toBe(3);
  });

  test('tracks selected skill, tool calls, confirmation and snapshot', () => {
    let state = reduceAguiEvents(createAguiState(), [
      { event_id: 'evt_skill', type: 'agent.skill.selected', sequence: 1, payload: { skill_id: 'sk_1', matched_reason: 'selected_skill_id' } },
      { event_id: 'evt_tool', type: 'tool.call.started', sequence: 2, payload: { tool_call_id: 'tool_1', tool_name: 'web_fetch' } },
      { event_id: 'evt_confirm', type: 'confirmation.required', sequence: 3, payload: { interrupt_id: 'int_1', payload_digest: 'sha256:1' } }
    ]);
    state = reduceAguiEvent(state, { event_id: 'evt_tool_done', type: 'tool.call.completed', sequence: 4, payload: { tool_call_id: 'tool_1', status: 'completed' } });
    state = applySnapshot(state, {
      run: { status: 'waiting_confirmation' },
      messages: [{ message_id: 'msg_snapshot', role: 'assistant', content_type: 'text', content: 'snapshot' }],
      assets: [{ asset_id: 'ast_1' }],
      blackboard: { blackboard_version: 'bb_1' },
      tasks: [{ task_id: 'task_1' }],
      last_event_sequence: 9,
      interrupt: { interrupt_id: 'int_snapshot', payload_digest: 'sha256:2' }
    });

    expect(state.selectedSkill.skill_id).toBe('sk_1');
    expect(state.tools[0]).toMatchObject({ tool_call_id: 'tool_1', status: 'completed' });
    expect(state.confirmation.interrupt_id).toBe('int_snapshot');
    expect(state.messages[0].content).toBe('snapshot');
    expect(state.lastSequence).toBe(9);
  });
});
