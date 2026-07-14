import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { SKILL_MARKET_IDS, skillMarketDetailResponseFixture } from '../../test/skillMarketFixtures.js';
import { parseSkillMarketDetailResponse } from './skillMarketContract.js';
import { SkillMarketDetailPage } from './SkillMarketDetailPage.jsx';

describe('SkillMarketDetailPage', () => {
  it('renders the strict public projection without an executable use action', async () => {
    const loadSkill = vi.fn().mockResolvedValue(parseSkillMarketDetailResponse(skillMarketDetailResponseFixture()));
    render(<SkillMarketDetailPage skillID={SKILL_MARKET_IDS.skill} loadSkill={loadSkill} />);

    expect(await screen.findByRole('heading', { name: '短片提示词助手' })).toBeInTheDocument();
    expect(loadSkill).toHaveBeenCalledWith(SKILL_MARKET_IDS.skill, { signal: expect.any(AbortSignal) });
    expect(screen.getByText('公开市场详情。')).toBeInTheDocument();
    expect(screen.getByText('提示词写法')).toBeInTheDocument();
    expect(screen.getByText(/不表示当前可执行或已开放使用/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /立即使用/ })).not.toBeInTheDocument();
  });

  it('distinguishes a public 404 and returns to the market', async () => {
    const notFound = Object.assign(new Error('Skill 暂不可用'), {
      status: 404,
      requestID: SKILL_MARKET_IDS.request
    });
    const onNavigate = vi.fn();
    render(<SkillMarketDetailPage
      skillID={SKILL_MARKET_IDS.skill}
      loadSkill={vi.fn().mockRejectedValue(notFound)}
      onNavigate={onNavigate}
    />);

    expect(await screen.findByRole('heading', { name: 'Skill 暂不可用' })).toBeInTheDocument();
    expect(screen.getByText(`请求标识：${SKILL_MARKET_IDS.request}`)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '返回 Skill 市场' }));
    expect(onNavigate).toHaveBeenCalledWith('/skills');
  });

  it('retries recoverable failures and aborts work on unmount', async () => {
    const result = parseSkillMarketDetailResponse(skillMarketDetailResponseFixture());
    const loadSkill = vi.fn()
      .mockRejectedValueOnce(new Error('暂时不可用'))
      .mockResolvedValueOnce(result);
    const view = render(<SkillMarketDetailPage skillID={SKILL_MARKET_IDS.skill} loadSkill={loadSkill} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('暂时不可用');
    await userEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('heading', { name: '短片提示词助手' })).toBeInTheDocument();
    view.unmount();

    let signal;
    let resolveRequest;
    const pendingLoad = vi.fn((_, { signal: requestSignal }) => {
      signal = requestSignal;
      return new Promise((resolve) => { resolveRequest = resolve; });
    });
    const pending = render(<SkillMarketDetailPage skillID={SKILL_MARKET_IDS.skill} loadSkill={pendingLoad} />);
    await waitFor(() => expect(pendingLoad).toHaveBeenCalledTimes(1));
    pending.unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolveRequest(result);
      await Promise.resolve();
    });
  });
});
