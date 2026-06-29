import { Circle, CircleAlert, CircleCheck, Clock3 } from 'lucide-react';
import { statusTone } from '../../utils/format.js';

const toneIcon = {
  success: CircleCheck,
  danger: CircleAlert,
  warning: Clock3,
  neutral: Circle
};

export function Badge({ children, tone }) {
  const resolvedTone = tone || statusTone(children);
  const Icon = toneIcon[resolvedTone] || Circle;
  return (
    <span className={`admin-badge admin-badge--${resolvedTone}`}>
      <Icon aria-hidden="true" size={12} />
      {children || '-'}
    </span>
  );
}
