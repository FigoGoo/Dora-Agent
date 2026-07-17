import { describe, expect, it } from 'vitest';
import {
  normalizePlanStoryboardPreviewEnqueueRequest,
  parsePlanStoryboardPreviewEnqueueRequest,
  parsePlanStoryboardPreviewEnqueueResponse,
  parseStoryboardPreviewCard,
  parseStoryboardPreviewProjection,
  PlanStoryboardPreviewContractError
} from './planStoryboardPreviewContract.js';

const IDS = Object.freeze({
  project: '019f0000-0000-7000-8000-000000000001',
  session: '019f0000-0000-7000-8000-000000000002',
  request: '019f0000-0000-7000-8000-000000000003',
  input: '019f0000-0000-7000-8000-000000000004',
  turn: '019f0000-0000-7000-8000-000000000005',
  run: '019f0000-0000-7000-8000-000000000006',
  toolCall: '019f0000-0000-7000-8000-000000000007',
  creationSpec: '019f0000-0000-7000-8000-000000000008',
  storyboardPreview: '019f0000-0000-7000-8000-000000000009'
});

describe('Plan Storyboard Preview strict contract', () => {
  it('normalizes the split CreationSpec ref and Tool Intent without exposing trusted fields to the Intent', () => {
    expect(normalizePlanStoryboardPreviewEnqueueRequest({
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
      toolIntent: { planningInstruction: '  规划三段式故事板  ', targetDurationSeconds: '60' }
    })).toEqual({
      schema_version: 'plan_storyboard.preview.enqueue-request.v1',
      creation_spec_ref: { id: IDS.creationSpec, version: 1, content_digest: 'a'.repeat(64) },
      tool_intent: {
        schema_version: 'plan_storyboard.preview.intent.v1',
        planning_instruction: '规划三段式故事板',
        target_duration_seconds: 60
      }
    });
    expect(normalizePlanStoryboardPreviewEnqueueRequest({
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
      toolIntent: { planningInstruction: '规划故事板', targetDurationSeconds: '' }
    }).tool_intent).not.toHaveProperty('target_duration_seconds');
  });

  it('strictly rejects unknown nested fields, untrusted IDs in Tool Intent, non-NFC text, and invalid duration', () => {
    const request = enqueueRequest();
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({ ...request, user_id: IDS.project })).toThrow('字段集合');
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      creation_spec_ref: { ...request.creation_spec_ref, project_id: IDS.project }
    })).toThrow('字段集合');
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, creation_spec_id: IDS.creationSpec }
    })).toThrow('字段集合');
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, planning_instruction: 'e\u0301' }
    })).toThrow('NFC');
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, target_duration_seconds: 4 }
    })).toThrow('5 至 600');
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, planning_instruction: '\ud800' }
    })).toThrow('NFC Unicode');
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, planning_instruction: '\udc00' }
    })).toThrow('NFC Unicode');
  });

  it('counts Unicode scalars while preserving valid UTF-8 supplementary characters', () => {
    const request = enqueueRequest();
    const maximum = '😀'.repeat(1000);
    expect(parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, planning_instruction: maximum }
    }).tool_intent.planning_instruction).toBe(maximum);
    expect(() => parsePlanStoryboardPreviewEnqueueRequest({
      ...request,
      tool_intent: { ...request.tool_intent, planning_instruction: `${maximum}😀` }
    })).toThrow('长度');
  });

  it('accepts only an exact pending/replayed enqueue envelope bound to the requested Session', () => {
    const parsed = parsePlanStoryboardPreviewEnqueueResponse(enqueueResponse(), IDS.session);
    expect(parsed).toMatchObject({ inputID: IDS.input, turnID: IDS.turn, status: 'pending', replayed: false });
    expect(() => parsePlanStoryboardPreviewEnqueueResponse({ ...enqueueResponse(), status: 'completed' }, IDS.session))
      .toThrow('status');
    expect(() => parsePlanStoryboardPreviewEnqueueResponse({ ...enqueueResponse(), replayed: 'false' }, IDS.session))
      .toThrow('布尔值');
    expect(() => parsePlanStoryboardPreviewEnqueueResponse({ ...enqueueResponse(), extra: true }, IDS.session))
      .toThrow('字段集合');
  });

  it('parses a completed isolated JSON Draft with provenance and local-only keys', () => {
    const parsed = parseStoryboardPreviewCard(completedCard());
    expect(parsed).toMatchObject({
      kind: 'plan_storyboard_preview',
      status: 'completed',
      storyboardPreviewID: IDS.storyboardPreview,
      projectID: IDS.project,
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
      version: 1
    });
    expect(parsed.elements.map((element) => element.key)).toEqual(['element_1', 'element_2', 'element_3']);
    expect(parsed.slots.map((slot) => slot.key)).toEqual(['slot_1', 'slot_2']);
    expect(parsed).not.toHaveProperty('storyboardID');
  });

  it('fails closed on recursive unknown fields, UUID-like local IDs, order gaps, enums, references, and cycles', () => {
    const base = completedCard();
    expect(() => parseStoryboardPreviewCard({ ...base, prompt: 'forbidden' })).toThrow('字段集合');
    const { slots: _missingSlots, ...missingTopLevel } = base;
    expect(() => parseStoryboardPreviewCard(missingTopLevel)).toThrow('字段集合');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      creation_spec_ref: { ...base.creation_spec_ref, raw_resource_ref: IDS.project }
    })).toThrow('字段集合');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      sections: [{ ...base.sections[0], section_id: IDS.storyboardPreview }, base.sections[1]]
    })).toThrow('字段集合');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [{ ...base.elements[0], key: IDS.storyboardPreview }, ...base.elements.slice(1)]
    })).toThrow('局部 key');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [base.elements[0], { ...base.elements[1], order: 3 }, base.elements[2]]
    })).toThrow('全局连续');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [{ ...base.elements[0], element_type: 'prompt' }, ...base.elements.slice(1)]
    })).toThrow('未知枚举');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      slots: [{ ...base.slots[0], element_key: 'element_24' }, base.slots[1]]
    })).toThrow('未知 Element');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      slots: [{ ...base.slots[0], asset_id: IDS.project }, base.slots[1]]
    })).toThrow('字段集合');
    const missingSlotPurpose = { ...base.slots[0] };
    delete missingSlotPurpose.purpose;
    expect(() => parseStoryboardPreviewCard({
      ...base,
      slots: [missingSlotPurpose, base.slots[1]]
    })).toThrow('字段集合');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [{ ...base.elements[0], dependency_keys: ['element_24'] }, ...base.elements.slice(1)]
    })).toThrow('未知 Element');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [{ ...base.elements[0], dependency_keys: ['element_1'] }, ...base.elements.slice(1)]
    })).toThrow('不得自引用');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [base.elements[0], { ...base.elements[1], dependency_keys: ['element_1', 'element_1'] }, base.elements[2]]
    })).toThrow('重复项');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [
        { ...base.elements[0], dependency_keys: ['element_2'] },
        { ...base.elements[1], dependency_keys: ['element_1'] },
        base.elements[2]
      ]
    })).toThrow('依赖环');
  });

  it('rejects gaps in every local key space and more than eight dependencies', () => {
    const base = completedCard();
    expect(() => parseStoryboardPreviewCard({
      ...base,
      sections: [base.sections[0], { ...base.sections[1], key: 'section_3' }]
    })).toThrow('数组顺序连续');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: [base.elements[0], { ...base.elements[1], key: 'element_3' }, base.elements[2]]
    })).toThrow('数组顺序连续');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      slots: [base.slots[0], { ...base.slots[1], key: 'slot_3' }]
    })).toThrow('数组顺序连续');

    const tenElements = cardAtLimits({ sectionCount: 1, elementCount: 10, slotsPerElement: 0 });
    tenElements.elements[9] = {
      ...tenElements.elements[9],
      dependency_keys: tenElements.elements.slice(0, 9).map((element) => element.key)
    };
    expect(() => parseStoryboardPreviewCard(tenElements)).toThrow('数量');

    const nineElements = cardAtLimits({ sectionCount: 1, elementCount: 9, slotsPerElement: 0 });
    nineElements.elements[8] = {
      ...nineElements.elements[8],
      dependency_keys: nineElements.elements.slice(0, 8).map((element) => element.key)
    };
    expect(parseStoryboardPreviewCard(nineElements).elements[8].dependencyKeys).toHaveLength(8);
  });

  it('requires section coverage, a maximum of four slots per element, and a bounded total duration', () => {
    const base = completedCard();
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: base.elements.map((element) => ({ ...element, section_key: 'section_1' }))
    })).toThrow('空 Section');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      slots: [1, 2, 3, 4, 5].map((index) => ({
        key: `slot_${index}`, element_key: 'element_1', slot_type: 'image', purpose: `槽位 ${index}`, required: true
      }))
    })).toThrow('超过 4');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: base.elements.map((element) => ({ ...element, duration_seconds: 300 }))
    })).toThrow('总时长');
    expect(() => parseStoryboardPreviewCard({
      ...base,
      elements: base.elements.map((element) => ({ ...element, duration_seconds: 1 }))
    })).toThrow('5 至 600');
  });

  it('accepts frozen maximum collection counts and rejects each collection outside its boundary', () => {
    const maximum = cardAtLimits({ sectionCount: 8, elementCount: 24, slotsPerElement: 4 });
    const parsed = parseStoryboardPreviewCard(maximum);
    expect(parsed.sections).toHaveLength(8);
    expect(parsed.elements).toHaveLength(24);
    expect(parsed.slots).toHaveLength(96);
    expect(parseStoryboardPreviewCard({ ...completedCard(), slots: [] }).slots).toEqual([]);

    expect(() => parseStoryboardPreviewCard({ ...maximum, sections: [] })).toThrow('sections 数量');
    expect(() => parseStoryboardPreviewCard({
      ...maximum,
      sections: [...maximum.sections, { key: 'section_9', title: '越界', objective: '越界' }]
    })).toThrow('sections 数量');
    expect(() => parseStoryboardPreviewCard({ ...maximum, elements: [] })).toThrow('elements 数量');
    expect(() => parseStoryboardPreviewCard({
      ...maximum,
      elements: [...maximum.elements, { ...maximum.elements[23], key: 'element_25', order: 25 }]
    })).toThrow('elements 数量');
    expect(() => parseStoryboardPreviewCard({
      ...maximum,
      slots: [...maximum.slots, { ...maximum.slots[95], key: 'slot_97' }]
    })).toThrow('slots 数量');
  });

  it('enforces the Business 64 KiB canonical Content limit using actual UTF-8 bytes and Go HTML escaping', () => {
    const multibyte = cardAtLimits({
      sectionCount: 3,
      elementCount: 12,
      slotsPerElement: 4,
      slotPurpose: '界'.repeat(500)
    });
    expect(() => parseStoryboardPreviewCard(multibyte)).toThrow('64 KiB UTF-8');

    const goEscaped = cardAtLimits({
      sectionCount: 1,
      elementCount: 6,
      slotsPerElement: 4,
      slotPurpose: '<'.repeat(500)
    });
    expect(new TextEncoder().encode(JSON.stringify({
      title: goEscaped.title,
      summary: goEscaped.summary,
      sections: goEscaped.sections,
      elements: goEscaped.elements,
      slots: goEscaped.slots
    })).byteLength).toBeLessThan(64 * 1024);
    expect(() => parseStoryboardPreviewCard(goEscaped)).toThrow('64 KiB UTF-8');
  });

  it('parses a safe failed Card and refuses completed-only provenance on the failed exact-set', () => {
    const parsed = parseStoryboardPreviewCard(failedCard());
    expect(parsed).toMatchObject({ status: 'failed', failureKind: 'runtime', retryable: true });
    expect(() => parseStoryboardPreviewCard({ ...failedCard(), project_id: IDS.project })).toThrow('字段集合');
    expect(() => parseStoryboardPreviewCard({ ...failedCard(), failure_kind: 'provider' })).toThrow('未知枚举');
  });

  it('binds result_code to completed, tool failure and runtime failure unions', () => {
    expect(() => parseStoryboardPreviewCard({
      ...completedCard(), result_code: 'STORYBOARD_PREVIEW_CANDIDATE_INVALID'
    })).toThrow('未知枚举');
    expect(() => parseStoryboardPreviewCard({
      ...failedCard(), failure_kind: 'tool', result_code: 'PLAN_STORYBOARD_RUNTIME_FAILED'
    })).toThrow('未知枚举');
    expect(() => parseStoryboardPreviewCard({
      ...failedCard(), failure_kind: 'runtime', result_code: 'STORYBOARD_PREVIEW_INTERNAL'
    })).toThrow('未知枚举');
    expect(parseStoryboardPreviewCard({
      ...failedCard(), failure_kind: 'tool', result_code: 'STORYBOARD_PREVIEW_INTERNAL'
    })).toMatchObject({ failureKind: 'tool', resultCode: 'STORYBOARD_PREVIEW_INTERNAL' });
  });

  it('degrades only an unknown future Card schema without exposing its untrusted body', () => {
    const projection = parseStoryboardPreviewProjection({
      ...completedCard(),
      schema_version: 'storyboard.preview.card.v2',
      title: '<script>alert(1)</script>'
    });
    expect(projection).toEqual({ kind: 'unsupported', schemaVersion: 'storyboard.preview.card.v2' });
    expect(projection).not.toHaveProperty('title');
    expect(() => parseStoryboardPreviewCard({ ...completedCard(), schema_version: 'storyboard.preview.card.v2' }))
      .toThrow(PlanStoryboardPreviewContractError);
  });
});

