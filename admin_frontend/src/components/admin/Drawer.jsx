import { X } from 'lucide-react';
import { IconButton } from './IconButton.jsx';

export function Drawer({ open, title, children, onClose }) {
  if (!open) {
    return null;
  }
  return (
    <div className="admin-overlay admin-overlay--drawer" role="presentation">
      <aside className="admin-drawer" role="dialog" aria-modal="true" aria-label={title}>
        <header>
          <h2>{title}</h2>
          <IconButton label="关闭" icon={X} tooltip={false} onClick={onClose} />
        </header>
        <div className="admin-drawer__body">{children}</div>
      </aside>
    </div>
  );
}
