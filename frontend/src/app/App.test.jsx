import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { projectBootstrapFixture, WORKSPACE_IDS, workspaceSnapshotFixture } from '../test/workspaceFixtures.js';
import { ownerSkillFixture, SKILL_IDS } from '../test/skillFixtures.js';
import {
  SKILL_MARKET_IDS,
  skillMarketDetailFixture,
  skillMarketListItemFixture
} from '../test/skillMarketFixtures.js';
import { skillReviewQueueResponseFixture } from '../test/skillReviewFixtures.js';
import { skillGovernanceListResponseFixture } from '../test/skillGovernanceFixtures.js';
import { AUTH_SESSION_EXPIRED_EVENT } from '../platform/auth/authSession.js';
import { App } from './App.jsx';

beforeEach(() => {
  vi.stubGlobal('fetch', mockAppFetch());
});

afterEach(() => {
  vi.restoreAllMocks();
  DefaultMockEventSource.instances = [];
  window.history.pushState({}, '', '/');
});

class DefaultMockEventSource {
  static instances = [];

  constructor(url) {
    this.url = url;
    this.listeners = {};
    this.close = vi.fn();
    DefaultMockEventSource.instances.push(this);
  }

  addEventListener(eventName, listener) {
    this.listeners[eventName] = listener;
  }

  removeEventListener(eventName) {
    delete this.listeners[eventName];
  }

  emit(event) {
    const listener = this.listeners[event.event];
    if (listener) {
      listener({ data: JSON.stringify(event) });
    }
  }
}

function ensureDefaultEventSource() {
  if (typeof window.EventSource !== 'function') {
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
  }
}

function emitMockEvents(events) {
  if (!events.length || window.EventSource !== DefaultMockEventSource) {
    return;
  }
  act(() => {
    DefaultMockEventSource.instances.forEach((source) => {
      events.forEach((event) => source.emit(event));
    });
  });
}

describe('DORAIGC landing page', () => {
  it('renders the approved brand and prompt-first creation entry', () => {
    render(<App />);

    const logo = screen.getByRole('img', { name: 'DORAIGC 标志' });
    expect(logo).toHaveAttribute('src', '/brand/doraigc-logo-mark-256.png');
    expect(logo).toHaveAttribute('srcset', '/brand/doraigc-logo-mark-256.png 1x, /brand/doraigc-logo-mark-512.png 2x');
    expect(screen.getByText('DORAIGC')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Dora Agent - 人人都是艺术大师' })).toBeInTheDocument();
    expect(screen.getByText('把灵感交给 Dora Agent，从一句想法生成影像、音乐、海报和商品内容，让每个人都能完成自己的创作。')).toBeInTheDocument();
    expect(screen.queryByText('AI creation agent')).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('由一个想法或故事开始...')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '添加素材' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '精选作品' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '开始创作' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '切换到日间主题' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '切换到夜间主题' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '新建创作' })).not.toBeInTheDocument();
  });

  it('keeps unauthenticated creation intent in the login modal', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.type(screen.getByPlaceholderText('由一个想法或故事开始...'), '做一个霓虹城市里的音乐短片');
    await user.click(screen.getByRole('button', { name: '开始创作' }));

    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    expect(dialog).toBeInTheDocument();
    expect(within(dialog).getByText('做一个霓虹城市里的音乐短片')).toBeInTheDocument();
  });

  it('selects only a published active Owner Skill and submits explicit QuickCreate v2', async () => {
    const ownerSkills = [
      ownerSkillFixture({
        content_status: 'published',
        has_unpublished_changes: false,
        allowed_actions: ['edit_draft']
      }),
      ownerSkillFixture({
        skill_id: '019f0000-0000-7000-8000-000000000124',
        definition: {
          ...ownerSkillFixture().definition,
          name: '尚未发布的 Skill'
        }
      })
    ];
    const fetchMock = mockAppFetch({ authenticatedBootstrap: true, ownerSkills });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await screen.findByRole('button', { name: '用户菜单' });
    await user.click(screen.getByRole('button', { name: 'Skill' }));
    const picker = screen.getByRole('dialog', { name: '选择 QuickCreate Skill' });
    const selectable = await within(picker).findByRole('checkbox', { name: '选择 剧情短片 Skill' });
    expect(selectable).toBeEnabled();
    expect(within(picker).getByRole('checkbox', { name: '选择 尚未发布的 Skill' })).toBeDisabled();
    await user.click(selectable);
    await user.type(screen.getByPlaceholderText('由一个想法或故事开始...'), '使用我的 Skill 创作');
    await user.click(screen.getByRole('button', { name: '开始创作' }));

    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => (
      requestPath(input) === '/api/v1/projects:quick-create'
    ))).toBe(true));
    const quickCall = fetchMock.mock.calls.find(([input]) => requestPath(input) === '/api/v1/projects:quick-create');
    expect(JSON.parse(quickCall[1].body)).toEqual({
      schema_version: 'project_quick_create.v2',
      initial_prompt: '使用我的 Skill 创作',
      enabled_skill_ids: [SKILL_IDS.skill]
    });
  });

  it('clears a selected QuickCreate Skill when a refreshed Owner projection reports session expiry', async () => {
    const published = ownerSkillFixture({
      content_status: 'published',
      has_unpublished_changes: false,
      allowed_actions: ['edit_draft']
    });
    const authenticatedFetch = mockAppFetch({ authenticatedBootstrap: true, ownerSkills: [published] });
    let ownerListAttempts = 0;
    const fetchMock = vi.fn(async (input, options = {}) => {
      if (requestPath(input) === '/api/v1/skills' && (options.method || 'GET') === 'GET') {
        ownerListAttempts += 1;
        if (ownerListAttempts === 2) {
          return jsonResponse({
            error: { code: 'UNAUTHENTICATED', message: '会话已过期', retryable: false }
          }, 401);
        }
      }
      return authenticatedFetch(input, options);
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await screen.findByRole('button', { name: '用户菜单' });
    await user.click(screen.getByRole('button', { name: 'Skill' }));
    await user.click(await screen.findByRole('checkbox', { name: '选择 剧情短片 Skill' }));
    await user.click(screen.getByRole('button', { name: '关闭 Skill 选择' }));
    await user.click(screen.getByRole('button', { name: 'Skill，已选择 1 个' }));

    expect(await screen.findByRole('button', { name: '登录' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Skill' })).not.toHaveClass('is-active');
    expect(screen.queryByRole('dialog', { name: '选择 QuickCreate Skill' })).not.toBeInTheDocument();
  });

  it('filters the public work feed by category', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '动漫' }));

    expect(screen.getByRole('button', { name: '动漫' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('article', { name: '放学后的短剧 作品卡' })).toBeInTheDocument();
    expect(screen.getByRole('article', { name: '机械伙伴竖屏剧 作品卡' })).toBeInTheDocument();
    expect(screen.queryByRole('article', { name: 'MV 分镜生成 作品卡' })).not.toBeInTheDocument();
  });

  it('opens a public work preview without requiring login', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '预览 MV 分镜生成' }));

    const dialog = screen.getByRole('dialog', { name: 'MV 分镜生成' });
    expect(within(dialog).getByText('MV')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '用这个方向创作' })).toBeInTheDocument();
  });

  it('uses low-weight tags and hover operation layers on content cards', async () => {
    const user = userEvent.setup();
    render(<App />);

    const workCard = screen.getByRole('article', { name: 'MV 分镜生成 作品卡' });
    const categoryTag = within(workCard).getByText('MV');
    expect(categoryTag).toHaveClass('transparent-tag');
    expect(categoryTag.querySelector('svg')).toBeNull();
    const muteButton = within(workCard).getByRole('button', { name: '静音预览 MV 分镜生成' });
    expect(muteButton).toHaveClass('work-card__icon-action');
    expect(muteButton).toHaveAttribute('aria-pressed', 'false');
    expect(within(workCard).getByRole('button', { name: '全屏播放 MV 分镜生成' })).toHaveClass('work-card__icon-action');
    expect(within(workCard).getByRole('button', { name: '预览 MV 分镜生成' }).closest('.work-card__operation-layer')).not.toBeNull();
    expect(workCard.querySelector('.work-card__avatar img')).toHaveAttribute('src', expect.stringContaining('/avatars/doraigc-avatar-'));
    expect(within(workCard).getByText('@Aplus影像')).toHaveClass('work-card__byline');
    expect(within(workCard).getByRole('heading', { name: 'MV 分镜生成' })).toHaveClass('work-card__title');
    screen.getAllByRole('article').forEach((card) => {
      expect(within(card).getByRole('heading')).toHaveClass('work-card__title');
    });
    expect(screen.queryByText('@Aplus影像 · 4.8k')).not.toBeInTheDocument();

    await user.click(muteButton);
    expect(muteButton).toHaveAttribute('aria-pressed', 'true');

    await user.click(within(workCard).getByRole('button', { name: '全屏播放 MV 分镜生成' }));
    expect(screen.getByRole('dialog', { name: 'MV 分镜生成' })).toBeInTheDocument();

    const cardRatios = screen.getAllByRole('article').map((card) => card.style.getPropertyValue('--work-ratio'));

    for (const ratio of ['16 / 9', '4 / 3', '1 / 1', '3 / 4', '9 / 16']) {
      expect(cardRatios).toContain(ratio);
    }

    expect(screen.getByRole('button', { name: '全部' })).toHaveClass('is-active');
  });

  it('adds a hover preview layer for every hot Skill', () => {
    const { container } = render(<App />);

    const hotSkillShells = container.querySelectorAll('.hot-skill-shell');
    expect(hotSkillShells).toHaveLength(8);
    expect(container.querySelectorAll('.skill-preview-card')).toHaveLength(8);
    expect(screen.getByRole('button', { name: 'AI 短剧一站式生成热门' })).toBeInTheDocument();
  });

  it('closes overlays with Escape', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '开始创作' }));
    expect(screen.getByRole('dialog', { name: '登录后继续创作' })).toBeInTheDocument();

    await user.keyboard('{Escape}');

    expect(screen.queryByRole('dialog', { name: '登录后继续创作' })).not.toBeInTheDocument();
  });

  it('exposes logo-derived theme tokens on the app shell', () => {
    render(<App />);

    const shell = screen.getByTestId('doraigc-shell');
    expect(shell).toHaveStyle({
      '--dora-lime': '#cfff24',
      '--dora-mint': '#35e0ba',
      '--dora-cyan': '#4bd8ff'
    });
  });
});

