import { requestJSON } from '../api/apiClient.js';

export const AUTH_SESSION_PATH = '/api/v1/auth/session';

// bootstrapAuthSession 读取服务端 Cookie 对应的安全 Principal 和内存 CSRF Token。
export function bootstrapAuthSession({ signal } = {}) {
  return requestJSON(AUTH_SESSION_PATH, {
    method: 'GET',
    signal,
    suppressAuthExpiryNotification: true
  });
}

// loginAuthSession 建立服务端 Web Session；凭据只进入本次请求，不写入浏览器存储。
export function loginAuthSession(credentials, { signal } = {}) {
  return requestJSON(AUTH_SESSION_PATH, {
    method: 'POST',
    body: JSON.stringify({
      email: credentials?.email,
      password: credentials?.password
    }),
    signal,
    suppressAuthExpiryNotification: true
  });
}

// logoutAuthSession 撤销当前 Web Session。CSRF Token 仍只存在于内存会话快照中。
export function logoutAuthSession({ csrfToken, signal } = {}) {
  if (!csrfToken) {
    throw new TypeError('logoutAuthSession 需要内存中的 CSRF Token');
  }
  const headers = { 'X-CSRF-Token': csrfToken };
  return requestJSON(AUTH_SESSION_PATH, {
    method: 'DELETE',
    headers,
    signal,
    suppressAuthExpiryNotification: true
  });
}

export const authSessionAPI = Object.freeze({
  bootstrap: bootstrapAuthSession,
  login: loginAuthSession,
  logout: logoutAuthSession
});
