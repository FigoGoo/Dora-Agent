import { afterEach, describe, expect, it, vi } from 'vitest';
import { promptPreviewCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { parsePromptPreviewCard } from '../aigc/writePromptsPreviewContract.js';
import { enqueueGenerateMediaPreview, mediaPreviewPath } from './mediaPreviewApi.js';
import { normalizeGenerateMediaPreviewRequest } from './mediaPreviewContract.js';

afterEach(() => vi.unstubAllGlobals());

describe('Media Preview API', () => {
  it('posts the strict Generate request through same-origin BFF with CSRF and idempotency', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(202, enqueueResponse()));
    vi.stubGlobal('fetch', fetchMock);
    const signal = new AbortController().signal;
    const request = normalizeGenerateMediaPreviewRequest({
      promptPreview: parsePromptPreviewCard(promptPreviewCardFixture()), targetLocalKey: 'slot_1'
    });
    const result = await enqueueGenerateMediaPreview({
      sessionID: WORKSPACE_IDS.session, request, idempotencyKey: WORKSPACE_IDS.request,
      csrfToken: 'csrf-media', signal
    });
    expect(result).toMatchObject({ toolKey: 'generate_media', status: 'pending' });
    expect(fetchMock).toHaveBeenCalledWith(mediaPreviewPath(WORKSPACE_IDS.session, 'generate_media'), expect.objectContaining({
      method: 'POST', credentials: 'include',
      headers: expect.objectContaining({ 'Idempotency-Key': WORKSPACE_IDS.request, 'X-CSRF-Token': 'csrf-media' }),
      body: JSON.stringify(request), signal
    }));
  });

  it('rejects malformed keys, missing CSRF and unknown tool paths before fetch', async () => {
    await expect(enqueueGenerateMediaPreview({ sessionID: WORKSPACE_IDS.session, request: {}, idempotencyKey: 'bad', csrfToken: '' }))
      .rejects.toThrow();
    expect(() => mediaPreviewPath(WORKSPACE_IDS.session, 'other')).toThrow('tool_key');
  });
});

function enqueueResponse() {
  return {
    schema_version: 'media_preview.enqueue.v1', request_id: WORKSPACE_IDS.request,
    session_id: WORKSPACE_IDS.session, input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn, run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall, tool_key: 'generate_media', status: 'pending', replayed: false
  };
}

function response(status, body) {
  return {
    ok: status >= 200 && status < 300, status, statusText: '',
    headers: { get: () => null }, text: vi.fn().mockResolvedValue(JSON.stringify(body))
  };
}
