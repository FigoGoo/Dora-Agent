# 视觉风格与设计 Token 规范

状态：active
owner：产品与需求责任域
更新时间：2026-06-27
适用范围：Dora-Agent Web 用户端首页、Agent 创作入口、Skill 快捷入口、项目卡片、精选作品展示，以及后续用户端页面的日间/夜间主题基线
相关代码路径：`docs/design/prototypes/flova-style-homepage.html`；用户端 `frontend/**`
相关契约：暂无。前端实现时不得发明 API、RPC 或 AG-UI 字段。

## 关联文档

- [UI/UE 设计总纲](./00-UIUE设计总纲.md)
- [站点信息架构与导航](./01-站点信息架构与导航.md)
- [统一 Agent 创作工作台体验设计](./02-统一Agent创作工作台体验设计.md)
- [首页体验设计](./03-首页体验设计.md)
- [管理端主题样式设计规范](./13-管理端主题样式设计规范.md)
- [本地 HTML 原型](./prototypes/flova-style-homepage.html)

## 背景

Dora-Agent 是面向音乐、图片、视频和组合内容创作的 AIGC Agent SaaS。用户端需要形成明确的创作心智：打开页面后，用户能立即看到 Agent 创作入口、可用 Skill、最近项目和精选作品，而不是进入传统后台或营销页。

本规范基于已确认的首页原型样式沉淀，参考 Flova 的暗色电影级创作控制台气质，但不照搬 Flova 的品牌资产、Logo、文案和具体业务结构。

## 设计目标

- 建立 Dora-Agent 用户端统一视觉语言：暗色电影级创作工作台、中心 Prompt 入口、半透明玻璃 Skill、内容作品流。
- 为前端提供可直接实现的颜色、透明度、字号、布局、动效和组件参数。
- 同时支持夜间主题和日间主题，第一版用户端首页以夜间主题为主视觉基线。
- 保证页面可读、可扫、可点击，不因氛围感牺牲交互清晰度。

## 非目标

- 不定义平台后台管理端视觉规范。后台应遵循 [管理端主题样式设计规范](./13-管理端主题样式设计规范.md)，保持高密度、表格化、可扫描。
- 不定义完整 Figma 变量交付格式。后续进入 Figma 设计系统时再导出。
- 不定义 API 字段、AG-UI payload 或业务状态字段。
- 不复制 Flova 品牌元素、Logo、素材和专有文案。

## 风格定位

关键词：

- Dark cinematic
- AI creation cockpit
- Prompt first
- Streaming gallery
- Transparent glass
- Low opacity neon accent
- Content as atmosphere

一句话原则：

> 页面不是营销落地页，而是用户进入 AI 创作室的第一屏。

视觉上应呈现：

- 大面积深黑背景，承托图片、视频和音乐作品封面。
- 中心化 Agent 输入框，优先引导用户“从一个想法开始”。
- Skill 卡片使用低透明度玻璃层，而不是实色按钮。
- 彩色只作为能量点和状态点，不大面积铺满。
- 作品和项目缩略图承担主要视觉氛围。

## 总体布局

### 桌面布局

```text
左侧 AppSideNav 220px 到 240px，可折叠为 64px 到 72px 图标栏
  ↓
右侧 ContextHeader 64px 到 70px
  ↓
中心 Hero 标题
  ↓
主 Prompt 输入框
  ↓
热门 Skill 横向卡片
  ↓
最近项目
  ↓
精选作品
```

核心尺寸：

| 区域 | 参数 |
| --- | --- |
| 左侧导航展开宽度 | `220px` 到 `240px` |
| 左侧导航折叠宽度 | `64px` 到 `72px` |
| ContextHeader 高度 | `64px` 到 `70px` |
| 主内容左边距 | `var(--sidebar)` |
| 主内容内边距 | `72px 20px 80px` |
| 页面最大内容宽度 | `1810px` |
| Prompt 最大宽度 | `1344px` |
| Skill 区域最大宽度 | `980px` |

### 响应式布局

