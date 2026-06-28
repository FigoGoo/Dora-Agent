import { describe, expect, test } from 'vitest';
import { prepareCreateBody, resolveRowIdentifier } from './ResourceListPage.jsx';

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
});
