import { userRequest } from './client.js';

export const marketplaceApi = {
  listSkills: (query = {}) => userRequest('/api/marketplace/skills', { query }),
  getSkill: (listingId) => userRequest(`/api/marketplace/skills/${encodeURIComponent(listingId)}`),
  listInstalledSkills: (query = {}) => userRequest('/api/marketplace/my-skills', { query }),
  installSkill: (body, options = {}) => userRequest('/api/marketplace/installations', { method: 'POST', body, ...options }),
  upgradeInstallation: (installationId, body, options = {}) =>
    userRequest(`/api/marketplace/installations/${encodeURIComponent(installationId)}/upgrade`, { method: 'POST', body, ...options })
};
