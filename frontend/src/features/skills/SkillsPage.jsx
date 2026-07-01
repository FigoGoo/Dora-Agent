import { useEffect, useMemo, useState } from 'react';
import { BarChart3, Blocks, Loader2, Play, Plus, Search, Send, Sparkles } from 'lucide-react';
import { creatorApi } from '../../lib/api/creator.js';
import { marketplaceApi } from '../../lib/api/marketplace.js';
import { skillLibraryFilters, skillLibraryTabs, skillMocks } from './skillMocks.js';

const DEFAULT_PAGE_SIZE = 24;
const DEFAULT_CREATOR_ANALYTICS = {
  usage_count: 0,
  revenue_hold_amount: 0,
  refund_count: 0,
  failure_code_summary: {}
};

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

function creatorVersionStatusLabel(status) {
  const labels = {
    draft: '草稿',
    submitted: '待审核',
    reviewing: '审核中',
    rejected: '已拒绝',
    published: '已发布',
    deprecated: '已废弃',
    removed: '已移除'
  };
  return labels[status] || status || '未知';
}

function creatorListingStatusLabel(status) {
  const labels = {
    not_listed: '未上架',
    draft: '上架草稿',
    pending_listing_review: '待上架审核',
    listed: '已上架',
    unlisted: '已下架',
    suspended: '已暂停',
    removed: '已移除'
  };
  return labels[status] || status || '未知';
}

function creatorReviewStatusLabel(status) {
  const labels = {
    not_submitted: '未提交',
    submitted: '已提交',
    reviewing: '审核中',
    approved: '已通过',
    rejected: '已拒绝'
  };
  return labels[status] || status || '未知';
}

