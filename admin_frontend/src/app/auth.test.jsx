import { describe, expect, test } from 'vitest';
import { getAdminEntryPath } from './auth.js';

describe('admin auth routing', () => {
  test('sends unauthenticated admins to login and password-rotation sessions to rotate page', () => {
    expect(getAdminEntryPath(null)).toBe('/admin/login');
    expect(getAdminEntryPath({ admin_id: 'adm_1', must_rotate_password: true })).toBe('/admin/rotate-password');
    expect(getAdminEntryPath({ admin_id: 'adm_1', must_rotate_password: false })).toBe('/admin');
  });
});
