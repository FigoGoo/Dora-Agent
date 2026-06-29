import { UserCircle } from 'lucide-react';
import { PUBLIC_PAGES } from '../../app/routes.js';
import { BrandLogo } from '../brand/BrandLogo.jsx';

export function SideNav({ activePage, activeNavTarget, isLoggedIn, navItems, onNavigate, onLogin, onToggleAccountMenu }) {
  return (
    <aside className="side-nav" aria-label="DORAIGC 导航">
      <BrandLogo />
      <nav>
        {navItems.map((item) => {
          const needsLogin = !isLoggedIn && !PUBLIC_PAGES.has(item.page);
          const isActive = activePage === item.page && (item.targetId ? activeNavTarget === item.targetId : !activeNavTarget);

          return (
            <button
              key={item.label}
              type="button"
              className={isActive ? 'nav-item is-active' : 'nav-item'}
              onClick={() => (needsLogin ? onLogin(`进入${item.label}`, `登录后进入${item.label}，继续刚才的创作安排。`, item.page) : onNavigate(item.page, item.targetId))}
            >
              <item.icon aria-hidden="true" size={24} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>
      {isLoggedIn ? (
        <div className="side-nav__footer">
          <button type="button" className="ghost-link" onClick={onToggleAccountMenu}>
            <UserCircle aria-hidden="true" size={18} />
            <span>账户</span>
          </button>
        </div>
      ) : null}
    </aside>
  );
}