export function SkillsPage({ isLoggedIn = false, onIntent, onUseSkill }) {
  const [activeTab, setActiveTab] = useState(skillLibraryTabs[0]);
  const [activeFilter, setActiveFilter] = useState(skillLibraryFilters[0]);
  const [query, setQuery] = useState('');
  const [listings, setListings] = useState(() => skillMocks.map(fallbackListingFromMock));
  const [installations, setInstallations] = useState([]);
  const [loading, setLoading] = useState(false);
  const [installingId, setInstallingId] = useState('');
  const [creatorName, setCreatorName] = useState('');
  const [creatorDescription, setCreatorDescription] = useState('');
  const [creatorItems, setCreatorItems] = useState([]);
  const [creatorAnalytics, setCreatorAnalytics] = useState(DEFAULT_CREATOR_ANALYTICS);
  const [creatorLoading, setCreatorLoading] = useState(false);
  const [creatorSaving, setCreatorSaving] = useState(false);
  const [creatorSubmittingId, setCreatorSubmittingId] = useState('');
  const [creatorNotice, setCreatorNotice] = useState('');

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

  useEffect(() => {
    let alive = true;

    async function loadCreatorPortal() {
      if (!isLoggedIn || activeTab !== '创作台' || typeof fetch !== 'function') {
        return;
      }
      setCreatorLoading(true);
      try {
        const [list, analytics] = await Promise.all([creatorApi.listListings({ page_size: DEFAULT_PAGE_SIZE }), creatorApi.getSkillUsageAnalytics()]);
        if (!alive) {
          return;
        }
        setCreatorItems(Array.isArray(list?.items) ? list.items : []);
        setCreatorAnalytics(analytics || DEFAULT_CREATOR_ANALYTICS);
        setCreatorNotice('');
      } catch {
        if (alive) {
          setCreatorItems([]);
          setCreatorAnalytics(DEFAULT_CREATOR_ANALYTICS);
          setCreatorNotice('创作者后台暂时不可用。');
        }
      } finally {
        if (alive) {
          setCreatorLoading(false);
        }
      }
    }

    loadCreatorPortal();

    return () => {
      alive = false;
    };
  }, [activeTab, isLoggedIn]);

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

  async function createCreatorDraft(event) {
    event.preventDefault();
    const name = creatorName.trim();
    const description = creatorDescription.trim();
    if (!isLoggedIn) {
      onIntent?.('创建 Skill', '登录后进入 Skill Builder。', 'skills');
      return;
    }
    if (!name || !description) {
      setCreatorNotice('请填写 Skill 名称和说明。');
      return;
    }
    setCreatorSaving(true);
    try {
      const result = await creatorApi.createSkillDraft(
        { name, description },
        { idempotencyKey: `creator-draft:${name}:${Date.now()}` }
      );
      if (result?.skill) {
        setCreatorItems((items) => [result.skill, ...items.filter((item) => item.skill_id !== result.skill.skill_id)]);
        setCreatorName('');
        setCreatorDescription('');
        setCreatorNotice('草稿已保存，可提交审核。');
      }
    } catch {
      setCreatorNotice('草稿保存失败，请稍后重试。');
    } finally {
      setCreatorSaving(false);
    }
  }

  async function submitCreatorSkill(skill) {
    if (!isLoggedIn) {
      onIntent?.('提交 Skill 审核', '登录后提交 Skill 版本审核。', 'skills');
      return;
    }
    setCreatorSubmittingId(skill.skill_id);
    try {
      const result = await creatorApi.submitSkillVersion(
        skill.skill_id,
        skill.version,
        {},
        { idempotencyKey: `creator-submit:${skill.skill_id}:${skill.version}` }
      );
      if (result?.skill_version) {
        setCreatorItems((items) => items.map((item) => (item.skill_id === result.skill_version.skill_id ? result.skill_version : item)));
        setCreatorNotice('已提交审核，等待平台确认。');
      }
    } catch {
      setCreatorNotice('提交审核失败，请检查状态后重试。');
    } finally {
      setCreatorSubmittingId('');
    }
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
        {activeTab !== '创作台' ? (
          <label className="skills-page__search">
            <Search aria-hidden="true" size={16} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 Skill" />
          </label>
        ) : null}
        <button
          className="skills-page__create"
          type="button"
          onClick={() => {
            if (isLoggedIn) {
              setActiveTab('创作台');
              return;
            }
            onIntent?.('创建 Skill', '登录后进入 Skill Builder。', 'skills');
          }}
        >
          <Plus aria-hidden="true" size={18} />
          <span>新建Skill</span>
        </button>
      </div>
      {activeTab !== '创作台' ? (
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
      ) : null}
      {activeTab === '创作台' ? (
        <div className="creator-portal" aria-label="创作者 Skill 发布后台">
          {!isLoggedIn ? (
            <div className="creator-portal__login">
              <strong>登录后创建和提交 Skill</strong>
              <button type="button" onClick={() => onIntent?.('创建 Skill', '登录后进入 Skill Builder。', 'skills')}>
                登录后创建
              </button>
            </div>
          ) : (
            <>
              <form className="creator-portal__form" aria-label="创建 Skill 草稿" onSubmit={createCreatorDraft}>
                <label>
                  <span>Skill 名称</span>
                  <input value={creatorName} onChange={(event) => setCreatorName(event.target.value)} maxLength={80} />
                </label>
                <label>
                  <span>Skill 说明</span>
                  <textarea value={creatorDescription} onChange={(event) => setCreatorDescription(event.target.value)} rows={3} maxLength={1000} />
                </label>
                <button type="submit" disabled={creatorSaving}>
                  {creatorSaving ? <Loader2 aria-hidden="true" size={15} /> : <Sparkles aria-hidden="true" size={15} />}
                  <span>{creatorSaving ? '保存中' : '保存草稿'}</span>
                </button>
              </form>
              <div className="creator-portal__summary" aria-label="创作者数据摘要">
                <div>
                  <BarChart3 aria-hidden="true" size={18} />
                  <span>使用次数</span>
                  <strong>{creatorAnalytics.usage_count || 0}</strong>
                </div>
                <div>
                  <BarChart3 aria-hidden="true" size={18} />
                  <span>结算 hold</span>
                  <strong>{creatorAnalytics.revenue_hold_amount || 0}</strong>
                </div>
                <div>
                  <BarChart3 aria-hidden="true" size={18} />
                  <span>退款数</span>
                  <strong>{creatorAnalytics.refund_count || 0}</strong>
                </div>
              </div>
              {creatorNotice ? <div className="creator-portal__notice">{creatorNotice}</div> : null}
              {creatorLoading ? (
                <div className="skills-page__empty">创作台更新中</div>
              ) : creatorItems.length ? (
                <div className="creator-portal__list" aria-label="创作者 Skill 列表">
                  {creatorItems.map((skill) => (
                    <article className="creator-skill-card" data-testid="creator-skill-card" key={skill.skill_id}>
                      <div>
                        <span>{creatorReviewStatusLabel(skill.review_status)}</span>
                        <h2>{skill.name}</h2>
                        <p>{skill.description}</p>
                      </div>
                      <dl>
                        <div>
                          <dt>版本</dt>
                          <dd>{skill.version}</dd>
                        </div>
                        <div>
                          <dt>版本状态</dt>
                          <dd>{creatorVersionStatusLabel(skill.version_status)}</dd>
                        </div>
                        <div>
                          <dt>上架状态</dt>
                          <dd>{creatorListingStatusLabel(skill.listing_status)}</dd>
                        </div>
                      </dl>
                      <button
                        type="button"
                        disabled={creatorSubmittingId === skill.skill_id || !['draft', 'rejected'].includes(skill.version_status)}
                        onClick={() => submitCreatorSkill(skill)}
                      >
                        {creatorSubmittingId === skill.skill_id ? <Loader2 aria-hidden="true" size={15} /> : <Send aria-hidden="true" size={15} />}
                        <span>{skill.version_status === 'submitted' || skill.version_status === 'reviewing' ? '等待审核' : '提交审核'}</span>
                      </button>
                    </article>
                  ))}
                </div>
              ) : (
                <div className="skills-page__empty">暂无创作者 Skill</div>
              )}
            </>
          )}
        </div>
      ) : visibleListings.length ? (
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
