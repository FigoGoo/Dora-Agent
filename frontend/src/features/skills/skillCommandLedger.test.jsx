import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import {
  AUTH_SESSION_STATUS,
  AuthSessionProvider,
  useAuthSession
} from '../../platform/auth/authSession.js';
import { SkillCommandLedgerProvider, useSkillCommandLedger } from './skillCommandLedger.jsx';

describe('SkillCommandLedgerProvider', () => {
  it('clears in-memory commands on Principal switch and logout', async () => {
    const user = userEvent.setup();
    const client = {
      bootstrap: vi.fn(),
      login: vi.fn().mockResolvedValue(authPayload('user-b')),
      logout: vi.fn().mockResolvedValue(null)
    };
    render(
      <AuthSessionProvider
        autoBootstrap={false}
        client={client}
        initialSession={{
          status: AUTH_SESSION_STATUS.AUTHENTICATED,
          user: { id: 'user-a', name: 'A' },
          csrfToken: 'csrf-a',
          sessionExpiresAt: '2026-07-15T08:00:00Z'
        }}
      >
        <SkillCommandLedgerProvider>
          <LedgerProbe />
        </SkillCommandLedgerProvider>
      </AuthSessionProvider>
    );

    await user.click(screen.getByRole('button', { name: '绑定命令' }));
    expect(screen.getByTestId('command-key')).toHaveTextContent('key-user-a');

    await user.click(screen.getByRole('button', { name: '切换 Principal' }));
    await waitFor(() => expect(screen.getByTestId('principal-id')).toHaveTextContent('user-b'));
    expect(screen.getByTestId('command-key')).toHaveTextContent('empty');

    await user.click(screen.getByRole('button', { name: '绑定命令' }));
    expect(screen.getByTestId('command-key')).toHaveTextContent('key-user-b');
    await user.click(screen.getByRole('button', { name: '退出登录' }));
    await waitFor(() => expect(screen.getByTestId('principal-id')).toHaveTextContent('anonymous'));
    expect(screen.getByTestId('command-key')).toHaveTextContent('empty');
  });

  it('clears commands for a successful same-Principal bootstrap authority epoch', async () => {
    const user = userEvent.setup();
    const client = {
      bootstrap: vi.fn().mockResolvedValue(authPayload('user-a')),
      login: vi.fn(),
      logout: vi.fn()
    };
    render(
      <AuthSessionProvider
        autoBootstrap={false}
        client={client}
        initialSession={{
          status: AUTH_SESSION_STATUS.AUTHENTICATED,
          user: { id: 'user-a', name: 'A', roles: ['user'], capabilities: ['skill.write'] },
          csrfToken: 'csrf-a',
          sessionExpiresAt: '2026-07-15T08:00:00Z'
        }}
      >
        <SkillCommandLedgerProvider><LedgerProbe /></SkillCommandLedgerProvider>
      </AuthSessionProvider>
    );

    await user.click(screen.getByRole('button', { name: '绑定命令' }));
    expect(screen.getByTestId('command-key')).toHaveTextContent('key-user-a');
    await user.click(screen.getByRole('button', { name: '重新解析 Session' }));

    await waitFor(() => expect(client.bootstrap).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId('principal-id')).toHaveTextContent('user-a');
    expect(screen.getByTestId('command-key')).toHaveTextContent('empty');
  });
});

function LedgerProbe() {
  const auth = useAuthSession();
  const ledger = useSkillCommandLedger();
  const [, setRevision] = useState(0);
  const principalID = auth.user?.id || 'anonymous';
  return (
    <section>
      <span data-testid="principal-id">{principalID}</span>
      <span data-testid="command-key">{ledger.get('skill:create')?.key || 'empty'}</span>
      <button type="button" onClick={() => {
        ledger.set('skill:create', { key: `key-${principalID}`, semantic: 'semantic' });
        setRevision((value) => value + 1);
      }}>
        绑定命令
      </button>
      <button type="button" onClick={() => auth.login({ email: 'b@example.com', password: 'secret' })}>
        切换 Principal
      </button>
      <button type="button" onClick={() => auth.logout()}>
        退出登录
      </button>
      <button type="button" onClick={() => auth.retryBootstrap()}>
        重新解析 Session
      </button>
    </section>
  );
}

function authPayload(principalID) {
  return {
    status: 'authenticated',
    principal: {
      id: principalID,
      display_name: principalID,
      email: `${principalID}@example.com`,
      account_status: 'active',
      roles: ['user'],
      capabilities: ['skill.write']
    },
    csrf_token: `csrf-${principalID}`,
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}
