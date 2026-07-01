import { userRequest } from './client.js';

export const creatorApi = {
  createSkillDraft: (body, options = {}) => userRequest('/api/creator/skills', { method: 'POST', body, ...options }),
  submitSkillVersion: (skillId, version, body = {}, options = {}) =>
    userRequest(`/api/creator/skills/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}/submit`, { method: 'POST', body, ...options }),
  listListings: (query = {}) => userRequest('/api/creator/listings', { query }),
  getSkillUsageAnalytics: () => userRequest('/api/creator/analytics/skill-usage')
};