describe('DORAIGC static client pages', () => {
  it('keeps the side navigation focused on creation and content entry points', () => {
    render(<App />);

    const navigation = screen.getByRole('complementary', { name: 'DORAIGC 导航' });

    expect(within(navigation).getByRole('button', { name: '快速创作' })).toBeInTheDocument();
    expect(within(navigation).queryByRole('button', { name: '工作台' })).not.toBeInTheDocument();
    expect(within(navigation).queryByRole('button', { name: '作品中心' })).not.toBeInTheDocument();
    expect(within(navigation).queryByRole('button', { name: '积分' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '148积分' })).toBeInTheDocument();
  });

  it('does not render the projects page before a direct-route auth bootstrap succeeds', async () => {
    window.history.pushState({}, '', '/projects');
    vi.stubGlobal('fetch', mockAppFetch({ authenticatedBootstrap: true }));

    render(<App />);

    expect(screen.getByRole('heading', { name: '正在确认登录状态' })).toBeInTheDocument();
    const navigation = await screen.findByRole('complementary', { name: 'DORAIGC 导航' });
    expect(await screen.findByRole('heading', { name: '项目' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '新建项目' })).toBeInTheDocument();
    expect(within(navigation).getByRole('button', { name: '项目' })).toHaveClass('is-active');
    expect(screen.queryByRole('heading', { name: 'Dora Agent - 人人都是艺术大师' })).not.toBeInTheDocument();
  });

  it('protects the direct Owner Skill create route before rendering Builder', async () => {
    window.history.pushState({}, '', '/my/skills/new');
    render(<App />);

    expect(screen.getByRole('heading', { name: '正在确认登录状态' })).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: '请先登录' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '创建 Skill' })).not.toBeInTheDocument();
  });

  it('loads the direct protected Owner Skill list after auth bootstrap', async () => {
    window.history.pushState({}, '', '/my/skills');
    vi.stubGlobal('fetch', mockAppFetch({ authenticatedBootstrap: true }));
    render(<App />);

    expect(await screen.findByText('剧情短片 Skill')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '草稿' })).toBeInTheDocument();
    expect(within(screen.getByRole('complementary', { name: 'DORAIGC 导航' }))
      .getByRole('button', { name: '我的 Skill' })).toHaveClass('is-active');
  });

  it('loads a valid direct protected Owner Skill edit route', async () => {
    window.history.pushState({}, '', `/my/skills/${SKILL_IDS.skill}/edit`);
    vi.stubGlobal('fetch', mockAppFetch({ authenticatedBootstrap: true }));
    render(<App />);

    expect(await screen.findByRole('heading', { name: '编辑 Skill 草稿' })).toBeInTheDocument();
    expect(screen.getByLabelText(/Skill 名称/)).toHaveValue('剧情短片 Skill');
    expect(screen.getByRole('button', { name: '保存草稿' })).toBeEnabled();
  });

  it.each([
    [['user'], ['project.read']],
    [['admin'], ['project.read']]
  ])('denies direct Reviewer routes without skill.review and sends zero Reviewer API calls', async (roles, capabilities) => {
    window.history.pushState({}, '', '/admin/skills/reviews');
    const fetchMock = mockReviewerAppFetch({ roles, capabilities });
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: '无 Skill 审核权限' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('不能使用 skill.review');
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/admin/skill-reviews')))
      .toHaveLength(0);
    expect(screen.queryByRole('button', { name: 'Skill 审核' })).not.toBeInTheDocument();
  });

  it('shows Reviewer navigation and loads the exact queue for skill.review', async () => {
    window.history.pushState({}, '', '/admin/skills/reviews');
    const fetchMock = mockReviewerAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: '待审核 Skill' })).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: '剧情短片 Skill' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Skill 审核' })).toHaveClass('is-active');
    const queueCalls = fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/admin/skill-reviews');
    expect(queueCalls).toHaveLength(1);
    expect(new URL(queueCalls[0][0], 'http://localhost').search).toBe('?status=reviewing');
  });

  it.each([
    [200, '无 Skill 审核权限'],
    [503, '认证服务暂不可用']
  ])('re-parses a queue 403 once and converges without an automatic retry (bootstrap %s)', async (retryBootstrapStatus, heading) => {
    window.history.pushState({}, '', '/admin/skills/reviews');
    const fetchMock = mockReviewerAppFetch({ queueStatus: 403, retryBootstrapStatus });
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument();
    const authCalls = fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/auth/session');
    const queueCalls = fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/admin/skill-reviews');
    expect(authCalls).toHaveLength(2);
    expect(queueCalls).toHaveLength(1);
  });

  it.each([
    ['/admin/skills/reviews/'],
    ['/admin/skills/reviews/not-a-uuid'],
    [`/admin/skills/reviews/${SKILL_IDS.review}/`]
  ])('keeps invalid Reviewer admin path %s protected and performs no Reviewer API call', async (path) => {
    window.history.pushState({}, '', path);
    const fetchMock = mockReviewerAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Skill 审核路径无效' })).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/admin/skill-reviews')))
      .toHaveLength(0);
  });

  it.each([
    ['Creator', ['user'], ['project.read'], false, false],
    ['Reviewer', ['skill_reviewer'], ['skill.review'], true, false],
    ['Governor', ['skill_governor'], ['skill.govern'], false, true],
    ['Reviewer + Governor', ['skill_reviewer', 'skill_governor'], ['skill.review', 'skill.govern'], true, true]
  ])('shows exact, independent Reviewer and Governance navigation for %s', async (
    _name,
    roles,
    capabilities,
    canReview,
    canGovern
  ) => {
    const fetchMock = mockGovernanceAppFetch({ roles, capabilities });
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    const navigation = await screen.findByRole('complementary', { name: 'DORAIGC 导航' });
    await screen.findByRole('button', { name: '用户菜单' });
    expect(within(navigation).queryByRole('button', { name: 'Skill 审核' }) !== null).toBe(canReview);
    expect(within(navigation).queryByRole('button', { name: 'Skill 治理' }) !== null).toBe(canGovern);
  });

  it.each([
    [['user'], ['project.read']],
    [['skill_reviewer'], ['skill.review']]
  ])('denies direct Governance routes without skill.govern and sends zero Governance API calls', async (
    roles,
    capabilities
  ) => {
    window.history.pushState({}, '', '/admin/skills/governance');
    const fetchMock = mockGovernanceAppFetch({ roles, capabilities });
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: '无 Skill 治理权限' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('不能使用 skill.govern');
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/admin/skill-governance')))
      .toHaveLength(0);
    expect(screen.queryByRole('button', { name: 'Skill 治理' })).not.toBeInTheDocument();
  });

  it('lets a pure Governor enter Governance without granting Reviewer navigation or API access', async () => {
    window.history.pushState({}, '', '/admin/skills/governance');
    const fetchMock = mockGovernanceAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    const navigation = await screen.findByRole('complementary', { name: 'DORAIGC 导航' });
    expect(within(navigation).getByRole('button', { name: 'Skill 治理' })).toHaveClass('is-active');
    expect(within(navigation).queryByRole('button', { name: 'Skill 审核' })).not.toBeInTheDocument();
    await waitFor(() => expect(fetchMock.mock.calls.filter(([input]) => (
      requestPath(input) === '/api/v1/admin/skill-governance'
    ))).toHaveLength(1));
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/admin/skill-reviews')))
      .toHaveLength(0);
    expect(screen.queryByRole('heading', { name: '无 Skill 治理权限' })).not.toBeInTheDocument();
  });

  it.each([
    [200, '无 Skill 治理权限'],
    [503, '认证服务暂不可用']
  ])('latches a Governance 403 after one authority re-parse (bootstrap %s)', async (
    retryBootstrapStatus,
    heading
  ) => {
    window.history.pushState({}, '', '/admin/skills/governance');
    const fetchMock = mockGovernanceAppFetch({ queueStatus: 403, retryBootstrapStatus });
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/auth/session')).toHaveLength(2);
    expect(fetchMock.mock.calls.filter(([input]) => (
      requestPath(input) === '/api/v1/admin/skill-governance'
    ))).toHaveLength(1);
  });

  it('fails a pure Governor closed on the Reviewer route without calling Reviewer APIs', async () => {
    window.history.pushState({}, '', '/admin/skills/reviews');
    const fetchMock = mockGovernanceAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: '无 Skill 审核权限' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('不能使用 skill.review');
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/admin/skill-reviews')))
      .toHaveLength(0);
  });

  it.each([
    ['/admin/skills/governance/'],
    ['/admin/skills/governance/not-a-uuid'],
    [`/admin/skills/governance/${SKILL_IDS.skill.toUpperCase()}`],
    [`/admin/skills/governance/${SKILL_IDS.skill}/`]
  ])('keeps invalid Governance admin path %s protected and performs no Governance API call', async (path) => {
    window.history.pushState({}, '', path);
    const fetchMock = mockGovernanceAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Skill 治理路径无效' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('规范小写 UUIDv7');
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/admin/skill-governance')))
      .toHaveLength(0);
  });

  it('returns to the protected login state when Owner Skill list reports 401', async () => {
    window.history.pushState({}, '', '/my/skills');
    vi.stubGlobal('fetch', mockAppFetch({ authenticatedBootstrap: true, ownerSkillsUnauthorized: true }));
    render(<App />);

    expect(await screen.findByRole('heading', { name: '请先登录' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '我的 Skill' })).not.toBeInTheDocument();
  });

  it('renders an explicit failure page for an invalid protected edit path', async () => {
    window.history.pushState({}, '', '/my/skills/not-a-uuid/edit');
    vi.stubGlobal('fetch', mockAppFetch({ authenticatedBootstrap: true }));
    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Skill 编辑路径无效' })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('不是有效的 UUIDv7');
  });

  it('redirects the legacy Skill route to the real public market API', async () => {
    window.history.pushState({}, '', '/skill');
    vi.stubGlobal('fetch', mockAppFetch({ authenticatedBootstrap: true }));

    render(<App />);

    const navigation = await screen.findByRole('complementary', { name: 'DORAIGC 导航' });
    expect(await screen.findByRole('heading', { name: 'Skill 市场', level: 2 })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/skills');
    expect(screen.getByRole('button', { name: '创建 Skill' })).toBeInTheDocument();
    expect(await screen.findAllByTestId('skill-market-card')).toHaveLength(1);
    expect(screen.queryByText('塔可夫斯基风格诗意短片')).not.toBeInTheDocument();
    expect(within(navigation).getByRole('button', { name: 'Skill 市场' })).toHaveClass('is-active');
  });

  it('loads a canonical public Skill detail exactly once', async () => {
    window.history.pushState({}, '', `/skills/${SKILL_MARKET_IDS.skill}`);
    const fetchMock = mockAppFetch();
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByRole('heading', { name: '短片提示词助手' })).toBeInTheDocument();
    expect(screen.getByText('公开市场详情。')).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => (
      requestPath(input) === `/api/v1/skill-market/${SKILL_MARKET_IDS.skill}`
    ))).toHaveLength(1);
    expect(screen.getByRole('button', { name: '登录后使用此 Skill' })).toBeInTheDocument();
  });

  it('recovers an anonymous Market preselection after login without creating until explicit submit', async () => {
    window.history.pushState({}, '', `/skills/${SKILL_MARKET_IDS.skill}`);
    const fetchMock = mockAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '登录后使用此 Skill' }));
    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    expect(within(dialog).getByText('短片提示词助手')).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/projects:quick-create')).toHaveLength(0);

    await submitLoginModal(user, dialog);
    expect(await screen.findByLabelText('已预选市场 Skill')).toHaveTextContent('短片提示词助手');
    expect(screen.getByRole('button', { name: '移除市场 Skill 短片提示词助手' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/');
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/projects:quick-create')).toHaveLength(0);

    await user.type(screen.getByPlaceholderText('由一个想法或故事开始...'), '使用公开 Skill 创作');
    await user.click(screen.getByRole('button', { name: '开始创作' }));
    await waitFor(() => expect(fetchMock.mock.calls.filter(([input]) => (
      requestPath(input) === '/api/v1/projects:quick-create'
    ))).toHaveLength(1));
    const quickCall = fetchMock.mock.calls.find(([input]) => requestPath(input) === '/api/v1/projects:quick-create');
    expect(JSON.parse(quickCall[1].body)).toEqual({
      schema_version: 'project_quick_create.v2',
      initial_prompt: '使用公开 Skill 创作',
      enabled_skill_ids: [SKILL_MARKET_IDS.skill]
    });
  });

  it('deduplicates a Market selection and combines it with a refreshed Owner selection in sorted v2 form', async () => {
    window.history.pushState({}, '', `/skills/${SKILL_MARKET_IDS.skill}`);
    const ownerSkill = ownerSkillFixture({
      content_status: 'published',
      has_unpublished_changes: false,
      governance_status: 'active'
    });
    const fetchMock = mockAppFetch({ authenticatedBootstrap: true, ownerSkills: [ownerSkill] });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '使用此 Skill 创作' }));
    expect(await screen.findByLabelText('已预选市场 Skill')).toHaveTextContent('短片提示词助手');
    expect(screen.getByRole('button', { name: 'Skill，已选择 1 个' })).toBeInTheDocument();

    act(() => {
      window.history.pushState({}, '', `/skills/${SKILL_MARKET_IDS.skill}`);
      window.dispatchEvent(new PopStateEvent('popstate'));
    });
    await user.click(await screen.findByRole('button', { name: '使用此 Skill 创作' }));
    const deduplicatedMarketSelection = await screen.findByLabelText('已预选市场 Skill');
    expect(within(deduplicatedMarketSelection).getAllByText('短片提示词助手')).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'Skill，已选择 1 个' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Skill，已选择 1 个' }));
    const picker = screen.getByRole('dialog', { name: '选择 QuickCreate Skill' });
    await user.click(await within(picker).findByRole('checkbox', { name: '选择 剧情短片 Skill' }));
    expect(screen.getByRole('button', { name: 'Skill，已选择 2 个' })).toBeInTheDocument();
    await user.click(within(picker).getByRole('button', { name: '刷新 Skill 列表' }));
    await waitFor(() => expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/skills')).toHaveLength(2));
    expect(screen.getByRole('button', { name: 'Skill，已选择 2 个' })).toBeInTheDocument();
    expect(screen.getByLabelText('已预选市场 Skill')).toHaveTextContent('短片提示词助手');

    await user.type(screen.getByPlaceholderText('由一个想法或故事开始...'), '混合 Skill 创作');
    await user.click(screen.getByRole('button', { name: '开始创作' }));
    await waitFor(() => expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/projects:quick-create')).toHaveLength(1));
    const quickCall = fetchMock.mock.calls.find(([input]) => requestPath(input) === '/api/v1/projects:quick-create');
    expect(JSON.parse(quickCall[1].body)).toEqual({
      schema_version: 'project_quick_create.v2',
      initial_prompt: '混合 Skill 创作',
      enabled_skill_ids: [SKILL_MARKET_IDS.skill, SKILL_IDS.skill]
    });
  });

  it('drops the Market preselection when its login is superseded by a newer authority epoch', async () => {
    window.history.pushState({}, '', `/skills/${SKILL_MARKET_IDS.skill}`);
    const baseFetch = mockAppFetch();
    let resolveLogin;
    const fetchMock = vi.fn((input, options = {}) => {
      const path = requestPath(input);
      const method = options.method || 'GET';
      if (path === '/api/v1/auth/session' && method === 'POST') {
        return new Promise((resolve) => {
          resolveLogin = () => resolve(jsonResponse(mockAuthPayload()));
        });
      }
      return baseFetch(input, options);
    });
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '登录后使用此 Skill' }));
    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    await submitLoginModal(user, dialog);
    await waitFor(() => expect(resolveLogin).toBeTypeOf('function'));

    act(() => window.dispatchEvent(new CustomEvent(AUTH_SESSION_EXPIRED_EVENT, { detail: { status: 401 } })));
    resolveLogin();

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '登录后继续创作' })).not.toBeInTheDocument());
    expect(window.location.pathname).toBe(`/skills/${SKILL_MARKET_IDS.skill}`);
    expect(screen.queryByLabelText('已预选市场 Skill')).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/projects:quick-create')).toHaveLength(0);
  });

  it('drops the pending Market login selection when browser navigation leaves its source flow', async () => {
    window.history.pushState({}, '', `/skills/${SKILL_MARKET_IDS.skill}`);
    const fetchMock = mockAppFetch();
    vi.stubGlobal('fetch', fetchMock);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '登录后使用此 Skill' }));
    expect(screen.getByRole('dialog', { name: '登录后继续创作' })).toBeInTheDocument();

    act(() => {
      window.history.pushState({}, '', '/skills');
      window.dispatchEvent(new PopStateEvent('popstate'));
    });

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '登录后继续创作' })).not.toBeInTheDocument());
    expect(await screen.findByRole('heading', { name: 'Skill 市场', level: 2 })).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input) === '/api/v1/projects:quick-create')).toHaveLength(0);
  });

  it.each([
    '/skills/not-a-uuid',
    `/skills/${SKILL_MARKET_IDS.skill.toUpperCase()}`,
    `/skills/${SKILL_MARKET_IDS.skill.replace(/^0/, '%30')}`,
    `/skills/${SKILL_MARKET_IDS.skill}/`,
    `/skills/${SKILL_MARKET_IDS.skill}/extra`
  ])('renders invalid public Skill path %s without any Market API request', async (path) => {
    window.history.pushState({}, '', path);
    const fetchMock = mockAppFetch();
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'Skill 详情路径无效' })).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([input]) => requestPath(input).startsWith('/api/v1/skill-market')))
      .toHaveLength(0);
  });

  it('redirects the legacy explore route to the home featured works section', async () => {
    window.history.pushState({}, '', '/explore');

    render(<App />);

    await waitFor(() => {
      expect(window.location.pathname).toBe('/');
    });

    const navigation = screen.getByRole('complementary', { name: 'DORAIGC 导航' });
    expect(screen.getByRole('heading', { name: '精选作品' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '精选作品中心' })).not.toBeInTheDocument();
    expect(within(navigation).getByRole('button', { name: '精选作品' })).toHaveClass('is-active');
    expect(within(navigation).getByRole('button', { name: '首页' })).not.toHaveClass('is-active');
  });

  it('keeps URL and page state in sync for client navigation', async () => {
    const user = userEvent.setup();
    render(<App />);

    await loginFromHeader(user);

    await user.click(screen.getByRole('button', { name: '项目' }));
    expect(window.location.pathname).toBe('/projects');
    expect(screen.getByRole('heading', { name: '项目' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '资产库' }));
    expect(window.location.pathname).toBe('/assets');
    expect(screen.getByRole('heading', { name: '资产库' })).toBeInTheDocument();

    window.history.pushState({}, '', '/projects');
    window.dispatchEvent(new Event('popstate'));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '项目' })).toBeInTheDocument();
    });
  });

  it('continues to a private page after login from navigation', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole('button', { name: '快速创作' }));

    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    expect(within(dialog).getByText('进入快速创作')).toBeInTheDocument();

    await submitLoginModal(user, dialog);

    await waitFor(() => expect(window.location.pathname).toBe(`/projects/${WORKSPACE_IDS.project}/workspace`));
    expect(await screen.findByText('工作台已就绪')).toBeInTheDocument();
    expect(screen.getByText(WORKSPACE_IDS.session)).toBeInTheDocument();
  });

  it('navigates through projects and assets pages after login', async () => {
    const user = userEvent.setup();
    render(<App />);

    await loginFromHeader(user);

    await user.click(screen.getByRole('button', { name: '项目' }));
    expect(window.location.pathname).toBe('/projects');
    expect(screen.getByRole('heading', { name: '项目' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '新建项目' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '最近编辑' })).not.toBeInTheDocument();
    expect(screen.getByText('Seedance 2.0 视频制作')).toBeInTheDocument();
    expect(screen.getByText('功能介绍 202606140505')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '资产库' }));
    expect(window.location.pathname).toBe('/assets');
    expect(screen.getByRole('heading', { name: '资产库' })).toBeInTheDocument();
    expect(screen.getByText('生成视频')).toBeInTheDocument();
    expect(screen.getByText('保存失败')).toBeInTheDocument();
  });

  it('renders the standalone workspace route with live AIGC panels', async () => {
    window.history.pushState({}, '', '/workspace');
    const fetchMock = mockAigcFetch();
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByRole('heading', { name: 'AIGC 创作工作台' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '故事板' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '对话' })).toBeInTheDocument();
    expect(screen.queryByRole('complementary', { name: 'DORAIGC 导航' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '上传资产' })).not.toBeInTheDocument();
    expect(await screen.findAllByText('苏寂')).toHaveLength(2);
    expect(screen.getByText('竹林归隐')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('输入创作需求或修改意见...')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/aigc/sessions', expect.objectContaining({ method: 'POST' }));
  });

  it('starts a fresh workspace session from the topbar', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      sessionIDs: ['s1', 's2'],
      messages: [{ id: 'm1', role: 'assistant', content: '旧会话故事板已生成。' }],
      jobs: [{ job_id: 'job-1', session_id: 's1', target_id: 'shot-1', status: 'running' }]
    });
    const storage = mockLocalStorage();
    class MockEventSource {
      static instances = [];

      constructor(url) {
        this.url = url;
        this.addEventListener = vi.fn();
        this.removeEventListener = vi.fn();
        this.close = vi.fn();
        MockEventSource.instances.push(this);
      }
    }
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('localStorage', storage);
    vi.stubGlobal('EventSource', MockEventSource);

    render(<App />);

    expect(await screen.findByText('Session s1')).toBeInTheDocument();
    expect(screen.getByText('旧会话故事板已生成。')).toBeInTheDocument();
    expect(screen.getByText('竹林归隐')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '新会话' }));

    await waitFor(() => {
      expect(screen.getByText('Session s2')).toBeInTheDocument();
    });
    expect(storage.setItem).toHaveBeenLastCalledWith('dora:aigc:demo_session_id', 's2');
    expect(screen.queryByText('旧会话故事板已生成。')).not.toBeInTheDocument();
    expect(screen.queryByText('竹林归隐')).not.toBeInTheDocument();
    expect(screen.getByText('把剧本、风格或 Skill.md 发给我，我会先规划规格和故事板。')).toBeInTheDocument();
    expect(MockEventSource.instances.map((source) => source.url)).toEqual([
      '/api/aigc/sessions/s1/events/stream',
      '/api/aigc/sessions/s2/events/stream'
    ]);
    expect(MockEventSource.instances[0].close).toHaveBeenCalled();
    const sessionCreates = fetchMock.mock.calls.filter(
      ([url, options]) => url === '/api/aigc/sessions' && options?.method === 'POST'
    );
    expect(sessionCreates).toHaveLength(2);
  });

  it('replaces a cached session automatically after local history is deleted', async () => {
    window.history.pushState({}, '', '/workspace');
    const storage = mockLocalStorage();
    storage.setItem('dora:aigc:demo_session_id', 'deleted-session');
    vi.stubGlobal('localStorage', storage);
    const baseFetch = mockAigcFetch({ sessionIDs: ['fresh-session'] });
    const fetchMock = vi.fn((input, options) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/sessions/deleted-session/messages') {
        return Promise.resolve(jsonResponse({ error: 'not found' }, 404));
      }
      return baseFetch(input, options);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('Session fresh-session')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/aigc/sessions/deleted-session/messages', expect.anything());
    expect(storage.removeItem).toHaveBeenCalledWith('dora:aigc:demo_session_id');
    expect(storage.setItem).toHaveBeenLastCalledWith('dora:aigc:demo_session_id', 'fresh-session');
  });

  it('ignores a previous session hydration that completes after switching sessions', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const storage = mockLocalStorage();
    vi.stubGlobal('localStorage', storage);
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);

    let createdSessionCount = 0;
    let resolveOldStoryboard;
    const oldStoryboardResponse = new Promise((resolve) => {
      resolveOldStoryboard = resolve;
    });
    const newStoryboard = {
      ...defaultAigcStoryboard(),
      session_id: 's2',
      shots: [{ ...defaultAigcStoryboard().shots[0], scene_description: '新会话镜头' }]
    };
    const oldStoryboard = {
      ...defaultAigcStoryboard(),
      session_id: 's1',
      shots: [{ ...defaultAigcStoryboard().shots[0], scene_description: '旧会话迟到镜头' }]
    };
    const fetchMock = vi.fn((input, options = {}) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/sessions' && options.method === 'POST') {
        createdSessionCount += 1;
        return Promise.resolve(
          jsonResponse({ id: createdSessionCount === 1 ? 's1' : 's2', user_id: 'demo-user', status: 'active' }, 201)
        );
      }
      if (path === '/api/aigc/sessions/s1/storyboard') {
        return oldStoryboardResponse;
      }
      if (path === '/api/aigc/sessions/s2/storyboard') {
        return Promise.resolve(jsonResponse(newStoryboard));
      }
      if (/\/api\/aigc\/sessions\/(s1|s2)\/assets$/.test(path)) {
        return Promise.resolve(jsonResponse({ assets: [] }));
      }
      if (/\/api\/aigc\/sessions\/(s1|s2)\/jobs$/.test(path)) {
        return Promise.resolve(jsonResponse({ jobs: [] }));
      }
      if (/\/api\/aigc\/sessions\/(s1|s2)\/messages$/.test(path)) {
        return Promise.resolve(jsonResponse({ messages: [] }));
      }
      return Promise.resolve(jsonResponse({ error: 'not found' }, 404));
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('Session s1')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: '新会话' }));
    expect(await screen.findByText('Session s2')).toBeInTheDocument();
    expect(await screen.findByText('新会话镜头')).toBeInTheDocument();

    await act(async () => {
      resolveOldStoryboard(jsonResponse(oldStoryboard));
      await oldStoryboardResponse;
    });

    expect(screen.getByText('新会话镜头')).toBeInTheDocument();
    expect(screen.queryByText('旧会话迟到镜头')).not.toBeInTheDocument();
  });

  it('ignores stale SSE events and lifecycle callbacks from an older session generation', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('localStorage', mockLocalStorage());
    vi.stubGlobal('fetch', mockAigcFetch({ sessionIDs: ['s1', 's2'] }));

    render(<App />);

    expect(await screen.findByText('Session s1')).toBeInTheDocument();
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    const oldSource = DefaultMockEventSource.instances[0];
    const staleOpen = oldSource.onopen;
    const staleError = oldSource.onerror;
    const staleInterrupt = oldSource.listeners['a2ui.interrupt_request'];

    await user.click(screen.getByRole('button', { name: '新会话' }));
    expect(await screen.findByText('Session s2')).toBeInTheDocument();
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(2));

    act(() => {
      staleError();
      staleInterrupt({
        data: JSON.stringify({
          event: 'a2ui.interrupt_request',
          payload: {
            checkpoint_id: 'stale-checkpoint',
            interrupt_id: 'stale-interrupt',
            title: '旧会话确认',
            message: '旧会话事件不应出现',
            actions: []
          }
        })
      });
    });

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.queryByText('旧会话事件不应出现')).not.toBeInTheDocument();

    act(() => {
      DefaultMockEventSource.instances[1].onerror();
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('事件流已断开，正在自动重连。');

    act(() => {
      staleOpen();
    });
    expect(screen.getByRole('alert')).toHaveTextContent('事件流已断开，正在自动重连。');
  });

  it('subscribes to the ready event and clears transport errors after automatic reconnection', async () => {
    window.history.pushState({}, '', '/workspace');
    const fetchMock = mockAigcFetch({ sessionIDs: ['s1'] });
    class MockEventSource {
      static instances = [];

      constructor(url) {
        this.url = url;
        this.listeners = {};
        this.close = vi.fn();
        MockEventSource.instances.push(this);
      }

      addEventListener(eventName, listener) {
        this.listeners[eventName] = listener;
      }

      removeEventListener(eventName) {
        delete this.listeners[eventName];
      }
    }
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('localStorage', mockLocalStorage());
    vi.stubGlobal('EventSource', MockEventSource);

    render(<App />);

    expect(await screen.findByText('Session s1')).toBeInTheDocument();
    await waitFor(() => expect(MockEventSource.instances).toHaveLength(1));
    const source = MockEventSource.instances[0];
    expect(source.listeners).toHaveProperty('a2ui.ready');

    await act(async () => {
      source.onerror();
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('事件流已断开，正在自动重连。');
    expect(source.close).toHaveBeenCalledTimes(1);

    await waitFor(() => expect(MockEventSource.instances).toHaveLength(2));
    const reconnectedSource = MockEventSource.instances[1];
    expect(reconnectedSource.listeners).toHaveProperty('a2ui.ready');

    await act(async () => {
      reconnectedSource.onopen();
    });
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });

  it('keeps application A2UI errors separate from EventSource transport errors', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('localStorage', mockLocalStorage());
    vi.stubGlobal('fetch', mockAigcFetch({ sessionIDs: ['s1'] }));

    render(<App />);

    expect(await screen.findByText('Session s1')).toBeInTheDocument();
    const source = DefaultMockEventSource.instances[0];
    expect(source.listeners).toHaveProperty('a2ui.error');

    act(() => {
      source.emit({
        event: 'a2ui.error',
        payload: { code: 'invalid_a2ui_action_envelope', message: 'Agent 输出协议错误' }
      });
    });

    expect(await screen.findByRole('alert')).toHaveTextContent('Agent 输出协议错误');
    expect(screen.queryByText('事件流已断开，正在自动重连。')).not.toBeInTheDocument();
  });

  it('resumes an agent interrupt through the unified message endpoint', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        {
          event: 'a2ui.interrupt_request',
          payload: {
            scope: 'agent',
            checkpoint_id: 'agent-cp-1',
            interrupt_id: 'interrupt-1',
            title: '确认参考图',
            message: '请确认参考图',
            actions: [{ key: 'confirm_reference_image', label: '确认参考图' }]
          }
        }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成参考图');
    await user.click(screen.getByRole('button', { name: '发送' }));
    await user.click(await screen.findByRole('button', { name: '确认参考图' }));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/messages/resume',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          checkpoint_id: 'agent-cp-1',
          interrupt_id: 'interrupt-1',
          content: '确认参考图',
          data: { approved: true, action_key: 'confirm_reference_image', note: '确认参考图' }
        })
      })
    );
  });

  it('keeps a newer interrupt when an older interrupt resolves on the same checkpoint', async () => {
    window.history.pushState({}, '', '/workspace');
    ensureDefaultEventSource();
    vi.stubGlobal('fetch', mockAigcFetch());

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    const source = DefaultMockEventSource.instances[0];
    act(() => {
      source.emit({ seq: 1, event: 'a2ui.interrupt_request', payload: { checkpoint_id: 'shared-cp', interrupt_id: 'old-interrupt', title: '旧确认', actions: [{ key: 'old', label: '旧操作' }] } });
      source.emit({ seq: 2, event: 'a2ui.interrupt_request', payload: { checkpoint_id: 'shared-cp', interrupt_id: 'new-interrupt', title: '新确认', actions: [{ key: 'new', label: '新操作' }] } });
      source.emit({ seq: 3, event: 'a2ui.interrupt_resolved', payload: { checkpoint_id: 'shared-cp', interrupt_id: 'old-interrupt', status: 'resumed' } });
    });

    expect(await screen.findByRole('button', { name: '新操作' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '旧操作' })).not.toBeInTheDocument();
  });

  it('applies A2UI storyboard patches and tool run updates in the workspace', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        updateCardEvent('storyboard', 'storyboard:s1', {
          patch: {
            next_version: 4,
            ops: [{ op: 'replace', path: '/shots/0/scene_description', value: '竹林夜雨' }]
          }
        }),
        updateCardEvent('tool_runs', 'tool_run:job-1', {
          tool_run: {
            job_id: 'job-1',
            session_id: 's1',
            target_id: 'shot-1',
            status: 'running'
          }
        })
      ],
      jobsAfterMessage: [{ job_id: 'job-1', session_id: 's1', target_id: 'shot-1', status: 'running' }],
      storyboardAfterMessage: {
        id: 'storyboard-1',
        session_id: 's1',
        version: 4,
        status: 'reviewing',
        key_elements: [
          {
            key: 'suji',
            type: 'character',
            name: '苏寂',
            description: '粗布麻衣，鬓角染霜，佩剑覆黑布。',
            status: 'reviewing'
          }
        ],
        shots: [
          {
            shot_id: 'shot-1',
            index: 1,
            duration_sec: 6,
            scene_description: '竹林夜雨',
            camera_design: '冷色自然光，固定长镜头。',
            status: 'generating'
          }
        ],
        audio_layers: [
          {
            layer_id: 'music-1',
            type: 'music',
            description: '悲凉沉郁，尾声渐弱。',
            status: 'draft'
          }
        ]
      }
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '开始生成镜头');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByText('竹林夜雨')).toBeInTheDocument();
    expect(screen.getByText('1 项正在生成 · 0/1 项已完成')).toBeInTheDocument();
    expect(screen.queryByText('shot-1')).not.toBeInTheDocument();
  });

  it('renders one user-facing capability status and keeps internal nodes out of chat', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        updateCardEvent('tool_runs', 'tool_run:call-1', {
          tool_run: {
            tool_call_id: 'call-1',
            tool_key: 'media_generator',
            display_name: 'Media Assets',
            status: 'running',
            summary: '正在为 Shot 01 生成关键帧',
            nodes: [
              { node_key: 'register_assets', display_name: '注册所有元素资产', status: 'succeeded' },
              { node_key: 'generate_assets', display_name: '生成素材', status: 'running' }
            ]
          }
        }),
        updateCardEvent('storyboard', 'storyboard:s1', {
          patch: {
            next_version: 4,
            ops: [{ op: 'replace', path: '/shots/0/scene_description', value: '竹林夜雨' }]
          }
        })
      ],
      storyboardAfterMessage: {
        ...defaultAigcStoryboard(),
        version: 4,
        shots: [
          {
            ...defaultAigcStoryboard().shots[0],
            scene_description: '竹林夜雨'
          }
        ]
      }
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成关键帧');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByText('竹林夜雨')).toBeInTheDocument();
    expect(screen.getByRole('article', { name: '素材生成进度' })).toBeInTheDocument();
    expect(screen.getByText('素材生成正在进行，完成后会自动同步到左侧故事板。')).toBeInTheDocument();
    expect(screen.queryByText('Media Assets')).not.toBeInTheDocument();
    expect(screen.queryByText('正在为 Shot 01 生成关键帧')).not.toBeInTheDocument();
    expect(screen.queryByText('注册所有元素资产')).not.toBeInTheDocument();
  });

  it('collapses provider progress into one high-level media capability card', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        updateCardEvent('tool_runs', 'tool_run:media_generator', {
          tool_run: {
            tool_key: 'media_generator',
            display_name: 'Media Assets',
            status: 'running',
            summary: '正在生成素材',
            nodes: [{ node_key: 'write_the_prompt:call-prompt', display_name: 'Write The Prompt', status: 'succeeded' }]
          }
        }),
        updateCardEvent('tool_runs', 'tool_run:media_generator', {
          tool_run: {
            tool_key: 'media_generator',
            display_name: 'Media Assets',
            status: 'running',
            summary: 'Image2 正在生成产品图',
            nodes: [{ node_key: 'image2:job-1', display_name: 'Image2 生成图片', status: 'running' }]
          }
        })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成素材');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('article', { name: '素材生成进度' })).toBeInTheDocument();
    expect(screen.getAllByText('素材生成')).toHaveLength(1);
    expect(screen.queryByText('Image2 正在生成产品图')).not.toBeInTheDocument();
    expect(screen.queryByText('Write The Prompt')).not.toBeInTheDocument();
    expect(screen.queryByText('Image2 生成图片')).not.toBeInTheDocument();
  });

  it('never renders one chat card per generated audio asset', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('fetch', mockAigcFetch());

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    const source = DefaultMockEventSource.instances[0];
    act(() => {
      source.emit({
        ...updateCardEvent('tool_runs', 'tool_run:generate_media', {
          tool_run: { tool_key: 'generate_media', status: 'running' }
        }),
        seq: 1
      });
      ['bgm', 'voice', 'sfx'].forEach((slot, index) => {
        source.emit({
          ...updateCardEvent('tool_runs', `job:audio-${slot}`, {
            tool_run: {
              job_id: `job-audio-${slot}`,
              tool_key: 'generate_media',
              target_id: 'elem_audio_mix',
              asset_slot: `asset_audio_${slot}`,
              display_name: '音频素材生成',
              status: 'succeeded',
              result_asset_ids: [`asset-audio-${slot}`]
            }
          }),
          seq: index + 2
        });
      });
    });

    expect(await screen.findByRole('article', { name: '素材生成进度' })).toBeInTheDocument();
    expect(screen.getAllByRole('article', { name: /素材生成进度/ })).toHaveLength(1);
    expect(screen.queryByText('音频素材生成')).not.toBeInTheDocument();
    expect(screen.queryByText('elem_audio_mix')).not.toBeInTheDocument();
    expect(screen.queryByText(/asset_audio_/)).not.toBeInTheDocument();
  });

  it('does not revive a stale planning waiting state after its Approval is gone', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('fetch', mockAigcFetch());

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    act(() => {
      DefaultMockEventSource.instances[0].emit({
        ...updateCardEvent('tool_runs', 'tool_run:legacy-spec', {
          tool_run: { tool_key: 'plan_creation_spec', status: 'waiting_user' }
        }),
        seq: 1
      });
      DefaultMockEventSource.instances[0].emit({
        ...updateCardEvent('tool_runs', 'tool_run:generate_media', {
          tool_run: { tool_key: 'generate_media', status: 'running' }
        }),
        seq: 2
      });
    });

    expect(await screen.findByRole('article', { name: '素材生成进度' })).toBeInTheDocument();
    expect(screen.queryByRole('article', { name: '创作规范进度' })).not.toBeInTheDocument();
  });

  it('refreshes storyboard resources from a terminal capability hint without adding detail cards', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    const fetchMock = mockAigcFetch();
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    act(() => {
      DefaultMockEventSource.instances[0].emit({
        ...updateCardEvent('tool_runs', 'tool_run:generate_media', {
          tool_run: { tool_key: 'generate_media', status: 'succeeded' },
          refresh_resources: ['storyboard', 'assets', 'jobs']
        }),
        seq: 1
      });
    });

    await waitFor(() => {
      for (const resource of ['storyboard', 'assets', 'jobs']) {
        const reads = fetchMock.mock.calls.filter(
          ([path, options = {}]) =>
            path === `/api/aigc/sessions/s1/${resource}` && (options.method || 'GET') === 'GET'
        );
        expect(reads.length).toBeGreaterThanOrEqual(2);
      }
    });
    expect(screen.getAllByRole('article', { name: '素材生成进度' })).toHaveLength(1);
    expect(screen.queryByText(/job-|asset-/)).not.toBeInTheDocument();
  });

  it('renders generated shot images after storyboard update hints and asset refresh', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const imageURL = 'https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/keyframe.png';
    const fetchMock = mockAigcFetch({
      assetsAfterMessage: [{ id: 'asset-1', session_id: 's1', kind: 'image', url: imageURL }],
      messageEvents: [
        updateCardEvent('tool_runs', 'tool_run:job-1', {
          tool_run: {
            job_id: 'job-1',
            session_id: 's1',
            target_type: 'shot',
            target_id: 'shot-1',
            status: 'succeeded',
            result_asset_ids: ['asset-1']
          }
        }),
        updateCardEvent('storyboard', 'storyboard:s1', {
          patch: {
            updates: [
              {
                target_type: 'shot',
                target_id: 'shot-1',
                field: 'keyframe_asset_id',
                asset_kind: 'image',
                asset_ids: ['asset-1'],
                status: 'generated'
              }
            ]
          }
        })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成第一镜关键帧');
    await user.click(screen.getByRole('button', { name: '发送' }));

    await waitFor(() => {
      expect(container.querySelector(`.aigc-shot-preview img[src="${imageURL}"]`)).not.toBeNull();
    });
  });

  it('renders A2UI info request forms and submits answers through chat', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        {
          ...appendCardEvent('brief-intake', {
            title: '补充产品信息',
            message: '请补充商品宣传短片的基础信息。',
            submit_label: '提交信息',
            root: 'root',
            components: [
              { id: 'root', component: { Card: { children: ['product', 'selling-points'] } } },
              { id: 'product', component: { TextInput: { key: 'product_name', label: '产品名称/品类', required: true } } },
              {
                id: 'selling-points',
                component: { TextInput: { key: 'core_selling_points', label: '核心卖点', multiline: true, required: true } }
              }
            ]
          })
        }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '开始吧');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('heading', { name: '补充产品信息' })).toBeInTheDocument();
    await user.type(screen.getByLabelText('产品名称/品类'), '智能手表');
    await user.type(screen.getByLabelText('核心卖点'), '长续航，健康监测');
    await user.click(screen.getByRole('button', { name: '提交' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/aigc/sessions/s1/messages',
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('智能手表')
        })
      );
    });
    const submitCall = fetchMock.mock.calls.find(([path, options]) => {
      if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
        return false;
      }
      return JSON.parse(options.body).content === '智能手表\n长续航，健康监测';
    });
    expect(JSON.parse(submitCall[1].body)).toEqual({
      content: '智能手表\n长续航，健康监测',
      ui_source: {
        type: 'a2ui_submit',
        card_id: 'brief-intake:brief-intake-instance'
      }
    });
  });

  it('renders append_card actions and submits text input as direct user text', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        {
          event: 'a2ui.action',
          payload: {
            a2ui_version: '1.0',
            actions: [
              {
                type: 'append_card',
                surface: 'chat',
                card_id: 'brief-card:brief-card-instance',
                card: {
                  kind: 'form',
                  title: '补充产品信息',
                  submit_label: '提交信息',
                  root: 'root',
                  components: [
                    { id: 'root', component: { Card: { children: ['product'] } } },
                    { id: 'product', component: { TextInput: { key: 'product_name', label: '产品名称/品类', required: true } } }
                  ]
                }
              }
            ]
          }
        }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '开始吧');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('heading', { name: '补充产品信息' })).toBeInTheDocument();
    await user.type(screen.getByLabelText('产品名称/品类'), '智能手表');
    await user.click(screen.getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const submitCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        try {
          return JSON.parse(options.body).content === '智能手表';
        } catch {
          return false;
        }
      });
      expect(submitCall).toBeTruthy();
      expect(JSON.parse(submitCall[1].body)).toEqual({
        content: '智能手表',
        ui_source: {
          type: 'a2ui_submit',
          card_id: 'brief-card:brief-card-instance'
        }
      });
    });
    expect(screen.getByText('智能手表')).toBeInTheDocument();
  });

  it('renders core A2UI components for interactive creation review', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        {
          ...appendCardEvent('brief-intake', {
            root: 'root',
            title: '商品信息采集',
            submit_label: '提交选择',
            components: [
              { id: 'root', component: { Card: { children: ['title', 'product', 'style', 'platforms', 'image', 'video', 'steps'] } } },
              { id: 'title', component: { Text: { value: '请确认商品宣传短片配置', usageHint: 'title' } } },
              { id: 'product', component: { TextInput: { key: 'product_name', label: '产品名称', required: true } } },
              {
                id: 'style',
                component: {
                  SingleChoice: {
                    key: 'visual_style',
                    label: '视觉风格',
                    options: [
                      { value: 'tech', label: '高级科技感' },
                      { value: 'warm', label: '温暖生活感' }
                    ]
                  }
                }
              },
              {
                id: 'platforms',
                component: {
                  MultiChoice: {
                    key: 'platforms',
                    label: '投放平台',
                    options: [
                      { value: 'douyin', label: '抖音' },
                      { value: 'xiaohongshu', label: '小红书' }
                    ]
                  }
                }
              },
              {
                id: 'image',
                component: { ImagePreview: { url: 'https://example.com/ref.png', title: '产品参考图', alt: '产品参考图' } }
              },
              {
                id: 'video',
                component: { VideoPreview: { url: 'https://example.com/preview.mp4', title: '视频预览' } }
              },
              {
                id: 'steps',
                component: {
                  VerticalSteps: {
                    steps: [
                      { title: 'Agent 分析', status: 'done' },
                      { title: '资产配置完成', status: 'running' }
                    ]
                  }
                }
              }
            ]
          })
        }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '开始吧');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('heading', { name: '商品信息采集' })).toBeInTheDocument();
    expect(screen.getByLabelText('产品名称')).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: '高级科技感' })).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: '抖音' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '产品参考图' })).toHaveAttribute('src', 'https://example.com/ref.png');
    expect(container.querySelector('video[src="https://example.com/preview.mp4"]')).not.toBeNull();
    expect(screen.getByText('Agent 分析')).toBeInTheDocument();
    expect(screen.getByText('资产配置完成')).toBeInTheDocument();

    await user.type(screen.getByLabelText('产品名称'), '智能手表');
    await user.click(screen.getByRole('radio', { name: '高级科技感' }));
    await user.click(screen.getByRole('checkbox', { name: '抖音' }));
    await user.click(screen.getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const submitCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        return JSON.parse(options.body).content === '高级科技感、抖音、智能手表';
      });
      expect(JSON.parse(submitCall[1].body)).toEqual({
        content: '高级科技感、抖音、智能手表',
        ui_source: {
          type: 'a2ui_submit',
          card_id: 'brief-intake:brief-intake-instance'
        }
      });
    });
  });

  it('rejects unsupported live A2UI versions and mixed envelopes without partially applying cards', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        {
          event: 'a2ui.action',
          payload: {
            a2ui_version: '2.0',
            actions: [
              {
                type: 'append_card',
                surface: 'chat',
                card_id: 'future-card',
                card: {
                  title: '不支持版本的卡片',
                  root: 'root',
                  components: [{ id: 'root', component: { Card: { children: [] } } }]
                }
              }
            ]
          }
        },
        {
          event: 'a2ui.action',
          payload: {
            a2ui_version: '1.0',
            actions: [
              {
                type: 'append_card',
                surface: 'chat',
                card_id: 'mixed-card',
                card: {
                  title: '不能部分渲染的卡片',
                  root: 'root',
                  components: [{ id: 'root', component: { Card: { children: [] } } }]
                }
              },
              { type: 'delete_card', card_id: 'mixed-card' }
            ]
          }
        }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '测试协议门禁');
    await user.click(screen.getByRole('button', { name: '发送' }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/aigc/sessions/s1/messages', expect.anything()));

    expect(screen.queryByRole('heading', { name: '不支持版本的卡片' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '不能部分渲染的卡片' })).not.toBeInTheDocument();
  });

  it('uses the same A2UI version and action gate while restoring history', async () => {
    window.history.pushState({}, '', '/workspace');
    const unsupportedVersion = appendCardEvent('future-history', {
      title: '历史中的未来版本卡片',
      root: 'root',
      components: [{ id: 'root', component: { Card: { children: [] } } }]
    });
    unsupportedVersion.payload.a2ui_version = '2.0';
    const mixedEnvelope = appendCardEvent('mixed-history', {
      title: '历史中的混合动作卡片',
      root: 'root',
      components: [{ id: 'root', component: { Card: { children: [] } } }]
    });
    mixedEnvelope.payload.actions.push({ type: 'delete_card', card_id: 'mixed-history' });
    vi.stubGlobal(
      'fetch',
      mockAigcFetch({
        messages: [
          { id: 'future-history', role: 'assistant', content: JSON.stringify(unsupportedVersion.payload), seq: 1 },
          { id: 'mixed-history', role: 'assistant', content: JSON.stringify(mixedEnvelope.payload), seq: 2 }
        ]
      })
    );

    render(<App />);

    await screen.findByText('Session s1');
    expect(screen.queryByRole('heading', { name: '历史中的未来版本卡片' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '历史中的混合动作卡片' })).not.toBeInTheDocument();
  });

  it('does not partially submit generic A2UI forms with missing required choices or uploads', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      uploadedAsset: {
        id: 'asset-required-1',
        session_id: 's1',
        kind: 'image',
        filename: 'required.png',
        url: 'https://example.com/required.png'
      },
      messageEvents: [
        appendCardEvent('required-form', {
          title: '完整填写创作信息',
          root: 'root',
          components: [
            { id: 'root', component: { Card: { children: ['name', 'style', 'platforms', 'asset'] } } },
            { id: 'name', component: { TextInput: { key: 'name', label: '产品名称', required: true } } },
            {
              id: 'style',
              component: {
                SingleChoice: {
                  key: 'style',
                  label: '视觉风格',
                  required: true,
                  options: [{ value: 'tech', label: '高级科技感' }]
                }
              }
            },
            {
              id: 'platforms',
              component: {
                MultiChoice: {
                  key: 'platforms',
                  label: '投放平台',
                  required: true,
                  options: [{ value: 'douyin', label: '抖音' }]
                }
              }
            },
            {
              id: 'asset',
              component: {
                FileUpload: { key: 'asset', label: '上传参考图', accept: 'image/*', required: true }
              }
            }
          ]
        })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '开始收集信息');
    await user.click(screen.getByRole('button', { name: '发送' }));
    const card = await screen.findByRole('article', { name: '完整填写创作信息' });
    await user.type(within(card).getByLabelText('产品名称'), '智能手表');
    await user.click(within(card).getByRole('button', { name: '提交' }));

    expect(await screen.findByText('请完成必填项：视觉风格、投放平台、上传参考图。')).toBeInTheDocument();
    let messagePosts = fetchMock.mock.calls.filter(
      ([path, options]) => path === '/api/aigc/sessions/s1/messages' && options?.method === 'POST'
    );
    expect(messagePosts).toHaveLength(1);

    await user.click(within(card).getByRole('radio', { name: '高级科技感' }));
    await user.click(within(card).getByRole('checkbox', { name: '抖音' }));
    await user.click(within(card).getByRole('button', { name: '提交' }));
    expect(await screen.findByText('请完成必填项：上传参考图。')).toBeInTheDocument();
    messagePosts = fetchMock.mock.calls.filter(
      ([path, options]) => path === '/api/aigc/sessions/s1/messages' && options?.method === 'POST'
    );
    expect(messagePosts).toHaveLength(1);

    await user.upload(within(card).getByLabelText('上传参考图'), new File(['png'], 'required.png', { type: 'image/png' }));
    expect(await within(card).findByRole('img', { name: 'required.png' })).toBeInTheDocument();
    await user.click(within(card).getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const submitCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        return JSON.parse(options.body).content === '高级科技感、抖音、asset-required-1、智能手表';
      });
      expect(submitCall).toBeTruthy();
    });
  });

  it('keeps capability status monotonic without exposing operation, job, or node identifiers', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('fetch', mockAigcFetch());

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    const source = DefaultMockEventSource.instances[0];
    act(() => {
      source.emit({
        ...updateCardEvent('tool_runs', 'operation:monotonic', {
          status_version: 3,
          operation: {
            operation_id: 'op-monotonic',
            session_id: 's1',
            tool_key: 'generate_media',
            target_id: 'target-monotonic',
            display_name: '单调状态任务',
            status: 'succeeded',
            summary: '最新终态'
          }
        }),
        seq: 1
      });
      source.emit({
        ...updateCardEvent('tool_runs', 'operation:monotonic', {
          status_version: 2,
          operation: {
            operation_id: 'op-monotonic',
            session_id: 's1',
            tool_key: 'generate_media',
            target_id: 'target-monotonic',
            display_name: '单调状态任务',
            status: 'running',
            summary: '过期运行状态'
          }
        }),
        seq: 2
      });
      source.emit({
        ...updateCardEvent('tool_runs', 'tool_run:call-monotonic', {
          tool_run: {
            tool_call_id: 'call-monotonic',
            display_name: '节点单调任务',
            status: 'waiting_jobs',
            status_version: 1
          }
        }),
        seq: 3
      });
      source.emit({
        ...updateCardEvent('tool_runs', 'tool_run:call-monotonic', {
          tool_run: {
            tool_call_id: 'call-monotonic',
            display_name: '节点单调任务',
            nodes: [
              {
                node_key: 'job:node-monotonic',
                display_name: '图片节点',
                status: 'succeeded',
                status_version: 3,
                description: '节点最新终态'
              }
            ]
          }
        }),
        seq: 4
      });
      source.emit({
        ...updateCardEvent('tool_runs', 'tool_run:call-monotonic', {
          tool_run: {
            tool_call_id: 'call-monotonic',
            display_name: '节点单调任务',
            nodes: [
              {
                node_key: 'job:node-monotonic',
                display_name: '图片节点',
                status: 'running',
                status_version: 2,
                description: '过期节点状态'
              }
            ]
          }
        }),
        seq: 5
      });
    });

    const capability = await screen.findByRole('article', { name: '素材生成进度' });
    expect(within(capability).getByText('完成')).toBeInTheDocument();
    expect(screen.queryByText('过期运行状态')).not.toBeInTheDocument();
    expect(screen.queryByText('最新终态')).not.toBeInTheDocument();
    expect(screen.queryByText('target-monotonic')).not.toBeInTheDocument();
    expect(screen.queryByText('节点最新终态')).not.toBeInTheDocument();
    expect(screen.queryByText('过期节点状态')).not.toBeInTheDocument();
  });

  it('keeps a completed assembly job result out of chat asset cards', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal(
      'fetch',
      mockAigcFetch({
        assets: [
          {
            id: 'assembly-manifest-1',
            session_id: 's1',
            kind: 'text',
            mime_type: 'application/json',
            filename: 'local-preview-manifest.json',
            url: '/api/aigc/local-assets/session/local-preview-manifest.json'
          }
        ]
      })
    );

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    act(() => {
      DefaultMockEventSource.instances[0].emit({
        ...updateCardEvent('tool_runs', 'job:assembly-1', {
          tool_run: {
            job_id: 'assembly-1',
            session_id: 's1',
            display_name: '本地成片清单',
            status: 'succeeded',
            status_version: 3,
            result_asset_ids: ['assembly-manifest-1']
          }
        }),
        seq: 1
      });
    });

    expect(await screen.findByText('后台生成任务已完成')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'local-preview-manifest.json' })).not.toBeInTheDocument();
    expect(screen.queryByText('assembly-1')).not.toBeInTheDocument();
  });

  it('keeps a plain-text confirmation on the message API and leaves the pending Approval untouched', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const approvalCard = appendCardEvent(
      'approval',
      {
        root: 'root',
        title: '审核故事板',
        status: 'pending',
        data: { approval_id: 'approval-pending', decision_version: 0 },
        components: [
          { id: 'root', component: { Card: { children: ['decision'] } } },
          {
            id: 'decision',
            component: {
              SingleChoice: {
                key: 'decision',
                label: '审核决定',
                required: true,
                options: [
                  { value: 'approved', label: '确认' },
                  { value: 'rejected', label: '拒绝' }
                ]
              }
            }
          }
        ]
      },
      { card_id: 'approval:approval-pending' }
    );
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm-markdown', role: 'assistant', content: '故事板已生成。\n\n请在系统审核卡中提交决定。', seq: 1 },
        { id: 'm-approval', role: 'assistant', content: JSON.stringify(approvalCard.payload), seq: 2 }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText(/请在系统审核卡中提交决定/)).toBeInTheDocument();
    expect(await screen.findByRole('article', { name: '审核故事板' })).toBeInTheDocument();
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '确认');
    await user.click(screen.getByRole('button', { name: '发送' }));

    await waitFor(() => {
      const messageCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        return JSON.parse(options.body).content === '确认';
      });
      expect(messageCall).toBeTruthy();
    });
    expect(
      fetchMock.mock.calls.filter(
        ([path, options]) => /^\/api\/aigc\/sessions\/s1\/approvals\/[^/]+\/decision$/.test(path) && options?.method === 'POST'
      )
    ).toHaveLength(0);
    expect(screen.getByRole('article', { name: '审核故事板' })).toBeInTheDocument();
  });

  it('renders canonical Approval controls and submits only through the Decision API', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const approvalCard = appendCardEvent(
      'approval',
      {
        root: 'root',
        title: '审核创作规范',
        status: 'pending',
        approval_id: 'approval-canonical',
        decision_version: 4,
        components: [
          { id: 'root', component: { Card: { children: ['details'] } } },
          { id: 'details', component: { Markdown: { value: '请审阅以上规范。' } } }
        ]
      },
      { card_id: 'approval:approval-canonical' }
    );
    const fetchMock = mockAigcFetch({
      messages: [{ id: 'm-approval', role: 'assistant', content: JSON.stringify(approvalCard.payload), seq: 1 }]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    const card = await screen.findByRole('article', { name: '审核创作规范' });
    expect(within(card).getByText('请审阅以上规范。')).toBeInTheDocument();
    expect(within(card).getByRole('radio', { name: '确认' })).toBeInTheDocument();
    expect(within(card).getByRole('radio', { name: '拒绝' })).toBeInTheDocument();
    await user.click(within(card).getByRole('radio', { name: '确认' }));
    await user.click(within(card).getByRole('button', { name: '提交' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/aigc/sessions/s1/approvals/approval-canonical/decision',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            decision: 'approved',
            expected_decision_version: 4,
            idempotency_key: 'approval:approval-canonical:decision:5:approved'
          })
        })
      );
    });
    expect(
      fetchMock.mock.calls.filter(
        ([path, options]) => path === '/api/aigc/sessions/s1/messages' && options?.method === 'POST'
      )
    ).toHaveLength(0);
  });

  it('shows pending approvals as one sequential next step instead of simultaneous cards', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const specApproval = appendCardEvent(
      'approval-spec',
      {
        root: 'root',
        title: '创作规范预览',
        status: 'pending',
        data: {
          approval_id: 'approval-spec',
          artifact_type: 'creation_spec_revision',
          decision_version: 0
        },
        components: [
          { id: 'root', component: { Card: { children: ['details'] } } },
          { id: 'details', component: { Markdown: { value: '创作规范详情' } } }
        ]
      },
      { card_id: 'approval:approval-spec' }
    );
    const storyboardApproval = appendCardEvent(
      'approval-storyboard',
      {
        root: 'root',
        title: '故事板预览',
        status: 'pending',
        data: {
          approval_id: 'approval-storyboard',
          artifact_type: 'storyboard_revision',
          decision_version: 0
        },
        components: [
          { id: 'root', component: { Card: { children: ['details'] } } },
          { id: 'details', component: { Markdown: { value: '故事板详情' } } }
        ]
      },
      { card_id: 'approval:approval-storyboard' }
    );
    vi.stubGlobal(
      'fetch',
      mockAigcFetch({
        messages: [
          { id: 'm-spec', role: 'assistant', content: JSON.stringify(specApproval.payload), seq: 1 },
          { id: 'm-storyboard', role: 'assistant', content: JSON.stringify(storyboardApproval.payload), seq: 2 }
        ]
      })
    );

    render(<App />);

    const specCard = await screen.findByRole('article', { name: '确认创作规范' });
    expect(screen.queryByRole('article', { name: '确认故事板方案' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: '提交' })).toHaveLength(1);
    expect(within(specCard).getByText('查看完整审核内容')).toBeInTheDocument();
    await user.click(within(specCard).getByRole('radio', { name: '确认' }));
    await user.click(within(specCard).getByRole('button', { name: '提交' }));

    expect(await screen.findByRole('article', { name: '确认故事板方案' })).toBeInTheDocument();
    expect(screen.queryByRole('article', { name: '确认创作规范' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: '提交' })).toHaveLength(1);
  });

  it('drops a model spec preview that arrives after the authoritative Approval and keeps it dropped after Decision', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('fetch', mockAigcFetch());

    render(<App />);

    await screen.findByText('Session s1');
    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    const source = DefaultMockEventSource.instances[0];
    act(() => {
      source.emit({
        ...appendCardEvent(
          'approval-spec-live',
          {
            root: 'root',
            title: '创作规范审核',
            status: 'pending',
            data: {
              approval_id: 'approval-spec-live',
              artifact_type: 'creation_spec_revision',
              decision_version: 0
            },
            components: [
              { id: 'root', component: { Card: { children: ['details'] } } },
              { id: 'details', component: { Markdown: { value: '系统审核详情' } } }
            ]
          },
          { card_id: 'approval:approval-spec-live' }
        ),
        seq: 1
      });
      source.emit({
        ...appendCardEvent(
          'spec-review',
          {
            root: 'root',
            title: '规格预览',
            components: [
              { id: 'root', component: { Card: { children: ['details'] } } },
              { id: 'details', component: { Markdown: { value: '模型重复规格预览' } } }
            ]
          },
          { card_id: 'spec-review:duplicate-live' }
        ),
        seq: 2
      });
    });

    const approvalCard = await screen.findByRole('article', { name: '确认创作规范' });
    expect(screen.queryByRole('article', { name: '规格预览' })).not.toBeInTheDocument();
    expect(screen.queryByText('模型重复规格预览')).not.toBeInTheDocument();
    await user.click(within(approvalCard).getByRole('radio', { name: '确认' }));
    await user.click(within(approvalCard).getByRole('button', { name: '提交' }));

    await waitFor(() => expect(screen.queryByRole('article', { name: '确认创作规范' })).not.toBeInTheDocument());
    expect(screen.queryByRole('article', { name: '规格预览' })).not.toBeInTheDocument();
    expect(screen.queryByText('模型重复规格预览')).not.toBeInTheDocument();
  });

  it('replays Approval, model preview, and terminal Decision history without restoring the preview', async () => {
    window.history.pushState({}, '', '/workspace');
    const approvalCard = appendCardEvent(
      'approval-storyboard-history',
      {
        root: 'root',
        title: '故事板审核',
        status: 'pending',
        data: {
          approval_id: 'approval-storyboard-history',
          artifact_type: 'storyboard_revision',
          decision_version: 0
        },
        components: [
          { id: 'root', component: { Card: { children: ['details'] } } },
          { id: 'details', component: { Markdown: { value: '系统故事板审核详情' } } }
        ]
      },
      { card_id: 'approval:approval-storyboard-history' }
    );
    const modelPreview = appendCardEvent(
      'storyboard-preview',
      {
        root: 'root',
        title: '故事板预览',
        components: [
          { id: 'root', component: { Card: { children: ['details'] } } },
          { id: 'details', component: { Markdown: { value: '历史中的重复故事板预览' } } }
        ]
      },
      { card_id: 'storyboard-preview:duplicate-history' }
    );
    const terminalDecision = updateCardEvent('chat', 'approval:approval-storyboard-history', {
      status: 'approved',
      data: { approval_id: 'approval-storyboard-history' }
    });
    vi.stubGlobal(
      'fetch',
      mockAigcFetch({
        messages: [
          { id: 'm-approval-history', role: 'assistant', content: JSON.stringify(approvalCard.payload), seq: 1 },
          { id: 'm-preview-history', role: 'assistant', content: JSON.stringify(modelPreview.payload), seq: 2 },
          { id: 'm-decision-history', role: 'assistant', content: JSON.stringify(terminalDecision.payload), seq: 3 }
        ]
      })
    );

    render(<App />);

    await screen.findByText('Session s1');
    expect(screen.queryByRole('article', { name: '确认故事板方案' })).not.toBeInTheDocument();
    expect(screen.queryByRole('article', { name: '故事板预览' })).not.toBeInTheDocument();
    expect(screen.queryByText('历史中的重复故事板预览')).not.toBeInTheDocument();
  });

  it('blocks an approval-like A2UI form that has no approval_id', async () => {
    window.history.pushState({}, '', '/workspace');
    const malformedCard = appendCardEvent('approval-missing-id', {
      root: 'root',
      title: '无效审核入口',
      status: 'pending',
      components: [
        { id: 'root', component: { Card: { children: ['decision'] } } },
        {
          id: 'decision',
          component: {
            SingleChoice: {
              key: 'decision',
              label: '审核决定',
              required: true,
              options: [
                { value: 'approved', label: '确认' },
                { value: 'rejected', label: '拒绝' }
              ]
            }
          }
        }
      ]
    });
    const fetchMock = mockAigcFetch({
      messages: [{ id: 'm-malformed', role: 'assistant', content: JSON.stringify(malformedCard.payload), seq: 1 }]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    const card = await screen.findByRole('article', { name: '无效审核入口' });
    expect(within(card).getByRole('alert')).toHaveTextContent('审批卡缺少 approval_id，无法提交');
    expect(within(card).queryByRole('radio', { name: '确认' })).not.toBeInTheDocument();
    expect(within(card).queryByRole('button', { name: '提交' })).not.toBeInTheDocument();
    expect(
      fetchMock.mock.calls.filter(
        ([path, options]) =>
          options?.method === 'POST' &&
          (path === '/api/aigc/sessions/s1/messages' ||
            /^\/api\/aigc\/sessions\/s1\/approvals\/[^/]+\/decision$/.test(path))
      )
    ).toHaveLength(0);
  });

  it('reuses the frozen approval version and idempotency key after a transport failure', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const approvalCard = appendCardEvent(
      'approval',
      {
        root: 'root',
        title: '审核创作规范',
        status: 'pending',
        data: { approval_id: 'approval-1', decision_version: 0 },
        components: [
          { id: 'root', component: { Card: { children: ['decision'] } } },
          {
            id: 'decision',
            component: {
              SingleChoice: {
                key: 'decision',
                label: '审核决定',
                required: true,
                options: [
                  { value: 'approved', label: '确认' },
                  { value: 'rejected', label: '拒绝' }
                ]
              }
            }
          }
        ]
      },
      { card_id: 'approval:approval-1' }
    );
    const fetchMock = mockAigcFetch({ messageEvents: [approvalCard], approvalDecisionFailures: 1 });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成创作规范');
    await user.click(screen.getByRole('button', { name: '发送' }));

    const card = await screen.findByRole('article', { name: '审核创作规范' });
    await user.click(within(card).getByRole('radio', { name: '确认' }));
    await user.click(within(card).getByRole('button', { name: '提交' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('temporary approval failure');

    await user.click(within(card).getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const calls = fetchMock.mock.calls.filter(
        ([path, options]) => path === '/api/aigc/sessions/s1/approvals/approval-1/decision' && options?.method === 'POST'
      );
      expect(calls).toHaveLength(2);
      const bodies = calls.map(([, options]) => JSON.parse(options.body));
      expect(bodies[0]).toEqual({
        decision: 'approved',
        expected_decision_version: 0,
        idempotency_key: 'approval:approval-1:decision:1:approved'
      });
      expect(bodies[1]).toEqual(bodies[0]);
    });
  });

  it('does not reuse an approval request when the decision changes after a failure', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const approvalCard = appendCardEvent(
      'approval',
      {
        root: 'root',
        title: '审核分镜方案',
        status: 'pending',
        data: { approval_id: 'approval-change', decision_version: 2 },
        components: [
          { id: 'root', component: { Card: { children: ['decision'] } } },
          {
            id: 'decision',
            component: {
              SingleChoice: {
                key: 'decision',
                label: '审核决定',
                required: true,
                options: [
                  { value: 'approved', label: '确认' },
                  { value: 'rejected', label: '拒绝' }
                ]
              }
            }
          }
        ]
      },
      { card_id: 'approval:approval-change' }
    );
    const fetchMock = mockAigcFetch({ messageEvents: [approvalCard], approvalDecisionFailures: 1 });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成分镜方案');
    await user.click(screen.getByRole('button', { name: '发送' }));

    const card = await screen.findByRole('article', { name: '审核分镜方案' });
    await user.click(within(card).getByRole('radio', { name: '确认' }));
    await user.click(within(card).getByRole('button', { name: '提交' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('temporary approval failure');

    await user.click(within(card).getByRole('radio', { name: '拒绝' }));
    await user.click(within(card).getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const calls = fetchMock.mock.calls.filter(
        ([path, options]) => path === '/api/aigc/sessions/s1/approvals/approval-change/decision' && options?.method === 'POST'
      );
      expect(calls).toHaveLength(2);
      expect(calls.map(([, options]) => JSON.parse(options.body))).toEqual([
        {
          decision: 'approved',
          expected_decision_version: 2,
          idempotency_key: 'approval:approval-change:decision:3:approved'
        },
        {
          decision: 'rejected',
          expected_decision_version: 2,
          idempotency_key: 'approval:approval-change:decision:3:rejected'
        }
      ]);
    });
    expect(await screen.findByText('已拒绝审核结果，可继续提出修改要求。')).toBeInTheDocument();
  });

  it('keeps a terminal approval removed when slower message hydration replays its append', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('localStorage', mockLocalStorage());

    const historicApproval = appendCardEvent(
      'approval',
      {
        root: 'root',
        title: '不可复活的审核卡',
        status: 'pending',
        data: { approval_id: 'approval-terminal' },
        components: [{ id: 'root', component: { Card: { children: [] } } }]
      },
      { card_id: 'approval:approval-terminal' }
    );
    let resolveMessages;
    const messagesResponse = new Promise((resolve) => {
      resolveMessages = resolve;
    });
    const fetchMock = vi.fn((input, options = {}) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/sessions' && options.method === 'POST') {
        return Promise.resolve(jsonResponse({ id: 's1', user_id: 'demo-user', status: 'active' }, 201));
      }
      if (path === '/api/aigc/sessions/s1/storyboard') {
        return Promise.resolve(jsonResponse(defaultAigcStoryboard()));
      }
      if (path === '/api/aigc/sessions/s1/messages') {
        return messagesResponse;
      }
      if (path === '/api/aigc/sessions/s1/assets') {
        return Promise.resolve(jsonResponse({ assets: [] }));
      }
      if (path === '/api/aigc/sessions/s1/jobs') {
        return Promise.resolve(jsonResponse({ jobs: [] }));
      }
      return Promise.resolve(jsonResponse({ error: 'not found' }, 404));
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    act(() => {
      DefaultMockEventSource.instances[0].emit(historicApproval);
    });
    expect(await screen.findByRole('heading', { name: '不可复活的审核卡' })).toBeInTheDocument();

    act(() => {
      DefaultMockEventSource.instances[0].emit(
        updateCardEvent('chat', 'approval:approval-terminal', { status: 'approved' })
      );
    });
    await waitFor(() => expect(screen.queryByRole('heading', { name: '不可复活的审核卡' })).not.toBeInTheDocument());

    await act(async () => {
      resolveMessages(
        jsonResponse({
          messages: [{ id: 'old-approval', role: 'assistant', content: JSON.stringify(historicApproval.payload), seq: 1 }]
        })
      );
      await messagesResponse;
    });

    expect(screen.queryByRole('heading', { name: '不可复活的审核卡' })).not.toBeInTheDocument();
  });

  it('keeps newer SSE and local resource state when slower hydration completes', async () => {
    window.history.pushState({}, '', '/workspace');
    DefaultMockEventSource.instances = [];
    vi.stubGlobal('EventSource', DefaultMockEventSource);
    vi.stubGlobal('localStorage', mockLocalStorage());

    let resolveBoard;
    let resolveAssets;
    let resolveJobs;
    let resolveMessages;
    const boardResponse = new Promise((resolve) => {
      resolveBoard = resolve;
    });
    const messagesResponse = new Promise((resolve) => {
      resolveMessages = resolve;
    });
    const assetsResponse = new Promise((resolve) => {
      resolveAssets = resolve;
    });
    const jobsResponse = new Promise((resolve) => {
      resolveJobs = resolve;
    });
    const fetchMock = vi.fn((input, options = {}) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/sessions' && options.method === 'POST') {
        return Promise.resolve(jsonResponse({ id: 's1', user_id: 'demo-user', status: 'active' }, 201));
      }
      if (path === '/api/aigc/sessions/s1/storyboard') {
        return boardResponse;
      }
      if (path === '/api/aigc/sessions/s1/messages') {
        return messagesResponse;
      }
      if (path === '/api/aigc/sessions/s1/assets') {
        return assetsResponse;
      }
      if (path === '/api/aigc/sessions/s1/jobs') {
        return jobsResponse;
      }
      return Promise.resolve(jsonResponse({ error: 'not found' }, 404));
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = render(<App />);

    await waitFor(() => expect(DefaultMockEventSource.instances).toHaveLength(1));
    const liveStoryboard = {
      ...defaultAigcStoryboard(),
      version: 4,
      shots: [
        {
          ...defaultAigcStoryboard().shots[0],
          scene_description: '实时镜头版本',
          keyframe_asset_id: 'asset-live'
        }
      ]
    };
    act(() => {
      DefaultMockEventSource.instances[0].emit(
        updateCardEvent('storyboard', 'storyboard:s1', {
          storyboard: liveStoryboard,
          assets: [{ id: 'asset-live', session_id: 's1', kind: 'image', url: 'https://example.com/live.png' }]
        })
      );
      DefaultMockEventSource.instances[0].emit(
        updateCardEvent('tool_runs', 'tool-run:live', {
          tool_run: {
            job_id: 'job-live',
            session_id: 's1',
            target_id: '实时任务目标',
            display_name: '实时生成任务',
            status: 'running'
          }
        })
      );
      DefaultMockEventSource.instances[0].emit(
        appendCardEvent(
          'approval',
          {
            root: 'root',
            title: '实时审核卡',
            data: { approval_id: 'approval-live' },
            components: [{ id: 'root', component: { Card: { children: [] } } }]
          },
          { card_id: 'approval:approval-live' }
        )
      );
      DefaultMockEventSource.instances[0].emit({
        event: 'a2ui.interrupt_request',
        payload: {
          checkpoint_id: 'live-checkpoint',
          interrupt_id: 'live-interrupt',
          title: '实时确认',
          message: '实时确认消息',
          actions: []
        }
      });
    });
    expect(await screen.findByText('实时镜头版本')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '实时审核卡' })).toBeInTheDocument();
    expect(screen.getByText('1 项正在生成 · 0/1 项已完成')).toBeInTheDocument();
    expect(screen.queryByText('实时任务目标')).not.toBeInTheDocument();
    expect(screen.getAllByText('实时确认消息').length).toBeGreaterThanOrEqual(1);
    expect(container.querySelector('img[src="https://example.com/live.png"]')).not.toBeNull();

    await act(async () => {
      resolveBoard(jsonResponse(defaultAigcStoryboard()));
      resolveAssets(
        jsonResponse({
          assets: [{ id: 'asset-stale', session_id: 's1', kind: 'image', url: 'https://example.com/stale.png' }]
        })
      );
      resolveJobs(jsonResponse({ jobs: [{ job_id: 'job-stale', session_id: 's1', target_id: '过期任务目标', status: 'failed' }] }));
      resolveMessages(jsonResponse({ messages: [{ id: 'history-1', role: 'user', content: '过期历史消息', seq: 1 }] }));
      await Promise.all([boardResponse, assetsResponse, jobsResponse, messagesResponse]);
    });

    expect(screen.getByText('实时镜头版本')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '实时审核卡' })).toBeInTheDocument();
    expect(screen.getByText('1 项正在生成 · 0/1 项已完成')).toBeInTheDocument();
    expect(screen.queryByText('实时任务目标')).not.toBeInTheDocument();
    expect(screen.queryByText('过期任务目标')).not.toBeInTheDocument();
    expect(screen.getAllByText('实时确认消息').length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText('过期历史消息')).not.toBeInTheDocument();
    expect(container.querySelector('img[src="https://example.com/live.png"]')).not.toBeNull();
    expect(container.querySelector('img[src="https://example.com/stale.png"]')).toBeNull();
  });

  it('keeps the newest same-session refresh when an older refresh finishes last', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    vi.stubGlobal('localStorage', mockLocalStorage());
    ensureDefaultEventSource();

    let jobsRequestCount = 0;
    let resolveOlderJobs;
    const olderJobsResponse = new Promise((resolve) => {
      resolveOlderJobs = resolve;
    });
    const fetchMock = vi.fn((input, options = {}) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/sessions' && options.method === 'POST') {
        return Promise.resolve(jsonResponse({ id: 's1', user_id: 'demo-user', status: 'active' }, 201));
      }
      if (path === '/api/aigc/sessions/s1/storyboard') {
        return Promise.resolve(jsonResponse(defaultAigcStoryboard()));
      }
      if (path === '/api/aigc/sessions/s1/assets') {
        return Promise.resolve(jsonResponse({ assets: [] }));
      }
      if (path === '/api/aigc/sessions/s1/messages') {
        return Promise.resolve(jsonResponse({ messages: [] }));
      }
      if (path === '/api/aigc/sessions/s1/jobs') {
        jobsRequestCount += 1;
        if (jobsRequestCount === 1) {
          return Promise.resolve(jsonResponse({ jobs: [] }));
        }
        if (jobsRequestCount === 2) {
          return olderJobsResponse;
        }
        return Promise.resolve(
          jsonResponse({ jobs: [{ job_id: 'job-newer', session_id: 's1', target_id: '最新刷新任务', status: 'running' }] })
        );
      }
      return Promise.resolve(jsonResponse({ error: 'not found' }, 404));
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    const refresh = screen.getByRole('button', { name: '刷新' });
    await user.click(refresh);
    await user.click(refresh);
    expect(await screen.findByText('1 项正在生成 · 0/1 项已完成')).toBeInTheDocument();
    expect(screen.queryByText('最新刷新任务')).not.toBeInTheDocument();

    await act(async () => {
      resolveOlderJobs(
        jsonResponse({ jobs: [{ job_id: 'job-older', session_id: 's1', target_id: '过期刷新任务', status: 'failed' }] })
      );
      await olderJobsResponse;
    });

    expect(screen.getByText('1 项正在生成 · 0/1 项已完成')).toBeInTheDocument();
    expect(screen.queryByText('最新刷新任务')).not.toBeInTheDocument();
    expect(screen.queryByText('过期刷新任务')).not.toBeInTheDocument();
  });

  it('keeps prompt review enabled but disables media actions for a pending storyboard', async () => {
    window.history.pushState({}, '', '/workspace');
    const fetchMock = mockAigcFetch({
      storyboard: pendingDynamicStoryboard(),
      assets: [
        { id: 'candidate-image', session_id: 's1', kind: 'image', availability: 'available', filename: 'candidate.png' },
        { id: 'existing-image', session_id: 's1', kind: 'image', availability: 'available', filename: 'existing.png' }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('故事板方案待审核，请先确认或拒绝后再生成、采用或填入素材。')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'cinematic pending frame' })).toBeEnabled();
    expect(screen.getByRole('button', { name: '采用候选' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '重新生成关键帧' })).toBeDisabled();
    expect(screen.getByRole('combobox', { name: '为关键帧选择已有素材' })).toBeDisabled();
  });

  it('keeps candidate approval cards out of chat while related generation jobs are still running', async () => {
    window.history.pushState({}, '', '/workspace');
    const storyboard = candidateReviewStoryboard();
    const fetchMock = mockAigcFetch({
      storyboard,
      assets: [{ id: 'candidate-image', session_id: 's1', kind: 'image', availability: 'available', filename: 'candidate.png' }],
      jobs: [
        {
          id: 'job-candidate',
          batch_id: 'batch-media',
          operation_id: 'operation-media',
          storyboard_id: storyboard.id,
          target_id: 'scene-1',
          asset_slot: 'keyframe',
          status: 'succeeded',
          result_asset_ids: ['candidate-image']
        },
        {
          id: 'job-related',
          batch_id: 'batch-media',
          operation_id: 'operation-media',
          storyboard_id: storyboard.id,
          target_id: 'scene-2',
          asset_slot: 'video',
          status: 'running'
        }
      ],
      messages: [candidateAssetApprovalMessage()]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('待统一确认')).toBeInTheDocument();
    expect(screen.queryByRole('article', { name: '请选择生成候选' })).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: '统一素材确认' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '确认并采用全部素材' })).not.toBeInTheDocument();
  });

  it('confirms all candidate assets once after their related jobs finish and refreshes workspace resources', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const storyboard = candidateReviewStoryboard();
    const approvedStoryboard = JSON.parse(JSON.stringify(storyboard));
    approvedStoryboard.version = storyboard.version + 1;
    approvedStoryboard.bindings[0] = { ...approvedStoryboard.bindings[0], state: 'active', approval_id: '' };
    approvedStoryboard.revisions[0].modules[0].elements[0].asset_slots[0] = {
      ...approvedStoryboard.revisions[0].modules[0].elements[0].asset_slots[0],
      status: 'active',
      active_binding_id: 'binding-candidate',
      candidate_ids: []
    };
    const fetchMock = mockAigcFetch({
      storyboard,
      assets: [{ id: 'candidate-image', session_id: 's1', kind: 'image', availability: 'available', filename: 'candidate.png' }],
      jobs: [
        {
          id: 'job-candidate',
          batch_id: 'batch-media',
          operation_id: 'operation-media',
          storyboard_id: storyboard.id,
          target_id: 'scene-1',
          asset_slot: 'keyframe',
          status: 'succeeded',
          result_asset_ids: ['candidate-image']
        },
        {
          id: 'job-related',
          batch_id: 'batch-media',
          operation_id: 'operation-media',
          storyboard_id: storyboard.id,
          target_id: 'scene-2',
          asset_slot: 'video',
          status: 'failed'
        }
      ],
      messages: [candidateAssetApprovalMessage()],
      candidateApprovalDecision: {
        batch: { id: 'candidate-batch-1', idempotency_key: 'candidate-approvals:storyboard-1:v2:approved' },
        summary: { total: 1, approved: 1, complete: true, all_approved: true },
        results: [{ approval_id: 'approval-candidate', binding_id: 'binding-candidate', status: 'approved', applied: true }],
        storyboard: approvedStoryboard
      }
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    const review = await screen.findByRole('region', { name: '统一素材确认' });
    expect(within(review).getByText('1 项待确认')).toBeInTheDocument();
    expect(screen.queryByRole('article', { name: '请选择生成候选' })).not.toBeInTheDocument();
    await user.click(within(review).getByRole('button', { name: '确认并采用全部素材' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/aigc/sessions/s1/storyboards/storyboard-1/candidate-approvals/decision',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            decision: 'approved',
            expected_storyboard_version: 2,
            idempotency_key: 'candidate-approvals:storyboard-1:v2:approved'
          })
        })
      );
    });
    await waitFor(() => expect(screen.queryByRole('region', { name: '统一素材确认' })).not.toBeInTheDocument());
    for (const suffix of ['storyboard', 'assets', 'jobs']) {
      const reads = fetchMock.mock.calls.filter(
        ([path, options = {}]) => path === `/api/aigc/sessions/s1/${suffix}` && (options.method || 'GET') === 'GET'
      );
      expect(reads.length).toBeGreaterThanOrEqual(2);
    }
  });

  it('renders dynamic timeline capabilities, revision dependencies, and zero-value content', async () => {
    window.history.pushState({}, '', '/workspace');
    const storyboard = pendingDynamicStoryboard();
    const revision = storyboard.revisions[0];
    revision.modules[0].capabilities.has_timeline = true;
    revision.modules[0].elements[0].content = { start_sec: 0, muted: false };
    revision.dependencies = [{ from_target_id: 'source', from_slot: 'image', to_target_id: 'scene-1', to_slot: 'keyframe', relation: 'reference' }];
    vi.stubGlobal('fetch', mockAigcFetch({ storyboard }));

    render(<App />);

    expect(await screen.findByText('时间线 · 1/1')).toBeInTheDocument();
    expect(screen.getByText('start_sec: 0；muted: false')).toBeInTheDocument();
    expect(screen.getByText('依赖：source:image → scene-1:keyframe（reference）')).toBeInTheDocument();
  });

  it('renders Markdown A2UI components as formatted content', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        appendCardEvent('spec-preview', {
            root: 'root',
            title: '规格预览',
            components: [
              { id: 'root', component: { Card: { children: ['summary'] } } },
              {
                id: 'summary',
                component: {
                  Markdown: {
                    value:
                      '# Final Video Spec\n\n- **标题**：竹林归隐\n- `比例`：9:16\n\n[查看资产](https://example.com/asset)\n\n<script>alert(1)</script>'
                  }
                }
              }
            ]
          })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '展示规格');
    await user.click(screen.getByRole('button', { name: '发送' }));

    const card = await screen.findByRole('article', { name: '规格预览' });
    expect(within(card).getByRole('heading', { name: 'Final Video Spec' })).toBeInTheDocument();
    expect(within(card).getByText('标题')).toHaveClass('aigc-a2ui-markdown__strong');
    expect(within(card).getByText('比例')).toHaveClass('aigc-a2ui-markdown__code');
    expect(within(card).getByRole('link', { name: '查看资产' })).toHaveAttribute('href', 'https://example.com/asset');
    expect(within(card).queryByText(/\*\*标题\*\*/)).not.toBeInTheDocument();
    expect(container.querySelector('.aigc-a2ui-markdown script')).toBeNull();
  });

  it('keeps an A2UI-rendered assistant response out of the plain message list after stream refresh', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const answer = '我的核心能力已整理。';
    const cardEvent = appendCardEvent('chat-s1', {
      root: 'root',
      title: 'Agent',
      components: [
        { id: 'root', component: { Card: { children: ['answer'] } } },
        { id: 'answer', component: { Markdown: { value: answer } } }
      ]
    });
    const fetchMock = mockAigcFetch({
      messageEvents: [cardEvent],
      messagesAfterMessage: [
        { id: 'm-user', role: 'user', content: '你有哪些能力' },
        { id: 'm-assistant', role: 'assistant', content: JSON.stringify(cardEvent.payload) }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '你有哪些能力');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByText(answer)).toBeInTheDocument();
    const assistantBubbleText = Array.from(container.querySelectorAll('.aigc-message--assistant p')).map((node) => node.textContent || '');
    expect(assistantBubbleText.some((text) => text.includes(answer))).toBe(false);
  });

  it('restores persisted A2UI assistant cards from message history after refresh', async () => {
    window.history.pushState({}, '', '/workspace');
    const cardEvent = appendCardEvent('history-card', {
      card_type: 'skill_select',
      root: 'root',
      title: '可用能力',
      components: [
        { id: 'root', component: { Card: { children: ['title', 'skill'] } } },
        { id: 'title', component: { Text: { value: '请选择能力', usageHint: 'title' } } },
        {
          id: 'skill',
          component: {
            SingleChoice: {
              key: 'skill_id',
              label: '可用能力',
              options: [{ value: 'product-video', label: '商品宣传短片' }]
            }
          }
        }
      ]
    });
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm-user', role: 'user', content: '你有哪些能力', seq: 1 },
        { id: 'm-a2ui', role: 'assistant', content: JSON.stringify(cardEvent.payload), seq: 2 }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container } = render(<App />);

    expect(await screen.findByText('你有哪些能力')).toBeInTheDocument();
    const card = await screen.findByRole('article', { name: '可用能力' });
    expect(within(card).getByText('请选择能力')).toBeInTheDocument();
    expect(within(card).getByLabelText('商品宣传短片')).toBeInTheDocument();
    expect(container.textContent).not.toContain('a2ui_version');
  });

  it('replays update_card payload fields with the same semantics as live events', async () => {
    window.history.pushState({}, '', '/workspace');
    const appended = appendCardEvent(
      'history-progress',
      {
        root: 'root',
        title: '历史任务进度',
        status: 'running',
        message: '旧进度',
        components: [{ id: 'root', component: { Card: { children: [] } } }]
      },
      { card_id: 'history-progress:instance-1' }
    );
    const updated = updateCardEvent('chat', 'history-progress:instance-1', {
      status: 'completed',
      message: '历史回放已完成'
    });
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'history-append', role: 'assistant', content: JSON.stringify(appended.payload), seq: 1 },
        { id: 'history-update', role: 'assistant', content: JSON.stringify(updated.payload), seq: 2 }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    const card = await screen.findByRole('article', { name: '历史任务进度' });
    expect(within(card).getByText('历史回放已完成')).toBeInTheDocument();
    expect(within(card).getByText('完成')).toBeInTheDocument();
    expect(within(card).queryByText('旧进度')).not.toBeInTheDocument();
  });

  it('keeps A2UI responses in chronological order with later user messages', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const firstAnswer = '第一轮能力说明';
    const secondPrompt = '做个商品宣传片吧';
    const fetchMock = mockAigcFetch({
      messageEvents: [
        appendCardEvent('capability-answer', {
            root: 'root',
            title: 'Agent',
            components: [
              { id: 'root', component: { Card: { children: ['answer'] } } },
              { id: 'answer', component: { Markdown: { value: firstAnswer } } }
            ]
          })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '你有什么能力');
    await user.click(screen.getByRole('button', { name: '发送' }));
    const firstSurface = (await screen.findByText(firstAnswer)).closest('article');

    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), secondPrompt);
    await user.click(screen.getByRole('button', { name: '发送' }));

    const secondUserMessage = screen.getByText(secondPrompt).closest('.aigc-message');
    expect(firstSurface.compareDocumentPosition(secondUserMessage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('renders skill-list A2UI choice surfaces without duplicating the persisted text prompt', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm1', role: 'user', content: '你有哪些Skill' }
      ],
      messageEvents: [
        {
          ...appendCardEvent('skill-selection', {
            root: 'root',
            title: '选择 Skill',
            submit_label: '选择 Skill',
            components: [
              { id: 'root', component: { Card: { children: ['title', 'skill'] } } },
              { id: 'title', component: { Text: { value: '请选择要加载的 Skill', usageHint: 'title' } } },
              {
                id: 'skill',
                component: {
                  SingleChoice: {
                    key: 'skill_name',
                    label: 'Skill',
                    required: true,
                    options: [
                      { value: '武侠短片', label: '武侠短片' },
                      { value: '商品宣传短片_v2', label: '商品宣传短片_v2' }
                    ]
                  }
                }
              }
            ]
          })
        }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '你有哪些Skill');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('radio', { name: '武侠短片' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: '商品宣传短片_v2' })).toBeInTheDocument();
    expect(screen.queryByText(/Skill 名称/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\*\*Skill\*\*/)).not.toBeInTheDocument();

    await user.click(screen.getByRole('radio', { name: '商品宣传短片_v2' }));
    await user.click(screen.getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const submitCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        return JSON.parse(options.body).content === '商品宣传短片_v2';
      });
      expect(submitCall).toBeTruthy();
    });
    expect(screen.getAllByText('商品宣传短片_v2').length).toBeGreaterThanOrEqual(1);
  });

  it('removes submitted A2UI choice cards and sends ui_source metadata', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const skillSelection = appendCardEvent('skill-selection', {
      root: 'root',
      title: '选择 Skill',
      components: [
        { id: 'root', component: { Card: { children: ['title', 'skill'] } } },
        { id: 'title', component: { Text: { value: '请选择要加载的 Skill', usageHint: 'title' } } },
        {
          id: 'skill',
          component: {
            SingleChoice: {
              key: 'skill_name',
              label: 'Skill',
              required: true,
              options: [
                { value: '商品宣传短片_v2', label: '商品宣传短片_v2' },
                { value: '武侠短片', label: '武侠短片' }
              ]
            }
          }
        }
      ]
    });
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm1', role: 'user', content: '你有哪些能力', seq: 1 },
        { id: 'm2', role: 'assistant', content: JSON.stringify(skillSelection.payload), seq: 2 }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('请选择要加载的 Skill')).toBeInTheDocument();
    await user.click(screen.getByRole('radio', { name: '商品宣传短片_v2' }));
    await user.click(screen.getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const submitCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        return JSON.parse(options.body).content === '商品宣传短片_v2';
      });
      expect(submitCall).toBeTruthy();
      expect(JSON.parse(submitCall[1].body)).toEqual({
        content: '商品宣传短片_v2',
        ui_source: {
          type: 'a2ui_submit',
          card_id: 'skill-selection:skill-selection-instance'
        }
      });
    });
    expect(screen.queryByText('请选择要加载的 Skill')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '提交' })).not.toBeInTheDocument();
  });

  it('does not restore A2UI cards that already have a submitted user message', async () => {
    window.history.pushState({}, '', '/workspace');
    const firstSkillSelection = appendCardEvent('skill-selection', {
      root: 'root',
      title: '选择 Skill A',
      components: [
        { id: 'root', component: { Card: { children: ['title', 'skill'] } } },
        { id: 'title', component: { Text: { value: '请选择要加载的 Skill A', usageHint: 'title' } } },
        {
          id: 'skill',
          component: {
            SingleChoice: {
              key: 'skill_name',
              label: 'Skill',
              required: true,
              options: [{ value: '商品宣传短片_v2', label: '商品宣传短片_v2' }]
            }
          }
        }
      ]
    }, { instance_id: 'skill-selection-instance-1' });
    const secondSkillSelection = appendCardEvent('skill-selection', {
      root: 'root',
      title: '选择 Skill B',
      components: [
        { id: 'root', component: { Card: { children: ['title', 'skill'] } } },
        { id: 'title', component: { Text: { value: '请选择要加载的 Skill B', usageHint: 'title' } } },
        {
          id: 'skill',
          component: {
            SingleChoice: {
              key: 'skill_name',
              label: 'Skill',
              required: true,
              options: [{ value: '武侠短片', label: '武侠短片' }]
            }
          }
        }
      ]
    }, { instance_id: 'skill-selection-instance-2' });
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm1', role: 'user', content: '你有哪些能力', seq: 1 },
        { id: 'm2', role: 'assistant', content: JSON.stringify(firstSkillSelection.payload), seq: 2 },
        {
          id: 'm3',
          role: 'user',
          content: '商品宣传短片_v2',
          seq: 3,
          metadata: {
            ui_source: {
              type: 'a2ui_submit',
              card_id: 'skill-selection:skill-selection-instance-1'
            }
          }
        },
        { id: 'm4', role: 'assistant', content: JSON.stringify(secondSkillSelection.payload), seq: 4 }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('商品宣传短片_v2')).toBeInTheDocument();
    expect(screen.queryByText('请选择要加载的 Skill A')).not.toBeInTheDocument();
    expect(screen.getByText('请选择要加载的 Skill B')).toBeInTheDocument();
    expect(screen.queryByRole('radio', { name: '商品宣传短片_v2' })).not.toBeInTheDocument();
  });

  it('uploads A2UI files as file ids while rendering file previews instead of raw ids', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      uploadedAsset: {
        id: 'asset-file-1',
        session_id: 's1',
        kind: 'image',
        filename: 'watch.png',
        url: 'https://example.com/watch.png'
      },
      messageEvents: [
        appendCardEvent('asset-intake', {
          root: 'root',
          title: '上传参考素材',
          submit_label: '发送素材',
          components: [
            { id: 'root', component: { Card: { children: ['file'] } } },
            {
              id: 'file',
              component: {
                FileUpload: {
                  key: 'reference_file',
                  label: '上传图片',
                  accept: 'image/*',
                  required: true
                }
              }
            }
          ]
        })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '上传参考图');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('heading', { name: '上传参考素材' })).toBeInTheDocument();
    const file = new File(['pngbytes'], 'watch.png', { type: 'image/png' });
    await user.upload(screen.getByLabelText('上传图片'), file);

    expect(await screen.findByRole('img', { name: 'watch.png' })).toHaveAttribute('src', 'https://example.com/watch.png');
    expect(screen.queryByText('asset-file-1')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '提交' }));

    await waitFor(() => {
      const submitCall = fetchMock.mock.calls.find(([path, options]) => {
        if (path !== '/api/aigc/sessions/s1/messages' || options?.method !== 'POST') {
          return false;
        }
        return JSON.parse(options.body).content === 'asset-file-1';
      });
      expect(submitCall).toBeTruthy();
    });
    expect(screen.getAllByRole('img', { name: 'watch.png' }).length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText('asset-file-1')).not.toBeInTheDocument();
  });

  it('renders action envelopes without showing raw protocol JSON', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageEvents: [
        appendCardEvent('brief-intake', {
            root: 'root',
            title: '补充产品信息',
            submit_label: '提交信息',
            components: [
              { id: 'root', component: { Card: { children: ['title', 'product', 'steps'] } } },
              { id: 'title', component: { Text: { value: '请补充商品宣传短片信息', usageHint: 'title' } } },
              { id: 'product', component: { TextInput: { key: 'product_name', label: '产品名称/品类', required: true } } },
              { id: 'steps', component: { VerticalSteps: { steps: [{ title: 'Agent 分析', status: 'running' }] } } }
            ]
          })
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '开始吧');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByRole('heading', { name: '补充产品信息' })).toBeInTheDocument();
    expect(screen.getByLabelText('产品名称/品类')).toBeInTheDocument();
    expect(screen.getByText('Agent 分析')).toBeInTheDocument();
    expect(screen.queryByText(/a2ui_version/)).not.toBeInTheDocument();
  });

  it('saves inline storyboard edits as a user patch', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      patchedStoryboard: {
        id: 'storyboard-1',
        session_id: 's1',
        version: 4,
        status: 'reviewing',
        key_elements: [
          {
            key: 'suji',
            type: 'character',
            name: '苏寂',
            description: '粗布麻衣，鬓角染霜，佩剑覆黑布。',
            status: 'reviewing'
          }
        ],
        shots: [
          {
            shot_id: 'shot-1',
            index: 1,
            duration_sec: 6,
            scene_description: '山雨欲来',
            camera_design: '冷色自然光，固定长镜头。',
            status: 'draft'
          }
        ],
        audio_layers: [
          {
            layer_id: 'music-1',
            type: 'music',
            description: '悲凉沉郁，尾声渐弱。',
            status: 'draft'
          }
        ]
      }
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await user.click(await screen.findByText('竹林归隐'));
    const editor = screen.getByDisplayValue('竹林归隐');
    await user.clear(editor);
    await user.type(editor, '山雨欲来');
    await user.click(screen.getByRole('button', { name: '保存修改' }));

    expect(await screen.findByText('山雨欲来')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/storyboards/storyboard-1',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({
          base_version: 3,
          source: 'user',
          ops: [{ op: 'replace', path: '/shots/0/scene_description', value: '山雨欲来' }]
        })
      })
    );
  });

  it('does not apply an inline edit response after switching session generations', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    vi.stubGlobal('localStorage', mockLocalStorage());
    const baseFetch = mockAigcFetch({ sessionIDs: ['s1', 's2'] });
    let resolvePatch;
    let patchSignal;
    const patchResponse = new Promise((resolve) => {
      resolvePatch = resolve;
    });
    const fetchMock = vi.fn((input, options = {}) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/sessions/s1/storyboards/storyboard-1' && options.method === 'PATCH') {
        patchSignal = options.signal;
        return patchResponse;
      }
      return baseFetch(input, options);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await user.click(await screen.findByText('竹林归隐'));
    const editor = screen.getByDisplayValue('竹林归隐');
    await user.clear(editor);
    await user.type(editor, '旧会话保存结果');
    await user.click(screen.getByRole('button', { name: '保存修改' }));
    await waitFor(() => expect(patchSignal).toBeDefined());

    await user.click(screen.getByRole('button', { name: '新会话' }));
    expect(await screen.findByText('Session s2')).toBeInTheDocument();
    expect(patchSignal.aborted).toBe(true);

    await act(async () => {
      resolvePatch(
        jsonResponse({
          storyboard: {
            ...defaultAigcStoryboard(),
            shots: [{ ...defaultAigcStoryboard().shots[0], scene_description: '旧会话保存结果' }]
          }
        })
      );
      await patchResponse;
    });

    expect(screen.getByText('Session s2')).toBeInTheDocument();
    expect(screen.queryByText('旧会话保存结果')).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('restores persisted chat messages in the workspace', async () => {
    window.history.pushState({}, '', '/workspace');
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm1', role: 'user', content: '生成一个武侠短片' },
        { id: 'm-tool-call', role: 'assistant', content: '内部规划后调用故事板工具。', tool_calls: 'W3siaWQiOiJjYWxsLTEifV0=' },
        { id: 'm2', role: 'assistant', content: '故事板已生成。' }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('生成一个武侠短片')).toBeInTheDocument();
    expect(screen.getByText('故事板已生成。')).toBeInTheDocument();
    expect(screen.queryByText('内部规划后调用故事板工具。')).not.toBeInTheDocument();
  });

  it('imports a Skill.md file and binds it to the current workspace session', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      createSkill: {
        skill: { id: 'skill-video', name: '武侠短片', description: '完整视频创作。', enabled: true },
        plan: { skill_id: 'skill-video', name: '武侠短片', description: '完整视频创作。', stages: [] }
      }
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    const input = await screen.findByLabelText('导入 Skill.md');
    const file = new File(
      [
        '<name>武侠短片</name>\n<description>完整视频创作。</description>\n<planner>\n1. 编写规格 → ** text_editor **\n</planner>'
      ],
      'Skill.md',
      { type: 'text/markdown' }
    );
    await user.upload(input, file);

    expect(await screen.findByText('已导入 Skill：武侠短片')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/skills',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          content: '<name>武侠短片</name>\n<description>完整视频创作。</description>\n<planner>\n1. 编写规格 → ** text_editor **\n</planner>'
        })
      })
    );
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/skill',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ skill_id: 'skill-video' })
      })
    );
  });

  it('does not bind or report a Skill import after switching session generations', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    vi.stubGlobal('localStorage', mockLocalStorage());
    const baseFetch = mockAigcFetch({ sessionIDs: ['s1', 's2'] });
    let resolveSkill;
    let skillSignal;
    const skillResponse = new Promise((resolve) => {
      resolveSkill = resolve;
    });
    const fetchMock = vi.fn((input, options = {}) => {
      const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
      if (path === '/api/aigc/skills' && options.method === 'POST') {
        skillSignal = options.signal;
        return skillResponse;
      }
      return baseFetch(input, options);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    const input = await screen.findByLabelText('导入 Skill.md');
    await user.upload(input, new File(['<name>迟到 Skill</name>'], 'Skill.md', { type: 'text/markdown' }));
    await waitFor(() => expect(skillSignal).toBeDefined());

    await user.click(screen.getByRole('button', { name: '新会话' }));
    expect(await screen.findByText('Session s2')).toBeInTheDocument();
    expect(skillSignal.aborted).toBe(true);

    await act(async () => {
      resolveSkill(
        jsonResponse({
          skill: { id: 'skill-late', name: '迟到 Skill', enabled: true },
          plan: { skill_id: 'skill-late', name: '迟到 Skill', stages: [] }
        }, 201)
      );
      await skillResponse;
    });

    const staleBindings = fetchMock.mock.calls.filter(
      ([inputValue, options]) =>
        new URL(typeof inputValue === 'string' ? inputValue : inputValue.url, 'http://localhost').pathname ===
          '/api/aigc/sessions/s1/skill' && options?.method === 'POST'
    );
    expect(staleBindings).toHaveLength(0);
    expect(screen.queryByText('已导入 Skill：迟到 Skill')).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('navigates through skills, featured works, and credits mock pages', async () => {
    const user = userEvent.setup();
    render(<App />);

    await loginFromHeader(user);

    await user.click(screen.getByRole('button', { name: 'Skill 市场' }));
    expect(window.location.pathname).toBe('/skills');
    expect(screen.getByRole('heading', { name: 'Skill 市场', level: 2 })).toBeInTheDocument();
    expect(await screen.findAllByTestId('skill-market-card')).toHaveLength(1);

    await user.click(screen.getByRole('button', { name: '精选作品' }));
    expect(window.location.pathname).toBe('/');
    expect(screen.getByRole('heading', { name: '精选作品' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '精选作品中心' })).not.toBeInTheDocument();
    expect(screen.getByText('MV 分镜生成')).toBeInTheDocument();
    expect(within(screen.getByRole('complementary', { name: 'DORAIGC 导航' })).getByRole('button', { name: '精选作品' })).toHaveClass('is-active');

    await user.click(screen.getByRole('button', { name: '—积分' }));
    expect(screen.getByRole('heading', { name: '积分中心' })).toBeInTheDocument();
    expect(screen.getByText('148 积分')).toBeInTheDocument();
    expect(screen.getByText('DORA-2026-CREATOR')).toBeInTheDocument();
  });

  it('keeps write actions on mock pages behind the login intent modal', async () => {
    const user = userEvent.setup();
    render(<App />);

    await loginFromHeader(user);

    await user.click(screen.getByRole('button', { name: '项目' }));
    await user.click(screen.getByRole('button', { name: '继续创作 Seedance 2.0 视频制作' }));

    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    expect(within(dialog).getByText('继续创作 Seedance 2.0 视频制作')).toBeInTheDocument();
    expect(within(dialog).getByText('进入项目后会恢复最近会话和资产上下文。')).toBeInTheDocument();
  });

  it('opens the account menu from the avatar after login', async () => {
    const user = userEvent.setup();
    render(<App />);

    await loginFromHeader(user);
    await user.click(screen.getByRole('button', { name: '用户菜单' }));

    const menu = screen.getByRole('dialog', { name: '账户与积分' });
    expect(menu).toHaveClass('account-menu--compact');
    expect(menu).toHaveClass('account-menu--slim');
    expect(within(menu).getByText('User')).toBeInTheDocument();
    expect(within(menu).getByText('zhuifei2099@gmail.com')).toBeInTheDocument();
    expect(within(menu).getByText('基础版')).toBeInTheDocument();
    expect(within(menu).getByRole('button', { name: '开通会员' })).toHaveClass('membership-button--theme');
    expect(within(menu).getByText('会员积分')).toBeInTheDocument();
    expect(within(menu).getByText('每周积分')).toBeInTheDocument();
    expect(within(menu).getByText('奖励积分')).toBeInTheDocument();
    expect(within(menu).getByRole('button', { name: '查看用量' })).toBeInTheDocument();
    expect(within(menu).getByText('语言')).toBeInTheDocument();
    expect(within(menu).getByText('反馈')).toBeInTheDocument();
    expect(within(menu).getByText('管理账户')).toBeInTheDocument();
    expect(within(menu).getByRole('button', { name: '退出登录' })).toBeInTheDocument();
  });

  it('uses user-facing copy and the same card system on private pages', async () => {
    const user = userEvent.setup();
    render(<App />);

    await loginFromHeader(user);

    await user.click(screen.getByRole('button', { name: '项目' }));
    expect(screen.getAllByTestId('project-card')).toHaveLength(11);
    expect(screen.getByText('创建新项目')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '资产库' }));
    expect(screen.getAllByTestId('content-card')).toHaveLength(3);
    expect(screen.getByText('查看已经生成的图片、视频与音频，快速带回当前创作。')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '上传素材' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Skill 市场' }));
    expect(screen.getByRole('heading', { name: 'Skill 市场', level: 2 })).toBeInTheDocument();
    expect(await screen.findAllByTestId('skill-market-card')).toHaveLength(1);
    expect(screen.getByText(/公开预览与详情页创作预选/)).toBeInTheDocument();
  });
});

