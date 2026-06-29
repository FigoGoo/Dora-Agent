import { Inbox } from 'lucide-react';

export function EmptyState({ title = '暂无数据', description, action }) {
  return (
    <div className="admin-empty">
      <Inbox aria-hidden="true" size={24} />
      <strong>{title}</strong>
      {description ? <p>{description}</p> : null}
      {action}
    </div>
  );
}
