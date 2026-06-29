import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Braces, CheckCircle2, Code2, Save, Tags, Wrench } from 'lucide-react';
import { Link, useNavigate } from 'react-router-dom';
import { AuditHint } from '../../components/admin/AuditHint.jsx';
import { Button } from '../../components/admin/Button.jsx';
import { PageHeader } from '../../components/admin/PageHeader.jsx';
import { useToast } from '../../components/admin/Toast.jsx';
import { adminApi } from '../../services/adminApi.js';
import { readListPayload } from '../../utils/format.js';
import { initialForm, prepareCreateBody, ResourceForm, validateRequiredFields } from './ResourceListPage.jsx';
import { pageConfigs } from './pageConfigs.jsx';

const systemSkillConfig = pageConfigs['skills/system'];
const createConfig = systemSkillConfig.create;

function toolReference(tool) {
  const toolKey = tool.tool_key || [tool.tool_name, tool.tool_type].filter(Boolean).join(':');
  const label = tool.display_name || tool.tool_name || toolKey;
  return `<tool id="${toolKey}">${label}</tool>`;
}

function EditorSidePanel({ tools = [] }) {
  return (
    <aside className="admin-skill-editor-page__side" aria-label="引用与校验">
      <section>
        <h2>引用与校验</h2>
        <p>源码保存结构化标签，文本模式只展示名称；后端会从 Markdown 解析运行时字段。</p>
      </section>
      <section>
        <h3>
          <Tags aria-hidden="true" size={15} />
          中文标签
        </h3>
        <div className="admin-skill-editor-page__chips">
          {['名称', '说明', '输入', '计划', '工具引用', 'AG-UI元素引用', '生成偏好', '提示词写法', '结果输出'].map((tag) => (
            <span key={tag}>{`<${tag}>`}</span>
          ))}
        </div>
      </section>
      <section>
        <h3>
          <Wrench aria-hidden="true" size={15} />
          工具引用
        </h3>
        <div className="admin-skill-editor-page__references">
          {tools.length ? (
            tools.slice(0, 8).map((tool) => (
              <article key={tool.tool_key || `${tool.tool_name}:${tool.tool_type}`}>
                <code>{toolReference(tool)}</code>
                {tool.description ? <p>{tool.description}</p> : null}
              </article>
            ))
          ) : (
            <p>暂无可引用 Tool，请先在 Tool 管理中注册名称和作用说明。</p>
          )}
        </div>
      </section>
      <section>
        <h3>
          <Braces aria-hidden="true" size={15} />
          AG-UI 引用
        </h3>
        <code>{'<agui id="storyboard_panel">故事板面板</agui>'}</code>
      </section>
      <section>
        <h3>
          <CheckCircle2 aria-hidden="true" size={15} />
          保存后生成
        </h3>
        <ul>
          <li>Skill 定义 JSON</li>
          <li>路由触发说明</li>
          <li>输入与输出运行时意图</li>
          <li>工具绑定关系</li>
        </ul>
      </section>
    </aside>
  );
}

export function SystemSkillEditorPage() {
  const [values, setValues] = useState(() => initialForm(createConfig.fields));
  const [errors, setErrors] = useState({});
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const toolsQuery = useQuery({
    queryKey: ['admin-skill-editor-tools'],
    queryFn: () => adminApi.list('/api/admin/tools', { status: 'active', page_size: 50 }),
    staleTime: 60_000
  });
  const tools = readListPayload(toolsQuery.data).items;

  const createMutation = useMutation({
    mutationFn: (body) => adminApi.post(createConfig.path, body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['admin-resource', systemSkillConfig.key] });
      toast?.notify('系统 Skill 草稿已保存');
      navigate('/admin/skills/system');
    },
    onError: (error) => toast?.notify(error.message || '保存失败，请稍后重试。', 'danger', { title: '保存失败', traceId: error.traceId })
  });

  function submitEditor(event) {
    event.preventDefault();
    const nextErrors = validateRequiredFields(createConfig.fields, values);
    if (Object.keys(nextErrors).length) {
      setErrors(nextErrors);
      toast?.notify('请补全必填字段后再保存。', 'warning', { title: '表单未完成' });
      return;
    }
    setErrors({});
    try {
      createMutation.mutate(prepareCreateBody(values, systemSkillConfig));
    } catch (error) {
      toast?.notify(error.message || '表单格式错误，请检查后重试。', 'danger', { title: '表单格式错误', traceId: error.traceId });
    }
  }

  return (
    <>
      <PageHeader
        title="创建系统 Skill 草稿"
        description="使用 Markdown 编写完整 Skill 内容；源码中的中文标签、工具和 AG-UI 引用由后端解析。"
        actions={
          <>
            <Link className="admin-btn admin-btn--secondary admin-btn--md" to="/admin/skills/system">
              <ArrowLeft aria-hidden="true" size={16} />
              <span>返回列表</span>
            </Link>
            <Button type="submit" form="admin-system-skill-editor" variant="primary" icon={Save} loading={createMutation.isPending}>
              保存草稿
            </Button>
          </>
        }
      />
      <div className="admin-skill-editor-page">
        <form id="admin-system-skill-editor" className="admin-skill-editor-page__form" onSubmit={submitEditor} noValidate>
          <div className="admin-skill-editor-page__form-title">
            <Code2 aria-hidden="true" size={16} />
            <span>编辑内容</span>
          </div>
          <ResourceForm
            fields={createConfig.fields}
            values={values}
            setValues={setValues}
            errors={errors}
            onFieldChange={(name) =>
              setErrors((current) => {
                if (!current[name]) {
                  return current;
                }
                const next = { ...current };
                delete next[name];
                return next;
              })
            }
          />
          <AuditHint />
        </form>
        <EditorSidePanel tools={tools} />
      </div>
    </>
  );
}
