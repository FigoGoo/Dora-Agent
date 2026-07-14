import { useEffect, useId, useRef, useState } from 'react';
import {
  createOwnerSkill,
  createSkillCommandKey,
  getOwnerSkill,
  submitOwnerSkillReview,
  updateOwnerSkillDraft
} from './skillApi.js';
import {
  createEmptySkillDefinition,
  compareExamples,
  compareUTF8,
  parseSkillDefinition,
  SKILL_CAPABILITY_FIELDS,
  SKILL_CAPABILITY_LABELS,
  SkillContractError
} from './skillContract.js';
import { createSkillCommandLedger, useOptionalSkillCommandLedger } from './skillCommandLedger.jsx';

const DEFAULT_CLIENT = Object.freeze({
  create: createOwnerSkill,
  get: getOwnerSkill,
  update: updateOwnerSkillDraft,
  submitReview: submitOwnerSkillReview
});
const REVIEW_STATUS_LABELS = Object.freeze({
  reviewing: '审核中', approved: '审核通过', rejected: '审核驳回', withdrawn: '已撤回'
});
const GOVERNANCE_STATUS_LABELS = Object.freeze({ active: '可用', suspended: '已暂停', offline: '已下架' });

export function SkillBuilderPage({
  skillID = '',
  csrfToken,
  client = DEFAULT_CLIENT,
  commandLedger: injectedCommandLedger,
  onNavigate = navigate
}) {
  const [definition, setDefinition] = useState(() => createEmptySkillDefinition());
  const [ownerSkill, setOwnerSkill] = useState(null);
  const [loadState, setLoadState] = useState(skillID ? 'loading' : 'ready');
  const [saveState, setSaveState] = useState('idle');
  const [reviewState, setReviewState] = useState('idle');
  const [dirty, setDirty] = useState(false);
  const [fieldErrors, setFieldErrors] = useState({});
  const [error, setError] = useState(null);
  const generationRef = useRef(0);
  const saveGenerationRef = useRef(0);
  const reviewGenerationRef = useRef(0);
  const saveControllerRef = useRef(null);
  const reviewControllerRef = useRef(null);
  const inheritedCommandLedger = useOptionalSkillCommandLedger();
  const localCommandLedgerRef = useRef(null);
  if (!localCommandLedgerRef.current) localCommandLedgerRef.current = createSkillCommandLedger();
  const commandLedger = injectedCommandLedger || inheritedCommandLedger || localCommandLedgerRef.current;

  useEffect(() => {
    if (!skillID) {
      generationRef.current += 1;
      setDefinition(createEmptySkillDefinition());
      setOwnerSkill(null);
      setDirty(false);
      setFieldErrors({});
      setError(null);
      setLoadState('ready');
      return undefined;
    }
    const controller = new AbortController();
    const generation = ++generationRef.current;
    setLoadState('loading');
    client.get(skillID, { signal: controller.signal }).then((result) => {
      if (generation !== generationRef.current || controller.signal.aborted) return;
      const pendingReview = commandLedger.get(reviewCommandScope(skillID));
      if (
        pendingReview
        && result.skill.draftETag === pendingReview.draftETag
        && result.skill.reviewStatus === 'reviewing'
      ) {
        commandLedger.clear(reviewCommandScope(skillID), pendingReview.key);
      }
      setOwnerSkill(result.skill);
      setDefinition(result.skill.definition);
      setDirty(false);
      setLoadState('ready');
    }).catch((caught) => {
      if (generation !== generationRef.current || controller.signal.aborted || caught?.name === 'AbortError') return;
      setError(publicError(caught));
      setLoadState('error');
    });
    return () => {
      controller.abort();
      generationRef.current += 1;
    };
  }, [client, commandLedger, skillID]);

  useEffect(() => () => {
    saveGenerationRef.current += 1;
    reviewGenerationRef.current += 1;
    saveControllerRef.current?.abort();
    reviewControllerRef.current?.abort();
    saveControllerRef.current = null;
    reviewControllerRef.current = null;
  }, [skillID]);

  function updateField(field, value) {
    setDefinition((current) => ({ ...current, [field]: value }));
    markDirty(`definition.${field}`);
  }

  function updateCapability(field, patch) {
    setDefinition((current) => ({ ...current, [field]: { ...current[field], ...patch } }));
    markDirty(`definition.${field}`);
  }

  function addExample() {
    setDefinition((current) => ({
      ...current,
      examples: [...current.examples, { input: '', output: '' }]
    }));
    markDirty('definition.examples');
  }

  function updateExample(index, patch) {
    setDefinition((current) => ({
      ...current,
      examples: current.examples.map((example, offset) => (
        offset === index ? { ...example, ...patch } : example
      ))
    }));
    markDirty(`definition.examples[${index}]`);
  }

  function removeExample(index) {
    setDefinition((current) => ({
      ...current,
      examples: current.examples.filter((_, offset) => offset !== index)
    }));
    markDirty('definition.examples');
  }

  function markDirty(field) {
    setDirty(true);
    setSaveState('idle');
    setReviewState('idle');
    setError(null);
    setFieldErrors((current) => {
      const next = { ...current };
      Object.keys(next).forEach((errorField) => {
        if (errorField === field || errorField.startsWith(`${field}.`) || errorField.startsWith(`${field}[`)) {
          delete next[errorField];
        }
      });
      return next;
    });
  }

  async function save(event) {
    event.preventDefault();
    if (formLocked) return;
    setFieldErrors({});
    setError(null);
    let normalized;
    try {
      normalized = parseSkillDefinition(normalizeDefinition(definition));
    } catch (caught) {
      setSaveState('invalid');
      setFieldErrors({ [caught.field || 'definition']: caught.message });
      return;
    }
    let createCommand = null;
    if (!skillID) {
      const semantic = JSON.stringify(normalized);
      createCommand = commandLedger.get(createCommandScope());
      if (createCommand && createCommand.semantic !== semantic) {
        setSaveState('semantic_conflict');
        setError(commandSemanticError('create'));
        return;
      }
      if (!createCommand) {
        createCommand = commandLedger.set(createCommandScope(), {
          key: createSkillCommandKey('skill-create'),
          semantic,
          definition: normalized
        });
      }
    }
    setSaveState('saving');
    saveControllerRef.current?.abort();
    const controller = new AbortController();
    const generation = ++saveGenerationRef.current;
    saveControllerRef.current = controller;
    try {
      const result = skillID
        ? await client.update({
            skillID,
            definition: normalized,
            draftETag: ownerSkill?.draftETag,
            csrfToken,
            signal: controller.signal
          })
        : await client.create({
            definition: createCommand.definition,
            idempotencyKey: createCommand.key,
            csrfToken,
            signal: controller.signal
          });
      if (!skillID) commandLedger.clear(createCommandScope(), createCommand.key);
      if (generation !== saveGenerationRef.current || controller.signal.aborted) return;
      setOwnerSkill(result.skill);
      setDefinition(result.skill.definition);
      setDirty(false);
      setSaveState('saved');
      if (!skillID) {
        onNavigate(`/my/skills/${encodeURIComponent(result.skill.skillID)}/edit`);
      }
    } catch (caught) {
      if (!skillID && isDefinitiveCommandRejection(caught)) {
        commandLedger.clear(createCommandScope(), createCommand?.key);
      }
      if (generation !== saveGenerationRef.current || controller.signal.aborted || caught?.name === 'AbortError') return;
      applyServerErrors(caught, setFieldErrors);
      setError(publicError(caught, skillID ? 'update' : 'create'));
      setSaveState(Number(caught?.status) === 409 ? 'conflict' : 'error');
    } finally {
      if (saveControllerRef.current === controller) saveControllerRef.current = null;
    }
  }

  async function submitReview() {
    if (!skillID || dirty || !ownerSkill?.allowedActions.includes('submit_review')) return;
    const draftETag = ownerSkill.draftETag;
    const semantic = `${skillID}\u0000${draftETag}`;
    const scope = reviewCommandScope(skillID);
    let reviewCommand = commandLedger.get(scope);
    if (reviewCommand && reviewCommand.semantic !== semantic) {
      setReviewState('semantic_conflict');
      setError(commandSemanticError('review'));
      return;
    }
    if (!reviewCommand) {
      reviewCommand = commandLedger.set(scope, {
        key: createSkillCommandKey('skill-review'),
        semantic,
        draftETag
      });
    }
    setReviewState('submitting');
    setError(null);
    reviewControllerRef.current?.abort();
    const controller = new AbortController();
    const generation = ++reviewGenerationRef.current;
    reviewControllerRef.current = controller;
    try {
      const result = await client.submitReview({
        skillID,
        idempotencyKey: reviewCommand.key,
        draftETag: reviewCommand.draftETag,
        csrfToken,
        signal: controller.signal
      });
      commandLedger.clear(scope, reviewCommand.key);
      if (generation !== reviewGenerationRef.current || controller.signal.aborted) return;
      setOwnerSkill(result.skill);
      setDefinition(result.skill.definition);
      setReviewState('submitted');
    } catch (caught) {
      if (isDefinitiveCommandRejection(caught)) commandLedger.clear(scope, reviewCommand.key);
      if (generation !== reviewGenerationRef.current || controller.signal.aborted || caught?.name === 'AbortError') return;
      applyServerErrors(caught, setFieldErrors);
      setError(publicError(caught, 'review'));
      setReviewState('error');
    } finally {
      if (reviewControllerRef.current === controller) reviewControllerRef.current = null;
    }
  }

  if (loadState === 'loading') return <section className="skill-builder-page"><p role="status">正在加载 Skill 草稿…</p></section>;
  if (loadState === 'error') {
    return (
      <section className="skill-builder-page">
        <p role="alert">{error?.message}</p>
        <button type="button" className="secondary-button" onClick={() => onNavigate('/my/skills')}>返回我的 Skill</button>
      </section>
    );
  }

  const canEdit = !skillID || ownerSkill?.allowedActions.includes('edit_draft');
  const formLocked = !canEdit || saveState === 'saving' || reviewState === 'submitting';
  return (
    <section className="skill-builder-page" aria-labelledby="skill-builder-title">
      <header className="skill-builder-page__header">
        <div>
          <h2 id="skill-builder-title">{skillID ? '编辑 Skill 草稿' : '创建 Skill'}</h2>
          <p>六个能力字段彼此独立；当前基础阶段不允许添加公共 Tool 引用。</p>
        </div>
        <button type="button" className="ghost-link" onClick={() => onNavigate('/my/skills')}>返回我的 Skill</button>
      </header>

      {ownerSkill ? <OwnerSkillStatus skill={ownerSkill} /> : null}
      {saveState === 'conflict' ? <p role="alert">{error?.message}</p> : null}
      {error && saveState !== 'conflict' ? <p role="alert">{error.message}</p> : null}
      {saveState === 'saved' ? <p role="status">草稿已保存</p> : null}
      {reviewState === 'submitted' ? <p role="status">已提交审核，后续编辑不会改变本次审核内容。</p> : null}

      <form className="skill-builder-form" onSubmit={save} noValidate>
        <fieldset className="skill-builder-form__fields" disabled={formLocked}>
        <BuilderTextField label="Skill 名称" value={definition.name} onChange={(value) => updateField('name', value)} error={fieldError(fieldErrors, 'definition.name')} required />
        <BuilderTextField label="简介" value={definition.summary} onChange={(value) => updateField('summary', value)} error={fieldError(fieldErrors, 'definition.summary')} multiline />
        <BuilderTextField label="分类" value={definition.category} onChange={(value) => updateField('category', value)} error={fieldError(fieldErrors, 'definition.category')} />
        <BuilderTextField label="标签（逗号分隔）" value={definition.tags.join(', ')} onChange={(value) => updateField('tags', splitCommaList(value))} error={fieldError(fieldErrors, 'definition.tags')} />
        <BuilderTextField label="输入说明" value={definition.input_description} onChange={(value) => updateField('input_description', value)} error={fieldError(fieldErrors, 'definition.input_description')} multiline />
        <BuilderTextField label="输出说明" value={definition.output_description} onChange={(value) => updateField('output_description', value)} error={fieldError(fieldErrors, 'definition.output_description')} multiline />
        <BuilderTextField label="Skill 调用规则" value={definition.invocation_rules} onChange={(value) => updateField('invocation_rules', value)} error={fieldError(fieldErrors, 'definition.invocation_rules')} multiline />

        <section className="skill-capability-grid" aria-labelledby="skill-capability-title">
          <h3 id="skill-capability-title">六个能力字段</h3>
          {SKILL_CAPABILITY_FIELDS.map((field) => (
            <CapabilityField
              key={field}
              field={field}
              value={definition[field]}
              error={fieldErrorEntry(fieldErrors, `definition.${field}`)}
              onChange={(patch) => updateCapability(field, patch)}
            />
          ))}
        </section>

        <section className="skill-example-editor" aria-labelledby="skill-example-title">
          <div className="skill-example-editor__header">
            <div>
              <h3 id="skill-example-title">输入输出示例</h3>
              <p>每一项输入与输出都独立保存，提交时按冻结 UTF-8 字节序规范化。</p>
            </div>
            <button type="button" className="secondary-button" onClick={addExample}>添加示例</button>
          </div>
          {fieldErrors['definition.examples'] ? (
            <p className="skill-field-error" role="alert">{fieldErrors['definition.examples']}</p>
          ) : null}
          {definition.examples.length === 0 ? <p>尚未添加示例。</p> : null}
          <div className="skill-example-editor__list">
            {definition.examples.map((example, index) => (
              <fieldset className="skill-example-editor__item" key={`example-${index}`}>
                <legend>示例 {index + 1}</legend>
                <BuilderTextField
                  label={`示例 ${index + 1} 输入`}
                  value={example.input}
                  onChange={(value) => updateExample(index, { input: value })}
                  error={fieldError(fieldErrors, `definition.examples[${index}].input`)}
                  multiline
                  required
                />
                <BuilderTextField
                  label={`示例 ${index + 1} 输出`}
                  value={example.output}
                  onChange={(value) => updateExample(index, { output: value })}
                  error={fieldError(fieldErrors, `definition.examples[${index}].output`)}
                  multiline
                  required
                />
                <button type="button" className="ghost-link" onClick={() => removeExample(index)}>
                  删除示例 {index + 1}
                </button>
              </fieldset>
            ))}
          </div>
        </section>
        <BuilderTextField label="开场提示（每行一条）" value={definition.starter_prompts.join('\n')} onChange={(value) => updateField('starter_prompts', value.split('\n'))} error={fieldError(fieldErrors, 'definition.starter_prompts')} multiline />
        <BuilderTextField label="市场详情" value={definition.market_listing.detail} onChange={(value) => updateField('market_listing', { ...definition.market_listing, detail: value })} error={fieldError(fieldErrors, 'definition.market_listing.detail')} multiline />
        <BuilderTextField label="版权声明" value={definition.market_listing.copyright_notice} onChange={(value) => updateField('market_listing', { ...definition.market_listing, copyright_notice: value })} error={fieldError(fieldErrors, 'definition.market_listing.copyright_notice')} multiline />
        <BuilderTextField label="用户须知" value={definition.market_listing.user_notice} onChange={(value) => updateField('market_listing', { ...definition.market_listing, user_notice: value })} error={fieldError(fieldErrors, 'definition.market_listing.user_notice')} multiline />

        <section className="skill-builder-form__security" aria-label="公共 Tool 引用状态">
          <strong>公共 Tool 引用</strong>
          <span>当前 W1 基础阶段不可添加，提交值固定为空集合。</span>
          {fieldError(fieldErrors, 'definition.public_tool_refs') ? (
            <span className="skill-field-error" role="alert">
              {fieldError(fieldErrors, 'definition.public_tool_refs')}
            </span>
          ) : null}
        </section>
        </fieldset>

        <div className="skill-builder-form__actions">
          <button className="start-button" type="submit" disabled={formLocked}>
            {saveState === 'saving' ? '正在保存…' : skillID ? '保存草稿' : '创建草稿'}
          </button>
          {skillID ? (
            <button
              className="secondary-button"
              type="button"
              disabled={dirty || saveState === 'saving' || reviewState === 'submitting' || !ownerSkill?.allowedActions.includes('submit_review')}
              onClick={submitReview}
            >
              {reviewState === 'submitting' ? '正在提交…' : '提交审核'}
            </button>
          ) : null}
        </div>
      </form>
    </section>
  );
}

