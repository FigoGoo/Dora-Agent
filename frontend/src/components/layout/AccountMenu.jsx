import { ChevronDown, Crown, Globe2, LogOut, MessageCircle, Settings } from 'lucide-react';
import { accountPointGroups } from '../../features/account/accountMock.js';

export function AccountMenu({ user, onOpenCredits, onLogout }) {
  return (
    <section className="account-menu account-menu--compact account-menu--slim" role="dialog" aria-modal="false" aria-labelledby="account-menu-title">
      <h2 id="account-menu-title" className="sr-only">账户与积分</h2>
      <div className="account-menu__profile">
        <div>
          <strong>{user.name || 'Dora 用户'}</strong>
          <span>{user.plan || '基础版'}</span>
          <p>{user.email}</p>
        </div>
      </div>
      <button className="membership-button membership-button--theme" type="button" aria-label="开通会员">
        <Crown aria-hidden="true" size={19} />
        <span>开通会员</span>
      </button>
      <div className="account-points" aria-label="积分概览">
        {accountPointGroups.map((group) => (
          <section className="account-point-group" key={group.title} aria-labelledby={`${group.title}-title`}>
            <div className="account-point-group__head">
              <group.icon aria-hidden="true" size={18} />
              <strong id={`${group.title}-title`}>{group.title}</strong>
              <span>{group.value}</span>
            </div>
            {group.items.map(([label, value]) => (
              <div className="account-point-row" key={label}>
                <span>{label}</span>
                {value ? <strong>{value}</strong> : null}
              </div>
            ))}
          </section>
        ))}
      </div>
      <button className="usage-button" type="button" onClick={onOpenCredits}>
        查看用量
      </button>
      <div className="account-menu__links">
        <button type="button">
          <Globe2 aria-hidden="true" size={19} />
          <span>语言</span>
          <strong>简体中文</strong>
          <ChevronDown aria-hidden="true" size={15} />
        </button>
        <button type="button">
          <MessageCircle aria-hidden="true" size={19} />
          <span>反馈</span>
        </button>
        <button type="button">
          <Settings aria-hidden="true" size={19} />
          <span>管理账户</span>
        </button>
        <button type="button" onClick={onLogout}>
          <LogOut aria-hidden="true" size={19} />
          <span>退出登录</span>
        </button>
      </div>
    </section>
  );
}
