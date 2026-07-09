// A2UI_EVENTS 列出前端会主动消费的新版 A2UI 事件名。
export const A2UI_EVENTS = Object.freeze({
  READY: 'a2ui.ready',
  ACTION: 'a2ui.action',
  INTERRUPT_REQUEST: 'a2ui.interrupt_request',
  ERROR: 'error'
});

// A2UI_EVENT_NAMES 供 EventSource 批量注册监听器使用。
export const A2UI_EVENT_NAMES = Object.freeze(Object.values(A2UI_EVENTS));

// A2UI_VERSION 与后端 ActionEnvelope.a2ui_version 保持一致。
export const A2UI_VERSION = '1.0';

// A2UI_ACTIONS 统一 ActionEnvelope 内的 action 类型。
export const A2UI_ACTIONS = Object.freeze({
  APPEND_CARD: 'append_card',
  UPDATE_CARD: 'update_card'
});

// A2UI_CARD_TYPES 统一业务消息卡类型，所有业务卡都扩展 A2UICard。
export const A2UI_CARD_TYPES = Object.freeze({
  GENERIC: 'card',
  INFO_COLLECTION: 'info_collection',
  SKILL_SELECT: 'skill_select',
  STORYBOARD: 'storyboard',
  TOOL_RUN: 'tool_run'
});

// A2UI_COMPONENTS 统一组件名，避免各渲染器散落硬编码字符串。
export const A2UI_COMPONENTS = Object.freeze({
  TEXT: 'Text',
  MARKDOWN: 'Markdown',
  COLUMN: 'Column',
  ROW: 'Row',
  CARD: 'Card',
  TEXT_INPUT: 'TextInput',
  SINGLE_CHOICE: 'SingleChoice',
  MULTI_CHOICE: 'MultiChoice',
  FILE_UPLOAD: 'FileUpload',
  IMAGE_PREVIEW: 'ImagePreview',
  VIDEO_PREVIEW: 'VideoPreview',
  VERTICAL_STEPS: 'VerticalSteps'
});

// A2UICard 是所有 A2UI 消息卡的协议基类。
export class A2UICard {
  constructor({ card_type = A2UI_CARD_TYPES.GENERIC, title, message, status, root = 'root', components = [], data, submit_label } = {}) {
    Object.assign(
      this,
      cleanObject({
        card_type,
        title,
        message,
        status,
        root,
        data,
        submit_label
      })
    );
    this.components = Array.isArray(components) ? [...components] : [];
  }
}

// InfoCollectionCard 表示需要用户补充信息的表单消息卡。
export class InfoCollectionCard extends A2UICard {
  constructor(options = {}) {
    super({ ...options, card_type: A2UI_CARD_TYPES.INFO_COLLECTION });
  }
}

// SkillSelectCard 表示让用户选择 Skill 或能力方向的消息卡。
export class SkillSelectCard extends A2UICard {
  constructor(options = {}) {
    super({ ...options, card_type: A2UI_CARD_TYPES.SKILL_SELECT });
  }
}

// StoryboardCard 表示故事板区域中的模块消息卡。
export class StoryboardCard extends A2UICard {
  constructor(options = {}) {
    super({ ...options, card_type: A2UI_CARD_TYPES.STORYBOARD });
  }
}

// ToolRunCard 表示工具运行状态消息卡。
export class ToolRunCard extends A2UICard {
  constructor(options = {}) {
    super({ ...options, card_type: A2UI_CARD_TYPES.TOOL_RUN });
  }
}

// createCard 创建通用消息卡实例。
export function createCard(value = {}) {
  return new A2UICard(value);
}

// createInfoCollectionCard 创建信息收集业务卡。
export function createInfoCollectionCard(value = {}) {
  return new InfoCollectionCard(value);
}

// createSkillSelectCard 创建 Skill 选择业务卡。
export function createSkillSelectCard(value = {}) {
  return new SkillSelectCard(value);
}

// createStoryboardCard 创建故事板业务卡。
export function createStoryboardCard(value = {}) {
  return new StoryboardCard(value);
}

// createToolRunCard 创建工具状态业务卡。
export function createToolRunCard(value = {}) {
  return new ToolRunCard(value);
}

// a2uiComponent 创建标准 one-of 组件节点，kind 必须来自 A2UI_COMPONENTS。
export function a2uiComponent(id, kind, value = {}) {
  return {
    id,
    component: {
      [kind]: value
    }
  };
}

// componentPayload 严格读取标准组件名，不兼容大小写变体或旧别名。
export function componentPayload(node, kind) {
  const component = node?.component || node || {};
  return component[kind] || null;
}

// text 创建 Text 组件。
export function text(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.TEXT, cleanObject(value));
}

// markdown 创建 Markdown 组件。
export function markdown(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.MARKDOWN, cleanObject(value));
}

// column 创建 Column 布局组件。
export function column(id, children = []) {
  return a2uiComponent(id, A2UI_COMPONENTS.COLUMN, { children: [...children] });
}

// row 创建 Row 布局组件。
export function row(id, children = []) {
  return a2uiComponent(id, A2UI_COMPONENTS.ROW, { children: [...children] });
}

// card 创建 Card 容器组件。
export function card(id, children = []) {
  return a2uiComponent(id, A2UI_COMPONENTS.CARD, { children: [...children] });
}

// textInput 创建 TextInput 表单组件。
export function textInput(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.TEXT_INPUT, cleanObject(value));
}

// singleChoice 创建 SingleChoice 表单组件。
export function singleChoice(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.SINGLE_CHOICE, cleanObject(value));
}

// multiChoice 创建 MultiChoice 表单组件。
export function multiChoice(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.MULTI_CHOICE, cleanObject(value));
}

// fileUpload 创建 FileUpload 文件上传组件。
export function fileUpload(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.FILE_UPLOAD, cleanObject(value));
}

// imagePreview 创建 ImagePreview 媒体组件。
export function imagePreview(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.IMAGE_PREVIEW, cleanObject(value));
}

// videoPreview 创建 VideoPreview 媒体组件。
export function videoPreview(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.VIDEO_PREVIEW, cleanObject(value));
}

// verticalSteps 创建 VerticalSteps 步骤条组件。
export function verticalSteps(id, value = {}) {
  return a2uiComponent(id, A2UI_COMPONENTS.VERTICAL_STEPS, cleanObject(value));
}

// cleanObject 移除 undefined 字段，保持输出 JSON 干净且稳定。
function cleanObject(value) {
  return Object.entries(value || {}).reduce((next, [key, item]) => {
    if (item !== undefined) {
      next[key] = item;
    }
    return next;
  }, {});
}
