import { useSyncExternalStore } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { LogOut } from 'lucide-react';
import { adminApi } from '../../services/adminApi.js';
import { clearAdminSession, getAdminSession, subscribeAdminSession } from '../../services/session.js';
import { IconButton } from '../admin/IconButton.jsx';
import { ToastProvider, useToast } from '../admin/Toast.jsx';
import { navGroups } from './navGroups.js';

function AdminShellInner() {
  const session = useSyncExternalStore(subscribeAdminSession, getAdminSession, getAdminSession);
  const navigate = useNavigate();
  const toast = useToast();

  async function logout() {
    try {
      await adminApi.logout();
      toast?.notify('已退出后台');
    } catch {
      toast?.notify('本地登录态已清理', 'warning');
    } finally {
      clearAdminSession();
      navigate('/admin/login', { replace: true });
    }
  }

  return (
    <div className="admin-shell">
      <aside className="admin-side-nav">
        <div className="admin-brand">
          <img src="/dora-admin-mark-dense-generated.png" alt="" aria-hidden="true" className="admin-brand__mark" />
          <div className="admin-brand__copy">
            <strong>Dora</strong>
            <span>平台后台</span>
          </div>
        </div>
        <nav aria-label="平台后台导航">
          {navGroups.map((group) => (
            <section key={group.title}>
              <h2>{group.title}</h2>
              {group.items.map((item) => (
                <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => (isActive ? 'is-active' : '')}>
                  <item.icon aria-hidden="true" size={18} />
                  <span>{item.label}</span>
                </NavLink>
              ))}
            </section>
          ))}
        </nav>
      </aside>
      <div className="admin-main">
        <header className="admin-top-bar">
          <div>
            <strong>{session?.account || '平台管理员'}</strong>
            <span>{session?.admin_id || '独立后台登录态'}</span>
          </div>
          <IconButton label="退出登录" icon={LogOut} onClick={logout} />
        </header>
        <main className="admin-page">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export function AdminShell() {
  return (
    <ToastProvider>
      <AdminShellInner />
    </ToastProvider>
  );
}
