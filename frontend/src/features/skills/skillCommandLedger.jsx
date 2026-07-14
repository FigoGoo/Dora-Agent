import { createContext, useContext, useEffect, useRef } from 'react';
import { AUTH_SESSION_STATUS, useAuthSession } from '../../platform/auth/authSession.js';

const SkillCommandLedgerContext = createContext(null);

// createSkillCommandLedger 创建仅驻留当前 App 生命周期的命令账本；不得持久化为浏览器权威状态。
// 页面刷新或进程崩溃后的 Unknown Outcome 恢复必须由服务端 recovery contract 承担。
export function createSkillCommandLedger() {
  const commands = new Map();
  return Object.freeze({
    get(scope) {
      return commands.get(String(scope)) || null;
    },
    set(scope, command) {
      const key = String(scope);
      const frozen = Object.freeze({ ...command });
      commands.set(key, frozen);
      return frozen;
    },
    clear(scope, expectedKey) {
      const key = String(scope);
      const current = commands.get(key);
      if (!current || (expectedKey != null && current.key !== expectedKey)) return false;
      return commands.delete(key);
    },
    clearAll() {
      commands.clear();
    }
  });
}

// SkillCommandLedgerProvider 将 Unknown Outcome 绑定提升到 Router 之上，并在会话或 Principal 变化时换新账本。
export function SkillCommandLedgerProvider({ children }) {
  const auth = useAuthSession();
  const authorityRef = useRef({ status: '', generation: 0, key: '' });
  if (auth.status === AUTH_SESSION_STATUS.AUTHENTICATED && authorityRef.current.status !== AUTH_SESSION_STATUS.AUTHENTICATED) {
    authorityRef.current.generation += 1;
  }
  authorityRef.current.status = auth.status;
  const authorityKey = auth.status === AUTH_SESSION_STATUS.AUTHENTICATED
    ? JSON.stringify([
        authorityRef.current.generation,
        String(auth.user?.id || ''),
        auth.user?.roles || [],
        auth.user?.capabilities || []
      ])
    : `${auth.status}:${authorityRef.current.generation}`;
  const ledgerRef = useRef(null);
  if (!ledgerRef.current) ledgerRef.current = createSkillCommandLedger();
  if (authorityRef.current.key !== authorityKey) {
    ledgerRef.current.clearAll();
    ledgerRef.current = createSkillCommandLedger();
    authorityRef.current.key = authorityKey;
  }
  useEffect(() => {
    return () => ledgerRef.current?.clearAll();
  }, []);
  return (
    <SkillCommandLedgerContext.Provider value={ledgerRef.current}>
      {children}
    </SkillCommandLedgerContext.Provider>
  );
}

// useOptionalSkillCommandLedger 允许 Builder 单测注入独立账本；生产路径始终由 App Provider 提供。
export function useOptionalSkillCommandLedger() {
  return useContext(SkillCommandLedgerContext);
}

export function useSkillCommandLedger() {
  const ledger = useOptionalSkillCommandLedger();
  if (!ledger) throw new Error('useSkillCommandLedger 必须在 SkillCommandLedgerProvider 内使用');
  return ledger;
}
