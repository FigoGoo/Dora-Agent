import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from './App.jsx';

describe('DORAIGC landing page', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    delete global.fetch;
  });

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

  it('continues a saved prompt by logging in and creating an Agent run', async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (url) => {
      if (url === '/api/auth/login') {
        return {
          ok: true,
          json: async () => ({
            code: 'OK',
            data: {
              access_token: 'user-token-1001',
              current_space_id: 'sp_1001'
            }
          })
        };
      }

      if (url === '/api/agent/sessions') {
        return {
          ok: true,
          json: async () => ({
            session_id: 'sess_frontend_full_flow',
            project_id: 'prj_active_1001',
            status: 'active'
          })
        };
      }

      if (url === '/api/agent/runs') {
        return {
          ok: true,
          json: async () => ({
            run_id: 'run_frontend_full_flow',
            session_id: 'sess_frontend_full_flow',
            project_id: 'prj_active_1001',
            status: 'running',
            stream_url: '/api/agent/runs/run_frontend_full_flow/stream'
          })
        };
      }

      if (url === '/api/agent/runs/run_frontend_full_flow/events?after_sequence=0&limit=100') {
        return {
          ok: true,
          json: async () => ({
            events: [
              {
                type: 'agent.skill.selected',
                sequence: 1,
                payload: {
                  skill_id: 'sk_storyboard',
                  title: '复杂分镜 Skill'
                }
              },
              {
                type: 'tool.call.completed',
                sequence: 2,
                payload: {
                  tool_name: 'storyboard_extract_20260630',
                  policy_allowed: true
                }
              },
              {
                type: 'generation.progress',
                sequence: 3,
                payload: {
                  stage: 'model_snapshot_resolved',
                  model_id: 'mdl_frontend_image'
                }
              },
              {
                type: 'confirmation.required',
                sequence: 4,
                payload: {
                  title: '确认生成资产'
                }
              }
            ],
            next_sequence: 4
          })
        };
      }

      throw new Error(`unexpected fetch ${url}`);
    });
    global.fetch = fetchMock;

    render(<App />);

    await user.type(screen.getByPlaceholderText('由一个想法或故事开始...'), '用新增分镜 Skill 生成一条城市广告片');
    await user.click(screen.getByRole('button', { name: '开始创作' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

    expect(await screen.findByRole('region', { name: 'Agent 工作台' })).toBeInTheDocument();
    expect(screen.getByText('run_frontend_full_flow')).toBeInTheDocument();
    expect(screen.getByText('复杂分镜 Skill')).toBeInTheDocument();
    expect(screen.getByText('storyboard_extract_20260630')).toBeInTheDocument();
    expect(screen.getByText('mdl_frontend_image')).toBeInTheDocument();
    expect(screen.getByText('确认生成资产')).toBeInTheDocument();

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/auth/login', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({
        login_type: 'personal',
        account: 'user1001@dora.local',
        password: 'local-user-change-me'
      })
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/agent/sessions', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({
        Authorization: 'Bearer user-token-1001',
        'X-Space-Id': 'sp_1001',
        'Idempotency-Key': expect.stringMatching(/^frontend-session-/)
      }),
      body: JSON.stringify({
        project_id: 'prj_active_1001',
        initial_title: '用新增分镜 Skill 生成一条城市广告片'
      })
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/agent/runs', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({
        Authorization: 'Bearer user-token-1001',
        'X-Space-Id': 'sp_1001',
        'Idempotency-Key': expect.stringMatching(/^frontend-run-/)
      }),
      body: expect.stringContaining('用新增分镜 Skill 生成一条城市广告片')
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/api/agent/runs/run_frontend_full_flow/events?after_sequence=0&limit=100',
      expect.objectContaining({
        method: 'GET',
        headers: expect.objectContaining({
          Authorization: 'Bearer user-token-1001',
          'X-Space-Id': 'sp_1001'
        })
      })
    );
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
