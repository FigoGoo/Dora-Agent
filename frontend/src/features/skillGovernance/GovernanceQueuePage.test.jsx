import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { AUTH_SESSION_STATUS, AuthSessionProvider } from '../../platform/auth/authSession.js';
import { SKILL_GOVERNANCE_IDS } from '../../test/skillGovernanceFixtures.js';
import { GovernanceQueuePage } from './GovernanceQueuePage.jsx';

describe('GovernanceQueuePage', () => {
  it('loads the active queue and navigates to the exact Skill governance detail', async () => {
    const user = userEvent.setup();
    const onNavigate = vi.fn();
    const loadSkills = vi.fn().mockResolvedValue({
      items: [queueItem()],
      nextCursor: null,
      requestID: SKILL_GOVERNANCE_IDS.request
    });
    renderQueue({ loadSkills, onNavigate });

    expect(screen.getByRole('status')).toHaveTextContent('正在加载');
    expect(await screen.findByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '正常' })).toHaveAttribute('aria-pressed', 'true');
    await user.click(screen.getByRole('button', { name: '查看治理详情' }));

    expect(onNavigate).toHaveBeenCalledWith(`/admin/skills/governance/${SKILL_GOVERNANCE_IDS.skill}`);
    expect(loadSkills).toHaveBeenCalledWith({
      status: 'active',
      cursor: null,
      signal: expect.any(AbortSignal)
    });
  });

  it('deduplicates skill_id across pages and drops the cursor on filter changes and refresh', async () => {
    const user = userEvent.setup();
    const loadSkills = vi.fn()
      .mockResolvedValueOnce({ items: [queueItem()], nextCursor: 'cursor_1' })
      .mockResolvedValueOnce({
        items: [
          queueItem(),
          queueItem({ skillID: SKILL_GOVERNANCE_IDS.secondSkill, name: '第二个 Skill' })
        ],
        nextCursor: 'cursor_2'
      })
      .mockResolvedValueOnce({ items: [], nextCursor: null })
      .mockResolvedValueOnce({ items: [], nextCursor: null });
    renderQueue({ loadSkills });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('heading', { name: '第二个 Skill' })).toBeInTheDocument();
    expect(screen.getAllByRole('heading', { name: '剧情短片 Skill' })).toHaveLength(1);
    expect(loadSkills.mock.calls[1][0]).toMatchObject({ status: 'active', cursor: 'cursor_1' });

    await user.click(screen.getByRole('button', { name: '已暂停' }));
    expect(await screen.findByRole('heading', { name: '当前没有已暂停的 Skill' })).toBeInTheDocument();
    expect(loadSkills.mock.calls[2][0]).toMatchObject({ status: 'suspended', cursor: null });
    expect(screen.queryByRole('heading', { name: '剧情短片 Skill' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '刷新队列' }));
    await waitFor(() => expect(loadSkills).toHaveBeenCalledTimes(4));
    expect(loadSkills.mock.calls[3][0]).toMatchObject({ status: 'suspended', cursor: null });
  });

  it('shows transient error/retry and the empty state', async () => {
    const user = userEvent.setup();
    const loadSkills = vi.fn()
      .mockRejectedValueOnce(Object.assign(new Error('治理队列暂不可用'), {
        status: 503,
        requestID: 'request-governance-1'
      }))
      .mockResolvedValueOnce({ items: [], nextCursor: null });
    renderQueue({ loadSkills });

    expect(await screen.findByRole('alert')).toHaveTextContent('治理队列暂不可用');
    expect(screen.getByText('请求 ID：request-governance-1')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('heading', { name: '当前没有正常的 Skill' })).toBeInTheDocument();
    expect(loadSkills).toHaveBeenCalledTimes(2);
  });

  it('fails closed and re-parses authority once after the stable governance 403', async () => {
    const forbidden = Object.assign(new Error('治理权限已撤销'), {
      status: 403,
      code: 'SKILL_GOVERNANCE_CAPABILITY_REQUIRED'
    });
    const loadSkills = vi.fn().mockRejectedValue(forbidden);
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()),
      login: vi.fn(),
      logout: vi.fn()
    };
    renderQueue({ loadSkills, authClient });

    expect(await screen.findByRole('alert')).toHaveTextContent('失去 Skill 治理权限');
    await waitFor(() => expect(authClient.bootstrap).toHaveBeenCalledTimes(1));
    expect(authClient.bootstrap).toHaveBeenCalledWith();
    expect(loadSkills).toHaveBeenCalledTimes(1);
  });
});

function renderQueue({ loadSkills, onNavigate = vi.fn(), authClient = defaultAuthClient() }) {
  return render(
    <AuthSessionProvider autoBootstrap={false} client={authClient} initialSession={governorSession()}>
      <GovernanceQueuePage loadSkills={loadSkills} onNavigate={onNavigate} />
    </AuthSessionProvider>
  );
}

function queueItem(overrides = {}) {
  return {
    skillID: SKILL_GOVERNANCE_IDS.skill,
    name: '剧情短片 Skill',
    summary: '当前发布内容',
    category: '短剧',
    publishedAt: '2026-07-14T10:00:00.123456789+08:00',
    governanceStatus: 'active',
    governanceEpoch: 1,
    allowedActions: ['suspend', 'offline'],
    ...overrides
  };
}

function governorSession() {
  return {
    status: AUTH_SESSION_STATUS.AUTHENTICATED,
    user: {
      id: '019f0000-0000-7000-8000-000000000126',
      name: 'Governor',
      roles: ['skill_governor'],
      capabilities: ['skill.govern']
    },
    csrfToken: 'csrf-governor',
    sessionExpiresAt: '2026-07-15T08:00:00Z'
  };
}

function authPayload() {
  return {
    status: 'authenticated',
    principal: {
      id: '019f0000-0000-7000-8000-000000000126',
      display_name: 'Governor',
      email: 'governor@example.com',
      account_status: 'active',
      roles: ['skill_governor'],
      capabilities: ['skill.govern']
    },
    csrf_token: 'csrf-governor-2',
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}

function defaultAuthClient() {
  return { bootstrap: vi.fn(), login: vi.fn(), logout: vi.fn() };
}
