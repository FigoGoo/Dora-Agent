import { requestJSON } from '../../platform/api/apiClient.js';
import {
  isCanonicalSkillMarketUUIDv7,
  parseSkillMarketDetailResponse,
  parseSkillMarketListResponse
} from './skillMarketContract.js';

export const SKILL_MARKET_PATH = '/api/v1/skill-market';

export async function listSkillMarket({ cursor, signal } = {}) {
  const query = new URLSearchParams();
  if (cursor != null) query.set('cursor', String(cursor));
  const suffix = query.size ? `?${query.toString()}` : '';
  const payload = await requestJSON(`${SKILL_MARKET_PATH}${suffix}`, { method: 'GET', signal });
  return parseSkillMarketListResponse(payload);
}

export async function getSkillMarketDetail(skillID, { signal } = {}) {
  if (!isCanonicalSkillMarketUUIDv7(skillID)) {
    throw new TypeError('Skill Market 详情 API 需要规范小写 UUIDv7 skill_id');
  }
  const payload = await requestJSON(`${SKILL_MARKET_PATH}/${skillID}`, { method: 'GET', signal });
  return parseSkillMarketDetailResponse(payload);
}
