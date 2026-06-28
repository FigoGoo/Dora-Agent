export function Tooltip({ content, children }) {
  return (
    <span className="admin-tooltip">
      {children}
      <span className="admin-tooltip__bubble" role="tooltip">
        {content}
      </span>
    </span>
  );
}
