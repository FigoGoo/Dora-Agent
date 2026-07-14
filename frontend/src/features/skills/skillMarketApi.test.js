import { describe, expect, it, vi } from 'vitest';
import {
  SKILL_MARKET_IDS,
  skillMarketDetailResponseFixture,
  skillMarketListResponseFixture
} from '../../test/skillMarketFixtures.js';
import { getSkillMarketDetail, listSkillMarket } from './skillMarketApi.js';

describe('Skill Market API', () => {
  it('loads the anonymous list with only an optional opaque cursor', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(skillMarketListResponseFixture({ next_cursor: 'next_1' })));
    vi.stubGlobal('fetch', fetchMock);

    const result = await listSkillMarket({ cursor: 'cursor_1' });

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/skill-market?cursor=cursor_1', expect.objectContaining({
      method: 'GET',
      credentials: 'include'
    }));
    expect(result.nextCursor).toBe('next_1');
  });

  it('loads a canonical detail through the independent public parser', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(skillMarketDetailResponseFixture()));
    vi.stubGlobal('fetch', fetchMock);

    const result = await getSkillMarketDetail(SKILL_MARKET_IDS.skill);

    expect(fetchMock.mock.calls[0][0]).toBe(`/api/v1/skill-market/${SKILL_MARKET_IDS.skill}`);
    expect(result.skill.skillID).toBe(SKILL_MARKET_IDS.skill);
    expect(result.skill).not.toHaveProperty('definition');
  });

  it('rejects a non-canonical detail ID before making a request', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    await expect(getSkillMarketDetail(SKILL_MARKET_IDS.skill.toUpperCase())).rejects.toThrow('规范小写 UUIDv7');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

function jsonResponse(payload, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } });
}
