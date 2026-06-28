import { describe, expect, test } from 'vitest';
import { prepareBody, prepareCreateBody, resolveRowIdentifier } from './ResourceListPage.jsx';

describe('ResourceListPage helpers', () => {
  test('resolves row identifiers from either a function or a field name', () => {
    expect(resolveRowIdentifier({ tool_name: 'draw', tool_type: 'builtin' }, (row) => `${row.tool_name}:${row.tool_type}`)).toBe('draw:builtin');
    expect(resolveRowIdentifier({ admin_id: 'adm_1' }, 'admin_id')).toBe('adm_1');
  });

  test('normalizes datetime-local and numeric create fields before posting', () => {
    const body = prepareCreateBody(
      {
        code_expires_at: '2026-07-06T08:30',
        count: '20',
        points: '50',
        reason: '运营活动'
      },
      {
        create: {
          fields: [
            { name: 'code_expires_at', type: 'datetime-local' },
            { name: 'count', type: 'number' },
            { name: 'points', type: 'number' },
            { name: 'reason' }
          ]
        }
      }
    );

    expect(body.code_expires_at).toBe(new Date('2026-07-06T08:30').toISOString());
    expect(body.count).toBe(20);
    expect(body.points).toBe(50);
    expect(body.reason).toBe('运营活动');
  });

  test('normalizes JSON, JSON string, array and checkbox action fields', () => {
    const body = prepareBody(
      {
        route_config: '{"timeout_ms":30000}',
        skill_spec_json: '{"steps":[]}',
        capability_tags: 'image\nfast',
        requires_confirmation: true
      },
      [
        { name: 'route_config', type: 'json' },
        { name: 'skill_spec_json', type: 'json-string' },
        { name: 'capability_tags', type: 'array' },
        { name: 'requires_confirmation', type: 'checkbox' }
      ]
    );

    expect(body.route_config).toEqual({ timeout_ms: 30000 });
    expect(body.skill_spec_json).toBe('{"steps":[]}');
    expect(body.capability_tags).toEqual(['image', 'fast']);
    expect(body.requires_confirmation).toBe(true);
  });
});
