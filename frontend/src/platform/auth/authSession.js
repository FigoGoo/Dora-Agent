import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState
} from 'react';
import { authSessionAPI } from './authApi.js';
import { AUTH_SESSION_EXPIRED_EVENT, notifyAuthSessionExpired } from './authEvents.js';

export { AUTH_SESSION_EXPIRED_EVENT, notifyAuthSessionExpired };

// AUTH_SESSION_STATUS 只描述可安全渲染的四种会话快照，不持久化认证 Token。
export const AUTH_SESSION_STATUS = Object.freeze({
  BOOTSTRAPPING: 'bootstrapping',
  ANONYMOUS: 'anonymous',
  AUTHENTICATED: 'authenticated',
  UNAVAILABLE: 'unavailable'
});

const AuthSessionContext = createContext(null);

// AuthSessionProvider 统一拥有 Bootstrap/Login/Logout 生命周期与内存 Principal。
export function AuthSessionProvider({
  children,
  initialSession,
  autoBootstrap = initialSession == null,
  client = authSessionAPI
}) {
  const [session, setSession] = useState(() => initialState(initialSession, autoBootstrap));
  const operationRef = useRef(0);

  useEffect(() => {
    function expireSession() {
      operationRef.current += 1;
      setSession(anonymousSession());
    }

    window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, expireSession);
    return () => window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, expireSession);
  }, []);

  const bootstrap = useCallback(async ({ deniedCapability = '' } = {}) => {
    const operation = ++operationRef.current;
    setSession(bootstrappingSession());
    try {
      const payload = await client.bootstrap();
      if (operation !== operationRef.current) {
        return null;
      }
      const next = withCapabilityDenial(authenticatedSessionFromPayload(payload), deniedCapability);
      setSession(next);
      return next;
    } catch (error) {
      if (operation !== operationRef.current) {
        throw error;
      }
      if (isMissingSession(error)) {
        const next = anonymousSession();
        setSession(next);
        return next;
      }
      const next = unavailableSession(error);
      setSession(next);
      throw error;
    }
  }, [client]);

  useEffect(() => {
    if (!autoBootstrap || initialSession != null) {
      return undefined;
    }
    bootstrap().catch(() => {});
    return () => {
      operationRef.current += 1;
    };
  }, [autoBootstrap, bootstrap, initialSession]);

  const login = useCallback(async (credentials) => {
    const operation = ++operationRef.current;
    setSession(bootstrappingSession());
    try {
      const payload = await client.login(credentials);
      if (operation !== operationRef.current) {
        return null;
      }
      const next = authenticatedSessionFromPayload(payload);
      setSession(next);
      return next;
    } catch (error) {
      if (operation === operationRef.current) {
        setSession(isInfrastructureFailure(error) ? unavailableSession(error) : anonymousSession(error));
      }
      throw error;
    }
  }, [client]);

  const logout = useCallback(async () => {
    const operation = ++operationRef.current;
    const csrfToken = session.csrfToken;
    setSession(bootstrappingSession());
    try {
      await client.logout({ csrfToken });
      if (operation !== operationRef.current) {
        return null;
      }
      const next = anonymousSession();
      setSession(next);
      return next;
    } catch (error) {
      if (operation !== operationRef.current) {
        throw error;
      }
      if (isMissingSession(error)) {
        const next = anonymousSession();
        setSession(next);
        return next;
      }
      setSession(unavailableSession(error));
      throw error;
    }
  }, [client, session.csrfToken]);

  const value = useMemo(
    () => ({
      ...session,
      isAuthenticated: session.status === AUTH_SESSION_STATUS.AUTHENTICATED,
      hasCapability: (capability) => hasCapability(session, capability),
      bootstrap,
      login,
      logout,
      retryBootstrap: bootstrap
    }),
    [bootstrap, login, logout, session]
  );

  return createElement(AuthSessionContext.Provider, { value }, children);
}

// useAuthSession 返回共享认证快照，禁止页面各自维护互相漂移的登录布尔值。
export function useAuthSession() {
  const value = useContext(AuthSessionContext);
  if (!value) {
    throw new Error('useAuthSession 必须在 AuthSessionProvider 内使用');
  }
  return value;
}

function initialState(initialSession, autoBootstrap) {
  if (initialSession != null) {
    return normalizeSession(initialSession);
  }
  return autoBootstrap ? bootstrappingSession() : anonymousSession();
}

