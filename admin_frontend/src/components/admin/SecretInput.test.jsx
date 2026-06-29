import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';
import { SecretInput } from './SecretInput.jsx';

describe('SecretInput', () => {
  test('toggles secret visibility without submitting the form', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn((event) => event.preventDefault());

    render(
      <form onSubmit={onSubmit}>
        <SecretInput label="密钥引用" value="secret-ref" onChange={() => {}} />
      </form>
    );

    const input = screen.getByLabelText('密钥引用');
    expect(input).toHaveAttribute('type', 'text');
    expect(input).toHaveAttribute('autocomplete', 'new-password');
    expect(input).toHaveAttribute('data-lpignore', 'true');
    expect(input).toHaveClass('is-masked');

    await user.click(screen.getByRole('button', { name: '显示输入' }));

    expect(input).toHaveAttribute('type', 'text');
    expect(input).not.toHaveClass('is-masked');
    expect(onSubmit).not.toHaveBeenCalled();

    await user.click(screen.getByRole('button', { name: '隐藏密钥' }));

    expect(input).toHaveAttribute('type', 'text');
    expect(input).toHaveClass('is-masked');
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
