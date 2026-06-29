import { FileClock } from 'lucide-react';

export function AuditHint({ children = '操作成功后将写入审计日志。' }) {
  return (
    <p className="admin-audit-hint">
      <FileClock aria-hidden="true" size={16} />
      {children}
    </p>
  );
}
