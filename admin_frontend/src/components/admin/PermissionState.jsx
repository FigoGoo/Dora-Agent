import { ShieldAlert } from 'lucide-react';

export function PermissionState({ title = '无权访问', description = '当前账号没有访问该后台能力的权限。' }) {
  return (
    <div className="admin-permission">
      <ShieldAlert aria-hidden="true" size={28} />
      <strong>{title}</strong>
      <p>{description}</p>
    </div>
  );
}
