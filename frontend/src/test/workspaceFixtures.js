export const WORKSPACE_IDS = Object.freeze({
  request: '019f0000-0000-7000-8000-000000000001',
  user: '019f0000-0000-7000-8000-000000000002',
  project: '019f0000-0000-7000-8000-000000000004',
  session: '019f0000-0000-7000-8000-000000000005',
  message: '019f0000-0000-7000-8000-000000000006',
  input: '019f0000-0000-7000-8000-000000000007',
  event: '019f0000-0000-7000-8000-000000000008',
  creationSpec: '019f0000-0000-7000-8000-000000000009',
  previewRequest: '019f0000-0000-7000-8000-000000000010',
  previewInput: '019f0000-0000-7000-8000-000000000011',
  turn: '019f0000-0000-7000-8000-000000000012',
  run: '019f0000-0000-7000-8000-000000000013',
  turnEvent: '019f0000-0000-7000-8000-000000000014',
  toolCall: '019f0000-0000-7000-8000-000000000015',
  asset: '019f0000-0000-7000-8000-000000000016',
  evidence: '019f0000-0000-7000-8000-000000000017',
  storyboardInput: '019f0000-0000-7000-8000-000000000018',
  storyboardTurn: '019f0000-0000-7000-8000-000000000019',
  storyboardRun: '019f0000-0000-7000-8000-000000000020',
  storyboardToolCall: '019f0000-0000-7000-8000-000000000021',
  storyboardCommand: '019f0000-0000-7000-8000-000000000022',
  storyboardPreview: '019f0000-0000-7000-8000-000000000023',
  storyboardEvent: '019f0000-0000-7000-8000-000000000024',
  promptInput: '019f0000-0000-7000-8000-000000000025',
  promptTurn: '019f0000-0000-7000-8000-000000000026',
  promptRun: '019f0000-0000-7000-8000-000000000027',
  promptToolCall: '019f0000-0000-7000-8000-000000000028',
  promptCommand: '019f0000-0000-7000-8000-000000000029',
  promptPreview: '019f0000-0000-7000-8000-000000000030',
  promptEvent: '019f0000-0000-7000-8000-000000000031',
  mediaInput: '019f0000-0000-7000-8000-000000000032',
  mediaTurn: '019f0000-0000-7000-8000-000000000033',
  mediaRun: '019f0000-0000-7000-8000-000000000034',
  mediaToolCall: '019f0000-0000-7000-8000-000000000035',
  mediaOperation: '019f0000-0000-7000-8000-000000000036',
  mediaBatch: '019f0000-0000-7000-8000-000000000037',
  mediaJob: '019f0000-0000-7000-8000-000000000038',
  mediaAsset: '019f0000-0000-7000-8000-000000000039',
  mediaTerminalInput: '019f0000-0000-7000-8000-000000000040',
  mediaAcceptedEvent: '019f0000-0000-7000-8000-000000000041',
  mediaTerminalEvent: '019f0000-0000-7000-8000-000000000042'
});

export function projectBootstrapFixture(overrides = {}) {
  return {
    project_id: WORKSPACE_IDS.project,
    title: '未命名项目',
    lifecycle_status: 'active',
    recent_run_status: 'idle',
    initial_prompt_status: 'absent',
    creation_status: 'ready',
    session_id: WORKSPACE_IDS.session,
    input_id: null,
    updated_at: '2026-07-14T00:00:00Z',
    request_id: WORKSPACE_IDS.request,
    ...overrides
  };
}

export function workspaceSnapshotFixture(overrides = {}) {
  return {
    schema_version: 'session.workspace.v2',
    request_id: WORKSPACE_IDS.request,
    session: {
      id: WORKSPACE_IDS.session,
      project_id: WORKSPACE_IDS.project,
      status: 'active',
      version: 1,
      created_at: '2026-07-14T00:00:00.000000Z',
      updated_at: '2026-07-14T00:00:00.000000Z'
    },
    messages: [],
    inputs: [],
    creation_spec_preview: null,
    latest_turn_output: null,
    analyze_materials_preview: null,
    event_high_watermark: 1,
    min_available_seq: 1,
    ...overrides
  };
}