async function loginFromHeader(user) {
  await user.click(await screen.findByRole('button', { name: '登录' }));
  const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
  await submitLoginModal(user, dialog);
  await screen.findByRole('button', { name: '用户菜单' });
}

async function submitLoginModal(user, dialog = screen.getByRole('dialog', { name: '登录后继续创作' })) {
  await user.type(within(dialog).getByRole('textbox', { name: '邮箱' }), 'user@example.com');
  await user.type(within(dialog).getByLabelText('密码'), 'test-password');
  await user.click(within(dialog).getByRole('button', { name: '登录并继续' }));
}

function mockAppFetch({
  authenticatedBootstrap = false,
  ownerSkillsUnauthorized = false,
  ownerSkills = [ownerSkillFixture()],
  marketItems = [skillMarketListItemFixture()]
} = {}) {
  let authenticated = authenticatedBootstrap;
  return vi.fn(async (input, options = {}) => {
    const path = new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
    const method = options.method || 'GET';
    if (path === '/api/v1/auth/session' && method === 'GET') {
      return authenticated
        ? jsonResponse(mockAuthPayload())
        : jsonResponse({ error: { code: 'UNAUTHENTICATED', message: '未登录', retryable: false } }, 401);
    }
    if (path === '/api/v1/auth/session' && method === 'POST') {
      authenticated = true;
      return jsonResponse(mockAuthPayload());
    }
    if (path === '/api/v1/auth/session' && method === 'DELETE') {
      authenticated = false;
      return new Response(null, { status: 204 });
    }
    if (path === '/api/v1/skill-market' && method === 'GET') {
      return jsonResponse({
        items: marketItems,
        next_cursor: null,
        request_id: SKILL_MARKET_IDS.request
      });
    }
    if (path === `/api/v1/skill-market/${SKILL_MARKET_IDS.skill}` && method === 'GET') {
      return jsonResponse({
        skill: skillMarketDetailFixture(),
        request_id: SKILL_MARKET_IDS.request
      });
    }
    if (path === '/api/v1/skills' && method === 'GET') {
      if (ownerSkillsUnauthorized) {
        return jsonResponse({
          error: { code: 'UNAUTHENTICATED', message: '会话已过期', retryable: false }
        }, 401);
      }
      return jsonResponse({
        items: ownerSkills,
        next_cursor: null,
        request_id: SKILL_IDS.request
      });
    }
    if (path === `/api/v1/skills/${SKILL_IDS.skill}` && method === 'GET') {
      return jsonResponse({
        skill: ownerSkillFixture(),
        request_id: SKILL_IDS.request
      });
    }
    if (path === '/api/v1/projects:quick-create' && method === 'POST') {
      return jsonResponse({
        project_id: WORKSPACE_IDS.project,
        session_id: null,
        input_id: null,
        creation_status: 'provisioning',
        workspace_ref: `/projects/${WORKSPACE_IDS.project}/workspace`,
        request_id: WORKSPACE_IDS.request
      }, 201);
    }
    if (path === `/api/v1/projects/${WORKSPACE_IDS.project}/bootstrap` && method === 'GET') {
      return jsonResponse(projectBootstrapFixture());
    }
    if (path === `/api/v1/agent/sessions/${WORKSPACE_IDS.session}/workspace` && method === 'GET') {
      return jsonResponse(workspaceSnapshotFixture());
    }
    return jsonResponse({ error: { code: 'NOT_FOUND', message: 'not found', retryable: false } }, 404);
  });
}

