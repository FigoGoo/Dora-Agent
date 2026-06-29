import { Blocks, Plus } from 'lucide-react';
import { skillLibraryFilters, skillLibraryTabs, skillMocks } from './skillMocks.js';

export function SkillsPage({ onIntent }) {
  return (
    <section className="mock-page skills-page" aria-labelledby="skills-title">
      <div className="skills-page__bar">
        <div className="skills-page__tabs" role="tablist" aria-label="Skill 视图">
          {skillLibraryTabs.map((tab) => (
            <button className={tab === '我的' ? 'skills-page__tab is-active' : 'skills-page__tab'} type="button" role="tab" aria-selected={tab === '我的'} key={tab}>
              {tab}
            </button>
          ))}
        </div>
        <button className="skills-page__create" type="button" onClick={() => onIntent('创建 Skill', '登录后进入 Skill Builder。')}>
          <Plus aria-hidden="true" size={18} />
          <span>新建Skill</span>
        </button>
      </div>
      <div className="skills-page__filters" aria-label="Skill 筛选">
        {skillLibraryFilters.map((filter) => (
          <button className={filter === '全部' ? 'skills-page__filter is-active' : 'skills-page__filter'} type="button" key={filter}>
            {filter}
          </button>
        ))}
      </div>
      <div className="skills-page__grid" aria-label="Skill 列表">
        {skillMocks.map((skill, index) => {
          const statusClassName = skill.status === '草稿' ? 'is-draft' : skill.status === '审核中' ? 'is-reviewing' : 'is-enabled';

          return (
            <article className={`skill-library-card skill-library-card--${skill.tone}`} data-testid="skill-card" key={`${skill.title}-${index}`}>
              <div className="skill-library-card__media">
                {skill.cover ? <img src={skill.cover} alt="" loading="lazy" /> : <Blocks aria-hidden="true" size={44} />}
                <div className="skill-library-card__models">
                  {skill.models.map((model) => (
                    <span key={model}>{model}</span>
                  ))}
                </div>
              </div>
              <div className="skill-library-card__content">
                <span className="skill-library-card__author">{skill.author}</span>
                <div className="skill-library-card__title-row">
                  <h2>{skill.title}</h2>
                  <span>{skill.version}</span>
                </div>
                <p>{skill.description}</p>
                <div className="skill-library-card__footer">
                  <span>{skill.owner}</span>
                  <strong className={statusClassName}>{skill.status}</strong>
                  <button className="skill-library-card__toggle" type="button" aria-label={`${skill.title} ${skill.status}`}>
                    <span />
                  </button>
                </div>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}