function CapabilityField({ field, value, error, onChange }) {
  const label = SKILL_CAPABILITY_LABELS[field];
  const enabled = value.applicability === 'enabled';
  const controlID = useId();
  const errorID = `${controlID}-error`;
  const selectInvalid = Boolean(error?.field.endsWith('.applicability'));
  const guidanceInvalid = Boolean(error) && !selectInvalid;
  return (
    <fieldset className="skill-capability-field">
      <legend>{label}</legend>
      <label>
        <span>适用性</span>
        <select
          id={`${controlID}-applicability`}
          aria-label={`${label}适用性`}
          aria-invalid={selectInvalid}
          aria-describedby={selectInvalid ? errorID : undefined}
          required
          value={value.applicability}
          onChange={(event) => onChange(event.target.value === 'enabled'
            ? { applicability: 'enabled', guidance: '', not_applicable_reason: '' }
            : { applicability: 'not_applicable', guidance: '', not_applicable_reason: '' })}
        >
          <option value="enabled">适用</option>
          <option value="not_applicable">不适用</option>
        </select>
      </label>
      <label>
        <span>{enabled ? '业务指导' : '不适用原因'}</span>
        <textarea
          id={`${controlID}-guidance`}
          aria-label={`${label}${enabled ? '业务指导' : '不适用原因'}`}
          aria-invalid={guidanceInvalid}
          aria-describedby={guidanceInvalid ? errorID : undefined}
          required
          rows={4}
          value={enabled ? value.guidance : value.not_applicable_reason}
          onChange={(event) => onChange(enabled
            ? { guidance: event.target.value, not_applicable_reason: '' }
            : { guidance: '', not_applicable_reason: event.target.value })}
        />
      </label>
      {error ? <span id={errorID} className="skill-field-error" role="alert">{error.message}</span> : null}
    </fieldset>
  );
}

