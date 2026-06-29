import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';
import { ConfirmDialog } from './ConfirmDialog.jsx';

describe('ConfirmDialog', () => {
  test('requires a reason before confirming a dangerous action', async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();

    render(
      <ConfirmDialog
        open
        tone="danger"
        title="禁用用户"
        objectLabel="用户 usr_1"
        impactItems={['该用户将无法登录', '操作会写入审计日志']}
        requireReason
        previewToken="preview_1"
        onConfirm={onConfirm}
        onClose={() => {}}
      />
    );

    await user.click(screen.getByRole('button', { name: '确认执行' }));
    expect(await screen.findByText('请输入操作原因。')).toBeInTheDocument();
    expect(onConfirm).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText('操作原因'), '违反平台规则');
    await user.click(screen.getByRole('button', { name: '确认执行' }));

    expect(onConfirm).toHaveBeenCalledWith({ reason: '违反平台规则', previewToken: 'preview_1' });
  });
});
