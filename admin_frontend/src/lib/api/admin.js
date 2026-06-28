import { adminRequest } from './client.js';

export const adminApi = {
  login: (body) => adminRequest('/api/admin/auth/login', { method: 'POST', body }),
  logout: () => adminRequest('/api/admin/auth/logout', { method: 'POST', body: {} }),
  rotatePassword: (body) => adminRequest('/api/admin/auth/rotate-password', { method: 'POST', body, reason: body.reason }),
  dashboard: () => adminRequest('/api/admin/dashboard'),
  list: (path, query) => adminRequest(path, { query }),
  get: (path) => adminRequest(path),
  post: (path, body = {}, options = {}) => adminRequest(path, { method: 'POST', body, ...options }),
  patch: (path, body = {}, options = {}) => adminRequest(path, { method: 'PATCH', body, ...options }),
  put: (path, body = {}, options = {}) => adminRequest(path, { method: 'PUT', body, ...options }),
  previewUserStatus: (userId, body) =>
    adminRequest(`/api/admin/users/${userId}/status/preview`, { method: 'POST', body }),
  confirmUserStatus: (userId, body, reason) =>
    adminRequest(`/api/admin/users/${userId}/status/confirm`, { method: 'POST', body, reason }),
  previewTakeDownWork: (publicWorkId, body, reason) =>
    adminRequest(`/api/admin/works/public/${publicWorkId}/take-down/preview`, { method: 'POST', body, reason }),
  confirmTakeDownWork: (publicWorkId, body, reason) =>
    adminRequest(`/api/admin/works/public/${publicWorkId}/take-down/confirm`, { method: 'POST', body, reason })
};
