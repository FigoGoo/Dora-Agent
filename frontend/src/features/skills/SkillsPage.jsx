import { useEffect, useMemo, useState } from 'react';
import { Blocks, Loader2, Play, Plus, Search, Sparkles } from 'lucide-react';
import { marketplaceApi } from '../../lib/api/marketplace.js';
import { skillLibraryFilters, skillLibraryTabs, skillMocks } from './skillMocks.js';

const DEFAULT_PAGE_SIZE = 24;

function fallbackListingFromMock(skill, index) {
  const isDraft = skill.status === '草稿';
  return {
    listing_id: `listing_fallback_${index}`,
    skill_id: `skill_fallback_${index}`,
    skill_version_id: `sv_fallback_${index}`,
    skill_version: skill.version.replace(/^V/i, '') || '1',
    skill_name: skill.title,
    skill_description: skill.description,
    creator_user_id: skill.author,
    status: isDraft ? 'draft' : 'listed',
    pricing_model: isDraft ? 'free' : 'fixed',
    usage_credits: isDraft ? 0 : 120,
    cover: skill.cover,
    models: skill.models,
    owner: skill.owner,
    tone: skill.tone
  };
}

function mergeInstalled(listings, installations) {
  const installedBySkill = new Map(installations.map((item) => [item.skill_id, item]));
  return listings.map((listing) => ({
    ...listing,
    installation: installedBySkill.get(listing.skill_id) || null
  }));
}

function displayName(listing) {
  return listing.skill_name || listing.skill_id || '未命名 Skill';
}

function pricingLabel(listing) {
  const credits = Number(listing.usage_credits || 0);
  return credits > 0 ? `${credits}积分/次` : '免费';
}

function statusLabel(listing) {
  if (listing.installation) {
    return '已安装';
  }
  if (String(listing.status || '').toLowerCase() !== 'listed') {
    return '未上架';
  }
  return Number(listing.usage_credits || 0) > 0 ? '付费' : '免费';
}

function statusClassName(listing) {
  const label = statusLabel(listing);
  if (label === '已安装') {
    return 'is-enabled';
  }
  if (label === '未上架') {
    return 'is-draft';
  }
  return 'is-reviewing';
}

function matchesFilter(listing, filter) {
  if (filter === '付费') {
    return Number(listing.usage_credits || 0) > 0;
  }
  if (filter === '免费') {
    return Number(listing.usage_credits || 0) === 0;
  }
  if (filter === '已安装') {
    return Boolean(listing.installation);
  }
  return true;
}

function matchesQuery(listing, query) {
  const text = query.trim().toLowerCase();
  if (!text) {
    return true;
  }
  return [listing.skill_name, listing.skill_description, listing.creator_user_id, listing.skill_id].some((value) =>
    String(value || '').toLowerCase().includes(text)
  );
}

