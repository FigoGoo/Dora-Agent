package a2ui

const (
	Version1 = "1.0"

	CardTypeGeneric        = "card"
	CardTypeInfoCollection = "info_collection"
	CardTypeSkillSelect    = "skill_select"
	CardTypeStoryboard     = "storyboard"
	CardTypeToolRun        = "tool_run"

	ComponentText          = "Text"
	ComponentMarkdown      = "Markdown"
	ComponentColumn        = "Column"
	ComponentRow           = "Row"
	ComponentCard          = "Card"
	ComponentTextInput     = "TextInput"
	ComponentSingleChoice  = "SingleChoice"
	ComponentMultiChoice   = "MultiChoice"
	ComponentFileUpload    = "FileUpload"
	ComponentImagePreview  = "ImagePreview"
	ComponentVideoPreview  = "VideoPreview"
	ComponentAudioPreview  = "AudioPreview"
	ComponentVerticalSteps = "VerticalSteps"

	ActionAppendCard = "append_card"
	ActionUpdateCard = "update_card"
)

// RenderEventHint 是服务端内部的 SSE 渲染事件载体。
// 对外协议仍然统一放在 Payload 的 ActionEnvelope 中，避免工具层直接拼 UI。
type RenderEventHint struct {
	Event        string `json:"event"`
	SurfaceID    string `json:"surface_id,omitempty"`
	DataModelKey string `json:"data_model_key,omitempty"`
	Payload      any    `json:"payload,omitempty"`
}

// ActionEnvelope 是 Agent 直出的唯一 A2UI 消息格式。
// actions 按顺序执行：append_card 新增卡片，update_card 更新已有卡片或工作区状态。
type ActionEnvelope struct {
	Version string   `json:"a2ui_version"`
	Actions []Action `json:"actions"`
}

// Action 描述一次 UI 意图；Card/Payload 的具体结构由 Agent 生成，后端只做透传和校验。
type Action struct {
	Type      string        `json:"type"`
	Surface   string        `json:"surface,omitempty"`
	Ref       string        `json:"ref,omitempty"`
	MessageID string        `json:"message_id,omitempty"`
	CardID    string        `json:"card_id,omitempty"`
	ActionID  string        `json:"action_id,omitempty"`
	Target    *ActionTarget `json:"target,omitempty"`
	Card      *Card         `json:"card,omitempty"`
	Patch     []PatchOp     `json:"patch,omitempty"`
	Payload   any           `json:"payload,omitempty"`
}

// ActionTarget 用稳定 card_id/ref/message_id 定位更新对象，避免前端重复渲染相同卡片。
type ActionTarget struct {
	Surface   string `json:"surface,omitempty"`
	Ref       string `json:"ref,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	CardID    string `json:"card_id,omitempty"`
}

// PatchOp 使用 JSON Pointer 路径，主要服务于故事板和卡片局部更新。
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// Card 是所有 A2UI 消息卡的协议基类。
// 业务卡通过结构体嵌入 Card 复用 title/root/components/data/submit_label 等公共字段。
type Card struct {
	Type        string      `json:"card_type,omitempty"`
	Title       string      `json:"title,omitempty"`
	Message     string      `json:"message,omitempty"`
	Status      string      `json:"status,omitempty"`
	Root        string      `json:"root"`
	Components  []Component `json:"components"`
	Data        any         `json:"data,omitempty"`
	SubmitLabel string      `json:"submit_label,omitempty"`
}

// InfoCollectionCard 表示需要用户补充信息的表单消息卡。
type InfoCollectionCard struct {
	Card
}

// SkillSelectCard 表示让用户选择 Skill 或能力方向的消息卡。
type SkillSelectCard struct {
	Card
}

// StoryboardCard 表示故事板区域中的模块消息卡。
type StoryboardCard struct {
	Card
}

// ToolRunCard 表示工具运行状态消息卡。
type ToolRunCard struct {
	Card
}

// Component 是 A2UI 卡片的组件节点，ID 用于父子关系和表单字段定位。
type Component struct {
	ID        string         `json:"id"`
	Component ComponentValue `json:"component"`
}

// ComponentValue 是以组件名为 key 的 one-of 结构，前端按 key 选择渲染器。
type ComponentValue map[string]any

// TextComp 表示普通文本节点，可直接给 value，也可从 dataKey 指向的数据模型读取。
type TextComp struct {
	Value     string `json:"value,omitempty"`
	DataKey   string `json:"dataKey,omitempty"`
	UsageHint string `json:"usageHint,omitempty"`
}

// MarkdownComp 表示 Markdown 内容节点，适合展示 Agent 生成的富文本说明。
type MarkdownComp struct {
	Value   string `json:"value,omitempty"`
	DataKey string `json:"dataKey,omitempty"`
}

// ColumnComp 表示纵向布局容器，Children 引用其他组件 ID。
type ColumnComp struct {
	Children []string `json:"children"`
}

// RowComp 表示横向布局容器，Children 引用其他组件 ID。
type RowComp struct {
	Children []string `json:"children"`
}

// CardComp 表示卡片内部容器，通常作为 append_card 的 root 组件。
type CardComp struct {
	Children []string `json:"children"`
}

// TextInputComp 表示文本输入字段，提交时字段文本会直接成为用户消息片段。
type TextInputComp struct {
	Key         string `json:"key"`
	Label       string `json:"label,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Multiline   bool   `json:"multiline,omitempty"`
}

