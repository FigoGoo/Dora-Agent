import { validatePromptPreviewSourceBinding } from '../aigc/writePromptsPreviewContract.js';

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
  if (event.seq === state.cursor + 1) {
    if (
      event.event === 'creation_spec.preview.completed'
      && classifyCreationSpecPreview(state.snapshot?.creationSpecPreview, event.payload) === 'invalid'
    ) {
      return 'reset';
    }
    if (event.event.startsWith('session.turn.') && classifyTurnOutput(state.snapshot, event.payload) === 'invalid') {
      return 'reset';
    }
    if (event.event.startsWith('analyze_materials.preview.') && event.event !== 'analyze_materials.preview.accepted'
      && classifyAnalyzeMaterialsPreview(state.snapshot, event.payload) === 'invalid') {
      return 'reset';
    }
    if (event.event.startsWith('plan_storyboard.preview.') && event.event !== 'plan_storyboard.preview.accepted'
      && classifyPlanStoryboardPreview(state.snapshot, event.payload) === 'invalid') {
      return 'reset';
    }
    if (event.event.startsWith('plan_storyboard.preview.') && event.event !== 'plan_storyboard.preview.accepted'
      && state.snapshot?.writePromptsPreview?.status === 'completed'
      && !promptPreviewMatchesSource(state.snapshot.writePromptsPreview, event.payload)) {
      return 'reset';
    }
    if (event.event.startsWith('write_prompts.preview.') && event.event !== 'write_prompts.preview.accepted'
      && classifyWritePromptsPreview(state.snapshot, event.payload) === 'invalid') {
      return 'reset';
    }
    if (event.event.startsWith('media.preview.')
      && classifyMediaPreview(state.snapshot, event.payload) === 'invalid') {
      return 'reset';
    }
    return 'apply';
  }
  if (event.seq <= state.cursor && state.eventIDsBySeq[event.seq] === event.eventID) return 'duplicate';
  return 'reset';
}

// classifyWorkspaceSnapshot 拒绝权威高水位前进时 CreationSpec 投影消失、倒退或同版本异义。
// V1 没有删除/撤回语义；出现这些组合时必须停止沿用旧 Card，并重新同步或失败关闭。
export function classifyWorkspaceSnapshot(state, project, snapshot) {
  const current = state.snapshot;
  const sameBinding = current
    && state.project?.projectID === project?.projectID
    && current.session?.id === snapshot?.session?.id;
  if (!sameBinding) return 'apply';
  if (classifyCreationSpecPreview(current.creationSpecPreview, snapshot.creationSpecPreview) === 'invalid') return 'invalid';
  if (classifyTurnOutputSnapshot(current, snapshot) === 'invalid') return 'invalid';
  if (classifyAnalyzeMaterialsSnapshot(current, snapshot) === 'invalid') return 'invalid';
  if (classifyPlanStoryboardSnapshot(current, snapshot) === 'invalid') return 'invalid';
  if (classifyWritePromptsSnapshot(current, snapshot) === 'invalid') return 'invalid';
  return classifyMediaPreviewSnapshot(current, snapshot) === 'invalid' ? 'invalid' : 'apply';
}

function applyAcceptedEvent(state, event) {
  let snapshot = state.snapshot;
  if (event.event === 'session.input.accepted') snapshot = applyInputAccepted(snapshot, event);
  if (event.event === 'creation_spec.preview.completed') snapshot = applyCreationSpecCompleted(snapshot, event);
  if (event.event === 'creation_spec.preview.failed') snapshot = applyCreationSpecFailed(snapshot, event);
  if (event.event === 'session.turn.completed') snapshot = applyTurnOutput(snapshot, event, 'resolved');
  if (event.event === 'session.turn.failed') snapshot = applyTurnOutput(snapshot, event, 'dead');
  if (event.event === 'session.turn.recovery_pending') snapshot = applyTurnOutput(snapshot, event, 'recovery_pending');
  if (event.event.startsWith('analyze_materials.preview.') && event.event !== 'analyze_materials.preview.accepted') {
    snapshot = applyAnalyzeMaterialsPreview(snapshot, event);
  }
  if (event.event.startsWith('plan_storyboard.preview.') && event.event !== 'plan_storyboard.preview.accepted') {
    snapshot = applyPlanStoryboardPreview(snapshot, event);
  }
  if (event.event.startsWith('write_prompts.preview.') && event.event !== 'write_prompts.preview.accepted') {
    snapshot = applyWritePromptsPreview(snapshot, event);
  }
  if (event.event.startsWith('media.preview.')) snapshot = applyMediaPreview(snapshot, event);
  return {
    ...state,
    cursor: event.seq,
    snapshot,
    eventIDsBySeq: { ...state.eventIDsBySeq, [event.seq]: event.eventID },
    events: [...state.events, event]
  };
}

