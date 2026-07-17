import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { PlanStoryboardPreviewForm } from './PlanStoryboardPreviewForm.jsx';

const IDS = Object.freeze({
  session: '019f0000-0000-7000-8000-000000000002',
  request: '019f0000-0000-7000-8000-000000000003',
  input: '019f0000-0000-7000-8000-000000000004',
  turn: '019f0000-0000-7000-8000-000000000005',
  run: '019f0000-0000-7000-8000-000000000006',
  toolCall: '019f0000-0000-7000-8000-000000000007',
  creationSpec: '019f0000-0000-7000-8000-000000000008',
  firstKey: '019f0000-0000-7000-8000-000000000021',
  secondKey: '019f0000-0000-7000-8000-000000000022',
  thirdKey: '019f0000-0000-7000-8000-000000000023'
});

describe('PlanStoryboardPreviewForm', () => {
  it('keeps trusted CreationSpec IDs out of user fields and submits only after explicit confirmation', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue(accepted());
    render(
      <PlanStoryboardPreviewForm
        sessionID={IDS.session}
        csrfToken="csrf-storyboard-preview"
        creationSpec={creationSpec()}
        initialPlanningInstruction="按开场、演示和收尾规划"
        enqueue={enqueue}
        keyFactory={() => IDS.firstKey}
      />
    );

    expect(screen.getByLabelText('故事板规划要求')).toHaveValue('按开场、演示和收尾规划');
    expect(screen.queryByDisplayValue(IDS.creationSpec)).not.toBeInTheDocument();
    expect(screen.queryByText(IDS.creationSpec)).not.toBeInTheDocument();
    expect(enqueue).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText('目标时长（秒，可选）'), '60');
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));

    expect(await screen.findByText(/请求已受理，正在等待 Storyboard JSON Draft/)).toBeInTheDocument();
    expect(screen.queryByText(/生成完成|已激活|已扣费/)).not.toBeInTheDocument();
    expect(enqueue).toHaveBeenCalledWith({
      sessionID: IDS.session,
      creationSpecRef: {
        id: IDS.creationSpec,
        version: 1,
        contentDigest: 'a'.repeat(64)
      },
      toolIntent: {
        planningInstruction: '按开场、演示和收尾规划',
        targetDurationSeconds: 60
      },
      idempotencyKey: IDS.firstKey,
      csrfToken: 'csrf-storyboard-preview',
      signal: expect.any(AbortSignal)
    });
  });

  it('reuses a key for duplicate submit and replaces it after semantic or CreationSpec snapshot changes', async () => {
    const user = userEvent.setup();
    const keyFactory = vi.fn()
      .mockReturnValueOnce(IDS.firstKey)
      .mockReturnValueOnce(IDS.secondKey)
      .mockReturnValueOnce(IDS.thirdKey);
    const enqueue = vi.fn()
      .mockResolvedValueOnce(accepted())
      .mockResolvedValueOnce(accepted({ replayed: true }))
      .mockResolvedValueOnce(accepted())
      .mockResolvedValueOnce(accepted());
    const props = {
      sessionID: IDS.session,
      csrfToken: 'csrf-storyboard-preview',
      enqueue,
      keyFactory
    };
    const { rerender } = render(
      <PlanStoryboardPreviewForm {...props} creationSpec={creationSpec()} />
    );
    const instruction = screen.getByLabelText('故事板规划要求');

    await user.type(instruction, '规划故事板');
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await screen.findByText(/请求已受理/);
    await user.click(screen.getByRole('button', { name: '再次确认受理状态' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(2));
    expect(enqueue.mock.calls[1][0].idempotencyKey).toBe(IDS.firstKey);

    await user.type(instruction, '，突出节奏');
    expect(screen.getByText(/规划语义已修改/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(3));
    expect(enqueue.mock.calls[2][0].idempotencyKey).toBe(IDS.secondKey);

    rerender(
      <PlanStoryboardPreviewForm
        {...props}
        creationSpec={creationSpec({ contentDigest: 'b'.repeat(64) })}
      />
    );
    await waitFor(() => expect(screen.getByRole('button', { name: '生成故事板开发预览' })).toBeEnabled());
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(4));
    expect(enqueue.mock.calls[3][0]).toMatchObject({
      idempotencyKey: IDS.thirdKey,
      creationSpecRef: { contentDigest: 'b'.repeat(64) }
    });
  });

  it('fails closed when the same idempotency key is replayed with different stable identities', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn()
      .mockResolvedValueOnce(accepted())
      .mockResolvedValueOnce(accepted({
        inputID: '019f0000-0000-7000-8000-000000000099',
        replayed: true
      }));
    render(
      <PlanStoryboardPreviewForm
        sessionID={IDS.session}
        csrfToken="csrf-storyboard-preview"
        creationSpec={creationSpec()}
        enqueue={enqueue}
        keyFactory={() => IDS.firstKey}
      />
    );
    await user.type(screen.getByLabelText('故事板规划要求'), '规划故事板');
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await screen.findByText(/请求已受理/);
    await user.click(screen.getByRole('button', { name: '再次确认受理状态' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('同键重放身份不一致');
    expect(enqueue).toHaveBeenCalledTimes(2);
    expect(enqueue.mock.calls[1][0].idempotencyKey).toBe(enqueue.mock.calls[0][0].idempotencyKey);
  });

  it('aborts an in-flight request when the trusted CreationSpec snapshot changes', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn(() => new Promise(() => {}));
    const props = {
      sessionID: IDS.session,
      csrfToken: 'csrf-storyboard-preview',
      enqueue,
      keyFactory: () => IDS.firstKey
    };
    const { rerender } = render(
      <PlanStoryboardPreviewForm {...props} creationSpec={creationSpec()} />
    );
    await user.type(screen.getByLabelText('故事板规划要求'), '规划故事板');
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await waitFor(() => expect(enqueue).toHaveBeenCalledTimes(1));
    const signal = enqueue.mock.calls[0][0].signal;
    expect(signal.aborted).toBe(false);

    rerender(
      <PlanStoryboardPreviewForm
        {...props}
        creationSpec={creationSpec({ contentDigest: 'b'.repeat(64) })}
      />
    );
    await waitFor(() => expect(signal.aborted).toBe(true));
  });

  it('attributes a persistent failure only to the accepted typed Input and disables without a Draft', async () => {
    const user = userEvent.setup();
    const enqueue = vi.fn().mockResolvedValue(accepted());
    const props = {
      sessionID: IDS.session,
      csrfToken: 'csrf-storyboard-preview',
      creationSpec: creationSpec(),
      enqueue,
      keyFactory: () => IDS.firstKey
    };
    const { rerender } = render(<PlanStoryboardPreviewForm {...props} />);
    await user.type(screen.getByLabelText('故事板规划要求'), '规划故事板');
    await user.click(screen.getByRole('button', { name: '生成故事板开发预览' }));
    await screen.findByText(/请求已受理/);

    rerender(<PlanStoryboardPreviewForm {...props} failure={{
      inputID: '019f0000-0000-7000-8000-000000000099',
      resultCode: 'UNRELATED_FAILURE',
      summary: '不相关失败'
    }} />);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    rerender(<PlanStoryboardPreviewForm {...props} failure={{
      inputID: IDS.input,
      resultCode: 'PLAN_STORYBOARD_RUNTIME_FAILED',
      summary: '故事板规划运行时未完成。'
    }} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('故事板规划运行时未完成');
    expect(screen.getByRole('alert')).toHaveTextContent('PLAN_STORYBOARD_RUNTIME_FAILED');

    rerender(<PlanStoryboardPreviewForm {...props} creationSpec={null} failure={null} />);
    expect(screen.getByRole('button', { name: '生成故事板开发预览' })).toBeDisabled();
    expect(screen.getByText('需要先生成可用的 Creation Spec Draft。')).toBeInTheDocument();
  });
});

function creationSpec(overrides = {}) {
  return {
    kind: 'card',
    status: 'draft',
    creationSpecID: IDS.creationSpec,
    version: 1,
    contentDigest: 'a'.repeat(64),
    ...overrides
  };
}

function accepted(overrides = {}) {
  return {
    requestID: IDS.request,
    inputID: IDS.input,
    turnID: IDS.turn,
    runID: IDS.run,
    toolCallID: IDS.toolCall,
    status: 'pending',
    replayed: false,
    ...overrides
  };
}
