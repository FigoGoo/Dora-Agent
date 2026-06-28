import { X } from 'lucide-react';
import { IconButton } from './IconButton.jsx';

export function Modal({ open, title, children, footer, onClose, size = 'md' }) {
  if (!open) {
    return null;
  }
  return (
    <div className="admin-overlay" role="presentation">
      <section className={`admin-modal admin-modal--${size}`} role="dialog" aria-modal="true" aria-label={title}>
        <header>
          <h2>{title}</h2>
          <IconButton label="关闭" icon={X} tooltip={false} onClick={onClose} />
        </header>
        <div className="admin-modal__body">{children}</div>
        {footer ? <footer>{footer}</footer> : null}
      </section>
    </div>
  );
}