function mockAuthPayload() {
  return {
    status: 'authenticated',
    principal: {
      id: 'user-1',
      display_name: 'User',
      email: 'zhuifei2099@gmail.com',
      account_status: 'active',
      roles: ['user'],
      capabilities: ['project.read']
    },
    csrf_token: 'csrf-1',
    session_expires_at: '2026-07-15T08:00:00Z'
  };
}

function mockReviewerAppFetch({
  roles = ['skill_reviewer'],
  capabilities = ['skill.review'],
  queueStatus = 200,
  retryBootstrapStatus = 200
} = {}) {
  let authReads = 0;
  return vi.fn(async (input, options = {}) => {
    const path = requestPath(input);
    const method = options.method || 'GET';
    if (path === '/api/v1/auth/session' && method === 'GET') {
      authReads += 1;
      if (authReads > 1 && retryBootstrapStatus !== 200) {
        return jsonResponse({
          error: { code: 'AUTH_UNAVAILABLE', message: '认证服务暂不可用', retryable: true }
        }, retryBootstrapStatus);
      }
      return jsonResponse({
        ...mockAuthPayload(),
        principal: { ...mockAuthPayload().principal, roles, capabilities }
      });
    }
    if (path === '/api/v1/admin/skill-reviews' && method === 'GET') {
      if (queueStatus !== 200) {
        return jsonResponse({
          error: {
            code: 'SKILL_REVIEW_CAPABILITY_REQUIRED',
            message: 'Reviewer capability required',
            retryable: false
          }
        }, queueStatus);
      }
      return jsonResponse(skillReviewQueueResponseFixture());
    }
    return jsonResponse({ error: { code: 'NOT_FOUND', message: 'not found', retryable: false } }, 404);
  });
}

