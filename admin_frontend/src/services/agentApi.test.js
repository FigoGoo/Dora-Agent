import { beforeEach, describe, expect, test, vi } from 'vitest';
import { agentApi, agentRequest, parseSseBlock, parseSseChunk } from './agentApi.js';

describe('agent API client', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  test('sends Agent bearer auth and selected_skill_id without using admin session', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ run_id: 'run_1', status: 'pending' })
    });
    vi.stubGlobal('fetch', fetchMock);

    const data = await agentApi.createRun({
      token: 'agent-token',
      spaceId: 'sp_1',
      traceId: 'trace_1',
      body: {
        session_id: 'ses_1',
        project_id: 'prj_1',
        selected_skill_id: 'sk_1',
        user_input: { client_message_id: 'cm_1', content_type: 'text', text: 'hello' }
      }
    });

    expect(data).toEqual({ run_id: 'run_1', status: 'pending' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/agent/runs');
    expect(init.headers.Authorization).toBe('Bearer agent-token');
    expect(init.headers['X-Space-Id']).toBe('sp_1');
    expect(init.headers['X-Trace-Id']).toBe('trace_1');
    expect(JSON.parse(init.body).selected_skill_id).toBe('sk_1');
  });

  test('parses Agent API error envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 409,
        json: async () => ({ code: 'RUN_STATE_CONFLICT', message: 'active run exists', trace_id: 'trace_2', retryable: true })
      })
    );

    await expect(agentRequest('/api/agent/runs', { method: 'POST', token: 'token', body: {} })).rejects.toMatchObject({
      code: 'RUN_STATE_CONFLICT',
      traceId: 'trace_2',
      retryable: true,
      status: 409
    });
  });

  test('parses SSE blocks incrementally', () => {
    const parsed = parseSseChunk('', 'id: 1\nevent: agent.message.delta\ndata: {"event_id":"evt_1","type":"agent.message.delta"}\n\nid: 2');

    expect(parsed.blocks).toHaveLength(1);
    expect(parsed.nextBuffer).toBe('id: 2');
    expect(parseSseBlock(parsed.blocks[0])).toEqual({ event_id: 'evt_1', type: 'agent.message.delta' });
  });
});