| 断点 | 规则 |
| --- | --- |
| `> 1200px` | 完整桌面左右布局，左侧 AppSideNav 展开 |
| `768-1199px` | 左侧 AppSideNav 折叠为图标栏，ContextHeader 保持可见 |
| `<= 760px` | 左侧导航收进 Drawer，ContextHeader 左右 16px，主内容上边距增加 |

响应式要求：

- 字体不得使用 viewport width 缩放。
- 固定格式组件必须有稳定宽高，hover 不得撑开布局。
- 文本超长必须省略或换行，不得溢出卡片。
- 宽屏只增加内容容器呼吸感，不把组件无限拉宽。

## 颜色 Token

### 夜间主题

夜间主题是用户端创作体验的第一视觉基线。

| Token | 值 | 用途 |
| --- | --- | --- |
| `--bg` | `#0a0a0a` | 页面基础背景 |
| `--bg-soft` | `#000000` | 深黑背景、页尾过渡 |
| `--surface-subtle` | `rgba(255,255,255,0.05)` | 弱面板、胶囊、卡片底 |
| `--surface` | `rgba(255,255,255,0.10)` | 悬浮面板、轻强调 |
| `--surface-soft` | `rgba(255,255,255,0.15)` | 选中态、强调态 |
| `--overlay-soft` | `rgba(0,0,0,0.30)` | Prompt、浮层背景 |
| `--line-minimal` | `rgba(255,255,255,0.05)` | Skill 默认边框 |
| `--line` | `rgba(255,255,255,0.10)` | 卡片边框 |
| `--line-strong` | `rgba(255,255,255,0.40)` | Prompt 默认边框 |
| `--text` | `#ffffff` | 主文本 |
| `--muted` | `rgba(255,255,255,0.50)` | 次级文本 |
| `--faint` | `rgba(255,255,255,0.30)` | 弱提示、章节说明 |
| `--accent` | `#9bc957` | AI Lime 主强调 |
| `--accent-soft` | `rgba(155,201,87,0.70)` | Prompt hover/focus 边框 |
| `--green` | `rgba(111,225,93,0.80)` | Hot、New、成功感 |
| `--teal` | `#32b9b9` | 次强调 |
| `--blue` | `#6bbcff` | 信息和辅助高亮 |
| `--gold` | `#f5d18a` | 活动、精选、权益 |

### 日间主题

日间主题用于用户主动切换或后续轻量场景。日间主题不复刻暗色电影氛围，而是保持清晰、轻面板和高可读。

| Token | 值 | 用途 |
| --- | --- | --- |
| `bg-page` | `#f7f8fa` | 页面背景 |
| `bg-app` | `#ffffff` | 主应用背景 |
| `surface-1` | `#ffffff` | 面板、卡片 |
| `surface-2` | `#f3f5f7` | 次级面板 |
| `surface-3` | `#edeff2` | Hover / 轻强调 |
| `text-primary` | `#111827` | 主文本 |
| `text-secondary` | `#4b5563` | 次级文本 |
| `text-tertiary` | `#6b7280` | 辅助文本 |
| `border-subtle` | `#e5e7eb` | 默认边框 |
| `border-strong` | `#d1d5db` | 强边框 |
| `accent-primary` | `#32b9b9` | 主按钮、主选中态 |
| `accent-primary-hover` | `#2ca994` | 主按钮 hover |
| `accent-blue` | `#3186ff` | 链接、信息态 |
| `accent-gold` | `#ddbf3c` | 精选、亮点、权益 |
| `danger` | `#f94543` | 风险、失败 |
| `success` | `#2ca994` | 成功 |
| `warning` | `#ddbf3c` | 警告 |

## 透明度规则

透明度是本风格的关键。禁止把玻璃组件做成实色块。

### 基础透明度