function mockGovernanceAppFetch({
  roles = ['skill_governor'],
  capabilities = ['skill.govern'],
  queueStatus = 200,
  retryBootstrapStatus = 200
} = {}) {
  let authReads = 0;
  return vi.fn(async (input, options = {}) => {
    const path = requestPath(input);
    const method = options.method || 'GET';
    if (path === '/api/v1/auth/session' && method === 'GET') {
      authReads += 1;
      if (authReads > 1 && retryBootstrapStatus !== 200) {
        return jsonResponse({
          error: { code: 'AUTH_UNAVAILABLE', message: '认证服务暂不可用', retryable: true }
        }, retryBootstrapStatus);
      }
      return jsonResponse({
        ...mockAuthPayload(),
        principal: { ...mockAuthPayload().principal, roles, capabilities }
      });
    }
    if (path === '/api/v1/admin/skill-governance' && method === 'GET') {
      if (queueStatus !== 200) {
        return jsonResponse({
          error: {
            code: 'SKILL_GOVERNANCE_CAPABILITY_REQUIRED',
            message: 'Governor capability required',
            retryable: false
          }
        }, queueStatus);
      }
      return jsonResponse(skillGovernanceListResponseFixture());
    }
    return jsonResponse({ error: { code: 'NOT_FOUND', message: 'not found', retryable: false } }, 404);
  });
}

