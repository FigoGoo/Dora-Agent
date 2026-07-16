import { useEffect, useState } from 'react';
import { X } from 'lucide-react';
import { BrandLogo } from '../brand/BrandLogo.jsx';

export function LoginModal({ intent, onClose, onSubmit }) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState('');

  useEffect(() => {
    setEmail('');
    setPassword('');
    setIsSubmitting(false);
    setErrorMessage('');
  }, [intent]);

  if (!intent) {
    return null;
  }

  async function handleSubmit(event) {
    event.preventDefault();
    if (isSubmitting) {
      return;
    }
    if (!email.trim() || !password) {
      setErrorMessage('请输入邮箱和密码');
      return;
    }
    setIsSubmitting(true);
    setErrorMessage('');
    try {
      await onSubmit({ email: email.trim(), password });
    } catch (error) {
      setErrorMessage(loginErrorMessage(error));
      setIsSubmitting(false);
    }
  }

  return (
    <div className="modal-backdrop" onClick={isSubmitting ? undefined : onClose}>
      <section className="login-modal" role="dialog" aria-modal="true" aria-labelledby="login-title" onClick={(event) => event.stopPropagation()}>
        <button className="icon-button login-modal__close" type="button" aria-label="关闭登录弹窗" title="关闭" onClick={onClose} disabled={isSubmitting}>
          <X aria-hidden="true" size={18} />
        </button>
        <div className="login-modal__brand-panel">
          <BrandLogo compact />
          <span className="login-modal__badge">已为你保留</span>
          <strong>当前想法和入口会在登录后继续。</strong>
        </div>
        <form className="login-modal__form" onSubmit={handleSubmit}>
          <h2 id="login-title">登录后继续创作</h2>
          <p>用账号保存项目、素材和积分记录，这次的动作不会丢。</p>
          <div className="intent-preview">
            <span>{intent.title}</span>
            <strong>{intent.prompt}</strong>
          </div>
          <label>
            <span>邮箱</span>
            <input
              name="email"
              type="email"
              autoComplete="username"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              disabled={isSubmitting}
            />
          </label>
          <label>
            <span>密码</span>
            <input
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              disabled={isSubmitting}
            />
          </label>
          {errorMessage ? <p className="login-modal__error" role="alert">{errorMessage}</p> : null}
          <div className="login-modal__actions">
            <button className="start-button" type="submit" disabled={isSubmitting}>
              {isSubmitting ? '登录中…' : '登录并继续'}
            </button>
            <button className="secondary-button" type="button" disabled>注册账号</button>
          </div>
          <button className="subtle-button" type="button" onClick={onClose} disabled={isSubmitting}>稍后再说</button>
        </form>
      </section>
    </div>
  );
}

function loginErrorMessage(error) {
  if (error?.code === 'AUTH_INVALID_CREDENTIALS') {
    return '邮箱或密码错误';
  }
  if (error?.code === 'AUTH_RATE_LIMITED' || Number(error?.status) === 429) {
    return '登录尝试过于频繁，请稍后再试';
  }
  return String(error?.message || '登录失败，请稍后重试');
}