function enqueueRequest() {
  return {
    schema_version: 'plan_storyboard.preview.enqueue-request.v1',
    creation_spec_ref: { id: IDS.creationSpec, version: 1, content_digest: 'a'.repeat(64) },
    tool_intent: {
      schema_version: 'plan_storyboard.preview.intent.v1',
      planning_instruction: '规划三段式故事板',
      target_duration_seconds: 30
    }
  };
}

function enqueueResponse() {
  return {
    schema_version: 'plan_storyboard.preview.enqueue.v1',
    request_id: IDS.request,
    session_id: IDS.session,
    input_id: IDS.input,
    turn_id: IDS.turn,
    run_id: IDS.run,
    tool_call_id: IDS.toolCall,
    status: 'pending',
    replayed: false
  };
}

function completedCard(overrides = {}) {
  return {
    schema_version: 'storyboard.preview.card.v1',
    input_id: IDS.input,
    turn_id: IDS.turn,
    run_id: IDS.run,
    tool_call_id: IDS.toolCall,
    status: 'completed',
    result_code: 'STORYBOARD_PREVIEW_DRAFT_CREATED',
    updated_at: '2026-07-17T10:00:00Z',
    storyboard_preview_id: IDS.storyboardPreview,
    project_id: IDS.project,
    creation_spec_ref: { id: IDS.creationSpec, version: 1, content_digest: 'a'.repeat(64) },
    version: 1,
    content_digest: 'b'.repeat(64),
    title: '新品短片故事板',
    summary: '通过三个连续元素完成开场、演示与收尾。',
    sections: [
      { key: 'section_1', title: '开场', objective: '建立产品认知' },
      { key: 'section_2', title: '演示与收尾', objective: '展示价值并给出行动号召' }
    ],
    elements: [
      {
        key: 'element_1', section_key: 'section_1', order: 1, element_type: 'scene', title: '产品登场',
        narrative_purpose: '建立视觉焦点', duration_seconds: 5, source_phase_key: 'phase_1', dependency_keys: []
      },
      {
        key: 'element_2', section_key: 'section_2', order: 2, element_type: 'shot', title: '功能演示',
        narrative_purpose: '展示核心卖点', duration_seconds: 15, source_phase_key: 'phase_2', dependency_keys: ['element_1']
      },
      {
        key: 'element_3', section_key: 'section_2', order: 3, element_type: 'caption', title: '行动号召',
        narrative_purpose: '推动转化', duration_seconds: 10, source_phase_key: 'phase_3', dependency_keys: ['element_2']
      }
    ],
    slots: [
      { key: 'slot_1', element_key: 'element_1', slot_type: 'image', purpose: '产品主视觉', required: true },
      { key: 'slot_2', element_key: 'element_2', slot_type: 'video', purpose: '功能演示画面', required: true }
    ],
    ...overrides
  };
}

