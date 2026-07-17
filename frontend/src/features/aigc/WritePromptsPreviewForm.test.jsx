import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { WritePromptsPreviewForm } from './WritePromptsPreviewForm.jsx';
import { parseStoryboardPreviewCard } from './planStoryboardPreviewContract.js';
import { storyboardPreviewCardFixture, WORKSPACE_IDS } from '../../test/workspaceFixtures.js';

describe('WritePromptsPreviewForm', () => {
  it('auto-binds the current Storyboard ref, exposes no trusted ID field, and submits only writing semantics', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue(accepted());
    render(
      <WritePromptsPreviewForm
        sessionID={WORKSPACE_IDS.session}
        csrfToken="csrf-write-prompts"
        storyboardPreview={storyboard()}
        enqueue={enqueue}
        keyFactory={() => WORKSPACE_IDS.request}
      />
    );
    expect(screen.getByText(/自动绑定当前 Storyboard Draft v1 的全部 2 个槽位/)).toBeInTheDocument();
    expect(screen.queryByDisplayValue(WORKSPACE_IDS.storyboardPreview)).not.toBeInTheDocument();
    await user.type(screen.getByLabelText('提示词写作要求'), '为每个媒体槽位编写清晰提示词');
    await user.selectOptions(screen.getByLabelText('输出语言（可选）'), 'en-US');
    await user.click(screen.getByRole('button', { name: '生成提示词开发预览' }));

    expect(await screen.findByText(/请求已受理，正在等待 Prompt JSON Draft/)).toBeInTheDocument();
    expect(enqueue).toHaveBeenCalledWith({
      sessionID: WORKSPACE_IDS.session,
      storyboardPreviewRef: {
        id: WORKSPACE_IDS.storyboardPreview,
        version: 1,
        contentDigest: 'b'.repeat(64)
      },
      toolIntent: {
        writingInstruction: '为每个媒体槽位编写清晰提示词',
        outputLanguage: 'en-US'
      },
      idempotencyKey: WORKSPACE_IDS.request,
      csrfToken: 'csrf-write-prompts',
      signal: expect.any(AbortSignal)
    });
  });

  it('reuses the same key for semantic replay and changes it after the Source digest changes', async () => {
    const user = userEvent.setup();
    const keyFactory = vi.fn().mockReturnValueOnce(WORKSPACE_IDS.request).mockReturnValueOnce(WORKSPACE_IDS.promptEvent);
    const enqueue = vi.fn()
      .mockResolvedValueOnce(accepted())
      .mockResolvedValueOnce(accepted({ replayed: true }))
      .mockResolvedValueOnce(accepted());
    const props = {
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-write-prompts',
      enqueue,
      keyFactory
    };
    const { rerender } = render(<WritePromptsPreviewForm {...props} storyboardPreview={storyboard()} />);
    await user.type(screen.getByLabelText('提示词写作要求'), '编写提示词');
    await user.click(screen.getByRole('button', { name: '生成提示词开发预览' }));
    await screen.findByText(/请求已受理/);
    await user.click(screen.getByRole('button', { name: '再次确认受理状态' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(2));
    expect(enqueue.mock.calls[1][0].idempotencyKey).toBe(WORKSPACE_IDS.request);

    rerender(<WritePromptsPreviewForm {...props} storyboardPreview={storyboard({ content_digest: 'c'.repeat(64) })} />);
    await user.click(screen.getByRole('button', { name: '生成提示词开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(3));
    expect(enqueue.mock.calls[2][0]).toMatchObject({
      idempotencyKey: WORKSPACE_IDS.promptEvent,
      storyboardPreviewRef: { contentDigest: 'c'.repeat(64) }
    });
  });

  it('disables without at least one current Storyboard Slot and attributes failure only to its accepted Input', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue(accepted());
    const props = {
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-write-prompts',
      storyboardPreview: storyboard(),
      enqueue,
      keyFactory: () => WORKSPACE_IDS.request
    };
    const { rerender } = render(<WritePromptsPreviewForm {...props} />);
    await user.type(screen.getByLabelText('提示词写作要求'), '编写提示词');
    await user.click(screen.getByRole('button', { name: '生成提示词开发预览' }));
    await screen.findByText(/请求已受理/);

    rerender(<WritePromptsPreviewForm {...props} failure={{
      inputID: WORKSPACE_IDS.storyboardInput,
      resultCode: 'UNRELATED',
      summary: '不相关失败'
    }} />);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    rerender(<WritePromptsPreviewForm {...props} failure={{
      inputID: WORKSPACE_IDS.promptInput,
      resultCode: 'PROMPT_PREVIEW_CANDIDATE_INVALID',
      summary: '候选未通过校验。'
    }} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('候选未通过校验');

    rerender(<WritePromptsPreviewForm {...props} storyboardPreview={storyboard({ slots: [] })} failure={null} />);
    expect(screen.getByRole('button', { name: '生成提示词开发预览' })).toBeDisabled();
    expect(screen.getByText('需要当前 Storyboard Draft 至少包含一个媒体槽位。')).toBeInTheDocument();
  });
});

function storyboard(overrides = {}) {
  return parseStoryboardPreviewCard(storyboardPreviewCardFixture(overrides));
}

function accepted(overrides = {}) {
  return {
    requestID: WORKSPACE_IDS.request,
    inputID: WORKSPACE_IDS.promptInput,
    turnID: WORKSPACE_IDS.promptTurn,
    runID: WORKSPACE_IDS.promptRun,
    toolCallID: WORKSPACE_IDS.promptToolCall,
    status: 'pending',
    replayed: false,
    ...overrides
  };
}
