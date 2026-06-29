import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, test, vi } from 'vitest';
import { SkillMarkdownEditor } from './SkillMarkdownEditor.jsx';

const markdown = `## 工具引用 <工具引用>

<tool id="web_fetch:browser">Web Fetch</tool>

## AG-UI 元素引用 <AG-UI元素引用>

<agui id="storyboard_panel">故事板面板</agui>`;

describe('SkillMarkdownEditor', () => {
  test('renders readable token names in text mode and raw tags in source mode', async () => {
    render(<SkillMarkdownEditor label="Skill 内容" value={markdown} onChange={vi.fn()} />);

    await userEvent.click(screen.getByRole('button', { name: '文本' }));

    expect(screen.getByText('Web Fetch')).toBeInTheDocument();
    expect(screen.getByText('故事板面板')).toBeInTheDocument();
    expect(screen.queryByText(/<tool/)).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '源码' }));

    expect(screen.getByLabelText('Skill 内容')).toHaveValue(markdown);
    expect(screen.getAllByText(/<tool id="web_fetch:browser">Web Fetch<\/tool>/).length).toBeGreaterThan(0);
  });
});
