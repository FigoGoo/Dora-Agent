import { Blocks, Plus } from 'lucide-react';

// 公开市场 HTTP 尚未注册时只显示真实不可用状态，禁止用生产 Mock 冒充市场数据。
export function SkillsPage({ isLoggedIn, onLogin, onNavigate = navigate }) {
  return (
    <section className="skills-page" aria-labelledby="skills-title">
      <div className="skills-page__bar">
        <div>
          <h2>Skill 市场</h2>
          <p>发现和使用平台审核发布的 Skill。</p>
        </div>
        <div className="skills-page__actions">
          <button className="secondary-button" type="button" onClick={() => (
            isLoggedIn ? onNavigate('/my/skills') : onLogin('查看我的 Skill', '登录后管理自己创建和发布的 Skill。', 'mySkills')
          )}>我的 Skill</button>
          <button className="skills-page__create" type="button" onClick={() => (
            isLoggedIn ? onNavigate('/my/skills/new') : onLogin('创建 Skill', '登录后进入结构化 Skill Builder。', 'mySkills')
          )}>
            <Plus aria-hidden="true" size={18} />
            <span>创建 Skill</span>
          </button>
        </div>
      </div>
      <section className="skill-state-panel" aria-label="Skill 市场状态">
        <Blocks aria-hidden="true" size={44} />
        <h3>Skill 市场暂未开放</h3>
        <p role="status">公开读接口将在真实发布与治理权限接线后开放；当前不会展示静态或伪造 Skill。</p>
      </section>
    </section>
  );
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
