import { RefreshCw, Sparkles, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { listOwnerSkills } from '../skills/skillApi.js';
import { PROJECT_QUICK_CREATE_MAX_SKILL_COUNT } from './projectQuickCreate.js';

const INITIAL_STATE = Object.freeze({ kind: 'idle', items: [], error: null });
const MAX_LIST_PAGES = 100;
const EMPTY_RETAINED_SKILL_IDS = Object.freeze([]);

export function QuickCreateSkillPicker({
  isAuthenticated,
  isDisabled = false,
  selectedSkillIDs,
  retainedSkillIDs = EMPTY_RETAINED_SKILL_IDS,
  onChange,
  onLogin,
  loadSkills = listOwnerSkills
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [state, setState] = useState(INITIAL_STATE);
  const requestRef = useRef(null);
  const generationRef = useRef(0);
  const triggerRef = useRef(null);
  const closeButtonRef = useRef(null);
  const selectedRef = useRef(selectedSkillIDs);
  const retainedRef = useRef(retainedSkillIDs);
  const onChangeRef = useRef(onChange);

  selectedRef.current = selectedSkillIDs;
  retainedRef.current = retainedSkillIDs;
  onChangeRef.current = onChange;

  const load = useCallback(async () => {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const generation = ++generationRef.current;
    setState((current) => ({ ...current, kind: 'loading', error: null }));
    try {
      const items = await loadAllOwnerSkills(loadSkills, controller.signal);
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setState({ kind: 'ready', items, error: null });
      const selectableIDs = new Set(items.filter(isSelectableSkill).map((skill) => skill.skillID));
      retainedRef.current.forEach((skillID) => selectableIDs.add(skillID));
      const nextSelection = selectedRef.current.filter((skillID) => selectableIDs.has(skillID));
      if (!sameIDs(nextSelection, selectedRef.current)) {
        onChangeRef.current(nextSelection);
      }
    } catch (error) {
      if (generation !== generationRef.current || controller.signal.aborted || error?.name === 'AbortError') return;
      setState({ kind: 'error', items: [], error: publicError(error) });
    } finally {
      if (requestRef.current === controller) requestRef.current = null;
    }
  }, [loadSkills]);

  useEffect(() => {
    if (!isOpen || !isAuthenticated) return undefined;
    void load();
    closeButtonRef.current?.focus();
    return () => {
      generationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
    };
  }, [isAuthenticated, isOpen, load]);

  useEffect(() => {
    if (isDisabled) setIsOpen(false);
  }, [isDisabled]);

  useEffect(() => {
    if (!isAuthenticated) {
      generationRef.current += 1;
      requestRef.current?.abort();
      requestRef.current = null;
      setIsOpen(false);
      setState(INITIAL_STATE);
    }
  }, [isAuthenticated]);

  useEffect(() => () => {
    generationRef.current += 1;
    requestRef.current?.abort();
  }, []);

  function handleTrigger() {
    if (!isAuthenticated) {
      onLogin('选择 Skill', '登录后可以把已发布的 Skill 绑定到本次创作。');
      return;
    }
    setIsOpen((current) => !current);
  }

  function closePicker({ restoreFocus = true } = {}) {
    setIsOpen(false);
    if (restoreFocus) {
      queueMicrotask(() => triggerRef.current?.focus());
    }
  }

  function toggleSkill(skillID) {
    if (isDisabled) return;
    const selected = selectedSkillIDs.includes(skillID);
    if (!selected && selectedSkillIDs.length >= PROJECT_QUICK_CREATE_MAX_SKILL_COUNT) return;
    onChange(selected
      ? selectedSkillIDs.filter((item) => item !== skillID)
      : [...selectedSkillIDs, skillID].sort());
  }

  const selectableCount = state.items.filter(isSelectableSkill).length;
  const selectionAtLimit = selectedSkillIDs.length >= PROJECT_QUICK_CREATE_MAX_SKILL_COUNT;

  return (
    <div className="quick-create-skill-picker">
      <button
        ref={triggerRef}
        className={selectedSkillIDs.length > 0 ? 'prompt-tool is-active' : 'prompt-tool'}
        type="button"
        aria-label={selectedSkillIDs.length > 0 ? `Skill，已选择 ${selectedSkillIDs.length} 个` : 'Skill'}
        aria-expanded={isOpen}
        aria-haspopup="dialog"
        disabled={isDisabled}
        onClick={handleTrigger}
      >
        <Sparkles aria-hidden="true" size={15} />
        <span>Skill</span>
        {selectedSkillIDs.length > 0 ? <em>{selectedSkillIDs.length}</em> : null}
      </button>

      {isOpen ? (
        <section
          className="quick-create-skill-picker__dialog"
          role="dialog"
          aria-label="选择 QuickCreate Skill"
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault();
              closePicker();
            }
          }}
        >
          <header>
            <div>
              <strong>选择本次创作使用的 Skill</strong>
              <p>仅已发布且状态可用的 Skill 可以绑定；不选择时继续使用原 QuickCreate 流程。</p>
            </div>
            <button
              type="button"
              aria-label="刷新 Skill 列表"
              disabled={state.kind === 'loading'}
              onClick={() => load()}
            >
              <RefreshCw aria-hidden="true" size={16} />
            </button>
            <button ref={closeButtonRef} type="button" aria-label="关闭 Skill 选择" onClick={() => closePicker()}>
              <X aria-hidden="true" size={16} />
            </button>
          </header>

          {state.kind === 'loading' ? <p className="quick-create-skill-picker__state" role="status">正在加载我的 Skill…</p> : null}
          {state.kind === 'error' ? (
            <div className="quick-create-skill-picker__state">
              <p role="alert">{state.error.message}</p>
              <button type="button" onClick={() => load()}>
                <RefreshCw aria-hidden="true" size={14} />
                重试
              </button>
            </div>
          ) : null}
          {state.kind === 'ready' && state.items.length === 0 ? (
            <div className="quick-create-skill-picker__state">
              <strong>还没有 Skill</strong>
              <p>先在“我的 Skill”中创建并发布，之后就能在快速创作中选择。</p>
            </div>
          ) : null}
          {state.kind === 'ready' && state.items.length > 0 ? (
            <>
              {selectableCount === 0 ? (
                <p className="quick-create-skill-picker__notice" role="status">暂无已发布且可用的 Skill。</p>
              ) : null}
              <p className="quick-create-skill-picker__notice" role={selectionAtLimit ? 'status' : undefined}>
                最多选择 {PROJECT_QUICK_CREATE_MAX_SKILL_COUNT} 个 Skill。
                {selectionAtLimit ? ' 已达上限，取消一个已选项后才能继续选择。' : null}
              </p>
              <div className="quick-create-skill-picker__list">
                {state.items.map((skill) => {
                  const selectable = isSelectableSkill(skill);
                  const selected = selectedSkillIDs.includes(skill.skillID);
                  const disabledByLimit = !selected && selectionAtLimit;
                  return (
                    <label className={selectable && !disabledByLimit ? 'quick-create-skill-option' : 'quick-create-skill-option is-disabled'} key={skill.skillID}>
                      <input
                        type="checkbox"
                        aria-label={`选择 ${skill.definition.name}`}
                        checked={selected}
                        disabled={!selectable || disabledByLimit || isDisabled}
                        onChange={() => toggleSkill(skill.skillID)}
                      />
                      <span>
                        <strong>{skill.definition.name}</strong>
                        <small>{disabledByLimit ? '已达到选择上限' : selectable ? '已发布 · 可用' : disabledReason(skill)}</small>
                      </span>
                    </label>
                  );
                })}
              </div>
            </>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}

export function isSelectableSkill(skill) {
  return skill?.contentStatus === 'published' && skill?.governanceStatus === 'active';
}

async function loadAllOwnerSkills(loadSkills, signal) {
  const itemsByID = new Map();
  const seenCursors = new Set();
  let cursor;
  for (let page = 0; page < MAX_LIST_PAGES; page += 1) {
    const result = await loadSkills({ cursor, signal });
    result.items.forEach((skill) => itemsByID.set(skill.skillID, skill));
    if (!result.nextCursor) return [...itemsByID.values()];
    if (seenCursors.has(result.nextCursor)) {
      throw new Error('Skill 列表返回了重复游标');
    }
    seenCursors.add(result.nextCursor);
    cursor = result.nextCursor;
  }
  throw new Error('Skill 列表分页超过安全上限');
}

function disabledReason(skill) {
  if (skill.contentStatus !== 'published') return '草稿尚未发布';
  if (skill.governanceStatus === 'suspended') return '已暂停';
  if (skill.governanceStatus === 'offline') return '已下架';
  return '当前不可用';
}

function sameIDs(left, right) {
  return left.length === right.length && left.every((item, index) => item === right[index]);
}

function publicError(error) {
  return { message: String(error?.message || '暂时无法加载我的 Skill。') };
}
