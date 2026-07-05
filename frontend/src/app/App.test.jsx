import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from './App.jsx';

afterEach(() => {
  vi.restoreAllMocks();
  window.history.pushState({}, '', '/');
});

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

  it('renders the projects page from a direct route', () => {
    window.history.pushState({}, '', '/projects');

    render(<App />);

    const navigation = screen.getByRole('complementary', { name: 'DORAIGC 导航' });
    expect(screen.getByRole('heading', { name: '项目' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '新建项目' })).toBeInTheDocument();
    expect(within(navigation).getByRole('button', { name: '项目' })).toHaveClass('is-active');
    expect(screen.queryByRole('heading', { name: 'Dora Agent - 人人都是艺术大师' })).not.toBeInTheDocument();
  });

  it('renders the Skill page from the direct route', () => {
    window.history.pushState({}, '', '/skill');

    render(<App />);

    const navigation = screen.getByRole('complementary', { name: 'DORAIGC 导航' });
    expect(screen.getByRole('heading', { name: 'Skill' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: '我的', selected: true })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '新建Skill' })).toBeInTheDocument();
    expect(screen.getAllByTestId('skill-card')).toHaveLength(10);
    expect(screen.getByText('塔可夫斯基风格诗意短片')).toBeInTheDocument();
    expect(within(navigation).getByRole('button', { name: 'Skill' })).toHaveClass('is-active');
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

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

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
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
    render(<App />);

    await user.click(screen.getByRole('button', { name: '快速创作' }));

    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    expect(within(dialog).getByText('进入快速创作')).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: '登录并继续' }));

    expect(openSpy).toHaveBeenCalledWith('/workspace', '_blank', 'noopener,noreferrer');
    expect(screen.queryByRole('heading', { name: 'Seedance 2.0 创作工作台' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '用户菜单' })).toBeInTheDocument();
  });

  it('navigates through workspace, projects, and assets mock pages after login', async () => {
    const user = userEvent.setup();
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
    render(<App />);

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

    await user.click(screen.getByRole('button', { name: '快速创作' }));
    expect(openSpy).toHaveBeenCalledWith('/workspace', '_blank', 'noopener,noreferrer');

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

  it('resumes a media graph interrupt from the confirmation card', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageStream: [
        {
          event: 'a2ui.interrupt_request',
          payload: {
            scope: 'media_graph',
            checkpoint_id: 'media-cp-1',
            interrupt_id: 'interrupt-1',
            title: '确认参考图',
            message: '请确认参考图',
            actions: [{ key: 'confirm_reference_image', label: '确认参考图' }]
          }
        }
      ],
      mediaGraphResume: {
        status: 'completed',
        output: {
          storyboard_id: 'storyboard-1',
          storyboard_version: 3,
          status: 'reference_confirmed',
          job_ids: ['job-1']
        }
      }
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '生成参考图');
    await user.click(screen.getByRole('button', { name: '发送' }));
    await user.click(await screen.findByRole('button', { name: '确认参考图' }));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/media-graph/resume',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          checkpoint_id: 'media-cp-1',
          interrupt_id: 'interrupt-1',
          approved: true,
          note: '确认参考图'
        })
      })
    );
  });

  it('applies streamed storyboard patches and job status updates in the workspace', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageStream: [
        {
          event: 'storyboard.patch',
          payload: {
            next_version: 4,
            ops: [{ op: 'replace', path: '/shots/0/scene_description', value: '竹林夜雨' }]
          }
        },
        {
          event: 'job.status',
          payload: {
            job_id: 'job-1',
            session_id: 's1',
            target_id: 'shot-1',
            status: 'running'
          }
        }
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
    expect(screen.getByText('shot-1')).toBeInTheDocument();
    expect(screen.getByText('生成中')).toBeInTheDocument();
  });

  it('renders generated shot images after storyboard update hints and asset refresh', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const imageURL = 'https://tos.doraigc.com/aigc/sessions/s1/assets/asset-1/keyframe.png';
    const fetchMock = mockAigcFetch({
      assetsAfterMessage: [{ id: 'asset-1', session_id: 's1', kind: 'image', url: imageURL }],
      messageStream: [
        {
          event: 'job.status',
          payload: {
            job_id: 'job-1',
            session_id: 's1',
            target_type: 'shot',
            target_id: 'shot-1',
            status: 'succeeded',
            result_asset_ids: ['asset-1']
          }
        },
        {
          event: 'storyboard.patch',
          payload: {
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
        }
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

  it('shows A2UI rendering progress events in the chat timeline', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageStream: [
        { event: 'a2ui.begin_rendering', payload: { surface: 'storyboard', message: '开始渲染故事板' } },
        { event: 'a2ui.surface_update', payload: { surface: 'storyboard', message: '故事板卡片已更新' } }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    await screen.findByText('竹林归隐');
    await user.type(screen.getByPlaceholderText('输入创作需求或修改意见...'), '规划故事板');
    await user.click(screen.getByRole('button', { name: '发送' }));

    expect(await screen.findByText('开始渲染故事板')).toBeInTheDocument();
    expect(screen.getByText('故事板卡片已更新')).toBeInTheDocument();
  });

  it('renders A2UI info request forms and submits answers through chat', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageStream: [
        {
          event: 'a2ui.surface_update',
          surface_id: 'brief-intake',
          payload: {
            component: 'form',
            kind: 'info_request',
            title: '补充产品信息',
            message: '请补充商品宣传短片的基础信息。',
            submit_label: '提交信息',
            fields: [
              { key: 'product_name', label: '产品名称/品类', required: true },
              { key: 'core_selling_points', label: '核心卖点', multiline: true, required: true }
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
    await user.type(screen.getByLabelText('核心卖点'), '长续航，健康监测');
    await user.click(screen.getByRole('button', { name: '提交信息' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/aigc/sessions/s1/messages/stream',
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('智能手表')
        })
      );
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/messages/stream',
      expect.objectContaining({
        method: 'POST',
        body: expect.stringContaining('核心卖点：长续航，健康监测')
      })
    );
  });

  it('renders core A2UI components for interactive creation review', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const fetchMock = mockAigcFetch({
      messageStream: [
        {
          event: 'a2ui.surface_update',
          surface_id: 'brief-intake',
          payload: {
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
          }
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
    await user.click(screen.getByRole('button', { name: '提交选择' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/aigc/sessions/s1/messages/stream',
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('产品名称：智能手表')
        })
      );
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/messages/stream',
      expect.objectContaining({
        method: 'POST',
        body: expect.stringContaining('视觉风格：高级科技感')
      })
    );
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/aigc/sessions/s1/messages/stream',
      expect.objectContaining({
        method: 'POST',
        body: expect.stringContaining('投放平台：抖音')
      })
    );
  });

  it('parses embedded A2UI envelopes from assistant text without showing raw protocol JSON', async () => {
    window.history.pushState({}, '', '/workspace');
    const user = userEvent.setup();
    const envelope = {
      a2ui_events: [
        {
          event: 'a2ui.surface_update',
          surface_id: 'brief-intake',
          payload: {
            root: 'root',
            title: '补充产品信息',
            submit_label: '提交信息',
            components: [
              { id: 'root', component: { Card: { children: ['title', 'product', 'steps'] } } },
              { id: 'title', component: { Text: { value: '请补充商品宣传短片信息', usageHint: 'title' } } },
              { id: 'product', component: { TextInput: { key: 'product_name', label: '产品名称/品类', required: true } } },
              { id: 'steps', component: { VerticalSteps: { steps: [{ title: 'Agent 分析', status: 'running' }] } } }
            ]
          }
        }
      ]
    };
    const fetchMock = mockAigcFetch({
      messageStream: [
        {
          event: 'chat.delta',
          payload: {
            text: `好的！开始 Stage 1。请补充资料：${JSON.stringify(envelope)}请填写以上信息。`
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
    expect(screen.getByLabelText('产品名称/品类')).toBeInTheDocument();
    expect(screen.getByText('Agent 分析')).toBeInTheDocument();
    expect(screen.queryByText(/a2ui_events/)).not.toBeInTheDocument();
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

  it('restores persisted chat messages in the workspace', async () => {
    window.history.pushState({}, '', '/workspace');
    const fetchMock = mockAigcFetch({
      messages: [
        { id: 'm1', role: 'user', content: '生成一个武侠短片' },
        { id: 'm2', role: 'assistant', content: '故事板已生成。' }
      ]
    });
    vi.stubGlobal('fetch', fetchMock);

    render(<App />);

    expect(await screen.findByText('生成一个武侠短片')).toBeInTheDocument();
    expect(screen.getByText('故事板已生成。')).toBeInTheDocument();
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

  it('navigates through skills, featured works, and credits mock pages', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    expect(window.location.pathname).toBe('/skill');
    expect(screen.getByRole('heading', { name: 'Skill' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: '我的', selected: true })).toBeInTheDocument();
    expect(screen.getAllByTestId('skill-card')).toHaveLength(10);
    expect(screen.getAllByText('剧情短片（音色参考）')).toHaveLength(2);
    expect(screen.getByText('审核中')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '精选作品' }));
    expect(window.location.pathname).toBe('/');
    expect(screen.getByRole('heading', { name: '精选作品' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: '精选作品中心' })).not.toBeInTheDocument();
    expect(screen.getByText('MV 分镜生成')).toBeInTheDocument();
    expect(within(screen.getByRole('complementary', { name: 'DORAIGC 导航' })).getByRole('button', { name: '精选作品' })).toHaveClass('is-active');

    await user.click(screen.getByRole('button', { name: '310积分' }));
    expect(screen.getByRole('heading', { name: '积分中心' })).toBeInTheDocument();
    expect(screen.getByText('148 积分')).toBeInTheDocument();
    expect(screen.getByText('DORA-2026-CREATOR')).toBeInTheDocument();
  });

  it('keeps write actions on mock pages behind the login intent modal', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

    await user.click(screen.getByRole('button', { name: '项目' }));
    await user.click(screen.getByRole('button', { name: '继续创作 Seedance 2.0 视频制作' }));

    const dialog = screen.getByRole('dialog', { name: '登录后继续创作' });
    expect(within(dialog).getByText('继续创作 Seedance 2.0 视频制作')).toBeInTheDocument();
    expect(within(dialog).getByText('进入项目后会恢复最近会话和资产上下文。')).toBeInTheDocument();
  });

  it('opens the account menu from the avatar after login', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));
    await user.click(screen.getByRole('button', { name: '用户菜单' }));

    const menu = screen.getByRole('dialog', { name: '账户与积分' });
    expect(menu).toHaveClass('account-menu--compact');
    expect(menu).toHaveClass('account-menu--slim');
    expect(within(menu).getByText('User')).toBeInTheDocument();
    expect(within(menu).getByText('zhuifei2099@gmail.com')).toBeInTheDocument();
    expect(within(menu).getByText('Free')).toBeInTheDocument();
    expect(within(menu).getByRole('button', { name: '开通会员' })).toHaveClass('membership-button--theme');
    expect(within(menu).getByText('会员积分')).toBeInTheDocument();
    expect(within(menu).getByText('每周积分')).toBeInTheDocument();
    expect(within(menu).getByText('奖励积分')).toBeInTheDocument();
    expect(within(menu).getByRole('button', { name: '查看用量' })).toBeInTheDocument();
    expect(within(menu).getByText('语言')).toBeInTheDocument();
    expect(within(menu).getByText('反馈')).toBeInTheDocument();
    expect(within(menu).getByText('管理账户')).toBeInTheDocument();
  });

  it('uses user-facing copy and the same card system on private pages', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

    await user.click(screen.getByRole('button', { name: '项目' }));
    expect(screen.getAllByTestId('project-card')).toHaveLength(11);
    expect(screen.getByText('创建新项目')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '资产库' }));
    expect(screen.getAllByTestId('content-card')).toHaveLength(3);
    expect(screen.getByText('查看已经生成的图片、视频与音频，快速带回当前创作。')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '上传素材' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    expect(screen.getAllByTestId('skill-card')).toHaveLength(10);
    expect(screen.queryByText(/静态|mock|系统|API|PRD/)).not.toBeInTheDocument();
  });
});

function mockAigcFetch(overrides = {}) {
  let storyboard = defaultAigcStoryboard();
  let assets = overrides.assets || [];
  let jobs = overrides.jobs || [];
  return vi.fn(async (input, options = {}) => {
    const url = typeof input === 'string' ? input : input.url;
    const path = new URL(url, 'http://localhost').pathname;
    const method = options.method || 'GET';

    if (path === '/api/aigc/sessions' && method === 'POST') {
      return jsonResponse({ id: 's1', user_id: 'demo-user', title: 'AIGC Demo', status: 'active' }, 201);
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
    if (path === '/api/aigc/sessions/s1/messages') {
      return jsonResponse({ messages: overrides.messages || [] });
    }
    if (path === '/api/aigc/sessions/s1/messages/stream' && method === 'POST') {
      (overrides.messageStream || []).forEach((event) => {
        if (event.event === 'storyboard.patch') {
          storyboard = applyMockStoryboardPatch(storyboard, event.payload);
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
      return sseResponse(overrides.messageStream || []);
    }
    if (path === '/api/aigc/sessions/s1/media-graph/resume' && method === 'POST') {
      return jsonResponse(overrides.mediaGraphResume || { status: 'completed' });
    }
    if (path === '/api/aigc/sessions/s1/messages/resume/stream' && method === 'POST') {
      return sseResponse(overrides.resumeStream || []);
    }
    return jsonResponse({ error: 'not found' }, 404);
  });
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

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' }
  });
}

function sseResponse(events) {
  const body = events
    .map((event) => `event: ${event.event}\ndata: ${JSON.stringify(event)}\n\n`)
    .join('');
  return new Response(body, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' }
  });
}
