import { beforeEach, describe, expect, test, vi } from 'vitest';

vi.mock('../../lib/api/admin.js', () => ({
  adminApi: {
    post: vi.fn(() => Promise.resolve({})),
    previewTakeDownWork: vi.fn(() => Promise.resolve({})),
    confirmTakeDownWork: vi.fn(() => Promise.resolve({}))
  }
}));

import { adminApi } from '../../lib/api/admin.js';
import { pageConfigs } from './pageConfigs.jsx';

describe('admin resource page configs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  test('uses stable fallback ids for tools and skill reviews', async () => {
    const tool = { tool_name: 'draw', tool_type: 'builtin', status: 'active' };
    expect(pageConfigs.tools.rowId(tool)).toBe('draw:builtin');
    await pageConfigs.tools.actions[0].preview(tool, '例行检查');
    expect(adminApi.post).toHaveBeenCalledWith('/api/admin/tools/draw:builtin/impact-preview', {
      target_status: 'active',
      tool_type: 'builtin'
    });
    expect(pageConfigs.tools.actions[4].confirmPath(tool)).toBe('/api/admin/tools/draw:builtin/status');
    expect(pageConfigs.tools.actions[4].body({ reason: '例行检查', row: tool })).toEqual({
      tool_type: 'builtin',
      status: 'disabled',
      reason: '例行检查'
    });

    const review = { version_id: 'skv_1' };
    expect(pageConfigs['skills/reviews'].rowId(review)).toBe('skv_1');
    expect(pageConfigs['skills/reviews'].actions[1].confirmPath(review)).toBe('/api/admin/skills/reviews/skv_1/confirm');
  });

  test('sends fields required by backend confirmations', async () => {
    const model = { model_id: 'mdl_1', resource_type: 'image', pricing_snapshot_id: 'price_1' };
    expect(pageConfigs.models.actions[1].body({ row: model })).toEqual({
      model_id: 'mdl_1',
      resource_type: 'image',
      pricing_snapshot_id: 'price_1'
    });

    const publicWork = { public_work_id: 'pw_1' };
    await pageConfigs['works/public'].actions[0].confirm(publicWork, { reason: '内容风险', previewToken: 'prev_1' });
    expect(adminApi.confirmTakeDownWork).toHaveBeenCalledWith(
      'pw_1',
      { reason: '内容风险', preview_token: 'prev_1', notify_author: true },
      '内容风险'
    );
  });

  test('maps tool policy form fields to backend body', () => {
    const body = pageConfigs.tools.actions[1].body({
      values: {
        tool_type: 'builtin',
        allowed: true,
        risk_level: 'high',
        requires_confirmation: true,
        timeout_ms: '30000',
        retry_policy: '{"max":"1"}',
        cancel_policy: '{}'
      }
    });

    expect(body).toEqual({
      tool_type: 'builtin',
      allowed: true,
      risk_level: 'high',
      requires_confirmation: true,
      timeout_ms: 30000,
      retry_policy: { max: '1' },
      cancel_policy: {}
    });
  });
});
