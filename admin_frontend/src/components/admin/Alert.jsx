import { AlertCircle, CheckCircle2, Info, TriangleAlert } from 'lucide-react';

const icons = {
  danger: AlertCircle,
  warning: TriangleAlert,
  success: CheckCircle2,
  info: Info
};

export function Alert({ tone = 'info', title, children, traceId }) {
  const Icon = icons[tone] || Info;
  return (
    <div className={`admin-alert admin-alert--${tone}`} role="alert">
      <Icon aria-hidden="true" size={18} />
      <div>
        {title ? <strong>{title}</strong> : null}
        {children ? <p>{children}</p> : null}
        {traceId ? <small>trace_id：{traceId}</small> : null}
      </div>
    </div>
  );
}
