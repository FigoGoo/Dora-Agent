import { Plus } from 'lucide-react';
import { projectMocks } from './projectMocks.js';

export function ProjectsPage({ onIntent }) {
  return (
    <section className="mock-page projects-page" aria-labelledby="projects-title">
      <div className="project-gallery" aria-label="项目列表">
        <button
          className="project-tile project-tile--create"
          data-testid="project-card"
          type="button"
          aria-label="新建项目"
          onClick={() => onIntent('新建项目', '登录后会从 Prompt 创建新项目。')}
        >
          <span className="project-tile__cover">
            <span className="project-tile__empty">
              <span className="project-tile__plus">
                <Plus aria-hidden="true" size={22} />
              </span>
              <strong>创建新项目</strong>
            </span>
          </span>
          <span className="project-tile__body">
            <strong>新建项目</strong>
            <small>开启您的创作之旅</small>
          </span>
        </button>
        {projectMocks.map((project) => (
          <button
            className="project-tile"
            data-testid="project-card"
            type="button"
            aria-label={`继续创作 ${project.title}`}
            key={project.title}
            onClick={() => onIntent(`继续创作 ${project.title}`, '进入项目后会恢复最近会话和资产上下文。')}
          >
            <span className="project-tile__cover">
              <img src={project.cover} alt="" loading="lazy" />
            </span>
            <span className="project-tile__body">
              <strong>{project.title}</strong>
              <small>{project.meta}</small>
            </span>
          </button>
        ))}
      </div>
    </section>
  );
}