| 类型 | 推荐值 |
| --- | --- |
| 页面暗遮罩 | `rgba(0,0,0,0.42)` 到 `rgba(0,0,0,0.90)` |
| Prompt 背景 | `rgba(0,0,0,0.30)` |
| Prompt hover 背景 | `rgba(0,0,0,0.36)` |
| 通用弱面板 | `rgba(255,255,255,0.04)` 到 `rgba(255,255,255,0.05)` |
| 通用 hover 面板 | `rgba(255,255,255,0.06)` 到 `rgba(255,255,255,0.10)` |
| Skill 默认边框 | `rgba(255,255,255,0.05)` |
| Skill hover 边框 | `rgba(255,255,255,0.14)` |
| 作品卡边框 | `rgba(255,255,255,0.08)` 到 `rgba(255,255,255,0.10)` |

### Skill 玻璃背景

Skill 默认卡片必须使用低透明多层玻璃，避免整块绿色、蓝色或灰色。

```css
--skill-glass-bg:
  radial-gradient(circle at 22% 56%, rgba(250, 188, 18, 0.04), transparent 36%),
  radial-gradient(circle at 38% 68%, rgba(8, 185, 98, 0.09), transparent 34%),
  radial-gradient(circle at 76% 66%, rgba(77, 136, 231, 0.13), transparent 32%),
  radial-gradient(circle at 35% 18%, rgba(249, 69, 67, 0.12), transparent 30%),
  linear-gradient(90deg, rgba(85, 72, 72, 0.20), rgba(49, 134, 255, 0.02)),
  rgba(255, 255, 255, 0.018);
```

### Skill hover 玻璃层

hover 层覆盖在 Skill 卡片内部，不改变卡片尺寸，不撑开布局。

```css
background:
  radial-gradient(circle at 18% 42%, rgba(255, 178, 111, 0.075), transparent 32%),
  radial-gradient(circle at 76% 34%, rgba(148, 184, 218, 0.07), transparent 34%),
  linear-gradient(90deg, rgba(255, 255, 255, 0.035), rgba(255, 255, 255, 0.012)),
  rgba(5, 5, 5, 0.30);
border: 1px solid rgba(255, 255, 255, 0.14);
backdrop-filter: blur(16px);
```

## 字体规范

### 字体栈

```css
--font-ui:
  Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont,
  "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", Arial, sans-serif;

--font-cn:
  PingFang, "PingFang SC", "Hiragino Sans GB", "Noto Sans SC",
  "Microsoft YaHei", sans-serif;

--font-display:
  PingFang, "PingFang SC", Inter, ui-sans-serif, system-ui, sans-serif;
```

使用规则：

- 全局 UI 使用 `--font-ui`。
- 中文标题、Skill、预览标题使用 `--font-cn`。
- Hero 主标题和副标题使用 `--font-display`。
- 字距使用 `letter-spacing: 0`。
- 开启 `-webkit-font-smoothing: antialiased`。

### 字号和字重

| 场景 | 字号 | 字重 | 行高 |
| --- | --- | --- | --- |
| 全局正文 | `14px` | `400` | `1.5` |
| Hero 主标题 | `40px` | `200` | `1.25` |
| Hero 副标题 | `16px` | `300` | `1.4` |
| 顶部活动胶囊 | `13px` | `600` | `1` |
| 账户胶囊 | `14px` | `600` | `1` |
| 左侧导航文字 | `11px` | `500` | `14px` |
| Prompt placeholder | `14px` | `500` | `20px` |
| Prompt 工具按钮 | `11px` | `500` | `1` |
| Section label | `14px` | `600` | `18px` |
| Skill 默认标题 | `13px` | `600` | `17px` |
| Skill hover 按钮 | `12px` | `500` | `12px` |
| Skill hover 描述 | `12px` | `400` | `14px` |
| Skill 预览标题 | `18px` | `600` | `24px` |
| Skill 预览说明 | `13px` | `400` | `20px` |
| Skill 预览 Tag | `11px` | `400` | `1` |
| 项目区标题 | `22px` | `700` | `30px` |
| 项目卡标题 | `14px` | `600` | `20px` |
| 项目卡说明 | `13px` | `400` | `18px` |
| 精选作品卡标题 | `16px` | `600` | `22px` |

字号禁用项：

