import { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowUp, Bell, Eye, Heart, ImagePlus, Images, LogIn, Maximize2, Plus, SlidersHorizontal, Sparkles, VolumeX, X } from 'lucide-react';
import { BrandLogo } from '../../components/brand/BrandLogo.jsx';
import { hotSkills, navItems, promptTools, publicWorks, recentProjects, workCategories } from './landingContent.js';

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

function openLoginIntent(setLoginIntent, title, prompt) {
  setLoginIntent({ title, prompt: prompt || '登录后会继续刚才的创作动作。' });
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

function SideNav({ onLogin }) {
  return (
    <aside className="side-nav" aria-label="DORAIGC 导航">
      <BrandLogo />
      <nav>
        {navItems.map((item) => (
          <button
            key={item.label}
            type="button"
            className={item.active ? 'nav-item is-active' : 'nav-item'}
            onClick={() => {
              if (!item.active) {
                onLogin(item.label);
              }
            }}
          >
            <item.icon aria-hidden="true" size={18} />
            <span>{item.label}</span>
          </button>
        ))}
      </nav>
      <div className="side-nav__footer">
        <button type="button" className="ghost-link" onClick={() => onLogin('登录')}>
          <LogIn aria-hidden="true" size={16} />
          <span>登录 / 注册</span>
        </button>
      </div>
    </aside>
  );
}

function ContextHeader({ onLogin }) {
  return (
    <header className="context-header">
      <div className="status-pill attention-tag">
        <span className="status-dot" />
        DORAIGC 创作者招募中
      </div>
      <div className="context-header__actions">
        <button className="credit-pill" type="button" onClick={() => onLogin('查看积分')}>
          <span>148</span>
          <span>积分</span>
        </button>
        <button className="icon-button" type="button" aria-label="通知" title="通知" onClick={() => onLogin('查看通知')}>
          <Bell aria-hidden="true" size={18} />
        </button>
        <button className="login-button" type="button" onClick={() => onLogin('登录')}>
          登录
        </button>
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
          <button className="prompt-tool" key={tool.label} type="button" onClick={() => onLogin(tool.label)}>
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

function LoginModal({ intent, onClose }) {
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
          <span className="login-modal__badge">Intent saved</span>
          <strong>你的创作上下文已经暂存。</strong>
        </div>
        <div className="login-modal__form">
          <h2 id="login-title">登录后继续创作</h2>
          <p>已保留你的动作和当前输入，登录后可以继续进入工作台。</p>
          <div className="intent-preview">
            <span>{intent.title}</span>
            <strong>{intent.prompt}</strong>
          </div>
          <div className="login-modal__actions">
            <button className="start-button" type="button">登录并继续</button>
            <button className="secondary-button" type="button">注册账号</button>
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

export function LandingPage() {
  const [prompt, setPrompt] = useState('');
  const [loginIntent, setLoginIntent] = useState(null);
  const [previewWork, setPreviewWork] = useState(null);
  const [likedWorks, setLikedWorks] = useState([]);
  const [mutedWorks, setMutedWorks] = useState([]);
  const [activeCategory, setActiveCategory] = useState('全部');

  function requestLogin(title, promptValue) {
    openLoginIntent(setLoginIntent, title, promptValue || prompt || '登录后会继续刚才的创作动作。');
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
    setPreviewWork(null);
    requestLogin('基于精选作品创作', work.intent);
  }

  useEffect(() => {
    function closeOverlay(event) {
      if (event.key === 'Escape') {
        setLoginIntent(null);
        setPreviewWork(null);
      }
    }

    window.addEventListener('keydown', closeOverlay);

    return () => {
      window.removeEventListener('keydown', closeOverlay);
    };
  }, []);

  return (
    <div className="doraigc-shell" style={themeStyle} data-testid="doraigc-shell">
      <SideNav onLogin={requestLogin} />
      <main className="landing-main">
        <ContextHeader onLogin={requestLogin} />
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
      </main>
      <LoginModal intent={loginIntent} onClose={() => setLoginIntent(null)} />
      <WorkPreviewModal work={previewWork} onClose={() => setPreviewWork(null)} onCreate={handleWorkCreate} />
    </div>
  );
}