function BuilderTextField({ label, value, onChange, error, multiline = false, required = false }) {
  const Control = multiline ? 'textarea' : 'input';
  const controlID = useId();
  const errorID = `${controlID}-error`;
  return (
    <label className="skill-builder-field" htmlFor={controlID}>
      <span>{label}{required ? ' *' : ''}</span>
      <Control
        id={controlID}
        value={value}
        rows={multiline ? 3 : undefined}
        required={required}
        aria-invalid={Boolean(error)}
        aria-describedby={error ? errorID : undefined}
        onChange={(event) => onChange(event.target.value)}
      />
      {error ? <span id={errorID} className="skill-field-error" role="alert">{error}</span> : null}
    </label>
  );
}

function OwnerSkillStatus({ skill }) {
  return (
    <dl className="skill-builder-status" aria-label="Skill 当前状态">
      <div><dt>内容</dt><dd>{skill.contentStatus === 'published' ? '已发布' : '草稿'}</dd></div>
      {skill.hasUnpublishedChanges ? <div><dt>修改</dt><dd>有未发布修改</dd></div> : null}
      {skill.reviewStatus ? <div><dt>审核</dt><dd>{REVIEW_STATUS_LABELS[skill.reviewStatus]}</dd></div> : null}
      <div><dt>治理</dt><dd>{GOVERNANCE_STATUS_LABELS[skill.governanceStatus]}</dd></div>
    </dl>
  );
}

