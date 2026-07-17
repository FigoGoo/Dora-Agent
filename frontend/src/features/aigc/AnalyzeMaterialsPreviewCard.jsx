// AnalyzeMaterialsPreviewCard 只渲染严格解析后的非权威安全投影，不解释 Markdown/HTML。
export function AnalyzeMaterialsPreviewCard({ preview }) {
  if (!preview || preview.kind !== 'analyze_materials_preview') return null;
  const failed = preview.status === 'failed';
  const title = failed
    ? (preview.failureKind === 'runtime' ? '素材分析运行未完成' : '素材分析未完成')
    : (preview.status === 'partial' ? '素材分析部分完成' : '素材分析已完成');
  return (
    <article
      className={`turn-output-card turn-output-card--${preview.status}`}
      aria-labelledby={`analyze-materials-title-${preview.turnID}`}
      data-turn-output-status={preview.status}
      data-turn-id={preview.turnID}
      data-tool-call-id={preview.toolCallID}
    >
      <p>开发预览 · 非权威结果 · 不会写入素材分析资源</p>
      <h2 id={`analyze-materials-title-${preview.turnID}`}>{title}</h2>
      {failed ? (
        <>
          <p role="alert">{preview.summary}</p>
          <p>失败类型：{preview.failureKind === 'runtime' ? '运行时失败' : '素材分析结果'}</p>
          {preview.retryable ? <p>可以稍后重新显式提交。</p> : null}
        </>
      ) : (
        <>
          <dl>
            <div><dt>覆盖状态</dt><dd>{preview.status === 'completed' ? '完整' : '部分'}</dd></div>
            <div><dt>目标素材</dt><dd>{preview.coverage.targetAssetIDs.length}</dd></div>
            <div><dt>证据引用</dt><dd>{preview.evidenceRefs.length}</dd></div>
            <div><dt>缺失要求</dt><dd>{preview.coverage.missingRequirements.length}</dd></div>
          </dl>
          <section aria-labelledby={`analyze-materials-summary-${preview.turnID}`}>
            <h3 id={`analyze-materials-summary-${preview.turnID}`}>素材摘要</h3>
            <ul>
              {preview.analysis.assetSummaries.map((summary) => (
                <li key={summary.assetID}>{summary.summary}</li>
              ))}
            </ul>
          </section>
          {preview.analysis.risks.length > 0 ? (
            <section aria-labelledby={`analyze-materials-risks-${preview.turnID}`}>
              <h3 id={`analyze-materials-risks-${preview.turnID}`}>风险提示</h3>
              <ul>{preview.analysis.risks.map((risk) => <li key={risk.riskID}>{risk.statement}</li>)}</ul>
            </section>
          ) : null}
        </>
      )}
      <footer>结果码：{preview.resultCode}</footer>
    </article>
  );
}