- 不使用 `vw` 控制字号。
- 不把小卡片标题放到 `18px` 以上。
- 不把 Skill hover 按钮做成 `14px` 粗体。
- 不用负字距。

## 背景规范

夜间首页背景由深色渐变和极弱彩色氛围组成。

```css
background:
  radial-gradient(circle at 24% 6%, rgba(128, 76, 48, 0.28), transparent 30%),
  radial-gradient(circle at 82% 5%, rgba(38, 114, 138, 0.32), transparent 32%),
  radial-gradient(circle at 54% 28%, rgba(67, 24, 28, 0.18), transparent 30%),
  linear-gradient(180deg, #101010 0%, #050505 45%, #000 100%);
```

页面遮罩：

```css
background:
  linear-gradient(90deg, rgba(0, 0, 0, 0.42), transparent 18%, transparent 82%, rgba(0, 0, 0, 0.72)),
  linear-gradient(180deg, transparent 0%, rgba(0, 0, 0, 0.58) 52%, rgba(0, 0, 0, 0.90) 100%);
```

约束：

- 可使用大面积暗色和局部低透明渐变。
- 不使用装饰性渐变球、光斑球、背景气泡。
- 背景不能抢过作品缩略图和 Prompt 入口。

## 圆角规范

| 组件 | 圆角 |
| --- | --- |
| Prompt 输入框 | `20px` |
| Skill 卡片 | `14px` |
| Skill 预览卡 | `18px` |
| 项目封面卡 | `12px` |
| 精选作品卡 | `16px` |
| 常规按钮 | `9999px` 或 `6px`，按组件语义决定 |
| Tag | `7px` |
| 头像 | `50%` |

规则：

- 创作端可使用更圆的胶囊和玻璃卡。
- 后台管理端不沿用沉浸式大圆角，普通卡片不超过 `8px`。
- 不允许卡片套卡片造成多层浮卡感。

## 阴影和模糊

| 组件 | 参数 |
| --- | --- |
| Prompt 输入框 | `backdrop-filter: blur(21.6px)` |
| Skill 默认卡片 | `backdrop-filter: blur(12px)` |
| Skill hover 层 | `backdrop-filter: blur(16px)` |
| Skill hover 按钮 | `backdrop-filter: blur(14px)` |
| Skill 预览 model badge | `backdrop-filter: blur(15px)` |
| 项目封面卡 | `backdrop-filter: blur(12px)` |
| Skill 预览卡阴影 | `0 4px 8px rgba(0,0,0,0.45)` |

规则：

- 夜间主题主要用背景层级、透明度和边框，不用强外发光。
- 阴影只用于悬浮预览、浮层、弹窗。
- 玻璃层必须有真实透明度和 blur，不用纯灰色模拟。

## 组件规范

### ContextHeader

位置：

- `position: fixed`
- `height: 70px`
- `right: 16px`
- `left: var(--sidebar)`

活动胶囊：

- 高度 `30px`
- 左右 padding `18px`
- 圆角 `9999px`
- 背景 `linear-gradient(90deg, rgba(62,104,52,0.8), rgba(50,103,164,0.8))`
- 边框 `rgba(255,255,255,0.10)`
- blur `10px`
- 字号 `13px`

账户胶囊：

- 高度 `30px`
- 背景 `rgba(255,255,255,0.05)`
- 边框 `rgba(255,255,255,0.05)`
- blur `20px`

### 左侧导航

尺寸：

- 展开宽度 `220px` 到 `240px`
- 折叠宽度 `64px` 到 `72px`
- 内边距 `20px 12px`
- 背景为从左侧黑色到透明的渐变

导航项：

- 文字 `11px / 500`
- 图标 `26px`
- 激活图标边框 `rgba(255,255,255,0.68)`
- 文本默认 `rgba(255,255,255,0.50)`
- hover/active 为白色

### Hero

标题：

- 文案应直接表达产品或创作入口，不写长营销句。
- 字号 `40px`
- 字重 `200`
- 颜色 `rgba(255,255,255,0.86)`

副标题：