export function workspaceSnapshotV1Fixture(overrides = {}) {
  const snapshot = workspaceSnapshotFixture({ ...overrides, schema_version: 'session.workspace.v1' });
  delete snapshot.latest_turn_output;
  delete snapshot.analyze_materials_preview;
  return snapshot;
}

export function workspaceSnapshotV3Fixture(overrides = {}) {
  return {
    ...workspaceSnapshotFixture(),
    schema_version: 'session.workspace.v3',
    plan_storyboard_preview: null,
    ...overrides
  };
}

export function workspaceSnapshotV4Fixture(overrides = {}) {
  return {
    ...workspaceSnapshotV3Fixture(),
    schema_version: 'session.workspace.v4',
    write_prompts_preview: null,
    ...overrides
  };
}

export function workspaceSnapshotV5Fixture(overrides = {}) {
  return {
    ...workspaceSnapshotV4Fixture(),
    schema_version: 'session.workspace.v5',
    media_previews: [],
    ...overrides
  };
}

export function storyboardPreviewCardFixture(overrides = {}) {
  return {
    schema_version: 'storyboard.preview.card.v1',
    input_id: WORKSPACE_IDS.storyboardInput,
    turn_id: WORKSPACE_IDS.storyboardTurn,
    run_id: WORKSPACE_IDS.storyboardRun,
    tool_call_id: WORKSPACE_IDS.storyboardToolCall,
    status: 'completed',
    result_code: 'STORYBOARD_PREVIEW_DRAFT_CREATED',
    updated_at: '2026-07-17T10:00:00Z',
    storyboard_preview_id: WORKSPACE_IDS.storyboardPreview,
    project_id: WORKSPACE_IDS.project,
    creation_spec_ref: { id: WORKSPACE_IDS.creationSpec, version: 1, content_digest: 'a'.repeat(64) },
    version: 1,
    content_digest: 'b'.repeat(64),
    title: '新品短片故事板',
    summary: '通过开场与演示两个元素建立产品认知。',
    sections: [{ key: 'section_1', title: '开场与演示', objective: '建立产品认知并展示价值' }],
    elements: [
      {
        key: 'element_1', section_key: 'section_1', order: 1, element_type: 'scene', title: '产品登场',
        narrative_purpose: '建立视觉焦点', duration_seconds: 10, source_phase_key: 'phase_1', dependency_keys: []
      },
      {
        key: 'element_2', section_key: 'section_1', order: 2, element_type: 'shot', title: '功能演示',
        narrative_purpose: '展示核心卖点', duration_seconds: 20, source_phase_key: 'phase_1', dependency_keys: ['element_1']
      }
    ],
    slots: [
      { key: 'slot_1', element_key: 'element_1', slot_type: 'image', purpose: '产品主视觉', required: true },
      { key: 'slot_2', element_key: 'element_2', slot_type: 'video', purpose: '功能演示画面', required: true }
    ],
    ...overrides
  };
}

export function storyboardPreviewFailureCardFixture(overrides = {}) {
  return {
    schema_version: 'storyboard.preview.card.v1',
    input_id: WORKSPACE_IDS.storyboardInput,
    turn_id: WORKSPACE_IDS.storyboardTurn,
    run_id: WORKSPACE_IDS.storyboardRun,
    tool_call_id: WORKSPACE_IDS.storyboardToolCall,
    status: 'failed',
    result_code: 'STORYBOARD_PREVIEW_CANDIDATE_INVALID',
    updated_at: '2026-07-17T10:00:00Z',
    failure_kind: 'tool',
    summary: '故事板候选未通过严格校验。',
    retryable: false,
    ...overrides
  };
}

