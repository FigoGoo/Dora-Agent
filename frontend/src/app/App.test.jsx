import { render, screen, within } from '@testing-library/react';
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
    expect(screen.getByRole('heading', { name: '项目' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '新建项目' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '最近编辑' })).not.toBeInTheDocument();
    expect(screen.getByText('Seedance 2.0 视频制作')).toBeInTheDocument();
    expect(screen.getByText('功能介绍 202606140505')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '资产库' }));
    expect(screen.getByRole('heading', { name: '资产库' })).toBeInTheDocument();
    expect(screen.getByText('生成视频')).toBeInTheDocument();
    expect(screen.getByText('保存失败')).toBeInTheDocument();
  });

  it('renders the standalone workspace route without the home shell navigation', () => {
    window.history.pushState({}, '', '/workspace');

    render(<App />);

    expect(screen.getByRole('heading', { name: '短剧制作' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '媒体文件' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '预览画布' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: '对话' })).toBeInTheDocument();
    expect(screen.queryByRole('complementary', { name: 'DORAIGC 导航' })).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('请输入你的消息...')).toBeInTheDocument();
  });

  it('navigates through skills, explore, and credits mock pages', async () => {
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole('button', { name: '登录' }));
    await user.click(screen.getByRole('button', { name: '登录并继续' }));

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    expect(screen.getByRole('heading', { name: 'Skill 中心' })).toBeInTheDocument();
    expect(screen.getByText('AI 短剧一站式生成')).toBeInTheDocument();
    expect(screen.getByText('待审核')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: '精选作品' }));
    expect(screen.getByRole('heading', { name: '精选作品中心' })).toBeInTheDocument();
    expect(screen.getByText('公开浏览')).toBeInTheDocument();

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
    expect(screen.getByText('查看已经生成或上传的素材，快速带回当前创作。')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Skill' }));
    expect(screen.getAllByTestId('content-card')).toHaveLength(3);
    expect(screen.queryByText(/静态|mock|系统|API|PRD/)).not.toBeInTheDocument();
  });
});