- 字号 `16px`
- 字重 `300`
- 颜色 `rgba(255,255,255,0.42)`

### Prompt 输入框

尺寸：

- 宽度 `min(1344px, calc(100vw - var(--sidebar) - 220px))`
- 最小高度 `140px`
- padding `22px`
- 圆角 `20px`

样式：

```css
border: 0.5px solid rgba(255,255,255,0.40);
background: rgba(0,0,0,0.30);
backdrop-filter: blur(21.6px);
```

hover/focus：

```css
border-color: rgba(155,201,87,0.70);
background: rgba(0,0,0,0.36);
```

Prompt 内部按钮：

- 高度 `25px`
- 字号 `11px`
- 边框 `rgba(255,255,255,0.20)`
- hover 背景 `rgba(255,255,255,0.06)`

### Skill 默认卡片

尺寸：

- 宽度 `178px`
- 高度 `54px`
- 圆角 `14px`
- gap `11px`
- padding `0 20px 0 23px`

样式：

- 背景使用 `--skill-glass-bg`
- 边框 `0.5px solid rgba(255,255,255,0.05)`
- blur `12px`
- 标题 `13px / 600 / 17px`
- 图标或缩略圆点 `28px`

规则：

- 卡片 hover 不放大，不改变布局。
- 默认卡片文字只显示一行，超出省略。
- `热门` 角标不得被卡片裁掉，因此卡片容器允许 `overflow: visible`。

### Skill hover 状态

结构要求：

- 外层 `.skill-top` 使用 `div` 或非按钮容器。
- `试一试` 必须是实际 `<button>`。
- 禁止 button 嵌套 button。

尺寸和排版：

- hover 层 `position: absolute; inset: -1px`
- gap `5px`
- 按钮高度 `25px`
- 按钮最小宽度 `78px`
- 按钮字号 `12px`
- 描述字号 `12px`

交互：

- hover 层只改变 opacity。
- 不改变卡片宽高。
- 不改变周围卡片位置。

### Skill 预览卡

尺寸：

- 宽度 `340px`
- 高度 `255px`
- top `64px`
- left `50%`
- 圆角 `18px`
- border `rgba(255,255,255,0.10)`
- 背景 `#1d2220`

动效：

```css
opacity: 0;
transform: translateX(-50%) translateY(18px) scale(0.96);
transition: opacity 0.2s cubic-bezier(0.4,0,0.2,1),
            transform 0.2s cubic-bezier(0.4,0,0.2,1);
```

hover：

```css
opacity: 1;
transform: translateX(-50%) translateY(0) scale(1);
```

图片：

- 图片亮度 `brightness(0.72)`
- 饱和度 `saturate(0.92)`
- 底部遮罩要足够承托文字

遮罩：

```css
linear-gradient(
  180deg,
  rgba(0,0,0,0.08),
  rgba(0,0,0,0.34) 40%,
  rgba(0,0,0,0.92)
)
```

文字：

- 标题 `18px / 600 / 24px`
- 说明 `13px / 400 / 20px`
- Tag `11px`，高度 `24px`

### 最近项目

项目封面：

- 高度 `170px`
- 圆角 `12px`
- 边框 `rgba(255,255,255,0.08)`
- 背景 `rgba(255,255,255,0.05)`
- blur `12px`

项目文字：

- 标题 `14px / 600 / 20px`
- 时间或说明 `13px / 400 / 18px`
- 最近项目行可以横向滚动，避免宽度不足时挤压卡片。

### 精选作品

作品卡：

- 圆角 `16px`
- 边框 `rgba(255,255,255,0.08)`
- 背景以封面图为主
- hover 可让封面 `scale(1.04)`
- 卡片文字放在底部暗遮罩上

分类 Tab：

- 高度 `31px`
- 最小宽度 `86px`
- 圆角 `9999px`
- 默认透明背景
- active 背景 `rgba(255,255,255,0.12)`

## 图标规范

前端实现统一使用 `lucide-react` 图标库。

使用规则：