export function storyboardAcceptedEventFixture(overrides = {}) {
  const payload = {
    schema_version: 'plan_storyboard.preview.accepted.v1',
    input_id: WORKSPACE_IDS.storyboardInput,
    turn_id: WORKSPACE_IDS.storyboardTurn,
    run_id: WORKSPACE_IDS.storyboardRun,
    tool_call_id: WORKSPACE_IDS.storyboardToolCall,
    business_command_id: WORKSPACE_IDS.storyboardCommand,
    intent_digest: 'c'.repeat(64),
    context_digest: 'd'.repeat(64),
    creation_spec_id: WORKSPACE_IDS.creationSpec,
    creation_spec_version: 1,
    creation_spec_content_digest: 'a'.repeat(64),
    ...(overrides.payload || {})
  };
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.event,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 2,
    event: 'plan_storyboard.preview.accepted',
    occurred_at: '2026-07-17T10:00:00Z',
    aggregate_type: 'plan_storyboard_preview',
    aggregate_id: payload.input_id,
    aggregate_version: 1,
    ...overrides,
    payload
  };
}

export function storyboardEventFixture(event = 'plan_storyboard.preview.completed', overrides = {}) {
  const payload = event === 'plan_storyboard.preview.completed'
    ? storyboardPreviewCardFixture(overrides.payload || {})
    : storyboardPreviewFailureCardFixture({
      failure_kind: event === 'plan_storyboard.preview.runtime_failed' ? 'runtime' : 'tool',
      result_code: event === 'plan_storyboard.preview.runtime_failed'
        ? 'PLAN_STORYBOARD_RUNTIME_FAILED'
        : 'STORYBOARD_PREVIEW_CANDIDATE_INVALID',
      ...(overrides.payload || {})
    });
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.storyboardEvent,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 3,
    event,
    occurred_at: '2026-07-17T10:00:01Z',
    aggregate_type: 'plan_storyboard_preview',
    aggregate_id: payload.input_id,
    aggregate_version: 1,
    ...overrides,
    payload
  };
}

export function promptPreviewCardFixture(overrides = {}) {
  return {
    schema_version: 'prompt.preview.card.v1',
    input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn,
    run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall,
    status: 'completed',
    result_code: 'PROMPT_PREVIEW_DRAFT_CREATED',
    updated_at: '2026-07-17T11:00:00Z',
    prompt_preview_id: WORKSPACE_IDS.promptPreview,
    project_id: WORKSPACE_IDS.project,
    storyboard_preview_ref: { id: WORKSPACE_IDS.storyboardPreview, version: 1, content_digest: 'b'.repeat(64) },
    version: 1,
    content_digest: 'e'.repeat(64),
    target_count: 2,
    prompts: [
      {
        target_local_key: 'slot_1', element_local_key: 'element_1', slot_type: 'image', media_kind: 'image',
        purpose: '产品主视觉', required: true, positive_prompt: '聚焦产品主体，使用清晰的商业摄影构图。',
        negative_constraints: ['避免文字水印'], output_language: 'zh-CN'
      },
      {
        target_local_key: 'slot_2', element_local_key: 'element_2', slot_type: 'video', media_kind: 'video',
        purpose: '功能演示画面', required: true, positive_prompt: '连续展示核心功能操作与即时反馈。',
        negative_constraints: [], output_language: 'zh-CN'
      }
    ],
    ...overrides
  };
}

export function promptPreviewFailureCardFixture(overrides = {}) {
  return {
    schema_version: 'prompt.preview.card.v1',
    input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn,
    run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall,
    status: 'failed',
    result_code: 'PROMPT_PREVIEW_CANDIDATE_INVALID',
    updated_at: '2026-07-17T11:00:00Z',
    failure_kind: 'tool',
    summary: '提示词候选未通过严格校验。',
    retryable: false,
    ...overrides
  };
}

