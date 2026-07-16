import { describe, expect, it, vi } from 'vitest';
import {
  SKILL_GOVERNANCE_IDS,
  skillGovernanceDecisionFixture,
  skillGovernanceDecisionResponseFixture,
  skillGovernanceDetailFixture,
  skillGovernanceDetailResponseFixture,
  skillGovernanceListItemFixture,
  skillGovernanceListResponseFixture
} from '../../test/skillGovernanceFixtures.js';
import {
  createGovernanceDecisionKey,
  decideGovernanceSkill,
  getGovernanceSkill,
  listGovernanceSkills,
  SKILL_GOVERNANCE_PATH
} from './governanceApi.js';

describe('Governor API client', () => {
  it('uses the exact status query, Base64URL cursor, Cookie credentials and signal', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(skillGovernanceListResponseFixture()))
      .mockResolvedValueOnce(jsonResponse(skillGovernanceListResponseFixture()));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();

    await listGovernanceSkills({ status: 'active' });
    await listGovernanceSkills({ status: 'active', cursor: 'opaque_cursor-2', signal: controller.signal });

    expect(fetchMock.mock.calls[0][0]).toBe(`${SKILL_GOVERNANCE_PATH}?status=active`);
    expect(fetchMock.mock.calls[1][0]).toBe(`${SKILL_GOVERNANCE_PATH}?status=active&cursor=opaque_cursor-2`);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'GET', credentials: 'include' });
    expect(fetchMock.mock.calls[1][1].signal).toBe(controller.signal);
  });

  it('fails before fetch for an unknown status/cursor and rejects response status drift', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(skillGovernanceListResponseFixture({
      items: [skillGovernanceListItemFixture({
        governance_status: 'suspended',
        governance_epoch: 2,
        allowed_actions: ['resume', 'offline']
      })]
    })));
    vi.stubGlobal('fetch', fetchMock);

    await expect(listGovernanceSkills({ status: 'unknown' })).rejects.toThrow('status');
    await expect(listGovernanceSkills({ status: 'active', cursor: 'not padded=' })).rejects.toThrow('Base64URL');
    expect(fetchMock).not.toHaveBeenCalled();

    await expect(listGovernanceSkills({ status: 'active' })).rejects.toMatchObject({
      code: 'INVALID_SKILL_GOVERNANCE_RESPONSE',
      status: 502
    });
  });

  it('loads the exact detail resource and requires response identity plus matching HTTP ETag', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDetailResponseFixture(), 200, {
        ETag: '"skill-governance-etag-1"'
      }))
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDetailResponseFixture({
        skill: skillGovernanceDetailFixture({ skill_id: SKILL_GOVERNANCE_IDS.secondSkill })
      }), 200, { ETag: '"skill-governance-etag-1"' }))
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDetailResponseFixture(), 200, {
        ETag: '"different-governance-etag"'
      }))
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDetailResponseFixture()));
    vi.stubGlobal('fetch', fetchMock);

    const result = await getGovernanceSkill(SKILL_GOVERNANCE_IDS.skill);
    expect(result.skill.skillID).toBe(SKILL_GOVERNANCE_IDS.skill);
    expect(fetchMock.mock.calls[0][0]).toBe(`${SKILL_GOVERNANCE_PATH}/${SKILL_GOVERNANCE_IDS.skill}`);

    await expect(getGovernanceSkill(SKILL_GOVERNANCE_IDS.skill)).rejects.toMatchObject({ field: 'skill.skill_id' });
    await expect(getGovernanceSkill(SKILL_GOVERNANCE_IDS.skill)).rejects.toMatchObject({ field: 'ETag' });
    await expect(getGovernanceSkill(SKILL_GOVERNANCE_IDS.skill)).rejects.toMatchObject({ field: 'ETag' });
  });

  it('posts the exact decision with CSRF, original key and strong If-Match', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(skillGovernanceDecisionResponseFixture(), 200, {
      ETag: '"skill-governance-etag-2"'
    }));
    vi.stubGlobal('fetch', fetchMock);

    const result = await decideGovernanceSkill({
      skillID: SKILL_GOVERNANCE_IDS.skill,
      action: 'suspend',
      reasonCode: 'content_safety',
      approvalReference: 'TICKET-123',
      idempotencyKey: 'stable-governance-key',
      governanceETag: '"skill-governance-etag-1"',
      csrfToken: 'csrf-governor'
    });

    expect(result.skill).toMatchObject({ governanceStatus: 'suspended', governanceETag: '"skill-governance-etag-2"' });
    expect(fetchMock).toHaveBeenCalledWith(
      `${SKILL_GOVERNANCE_PATH}/${SKILL_GOVERNANCE_IDS.skill}/decisions`,
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        headers: expect.objectContaining({
          'Idempotency-Key': 'stable-governance-key',
          'If-Match': '"skill-governance-etag-1"',
          'X-CSRF-Token': 'csrf-governor',
          'Content-Type': 'application/json'
        }),
        body: JSON.stringify({
          action: 'suspend',
          reason_code: 'content_safety',
          approval_reference: 'TICKET-123'
        })
      })
    );
  });

  it.each([
    [{ skillID: 'NOT-A-UUID' }, 'UUIDv7'],
    [{ action: 'delete' }, 'action'],
    [{ reasonCode: 'risk_cleared' }, 'reason_code'],
    [{ approvalReference: 'ticket-123' }, 'approval_reference'],
    [{ idempotencyKey: 'key with spaces' }, 'Idempotency-Key'],
    [{ governanceETag: 'W/"weak"' }, 'strong'],
    [{ csrfToken: '' }, 'CSRF']
  ])('rejects malformed decision input before fetch: %o', async (overrides, message) => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    await expect(decideGovernanceSkill({
      skillID: SKILL_GOVERNANCE_IDS.skill,
      action: 'suspend',
      reasonCode: 'content_safety',
      approvalReference: 'TICKET-123',
      idempotencyKey: 'stable-governance-key',
      governanceETag: '"skill-governance-etag-1"',
      csrfToken: 'csrf-governor',
      ...overrides
    })).rejects.toThrow(message);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('rejects decision response resource, action-result and HTTP ETag drift as unknown outcome', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDecisionResponseFixture({
        skill: skillGovernanceDecisionFixture({ skill_id: SKILL_GOVERNANCE_IDS.secondSkill })
      }), 200, { ETag: '"skill-governance-etag-2"' }))
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDecisionResponseFixture({
        skill: skillGovernanceDecisionFixture({
          governance_status: 'offline',
          allowed_actions: []
        })
      }), 200, { ETag: '"skill-governance-etag-2"' }))
      .mockResolvedValueOnce(jsonResponse(skillGovernanceDecisionResponseFixture(), 200, {
        ETag: '"wrong-etag"'
      }));
    vi.stubGlobal('fetch', fetchMock);

    const command = {
      skillID: SKILL_GOVERNANCE_IDS.skill,
      action: 'suspend',
      reasonCode: 'content_safety',
      approvalReference: 'TICKET-123',
      idempotencyKey: 'stable-governance-key',
      governanceETag: '"skill-governance-etag-1"',
      csrfToken: 'csrf-governor'
    };
    await expect(decideGovernanceSkill(command)).rejects.toMatchObject({ field: 'skill.skill_id', status: 502 });
    await expect(decideGovernanceSkill(command)).rejects.toMatchObject({ field: 'skill.governance_status', status: 502 });
    await expect(decideGovernanceSkill(command)).rejects.toMatchObject({ field: 'ETag', status: 502 });
  });

  it('creates a namespaced secure decision key', () => {
    vi.stubGlobal('crypto', { randomUUID: vi.fn(() => '12345678-1234-4123-8123-123456789abc') });
    expect(createGovernanceDecisionKey()).toBe(
      'skill-governance-decision-12345678-1234-4123-8123-123456789abc'
    );
  });
});

function jsonResponse(payload, status = 200, extraHeaders = {}) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json', ...extraHeaders }
  });
}