function failedCard(overrides = {}) {
  return {
    schema_version: 'storyboard.preview.card.v1',
    input_id: IDS.input,
    turn_id: IDS.turn,
    run_id: IDS.run,
    tool_call_id: IDS.toolCall,
    status: 'failed',
    result_code: 'PLAN_STORYBOARD_RUNTIME_FAILED',
    updated_at: '2026-07-17T10:00:00Z',
    failure_kind: 'runtime',
    summary: '故事板运行时未完成。',
    retryable: true,
    ...overrides
  };
}

function cardAtLimits({ sectionCount, elementCount, slotsPerElement, slotPurpose = '媒体槽位' }) {
  const sections = Array.from({ length: sectionCount }, (_, index) => ({
    key: `section_${index + 1}`,
    title: `章节 ${index + 1}`,
    objective: `章节目标 ${index + 1}`
  }));
  const elements = Array.from({ length: elementCount }, (_, index) => ({
    key: `element_${index + 1}`,
    section_key: `section_${(index % sectionCount) + 1}`,
    order: index + 1,
    element_type: 'scene',
    title: `元素 ${index + 1}`,
    narrative_purpose: `叙事目标 ${index + 1}`,
    duration_seconds: 5,
    source_phase_key: `phase_${(index % 6) + 1}`,
    dependency_keys: index === 0 ? [] : [`element_${index}`]
  }));
  const slots = elements.flatMap((element, elementIndex) => (
    Array.from({ length: slotsPerElement }, (_, slotIndex) => {
      const index = (elementIndex * slotsPerElement) + slotIndex + 1;
      return {
        key: `slot_${index}`,
        element_key: element.key,
        slot_type: 'image',
        purpose: slotPurpose,
        required: slotIndex === 0
      };
    })
  ));
  return completedCard({ sections, elements, slots });
}
