import { Bell, Blocks, FolderKanban, Ticket, UserCircle } from 'lucide-react';
import { AccountMenu } from './AccountMenu.jsx';

const PAGE_TITLE_CONFIG = {
  projects: {
    icon: FolderKanban,
    id: 'projects-title',
    label: '项目'
  },
  skills: {
    icon: Blocks,
    id: 'skills-title',
    label: 'Skill'
  }
};

export function ContextHeader({ activePage, isLoggedIn, user, isAccountMenuOpen, onLogin, onToggleAccountMenu, onOpenCredits }) {
  const pageTitle = PAGE_TITLE_CONFIG[activePage];
  const PageIcon = pageTitle?.icon;
  const headerClassName = pageTitle ? `context-header context-header--${activePage}` : 'context-header';

  return (
    <header className={headerClassName}>
      {pageTitle ? (
        <div className="context-page-title">
          <PageIcon aria-hidden="true" size={18} />
          <h1 id={pageTitle.id}>{pageTitle.label}</h1>
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
