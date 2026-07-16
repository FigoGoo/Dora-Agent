import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { AUTH_SESSION_STATUS, AuthSessionProvider } from '../../platform/auth/authSession.js';
import { SKILL_GOVERNANCE_IDS } from '../../test/skillGovernanceFixtures.js';
import { skillDefinitionFixture } from '../../test/skillFixtures.js';
import { createSkillCommandLedger } from '../skills/skillCommandLedger.jsx';
import { GovernanceDetailPage } from './GovernanceDetailPage.jsx';

describe('GovernanceDetailPage', () => {
  it('renders the current published Definition read-only and only exposes allowed actions', async () => {
    const client = governanceClient();
    renderDetail({ client });

    expect(await screen.findByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '当前发布内容' })).toHaveTextContent('适合剧情短片创作');
    expect(screen.getByText('当前发布 SkillDefinitionV1')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '暂停' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '永久下架' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '恢复' })).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  });

  it('renders a missing governance object without retrying a different resource', async () => {
    const client = governanceClient();
    client.get.mockRejectedValue(Object.assign(new Error('治理对象不存在'), {
      status: 404,
      code: 'SKILL_GOVERNANCE_NOT_FOUND'
    }));
    renderDetail({ client });

    expect(await screen.findByRole('heading', { name: 'Skill 治理对象不存在' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('治理对象不存在');
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument();
    expect(client.get).toHaveBeenCalledTimes(1);
  });

  it('retries a transient detail load with the same Skill resource', async () => {
    const user = userEvent.setup();
    const client = governanceClient();
    client.get
      .mockRejectedValueOnce(Object.assign(new Error('治理详情暂不可用'), { status: 503 }))
      .mockResolvedValueOnce(detailResult());
    renderDetail({ client });

    expect(await screen.findByRole('alert')).toHaveTextContent('治理详情暂不可用');
    await user.click(screen.getByRole('button', { name: '重试' }));
    expect(await screen.findByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(client.get).toHaveBeenCalledTimes(2);
    expect(client.get.mock.calls.every(([skillID]) => skillID === SKILL_GOVERNANCE_IDS.skill)).toBe(true);
  });

  it('clears a pending command when the detail read reports stable capability revocation', async () => {
    const ledger = createSkillCommandLedger();
    ledger.set(decisionScope(), { key: 'pending-governance-key' });
    const client = governanceClient();
    client.get.mockRejectedValue(Object.assign(new Error('治理权限已撤销'), {
      status: 403,
      code: 'SKILL_GOVERNANCE_CAPABILITY_REQUIRED'
    }));
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()),
      login: vi.fn(),
      logout: vi.fn()
    };
    renderDetail({ client, ledger, authClient });

    expect(await screen.findByRole('heading', { name: '无 Skill 治理权限' })).toBeInTheDocument();
    await waitFor(() => expect(authClient.bootstrap).toHaveBeenCalledTimes(1));
    expect(ledger.get(decisionScope())).toBeNull();
    expect(client.get).toHaveBeenCalledTimes(1);
  });

  it('submits an action-specific reason and approval reference with the frozen command semantics', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    expect(screen.getByRole('option', { name: /事件止损.*incident_containment/ })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /风险已解除.*risk_cleared/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));

    expect(await screen.findByText('治理处置已完成，页面已更新为权威结果。')).toBeInTheDocument();
    expect(client.decide).toHaveBeenCalledWith({
      skillID: SKILL_GOVERNANCE_IDS.skill,
      action: 'suspend',
      reasonCode: 'content_safety',
      approvalReference: 'TICKET-123',
      idempotencyKey: 'governance-key-1',
      governanceETag: '"skill-governance-etag-1"',
      csrfToken: 'csrf-governor',
      signal: expect.any(AbortSignal)
    });
    expect(ledger.get(decisionScope())).toBeNull();
    expect(screen.getByRole('button', { name: '恢复' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '永久下架' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '暂停' })).not.toBeInTheDocument();
  });

  it('retries an unknown outcome with the original key, ETag, action, reason and approval reference', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    client.decide
      .mockRejectedValueOnce(Object.assign(new Error('响应在提交后丢失'), { status: 503 }))
      .mockResolvedValueOnce(decisionResult());
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '永久下架', reasonCode: 'owner_request', approvalReference: 'CASE-42' });
    await user.click(screen.getByRole('button', { name: '提交永久下架处置' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('响应在提交后丢失');
    expect(ledger.get(decisionScope())).toMatchObject({
      key: 'governance-key-1',
      skillID: SKILL_GOVERNANCE_IDS.skill,
      action: 'offline',
      reasonCode: 'owner_request',
      approvalReference: 'CASE-42',
      governanceETag: '"skill-governance-etag-1"'
    });

    await user.click(screen.getByRole('button', { name: '使用原命令重试' }));
    await screen.findByText('治理处置已完成，页面已更新为权威结果。');

    expect(client.createKey).toHaveBeenCalledTimes(1);
    expect(client.decide).toHaveBeenCalledTimes(2);
    const first = client.decide.mock.calls[0][0];
    const second = client.decide.mock.calls[1][0];
    expect(second).toMatchObject({
      skillID: first.skillID,
      action: first.action,
      reasonCode: first.reasonCode,
      approvalReference: first.approvalReference,
      idempotencyKey: first.idempotencyKey,
      governanceETag: first.governanceETag
    });
  });

  it('clears a governance-conflict command, refreshes authority automatically and returns to idle', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    const authoritativeRefresh = deferred();
    client.get
      .mockResolvedValueOnce(detailResult())
      .mockReturnValueOnce(authoritativeRefresh.promise);
    const refreshedDetail = detailResult({
        governanceStatus: 'suspended',
        governanceEpoch: 2,
        governanceETag: '"skill-governance-etag-2"',
        allowedActions: ['resume', 'offline']
      });
    client.decide.mockRejectedValue(Object.assign(new Error('治理版本已变化'), {
      status: 409,
      code: 'SKILL_GOVERNANCE_CONFLICT'
    }));
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));

    expect(await screen.findByText('治理版本已变化')).toBeInTheDocument();
    await waitFor(() => expect(client.get).toHaveBeenCalledTimes(2));
    authoritativeRefresh.resolve(refreshedDetail);
    expect(await screen.findAllByText('已暂停')).toHaveLength(2);
    expect(ledger.get(decisionScope())).toBeNull();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '恢复' })).toBeEnabled();
  });

  it('does not create a new key for IDEMPOTENCY_CONFLICT', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    client.decide.mockRejectedValue(Object.assign(new Error('Key 已绑定其他语义'), {
      status: 409,
      code: 'IDEMPOTENCY_CONFLICT'
    }));
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Key 已绑定其他语义');
    expect(client.createKey).toHaveBeenCalledTimes(1);
    expect(client.decide).toHaveBeenCalledTimes(1);
    expect(ledger.get(decisionScope())).toMatchObject({ key: 'governance-key-1' });
    expect(screen.queryByRole('button', { name: '使用原命令重试' })).not.toBeInTheDocument();
  });

  it('clears the command and re-parses authority only for the stable capability 403', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    client.decide.mockRejectedValue(Object.assign(new Error('治理权限已撤销'), {
      status: 403,
      code: 'SKILL_GOVERNANCE_CAPABILITY_REQUIRED'
    }));
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()),
      login: vi.fn(),
      logout: vi.fn()
    };
    renderDetail({ client, ledger, authClient });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));

    expect(await screen.findByRole('heading', { name: '无 Skill 治理权限' })).toBeInTheDocument();
    await waitFor(() => expect(authClient.bootstrap).toHaveBeenCalledTimes(1));
    expect(ledger.get(decisionScope())).toBeNull();
    expect(client.decide).toHaveBeenCalledTimes(1);
  });

  it('treats CSRF_INVALID as a deterministic 403 without latching capability denial', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    client.decide.mockRejectedValue(Object.assign(new Error('CSRF 校验失败'), {
      status: 403,
      code: 'CSRF_INVALID'
    }));
    const authClient = {
      bootstrap: vi.fn().mockResolvedValue(authPayload()),
      login: vi.fn(),
      logout: vi.fn()
    };
    renderDetail({ client, ledger, authClient });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('CSRF 校验失败');
    expect(screen.getByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(authClient.bootstrap).not.toHaveBeenCalled();
    expect(ledger.get(decisionScope())).toBeNull();
  });

  it.each([
    [{ status: 401, code: 'UNAUTHENTICATED' }, '会话已失效'],
    [{ status: 404, code: 'SKILL_GOVERNANCE_NOT_FOUND' }, '治理对象不存在'],
    [{ status: 400, code: 'INVALID_REQUEST' }, '治理命令不合法']
  ])('clears a command after deterministic failure %o', async (failure, message) => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    client.decide.mockRejectedValue(Object.assign(new Error(message), failure));
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(message);
    expect(ledger.get(decisionScope())).toBeNull();
  });

  it('renders offline as terminal with no recovery action', async () => {
    const client = governanceClient({
      detail: detailResult({
        governanceStatus: 'offline',
        governanceEpoch: 2,
        governanceETag: '"skill-governance-etag-offline"',
        allowedActions: []
      })
    });
    renderDetail({ client });

    expect(await screen.findByRole('heading', { name: '该 Skill 已永久下架' })).toBeInTheDocument();
    expect(screen.getAllByText('已永久下架')).toHaveLength(2);
    expect(screen.queryByRole('button', { name: '恢复' })).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: '治理动作' })).not.toBeInTheDocument();
  });

  it('keeps the original command when a successful-looking response has another skill_id', async () => {
    const user = userEvent.setup();
    const ledger = createSkillCommandLedger();
    const client = governanceClient();
    client.decide.mockResolvedValue(decisionResult({ skillID: SKILL_GOVERNANCE_IDS.secondSkill }));
    renderDetail({ client, ledger });

    await screen.findByRole('heading', { name: '剧情短片 Skill' });
    await fillDecisionForm(user, { action: '暂停', reasonCode: 'content_safety', approvalReference: 'TICKET-123' });
    await user.click(screen.getByRole('button', { name: '提交暂停处置' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('资源身份');
    expect(screen.getByRole('button', { name: '使用原命令重试' })).toBeInTheDocument();
    expect(ledger.get(decisionScope())).toMatchObject({ key: 'governance-key-1' });
  });
});

async function fillDecisionForm(user, { action, reasonCode, approvalReference }) {
  await user.click(screen.getByRole('button', { name: action }));
  await user.selectOptions(screen.getByLabelText('原因代码'), reasonCode);
  await user.type(screen.getByLabelText('外部审批引用'), approvalReference);
}

function renderDetail({
  client,
  ledger = createSkillCommandLedger(),
  authClient = defaultAuthClient()
}) {
  return render(
    <AuthSessionProvider autoBootstrap={false} client={authClient} initialSession={governorSession()}>
      <GovernanceDetailPage
        skillID={SKILL_GOVERNANCE_IDS.skill}
        csrfToken="csrf-governor"
        client={client}
        commandLedger={ledger}
        onNavigate={vi.fn()}
      />
    </AuthSessionProvider>
  );
}

function governanceClient({ detail = detailResult(), decision = decisionResult() } = {}) {
  return {
    get: vi.fn().mockResolvedValue(detail),
    decide: vi.fn().mockResolvedValue(decision),
    createKey: vi.fn().mockReturnValue('governance-key-1')
  };
}

function detailResult(overrides = {}) {
  return {
    skill: {
      skillID: SKILL_GOVERNANCE_IDS.skill,
      definition: skillDefinitionFixture(),
      publishedAt: '2026-07-14T10:00:00.123456789+08:00',
      governanceStatus: 'active',
      governanceEpoch: 1,
      governanceETag: '"skill-governance-etag-1"',
      allowedActions: ['suspend', 'offline'],
      ...overrides
    },
    requestID: SKILL_GOVERNANCE_IDS.request
  };
}

function decisionResult(overrides = {}) {
  return {
    skill: {
      skillID: SKILL_GOVERNANCE_IDS.skill,
      governanceStatus: 'suspended',
      governanceEpoch: 2,
      transitionedAt: '2026-07-14T10:05:00.123456789+08:00',
      governanceETag: '"skill-governance-etag-2"',
      allowedActions: ['resume', 'offline'],
      ...overrides
    },
    requestID: SKILL_GOVERNANCE_IDS.request
  };
}

function decisionScope() {
  return `skill-governance-decision:${SKILL_GOVERNANCE_IDS.skill}`;
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

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
