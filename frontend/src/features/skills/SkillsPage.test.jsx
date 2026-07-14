import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import {
  SKILL_MARKET_IDS,
  skillMarketListItemFixture,
  skillMarketListResponseFixture
} from '../../test/skillMarketFixtures.js';
import { parseSkillMarketListResponse } from './skillMarketContract.js';
import { SkillsPage } from './SkillsPage.jsx';

describe('SkillsPage public market', () => {
  it('renders loading, real list pagination, cross-page de-duplication and detail navigation', async () => {
    let resolveFirst;
    const first = new Promise((resolve) => { resolveFirst = resolve; });
    const loadMarket = vi.fn()
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce(parseSkillMarketListResponse(skillMarketListResponseFixture({
        items: [
          skillMarketListItemFixture(),
          skillMarketListItemFixture({
            skill_id: SKILL_MARKET_IDS.secondSkill,
            name: '故事板助手',
            published_at: '2026-07-13T10:00:00Z'
          })
        ],
        next_cursor: null
      })));
    const onNavigate = vi.fn();

    render(<SkillsPage loadMarket={loadMarket} onNavigate={onNavigate} isLoggedIn />);
    expect(screen.getByRole('heading', { name: '正在加载 Skill 市场' })).toBeInTheDocument();

    await act(async () => {
      resolveFirst(parseSkillMarketListResponse(skillMarketListResponseFixture({ next_cursor: 'next_1' })));
      await first;
    });
    expect(await screen.findAllByTestId('skill-market-card')).toHaveLength(1);
    expect(screen.getByText(/搜索、收藏、费用、指标和跨发布者使用尚未开放/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /立即使用/ })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: '加载更多' }));
    await waitFor(() => expect(screen.getAllByTestId('skill-market-card')).toHaveLength(2));
    expect(loadMarket).toHaveBeenLastCalledWith({ cursor: 'next_1', signal: expect.any(AbortSignal) });
    expect(screen.getByText('已显示全部公开 Skill')).toBeInTheDocument();

    const firstCard = screen.getAllByTestId('skill-market-card')[0];
    await userEvent.click(within(firstCard).getByRole('button', { name: '查看 短片提示词助手 详情' }));
    expect(onNavigate).toHaveBeenCalledWith(`/skills/${SKILL_MARKET_IDS.skill}`);
  });

  it('recovers from an initial error into an honest empty state', async () => {
    const loadMarket = vi.fn()
      .mockRejectedValueOnce(new Error('市场服务暂时不可用'))
      .mockResolvedValueOnce(parseSkillMarketListResponse(skillMarketListResponseFixture({ items: [] })));

    render(<SkillsPage loadMarket={loadMarket} />);

    expect(await screen.findByRole('alert')).toHaveTextContent('市场服务暂时不可用');
    await userEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('heading', { name: '暂时没有公开 Skill' })).toBeInTheDocument();
  });

  it('aborts an in-flight list request on unmount', async () => {
    let signal;
    let resolveRequest;
    const loadMarket = vi.fn(({ signal: requestSignal }) => {
      signal = requestSignal;
      return new Promise((resolve) => { resolveRequest = resolve; });
    });
    const { unmount } = render(<SkillsPage loadMarket={loadMarket} />);
    await waitFor(() => expect(loadMarket).toHaveBeenCalledTimes(1));
    unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolveRequest(parseSkillMarketListResponse(skillMarketListResponseFixture({ items: [] })));
      await Promise.resolve();
    });
  });
});
