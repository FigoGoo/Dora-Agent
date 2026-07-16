// createWorkspaceState 返回不包含其他 Project 残留数据的正式状态机初态。
export function createWorkspaceState() {
  return {
    kind: 'loading',
    phase: 'auth',
    streamState: 'connecting',
    project: null,
    snapshot: null,
    cursor: 0,
    eventIDsBySeq: {},
    events: [],
    error: null
  };
}

// workspaceReducer 只接受已经通过契约校验的 Snapshot/Event，终态会清除不允许继续展示的数据。
export function workspaceReducer(state, action) {
  switch (action.type) {
    case 'loading':
      return { ...createWorkspaceState(), phase: action.phase || 'project' };
    case 'loading_phase':
      return { ...state, kind: state.kind === 'reset' ? 'reset' : 'loading', phase: action.phase, error: null };
    case 'snapshot_ready':
      return {
        ...state,
        kind: 'ready',
        phase: 'complete',
        streamState: 'connecting',
        project: action.project,
        snapshot: action.snapshot,
        cursor: action.snapshot.eventHighWatermark,
        eventIDsBySeq: {},
        events: [],
        error: null
      };
    case 'stream_connecting':
      return { ...state, kind: state.snapshot ? 'ready' : state.kind, streamState: 'connecting', error: null };
    case 'stream_live':
      return { ...state, kind: 'ready', streamState: 'live', error: null };
    case 'stream_reconnecting':
      return { ...state, kind: 'ready', streamState: 'reconnecting', error: action.error || null };
    case 'event_applied':
      return applyAcceptedEvent(state, action.event);
    case 'reset':
      return { ...state, kind: 'reset', streamState: 'connecting', error: action.error || null };
    case 'offline':
      return { ...state, kind: 'offline', streamState: 'reconnecting', error: action.error || null };
    case 'unauthorized':
      return { ...createWorkspaceState(), kind: 'unauthorized', phase: 'complete', error: action.error || null };
    case 'not_found':
      return { ...createWorkspaceState(), kind: 'not_found', phase: 'complete', error: action.error || null };
    default:
      return state;
  }
}

// classifyWorkspaceEvent 在推进 Cursor 前检查连续性和 Seq→EventID 重复语义。
export function classifyWorkspaceEvent(state, event) {
  if (event.seq === state.cursor + 1) return 'apply';
  if (event.seq <= state.cursor && state.eventIDsBySeq[event.seq] === event.eventID) return 'duplicate';
  return 'reset';
}

function applyAcceptedEvent(state, event) {
  const snapshot = event.event === 'session.input.accepted'
    ? applyInputAccepted(state.snapshot, event)
    : state.snapshot;
  return {
    ...state,
    cursor: event.seq,
    snapshot,
    eventIDsBySeq: { ...state.eventIDsBySeq, [event.seq]: event.eventID },
    events: [...state.events, event]
  };
}

function applyInputAccepted(snapshot, event) {
  if (!snapshot) return snapshot;
  const inputID = event.payload.input_id;
  if (!snapshot.inputs.some((input) => input.id === inputID)) return snapshot;
  return {
    ...snapshot,
    inputs: snapshot.inputs.map((input) => input.id === inputID ? { ...input, status: event.payload.status } : input)
  };
}
