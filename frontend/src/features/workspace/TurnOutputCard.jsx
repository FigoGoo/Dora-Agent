import { AnalyzeMaterialsPreviewCard } from '../aigc/AnalyzeMaterialsPreviewCard.jsx';

export function TurnOutputCard({ output, onOpenToolbox = focusToolbox }) {
  if (!output) return null;
  if (output.kind === 'analyze_materials_preview') {
    return <AnalyzeMaterialsPreviewCard preview={output} />;
  }
  if (output.kind === 'direct_response') {
    return (
      <article
        className="turn-output-card turn-output-card--completed"
        aria-labelledby="turn-output-card-title"
        data-turn-output-status={output.status}
        data-turn-id={output.turnID}
      >
        <h2 id="turn-output-card-title">需求已接收</h2>
        <p>{output.summary}</p>
        <button type="button" className="start-button" onClick={onOpenToolbox}>打开工具箱</button>
      </article>
    );
  }
  if (output.kind === 'failure') {
    const recovering = output.status === 'recovery_pending';
    return (
      <article
        className={`turn-output-card turn-output-card--${output.status}`}
        aria-labelledby="turn-output-card-title"
        data-turn-output-status={output.status}
        data-turn-id={output.turnID}
      >
        <h2 id="turn-output-card-title">{recovering ? '正在恢复处理' : '处理未完成'}</h2>
        <p role={recovering ? 'status' : 'alert'}>{output.summary}</p>
        {output.retryable && !recovering ? <p>你可以稍后重新提交需求。</p> : null}
      </article>
    );
  }
  return null;
}

function focusToolbox() {
  const toolbox = document.getElementById('workspace-toolbox');
  if (!toolbox) return;
  toolbox.scrollIntoView?.({ behavior: 'smooth', block: 'start' });
  toolbox.focus({ preventScroll: true });
}
