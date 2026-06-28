import { Search, X } from 'lucide-react';
import { AdminSelect } from './AdminSelect.jsx';
import { Button } from './Button.jsx';

export function FilterBar({ keyword, onKeywordChange, status, onStatusChange, statusOptions = [], onClear, children, showKeyword = true }) {
  return (
    <div className="admin-filter-bar">
      {showKeyword ? (
        <label className="admin-search">
          <Search aria-hidden="true" size={16} />
          <input placeholder="搜索关键词" value={keyword || ''} onChange={(event) => onKeywordChange?.(event.target.value)} />
        </label>
      ) : null}
      {statusOptions.length ? (
        <AdminSelect ariaLabel="状态筛选" className="admin-select--filter" value={status || ''} onChange={onStatusChange} options={[{ label: '全部状态', value: '' }, ...statusOptions]} />
      ) : null}
      {children}
      <Button type="button" variant="ghost" size="sm" icon={X} onClick={onClear}>
        清除
      </Button>
    </div>
  );
}
