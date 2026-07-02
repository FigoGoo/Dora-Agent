import { beforeEach, describe, expect, test, vi } from 'vitest';
import { adminRequest, parseApiError, safeHeaderValue } from './client.js';
import { getAdminSession, saveAdminSession } from '../auth/session.js';

describe('admin API client', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  test('keeps generated request headers ASCII safe', () => {
    expect(safeHeaderValue('skill_test:run_1', 'fallback')).toBe('skill_test:run_1');
    expect(safeHeaderValue('审核测试', 'fallback')).toBe('fallback');
  });

  test('adds admin auth and idempotency header to write requests', async () => {
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
    expect(init.headers['X-Admin-Reason']).toBeUndefined();
    expect(body).toEqual({ target_status: 'disabled', preview_token: 'preview_1' });
  });

  test('keeps non-ASCII admin reason in the JSON body instead of request headers', async () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'root', status: 'active', access_token: 'token_1' });
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ code: 'OK', data: { ok: true }, trace_id: 'trace_1' })
    });
    vi.stubGlobal('fetch', fetchMock);

    await adminRequest('/api/admin/models/default', {
      method: 'POST',
      reason: '设为默认：DeepSeek V4 Fast\n后台操作',
      body: { model_id: 'mdl_deepseek_v4_fast', reason: '设为默认：DeepSeek V4 Fast\n后台操作' }
    });

    const [, init] = fetchMock.mock.calls[0];
    const body = JSON.parse(init.body);
    expect(init.headers['X-Admin-Reason']).toBeUndefined();
    expect(init.headers['X-Admin-Reason-Encoding']).toBeUndefined();
    expect(body.reason).toBe('设为默认：DeepSeek V4 Fast\n后台操作');
  });

  test('renews the remembered admin session from response headers', async () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'root', status: 'active', access_token: 'token_1', expires_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString() });
    const renewedAt = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString();
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