function normalizeDefinition(definition) {
  const normalized = {
    ...definition,
    schema_version: 'skill_definition.v1',
    name: normalizeText(definition.name),
    summary: normalizeText(definition.summary),
    category: normalizeText(definition.category),
    tags: uniqueNormalized(definition.tags),
    input_description: normalizeText(definition.input_description),
    output_description: normalizeText(definition.output_description),
    invocation_rules: normalizeText(definition.invocation_rules),
    examples: normalizeExamples(definition.examples),
    starter_prompts: uniqueNormalized(definition.starter_prompts),
    market_listing: {
      cover_asset_id: null,
      detail: normalizeText(definition.market_listing.detail),
      copyright_notice: normalizeText(definition.market_listing.copyright_notice),
      user_notice: normalizeText(definition.market_listing.user_notice)
    },
    public_tool_refs: []
  };
  SKILL_CAPABILITY_FIELDS.forEach((field) => {
    const capability = definition[field];
    normalized[field] = capability.applicability === 'enabled'
      ? { applicability: 'enabled', guidance: normalizeText(capability.guidance), not_applicable_reason: '' }
      : { applicability: 'not_applicable', guidance: '', not_applicable_reason: normalizeText(capability.not_applicable_reason) };
  });
  return normalized;
}

function normalizeText(value) {
  return String(value || '').normalize('NFC').trim();
}

