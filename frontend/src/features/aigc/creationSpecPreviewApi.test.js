import { describe, expect, it, vi } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { creationSpecPreviewPath, enqueueCreationSpecPreview } from './creationSpecPreviewApi.js';

describe('Creation Spec Preview API', () => {
  it('POSTs the exact same-origin body, CSRF and UUIDv7 key and accepts only 202 pending', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(202, {
      schema_version: 'plan_creation_spec.preview.enqueue.v1',
      request_id: WORKSPACE_IDS.previewRequest,
      session_id: WORKSPACE_IDS.session,
      input_id: WORKSPACE_IDS.previewInput,
      status: 'pending'
    }));
    vi.stubGlobal('fetch', fetchMock);

    const accepted = await enqueueCreationSpecPreview({
      sessionID: WORKSPACE_IDS.session,
      idempotencyKey: WORKSPACE_IDS.previewRequest,
      csrfToken: 'csrf-preview',
      intent: {
        goal: '制作新品短片',
        deliverableType: 'video',
        audience: '',
        locale: 'zh-CN',
        constraints: []
      }
    });

    expect(accepted).toMatchObject({ inputID: WORKSPACE_IDS.previewInput, status: 'pending' });
    expect(fetchMock).toHaveBeenCalledWith(
      creationSpecPreviewPath(WORKSPACE_IDS.session),
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        headers: expect.objectContaining({
          'Content-Type': 'application/json',
          'Idempotency-Key': WORKSPACE_IDS.previewRequest,
          'X-CSRF-Token': 'csrf-preview'
        }),
        body: JSON.stringify({
          schema_version: 'plan_creation_spec.preview.intent.v1',
          goal: '制作新品短片',
          deliverable_type: 'video',
          locale: 'zh-CN',
          constraints: []
        })
      })
    );
  });

  it('rejects HTTP 200, malformed UUIDv7 inputs, and a missing CSRF token', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(200, {})));
    await expect(enqueueCreationSpecPreview({
      sessionID: WORKSPACE_IDS.session,
      idempotencyKey: WORKSPACE_IDS.previewRequest,
      csrfToken: 'csrf-preview',
      intent: validIntent()
    })).rejects.toMatchObject({ code: 'UNEXPECTED_HTTP_STATUS', status: 200 });
    await expect(enqueueCreationSpecPreview({
      sessionID: 'not-a-session', idempotencyKey: WORKSPACE_IDS.previewRequest, csrfToken: 'csrf', intent: validIntent()
    })).rejects.toThrow('session_id');
    await expect(enqueueCreationSpecPreview({
      sessionID: WORKSPACE_IDS.session, idempotencyKey: 'v4-key', csrfToken: 'csrf', intent: validIntent()
    })).rejects.toThrow('UUIDv7');
    await expect(enqueueCreationSpecPreview({
      sessionID: WORKSPACE_IDS.session, idempotencyKey: WORKSPACE_IDS.previewRequest, intent: validIntent()
    })).rejects.toThrow('CSRF');
  });
});

function validIntent() {
  return { goal: '制作新品短片', deliverableType: 'video', locale: 'zh-CN', constraints: [] };
}

function response(status, body) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: { get: () => null },
    text: vi.fn().mockResolvedValue(JSON.stringify(body))
  };
}