function requestPath(input) {
  return new URL(typeof input === 'string' ? input : input.url, 'http://localhost').pathname;
}

function mockLocalStorage() {
  const items = new Map();
  return {
    getItem: vi.fn((key) => (items.has(key) ? items.get(key) : null)),
    setItem: vi.fn((key, value) => {
      items.set(key, String(value));
    }),
    removeItem: vi.fn((key) => {
      items.delete(key);
    }),
    clear: vi.fn(() => {
      items.clear();
    })
  };
}

function appendCardEvent(cardID, card, options = {}) {
  const finalCardID = options.card_id || `${cardID}:${options.instance_id || `${cardID}-instance`}`;
  return {
    event: 'a2ui.action',
    payload: {
      a2ui_version: '1.0',
      actions: [
        {
          type: 'append_card',
          surface: 'chat',
          card_id: finalCardID,
          card
        }
      ]
    }
  };
}

function updateCardEvent(surface, cardID, payload) {
  return {
    event: 'a2ui.action',
    payload: {
      a2ui_version: '1.0',
      actions: [
        {
          type: 'update_card',
          surface,
          target: { surface, card_id: cardID },
          payload
        }
      ]
    }
  };
}

function mockAigcFetch(overrides = {}) {
  ensureDefaultEventSource();
  let storyboard = overrides.storyboard || defaultAigcStoryboard();
  let assets = overrides.assets || [];
  let jobs = overrides.jobs || [];
  let messages = overrides.messages || [];
  let createdSessionCount = 0;
  let approvalDecisionCount = 0;
  const sessionIDs = overrides.sessionIDs || ['s1'];
  return vi.fn(async (input, options = {}) => {
    const url = typeof input === 'string' ? input : input.url;
    const path = new URL(url, 'http://localhost').pathname;
    const method = options.method || 'GET';

    if (path === '/api/aigc/sessions' && method === 'POST') {
      const id = sessionIDs[createdSessionCount] || `s${createdSessionCount + 1}`;
      createdSessionCount += 1;
      return jsonResponse({ id, user_id: 'demo-user', title: 'AIGC Demo', status: 'active' }, 201);
    }
    if (path === '/api/aigc/skills' && method === 'POST') {
      return jsonResponse(
        overrides.createSkill || {
          skill: { id: 'skill-video', name: '武侠短片', description: '完整视频创作。', enabled: true },
          plan: { skill_id: 'skill-video', name: '武侠短片', description: '完整视频创作。', stages: [] }
        },
        201
      );
    }
    if (path === '/api/aigc/assets' && method === 'POST') {
      const uploaded = overrides.uploadedAsset || {
        id: `asset-upload-${assets.length + 1}`,
        session_id: 's1',
        kind: 'image',
        filename: 'upload.png',
        url: 'https://example.com/upload.png'
      };
      assets = [...assets, uploaded];
      return jsonResponse(uploaded, 201);
    }
    const sessionMatch = path.match(/^\/api\/aigc\/sessions\/([^/]+)(\/.*)?$/);
    const requestSessionID = sessionMatch?.[1];
    const sessionPath = sessionMatch?.[2] || '';
    if (requestSessionID && requestSessionID !== 's1') {
      if (sessionPath === '/storyboard') {
        return jsonResponse({ error: 'not found' }, 404);
      }
      if (sessionPath === '/assets') {
        return jsonResponse({ assets: [] });
      }
      if (sessionPath === '/jobs') {
        return jsonResponse({ jobs: [] });
      }
      if (sessionPath === '/messages') {
        return jsonResponse({ messages: [] });
      }
    }
    if (path === '/api/aigc/sessions/s1/skill' && method === 'POST') {
      return jsonResponse({ id: 's1', user_id: 'demo-user', skill_id: 'skill-video', title: 'AIGC Demo', status: 'active' });
    }
    if (path === '/api/aigc/sessions/s1/storyboard') {
      return jsonResponse(storyboard);
    }
    if (path === '/api/aigc/sessions/s1/storyboards/storyboard-1' && method === 'PATCH') {
      storyboard = overrides.patchedStoryboard || storyboard;
      return jsonResponse({ storyboard });
    }
    if (path === '/api/aigc/sessions/s1/assets') {
      return jsonResponse({ assets });
    }
    if (path === '/api/aigc/sessions/s1/jobs') {
      return jsonResponse({ jobs });
    }
    if (path === '/api/aigc/sessions/s1/messages' && method === 'GET') {
      return jsonResponse({ messages });
    }
    if (path === '/api/aigc/sessions/s1/messages' && method === 'POST') {
      const messageEvents = overrides.messageEvents || [];
      messages = appendRequestMessages(messages, options.body, messageEvents);
      messageEvents.forEach((event) => {
        const patch = mockStoryboardPatchFromEvent(event);
        if (patch) {
          storyboard = applyMockStoryboardPatch(storyboard, patch);
        }
      });
      if (overrides.storyboardAfterMessage) {
        storyboard = overrides.storyboardAfterMessage;
      }
      if (overrides.assetsAfterMessage) {
        assets = overrides.assetsAfterMessage;
      }
      if (overrides.jobsAfterMessage) {
        jobs = overrides.jobsAfterMessage;
      }
      if (overrides.messagesAfterMessage) {
        messages = overrides.messagesAfterMessage;
      }
      emitMockEvents(messageEvents);
      return jsonResponse({ run_id: 'run-1', status: 'completed' });
    }
    if (path === '/api/aigc/sessions/s1/messages/resume' && method === 'POST') {
      const resumeEvents = overrides.resumeEvents || [];
      messages = appendRequestMessages(messages, options.body, resumeEvents);
      emitMockEvents(resumeEvents);
      return jsonResponse({ run_id: 'run-resume-1', status: 'completed' });
    }
    if (
      path === '/api/aigc/sessions/s1/storyboards/storyboard-1/candidate-approvals/decision' &&
      method === 'POST'
    ) {
      const result = overrides.candidateApprovalDecision || {
        summary: { total: 0, approved: 0, complete: true, all_approved: true },
        results: [],
        storyboard
      };
      if (result.storyboard) {
        storyboard = result.storyboard;
      }
      if (overrides.assetsAfterCandidateDecision) {
        assets = overrides.assetsAfterCandidateDecision;
      }
      if (overrides.jobsAfterCandidateDecision) {
        jobs = overrides.jobsAfterCandidateDecision;
      }
      return jsonResponse(result, overrides.candidateApprovalDecisionStatus || 200);
    }
    if (/^\/api\/aigc\/sessions\/s1\/approvals\/[^/]+\/decision$/.test(path) && method === 'POST') {
      approvalDecisionCount += 1;
      if (approvalDecisionCount <= (overrides.approvalDecisionFailures || 0)) {
        return jsonResponse({ error: 'temporary approval failure' }, 503);
      }
      return jsonResponse(overrides.approvalDecision || { applied: true, decision: { approval: { status: 'approved' } } });
    }
    return jsonResponse({ error: 'not found' }, 404);
  });
}

