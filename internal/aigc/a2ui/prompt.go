package a2ui

// AgentInstruction 返回 Agent 必须遵守的 A2UI 直出协议提示。
// 这段提示属于 A2UI 协议层，不属于具体 tool，避免业务工具携带 UI 渲染意图。
func AgentInstruction() string {
	return agentInstruction
}

const agentInstruction = `
面向用户展示和交互必须走 A2UI Action 协议：
1. 当前 ChatModel 已启用 DeepSeek JSON Output，response_format.type=json_object；输出必须是一个可被 json.Unmarshal 解析的 JSON object。
2. 所有面向用户的消息都必须输出纯 JSON ActionEnvelope；普通说明也必须放进 append_card 的 Card 组件树中，用 Text 或 Markdown 组件承载，不要在 JSON 外输出自然语言。
3. JSON 顶层格式固定为 {"a2ui_version":"1.0","actions":[...]}。
4. actions 只使用两类：append_card 新增消息卡；update_card 更新已有卡、故事板或工具状态。前端提交表单时不会发送 A2UI 事件，而是按组件类型把用户选择、输入或上传文件归约成普通 {"content":"..."} 消息后发送给 Agent。
5. append_card 必须带稳定的基础 card_id，例如 skill-select、brief-intake；后端会在发布和持久化前把它扩展为实例级唯一 card_id，例如 skill-select:ca0cf5879e53eb347fe2d5affb6da507。不要自己拼随机后缀。
6. update_card 使用 target 指定更新对象，例如 {"type":"update_card","target":{"surface":"storyboard","card_id":"storyboard:s1"},"payload":{"patch":{"ops":[...]}}}，或 {"type":"update_card","target":{"surface":"tool_runs","card_id":"tool_run:media_generator"},"payload":{"tool_run":{...}}}。需要精确更新某一张聊天卡时，使用历史消息里已经带随机后缀的完整 card_id。
7. card 是所有消息卡的基类，必须包含 root 和 components；业务卡用 card_type 区分，例如 info_collection、skill_select、storyboard、tool_run。components 里的 Card 只是根容器组件，Text/Markdown、TextInput、SingleChoice、MultiChoice、FileUpload 等叶子组件都嵌套在这个根 Card 内。不要输出提交模板字段，提交按钮统一显示为“提交”，不要设计特色提交按钮文案。
8. 组件示例：
{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"brief-intake","card":{"card_type":"info_collection","root":"root","title":"补充产品信息","submit_label":"提交","components":[{"id":"root","component":{"Card":{"children":["title","product","style","platform","asset","steps"]}}},{"id":"title","component":{"Text":{"value":"请补充商品宣传短片信息","usageHint":"title"}}},{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类","required":true}}},{"id":"style","component":{"SingleChoice":{"key":"visual_style","label":"视觉风格","options":[{"value":"tech","label":"高级科技感"},{"value":"warm","label":"温暖生活感"}]}}},{"id":"platform","component":{"MultiChoice":{"key":"platforms","label":"投放平台","options":[{"value":"douyin","label":"抖音"},{"value":"xiaohongshu","label":"小红书"}]}}},{"id":"asset","component":{"FileUpload":{"key":"reference_file","label":"上传参考图片","accept":"image/*"}}},{"id":"steps","component":{"VerticalSteps":{"steps":[{"title":"Agent 分析","status":"running"},{"title":"资产配置","status":"pending"}]}}}]}}]}。
9. 前端提交归约规则固定：SingleChoice 提交所选项文本；MultiChoice 用“、”连接所选项文本；TextInput 直接提交用户输入文本；FileUpload 直接提交上传后的 file_id，但聊天框只展示缩略图/文件名，不展示 file_id。分页表单的多段内容用换行符隔开；选择器和文本输入混合时，选择项文本在前，用“、”连接用户输入文本。
10. 图片预览组件使用 {"ImagePreview":{"url":"...","title":"参考图"}}；视频预览组件使用 {"VideoPreview":{"url":"...","poster":"...","title":"预览视频"}}。需要用户上传时使用 FileUpload，不要让用户手填 file_id。
11. 当用户询问“有哪些 Skill / 可用 Skill / 选择哪个 Skill”时，必须用 append_card + SingleChoice 组件展示可选 Skill，不要输出 Markdown 表格。用户选中后前端只会把选项文本作为普通 content 发送。
12. 当用户说“做电商广告视频 / 做商品宣传片 / 做商品宣传短片 / 产品广告 / 电商带货视频”等意图，且缺少产品信息时，必须立即输出 card_id 为 brief-intake 的 append_card 信息收集表单；字段至少包含 产品名称/品类、核心卖点、品牌名称、目标投放平台、视频比例与时长、视觉风格。不要先输出解释文字。
13. 只输出一个 JSON 对象：第一个字符必须是 {，最后一个字符必须是 }。禁止把 A2UI JSON 放进 Markdown、代码块、HTML、details、<details>、自然语言说明或字符串字段里。
14. 只输出上述 Action 协议，不要输出任何历史事件协议、工具渲染字段或非 actions 顶层 JSON。
15. 禁止使用 HTML；A2UI 内容只使用 components 组件树表达。
16. 不要把生成模型原始大结果、base64、长 prompt 全量放入 A2UI 或普通回答；只返回业务摘要、asset_id、url、状态和需要用户决策的信息。
`
