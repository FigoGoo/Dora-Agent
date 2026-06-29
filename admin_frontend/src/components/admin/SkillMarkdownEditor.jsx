import { useId, useRef, useState } from 'react';
import { Code2, Eye } from 'lucide-react';

const REFERENCE_TAG_PATTERN = /(<tool\s+[^>]*id\s*=\s*["'][^"']+["'][^>]*>.*?<\/tool>|<agui\s+[^>]*id\s*=\s*["'][^"']+["'][^>]*>.*?<\/agui>|<ag-ui\s+[^>]*id\s*=\s*["'][^"']+["'][^>]*>.*?<\/ag-ui>)/gis;
const TOOL_TAG_PATTERN = /^<tool\b/i;
const AGUI_TAG_PATTERN = /^<ag-?ui\b/i;
const TAG_PART_PATTERN = /<(tool|agui|ag-ui)\s+[^>]*id\s*=\s*["']([^"']+)["'][^>]*>(.*?)<\/(?:tool|agui|ag-ui)>/is;

function referenceParts(text) {
  return String(text || '')
    .split(REFERENCE_TAG_PATTERN)
    .filter((part) => part !== '');
}

function referenceLabel(tag) {
  const match = String(tag || '').match(TAG_PART_PATTERN);
  return match ? match[3].trim() || match[2].trim() : tag;
}

function referenceKind(tag) {
  if (TOOL_TAG_PATTERN.test(tag)) {
    return 'tool';
  }
  if (AGUI_TAG_PATTERN.test(tag)) {
    return 'agui';
  }
  return '';
}

function renderSource(value) {
  return referenceParts(value).map((part, index) => {
    const kind = referenceKind(part);
    if (!kind) {
      return <span key={index}>{part}</span>;
    }
    return (
      <span key={index} className={`admin-skill-editor-token admin-skill-editor-token--${kind}`}>
        {part}
      </span>
    );
  });
}

function renderPreviewLine(line, index) {
  const heading = line.match(/^(#{1,6})\s+(.+?)\s*$/);
  if (heading) {
    return (
      <div key={index} className="admin-skill-editor-preview__heading">
        {referenceParts(heading[2].replace(/\s+<[^>]+>\s*$/, '')).map((part, partIndex) => renderPreviewPart(part, partIndex))}
      </div>
    );
  }
  const trimmed = line.trim();
  if (!trimmed) {
    return <div key={index} className="admin-skill-editor-preview__spacer" />;
  }
  return (
    <p key={index} className={trimmed.startsWith('-') ? 'admin-skill-editor-preview__list-item' : undefined}>
      {referenceParts(line).map((part, partIndex) => renderPreviewPart(part, partIndex))}
    </p>
  );
}

function renderPreviewPart(part, index) {
  const kind = referenceKind(part);
  if (!kind) {
    return <span key={index}>{part}</span>;
  }
  return (
    <span key={index} className={`admin-skill-editor-chip admin-skill-editor-chip--${kind}`}>
      {referenceLabel(part)}
    </span>
  );
}

export function SkillMarkdownEditor({ label, value, onChange, rows = 18, hint, required, error }) {
  const id = useId();
  const [mode, setMode] = useState('source');
  const highlighterRef = useRef(null);
  const lineHeight = 22;
  const editorHeight = Math.max(220, rows * lineHeight + 24);
  const syncScroll = (event) => {
    if (!highlighterRef.current) {
      return;
    }
    highlighterRef.current.scrollTop = event.currentTarget.scrollTop;
    highlighterRef.current.scrollLeft = event.currentTarget.scrollLeft;
  };

  return (
    <div className="admin-field admin-skill-editor">
      <div className="admin-skill-editor__top">
        <span>{label}</span>
        <div className="admin-skill-editor__tabs" role="group" aria-label={`${label}显示模式`}>
          <button type="button" className={mode === 'text' ? 'is-active' : ''} onClick={() => setMode('text')}>
            <Eye size={14} aria-hidden="true" />
            文本
          </button>
          <button type="button" className={mode === 'source' ? 'is-active' : ''} onClick={() => setMode('source')}>
            <Code2 size={14} aria-hidden="true" />
            源码
          </button>
        </div>
      </div>

      {mode === 'text' ? (
        <div className="admin-skill-editor-preview" style={{ minHeight: editorHeight }}>
          {String(value || '')
            .split(/\r?\n/)
            .map(renderPreviewLine)}
        </div>
      ) : (
        <div className={`admin-skill-editor-source ${error ? 'is-error' : ''}`} style={{ minHeight: editorHeight }}>
          <pre ref={highlighterRef} aria-hidden="true">
            {renderSource(value)}
          </pre>
          <textarea
            id={id}
            aria-label={label}
            value={value || ''}
            onChange={onChange}
            onScroll={syncScroll}
            rows={rows}
            aria-invalid={Boolean(error)}
            aria-required={required || undefined}
            spellCheck={false}
          />
        </div>
      )}

      {error ? <small className="admin-field__error">{error}</small> : null}
      {!error && hint ? <small>{hint}</small> : null}
    </div>
  );
}
