import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { WORKSPACE_IDS } from '../../test/workspaceFixtures.js';
import { parseCreationSpecPreviewFailure } from './creationSpecPreviewContract.js';
import { CreationSpecPreviewForm } from './CreationSpecPreviewForm.jsx';

describe('CreationSpecPreviewForm', () => {
  it('prefills a handed-off goal but still waits for an explicit Preview submit', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.previewRequest,
      inputID: WORKSPACE_IDS.previewInput,
      status: 'pending'
    });
    render(
      <CreationSpecPreviewForm
        sessionID={WORKSPACE_IDS.session}
        csrfToken="csrf-preview"
        initialGoal="QuickCreate 交接的创作目标"
        enqueue={enqueue}
        keyFactory={() => WORKSPACE_IDS.previewRequest}
      />
    );

    expect(screen.getByLabelText('创作目标')).toHaveValue('QuickCreate 交接的创作目标');
    expect(enqueue).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: '生成开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(1));
    expect(enqueue.mock.calls[0][0].intent.goal).toBe('QuickCreate 交接的创作目标');
  });

  it('applies a late QuickCreate handoff only while the goal is still empty', async () => {
    const props = {
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-preview',
      enqueue: vi.fn(),
      keyFactory: () => WORKSPACE_IDS.previewRequest
    };
    const { rerender } = render(<CreationSpecPreviewForm {...props} initialGoal="" />);
    const goal = screen.getByLabelText('创作目标');

    rerender(<CreationSpecPreviewForm {...props} initialGoal="迟到的 QuickCreate 目标" />);
    expect(goal).toHaveValue('迟到的 QuickCreate 目标');

    await userEvent.setup().type(goal, '（用户补充）');
    rerender(<CreationSpecPreviewForm {...props} initialGoal="不得覆盖用户输入" />);
    expect(goal).toHaveValue('迟到的 QuickCreate 目标（用户补充）');
  });

  it('shows accepted/pending without guessing success and reuses the same key on a duplicate click', async () => {
    const user = userEvent.setup();
    const keyFactory = vi.fn().mockReturnValue(WORKSPACE_IDS.previewRequest);
    const enqueue = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.previewRequest,
      inputID: WORKSPACE_IDS.previewInput,
      status: 'pending'
    });
    render(
      <CreationSpecPreviewForm
        sessionID={WORKSPACE_IDS.session}
        csrfToken="csrf-preview"
        cursor={7}
        enqueue={enqueue}
        keyFactory={keyFactory}
      />
    );

    await user.type(screen.getByLabelText('创作目标'), '制作新品短片');
    await user.type(screen.getByLabelText('目标受众（可选）'), '年轻消费者');
    await user.type(screen.getByLabelText('约束（每行一项，最多 8 项）'), '时长 30 秒\n使用中文');
    await user.click(screen.getByRole('button', { name: '生成开发预览' }));

    expect(await screen.findByText(/请求已受理，正在等待 Creation Spec Draft/)).toBeInTheDocument();
    expect(screen.queryByText(/权威工作台投影更新/)).not.toBeInTheDocument();
    expect(enqueue).toHaveBeenCalledWith(expect.objectContaining({
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-preview',
      idempotencyKey: WORKSPACE_IDS.previewRequest,
      intent: {
        goal: '制作新品短片',
        deliverableType: 'video',
        audience: '年轻消费者',
        locale: 'zh-CN',
        constraints: ['时长 30 秒', '使用中文']
      },
      signal: expect.any(AbortSignal)
    }));

    await user.click(screen.getByRole('button', { name: '再次确认受理状态' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(2));
    expect(enqueue.mock.calls[1][0].idempotencyKey).toBe(enqueue.mock.calls[0][0].idempotencyKey);
    expect(keyFactory).toHaveBeenCalledTimes(1);
  });

  it('generates a new key after semantic edits and does not attribute another Card update to the pending input', async () => {
    const user = userEvent.setup();
    const keyFactory = vi.fn()
      .mockReturnValueOnce(WORKSPACE_IDS.previewRequest)
      .mockReturnValueOnce('019f0000-0000-7000-8000-000000000099');
    const enqueue = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.previewRequest,
      inputID: WORKSPACE_IDS.previewInput,
      status: 'pending'
    });
    const props = {
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-preview',
      cursor: 7,
      enqueue,
      keyFactory
    };
    const { rerender } = render(<CreationSpecPreviewForm {...props} />);
    const goal = screen.getByLabelText('创作目标');
    await user.type(goal, '目标 A');
    await user.click(screen.getByRole('button', { name: '生成开发预览' }));
    await screen.findByText(/^请求已受理，正在等待 Creation Spec Draft/);
    await user.clear(goal);
    await user.type(goal, '目标 B');
    expect(screen.getByText(/表单语义已修改/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '生成开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(2));
    expect(enqueue.mock.calls[1][0].idempotencyKey).not.toBe(enqueue.mock.calls[0][0].idempotencyKey);

    rerender(
      <CreationSpecPreviewForm
        {...props}
        preview={{ kind: 'card', creationSpecID: 'unrelated', version: 2, contentDigest: 'b'.repeat(64) }}
        cursor={8}
        latestPreviewEvent={{ event: 'creation_spec.preview.completed', seq: 8 }}
      />
    );
    expect(await screen.findByText(/^请求已受理，正在等待 Creation Spec Draft/)).toBeInTheDocument();
    expect(screen.queryByText(/权威工作台投影更新/)).not.toBeInTheDocument();
  });

  it('shows a strict persistent failure only for the accepted input', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue({
      requestID: WORKSPACE_IDS.previewRequest,
      inputID: WORKSPACE_IDS.previewInput,
      status: 'pending'
    });
    const props = {
      sessionID: WORKSPACE_IDS.session,
      csrfToken: 'csrf-preview',
      enqueue,
      keyFactory: () => WORKSPACE_IDS.previewRequest
    };
    const { rerender } = render(<CreationSpecPreviewForm {...props} />);
    await user.type(screen.getByLabelText('创作目标'), '目标 A');
    await user.click(screen.getByRole('button', { name: '生成开发预览' }));
    await screen.findByText(/^请求已受理，正在等待 Creation Spec Draft/);
    const failure = parseCreationSpecPreviewFailure({
      input_id: WORKSPACE_IDS.previewInput,
      result_code: 'CREATION_SPEC_PREVIEW_INVALID',
      summary: '目标信息不足。',
      retryable: false
    });
    rerender(<CreationSpecPreviewForm {...props} failure={failure} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('预览生成失败：目标信息不足。');
    expect(screen.getByRole('alert')).toHaveTextContent('CREATION_SPEC_PREVIEW_INVALID');
  });
});
