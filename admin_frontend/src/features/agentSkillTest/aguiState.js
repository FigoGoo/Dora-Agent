export function createAguiState() {
  return {
    events: [],
    seenEventIds: [],
    lastSequence: 0,
    runStatus: 'idle',
    messages: [],
    thinking: '',
    selectedSkill: null,
    missingSkills: [],
    tools: [],
    generationTasks: [],
    confirmation: null,
    credit: null,
    assets: [],
    blackboard: null,
    snapshot: null,
    errors: []
  };
}

function eventType(event) {
  return event?.type || event?.event_type || '';
}

function eventPayload(event) {
  return event?.payload || {};
}

function textDelta(payload) {
  return payload.delta || payload.text_delta || '';
}

function upsertById(items, idKey, nextItem) {
  const index = items.findIndex((item) => item[idKey] === nextItem[idKey]);
  if (index < 0) {
    return [...items, nextItem];
  }
  return items.map((item, currentIndex) => (currentIndex === index ? { ...item, ...nextItem } : item));
}

function updateMessage(messages, payload, patch) {
  const messageId = payload.message_id || 'message_pending';
  const existing = messages.find((message) => message.message_id === messageId);
  const next = {
    message_id: messageId,
    role: payload.role || existing?.role || 'assistant',
    content_type: payload.content_type || existing?.content_type || 'text',
    content: existing?.content || '',
    final: existing?.final || false,
    ...patch
  };
  return upsertById(messages, 'message_id', next);
}

function appendMessageDelta(messages, payload) {
  const delta = textDelta(payload);
  const messageId = payload.message_id || 'message_pending';
  const existing = messages.find((message) => message.message_id === messageId);
  return updateMessage(messages, payload, { content: `${existing?.content || ''}${delta}`, final: false });
}

function completeMessage(messages, payload) {
  const finalText = payload.final_text || payload.text || '';
  const messageId = payload.message_id || 'message_pending';
  const existing = messages.find((message) => message.message_id === messageId);
  return updateMessage(messages, payload, {
    content: finalText || existing?.content || payload.final_text_digest || '',
    final: true
  });
}

function mapSnapshotMessages(snapshot) {
  return (snapshot?.messages || []).map((message) => ({
    message_id: message.message_id,
    role: message.role,
    content_type: message.content_type,
    content: message.content,
    final: message.final
  }));
}

export function applySnapshot(state, snapshot) {
  return {
    ...state,
    snapshot,
    runStatus: snapshot?.run?.status || state.runStatus,
    messages: mapSnapshotMessages(snapshot),
    assets: snapshot?.assets || [],
    blackboard: snapshot?.blackboard || null,
    generationTasks: snapshot?.tasks || [],
    confirmation: snapshot?.interrupt || state.confirmation,
    lastSequence: Math.max(state.lastSequence, Number(snapshot?.last_event_sequence || 0))
  };
}

export function reduceAguiEvent(state, event) {
  if (!event || !event.event_id || state.seenEventIds.includes(event.event_id)) {
    return state;
  }
  const type = eventType(event);
  const payload = eventPayload(event);
  const next = {
    ...state,
    events: [...state.events, event].sort((a, b) => Number(a.sequence || 0) - Number(b.sequence || 0)),
    seenEventIds: [...state.seenEventIds, event.event_id],
    lastSequence: Math.max(state.lastSequence, Number(event.sequence || 0))
  };

  switch (type) {
    case 'agent.run.started':
    case 'agent.run.completed':
    case 'agent.run.failed':
    case 'agent.run.cancelled':
      next.runStatus = payload.run_status || type.replace('agent.run.', '');
      if (type === 'agent.run.failed') {
        next.errors = [...next.errors, payload];
      }
      return next;
    case 'agent.thinking.delta':
    case 'agent.thinking.started':
      next.thinking = `${state.thinking}${textDelta(payload)}`;
      return next;
    case 'agent.thinking.completed':
      next.thinking = payload.summary || state.thinking;
      return next;
    case 'agent.message.delta':
      next.messages = appendMessageDelta(state.messages, payload);
      return next;
    case 'agent.message.completed':
      next.messages = completeMessage(state.messages, payload);
      return next;
    case 'agent.skill.selected':
      next.selectedSkill = payload;
      return next;
    case 'agent.skill.missing':
      next.missingSkills = [...state.missingSkills, payload];
      return next;
    case 'credits.estimated':
    case 'credits.frozen':
    case 'credits.charged':
    case 'credits.released':
    case 'credits.insufficient':
      next.credit = { ...state.credit, ...payload, credit_status: payload.credit_status || type.replace('credits.', '') };
      if (type === 'credits.insufficient') {
        next.errors = [...next.errors, payload];
      }
      return next;
    case 'confirmation.required':
    case 'confirmation.accepted':
    case 'confirmation.rejected':
      next.confirmation = { ...state.confirmation, ...payload, status: type.replace('confirmation.', '') };
      return next;
    case 'resume.accepted':
      next.runStatus = 'resuming';
      next.confirmation = state.confirmation ? { ...state.confirmation, resume_status: payload.resume_status || 'accepted' } : state.confirmation;
      return next;
    case 'tool.call.started':
    case 'tool.call.progress':
    case 'tool.call.completed':
    case 'tool.call.failed':
      next.tools = upsertById(state.tools, 'tool_call_id', { ...payload, status: payload.status || type.replace('tool.call.', '') });
      if (type === 'tool.call.failed') {
        next.errors = [...next.errors, payload];
      }
      return next;
    case 'generation.progress':
    case 'generation.artifact.completed':
      next.generationTasks = upsertById(state.generationTasks, 'task_id', payload);
      return next;
    case 'asset.save.completed':
    case 'workspace.assets.updated':
      next.assets = payload.assets || state.assets;
      return next;
    case 'workspace.blackboard.updated':
      next.blackboard = payload;
      return next;
    case 'project.archived.blocked':
      next.runStatus = 'blocked';
      next.errors = [...next.errors, payload];
      return next;
    default:
      return next;
  }
}

export function reduceAguiEvents(state, events = []) {
  return events.reduce((current, event) => reduceAguiEvent(current, event), state);
}