function applyAnalyzeMaterialsPreview(snapshot, event) {
  if (!snapshot) return snapshot;
  const inputStatus = event.event === 'analyze_materials.preview.runtime_failed' ? 'dead' : 'resolved';
  return {
    ...snapshot,
    analyzeMaterialsPreview: event.payload,
    inputs: snapshot.inputs.map((input) => input.id === event.payload.inputID
      ? { ...input, status: inputStatus }
      : input)
  };
}

function classifyAnalyzeMaterialsPreview(snapshot, incoming) {
  if (!snapshot || !incoming || !snapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  const current = snapshot.analyzeMaterialsPreview;
  if (!current) return 'apply';
  if (current.turnID === incoming.turnID) return analyzeMaterialsEquals(current, incoming) ? 'apply' : 'invalid';
  return compareInputOrder(snapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function classifyAnalyzeMaterialsSnapshot(currentSnapshot, incomingSnapshot) {
  const current = currentSnapshot.analyzeMaterialsPreview;
  const incoming = incomingSnapshot.analyzeMaterialsPreview;
  if (!current) return 'apply';
  if (!incoming || !incomingSnapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  if (current.turnID === incoming.turnID) return analyzeMaterialsEquals(current, incoming) ? 'apply' : 'invalid';
  return compareInputOrder(incomingSnapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function analyzeMaterialsEquals(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

// applyPlanStoryboardPreview 只投影 terminal Card；Tool failed 仍是已解析业务结果，只有 runtime_failed 进入 dead。
function applyPlanStoryboardPreview(snapshot, event) {
  if (!snapshot) return snapshot;
  const inputStatus = event.event === 'plan_storyboard.preview.runtime_failed' ? 'dead' : 'resolved';
  return {
    ...snapshot,
    planStoryboardPreview: event.payload,
    inputs: snapshot.inputs.map((input) => input.id === event.payload.inputID
      ? { ...input, status: inputStatus }
      : input)
  };
}

function classifyPlanStoryboardPreview(snapshot, incoming) {
  if (!snapshot || !incoming) return 'invalid';
  const current = snapshot.planStoryboardPreview;
  if (!current) return 'apply';
  if (current.kind === 'unsupported' || incoming.kind === 'unsupported') return 'apply';
  if (current.turnID === incoming.turnID) return storyboardPreviewEquals(current, incoming) ? 'apply' : 'invalid';
  return compareInputOrder(snapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function classifyPlanStoryboardSnapshot(currentSnapshot, incomingSnapshot) {
  const current = currentSnapshot.planStoryboardPreview;
  const incoming = incomingSnapshot.planStoryboardPreview;
  if (!current) return 'apply';
  if (!incoming) return 'invalid';
  if (current.kind === 'unsupported' || incoming.kind === 'unsupported') return 'apply';
  if (!incomingSnapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  if (current.turnID === incoming.turnID) return storyboardPreviewEquals(current, incoming) ? 'apply' : 'invalid';
  return compareInputOrder(incomingSnapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function storyboardPreviewEquals(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

// applyWritePromptsPreview 只投影 terminal Card；accepted 只推进 Cursor，绝不覆盖 terminal。
function applyWritePromptsPreview(snapshot, event) {
  if (!snapshot) return snapshot;
  const inputStatus = event.event === 'write_prompts.preview.runtime_failed' ? 'dead' : 'resolved';
  return {
    ...snapshot,
    writePromptsPreview: event.payload,
    inputs: snapshot.inputs.map((input) => input.id === event.payload.inputID
      ? { ...input, status: inputStatus }
      : input)
  };
}

function classifyWritePromptsPreview(snapshot, incoming) {
  if (!snapshot || !incoming || !snapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  if (!promptPreviewMatchesSource(incoming, snapshot.planStoryboardPreview)) return 'invalid';
  const current = snapshot.writePromptsPreview;
  if (!current) return 'apply';
  if (current.turnID === incoming.turnID) return promptPreviewEquals(current, incoming) ? 'apply' : 'invalid';
  return compareInputOrder(snapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function classifyWritePromptsSnapshot(currentSnapshot, incomingSnapshot) {
  const current = currentSnapshot.writePromptsPreview;
  const incoming = incomingSnapshot.writePromptsPreview;
  if (!current) return 'apply';
  if (!incoming || !incomingSnapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  if (!promptPreviewMatchesSource(incoming, incomingSnapshot.planStoryboardPreview)) return 'invalid';
  if (current.turnID === incoming.turnID) return promptPreviewEquals(current, incoming) ? 'apply' : 'invalid';
  return compareInputOrder(incomingSnapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function promptPreviewMatchesSource(preview, storyboardPreview) {
  if (preview.status !== 'completed') return true;
  try {
    validatePromptPreviewSourceBinding(preview, storyboardPreview);
    return true;
  } catch {
    return false;
  }
}

function promptPreviewEquals(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

// Media Preview 保留 accepted 与 terminal 历史；终态只追加，绝不覆盖同一请求的 accepted Card。
function applyMediaPreview(snapshot, event) {
  if (!snapshot) return snapshot;
  const inputStatus = event.event === 'media.preview.runtime_failed' ? 'dead' : 'resolved';
  const affected = new Set([event.payload.inputID, event.aggregateID]);
  return {
    ...snapshot,
    mediaPreviews: [...(snapshot.mediaPreviews || []), event.payload],
    inputs: snapshot.inputs.map((input) => affected.has(input.id)
      ? { ...input, status: input.id === event.payload.inputID ? inputStatus : 'resolved' }
      : input)
  };
}

function classifyMediaPreview(snapshot, incoming) {
  if (!snapshot || !incoming || !snapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  const current = snapshot.mediaPreviews || [];
  if (current.length >= 16 || current.some((card) => mediaPreviewEquals(card, incoming))) return 'invalid';
  const sameInput = current.filter((card) => card.inputID === incoming.inputID);
  if (incoming.status === 'accepted' || incoming.operationID === '') {
    return sameInput.length === 0 ? 'apply' : 'invalid';
  }
  if (sameInput.some((card) => card.status !== 'accepted')) return 'invalid';
  const accepted = sameInput.find((card) => card.status === 'accepted');
  if (!accepted) return 'invalid';
  return accepted.toolKey === incoming.toolKey
    && accepted.turnID === incoming.turnID
    && accepted.runID === incoming.runID
    && accepted.toolCallID === incoming.toolCallID
    && accepted.operationID === incoming.operationID
    && accepted.batchID === incoming.batchID
    && accepted.assetRef?.id === incoming.assetRef?.id
    ? 'apply' : 'invalid';
}

function classifyMediaPreviewSnapshot(currentSnapshot, incomingSnapshot) {
  const current = currentSnapshot.mediaPreviews || [];
  const incoming = incomingSnapshot.mediaPreviews || [];
  if (current.length > incoming.length) return 'invalid';
  return current.every((card, index) => mediaPreviewEquals(card, incoming[index])) ? 'apply' : 'invalid';
}

function mediaPreviewEquals(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

// applyCreationSpecCompleted 只接收已由 classifyWorkspaceEvent 验证的资源级单调投影。
function applyCreationSpecCompleted(snapshot, event) {
  if (!snapshot) return snapshot;
  return {
    ...snapshot,
    creationSpecPreview: event.payload,
    creationSpecPreviewFailure: null
  };
}

// 同一资源只能保持 exact 同义版本或向更高版本前进；新资源与未知未来版本允许替换。
function classifyCreationSpecPreview(current, incoming) {
  if (!current) return 'apply';
  if (!incoming) return 'invalid';
  if (incoming.kind === 'unsupported' || current.kind === 'unsupported') return 'apply';
  if (incoming.creationSpecID !== current.creationSpecID) return 'apply';
  if (incoming.version < current.version) return 'invalid';
  if (incoming.version === current.version && incoming.contentDigest !== current.contentDigest) return 'invalid';
  return 'apply';
}

function applyCreationSpecFailed(snapshot, event) {
  if (!snapshot) return snapshot;
  return { ...snapshot, creationSpecPreviewFailure: event.payload };
}

function applyTurnOutput(snapshot, event, inputStatus) {
  if (!snapshot) return snapshot;
  return {
    ...snapshot,
    latestTurnOutput: event.payload,
    inputs: snapshot.inputs.map((input) => input.id === event.payload.inputID
      ? { ...input, status: inputStatus }
      : input)
  };
}

function classifyTurnOutput(snapshot, incoming) {
  if (!snapshot || !incoming || !snapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  const current = snapshot.latestTurnOutput;
  if (!current) return 'apply';
  if (current.turnID === incoming.turnID) {
    if (current.runID !== incoming.runID || current.inputID !== incoming.inputID) return 'invalid';
    if (current.status !== 'recovery_pending') return 'invalid';
    if (incoming.status === 'recovery_pending') return turnOutputEquals(current, incoming) ? 'apply' : 'invalid';
    return 'apply';
  }
  return compareInputOrder(snapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function classifyTurnOutputSnapshot(currentSnapshot, incomingSnapshot) {
  const current = currentSnapshot.latestTurnOutput;
  const incoming = incomingSnapshot.latestTurnOutput;
  if (!current) return 'apply';
  if (!incoming || !incomingSnapshot.inputs.some((input) => input.id === incoming.inputID)) return 'invalid';
  if (current.turnID === incoming.turnID) {
    if (current.runID !== incoming.runID || current.inputID !== incoming.inputID) return 'invalid';
    if (turnOutputEquals(current, incoming)) return 'apply';
    return current.status === 'recovery_pending' && incoming.status !== 'recovery_pending' ? 'apply' : 'invalid';
  }
  return compareInputOrder(incomingSnapshot.inputs, current.inputID, incoming.inputID) < 0 ? 'apply' : 'invalid';
}

function turnOutputEquals(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

function compareInputOrder(inputs, leftID, rightID) {
  const left = inputs.find((input) => input.id === leftID)?.enqueueSeq;
  const right = inputs.find((input) => input.id === rightID)?.enqueueSeq;
  if (!Number.isSafeInteger(left) || !Number.isSafeInteger(right)) return 0;
  return left - right;
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
