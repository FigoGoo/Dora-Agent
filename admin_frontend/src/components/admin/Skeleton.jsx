export function SkeletonBlock({ rows = 4 }) {
  return (
    <div className="admin-skeleton" aria-label="正在加载数据">
      {Array.from({ length: rows }).map((_, index) => (
        <span key={index} />
      ))}
    </div>
  );
}
