import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { App } from './App.jsx';

describe('DORAIGC landing page', () => {
  it('renders the approved brand and prompt-first creation entry', () => {
    render(<App />);

    expect(screen.getByRole('img', { name: 'DORAIGC 标志' })).toBeInTheDocument();
    expect(screen.getByText('DORAIGC')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '创作，从一个想法开始' })).toBeInTheDocument();
    expect(screen.getByPlaceholderText('描述你想创作的内容...')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '开始创作' })).toBeInTheDocument();
  });

  it('keeps unauthenticated creation intent in the login modal', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.type(screen.getByPlaceholderText('描述你想创作的内容...'), '做一个霓虹城市里的音乐短片');
    await user.click(screen.getByRole('button', { name: '开始创作' }));

    expect(screen.getByRole('dialog', { name: '登录后继续创作' })).toBeInTheDocument();
    expect(screen.getByText('做一个霓虹城市里的音乐短片')).toBeInTheDocument();
  });

  it('exposes logo-derived theme tokens on the app shell', () => {
    render(<App />);

    const shell = screen.getByTestId('doraigc-shell');
    expect(shell).toHaveStyle({
      '--dora-lime': '#cfff24',
      '--dora-mint': '#35e0ba',
      '--dora-cyan': '#4bd8ff'
    });
  });
});