// ChoiceOption 表示单选或多选项，Value 是提交给 Agent 的稳定值。
type ChoiceOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// ChoiceComp 表示单选/多选字段，Options 由 Agent 按业务场景生成。
type ChoiceComp struct {
	Key      string         `json:"key"`
	Label    string         `json:"label,omitempty"`
	Required bool           `json:"required,omitempty"`
	Options  []ChoiceOption `json:"options"`
}

// FileUploadComp 表示文件上传字段，提交给 Agent 的内容是上传后的 file_id。
type FileUploadComp struct {
	Key      string `json:"key"`
	Label    string `json:"label,omitempty"`
	Required bool   `json:"required,omitempty"`
	Accept   string `json:"accept,omitempty"`
	Multiple bool   `json:"multiple,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

// MediaPreviewComp 表示图片、视频或音频预览节点，用于展示生成资产或用户上传素材。
type MediaPreviewComp struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Caption string `json:"caption,omitempty"`
	Alt     string `json:"alt,omitempty"`
	Poster  string `json:"poster,omitempty"`
}

// StepComp 表示步骤条中的单个节点，用于展示工具进度或业务阶段。
type StepComp struct {
	Key         string `json:"key,omitempty"`
	Title       string `json:"title"`
	Status      string `json:"status,omitempty"`
	Description string `json:"description,omitempty"`
}

// VerticalStepsComp 表示纵向步骤条，适合渲染工具执行状态和阶段同步。
type VerticalStepsComp struct {
	Steps []StepComp `json:"steps"`
}

// NewComponent 创建通用 A2UI 组件节点，调用方负责保证 kind/value 匹配。
func NewComponent(id string, kind string, value any) Component {
	return Component{ID: id, Component: ComponentValue{kind: value}}
}

// NewCard 创建消息卡基类，并复制 components 避免调用方切片后续变更影响协议对象。
func NewCard(cardType string, title string, root string, components []Component) Card {
	return Card{
		Type:       cardType,
		Title:      title,
		Root:       root,
		Components: append([]Component(nil), components...),
	}
}

// Text 创建文本组件。
func Text(id string, value string, dataKey string, usageHint string) Component {
	return NewComponent(id, ComponentText, TextComp{Value: value, DataKey: dataKey, UsageHint: usageHint})
}

// Markdown 创建 Markdown 组件，默认从 dataKey 指向的数据模型读取内容。
func Markdown(id string, dataKey string) Component {
	return NewComponent(id, ComponentMarkdown, MarkdownComp{DataKey: dataKey})
}

// Column 创建纵向布局组件，并复制 children 避免调用方切片后续变更影响协议对象。
func Column(id string, children []string) Component {
	return NewComponent(id, ComponentColumn, ColumnComp{Children: append([]string(nil), children...)})
}

// Row 创建横向布局组件，并复制 children 避免调用方切片后续变更影响协议对象。
func Row(id string, children []string) Component {
	return NewComponent(id, ComponentRow, RowComp{Children: append([]string(nil), children...)})
}

// CardContainer 创建卡片布局组件，并复制 children 避免调用方切片后续变更影响协议对象。
func CardContainer(id string, children []string) Component {
	return NewComponent(id, ComponentCard, CardComp{Children: append([]string(nil), children...)})
}

// TextInput 创建文本输入组件。
func TextInput(id string, value TextInputComp) Component {
	return NewComponent(id, ComponentTextInput, value)
}

// SingleChoice 创建单选组件。
func SingleChoice(id string, value ChoiceComp) Component {
	return NewComponent(id, ComponentSingleChoice, value)
}

// MultiChoice 创建多选组件。
func MultiChoice(id string, value ChoiceComp) Component {
	return NewComponent(id, ComponentMultiChoice, value)
}

// FileUpload 创建文件上传组件。
func FileUpload(id string, value FileUploadComp) Component {
	return NewComponent(id, ComponentFileUpload, value)
}

// ImagePreview 创建图片预览组件。
func ImagePreview(id string, value MediaPreviewComp) Component {
	return NewComponent(id, ComponentImagePreview, value)
}

// VideoPreview 创建视频预览组件。
func VideoPreview(id string, value MediaPreviewComp) Component {
	return NewComponent(id, ComponentVideoPreview, value)
}

// AudioPreview 创建音频试听组件。
func AudioPreview(id string, value MediaPreviewComp) Component {
	return NewComponent(id, ComponentAudioPreview, value)
}

// VerticalSteps 创建纵向步骤条组件。
func VerticalSteps(id string, value VerticalStepsComp) Component {
	return NewComponent(id, ComponentVerticalSteps, value)
}
