import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { analyzeMaterialsPreviewCardFixture, directResponseCardFixture, turnFailureCardFixture } from '../../test/workspaceFixtures.js';
import { parseAnalyzeMaterialsPreviewCard } from '../aigc/analyzeMaterialsPreviewContract.js';
import { parseDirectResponseCard, parseTurnFailureCard } from './turnOutputContract.js';
import { TurnOutputCard } from './TurnOutputCard.jsx';

describe('TurnOutputCard', () => {
  it('renders Direct Response as a Card rather than an assistant message', () => {
    const onOpenToolbox = vi.fn();
    render(<TurnOutputCard output={parseDirectResponseCard(directResponseCardFixture())} onOpenToolbox={onOpenToolbox} />);
    expect(screen.getByRole('heading', { name: '需求已接收' })).toBeInTheDocument();
    expect(screen.getByText('已收到你的创作需求。你可以继续打开工具箱选择下一步流程。')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '打开工具箱' }));
    expect(onOpenToolbox).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['failed', '处理未完成'],
    ['recovery_pending', '正在恢复处理']
  ])('renders the safe %s Failure Card without toolbox action', (status, heading) => {
    render(<TurnOutputCard output={parseTurnFailureCard(turnFailureCardFixture({ status }))} />);
    expect(screen.getByRole('heading', { name: heading })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '打开工具箱' })).not.toBeInTheDocument();
  });

  it('delegates Analyze Materials output to the read-only Card', () => {
    render(<TurnOutputCard output={parseAnalyzeMaterialsPreviewCard(analyzeMaterialsPreviewCardFixture())} />);
    expect(screen.getByRole('heading', { name: '素材分析已完成' })).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });
});
