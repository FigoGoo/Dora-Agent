export class AgentApiError extends Error {
  constructor({ message, code, traceId, retryable, status }) {
    super(message);
    this.name = 'AgentApiError';
    this.code = code || 'UNKNOWN';
    this.traceId = traceId || '';
    this.retryable = Boolean(retryable);
    this.status = status || 0;
  }
}

function buildQuery(params = {}) {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      query.set(key, String(value));
    }
  });
  const serialized = query.toString();
  return serialized ? `?${serialized}` : '';
}

function normalizeBearerToken(token) {
  const value = String(token || '').trim();
  if (!value) {
    return '';
  }
  return value.toLowerCase().startsWith('bearer ') ? value : `Bearer ${value}`;
}

export function parseAgentApiError(payload, status = 0) {
  const detail = payload?.error || payload?.details || payload || {};
  return new AgentApiError({
    message: detail.message || payload?.message || 'Agent API 请求失败。',
    code: detail.code || payload?.code,
    traceId: payload?.trace_id || detail.support_trace_id || detail.trace_id,
    retryable: detail.retryable ?? payload?.retryable,
    status
  });
}

export async function agentRequest(path, options = {}) {
  const method = (options.method || 'GET').toUpperCase();
  const headers = {
    Accept: options.accept || 'application/json',
    ...options.headers
  };
  const authorization = normalizeBearerToken(options.token);
  if (authorization) {
    headers.Authorization = authorization;
  }
  if (options.spaceId) {
    headers['X-Space-Id'] = options.spaceId;
  }
  if (options.traceId) {
    headers['X-Trace-Id'] = options.traceId;
  }
  if (options.lastEventId) {
    headers['Last-Event-ID'] = options.lastEventId;
  }

  let body;
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(options.body);
  }

  const response = await fetch(`${path}${buildQuery(options.query)}`, {
    method,
    headers,
    body,
    signal: options.signal
  });
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    throw parseAgentApiError(payload, response.status);
  }
  return payload?.data || payload;
}

export const agentApi = {
  createSession: ({ token, spaceId, traceId, body, signal }) =>
    agentRequest('/api/agent/sessions', { method: 'POST', token, spaceId, traceId, body, signal }),
  getSession: ({ token, spaceId, traceId, sessionId, signal }) =>
    agentRequest(`/api/agent/sessions/${sessionId}`, { token, spaceId, traceId, signal }),
  createRun: ({ token, spaceId, traceId, body, signal }) =>
    agentRequest('/api/agent/runs', { method: 'POST', token, spaceId, traceId, body, signal }),
  getRun: ({ token, spaceId, traceId, runId, signal }) =>
    agentRequest(`/api/agent/runs/${runId}`, { token, spaceId, traceId, signal }),
  replayEvents: ({ token, spaceId, traceId, runId, afterSequence = 0, pageSize = 100, signal }) =>
    agentRequest(`/api/agent/runs/${runId}/events`, {
      token,
      spaceId,
      traceId,
      query: { after_sequence: afterSequence, page_size: pageSize, limit: pageSize },
      signal
    }),
  appendUserInput: ({ token, spaceId, traceId, runId, body, signal }) =>
    agentRequest(`/api/agent/runs/${runId}/messages`, { method: 'POST', token, spaceId, traceId, body, signal }),
  acceptInterrupt: ({ token, spaceId, traceId, runId, interruptId, body, signal }) =>
    agentRequest(`/api/agent/runs/${runId}/interrupts/${interruptId}/accept`, { method: 'POST', token, spaceId, traceId, body, signal }),
  rejectInterrupt: ({ token, spaceId, traceId, runId, interruptId, body, signal }) =>
    agentRequest(`/api/agent/runs/${runId}/interrupts/${interruptId}/reject`, { method: 'POST', token, spaceId, traceId, body, signal }),
  cancelRun: ({ token, spaceId, traceId, runId, body, signal }) =>
    agentRequest(`/api/agent/runs/${runId}/cancel`, { method: 'POST', token, spaceId, traceId, body, signal }),
  getSnapshot: ({ token, spaceId, traceId, runId, signal }) =>
    agentRequest(`/api/agent/runs/${runId}/snapshot`, { token, spaceId, traceId, signal })
};

export function parseSseChunk(buffer, chunk) {
  const combined = `${buffer}${chunk}`;
  const parts = combined.split(/\r?\n\r?\n/);
  return {
    nextBuffer: parts.pop() || '',
    blocks: parts
  };
}

export function parseSseBlock(block) {
  const lines = block.split(/\r?\n/);
  const data = [];
  for (const line of lines) {
    if (line.startsWith('data:')) {
      data.push(line.slice(5).trimStart());
    }
  }
  if (!data.length) {
    return null;
  }
  return JSON.parse(data.join('\n'));
}

export async function openRunStream({ token, spaceId, traceId, runId, lastEventId, signal, onEvent }) {
  const headers = { Accept: 'text/event-stream' };
  const authorization = normalizeBearerToken(token);
  if (authorization) {
    headers.Authorization = authorization;
  }
  if (spaceId) {
    headers['X-Space-Id'] = spaceId;
  }
  if (traceId) {
    headers['X-Trace-Id'] = traceId;
  }
  if (lastEventId) {
    headers['Last-Event-ID'] = lastEventId;
  }

  const response = await fetch(`/api/agent/runs/${runId}/stream`, { headers, signal });
  if (!response.ok) {
    const payload = await response.json().catch(() => null);
    throw parseAgentApiError(payload, response.status);
  }
  if (!response.body) {
    return;
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    const parsed = parseSseChunk(buffer, decoder.decode(value, { stream: true }));
    buffer = parsed.nextBuffer;
    parsed.blocks.forEach((block) => {
      const event = parseSseBlock(block);
      if (event) {
        onEvent?.(event);
      }
    });
  }
}
