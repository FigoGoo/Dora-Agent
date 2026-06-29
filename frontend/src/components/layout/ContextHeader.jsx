import { Bell, FolderKanban, Ticket, UserCircle } from 'lucide-react';
import { AccountMenu } from './AccountMenu.jsx';

export function ContextHeader({ activePage, isLoggedIn, user, isAccountMenuOpen, onLogin, onToggleAccountMenu, onOpenCredits }) {
  return (
    <header className={activePage === 'projects' ? 'context-header context-header--projects' : 'context-header'}>
      {activePage === 'projects' ? (
        <div className="projects-page__title">
          <FolderKanban aria-hidden="true" size={18} />
          <h1 id="projects-title">项目</h1>
        </div>
      ) : (
        <div className="status-pill attention-tag">
          <span className="status-dot" />
          DORAIGC 创作者招募中
        </div>
      )}
      <div className="context-header__actions">
        <button className="credit-pill" type="button" onClick={isLoggedIn ? onOpenCredits : () => onLogin('查看积分')}>
          <Ticket aria-hidden="true" size={16} />
          <span>{isLoggedIn ? user.credits : 148}</span>
          <span>积分</span>
        </button>
        <button className="icon-button" type="button" aria-label="通知" title="通知" onClick={() => onLogin('查看通知')}>
          <Bell aria-hidden="true" size={18} />
        </button>
        {isLoggedIn ? (
          <div className="account-menu-shell">
            <span className="plan-pill">{user.plan}</span>
            <button
              className="avatar-button"
              type="button"
              aria-label="用户菜单"
              aria-expanded={isAccountMenuOpen}
              onClick={onToggleAccountMenu}
            >
              <UserCircle aria-hidden="true" size={22} />
            </button>
            {isAccountMenuOpen ? <AccountMenu user={user} onOpenCredits={onOpenCredits} /> : null}
          </div>
        ) : (
          <button className="login-button" type="button" onClick={() => onLogin('登录')}>
            登录
          </button>
        )}
      </div>
    </header>
  );
}