function uniqueNormalized(items) {
  return [...new Set((items || []).map(normalizeText).filter(Boolean))].sort(compareUTF8);
}

function splitCommaList(value) {
  return String(value).split(/[，,]/);
}

function normalizeExamples(items) {
  const unique = new Map();
  (items || []).forEach((example) => {
    const normalized = { input: normalizeText(example.input), output: normalizeText(example.output) };
    const key = JSON.stringify([normalized.input, normalized.output]);
    if (!unique.has(key)) unique.set(key, normalized);
  });
  return [...unique.values()].sort(compareExamples);
}

function fieldError(errors, prefix) {
  return fieldErrorEntry(errors, prefix)?.message || '';
}

function fieldErrorEntry(errors, prefix) {
  const matched = Object.entries(errors).find(([field]) => (
    field === prefix || field.startsWith(`${prefix}.`) || field.startsWith(`${prefix}[`)
  ));
  return matched ? { field: matched[0], message: matched[1] } : null;
}

function applyServerErrors(error, setFieldErrors) {
  const details = error?.details;
  const candidates = Array.isArray(details)
    ? details
    : details?.field_errors || details?.fields || details?.errors;
  if (!Array.isArray(candidates)) return;
  const next = {};
  candidates.forEach((item) => {
    if (item?.field && item?.message) {
      const path = serverFieldDisplayPath(normalizeDefinitionFieldPath(item.field));
      if (!next[path]) next[path] = String(item.message);
    }
  });
  if (Object.keys(next).length) setFieldErrors(next);
}

