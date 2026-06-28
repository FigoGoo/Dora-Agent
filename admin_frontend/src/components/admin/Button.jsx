export function Button({ children, variant = 'secondary', size = 'md', loading = false, icon: Icon, ...props }) {
  const iconSize = size === 'sm' || size === 'row' ? 14 : 16;

  return (
    <button className={`admin-btn admin-btn--${variant} admin-btn--${size}`} disabled={loading || props.disabled} {...props}>
      {Icon ? <Icon aria-hidden="true" size={iconSize} /> : null}
      <span>{loading ? '处理中' : children}</span>
    </button>
  );
}
