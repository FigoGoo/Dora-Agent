import { describe, expect, it, vi } from 'vitest';
import { AUTH_SESSION_EXPIRED_EVENT } from '../auth/authSession.js';
import { ApiError, requestJSON, requestOptionalJSON } from './apiClient.js';

describe('API Client', () => {
  it('uses same-origin Cookie credentials and preserves caller headers', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ ok: true }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(
      requestJSON('/api/example', {
        method: 'POST',
        credentials: 'omit',
        headers: { 'Idempotency-Key': 'idem-1' },
        body: JSON.stringify({ name: 'Dora' })
      })
    ).resolves.toEqual({ ok: true });

    expect(fetchMock).toHaveBeenCalledWith('/api/example', {
      credentials: 'include',
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'idem-1'
      },
      body: JSON.stringify({ name: 'Dora' })
    });
  });

  it('does not set a multipart content type for FormData', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(null, 204));
    vi.stubGlobal('fetch', fetchMock);
    const form = new FormData();
    form.set('file', new Blob(['fixture']), 'fixture.txt');

    await expect(requestJSON('/api/upload', { method: 'POST', body: form })).resolves.toBeNull();

    const headers = fetchMock.mock.calls[0][1].headers;
    expect(headers.Accept).toBe('application/json');
    expect(headers['Content-Type']).toBeUndefined();
  });

  it('maps a structured error envelope without losing request correlation', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        response(
          {
            error: {
              code: 'VERSION_CONFLICT',
              message: '资源版本已经变化',
              request_id: 'req-7',
              retryable: false,
              details: { expected_version: 7 }
            }
          },
          409
        )
      )
    );

    const error = await requestJSON('/api/resource').catch((caught) => caught);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 409,
      code: 'VERSION_CONFLICT',
      message: '资源版本已经变化',
      requestID: 'req-7',
      retryable: false,
      details: { expected_version: 7 }
    });
  });

  it('returns null only for optional 404 resources', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ code: 'NOT_FOUND', message: '不存在' }, 404)));

    await expect(requestOptionalJSON('/api/missing')).resolves.toBeNull();
    await expect(requestJSON('/api/missing')).rejects.toMatchObject({ status: 404, code: 'NOT_FOUND' });
  });

  it('broadcasts a minimal auth-expired event for 401 responses', async () => {
    const listener = vi.fn();
    window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, listener);
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(response({ code: 'SESSION_EXPIRED', message: '登录已失效', request_id: 'req-auth' }, 401))
    );

    await expect(requestJSON('/api/private')).rejects.toMatchObject({ status: 401, code: 'SESSION_EXPIRED' });

    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].detail).toEqual({
      status: 401,
      code: 'SESSION_EXPIRED',
      request_id: 'req-auth'
    });
    window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, listener);
  });

  it('fails closed when a successful response is not valid JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response('not-json')));

    await expect(requestJSON('/api/broken')).rejects.toMatchObject({
      status: 200,
      code: 'INVALID_JSON_RESPONSE'
    });
  });
});

function response(body, status = 200, extraHeaders = {}) {
  const headers = new Map(
    Object.entries({ 'content-type': 'application/json', ...extraHeaders }).map(([key, value]) => [key.toLowerCase(), value])
  );
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: { get: (name) => headers.get(String(name).toLowerCase()) || null },
    text: vi.fn().mockResolvedValue(
      body == null ? '' : typeof body === 'string' ? body : JSON.stringify(body)
    )
  };
}