export function promptAcceptedEventFixture(overrides = {}) {
  const payload = {
    schema_version: 'write_prompts.preview.accepted.v1',
    input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn,
    run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall,
    business_command_id: WORKSPACE_IDS.promptCommand,
    intent_digest: 'f'.repeat(64),
    context_digest: '0'.repeat(64),
    storyboard_preview_id: WORKSPACE_IDS.storyboardPreview,
    storyboard_preview_version: 1,
    storyboard_preview_content_digest: 'b'.repeat(64),
    ...(overrides.payload || {})
  };
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.event,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 4,
    event: 'write_prompts.preview.accepted',
    occurred_at: '2026-07-17T11:00:00Z',
    aggregate_type: 'write_prompts_preview',
    aggregate_id: payload.input_id,
    aggregate_version: 1,
    ...overrides,
    payload
  };
}

export function promptEventFixture(event = 'write_prompts.preview.completed', overrides = {}) {
  const payload = event === 'write_prompts.preview.completed'
    ? promptPreviewCardFixture(overrides.payload || {})
    : promptPreviewFailureCardFixture({
      failure_kind: event === 'write_prompts.preview.runtime_failed' ? 'runtime' : 'tool',
      result_code: event === 'write_prompts.preview.runtime_failed'
        ? 'WRITE_PROMPTS_RUNTIME_FAILED'
        : 'PROMPT_PREVIEW_CANDIDATE_INVALID',
      ...(overrides.payload || {})
    });
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.promptEvent,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 5,
    event,
    occurred_at: '2026-07-17T11:00:01Z',
    aggregate_type: 'write_prompts_preview',
    aggregate_id: payload.input_id,
    aggregate_version: 1,
    ...overrides,
    payload
  };
}

export function analyzeMaterialsPreviewCardFixture(overrides = {}) {
  return {
    schema_version: 'analyze_materials.preview.card.v1',
    input_id: WORKSPACE_IDS.input,
    turn_id: WORKSPACE_IDS.turn,
    run_id: WORKSPACE_IDS.run,
    tool_call_id: WORKSPACE_IDS.toolCall,
    status: 'completed',
    result_code: 'MATERIAL_ANALYSIS_PREVIEW_COMPLETED',
    analysis: {
      schema_version: 'material_analysis.preview.candidate.v1',
      asset_summaries: [{
        asset_id: WORKSPACE_IDS.asset,
        summary: '素材以城市中的红色自行车为主体。',
        observations: [{ observation_id: 'observation_1', text: '画面主体是一辆红色自行车。', evidence_ids: [WORKSPACE_IDS.evidence] }],
        inferences: []
      }],
      cross_asset_findings: [], usable_elements: [], risks: [], open_questions: [], unused_evidence_ids: []
    },
    coverage: {
      status: 'completed', evidence_policy_version: 'analyze_materials.preview.evidence-policy.v1',
      target_asset_ids: [WORKSPACE_IDS.asset], analyzable_asset_ids: [WORKSPACE_IDS.asset],
      included_evidence_ids: [WORKSPACE_IDS.evidence], missing_requirements: [],
      target_asset_set_digest: 'a'.repeat(64), included_evidence_set_digest: 'b'.repeat(64),
      missing_requirement_set_digest: 'c'.repeat(64)
    },
    evidence_refs: [{
      evidence_id: WORKSPACE_IDS.evidence, asset_id: WORKSPACE_IDS.asset, asset_version: 1,
      media_type: 'image', evidence_kind: 'visual_description', content_digest: 'd'.repeat(64),
      locator: { kind: 'image_whole' }
    }],
    ...overrides
  };
}

export function analyzeMaterialsFailureCardFixture(overrides = {}) {
  return {
    schema_version: 'analyze_materials.preview.card.v1', input_id: WORKSPACE_IDS.input,
    turn_id: WORKSPACE_IDS.turn, run_id: WORKSPACE_IDS.run, tool_call_id: WORKSPACE_IDS.toolCall,
    status: 'failed', result_code: 'MATERIAL_ANALYSIS_DEPENDENCY_NOT_READY',
    failure_kind: 'tool', summary: '素材证据尚不足以生成可信分析', retryable: false,
    ...overrides
  };
}

