import { describe, expect, it } from 'vitest';
import {
  normalizeWritePromptsPreviewEnqueueRequest,
  parsePromptPreviewCard,
  parseWritePromptsPreviewEnqueueRequest,
  parseWritePromptsPreviewEnqueueResponse,
  validatePromptPreviewSourceBinding
} from './writePromptsPreviewContract.js';
import { promptPreviewCardFixture, storyboardPreviewCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';

describe('Write Prompts Preview strict contract', () => {
  it('normalizes only the trusted Storyboard ref and model-controllable writing fields', () => {
    expect(normalizeWritePromptsPreviewEnqueueRequest({
      storyboardPreviewRef: { id: WORKSPACE_IDS.storyboardPreview, version: 1, contentDigest: 'b'.repeat(64) },
      toolIntent: { writingInstruction: '  为全部槽位编写提示词  ', outputLanguage: 'zh-CN' }
    })).toEqual({
      schema_version: 'write_prompts.preview.enqueue-request.v1',
      storyboard_preview_ref: { id: WORKSPACE_IDS.storyboardPreview, version: 1, content_digest: 'b'.repeat(64) },
      tool_intent: {
        schema_version: 'write_prompts.preview.intent.v1',
        writing_instruction: '为全部槽位编写提示词',
        output_language: 'zh-CN'
      }
    });
  });

  it('fails closed on unknown request fields, untrusted target selection, non-NFC text and languages', () => {
    const request = enqueueRequest();
    expect(() => parseWritePromptsPreviewEnqueueRequest({ ...request, project_id: WORKSPACE_IDS.project })).toThrow('字段集合');
    expect(() => parseWritePromptsPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, target_local_keys: ['slot_1'] }
    })).toThrow('字段集合');
    expect(() => parseWritePromptsPreviewEnqueueRequest({
      ...request,
      storyboard_preview_ref: { ...request.storyboard_preview_ref, version: 2 }
    })).toThrow('version');
    expect(() => parseWritePromptsPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, writing_instruction: 'e\u0301' }
    })).toThrow('NFC');
    expect(() => parseWritePromptsPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, output_language: 'auto' }
    })).toThrow('未知枚举');
  });

  it('parses the completed exact-set Card and rejects recursive drift', () => {
    const parsed = parsePromptPreviewCard(promptPreviewCardFixture());
    expect(parsed).toMatchObject({
      kind: 'write_prompts_preview',
      status: 'completed',
      promptPreviewID: WORKSPACE_IDS.promptPreview,
      projectID: WORKSPACE_IDS.project,
      targetCount: 2
    });
    expect(parsed.prompts.map((prompt) => prompt.targetLocalKey)).toEqual(['slot_1', 'slot_2']);

    const base = promptPreviewCardFixture();
    expect(() => parsePromptPreviewCard({ ...base, ready: true })).toThrow('字段集合');
    expect(() => parsePromptPreviewCard({
      ...base,
      storyboard_preview_ref: { ...base.storyboard_preview_ref, status: 'draft' }
    })).toThrow('字段集合');
    expect(() => parsePromptPreviewCard({ ...base, target_count: 1 })).toThrow('target_count');
    expect(() => parsePromptPreviewCard({
      ...base,
      prompts: [{ ...base.prompts[0], provider: 'forbidden' }, base.prompts[1]]
    })).toThrow('字段集合');
    expect(() => parsePromptPreviewCard({
      ...base,
      prompts: [{ ...base.prompts[0], media_kind: 'video' }, base.prompts[1]]
    })).toThrow('slot_type');
    expect(() => parsePromptPreviewCard({
      ...base,
      prompts: [{ ...base.prompts[0], negative_constraints: ['重复', '重复'] }, base.prompts[1]]
    })).toThrow('重复项');
    expect(parsePromptPreviewCard({
      ...base,
      prompts: [base.prompts[1], base.prompts[0]]
    }).prompts.map((prompt) => prompt.targetLocalKey)).toEqual(['slot_2', 'slot_1']);
    expect(() => parsePromptPreviewCard({
      ...base,
      prompts: [{ ...base.prompts[0], output_language: 'ja-JP' }, base.prompts[1]]
    })).toThrow('未知枚举');
  });

  it('requires the Prompt target set to exactly match the current Storyboard Source Card', () => {
    const prompt = parsePromptPreviewCard(promptPreviewCardFixture());
    const storyboard = {
      ...storyboardPreviewCardFixture(),
      kind: 'plan_storyboard_preview',
      schemaVersion: 'storyboard.preview.card.v1',
      status: 'completed',
      storyboardPreviewID: WORKSPACE_IDS.storyboardPreview,
      projectID: WORKSPACE_IDS.project,
      version: 1,
      contentDigest: 'b'.repeat(64),
      elements: storyboardPreviewCardFixture().elements.map((item) => ({
        key: item.key, order: item.order
      })),
      slots: storyboardPreviewCardFixture().slots.map((item) => ({
        key: item.key,
        elementKey: item.element_key,
        slotType: item.slot_type,
        purpose: item.purpose,
        required: item.required
      }))
    };
    expect(validatePromptPreviewSourceBinding(prompt, storyboard)).toBe(prompt);
    expect(() => validatePromptPreviewSourceBinding(prompt, {
      ...storyboard,
      contentDigest: 'c'.repeat(64)
    })).toThrow('Source Binding');
    expect(() => validatePromptPreviewSourceBinding(prompt, {
      ...storyboard,
      slots: storyboard.slots.slice(0, 1)
    })).toThrow('完整 Slot');
    expect(() => validatePromptPreviewSourceBinding(parsePromptPreviewCard(promptPreviewCardFixture({
      prompts: [
        { ...promptPreviewCardFixture().prompts[0], purpose: '伪造用途' },
        promptPreviewCardFixture().prompts[1]
      ]
    })), storyboard)).toThrow('可信字段');

    const crossElementStoryboard = {
      ...storyboard,
      slots: [
        { ...storyboard.slots[0], elementKey: 'element_2' },
        { ...storyboard.slots[1], elementKey: 'element_1' }
      ]
    };
    const crossElementCard = promptPreviewCardFixture({
      prompts: [
        {
          ...promptPreviewCardFixture().prompts[1],
          target_local_key: 'slot_2', element_local_key: 'element_1', slot_type: 'video', media_kind: 'video',
          purpose: '功能演示画面', required: true
        },
        {
          ...promptPreviewCardFixture().prompts[0],
          target_local_key: 'slot_1', element_local_key: 'element_2', slot_type: 'image', media_kind: 'image',
          purpose: '产品主视觉', required: true
        }
      ]
    });
    expect(validatePromptPreviewSourceBinding(parsePromptPreviewCard(crossElementCard), crossElementStoryboard))
      .toMatchObject({ prompts: [{ targetLocalKey: 'slot_2' }, { targetLocalKey: 'slot_1' }] });
  });

  it('accepts only frozen failed codes and an exact pending enqueue response', () => {
    expect(parsePromptPreviewCard({
      schema_version: 'prompt.preview.card.v1',
      input_id: WORKSPACE_IDS.promptInput,
      turn_id: WORKSPACE_IDS.promptTurn,
      run_id: WORKSPACE_IDS.promptRun,
      tool_call_id: WORKSPACE_IDS.promptToolCall,
      status: 'failed',
      result_code: 'WRITE_PROMPTS_RUNTIME_FAILED',
      updated_at: '2026-07-17T11:00:00Z',
      failure_kind: 'runtime',
      summary: '运行时未完成。',
      retryable: true
    })).toMatchObject({ status: 'failed', failureKind: 'runtime' });
    expect(() => parsePromptPreviewCard({
      schema_version: 'prompt.preview.card.v1',
      input_id: WORKSPACE_IDS.promptInput,
      turn_id: WORKSPACE_IDS.promptTurn,
      run_id: WORKSPACE_IDS.promptRun,
      tool_call_id: WORKSPACE_IDS.promptToolCall,
      status: 'failed',
      result_code: 'PROMPT_PREVIEW_INTERNAL',
      updated_at: '2026-07-17T11:00:00Z',
      failure_kind: 'tool',
      summary: '内部错误不应投影。',
      retryable: false
    })).toThrow('未知枚举');

    const parsed = parseWritePromptsPreviewEnqueueResponse(enqueueResponse(), WORKSPACE_IDS.session);
    expect(parsed).toMatchObject({ inputID: WORKSPACE_IDS.promptInput, status: 'pending', replayed: false });
    expect(() => parseWritePromptsPreviewEnqueueResponse({ ...enqueueResponse(), status: 'completed' }, WORKSPACE_IDS.session))
      .toThrow('status');
  });
});

function enqueueRequest() {
  return {
    schema_version: 'write_prompts.preview.enqueue-request.v1',
    storyboard_preview_ref: { id: WORKSPACE_IDS.storyboardPreview, version: 1, content_digest: 'b'.repeat(64) },
    tool_intent: {
      schema_version: 'write_prompts.preview.intent.v1',
      writing_instruction: '为全部槽位编写提示词',
      output_language: 'zh-CN'
    }
  };
}

function enqueueResponse() {
  return {
    schema_version: 'write_prompts.preview.enqueue.v1',
    request_id: WORKSPACE_IDS.request,
    session_id: WORKSPACE_IDS.session,
    input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn,
    run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall,
    status: 'pending',
    replayed: false
  };
}
