const WRITE_METHODS = new Set(['POST', 'PATCH', 'PUT', 'DELETE']);

export class ApiError extends Error {
  constructor({ message, code, traceId, field, retryable, status }) {
    super(message);
    this.name = 'ApiError';
    this.code = code || 'UNKNOWN';
    this.traceId = traceId || '';
    this.field = field || '';
    this.retryable = Boolean(retryable);
    this.status = status || 0;
  }
}

export function buildQuery(params = {}) {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      query.set(key, String(value));
    }
  });
  const serialized = query.toString();
  return serialized ? `?${serialized}` : '';
}

function safeHeaderValue(value, fallback) {
  const text = String(value || '').trim();
  return text && /^[\x20-\x7E]+$/.test(text) ? text : fallback;
}

function storageValue(key) {
  if (typeof window === 'undefined') {
    return '';
  }
  let storage;
  try {
    storage = window.localStorage;
  } catch {
    return '';
  }
  if (!storage || typeof storage.getItem !== 'function') {
    return '';
  }
  try {
    return storage.getItem(key) || '';
  } catch {
    return '';
  }
}

function requestId() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `req-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function userAccessToken() {
  const directToken = storageValue('dora_user_access_token');
  if (directToken) {
    return directToken;
  }
  const rawSession = storageValue('dora_user_session');
  if (!rawSession) {
    return '';
  }
  try {
    return JSON.parse(rawSession)?.access_token || '';
  } catch {
    return '';
  }
}

function parseApiError(payload, status = 0) {
  const detail = payload?.error || payload || {};
  return new ApiError({
    message: detail.message || payload?.message || '请求失败，请稍后重试。',
    code: detail.code || payload?.code,
    traceId: payload?.trace_id || detail.support_trace_id,
    field: detail.field,
    retryable: detail.retryable,
    status
  });
}

export async function userRequest(path, options = {}) {
  if (typeof fetch !== 'function') {
    throw new ApiError({ code: 'FETCH_UNAVAILABLE', message: '当前环境不可请求服务。' });
  }
  const method = (options.method || 'GET').toUpperCase();
  const headers = {
    Accept: 'application/json',
    ...options.headers
  };
  const token = userAccessToken();
  let body = options.body;

  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  if (WRITE_METHODS.has(method)) {
    body = { ...body };
    headers['Content-Type'] = 'application/json';
    headers['Idempotency-Key'] = safeHeaderValue(options.idempotencyKey, `user-${requestId()}`);
  }

  const response = await fetch(`${path}${buildQuery(options.query)}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: options.signal
  });
  const payload = await response.json().catch(() => null);

  if (!response.ok || payload?.code !== 'OK') {
    throw parseApiError(payload, response.status);
  }
  return payload.data;
}