export function analyzeMaterialsEventFixture(event = 'analyze_materials.preview.completed', overrides = {}) {
  const payload = event === 'analyze_materials.preview.completed' || event === 'analyze_materials.preview.partial'
    ? analyzeMaterialsPreviewCardFixture({ status: event.endsWith('.partial') ? 'partial' : 'completed', result_code: event.endsWith('.partial') ? 'MATERIAL_ANALYSIS_PREVIEW_PARTIAL' : 'MATERIAL_ANALYSIS_PREVIEW_COMPLETED', coverage: { ...analyzeMaterialsPreviewCardFixture().coverage, status: event.endsWith('.partial') ? 'partial' : 'completed' }, ...(overrides.payload || {}) })
    : analyzeMaterialsFailureCardFixture({ failure_kind: event.endsWith('.runtime_failed') ? 'runtime' : 'tool', result_code: event.endsWith('.runtime_failed') ? 'ANALYZE_MATERIALS_RUNTIME_FAILED' : 'MATERIAL_ANALYSIS_DEPENDENCY_NOT_READY', ...(overrides.payload || {}) });
  return { schema_version: 'workspace.event.v1', payload_schema_version: 'session.event.v1', event_id: WORKSPACE_IDS.turnEvent, session_id: WORKSPACE_IDS.session, project_id: WORKSPACE_IDS.project, seq: 3, event, occurred_at: '2026-07-17T00:00:02.000000Z', aggregate_type: 'session_turn', aggregate_id: payload.turn_id, aggregate_version: 1, ...overrides, payload };
}

export function analyzeMaterialsAcceptedEventFixture(overrides = {}) {
  const payload = {
    session_id: WORKSPACE_IDS.session, input_id: WORKSPACE_IDS.input, turn_id: WORKSPACE_IDS.turn,
    run_id: WORKSPACE_IDS.run, request_id: WORKSPACE_IDS.request,
    source_type: 'analyze_materials_preview', intent_digest: 'e'.repeat(64),
    tool_call_id: WORKSPACE_IDS.toolCall, context_digest: 'f'.repeat(64),
    ...(overrides.payload || {})
  };
  return {
    schema_version: 'workspace.event.v1', payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.event, session_id: WORKSPACE_IDS.session, project_id: WORKSPACE_IDS.project,
    seq: 2, event: 'analyze_materials.preview.accepted', occurred_at: '2026-07-17T00:00:01.000000Z',
    aggregate_type: 'session_input', aggregate_id: payload.input_id, aggregate_version: 1,
    ...overrides, payload
  };
}

export function directResponseCardFixture(overrides = {}) {
  return {
    schema_version: 'session.turn.direct_response.card.v1',
    turn_id: WORKSPACE_IDS.turn,
    run_id: WORKSPACE_IDS.run,
    input_id: WORKSPACE_IDS.input,
    status: 'completed',
    message_code: 'creation_request_received',
    summary: '已收到你的创作需求。你可以继续打开工具箱选择下一步流程。',
    available_actions: ['open_toolbox'],
    ...overrides
  };
}

export function turnFailureCardFixture(overrides = {}) {
  return {
    schema_version: 'session.turn.failure.card.v1',
    turn_id: WORKSPACE_IDS.turn,
    run_id: WORKSPACE_IDS.run,
    input_id: WORKSPACE_IDS.input,
    status: 'failed',
    error_code: 'MODEL_RESPONSE_INVALID',
    retryable: false,
    summary: '暂时无法完成处理，请稍后重试。',
    ...overrides
  };
}

export function turnEventFixture(event = 'session.turn.completed', overrides = {}) {
  const payloadOverrides = overrides.payload || {};
  const payload = event === 'session.turn.completed'
    ? directResponseCardFixture(payloadOverrides)
    : turnFailureCardFixture({
      status: event === 'session.turn.failed' ? 'failed' : 'recovery_pending',
      error_code: event === 'session.turn.failed' ? 'MODEL_RESPONSE_INVALID' : 'MODEL_RESULT_UNKNOWN',
      retryable: event === 'session.turn.recovery_pending',
      summary: event === 'session.turn.failed' ? '暂时无法完成处理，请稍后重试。' : '处理结果正在恢复，请稍后查看。',
      ...payloadOverrides
    });
  const envelope = {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.turnEvent,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 3,
    event,
    occurred_at: '2026-07-17T00:00:02.000000Z',
    aggregate_type: 'session_turn',
    aggregate_id: payload.turn_id,
    aggregate_version: 2,
    ...overrides
  };
  return { ...envelope, payload };
}

