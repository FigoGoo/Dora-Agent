import { EmptyState } from './EmptyState.jsx';
import { Alert } from './Alert.jsx';
import { SkeletonBlock } from './Skeleton.jsx';

function resolveTableRowKey(row, rowKey, index) {
  if (typeof rowKey === 'function') {
    return rowKey(row) || row.id || row.key || index;
  }
  return row[rowKey] || row.id || row.key || index;
}

export function DataTable({ columns, rows = [], state = 'success', emptyText = '暂无数据', errorText = '加载失败', rowKey = 'id' }) {
  const colSpan = Math.max(columns.length, 1);

  return (
    <div className="admin-table-wrap">
      <table className="admin-table">
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.key} style={{ width: column.width }}>
                {column.title}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {state === 'loading' ? (
            <tr>
              <td colSpan={colSpan}>
                <SkeletonBlock rows={4} />
                <span className="admin-sr-only">正在加载数据</span>
              </td>
            </tr>
          ) : null}
          {state === 'error' ? (
            <tr>
              <td colSpan={colSpan}>
                <div className="admin-state">
                  <Alert tone="danger" title={errorText} />
                </div>
              </td>
            </tr>
          ) : null}
          {state === 'empty' ? (
            <tr>
              <td colSpan={colSpan}>
                <EmptyState title={emptyText} />
              </td>
            </tr>
          ) : null}
          {state === 'success'
            ? rows.map((row, index) => (
                <tr key={resolveTableRowKey(row, rowKey, index)}>
                  {columns.map((column) => (
                    <td key={column.key}>{column.render ? column.render(row) : row[column.key] || '-'}</td>
                  ))}
                </tr>
              ))
            : null}
        </tbody>
      </table>
    </div>
  );
}
