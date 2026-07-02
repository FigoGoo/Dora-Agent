import { clearAdminSession, getAdminSession, renewAdminSession } from '../auth/session.js';

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

export function safeHeaderValue(value, fallback) {
  const text = String(value || '').trim();
  return text && /^[\x20-\x7E]+$/.test(text) ? text : fallback;
}

export function parseApiError(payload, status = 0) {
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

export async function adminRequest(path, options = {}) {
  const method = (options.method || 'GET').toUpperCase();
  const session = getAdminSession();
  const headers = {
    Accept: 'application/json',
    ...options.headers
  };
  let body = options.body;

  if (session?.access_token) {
    headers.Authorization = `Bearer ${session.access_token}`;
  }

  if (WRITE_METHODS.has(method)) {
    body = { ...body };
    headers['Content-Type'] = 'application/json';
    headers['Idempotency-Key'] = safeHeaderValue(options.idempotencyKey, `admin-${crypto.randomUUID()}`);
  }

  const response = await fetch(`${path}${buildQuery(options.query)}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: options.signal
  });
  const payload = await response.json().catch(() => null);
  const renewedExpiresAt = response.headers?.get?.('X-Admin-Session-Expires-At');
  if (renewedExpiresAt) {
    renewAdminSession(renewedExpiresAt);
  }

  if (!response.ok || payload?.code !== 'OK') {
    if (response.status === 401) {
      clearAdminSession();
    }
    throw parseApiError(payload, response.status);
  }
  return payload.data;
}