- React 组件中从 `lucide-react` 按需引入图标。
- 导航、工具按钮、上传、下载、复制、分享、点赞、播放、暂停、设置、筛选、搜索、关闭、返回等常见动作必须优先使用 `lucide-react`。
- 图标按钮必须提供 `aria-label`；仅图标按钮还必须提供 tooltip。
- 图标尺寸默认 `16px`、`18px`、`20px`、`24px` 四档；左侧导航图标可使用 `26px` 容器承载。
- 图标描边跟随当前文本色或 token 色，不单独写死随机颜色。
- 原型 HTML 中的字符图标只作为占位，正式前端不得沿用。

禁用规则：

- 不新增其他 icon 库，除非产品设计师和前端责任域共同确认。
- 不把 emoji 当作功能图标。
- 不手写内联 SVG 替代 `lucide-react` 已有图标。
- 不用图片资源承载常规 UI 图标。

## 动效规范

统一 easing：

```css
cubic-bezier(0.4, 0, 0.2, 1)
```

时长：

| 场景 | 时长 |
| --- | --- |
| 按钮 hover | `0.15s` 到 `0.20s` |
| 卡片 hover | `0.20s` |
| Prompt focus | `0.30s` |
| 作品封面 scale | `0.30s` |

规则：

- Skill 默认卡片 hover 不放大。
- Skill 预览卡可以轻微浮现，但不得影响文档流。
- 作品封面可轻微放大，放大比例不超过 `1.04`。
- 禁止大范围弹跳、旋转、视差和持续闪烁。
- 后续实现应支持 `prefers-reduced-motion` 降低动效。

## 图像和媒体规范

首页、工作台和精选作品必须使用真实或生成的位图资产承载视觉氛围。

要求：

- 图片必须能表达作品、项目、Skill 或创作场景。
- 不使用纯 SVG 装饰图替代真实内容图。
- 不使用过度模糊、过暗、不可辨认的氛围图。
- 视频、图片、音乐项目应有明确封面、类型和标题。
- 图片卡上的文字必须有足够暗遮罩，保证可读。

## 禁用项

- 禁止大面积单一紫蓝渐变。
- 禁止装饰性渐变球、光斑球、背景气泡。
- 禁止卡片套卡片。
- 禁止 Skill hover 导致布局变形。
- 禁止按钮嵌套按钮。
- 禁止使用负字距。
- 禁止使用 viewport width 控制字号。
- 禁止用纯灰色模拟玻璃层。
- 禁止用 emoji、图片或手写 SVG 作为常规功能图标。
- 禁止在首页首屏堆放复杂后台信息。
- 禁止把平台后台样式和用户端创作样式混用。

## 前端落地要求

实现时必须：

- 把颜色、字体、间距、圆角、透明度抽成 CSS token 或主题变量。
- 夜间和日间主题使用同一套语义 token。
- 组件 hover/focus/active/disabled 状态齐全。
- Skill hover 使用 opacity 切换，不改变布局尺寸。
- 常见工具按钮使用 `lucide-react` icon + tooltip。
- 文本溢出必须省略或换行处理。
- 宽屏、标准桌面、笔记本、小屏都要截图验收。

## 验收标准

- 首页第一屏能明确传达 AI 创作入口，而不是普通后台或营销页。
- Prompt 输入框居中且视觉权重最高。
- Skill 卡片透明度接近低透明玻璃，不是实色按钮。
- `试一试` 是真实按钮，字号紧凑，不撑开卡片。
- Skill hover 后卡片不变形，周围卡片不位移。
- 预览卡图片不发灰过亮，底部文字可读。
- 字体层级符合本规范，不出现局部字号过大。
- 页面在 1440px、1920px、超宽屏下保持弹性布局。
- 日间主题后续实现时不得重新发明 token。

## 待后续补充

- Figma 变量和组件库命名。
- 日间主题完整视觉稿。
- 管理端组件库变量和后台页面视觉稿。
- Agent 创作工作台的 A2UI 组件视觉细则。
- 移动窄屏是否仅做查看能力，还是支持完整创作。