export function SkillsPage({ isLoggedIn = false, onIntent, onUseSkill }) {
  const [activeTab, setActiveTab] = useState(skillLibraryTabs[0]);
  const [activeFilter, setActiveFilter] = useState(skillLibraryFilters[0]);
  const [query, setQuery] = useState('');
  const [listings, setListings] = useState(() => skillMocks.map(fallbackListingFromMock));
  const [installations, setInstallations] = useState([]);
  const [loading, setLoading] = useState(false);
  const [installingId, setInstallingId] = useState('');

  useEffect(() => {
    let alive = true;

    async function loadMarketplace() {
      if (!isLoggedIn || typeof fetch !== 'function') {
        return;
      }
      setLoading(true);
      try {
        const [marketplace, installed] = await Promise.all([
          marketplaceApi.listSkills({ page_size: DEFAULT_PAGE_SIZE, query }),
          marketplaceApi.listInstalledSkills({ page_size: DEFAULT_PAGE_SIZE })
        ]);
        if (!alive) {
          return;
        }
        const nextListings = Array.isArray(marketplace?.items) && marketplace.items.length ? marketplace.items : skillMocks.map(fallbackListingFromMock);
        setListings(nextListings);
        setInstallations(Array.isArray(installed?.items) ? installed.items : []);
      } catch {
        if (alive) {
          setListings(skillMocks.map(fallbackListingFromMock));
          setInstallations([]);
        }
      } finally {
        if (alive) {
          setLoading(false);
        }
      }
    }

    loadMarketplace();

    return () => {
      alive = false;
    };
  }, [isLoggedIn, query]);

  const mergedListings = useMemo(() => mergeInstalled(listings, installations), [listings, installations]);
  const visibleListings = useMemo(() => {
    const source = activeTab === '已安装' ? mergedListings.filter((listing) => listing.installation) : mergedListings;
    return source.filter((listing) => matchesFilter(listing, activeFilter)).filter((listing) => matchesQuery(listing, query));
  }, [activeFilter, activeTab, mergedListings, query]);

  async function installListing(listing) {
    const name = displayName(listing);
    if (!isLoggedIn) {
      onIntent?.(`安装 ${name}`, '登录后安装并加入我的 Skill。', 'skills');
      return;
    }
    setInstallingId(listing.listing_id);
    try {
      const result = await marketplaceApi.installSkill(
        {
          listing_id: listing.listing_id,
          target_scope: 'personal'
        },
        { idempotencyKey: `install:${listing.listing_id}:personal` }
      );
      if (result?.installation) {
        setInstallations((items) => {
          const withoutCurrent = items.filter((item) => item.installation_id !== result.installation.installation_id && item.skill_id !== result.installation.skill_id);
          return [result.installation, ...withoutCurrent];
        });
      }
    } catch {
      onIntent?.(`安装 ${name}`, '稍后重试安装。', 'skills');
    } finally {
      setInstallingId('');
    }
  }

  function useListing(listing) {
    const name = displayName(listing);
    if (!isLoggedIn) {
      onIntent?.(`使用 ${name}`, '登录后将这个 Skill 带入创作。', 'skills');
      return;
    }
    onUseSkill?.(listing);
  }

  return (
    <section className="mock-page skills-page" aria-labelledby="skills-title">
      <div className="skills-page__bar">
        <div className="skills-page__tabs" role="tablist" aria-label="Skill 视图">
          {skillLibraryTabs.map((tab) => (
            <button
              className={activeTab === tab ? 'skills-page__tab is-active' : 'skills-page__tab'}
              type="button"
              role="tab"
              aria-selected={activeTab === tab}
              key={tab}
              onClick={() => setActiveTab(tab)}
            >
              {tab}
            </button>
          ))}
        </div>
        <label className="skills-page__search">
          <Search aria-hidden="true" size={16} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 Skill" />
        </label>
        <button className="skills-page__create" type="button" onClick={() => onIntent?.('创建 Skill', '登录后进入 Skill Builder。')}>
          <Plus aria-hidden="true" size={18} />
          <span>新建Skill</span>
        </button>
      </div>
      <div className="skills-page__filters" aria-label="Skill 筛选">
        {skillLibraryFilters.map((filter) => (
          <button
            className={activeFilter === filter ? 'skills-page__filter is-active' : 'skills-page__filter'}
            type="button"
            aria-pressed={activeFilter === filter}
            key={filter}
            onClick={() => setActiveFilter(filter)}
          >
            {filter}
          </button>
        ))}
        {loading ? (
          <span className="skills-page__loading">
            <Loader2 aria-hidden="true" size={14} />
            更新中
          </span>
        ) : null}
      </div>
      {visibleListings.length ? (
        <div className="skills-page__grid" aria-label="Skill 列表">
          {visibleListings.map((skill, index) => (
            <article className={`skill-library-card skill-library-card--${skill.tone || 'empty'}`} data-testid="skill-card" key={skill.listing_id || `${skill.skill_id}-${index}`}>
              <div className="skill-library-card__media">
                {skill.cover ? <img src={skill.cover} alt="" loading="lazy" /> : <Blocks aria-hidden="true" size={44} />}
                <div className="skill-library-card__models">
                  {(skill.models || [pricingLabel(skill)]).map((model) => (
                    <span key={model}>{model}</span>
                  ))}
                </div>
              </div>
              <div className="skill-library-card__content">
                <span className="skill-library-card__author">{skill.creator_user_id || skill.owner || 'DORAIGC'}</span>
                <div className="skill-library-card__title-row">
                  <h2>{displayName(skill)}</h2>
                  <span>V{skill.skill_version || '1'}</span>
                </div>
                <p>{skill.skill_description || '可安装到工作台继续创作。'}</p>
                <div className="skill-library-card__footer">
                  <span>{pricingLabel(skill)}</span>
                  <strong className={statusClassName(skill)}>{statusLabel(skill)}</strong>
                  {skill.installation ? (
                    <button className="skill-library-card__action" type="button" onClick={() => useListing(skill)}>
                      <Play aria-hidden="true" size={14} />
                      <span>使用</span>
                    </button>
                  ) : (
                    <button
                      className={installingId === skill.listing_id ? 'skill-library-card__action is-loading' : 'skill-library-card__action'}
                      type="button"
                      disabled={installingId === skill.listing_id}
                      onClick={() => installListing(skill)}
                    >
                      {installingId === skill.listing_id ? <Loader2 aria-hidden="true" size={14} /> : <Sparkles aria-hidden="true" size={14} />}
                      <span>{installingId === skill.listing_id ? '安装中' : '安装'}</span>
                    </button>
                  )}
                </div>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <div className="skills-page__empty">暂无可显示 Skill</div>
      )}
    </section>
  );
}
