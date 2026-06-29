import { useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowUp,
  Bell,
  CalendarDays,
  ChevronDown,
  Crown,
  Eye,
  FolderKanban,
  Gift,
  Globe2,
  Heart,
  ImagePlus,
  Images,
  Maximize2,
  MessageCircle,
  Plus,
  Settings,
  SlidersHorizontal,
  Sparkles,
  Ticket,
  UserCircle,
  VolumeX,
  X
} from 'lucide-react';
import { BrandLogo } from '../../components/brand/BrandLogo.jsx';
import { getPageFromPath, getPathForPage, PUBLIC_PAGES, WORKSPACE_ROUTE } from './landingRoutes.js';
import {
  agentWorkspaceMock,
  assetMocks,
  creditMock,
  hotSkills,
  navItems,
  projectMocks,
  promptTools,
  publicWorks,
  recentProjects,
  skillMocks,
  userWorkMocks,
  workCategories,
  workspaceMock
} from './landingContent.js';
import { ProjectsPage } from './pages/ProjectsPage.jsx';

const themeStyle = {
  '--dora-lime': '#cfff24',
  '--dora-green': '#77f165',
  '--dora-mint': '#35e0ba',
  '--dora-cyan': '#4bd8ff',
  '--dora-coral': '#ff6f61',
  '--dora-bg': '#05070a',
  '--dora-panel': '#10151a'
};

const MASONRY_DEFAULT_CARD_WIDTH = 320;
const MASONRY_DEFAULT_GAP = 8;
const MASONRY_META_HEIGHT = 36;
const CARTOON_AVATAR_COUNT = 12;
const currentUser = {
  name: 'User',
  email: 'zhuifei2099@gmail.com',
  plan: 'Free',
  credits: 310
};

const accountPointGroups = [
  {
    icon: Ticket,
    title: '会员积分',
    value: '0',
    items: [
      ['套餐', '0'],
      ['购买积分', '0'],
      ['SD 2.0 专属积分', '0'],
      ['额外', '0']
    ]
  },
  {
    icon: CalendarDays,
    title: '每周积分',
    value: '200',
    items: [['每周一 00:00 刷新', '']]
  },
  {
    icon: Gift,
    title: '奖励积分',
    value: '110',
    items: [
      ['邀请奖励', '0'],
      ['探索奖励', '110']
    ]
  }
];

function openLoginIntent(setLoginIntent, title, prompt, targetPage) {
  setLoginIntent({ title, prompt: prompt || '登录后会继续刚才的创作动作。', targetPage });
}

function openWorkspaceInNewTab() {
  if (typeof window !== 'undefined') {
    window.open(WORKSPACE_ROUTE, '_blank', 'noopener,noreferrer');
  }
}

function parseRatio(ratio) {
  const [width, height] = ratio.split('/').map((item) => Number(item.trim()));
  return width > 0 && height > 0 ? width / height : 4 / 3;
}

function getAvatarNumber(author) {
  const hash = Array.from(author).reduce((sum, char) => sum + char.charCodeAt(0), 0);
  return (hash % CARTOON_AVATAR_COUNT) + 1;
}

function getCartoonAvatarSrc(author) {
  return `/avatars/doraigc-avatar-${String(getAvatarNumber(author)).padStart(2, '0')}.png`;
}

function createMasonryColumns(items, columnCount, cardWidth) {
  const columns = Array.from({ length: columnCount }, () => ({ height: 0, works: [] }));

  for (const work of items) {
    const targetColumn = columns.reduce((shortest, column) => (column.height < shortest.height ? column : shortest), columns[0]);
    targetColumn.works.push(work);
    targetColumn.height += cardWidth / parseRatio(work.ratio) + MASONRY_META_HEIGHT + MASONRY_DEFAULT_GAP;
  }

  return columns.map((column) => column.works);
}

