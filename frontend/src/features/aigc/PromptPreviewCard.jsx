import { useId } from 'react';

const SLOT_TYPE_LABELS = Object.freeze({ image: '图片', video: '视频', audio: '音频', voiceover: '配音', caption: '字幕' });
const LANGUAGE_LABELS = Object.freeze({ 'zh-CN': '简体中文', 'en-US': 'English' });

// PromptPreviewCard 只渲染双 Validator 通过的隔离 Draft，不解释 Markdown/HTML 或生产 Prompt 语义。
export function PromptPreviewCard({ preview }) {
  const failureTitleID = `prompt-preview-failure-${useId()}`;
  if (!preview || preview.kind !== 'write_prompts_preview') return null;
  if (preview.status === 'failed') {
    return (
      <article className="prompt-preview-card prompt-preview-card--failed" aria-labelledby={failureTitleID} data-prompt-preview-status="failed">
        <p>开发预览 · 隔离 Prompt Draft · 未审核/未扣费/不可生成媒体</p>
        <h3 id={failureTitleID}>提示词生成未完成</h3>
        <p role="alert">{preview.summary}</p>
        <p>失败类型：{preview.failureKind === 'runtime' ? '运行时失败' : '提示词结果失败'}</p>
        {preview.retryable ? <p>可以稍后重新显式提交。</p> : null}
        <footer>结果码：{preview.resultCode}</footer>
      </article>
    );
  }
  if (preview.status !== 'completed') return null;
  return (
    <article
      className="prompt-preview-card"
      aria-labelledby={`prompt-preview-title-${preview.promptPreviewID}`}
      data-prompt-preview-id={preview.promptPreviewID}
      data-prompt-preview-version={preview.version}
      data-prompt-preview-status="completed"
    >
      <header>
        <p>开发预览 · 隔离 Prompt Draft · 未审核/未扣费/不可生成媒体</p>
        <h3 id={`prompt-preview-title-${preview.promptPreviewID}`}>媒体提示词预览</h3>
        <span>v{preview.version}</span>
      </header>
      <dl>
        <div><dt>Storyboard Source</dt><dd>Draft v{preview.storyboardPreviewRef.version}</dd></div>
        <div><dt>目标槽位</dt><dd>{preview.targetCount}</dd></div>
      </dl>
      <ol aria-label="提示词目标">
        {preview.prompts.map((prompt) => (
          <li key={prompt.targetLocalKey}>
            <header>
              <strong>{prompt.targetLocalKey}</strong>
              <span>{SLOT_TYPE_LABELS[prompt.slotType]} · {LANGUAGE_LABELS[prompt.outputLanguage]}</span>
            </header>
            <p>{prompt.purpose}{prompt.required ? '（必需）' : '（可选）'}</p>
            <section aria-label={`${prompt.targetLocalKey} 正向提示词`}>
              <h4>正向提示词</h4>
              <p>{prompt.positivePrompt}</p>
            </section>
            <section aria-label={`${prompt.targetLocalKey} 负面约束`}>
              <h4>负面约束</h4>
              {prompt.negativeConstraints.length === 0
                ? <p>无</p>
                : <ul>{prompt.negativeConstraints.map((constraint) => <li key={constraint}>{constraint}</li>)}</ul>}
            </section>
          </li>
        ))}
      </ol>
      <footer>结果码：{preview.resultCode} · 最近更新：{preview.updatedAt}</footer>
    </article>
  );
}
