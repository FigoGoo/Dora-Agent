import { X } from 'lucide-react';
import { BrandLogo } from '../brand/BrandLogo.jsx';

export function LoginModal({ intent, onClose, onComplete }) {
  if (!intent) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section className="login-modal" role="dialog" aria-modal="true" aria-labelledby="login-title" onClick={(event) => event.stopPropagation()}>
        <button className="icon-button login-modal__close" type="button" aria-label="关闭登录弹窗" title="关闭" onClick={onClose}>
          <X aria-hidden="true" size={18} />
        </button>
        <div className="login-modal__brand-panel">
          <BrandLogo compact />
          <span className="login-modal__badge">已为你保留</span>
          <strong>当前想法和入口会在登录后继续。</strong>
        </div>
        <div className="login-modal__form">
          <h2 id="login-title">登录后继续创作</h2>
          <p>用账号保存项目、素材和积分记录，这次的动作不会丢。</p>
          <div className="intent-preview">
            <span>{intent.title}</span>
            <strong>{intent.prompt}</strong>
          </div>
          <div className="login-modal__actions">
            <button className="start-button" type="button" onClick={onComplete}>登录并继续</button>
            <button className="secondary-button" type="button" onClick={onComplete}>注册账号</button>
          </div>
          <button className="subtle-button" type="button" onClick={onClose}>稍后再说</button>
        </div>
      </section>
    </div>
  );
}
