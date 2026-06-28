import { createElement } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { getAdminSession } from '../lib/auth/session.js';

export function getAdminEntryPath(session) {
  if (!session?.admin_id) {
    return '/admin/login';
  }
  if (session.must_rotate_password) {
    return '/admin/rotate-password';
  }
  return '/admin';
}

export function RequireAdminSession() {
  const location = useLocation();
  const session = getAdminSession();

  if (!session?.admin_id) {
    return createElement(Navigate, { to: '/admin/login', replace: true, state: { from: location.pathname } });
  }
  if (session.must_rotate_password && location.pathname !== '/admin/rotate-password') {
    return createElement(Navigate, { to: '/admin/rotate-password', replace: true });
  }
  return createElement(Outlet);
}
