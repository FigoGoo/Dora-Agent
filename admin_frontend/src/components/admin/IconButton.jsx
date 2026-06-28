import { Tooltip } from './Tooltip.jsx';

function IconButtonContent({ label, icon: Icon, ...props }) {
  return (
    <button className="admin-icon-btn" aria-label={label} {...props}>
      <Icon aria-hidden="true" size={18} />
    </button>
  );
}

export function IconButton({ label, icon: Icon, tooltip = true, ...props }) {
  if (!tooltip) {
    return <IconButtonContent label={label} icon={Icon} {...props} />;
  }
  return (
    <Tooltip content={label}>
      <IconButtonContent label={label} icon={Icon} {...props} />
    </Tooltip>
  );
}
