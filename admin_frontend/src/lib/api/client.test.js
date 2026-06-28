import { beforeEach, describe, expect, test, vi } from 'vitest';
import { adminRequest, createRequestHash, parseApiError } from './client.js';
import { getAdminSession, saveAdminSession } from '../auth/session.js';

describe('admin API client', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  test('creates stable request hashes for semantic request bodies', async () => {
    const left = await createRequestHash({ reason: 'disable', target_status: 'disabled' });
    const right = await createRequestHash({ target_status: 'disabled', reason: 'disable' });

    expect(left).toBe(right);
    expect(left).toMatch(/^[a-f0-9]{64}$/);
  });

  test('adds admin auth, idempotency, reason and request_hash to write requests', async () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'root', status: 'active', access_token: 'token_1' });
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ code: 'OK', data: { ok: true }, trace_id: 'trace_1' })
    });
    vi.stubGlobal('fetch', fetchMock);

    const data = await adminRequest('/api/admin/users/usr_1/status/confirm', {
      method: 'POST',
      reason: 'policy violation',
      body: { target_status: 'disabled', preview_token: 'preview_1' }
    });

    expect(data).toEqual({ ok: true });
    const [, init] = fetchMock.mock.calls[0];
    const body = JSON.parse(init.body);
    expect(init.headers.Authorization).toBe('Bearer token_1');
    expect(init.headers['Idempotency-Key']).toMatch(/^admin-/);
    expect(init.headers['X-Admin-Reason']).toBe('policy violation');
    expect(body.request_hash).toMatch(/^[a-f0-9]{64}$/);
  });

  test('renews the remembered admin session from response headers', async () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'root', status: 'active', access_token: 'token_1', expires_at: '2026-06-29T00:00:00Z' });
    const renewedAt = '2026-07-06T00:00:00Z';
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        headers: new Headers({ 'X-Admin-Session-Expires-At': renewedAt }),
        json: async () => ({ code: 'OK', data: { ok: true }, trace_id: 'trace_1' })
      })
    );

    await adminRequest('/api/admin/dashboard');

    expect(getAdminSession()?.expires_at).toBe(renewedAt);
  });

  test('parses error envelopes with trace id and field context', () => {
    const error = parseApiError({
      code: 'INVALID_ARGUMENT',
      message: 'invalid',
      trace_id: 'trace_1',
      error: { code: 'MISSING_REQUIRED_FIELD', message: 'reason required', retryable: false, field: 'reason' }
    });

    expect(error.message).toBe('reason required');
    expect(error.code).toBe('MISSING_REQUIRED_FIELD');
    expect(error.traceId).toBe('trace_1');
    expect(error.field).toBe('reason');
    expect(error.retryable).toBe(false);
  });
});
