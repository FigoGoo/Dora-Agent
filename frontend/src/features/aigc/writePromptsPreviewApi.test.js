import { afterEach, describe, expect, it, vi } from 'vitest';
import { enqueueWritePromptsPreview, writePromptsPreviewPath } from './writePromptsPreviewApi.js';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';

afterEach(() => vi.unstubAllGlobals());

describe('Write Prompts Preview API', () => {
  it('POSTs the exact split DTO through the same-origin BFF with CSRF and idempotency', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(202, enqueueResponse()));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();
    const accepted = await enqueueWritePromptsPreview({
      sessionID: WORKSPACE_IDS.session,
      storyboardPreviewRef: { id: WORKSPACE_IDS.storyboardPreview, version: 1, contentDigest: 'b'.repeat(64) },
      toolIntent: { writingInstruction: '为全部槽位编写提示词', outputLanguage: 'zh-CN' },
      idempotencyKey: WORKSPACE_IDS.request,
      csrfToken: 'csrf-write-prompts',
      signal: controller.signal
    });
    expect(accepted).toMatchObject({ inputID: WORKSPACE_IDS.promptInput, status: 'pending' });
    expect(fetchMock).toHaveBeenCalledWith(writePromptsPreviewPath(WORKSPACE_IDS.session), expect.objectContaining({
      method: 'POST',
      credentials: 'include',
      headers: expect.objectContaining({
        'Idempotency-Key': WORKSPACE_IDS.request,
        'X-CSRF-Token': 'csrf-write-prompts'
      }),
      body: JSON.stringify({
        schema_version: 'write_prompts.preview.enqueue-request.v1',
        storyboard_preview_ref: { id: WORKSPACE_IDS.storyboardPreview, version: 1, content_digest: 'b'.repeat(64) },
        tool_intent: {
          schema_version: 'write_prompts.preview.intent.v1',
          writing_instruction: '为全部槽位编写提示词',
          output_language: 'zh-CN'
        }
      }),
      signal: controller.signal
    }));
  });

  it('rejects a missing CSRF token, malformed UUID and cross-session receipt', async () => {
    await expect(enqueueWritePromptsPreview({ ...validCommand(), csrfToken: '' })).rejects.toThrow('CSRF');
    await expect(enqueueWritePromptsPreview({ ...validCommand(), idempotencyKey: 'bad' })).rejects.toThrow('UUIDv7');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(202, {
      ...enqueueResponse(), session_id: '019f0000-0000-7000-8000-000000000099'
    })));
    await expect(enqueueWritePromptsPreview(validCommand())).rejects.toThrow('session_id 不一致');
  });
});

function validCommand() {
  return {
    sessionID: WORKSPACE_IDS.session,
    storyboardPreviewRef: { id: WORKSPACE_IDS.storyboardPreview, version: 1, contentDigest: 'b'.repeat(64) },
    toolIntent: { writingInstruction: '编写提示词', outputLanguage: '' },
    idempotencyKey: WORKSPACE_IDS.request,
    csrfToken: 'csrf-write-prompts'
  };
}

function enqueueResponse() {
  return {
    schema_version: 'write_prompts.preview.enqueue.v1',
    request_id: WORKSPACE_IDS.request,
    session_id: WORKSPACE_IDS.session,
    input_id: WORKSPACE_IDS.promptInput,
    turn_id: WORKSPACE_IDS.promptTurn,
    run_id: WORKSPACE_IDS.promptRun,
    tool_call_id: WORKSPACE_IDS.promptToolCall,
    status: 'pending',
    replayed: false
  };
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