function normalizeSession(session) {
  if (session?.status === AUTH_SESSION_STATUS.AUTHENTICATED && session.user) {
    return authenticatedSession(
      session.user,
      session.csrfToken,
      session.sessionExpiresAt,
      session.deniedCapabilities
    );
  }
  if (session?.status === AUTH_SESSION_STATUS.UNAVAILABLE) {
    return unavailableSession(session.error);
  }
  if (session?.status === AUTH_SESSION_STATUS.BOOTSTRAPPING) {
    return bootstrappingSession();
  }
  return anonymousSession();
}

function authenticatedSessionFromPayload(payload) {
  const principal = payload?.principal;
  if (
    payload?.status !== AUTH_SESSION_STATUS.AUTHENTICATED
    || !principal
    || typeof principal !== 'object'
    || !principal.id
    || principal.account_status !== 'active'
    || !Array.isArray(principal.roles)
    || !Array.isArray(principal.capabilities)
    || typeof payload.csrf_token !== 'string'
    || !payload.csrf_token
    || typeof payload.session_expires_at !== 'string'
    || !payload.session_expires_at
    || Number.isNaN(Date.parse(payload.session_expires_at))
  ) {
    const error = new Error('认证服务未返回有效 Principal');
    error.code = 'INVALID_AUTH_SESSION_RESPONSE';
    throw error;
  }
  return authenticatedSession(
    {
      id: principal.id,
      display_name: principal.display_name,
      email: principal.email,
      account_status: principal.account_status,
      roles: principal.roles,
      capabilities: principal.capabilities
    },
    payload.csrf_token,
    payload.session_expires_at
  );
}

function bootstrappingSession() {
  return {
    status: AUTH_SESSION_STATUS.BOOTSTRAPPING,
    user: null,
    csrfToken: '',
    sessionExpiresAt: '',
    deniedCapabilities: [],
    error: null
  };
}

function anonymousSession(error) {
  return {
    status: AUTH_SESSION_STATUS.ANONYMOUS,
    user: null,
    csrfToken: '',
    sessionExpiresAt: '',
    deniedCapabilities: [],
    error: error ? publicError(error) : null
  };
}

function authenticatedSession(user, csrfToken = '', sessionExpiresAt = '', deniedCapabilities = []) {
  return {
    status: AUTH_SESSION_STATUS.AUTHENTICATED,
    user: sanitizeUser(user),
    csrfToken: String(csrfToken || ''),
    sessionExpiresAt: String(sessionExpiresAt || ''),
    deniedCapabilities: stringList(deniedCapabilities),
    error: null
  };
}

// withCapabilityDenial keeps the authoritative Bootstrap Principal intact while separately latching
// an endpoint capability rejection. The latch prevents a stale Bootstrap projection from remounting
// the rejected page in a loop and is cleared by the next ordinary Bootstrap/Login authority epoch.
function withCapabilityDenial(session, capability) {
  if (
    session.status !== AUTH_SESSION_STATUS.AUTHENTICATED
    || typeof capability !== 'string'
    || !capability
  ) return session;
  return {
    ...session,
    deniedCapabilities: [capability]
  };
}

function hasCapability(session, capability) {
  return session.status === AUTH_SESSION_STATUS.AUTHENTICATED
    && typeof capability === 'string'
    && Boolean(capability)
    && session.user.capabilities.includes(capability)
    && !session.deniedCapabilities.includes(capability);
}

function unavailableSession(error) {
  return {
    status: AUTH_SESSION_STATUS.UNAVAILABLE,
    user: null,
    csrfToken: '',
    sessionExpiresAt: '',
    deniedCapabilities: [],
    error: publicError(error)
  };
}

function sanitizeUser(user) {
  return {
    id: String(user?.id || user?.user_id || user?.principal_id || ''),
    name: String(user?.name || user?.display_name || ''),
    email: String(user?.email || ''),
    avatar: String(user?.avatar || user?.avatar_url || ''),
    plan: String(user?.plan || ''),
    credits: user?.credits,
    roles: stringList(user?.roles),
    capabilities: stringList(user?.capabilities || user?.permissions),
    permissions: stringList(user?.permissions || user?.capabilities),
    accountStatus: String(user?.account_status || '')
  };
}

function stringList(value) {
  return Array.isArray(value) ? value.map((item) => String(item)).filter(Boolean) : [];
}

function publicError(error) {
  if (!error) {
    return null;
  }
  return {
    status: Number(error.status) || 0,
    code: String(error.code || 'AUTH_SESSION_UNAVAILABLE'),
    message: String(error.message || '认证服务暂不可用'),
    requestID: String(error.requestID || ''),
    retryable: Boolean(error.retryable)
  };
}

function isMissingSession(error) {
  return Number(error?.status) === 401;
}

function isInfrastructureFailure(error) {
  const status = Number(error?.status) || 0;
  return status === 0 || status >= 500;
}
