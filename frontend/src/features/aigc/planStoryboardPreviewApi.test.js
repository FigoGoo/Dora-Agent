import { afterEach, describe, expect, it, vi } from 'vitest';
import { enqueuePlanStoryboardPreview, planStoryboardPreviewPath } from './planStoryboardPreviewApi.js';

const IDS = Object.freeze({
  session: '019f0000-0000-7000-8000-000000000002',
  request: '019f0000-0000-7000-8000-000000000003',
  input: '019f0000-0000-7000-8000-000000000004',
  turn: '019f0000-0000-7000-8000-000000000005',
  run: '019f0000-0000-7000-8000-000000000006',
  toolCall: '019f0000-0000-7000-8000-000000000007',
  creationSpec: '019f0000-0000-7000-8000-000000000008'
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('Plan Storyboard Preview API', () => {
  it('POSTs only the split exact DTO with same-origin credentials, CSRF and UUIDv7 idempotency', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(response(202, enqueueResponse()))
      .mockResolvedValueOnce(response(202, { ...enqueueResponse(), replayed: true }));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();

    const accepted = await enqueuePlanStoryboardPreview({
      sessionID: IDS.session,
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
      toolIntent: { planningInstruction: '规划三段式故事板', targetDurationSeconds: 60 },
      idempotencyKey: IDS.request,
      csrfToken: 'csrf-storyboard-preview',
      signal: controller.signal
    });

    expect(accepted).toMatchObject({
      requestID: IDS.request,
      inputID: IDS.input,
      turnID: IDS.turn,
      runID: IDS.run,
      toolCallID: IDS.toolCall,
      status: 'pending',
      replayed: false
    });
    expect(fetchMock).toHaveBeenCalledWith(
      planStoryboardPreviewPath(IDS.session),
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        headers: expect.objectContaining({
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'Idempotency-Key': IDS.request,
          'X-CSRF-Token': 'csrf-storyboard-preview'
        }),
        body: JSON.stringify({
          schema_version: 'plan_storyboard.preview.enqueue-request.v1',
          creation_spec_ref: {
            id: IDS.creationSpec,
            version: 1,
            content_digest: 'a'.repeat(64)
          },
          tool_intent: {
            schema_version: 'plan_storyboard.preview.intent.v1',
            planning_instruction: '规划三段式故事板',
            target_duration_seconds: 60
          }
        }),
        signal: controller.signal
      })
    );

    const replayed = await enqueuePlanStoryboardPreview({
      sessionID: IDS.session,
      creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
      toolIntent: { planningInstruction: '规划三段式故事板', targetDurationSeconds: 60 },
      idempotencyKey: IDS.request,
      csrfToken: 'csrf-storyboard-preview'
    });
    expect(replayed).toMatchObject({
      requestID: accepted.requestID,
      inputID: accepted.inputID,
      turnID: accepted.turnID,
      runID: accepted.runID,
      toolCallID: accepted.toolCallID,
      status: 'pending',
      replayed: true
    });
    expect(fetchMock.mock.calls[1][1].body).toBe(fetchMock.mock.calls[0][1].body);
    expect(fetchMock.mock.calls[1][1].headers['Idempotency-Key']).toBe(IDS.request);
  });

  it('rejects HTTP 200, malformed UUIDv7 inputs, a missing CSRF token, and cross-session receipts', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(200, {})));
    await expect(enqueuePlanStoryboardPreview(validCommand())).rejects.toMatchObject({
      code: 'UNEXPECTED_HTTP_STATUS',
      status: 200
    });
    await expect(enqueuePlanStoryboardPreview({ ...validCommand(), sessionID: 'not-a-session' }))
      .rejects.toThrow('session_id');
    await expect(enqueuePlanStoryboardPreview({ ...validCommand(), idempotencyKey: 'v4-key' }))
      .rejects.toThrow('UUIDv7');
    await expect(enqueuePlanStoryboardPreview({ ...validCommand(), csrfToken: '' }))
      .rejects.toThrow('CSRF');

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(202, {
      ...enqueueResponse(),
      session_id: '019f0000-0000-7000-8000-000000000099'
    })));
    await expect(enqueuePlanStoryboardPreview(validCommand())).rejects.toThrow('session_id 不一致');
  });
});

function validCommand() {
  return {
    sessionID: IDS.session,
    creationSpecRef: { id: IDS.creationSpec, version: 1, contentDigest: 'a'.repeat(64) },
    toolIntent: { planningInstruction: '规划故事板', targetDurationSeconds: '' },
    idempotencyKey: IDS.request,
    csrfToken: 'csrf-storyboard-preview'
  };
}

function enqueueResponse() {
  return {
    schema_version: 'plan_storyboard.preview.enqueue.v1',
    request_id: IDS.request,
    session_id: IDS.session,
    input_id: IDS.input,
    turn_id: IDS.turn,
    run_id: IDS.run,
    tool_call_id: IDS.toolCall,
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