function serverFieldDisplayPath(path) {
  return /^definition\.examples\[\d+\](?:\.|$)/.test(path) ? 'definition.examples' : path;
}

function createCommandScope() {
  return 'skill:create';
}

function reviewCommandScope(skillID) {
  return `skill:review:${skillID}`;
}

function normalizeDefinitionFieldPath(field) {
  const value = String(field).trim();
  if (!value || value === 'definition') return 'definition';
  if (value.startsWith('definition.') || value.startsWith('definition[')) return value;
  return `definition.${value}`;
}

function publicError(error, operation = 'request') {
  if (error instanceof SkillContractError) {
    return { message: error.message, code: error.code, requestID: '', status: error.status };
  }
  const status = Number(error?.status) || 0;
  const code = String(error?.code || 'SKILL_REQUEST_FAILED');
  if (status === 409) {
    return {
      message: conflictMessage(code, operation),
      code,
      requestID: String(error?.requestID || ''),
      status
    };
  }
  return {
    message: String(error?.message || 'Skill 请求暂时失败。'),
    code,
    requestID: String(error?.requestID || ''),
    status
  };
}

function conflictMessage(code, operation) {
  if (code === 'SKILL_DRAFT_CONFLICT') {
    return '草稿已在其他位置更新，本地表单已保留。请刷新并核对服务端草稿后再保存。';
  }
  if (code === 'IDEMPOTENCY_CONFLICT') {
    return '幂等请求与首次提交语义冲突；已保留首次 Key 绑定，不会自动换 Key 重发。';
  }
  if (code === 'SKILL_REVIEW_CONFLICT') {
    return '审核状态已发生变化，本地草稿未被修改。请刷新最新审核状态后再操作。';
  }
  return operation === 'review'
    ? '提交审核发生冲突，本地草稿未被修改。请刷新状态后重试。'
    : '请求发生冲突，本地表单已保留。请刷新最新状态后重试。';
}

function commandSemanticError(operation) {
  return {
    code: operation === 'create' ? 'SKILL_CREATE_OUTCOME_UNKNOWN' : 'SKILL_REVIEW_OUTCOME_UNKNOWN',
    message: operation === 'create'
      ? '上一次创建结果仍未知，幂等 Key 已与首次规范化内容绑定。请恢复首次内容重试，不能用新内容或新 Key 重发。'
      : '上一次提审结果仍未知，幂等 Key 已与首次 draft_etag 绑定。当前草稿已变化，不能自动换 Key 再次提审。',
    requestID: '',
    status: 0
  };
}

function isDefinitiveCommandRejection(error) {
  const status = Number(error?.status) || 0;
  return status >= 400 && status < 500 && ![408, 409, 425, 429].includes(status);
}

function navigate(path) {
  window.history.pushState({}, '', path);
  window.dispatchEvent(new Event('dora:navigate'));
}
