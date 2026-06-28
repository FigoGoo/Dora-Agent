import { render, screen } from '@testing-library/react';
import { describe, expect, test } from 'vitest';
import { DataTable } from './DataTable.jsx';

describe('DataTable', () => {
  const columns = [
    { key: 'name', title: '名称' },
    { key: 'status', title: '状态', render: (row) => row.status }
  ];

  test('renders loading, empty, error and data states without changing table structure', () => {
    const { rerender } = render(<DataTable columns={columns} rows={[]} state="loading" />);
    expect(screen.getByText('正在加载数据')).toBeInTheDocument();

    rerender(<DataTable columns={columns} rows={[]} state="empty" emptyText="暂无管理员" />);
    expect(screen.getByText('暂无管理员')).toBeInTheDocument();

    rerender(<DataTable columns={columns} rows={[]} state="error" errorText="加载失败" />);
    expect(screen.getByText('加载失败')).toBeInTheDocument();

    rerender(<DataTable columns={columns} rows={[{ id: '1', name: 'Root', status: 'active' }]} />);
    expect(screen.getByRole('columnheader', { name: '名称' })).toBeInTheDocument();
    expect(screen.getByText('Root')).toBeInTheDocument();
  });

  test('accepts a row key resolver for resources without a single id field', () => {
    render(
      <DataTable
        columns={columns}
        rows={[{ tool_name: 'draw', tool_type: 'builtin', name: 'Draw', status: 'active' }]}
        rowKey={(row) => `${row.tool_name}:${row.tool_type}`}
      />
    );

    expect(screen.getByText('Draw')).toBeInTheDocument();
  });
});
