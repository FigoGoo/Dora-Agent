import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { AUTH_SESSION_STATUS, AuthSessionProvider } from '../../platform/auth/authSession.js';
import { SKILL_REVIEW_IDS } from '../../test/skillReviewFixtures.js';
import { SkillReviewQueuePage } from './SkillReviewQueuePage.jsx';

describe('SkillReviewQueuePage', () => {
  it('renders the oldest frozen submissions and navigates to the exact detail route', async () => {
    const user = userEvent.setup();
    const onNavigate = vi.fn();
    const loadReviews = vi.fn().mockResolvedValue({
      items: [queueItem()], nextCursor: null, requestID: SKILL_REVIEW_IDS.request
    });
    renderQueue({ loadReviews, onNavigate });

    expect(await screen.findByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(screen.getByText('冻结提交 sentinel A')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '查看冻结详情' }));

    expect(onNavigate).toHaveBeenCalledWith(`/admin/skills/reviews/${SKILL_REVIEW_IDS.review}`);
    expect(loadReviews).toHaveBeenCalledWith({ cursor: null, signal: expect.any(AbortSignal) });
  });

  it('covers empty, pagination and duplicate-page fail-closed states', async () => {
    const user = userEvent.setup();
    const loadReviews = vi.fn()
      .mockResolvedValueOnce({ items: [queueItem()], nextCursor: 'cursor-2' })
      .mockResolvedValueOnce({
        items: [queueItem({ reviewID: SKILL_REVIEW_IDS.secondReview, skillID: SKILL_REVIEW_IDS.secondSkill, name: '第二个 Skill' })],
        nextCursor: 'cursor-3'
      })
      .mockResolvedValueOnce({
        items: [queueItem({ reviewID: SKILL_REVIEW_IDS.secondReview, skillID: '019f0000-0000-7000-8000-000000000130' })],
        nextCursor: null
      });
    renderQueue({ loadReviews });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('heading', { name: '第二个 Skill' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('重复项目');

    expect(loadReviews.mock.calls[1][0].cursor).toBe('cursor-2');
    expect(loadReviews.mock.calls[2][0].cursor).toBe('cursor-3');
  });

  it('shows an empty state and can explicitly retry a transient error', async () => {
    const user = userEvent.setup();
    const loadReviews = vi.fn()
      .mockRejectedValueOnce(Object.assign(new Error('队列暂不可用'), { status: 503, requestID: 'request-1' }))
      .mockResolvedValueOnce({ items: [], nextCursor: null });
    renderQueue({ loadReviews });

    expect(await screen.findByRole('alert')).toHaveTextContent('队列暂不可用');
    expect(screen.getByText('请求 ID：request-1')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('heading', { name: '当前没有待审核 Skill' })).toBeInTheDocument();
    expect(loadReviews).toHaveBeenCalledTimes(2);
  });

  it('re-parses the authoritative session once after the stable capability 403', async () => {
    const forbidden = Object.assign(new Error('权限已撤销'), { status: 403, code: 'SKILL_REVIEW_CAPABILITY_REQUIRED' });
    const loadReviews = vi.fn().mockRejectedValue(forbidden);
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()),
      login: vi.fn(),
      logout: vi.fn()
    };
    renderQueue({ loadReviews, authClient });

    expect(await screen.findByRole('alert')).toHaveTextContent('失去 Skill 审核权限');
    await waitFor(() => expect(authClient.bootstrap).toHaveBeenCalledTimes(1));
    expect(authClient.bootstrap).toHaveBeenCalledWith();
    expect(loadReviews).toHaveBeenCalledTimes(1);
  });

  it('does not re-bootstrap authority for an unrelated 403 error code', async () => {
    const forbidden = Object.assign(new Error('CSRF 校验失败'), { status: 403, code: 'CSRF_INVALID' });
    const loadReviews = vi.fn().mockRejectedValue(forbidden);
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()),
      login: vi.fn(),
      logout: vi.fn()
    };
    renderQueue({ loadReviews, authClient });

    expect(await screen.findByRole('alert')).toHaveTextContent('CSRF 校验失败');
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument();
    expect(authClient.bootstrap).not.toHaveBeenCalled();
  });
});

function renderQueue({ loadReviews, onNavigate = vi.fn(), authClient = defaultAuthClient() }) {
  return render(
    <AuthSessionProvider autoBootstrap={false} client={authClient} initialSession={reviewerSession()}>
      <SkillReviewQueuePage loadReviews={loadReviews} onNavigate={onNavigate} />
    </AuthSessionProvider>
  );
}

function queueItem(overrides = {}) {
  return {
    reviewID: SKILL_REVIEW_IDS.review,
    skillID: SKILL_REVIEW_IDS.skill,
    name: '剧情短片 Skill',
    summary: '冻结提交 sentinel A',
    category: '短剧',
    status: 'reviewing',
    submittedAt: '2026-07-14T10:00:00.123456789+08:00',
    allowedActions: ['approve_and_publish'],
    ...overrides
  };
}

function reviewerSession() {
  return {
    status: AUTH_SESSION_STATUS.AUTHENTICATED,
    user: {
      id: SKILL_REVIEW_IDS.owner,
      name: 'Reviewer',
      roles: ['skill_reviewer'],
      capabilities: ['skill.review']
    },
    csrfToken: 'csrf-reviewer',
    sessionExpiresAt: '2026-07-15T08:00:00Z'
  };
}

function authPayload() {
  return {
    status: 'authenticated',
    principal: {
      id: SKILL_REVIEW_IDS.owner,
      display_name: 'Reviewer',
      email: 'reviewer@example.com',
      account_status: 'active',
      roles: ['skill_reviewer'],
      capabilities: ['skill.review']
    },
    csrf_token: 'csrf-reviewer-2',
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}

function defaultAuthClient() {
  return { bootstrap: vi.fn(), login: vi.fn(), logout: vi.fn() };
}