function mockStoryboardPatchFromEvent(event) {
  if (event.event !== 'a2ui.action') {
    return null;
  }
  const actions = Array.isArray(event.payload?.actions) ? event.payload.actions : [];
  const action = actions.find((item) => item?.type === 'update_card' && (item.target?.surface || item.surface) === 'storyboard');
  if (action) {
    return action.payload?.patch;
  }
  return null;
}

function appendRequestMessages(messages, body, events) {
  const next = [...messages];
  const request = parseJSONBody(body);
  const content = request?.content || (request ? JSON.stringify(request) : '');
  if (content) {
    next.push({ id: `m-user-${next.length + 1}`, role: 'user', content });
  }
  events.forEach((event) => {
    if (event.event !== 'a2ui.action') {
      return;
    }
    const actions = Array.isArray(event.payload?.actions) ? event.payload.actions : [];
    if (actions.some((action) => action?.type === 'append_card' && (action.surface || action.target?.surface || 'chat') === 'chat')) {
      next.push({ id: `m-assistant-${next.length + 1}`, role: 'assistant', content: JSON.stringify(event.payload) });
    }
  });
  return next;
}

function parseJSONBody(body) {
  if (!body) {
    return null;
  }
  try {
    return JSON.parse(body);
  } catch {
    return null;
  }
}