export function creationSpecPreviewCardFixture(overrides = {}) {
  return {
    schema_version: 'creation_spec.preview.card.v1',
    creation_spec_id: WORKSPACE_IDS.creationSpec,
    project_id: WORKSPACE_IDS.project,
    version: 1,
    status: 'draft',
    content_digest: 'a'.repeat(64),
    title: '新品短片创作规范',
    goal: '为新品发布制作一支 30 秒中文短片',
    deliverable_type: 'video',
    audience: '年轻消费者',
    locale: 'zh-CN',
    phases: [{ key: 'phase_1', title: '内容策划', objective: '冻结叙事结构', output: '脚本大纲' }],
    constraints: ['时长不超过 30 秒'],
    acceptance_criteria: ['成片时长不超过 30 秒'],
    updated_at: '2026-07-16T00:00:02.000000Z',
    ...overrides
  };
}

export function creationSpecPreviewCompletedEventFixture(overrides = {}) {
  const card = creationSpecPreviewCardFixture(overrides.payload || {});
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: overrides.event_id || WORKSPACE_IDS.event,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 2,
    event: 'creation_spec.preview.completed',
    occurred_at: '2026-07-16T00:00:02.000000Z',
    aggregate_type: 'creation_spec',
    aggregate_id: card.creation_spec_id,
    aggregate_version: card.version,
    payload: card,
    ...overrides,
    payload: card
  };
}

export function creationSpecPreviewFailedEventFixture(overrides = {}) {
  const payload = {
    input_id: WORKSPACE_IDS.previewInput,
    result_code: 'CREATION_SPEC_PREVIEW_INVALID',
    summary: '无法生成预览，请修改目标后重试。',
    retryable: false,
    ...(overrides.payload || {})
  };
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: overrides.event_id || WORKSPACE_IDS.event,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 2,
    event: 'creation_spec.preview.failed',
    occurred_at: '2026-07-16T00:00:02.000000Z',
    aggregate_type: 'session_input',
    aggregate_id: payload.input_id,
    aggregate_version: 1,
    payload,
    ...overrides,
    payload
  };
}

export function messageFixture(overrides = {}) {
  return {
    id: WORKSPACE_IDS.message,
    message_seq: 1,
    role: 'user',
    content: '创建一支短片',
    created_at: '2026-07-14T00:00:00.000000Z',
    ...overrides
  };
}

export function inputFixture(overrides = {}) {
  return {
    id: WORKSPACE_IDS.input,
    message_id: WORKSPACE_IDS.message,
    source_type: 'user_message',
    status: 'pending',
    enqueue_seq: 1,
    available_at: '2026-07-14T00:00:00.000000Z',
    created_at: '2026-07-14T00:00:00.000000Z',
    updated_at: '2026-07-14T00:00:00.000000Z',
    ...overrides
  };
}

export function inputAcceptedEventFixture(overrides = {}) {
  return {
    schema_version: 'workspace.event.v1',
    payload_schema_version: 'session.event.v1',
    event_id: WORKSPACE_IDS.event,
    session_id: WORKSPACE_IDS.session,
    project_id: WORKSPACE_IDS.project,
    seq: 2,
    event: 'session.input.accepted',
    occurred_at: '2026-07-14T00:00:01.000000Z',
    aggregate_type: 'session_input',
    aggregate_id: WORKSPACE_IDS.input,
    aggregate_version: 1,
    payload: {
      session_id: WORKSPACE_IDS.session,
      input_id: WORKSPACE_IDS.input,
      message_id: WORKSPACE_IDS.message,
      enqueue_seq: 1,
      status: 'pending'
    },
    ...overrides
  };
}
