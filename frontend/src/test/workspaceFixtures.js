export const WORKSPACE_IDS = Object.freeze({
  request: '019f0000-0000-7000-8000-000000000001',
  user: '019f0000-0000-7000-8000-000000000002',
  project: '019f0000-0000-7000-8000-000000000004',
  session: '019f0000-0000-7000-8000-000000000005',
  message: '019f0000-0000-7000-8000-000000000006',
  input: '019f0000-0000-7000-8000-000000000007',
  event: '019f0000-0000-7000-8000-000000000008'
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
    schema_version: 'session.workspace.v1',
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
    event_high_watermark: 1,
    min_available_seq: 1,
    ...overrides
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
