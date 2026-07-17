import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PromptPreviewCard } from './PromptPreviewCard.jsx';
import { parsePromptPreviewCard } from './writePromptsPreviewContract.js';
import { promptPreviewCardFixture, promptPreviewFailureCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';

describe('PromptPreviewCard', () => {
  it('renders validated Prompt text as inert content without exposing trusted identities', () => {
    const hostile = '<img src=x onerror=alert(1)>';
    const payload = promptPreviewCardFixture();
    payload.prompts[0] = { ...payload.prompts[0], positive_prompt: hostile };
    const { container } = render(<PromptPreviewCard preview={parsePromptPreviewCard(payload)} />);
    const card = screen.getByRole('article');
    expect(within(card).getByText('开发预览 · 隔离 Prompt Draft · 未审核/未扣费/不可生成媒体')).toBeInTheDocument();
    expect(within(card).getByRole('heading', { name: '媒体提示词预览' })).toBeInTheDocument();
    expect(within(card).getByText(hostile)).toBeInTheDocument();
    expect(container.querySelector('img')).toBeNull();
    expect(card).toHaveAttribute('data-prompt-preview-id', WORKSPACE_IDS.promptPreview);
    expect(container.innerHTML).not.toContain(WORKSPACE_IDS.project);
    expect(container.innerHTML).not.toContain(WORKSPACE_IDS.storyboardPreview);
    expect(container.innerHTML).not.toContain(WORKSPACE_IDS.promptInput);
    expect(within(card).getByText('无')).toBeInTheDocument();
  });

  it('renders a safe failure without completed resource fields', () => {
    const preview = parsePromptPreviewCard(promptPreviewFailureCardFixture({
      failure_kind: 'runtime',
      result_code: 'WRITE_PROMPTS_RUNTIME_FAILED',
      summary: '提示词运行时未完成。',
      retryable: true
    }));
    const { container } = render(<PromptPreviewCard preview={preview} />);
    const card = screen.getByRole('article');
    expect(within(card).getByRole('heading', { name: '提示词生成未完成' })).toBeInTheDocument();
    expect(within(card).getByText('失败类型：运行时失败')).toBeInTheDocument();
    expect(card).not.toHaveAttribute('data-prompt-preview-id');
    expect(container.innerHTML).not.toContain(WORKSPACE_IDS.project);
  });
});