function applyMockStoryboardPatch(storyboard, patch) {
  const next = JSON.parse(JSON.stringify(storyboard));
  if (patch?.ops?.length) {
    patch.ops.forEach((op) => applyMockPatchOp(next, op));
  }
  if (patch?.updates?.length) {
    patch.updates.forEach((update) => applyMockStoryboardUpdate(next, update));
  }
  if (patch?.next_version) {
    next.version = patch.next_version;
  }
  return next;
}

function applyMockPatchOp(root, op) {
  const tokens = op.path.split('/').slice(1);
  let target = root;
  for (let index = 0; index < tokens.length - 1; index += 1) {
    target = target[tokens[index]];
  }
  const last = tokens[tokens.length - 1];
  if (op.op === 'remove') {
    delete target[last];
    return;
  }
  target[last] = op.value;
}

function applyMockStoryboardUpdate(storyboard, update) {
  const target =
    update.target_type === 'shot'
      ? storyboard.shots.find((shot) => shot.shot_id === update.target_id)
      : update.target_type === 'key_element'
        ? storyboard.key_elements.find((element) => element.key === update.target_id)
        : storyboard.audio_layers.find((layer) => layer.layer_id === update.target_id);
  if (!target) {
    return;
  }
  const field = update.field || (update.target_type === 'key_element' ? 'asset_ids' : 'keyframe_asset_id');
  if (field === 'asset_ids') {
    target.asset_ids = [...new Set([...(target.asset_ids || []), ...(update.asset_ids || [])])];
  } else if (update.asset_ids?.length) {
    target[field] = update.asset_ids[0];
  }
  if (update.status) {
    target.status = update.status === 'generated' ? 'ready' : update.status;
  }
}

function defaultAigcStoryboard() {
  return {
    id: 'storyboard-1',
    session_id: 's1',
    version: 3,
    status: 'reviewing',
    key_elements: [
      {
        key: 'suji',
        type: 'character',
        name: '苏寂',
        description: '粗布麻衣，鬓角染霜，佩剑覆黑布。',
        status: 'reviewing'
      }
    ],
    shots: [
      {
        shot_id: 'shot-1',
        index: 1,
        duration_sec: 6,
        scene_description: '竹林归隐',
        camera_design: '冷色自然光，固定长镜头。',
        status: 'draft'
      }
    ],
    audio_layers: [
      {
        layer_id: 'music-1',
        type: 'music',
        description: '悲凉沉郁，尾声渐弱。',
        status: 'draft'
      }
    ]
  };
}

function pendingDynamicStoryboard() {
  return {
    id: 'storyboard-1',
    session_id: 's1',
    version: 2,
    status: 'reviewing',
    pending_revision_id: 'revision-pending',
    revisions: [
      {
        id: 'revision-pending',
        storyboard_id: 'storyboard-1',
        status: 'reviewing',
        scenario: 'short_drama',
        modules: [
          {
            id: 'module-scenes',
            key: 'scenes',
            semantic_type: 'scene',
            title: '分镜',
            order: 1,
            planned_count: 1,
            capabilities: { has_quantity: true, requires_prompt: true, requires_asset: true },
            elements: [
              {
                id: 'scene-1',
                key: 'scene-1',
                semantic_type: 'scene',
                title: '开场',
                revision: 1,
                content: { description: '待审核的开场镜头' },
                prompt_slots: [
                  { purpose: 'keyframe', prompt: 'cinematic pending frame', revision: 1, status: 'ready' }
                ],
                asset_slots: [
                  {
                    key: 'keyframe',
                    role: '关键帧',
                    media_kind: 'image',
                    required: true,
                    review_required: true,
                    generation_epoch: 0,
                    candidate_ids: ['binding-candidate'],
                    status: 'candidate'
                  }
                ]
              }
            ]
          }
        ]
      }
    ],
    bindings: [
      {
        id: 'binding-candidate',
        storyboard_id: 'storyboard-1',
        target_id: 'scene-1',
        asset_slot: 'keyframe',
        asset_id: 'candidate-image',
        state: 'candidate',
        approval_id: 'approval-candidate'
      }
    ]
  };
}

function candidateReviewStoryboard() {
  const storyboard = pendingDynamicStoryboard();
  storyboard.status = 'active';
  storyboard.pending_revision_id = '';
  storyboard.active_revision_id = 'revision-pending';
  storyboard.revisions[0].status = 'active';
  return storyboard;
}

function candidateAssetApprovalMessage() {
  const event = appendCardEvent(
    'candidate-approval',
    {
      root: 'root',
      title: '请选择生成候选',
      status: 'pending',
      data: {
        approval_id: 'approval-candidate',
        artifact_type: 'candidate_asset',
        asset_id: 'candidate-image'
      },
      components: [
        { id: 'root', component: { Card: { children: ['preview', 'decision'] } } },
        { id: 'preview', component: { ImagePreview: { url: 'https://example.com/candidate.png', title: 'candidate.png' } } },
        {
          id: 'decision',
          component: {
            SingleChoice: {
              key: 'decision',
              label: '审核决定',
              required: true,
              options: [
                { value: 'approved', label: '采用候选' },
                { value: 'rejected', label: '拒绝候选' }
              ]
            }
          }
        }
      ]
    },
    { card_id: 'approval:approval-candidate' }
  );
  return { id: 'candidate-approval-message', role: 'assistant', content: JSON.stringify(event.payload), seq: 1 };
}

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' }
  });
}
