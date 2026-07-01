import { useSyncExternalStore } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import {
  Blocks,
  BookOpenCheck,
  Boxes,
  ClipboardList,
  Coins,
  CreditCard,
  FileText,
  Gauge,
  LogOut,
  Package,
  Shield,
  Sparkles,
  UserCog,
  Users,
  WandSparkles,
  Wrench
} from 'lucide-react';
import { adminApi } from '../lib/api/admin.js';
import { clearAdminSession, getAdminSession, subscribeAdminSession } from '../lib/auth/session.js';
import { IconButton } from '../components/admin/IconButton.jsx';
import { ToastProvider, useToast } from '../components/admin/Toast.jsx';

export const navGroups = [
  { title: '概览', items: [{ to: '/admin', label: '后台首页', icon: Gauge, end: true }] },
  {
    title: '账号与用户',
    items: [
      { to: '/admin/admins', label: '管理员账号', icon: UserCog },
      { to: '/admin/users', label: '用户管理', icon: Users }
    ]
  },
  {
    title: '能力配置',
    items: [
      { to: '/admin/skills/system', label: '系统 Skill', icon: WandSparkles },
      { to: '/admin/skills/reviews', label: 'Skill 审核', icon: BookOpenCheck },
      { to: '/admin/skills/marketplace', label: 'Skill 市场', icon: Boxes },
      { to: '/admin/models/providers', label: '模型供应商', icon: Blocks },
      { to: '/admin/models', label: '模型管理', icon: Sparkles, end: true },
      { to: '/admin/tools', label: 'Tool 管理', icon: Wrench }
    ]
  },
  {
    title: '付费与财务',
    items: [
      { to: '/admin/billing/packages', label: '付费套餐', icon: Package },
      { to: '/admin/billing/skus', label: 'SKU 管理', icon: CreditCard },
      { to: '/admin/billing/orders', label: '订单管理', icon: ClipboardList },
      { to: '/admin/billing/credit-lots', label: '积分批次', icon: Coins },
      { to: '/admin/billing/redeem-codes', label: '兑换码', icon: ClipboardList },
      { to: '/admin/billing/enterprise-contracts', label: '企业合同', icon: FileText },
      { to: '/admin/billing/invoices', label: '发票财务', icon: FileText },
      { to: '/admin/billing/promotions', label: '促销活动', icon: CreditCard }
    ]
  },
  {
    title: '运营与审计',
    items: [
      { to: '/admin/credits/grants', label: '积分发放', icon: Coins },
      { to: '/admin/skills/refunds', label: 'Skill 退款', icon: ClipboardList },
      { to: '/admin/skills/settlements', label: 'Skill 结算', icon: Coins },
      { to: '/admin/credits/codes', label: '兑换码', icon: ClipboardList },
      { to: '/admin/works/public', label: '精选作品', icon: Boxes },
      { to: '/admin/audit-logs', label: '审计日志', icon: Shield },
      { to: '/admin/asset-element-types', label: '资产元素类型', icon: Boxes }
    ]
  }
];

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
