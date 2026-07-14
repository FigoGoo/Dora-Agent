import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { AUTH_SESSION_STATUS, AuthSessionProvider } from '../../platform/auth/authSession.js';
import {
  currentPublishedFixture,
  SKILL_REVIEW_IDS,
  skillReviewDecisionResponseFixture,
  skillReviewDetailFixture,
  skillReviewDetailResponseFixture
} from '../../test/skillReviewFixtures.js';
import { createSkillCommandLedger } from '../skills/skillCommandLedger.jsx';
import { SkillReviewDetailPage } from './SkillReviewDetailPage.jsx';
import {
  parseSkillReviewDecisionResponse,
  parseSkillReviewDetailResponse
} from './skillReviewContract.js';

describe('SkillReviewDetailPage', () => {
  it('renders the complete submitted and current published Definitions read-only', async () => {
    const client = reviewClient({
      review: parsedReview({
        current_published: currentPublishedFixture(),
        comparison: { has_current_published: true, same_content: false }
      })
    });
    renderDetail({ client });

    expect(await screen.findByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '本次冻结提交' })).toHaveTextContent('冻结提交 sentinel A');
    expect(screen.getByRole('region', { name: '当前发布内容' })).toHaveTextContent('当前已发布内容');
    expect(screen.getByText('本次提交与当前发布内容不同')).toBeInTheDocument();
    expect(screen.getAllByText('六个能力字段')).toHaveLength(2);
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('approves with the frozen ETag and converges to the published snapshot', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = reviewClient();
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));

    expect(await screen.findByText('审核已批准，冻结内容已原子发布。')).toBeInTheDocument();
    expect(screen.getByText('已批准并发布')).toBeInTheDocument();
    expect(client.approve).toHaveBeenCalledWith(expect.objectContaining({
      reviewID: SKILL_REVIEW_IDS.review,
      idempotencyKey: 'decision-key-1',
      reviewETag: '"skill-review-etag-1"',
      csrfToken: 'csrf-reviewer',
      signal: expect.any(AbortSignal)
    }));
    expect(ledger.get(decisionScope())).toBeNull();
    expect(screen.getByRole('button', { name: '批准并发布' })).toBeDisabled();
  });

  it('keeps the same in-memory key and ETag for explicit unknown-outcome retry', async () => {
    const user = userEvent.setup();
    const unknown = Object.assign(new Error('上游响应丢失'), { status: 503, code: 'UPSTREAM_UNAVAILABLE' });
    const client = reviewClient();
    client.approve
      .mockRejectedValueOnce(unknown)
      .mockResolvedValueOnce(parsedDecision());
    renderDetail({ client });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('上游响应丢失');
    expect(screen.getByRole('button', { name: '批准并发布' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: '使用原请求重试' }));
    await screen.findByText('审核已批准，冻结内容已原子发布。');

    expect(client.createKey).toHaveBeenCalledTimes(1);
    expect(client.approve).toHaveBeenCalledTimes(2);
    const first = client.approve.mock.calls[0][0];
    const second = client.approve.mock.calls[1][0];
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
    expect(second.reviewETag).toBe(first.reviewETag);
  });

  it('treats a valid-looking 2xx decision for another Skill as unknown and keeps its command', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const mismatched = parsedDecision();
    mismatched.review = { ...mismatched.review, skillID: SKILL_REVIEW_IDS.secondSkill };
    const client = reviewClient();
    client.approve.mockResolvedValue(mismatched);
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('资源身份');
    expect(screen.getByRole('button', { name: '使用原请求重试' })).toBeInTheDocument();
    expect(ledger.get(decisionScope())).toMatchObject({
      key: 'decision-key-1',
      reviewETag: '"skill-review-etag-1"'
    });
  });

  it('keeps context on review conflict, then discards the command before authoritative refresh', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const terminal = parsedReview({
      status: 'approved',
      updated_at: '2026-07-14T10:06:00.123456789Z',
      review_etag: '"skill-review-etag-2"',
      allowed_actions: []
    });
    const client = reviewClient();
    client.get.mockResolvedValueOnce({ review: parsedReview(), requestID: SKILL_REVIEW_IDS.request })
      .mockResolvedValueOnce({ review: terminal, requestID: SKILL_REVIEW_IDS.request });
    client.approve.mockRejectedValue(Object.assign(new Error('审核版本已变化'), {
      status: 409,
      code: 'SKILL_REVIEW_CONFLICT'
    }));
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('审核版本已变化');
    expect(ledger.get(decisionScope())).toMatchObject({ key: 'decision-key-1' });
    await user.click(screen.getByRole('button', { name: '刷新权威详情并废弃旧命令' }));

    expect(await screen.findByText('已批准并发布')).toBeInTheDocument();
    expect(client.get).toHaveBeenCalledTimes(2);
    expect(ledger.get(decisionScope())).toBeNull();
    expect(screen.queryByRole('button', { name: '使用原请求重试' })).not.toBeInTheDocument();
  });

  it('requires an explicit authoritative refresh before replacing a stale pending command', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    ledger.set(decisionScope(), {
      key: 'old-unknown-key',
      semantic: `${SKILL_REVIEW_IDS.review}\u0000"old-etag"\u0000approved`,
      reviewETag: '"old-etag"',
      decision: 'approved'
    });
    const client = reviewClient();
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('仍绑定旧 ETag');
    expect(client.approve).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: '刷新权威详情并废弃旧命令' }));

    await waitFor(() => expect(client.get).toHaveBeenCalledTimes(2));
    expect(ledger.get(decisionScope())).toBeNull();
    expect(screen.getByRole('button', { name: '批准并发布' })).toBeEnabled();
  });

  it('does not invent a new key for idempotency conflict', async () => {
    const user = userEvent.setup();
    const client = reviewClient();
    client.approve.mockRejectedValue(Object.assign(new Error('Key 已绑定其他语义'), {
      status: 409,
      code: 'IDEMPOTENCY_CONFLICT'
    }));
    renderDetail({ client });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Key 已绑定其他语义');
    expect(screen.queryByRole('button', { name: '使用原请求重试' })).not.toBeInTheDocument();
    expect(client.createKey).toHaveBeenCalledTimes(1);
    expect(client.approve).toHaveBeenCalledTimes(1);
  });

  it('clears retry-decision and re-parses authority exactly once after 403', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = reviewClient();
    client.approve.mockRejectedValue(Object.assign(new Error('审核权限已撤销'), {
      status: 403,
      code: 'SKILL_REVIEW_CAPABILITY_REQUIRED'
    }));
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()), login: vi.fn(), logout: vi.fn()
    };
    renderDetail({ client, ledger, authClient });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('审核权限已撤销');
    await waitFor(() => expect(authClient.bootstrap).toHaveBeenCalledTimes(1));
    expect(authClient.bootstrap).toHaveBeenCalledWith();
    expect(client.approve).toHaveBeenCalledTimes(1);
    expect(ledger.get(decisionScope())).toBeNull();
    expect(screen.queryByRole('button', { name: '使用原请求重试' })).not.toBeInTheDocument();
  });

  it('treats a non-capability 403 as a definitive decision rejection without re-bootstrap', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = reviewClient();
    client.approve.mockRejectedValue(Object.assign(new Error('CSRF 校验失败'), {
      status: 403,
      code: 'CSRF_INVALID'
    }));
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()), login: vi.fn(), logout: vi.fn()
    };
    renderDetail({ client, ledger, authClient });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await user.click(screen.getByRole('button', { name: '批准并发布' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('CSRF 校验失败');
    expect(screen.getByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(authClient.bootstrap).not.toHaveBeenCalled();
    expect(ledger.get(decisionScope())).toBeNull();
  });

  it.each([
    [{ status: 403, code: 'SKILL_REVIEW_CAPABILITY_REQUIRED' }, '无 Skill 审核权限'],
    [{ status: 403, code: 'CSRF_INVALID' }, '审核详情暂不可用'],
    [{ status: 404 }, '审核记录不存在']
  ])('fails closed on detail load error %o with no retry decision', async (failure, heading) => {
    const client = reviewClient();
    client.get.mockRejectedValue(Object.assign(new Error('详情不可访问'), failure));
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()), login: vi.fn(), logout: vi.fn()
    };
    renderDetail({ client, authClient });

    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '使用原请求重试' })).not.toBeInTheDocument();
    if (failure.code !== 'SKILL_REVIEW_CAPABILITY_REQUIRED') {
      expect(authClient.bootstrap).not.toHaveBeenCalled();
    }
  });
});

function renderDetail({ client, ledger = createSkillCommandLedger(), authClient = defaultAuthClient() }) {
  return render(
    <AuthSessionProvider autoBootstrap={false} client={authClient} initialSession={reviewerSession()}>
      <SkillReviewDetailPage
        reviewID={SKILL_REVIEW_IDS.review}
        csrfToken="csrf-reviewer"
        client={client}
        commandLedger={ledger}
        onNavigate={vi.fn()}
      />
    </AuthSessionProvider>
  );
}

function reviewClient({ review = parsedReview() } = {}) {
  return {
    get: vi.fn().mockResolvedValue({ review, requestID: SKILL_REVIEW_IDS.request }),
    approve: vi.fn().mockResolvedValue(parsedDecision()),
    createKey: vi.fn().mockReturnValue('decision-key-1')
  };
}

function parsedReview(overrides = {}) {
  return parseSkillReviewDetailResponse(skillReviewDetailResponseFixture({
    review: skillReviewDetailFixture(overrides)
  })).review;
}

function parsedDecision() {
  return parseSkillReviewDecisionResponse(skillReviewDecisionResponseFixture());
}

function decisionScope() {
  return `skill-review-decision:${SKILL_REVIEW_IDS.review}`;
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
