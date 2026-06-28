import { ChevronLeft, ChevronRight } from 'lucide-react';
import { AdminSelect } from './AdminSelect.jsx';
import { Button } from './Button.jsx';

export function Pagination({ pageSize, total, pageToken, onPageSizeChange, onNext, onPrevious, previousDisabled, nextDisabled }) {
  return (
    <div className="admin-pagination">
      <span>共 {total || 0} 条</span>
      <span className="admin-pagination__size">
        每页
        <AdminSelect
          ariaLabel="每页"
          className="admin-select--pagination"
          value={pageSize}
          onChange={onPageSizeChange}
          options={[10, 20, 50].map((size) => ({ label: String(size), value: size }))}
        />
      </span>
      <Button type="button" variant="ghost" size="sm" icon={ChevronLeft} disabled={previousDisabled} onClick={onPrevious}>
        上一页
      </Button>
      <Button type="button" variant="ghost" size="sm" icon={ChevronRight} disabled={nextDisabled && !pageToken} onClick={onNext}>
        下一页
      </Button>
    </div>
  );
}