function SideNav({ activePage, isLoggedIn, onNavigate, onLogin, onToggleAccountMenu }) {
  return (
    <aside className="side-nav" aria-label="DORAIGC 导航">
      <BrandLogo />
      <nav>
        {navItems.map((item) => {
          const needsLogin = !isLoggedIn && !PUBLIC_PAGES.has(item.page);

          return (
            <button
              key={item.label}
              type="button"
              className={activePage === item.page ? 'nav-item is-active' : 'nav-item'}
              onClick={() => (needsLogin ? onLogin(`进入${item.label}`, `登录后进入${item.label}，继续刚才的创作安排。`, item.page) : onNavigate(item.page))}
            >
              <item.icon aria-hidden="true" size={22} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>
      {isLoggedIn ? (
        <div className="side-nav__footer">
          <button type="button" className="ghost-link" onClick={onToggleAccountMenu}>
            <UserCircle aria-hidden="true" size={18} />
            <span>账户</span>
          </button>
        </div>
      ) : null}
    </aside>
  );
}

function AccountMenu({ user, onOpenCredits }) {
  return (
    <section className="account-menu account-menu--compact account-menu--slim" role="dialog" aria-modal="false" aria-labelledby="account-menu-title">
      <h2 id="account-menu-title" className="sr-only">账户与积分</h2>
      <div className="account-menu__profile">
        <div>
          <strong>{user.name}</strong>
          <span>{user.plan}</span>
          <p>{user.email}</p>
        </div>
      </div>
      <button className="membership-button membership-button--theme" type="button" aria-label="开通会员">
        <Crown aria-hidden="true" size={19} />
        <span>开通会员</span>
      </button>
      <div className="account-points" aria-label="积分概览">
        {accountPointGroups.map((group) => (
          <section className="account-point-group" key={group.title} aria-labelledby={`${group.title}-title`}>
            <div className="account-point-group__head">
              <group.icon aria-hidden="true" size={18} />
              <strong id={`${group.title}-title`}>{group.title}</strong>
              <span>{group.value}</span>
            </div>
            {group.items.map(([label, value]) => (
              <div className="account-point-row" key={label}>
                <span>{label}</span>
                {value ? <strong>{value}</strong> : null}
              </div>
            ))}
          </section>
        ))}
      </div>
      <button className="usage-button" type="button" onClick={onOpenCredits}>
        查看用量
      </button>
      <div className="account-menu__links">
        <button type="button">
          <Globe2 aria-hidden="true" size={19} />
          <span>语言</span>
          <strong>简体中文</strong>
          <ChevronDown aria-hidden="true" size={15} />
        </button>
        <button type="button">
          <MessageCircle aria-hidden="true" size={19} />
          <span>反馈</span>
        </button>
        <button type="button">
          <Settings aria-hidden="true" size={19} />
          <span>管理账户</span>
        </button>
      </div>
    </section>
  );
}

function ContextHeader({ activePage, isLoggedIn, user, isAccountMenuOpen, onLogin, onToggleAccountMenu, onOpenCredits }) {
  return (
    <header className={activePage === 'projects' ? 'context-header context-header--projects' : 'context-header'}>
      {activePage === 'projects' ? (
        <div className="projects-page__title">
          <FolderKanban aria-hidden="true" size={18} />
          <h1 id="projects-title">项目</h1>
        </div>
      ) : (
        <div className="status-pill attention-tag">
          <span className="status-dot" />
          DORAIGC 创作者招募中
        </div>
      )}
      <div className="context-header__actions">
        <button className="credit-pill" type="button" onClick={isLoggedIn ? onOpenCredits : () => onLogin('查看积分')}>
          <Ticket aria-hidden="true" size={16} />
          <span>{isLoggedIn ? user.credits : 148}</span>
          <span>积分</span>
        </button>
        <button className="icon-button" type="button" aria-label="通知" title="通知" onClick={() => onLogin('查看通知')}>
          <Bell aria-hidden="true" size={18} />
        </button>
        {isLoggedIn ? (
          <div className="account-menu-shell">
            <span className="plan-pill">{user.plan}</span>
            <button
              className="avatar-button"
              type="button"
              aria-label="用户菜单"
              aria-expanded={isAccountMenuOpen}
              onClick={onToggleAccountMenu}
            >
              <UserCircle aria-hidden="true" size={22} />
            </button>
            {isAccountMenuOpen ? <AccountMenu user={user} onOpenCredits={onOpenCredits} /> : null}
          </div>
        ) : (
          <button className="login-button" type="button" onClick={() => onLogin('登录')}>
            登录
          </button>
        )}
      </div>
    </header>
  );
}

function PromptComposer({ prompt, onPromptChange, onLogin }) {
  return (
    <section className="prompt-composer" aria-label="快速创作">
      <textarea
        value={prompt}
        maxLength={2000}
        rows={1}
        onChange={(event) => onPromptChange(event.target.value)}
        placeholder="由一个想法或故事开始..."
      />
      <div className="prompt-composer__tools" aria-label="创作工具">
        <button className="prompt-tool prompt-tool--plus" type="button" aria-label="添加素材" onClick={() => onLogin('添加素材')}>
          <ImagePlus aria-hidden="true" size={16} />
        </button>
        {promptTools.map((tool) => (
          <button className="prompt-tool" key={tool.label} type="button" aria-label={`打开${tool.label}`} onClick={() => onLogin(tool.label)}>
            {tool.label === '模型' ? <SlidersHorizontal aria-hidden="true" size={15} /> : null}
            {tool.label === 'Skill' ? <Sparkles aria-hidden="true" size={15} /> : null}
            {tool.label === '资产库' ? <Images aria-hidden="true" size={15} /> : null}
            <span>{tool.label}</span>
            {tool.badge ? <em>{tool.badge}</em> : null}
          </button>
        ))}
      </div>
      <button className="prompt-composer__submit" type="button" aria-label="开始创作" onClick={() => onLogin('开始创作', prompt)}>
        <ArrowUp aria-hidden="true" size={19} />
      </button>
      <div className="prompt-composer__count" aria-hidden="true">
        {prompt.length}/2000
      </div>
    </section>
  );
}

function HotSkills({ onUse }) {
  return (
    <section className="hot-skills" aria-label="热门 Skills">
      <h2>热门 Skills</h2>
      <div className="hot-skill-list">
        {hotSkills.map((skill) => (
          <div className="hot-skill-shell" key={skill.title}>
            <button className="hot-skill" type="button" onClick={() => onUse(skill)}>
              <span className="skill-avatar" aria-hidden="true">
                <img src={skill.avatar || getCartoonAvatarSrc(skill.author)} alt="" loading="lazy" />
              </span>
              <span>{skill.title}</span>
              {skill.badge ? <em>{skill.badge}</em> : null}
            </button>
            <div className="skill-preview-card" aria-hidden="true">
              <img src={skill.preview || skill.avatar || getCartoonAvatarSrc(skill.author)} alt="" loading="lazy" />
              <div className="skill-preview-card__body">
                <span>{skill.author}</span>
                <strong>{skill.title}</strong>
                <p>{skill.description}</p>
                <div>
                  {skill.tags.map((tag) => (
                    <small key={tag}>{tag}</small>
                  ))}
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

function RecentProjects({ onUse }) {
  return (
    <section className="recent-projects" aria-labelledby="recent-projects-title">
      <div className="recent-projects__heading">
        <h2 id="recent-projects-title">最近项目</h2>
        <button type="button" onClick={() => onUse('查看全部项目')}>查看全部</button>
      </div>
      <div className="project-strip">
        {recentProjects.map((project) => (
          <button className={project.action ? 'project-card project-card--new' : 'project-card'} key={project.title} type="button" onClick={() => onUse(project.title)}>
            <span className="project-card__cover">
              {project.action ? <Plus aria-hidden="true" size={26} /> : <img src={project.cover} alt="" loading="lazy" />}
            </span>
            <strong>{project.title}</strong>
            <small>{project.meta}</small>
          </button>
        ))}
      </div>
    </section>
  );
}

function WorkCategoryBridge({ activeCategory, onCategoryChange }) {
  return (
    <section className="featured-bridge" aria-labelledby="public-works-title">
      <h2 id="public-works-title">精选作品</h2>
      <div className="work-tabs" aria-label="作品分类">
        {workCategories.map((category) => (
          <button
            key={category}
            className={activeCategory === category ? 'work-tab is-active' : 'work-tab'}
            type="button"
            aria-pressed={activeCategory === category}
            onClick={() => onCategoryChange(category)}
          >
            {category}
          </button>
        ))}
      </div>
    </section>
  );
}

function PublicWorks({ activeCategory, likedWorks, mutedWorks, onLike, onToggleMute, onPreview }) {
  const gridRef = useRef(null);
  const filteredWorks = useMemo(
    () => (activeCategory === '全部' ? publicWorks : publicWorks.filter((work) => work.categories.includes(activeCategory))),
    [activeCategory]
  );
  const [masonry, setMasonry] = useState({
    cardWidth: MASONRY_DEFAULT_CARD_WIDTH,
    columnCount: 4
  });
  const columns = useMemo(
    () => createMasonryColumns(filteredWorks, masonry.columnCount, masonry.cardWidth),
    [filteredWorks, masonry.cardWidth, masonry.columnCount]
  );

  useEffect(() => {
    const grid = gridRef.current;

    if (!grid) {
      return undefined;
    }

    function updateMasonry() {
      const gridWidth = grid.getBoundingClientRect().width || grid.clientWidth;
      const styles = getComputedStyle(grid);
      const cardWidth = Number.parseFloat(styles.getPropertyValue('--work-card-width')) || MASONRY_DEFAULT_CARD_WIDTH;
      const gap = Number.parseFloat(styles.getPropertyValue('--work-card-gap')) || MASONRY_DEFAULT_GAP;
      const isMobile = typeof window.matchMedia === 'function' && window.matchMedia('(max-width: 760px)').matches;
      const nextColumnCount = isMobile ? 1 : Math.max(1, Math.min(filteredWorks.length, Math.floor((gridWidth + gap) / (cardWidth + gap))));
      const nextCardWidth = isMobile ? gridWidth : (gridWidth - gap * (nextColumnCount - 1)) / nextColumnCount;
      const roundedCardWidth = Number(nextCardWidth.toFixed(2));

      setMasonry((value) => (
        value.cardWidth === roundedCardWidth && value.columnCount === nextColumnCount
          ? value
          : { cardWidth: roundedCardWidth, columnCount: nextColumnCount }
      ));
    }

    updateMasonry();

    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateMasonry);
      return () => window.removeEventListener('resize', updateMasonry);
    }

    const observer = new ResizeObserver(updateMasonry);
    observer.observe(grid);

    return () => observer.disconnect();
  }, [filteredWorks.length]);

  return (
    <section className="public-works" aria-labelledby="public-works-title">
      <div className="work-grid" ref={gridRef} style={{ '--masonry-card-width': `${masonry.cardWidth}px` }}>
        {columns.map((column, columnIndex) => (
          <div className="work-column" key={`work-column-${columnIndex + 1}`}>
            {column.map((work) => (
              <article className="work-card" key={work.title} aria-label={`${work.title} 作品卡`} style={{ '--work-ratio': work.ratio }}>
                <div className="work-card__media">
                  <img src={work.cover} alt="" loading="lazy" />
                  <span className="work-card__tag transparent-tag">
                    {work.type}
                  </span>
                  <div className="work-card__media-tools" aria-label={`${work.title} 播放工具`}>
                    <button
                      className={mutedWorks.includes(work.title) ? 'work-card__icon-action is-active' : 'work-card__icon-action'}
                      type="button"
                      aria-label={`静音预览 ${work.title}`}
                      aria-pressed={mutedWorks.includes(work.title)}
                      onClick={() => onToggleMute(work)}
                    >
                      <VolumeX aria-hidden="true" size={15} />
                    </button>
                    <button className="work-card__icon-action" type="button" aria-label={`全屏播放 ${work.title}`} onClick={() => onPreview(work)}>
                      <Maximize2 aria-hidden="true" size={15} />
                    </button>
                  </div>
                  <div className="work-card__operation-layer">
                    <button
                      className="work-card__preview"
                      type="button"
                      aria-label={`预览 ${work.title}`}
                      onClick={() => onPreview(work)}
                    >
                      <Eye aria-hidden="true" size={14} />
                      <span>查看</span>
                    </button>
                    <button
                      className={likedWorks.includes(work.title) ? 'work-card__like is-liked' : 'work-card__like'}
                      type="button"
                      aria-pressed={likedWorks.includes(work.title)}
                      onClick={() => onLike(work)}
                    >
                      <Heart aria-hidden="true" size={14} />
                      <span>{work.metric}</span>
                    </button>
                  </div>
                </div>
                <div className="work-card__meta">
                  <span className="work-card__avatar" aria-hidden="true">
                    <img src={getCartoonAvatarSrc(work.author)} alt="" loading="lazy" />
                  </span>
                  <span className="work-card__byline">{work.author}</span>
                  <h3 className="work-card__title">{work.title}</h3>
                </div>
              </article>
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}

function LoginModal({ intent, onClose, onComplete }) {
  if (!intent) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section className="login-modal" role="dialog" aria-modal="true" aria-labelledby="login-title" onClick={(event) => event.stopPropagation()}>
        <button className="icon-button login-modal__close" type="button" aria-label="关闭登录弹窗" title="关闭" onClick={onClose}>
          <X aria-hidden="true" size={18} />
        </button>
        <div className="login-modal__brand-panel">
          <BrandLogo compact />
          <span className="login-modal__badge">已为你保留</span>
          <strong>当前想法和入口会在登录后继续。</strong>
        </div>
        <div className="login-modal__form">
          <h2 id="login-title">登录后继续创作</h2>
          <p>用账号保存项目、素材和积分记录，这次的动作不会丢。</p>
          <div className="intent-preview">
            <span>{intent.title}</span>
            <strong>{intent.prompt}</strong>
          </div>
          <div className="login-modal__actions">
            <button className="start-button" type="button" onClick={onComplete}>登录并继续</button>
            <button className="secondary-button" type="button" onClick={onComplete}>注册账号</button>
          </div>
          <button className="subtle-button" type="button" onClick={onClose}>稍后再说</button>
        </div>
      </section>
    </div>
  );
}

function WorkPreviewModal({ work, onClose, onCreate }) {
  if (!work) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <section className="preview-modal" role="dialog" aria-modal="true" aria-labelledby="preview-title" onClick={(event) => event.stopPropagation()}>
        <button className="icon-button preview-modal__close" type="button" aria-label="关闭作品预览" title="关闭" onClick={onClose}>
          <X aria-hidden="true" size={18} />
        </button>
        <img src={work.cover} alt="" />
        <div className="preview-modal__body">
          <span className="work-card__tag transparent-tag">
            {work.type}
          </span>
          <h2 id="preview-title">{work.title}</h2>
          <p>{work.description}</p>
          <div className="preview-modal__meta">
            <span>{work.metric}</span>
            <span>公开喜欢</span>
          </div>
          <button className="start-button" type="button" onClick={() => onCreate(work)}>
            用这个方向创作
          </button>
        </div>
      </section>
    </div>
  );
}

function PageHeader({ eyebrow, title, copy, children }) {
  return (
    <section className="mock-page__header" aria-labelledby={`${title}-title`}>
      <span className="mock-page__eyebrow">{eyebrow}</span>
      <div>
        <h1 id={`${title}-title`}>{title}</h1>
        <p>{copy}</p>
      </div>
      {children}
    </section>
  );
}

function WorkspacePage({ onIntent }) {
  return (
    <section className="mock-page workspace-page" aria-labelledby="workspace-title">
      <PageHeader
        eyebrow="继续创作"
        title={workspaceMock.title}
        copy="从一个想法继续推进镜头、素材和 Agent 对话，随时查看正在生成的内容。"
      >
        <button className="start-button" type="button" onClick={() => onIntent('继续生成 Seedance 2.0', workspaceMock.prompt)}>
          继续生成
        </button>
      </PageHeader>
      <div className="workspace-layout">
        <section className="mock-panel storyboard-panel" aria-labelledby="storyboard-title">
          <div className="mock-panel__head">
            <span className="transparent-tag">{workspaceMock.project}</span>
            <strong id="storyboard-title">{workspaceMock.status}</strong>
          </div>
          <ol className="storyboard-list">
            {workspaceMock.storyboard.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ol>
        </section>
        <section className="mock-panel preview-stage" aria-labelledby="preview-stage-title">
          <img src="/works/mv-city-generated.png" alt="" />
          <div>
            <span className="transparent-tag">16:9 预览已生成</span>
            <h2 id="preview-stage-title">城市雨夜主视觉</h2>
            <p>{workspaceMock.prompt}</p>
          </div>
        </section>
        <aside className="mock-panel chat-panel" aria-label="Agent 对话面板">
          <div className="credit-estimate">
            <span>{workspaceMock.credit}</span>
            <strong>预计 18 积分</strong>
          </div>
          <div className="message-stack">
            {workspaceMock.messages.map((message) => (
              <div className="chat-message" key={`${message.role}-${message.text}`}>
                <span>{message.role}</span>
                <p>{message.text}</p>
              </div>
            ))}
          </div>
          <div className="asset-chip-row">
            {workspaceMock.assets.map((asset) => (
              <button key={asset.title} type="button" onClick={() => onIntent(`引用资产 ${asset.title}`, '登录后会把该资产带入当前会话。')}>
                <img src={asset.cover} alt="" />
                <span>{asset.title}</span>
              </button>
            ))}
          </div>
        </aside>
      </div>
    </section>
  );
}

export function AgentWorkspacePage() {
  return (
    <div className="doraigc-shell agent-workspace-shell" style={themeStyle} data-testid="agent-workspace-shell">
      <header className="agent-workspace-topbar">
        <div className="agent-workspace-brand">
          <BrandLogo compact />
          <div>
            <h1>{agentWorkspaceMock.title}</h1>
            <span>{agentWorkspaceMock.project}</span>
          </div>
        </div>
        <nav className="agent-workspace-tools" aria-label="工作台工具">
          <button className="agent-workspace-tool is-active" type="button">
            <Images aria-hidden="true" size={16} />
            <span>媒体文件</span>
          </button>
          <button className="agent-workspace-tool" type="button">
            <Settings aria-hidden="true" size={16} />
            <span>剪辑</span>
          </button>
          <button className="agent-workspace-icon-button" type="button" aria-label="复制当前镜头" title="复制当前镜头">
            <Plus aria-hidden="true" size={16} />
          </button>
        </nav>
        <div className="agent-workspace-account">
          <button className="agent-workspace-icon-button agent-workspace-icon-button--accent" type="button" aria-label="打开对话" title="打开对话">
            <MessageCircle aria-hidden="true" size={16} />
          </button>
          <button className="agent-workspace-export" type="button">
            <ArrowUp aria-hidden="true" size={16} />
            <span>导出</span>
          </button>
          <span className="agent-workspace-credit">
            <Ticket aria-hidden="true" size={15} />
            <strong>{agentWorkspaceMock.credits}</strong>
            <span>积分</span>
          </span>
          <span className="agent-workspace-plan">{agentWorkspaceMock.plan}</span>
          <button className="agent-workspace-avatar" type="button" aria-label="用户菜单">
            <UserCircle aria-hidden="true" size={22} />
          </button>
        </div>
      </header>

      <main className="agent-workspace-main">
        <section className="agent-workspace-pane agent-media-pane" aria-label="媒体文件">
          <div className="agent-workspace-pane__header">
            <div>
              <Images aria-hidden="true" size={16} />
              <strong>媒体文件</strong>
            </div>
            <button className="agent-workspace-icon-button" type="button" aria-label="添加媒体文件" title="添加媒体文件">
              <Plus aria-hidden="true" size={15} />
            </button>
          </div>
          <div className="agent-media-grid">
            {agentWorkspaceMock.files.map((file) => (
              <button className={file.active ? 'agent-media-card is-active' : 'agent-media-card'} type="button" key={file.title}>
                <img src={file.cover} alt="" loading="lazy" />
                <span>{file.title}</span>
                <small>{file.type}</small>
              </button>
            ))}
          </div>
        </section>

        <section className="agent-workspace-pane agent-preview-pane" aria-label="预览画布">
          <div className="agent-workspace-pane__header">
            <div>
              <Eye aria-hidden="true" size={16} />
              <strong>预览</strong>
              <span>{agentWorkspaceMock.previewTitle}</span>
            </div>
            <div className="agent-preview-actions" aria-label="预览操作">
              <button className="agent-workspace-icon-button" type="button" aria-label="收藏结果" title="收藏结果">
                <Heart aria-hidden="true" size={15} />
              </button>
              <button className="agent-workspace-icon-button" type="button" aria-label="下载结果" title="下载结果">
                <ArrowUp aria-hidden="true" size={15} />
              </button>
              <button className="agent-workspace-icon-button" type="button" aria-label="全屏预览" title="全屏预览">
                <Maximize2 aria-hidden="true" size={15} />
              </button>
            </div>
          </div>
          <div className="agent-preview-tags" aria-label="生成参数">
            <span>{agentWorkspaceMock.model}</span>
            <span>{agentWorkspaceMock.size}</span>
          </div>
          <div className="agent-preview-canvas">
            <div className="agent-preview-frame">
              {agentWorkspaceMock.previewImages.map((image) => (
                <figure key={image.title}>
                  <img src={image.cover} alt="" loading="lazy" />
                  <figcaption>{image.title}</figcaption>
                </figure>
              ))}
            </div>
          </div>
          <div className="agent-preview-composer" aria-label="预览修改">
            <textarea placeholder="输入评论，编辑当前镜头并生成新画面..." rows={1} />
            <button className="agent-workspace-icon-button agent-workspace-icon-button--send" type="button" aria-label="发送预览修改">
              <ArrowUp aria-hidden="true" size={15} />
            </button>
            <button className="agent-workspace-secondary-action" type="button">手动编辑</button>
            <button className="agent-workspace-secondary-action" type="button">重新生成</button>
          </div>
        </section>

        <section className="agent-workspace-pane agent-chat-pane" aria-label="对话">
          <div className="agent-workspace-pane__header">
            <div>
              <MessageCircle aria-hidden="true" size={16} />
              <strong>对话</strong>
            </div>
            <button className="agent-workspace-icon-button" type="button" aria-label="收起对话" title="收起对话">
              <X aria-hidden="true" size={15} />
            </button>
          </div>
          <div className="agent-chat-scroll">
            <article className="agent-chat-message">
              <div className="agent-chat-thumbs">
                {agentWorkspaceMock.files.slice(0, 3).map((file) => (
                  <img src={file.cover} alt="" loading="lazy" key={file.title} />
                ))}
              </div>
              <p>三张关键元素图像已生成完成。</p>
            </article>
            <article className="agent-chat-message agent-chat-message--result">
              <strong>生成结果概览</strong>
              <ul>
                {agentWorkspaceMock.resultSummary.map((item) => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
              <p>{agentWorkspaceMock.nextStep}</p>
            </article>
            <article className="agent-confirm-card">
              <strong>{agentWorkspaceMock.confirmation.title}</strong>
              <div>
                {agentWorkspaceMock.confirmation.options.map((option, index) => (
                  <label key={option}>
                    <input type="radio" name="workspace-confirmation" defaultChecked={index === 0} />
                    <span>{option}</span>
                  </label>
                ))}
              </div>
              <button className="agent-workspace-send-button" type="button">发送</button>
            </article>
          </div>
          <div className="agent-chat-feedback" aria-label="结果反馈">
            <span>这个结果怎么样？</span>
            {[1, 2, 3, 4, 5].map((value) => (
              <button className="agent-rating-dot" type="button" aria-label={`评分 ${value}`} key={value} />
            ))}
          </div>
          <div className="agent-chat-composer" aria-label="发送消息">
            <textarea placeholder="请输入你的消息..." rows={2} />
            <div className="agent-chat-composer__tools">
              <button type="button" aria-label="添加内容">
                <Plus aria-hidden="true" size={15} />
              </button>
              <button type="button">
                <SlidersHorizontal aria-hidden="true" size={15} />
                <span>模型</span>
                <em>新</em>
              </button>
              <button type="button">
                <Sparkles aria-hidden="true" size={15} />
                <span>Skill</span>
              </button>
              <button type="button">
                <Images aria-hidden="true" size={15} />
                <span>资产库</span>
              </button>
              <button className="agent-workspace-icon-button--send" type="button" aria-label="发送消息">
                <ArrowUp aria-hidden="true" size={15} />
              </button>
            </div>
          </div>
        </section>
      </main>
    </div>
  );
}

function AssetsPage({ onIntent }) {
  return (
    <section className="mock-page" aria-labelledby="assets-title">
      <PageHeader
        eyebrow="素材与生成结果"
        title="资产库"
        copy="查看已经生成或上传的素材，快速带回当前创作。"
      >
        <button className="start-button" type="button" onClick={() => onIntent('上传素材', '登录后会进入上传和内容安全检查。')}>
          上传素材
        </button>
      </PageHeader>
      <div className="filter-row" aria-label="资产筛选">
        {['全部', '图片', '视频', '音乐', '异常资产'].map((item) => (
          <button className={item === '全部' ? 'work-tab is-active' : 'work-tab'} type="button" key={item}>
            {item}
          </button>
        ))}
      </div>
      <div className="asset-grid">
        {assetMocks.map((asset) => (
          <article className="asset-card content-card" data-testid="content-card" key={asset.title}>
            <img src={asset.cover} alt="" />
            <div className="content-card__body">
              <span className="transparent-tag">{asset.type}</span>
              <strong>{asset.title}</strong>
              <p>{asset.project} · {asset.source}</p>
              <small>{asset.status}</small>
            </div>
            <button className="secondary-button" type="button" onClick={() => onIntent(`引用资产 ${asset.title}`, '登录后会把资产引用到当前会话。')}>
              引用
            </button>
          </article>
        ))}
      </div>
    </section>
  );
}

function SkillsPage({ onIntent }) {
  return (
    <section className="mock-page" aria-labelledby="skills-title">
      <PageHeader
        eyebrow="创作方法"
        title="Skill 中心"
        copy="整理常用创作方法，查看审核进度和可用版本。"
      >
        <button className="start-button" type="button" onClick={() => onIntent('创建 Skill', '登录后进入 Skill Builder。')}>
          创建 Skill
        </button>
      </PageHeader>
      <div className="mock-card-grid mock-card-grid--three">
        {skillMocks.map((skill) => (
          <article className="mock-card content-card skill-library-card" data-testid="content-card" key={skill.title}>
            <img src={skill.cover} alt="" />
            <div className="content-card__body">
              <span className="transparent-tag">{skill.owner}</span>
              <h2>{skill.title}</h2>
              <p>{skill.description}</p>
              <div className="tag-row">
                {skill.tags.map((tag) => (
                  <small key={tag}>{tag}</small>
                ))}
              </div>
              <strong className="status-text">{skill.status}</strong>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function WorksPage({ onIntent }) {
  return (
    <section className="mock-page" aria-labelledby="works-title">
      <PageHeader
        eyebrow="我的作品"
        title="作品中心"
        copy="管理自己的作品草稿、公开状态和分享内容。"
      >
        <button className="start-button" type="button" onClick={() => onIntent('创建作品', '登录后会从项目资产选择封面和作品内容。')}>
          创建作品
        </button>
      </PageHeader>
      <div className="mock-card-grid mock-card-grid--three">
        {userWorkMocks.map((work) => (
          <article className="mock-card content-card work-library-card" data-testid="content-card" key={work.title}>
            <img src={work.cover} alt="" />
            <div className="content-card__body">
              <span className="transparent-tag">{work.state}</span>
              <h2>{work.title}</h2>
              <p>{work.meta}</p>
              <button className="secondary-button" type="button" onClick={() => onIntent(`编辑作品 ${work.title}`, '登录后会打开作品详情和分享设置。')}>
                编辑
              </button>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function ExplorePage({ activeCategory, onCategoryChange, likedWorks, mutedWorks, onLike, onToggleMute, onPreview }) {
  return (
    <section className="mock-page explore-page" aria-labelledby="explore-title">
      <PageHeader
        eyebrow="灵感广场"
        title="精选作品中心"
        copy="浏览大家公开分享的灵感作品，找到可继续创作的方向。"
      >
        <span className="status-pill">公开浏览</span>
      </PageHeader>
      <WorkCategoryBridge activeCategory={activeCategory} onCategoryChange={onCategoryChange} />
      <PublicWorks
        activeCategory={activeCategory}
        likedWorks={likedWorks}
        mutedWorks={mutedWorks}
        onLike={onLike}
        onToggleMute={onToggleMute}
        onPreview={onPreview}
      />
    </section>
  );
}

function CreditsPage({ onIntent }) {
  return (
    <section className="mock-page" aria-labelledby="credits-title">
      <PageHeader
        eyebrow="创作积分"
        title="积分中心"
        copy="展示余额、即将过期积分、兑换码入口和最近流水；生成扣费仍由工作台确认。"
      >
        <button className="start-button" type="button" onClick={() => onIntent('兑换积分', creditMock.redeemCode)}>
          兑换
        </button>
      </PageHeader>
      <div className="credit-overview">
        <div>
          <span>当前余额</span>
          <strong>{creditMock.balance}</strong>
          <p>{creditMock.expiring}</p>
        </div>
        <div className="redeem-box">
          <span>兑换码示例</span>
          <strong>{creditMock.redeemCode}</strong>
        </div>
      </div>
      <div className="ledger-list" aria-label="积分流水">
        {creditMock.ledger.map((item) => (
          <div className="ledger-item" key={item.title}>
            <span>{item.title}</span>
            <strong>{item.amount}</strong>
            <small>{item.status}</small>
          </div>
        ))}
      </div>
    </section>
  );
}

export function LandingPage() {
  const [prompt, setPrompt] = useState('');
  const [activePage, setActivePage] = useState('home');
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isAccountMenuOpen, setIsAccountMenuOpen] = useState(false);
  const [loginIntent, setLoginIntent] = useState(null);
  const [previewWork, setPreviewWork] = useState(null);
  const [likedWorks, setLikedWorks] = useState([]);
  const [mutedWorks, setMutedWorks] = useState([]);
  const [activeCategory, setActiveCategory] = useState('全部');

  function requestLogin(title, promptValue, targetPage) {
    openLoginIntent(setLoginIntent, title, promptValue || prompt || '登录后会继续刚才的创作动作。', targetPage);
    setIsAccountMenuOpen(false);
  }

  function handleLoginComplete() {
    setIsLoggedIn(true);

    if (loginIntent?.targetPage === 'workspace') {
      openWorkspaceInNewTab();
    } else if (loginIntent?.targetPage) {
      setActivePage(loginIntent.targetPage);
    }

    setLoginIntent(null);
    setIsAccountMenuOpen(false);
  }

  function handleNavigate(page) {
    if (page === 'workspace') {
      openWorkspaceInNewTab();
      setIsAccountMenuOpen(false);
      return;
    }

    setActivePage(page);
    setIsAccountMenuOpen(false);
  }

  function openCreditsPage() {
    setActivePage('credits');
    setIsAccountMenuOpen(false);
  }

  function handleWorkLike(work) {
    setLikedWorks((items) => (items.includes(work.title) ? items : [...items, work.title]));
    requestLogin('点赞精选作品', work.title);
  }

  function handleToggleMute(work) {
    setMutedWorks((items) => (items.includes(work.title) ? items.filter((title) => title !== work.title) : [...items, work.title]));
  }

  function handleWorkCreate(work) {
    setPrompt(work.intent);
    setActivePage('home');
    setPreviewWork(null);
    requestLogin('基于精选作品创作', work.intent);
  }

  useEffect(() => {
    function closeOverlay(event) {
      if (event.key === 'Escape') {
        setLoginIntent(null);
        setPreviewWork(null);
        setIsAccountMenuOpen(false);
      }
    }

    window.addEventListener('keydown', closeOverlay);

    return () => {
      window.removeEventListener('keydown', closeOverlay);
    };
  }, []);

  return (
    <div className="doraigc-shell" style={themeStyle} data-testid="doraigc-shell">
      <SideNav
        activePage={activePage}
        isLoggedIn={isLoggedIn}
        onNavigate={handleNavigate}
        onLogin={requestLogin}
        onToggleAccountMenu={() => setIsAccountMenuOpen((value) => !value)}
      />
      <main className={activePage === 'projects' ? 'landing-main landing-main--projects' : 'landing-main'}>
        <ContextHeader
          activePage={activePage}
          isLoggedIn={isLoggedIn}
          user={currentUser}
          isAccountMenuOpen={isAccountMenuOpen}
          onLogin={requestLogin}
          onToggleAccountMenu={() => setIsAccountMenuOpen((value) => !value)}
          onOpenCredits={openCreditsPage}
        />
        {activePage === 'home' ? (
          <>
            <section className="creation-screen" aria-labelledby="creation-title">
              <div className="creation-stack">
                <div className="landing-hero">
                  <h1 id="creation-title">Dora Agent - 人人都是艺术大师</h1>
                  <p className="hero-copy">把灵感交给 Dora Agent，从一句想法生成影像、音乐、海报和商品内容，让每个人都能完成自己的创作。</p>
                  <PromptComposer
                    prompt={prompt}
                    onPromptChange={setPrompt}
                    onLogin={requestLogin}
                  />
                </div>
                <HotSkills onUse={(skill) => requestLogin(skill.title, skill.title)} />
                <RecentProjects onUse={requestLogin} />
              </div>
              <WorkCategoryBridge activeCategory={activeCategory} onCategoryChange={setActiveCategory} />
            </section>
            <PublicWorks
              activeCategory={activeCategory}
              likedWorks={likedWorks}
              mutedWorks={mutedWorks}
              onLike={handleWorkLike}
              onToggleMute={handleToggleMute}
              onPreview={setPreviewWork}
            />
          </>
        ) : null}
        {activePage === 'workspace' ? <WorkspacePage onIntent={requestLogin} /> : null}
        {activePage === 'projects' ? <ProjectsPage onIntent={requestLogin} /> : null}
        {activePage === 'assets' ? <AssetsPage onIntent={requestLogin} /> : null}
        {activePage === 'skills' ? <SkillsPage onIntent={requestLogin} /> : null}
        {activePage === 'works' ? <WorksPage onIntent={requestLogin} /> : null}
        {activePage === 'explore' ? (
          <ExplorePage
            activeCategory={activeCategory}
            onCategoryChange={setActiveCategory}
            likedWorks={likedWorks}
            mutedWorks={mutedWorks}
            onLike={handleWorkLike}
            onToggleMute={handleToggleMute}
            onPreview={setPreviewWork}
          />
        ) : null}
        {activePage === 'credits' ? <CreditsPage onIntent={requestLogin} /> : null}
      </main>
      <LoginModal intent={loginIntent} onClose={() => setLoginIntent(null)} onComplete={handleLoginComplete} />
      <WorkPreviewModal work={previewWork} onClose={() => setPreviewWork(null)} onCreate={handleWorkCreate} />
    </div>
  );
}
