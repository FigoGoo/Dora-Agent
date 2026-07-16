// AUTH_SESSION_EXPIRED_EVENT 是 API Client 与 React 会话层之间的最小失效通知契约。
export const AUTH_SESSION_EXPIRED_EVENT = 'dora:auth-session-expired';

// notifyAuthSessionExpired 广播 Cookie 会话已经失效，不携带响应正文或凭据。
export function notifyAuthSessionExpired(error) {
  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') {
    return;
  }
  window.dispatchEvent(
    new CustomEvent(AUTH_SESSION_EXPIRED_EVENT, {
      detail: {
        status: Number(error?.status) || 401,
        code: String(error?.code || 'UNAUTHENTICATED'),
        request_id: String(error?.requestID || '')
      }
    })
  );
}
