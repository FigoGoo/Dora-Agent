import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';
import { AdminSelect } from './AdminSelect.jsx';

describe('AdminSelect', () => {
  test('opens a custom listbox and selects an option without rendering a native select', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();

    render(
      <AdminSelect
        ariaLabel="状态筛选"
        value=""
        onChange={onChange}
        options={[
          { label: '全部状态', value: '' },
          { label: '启用', value: 'active' },
          { label: '停用', value: 'disabled' }
        ]}
      />
    );

    expect(document.querySelector('select')).toBeNull();

    await user.click(screen.getByRole('button', { name: '状态筛选：全部状态' }));

    expect(screen.getByRole('listbox', { name: '状态筛选' })).toBeInTheDocument();

    await user.click(screen.getByRole('option', { name: '启用' }));

    expect(onChange).toHaveBeenCalledWith('active');
  });
});
