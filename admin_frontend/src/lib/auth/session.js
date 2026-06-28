const SESSION_KEY = 'dora_admin_session';

let cachedRaw = undefined;
let cachedSession = null;

function storage() {
  return globalThis.localStorage || globalThis.sessionStorage;
}

function isExpired(session) {
  if (!session?.expires_at) {
    return false;
  }
  const expiresAt = Date.parse(session.expires_at);
  return Number.isFinite(expiresAt) && expiresAt <= Date.now();
}

function resetCache() {
  cachedRaw = null;
  cachedSession = null;
}

function notifySessionChange() {
  window.dispatchEvent(new Event('admin-session-change'));
}

export function getAdminSession() {
  const raw = storage().getItem(SESSION_KEY);
  if (raw === cachedRaw) {
    if (isExpired(cachedSession)) {
      clearAdminSession();
      return null;
    }
    return cachedSession;
  }
  cachedRaw = raw;
  if (!raw) {
    cachedSession = null;
    return null;
  }
  try {
    cachedSession = JSON.parse(raw);
    if (isExpired(cachedSession)) {
      clearAdminSession();
      return null;
    }
    return cachedSession;
  } catch {
    resetCache();
    storage().removeItem(SESSION_KEY);
    notifySessionChange();
    return null;
  }
}

export function saveAdminSession(session) {
  cachedRaw = JSON.stringify(session);
  cachedSession = session;
  storage().setItem(SESSION_KEY, cachedRaw);
  notifySessionChange();
}

export function renewAdminSession(expiresAt) {
  const session = getAdminSession();
  if (!session || !expiresAt) {
    return;
  }
  saveAdminSession({ ...session, expires_at: expiresAt });
}

export function clearAdminSession() {
  resetCache();
  storage().removeItem(SESSION_KEY);
  notifySessionChange();
}

export function subscribeAdminSession(listener) {
  window.addEventListener('admin-session-change', listener);
  window.addEventListener('storage', listener);
  return () => {
    window.removeEventListener('admin-session-change', listener);
    window.removeEventListener('storage', listener);
  };
}
