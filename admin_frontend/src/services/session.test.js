import { beforeEach, describe, expect, test } from 'vitest';
import { clearAdminSession, getAdminSession, saveAdminSession } from './session.js';

describe('admin session store', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
  });

  test('returns the same snapshot reference while stored session is unchanged', () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'admin@dora.local', must_rotate_password: false, expires_at: '2099-01-01T00:00:00Z' });

    const first = getAdminSession();
    const second = getAdminSession();

    expect(second).toBe(first);
  });

  test('persists admin sessions for the seven day remember-login window', () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'admin@dora.local', must_rotate_password: false, expires_at: '2099-01-01T00:00:00Z' });

    sessionStorage.clear();

    expect(JSON.parse(localStorage.getItem('dora_admin_session')).admin_id).toBe('adm_1');
    expect(getAdminSession()?.admin_id).toBe('adm_1');
  });

  test('clears expired persisted sessions before routing or requests use them', () => {
    localStorage.setItem(
      'dora_admin_session',
      JSON.stringify({ admin_id: 'adm_expired', account: 'expired', access_token: 'expired_token', expires_at: '2000-01-01T00:00:00Z' })
    );

    expect(getAdminSession()).toBeNull();
    expect(localStorage.getItem('dora_admin_session')).toBeNull();
  });

  test('clearAdminSession removes the remembered session copy', () => {
    saveAdminSession({ admin_id: 'adm_1', account: 'admin@dora.local', access_token: 'token_1', expires_at: '2099-01-01T00:00:00Z' });

    clearAdminSession();

    expect(localStorage.getItem('dora_admin_session')).toBeNull();
    expect(getAdminSession()).toBeNull();
  });
});
