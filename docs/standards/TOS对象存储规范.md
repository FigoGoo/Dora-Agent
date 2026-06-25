# TOS 对象存储规范

状态：active
owner：主控 Codex 汇总维护
更新时间：2026-06-25
适用范围：Dora-Agent 业务微服务、前端直传、资产、公开作品媒体、TOS 公共桶对象 key 和清理策略
相关代码路径：业务微服务 `services/business/**`；用户端 `frontend/**`；配置模板 `.env.example`
相关契约：[C 端与后台业务 API 契约草案](../contracts/api/C端与后台业务API契约草案.md)、[业务微服务 RPC 契约草案](../contracts/rpc/业务微服务RPC契约草案.md)

## 背景

Dora-Agent 第一版使用火山引擎 TOS 公共桶保存上传素材、生成资产和公开作品媒体。前端不接触 TOS AK/SK；业务后端按权限、空间、项目、文件类型和大小签发上传凭证，前端直接上传到 TOS，上传完成后再调用业务 API 确认资产。

公共桶对象 URL 天然可访问，且第一版接入 CDN 域名 `https://tos.doraigc.com`，不支持 CDN 缓存失效。因此必须通过不可猜测 object key、业务 API 不暴露非公开资产 URL、公开快照独立前缀和下架后的业务隐藏 / 换 key / 删除对象策略降低风险。

## 目标

- 统一 TOS bucket、base URL、object key 目录和命名规则。
- 约定前端直传 TOS 的上传签名、确认和失败清理流程。
- 区分业务资产、生成过程产物、派生预览和公开作品快照媒体。
- 避免 object key 泄露用户隐私、原始文件名、业务标题或可猜测路径。
- 为取消分享、后台下架和对象清理提供可执行规则，同时明确 CDN 已缓存 URL 不承诺即时失效。

## 非目标

- 不定义 TOS SDK 具体代码实现。
- 不在仓库中保存真实 AK/SK。
- 不把 TOS 公共桶当作业务权限来源。
- 不承诺公共桶 URL 泄露后仍可通过业务权限拦截访问。

## 配置

| 配置项 | 是否可进 `.env.example` | 说明 |
| --- | --- | --- |
| `TOS_ENDPOINT` | 是 | TOS endpoint，例如北京区域 endpoint |
| `TOS_BUCKET` | 是 | 公共桶名称 |
| `TOS_REGION` | 是 | TOS region |
| `TOS_BASE_URL` | 是 | 前端访问公共对象的 CDN 域名，第一版固定为 `https://tos.doraigc.com` |
| `TOS_REQUEST_TIMEOUT` | 是 | 后端 TOS 请求超时 |
| `TOS_CONNECT_TIMEOUT` | 是 | 后端 TOS 连接超时 |
| `TOS_ACCESS_KEY_ID` | 否 | 只能放 `.env.local` 或部署密钥 |
| `TOS_SECRET_ACCESS_KEY` | 否 | 只能放 `.env.local` 或部署密钥 |

## Object Key 总规则

- object key 只能由业务后端生成，前端不得自造 key。
- object key 使用 ASCII 小写字母、数字、短横线、下划线和 `/`。
- object key 不包含手机号、邮箱、用户昵称、项目标题、作品标题、原始文件名、提示词、业务文案或其他可识别信息。
- object key 必须包含环境前缀：`local`、`dev`、`staging`、`prod`。
- object key 必须包含不可猜测 ID：`asset_id`、`run_id`、`artifact_id`、`snapshot_id`、`media_id` 或随机 `object_id`。
- 文件扩展名由后端根据允许的 MIME 类型推导，不直接信任用户上传文件名。
- 同一个业务对象的新版本不得覆盖旧 key；使用新 `object_id` 或新版本目录。
- 下架时不依赖业务列表隐藏；后续重新公开必须换 key。第一版不支持 CDN 缓存失效，已缓存 URL 不承诺即时失效。

## 目录规范

### 用户上传资产

用于用户在资产库或工作台上传素材。上传前由业务后端签发 key，前端直传后再确认。

```text
{env}/spaces/{space_id}/projects/{project_id}/assets/{asset_id}/original/{object_id}.{ext}
```

示例：

```text
prod/spaces/sp_xxx/projects/proj_xxx/assets/asset_xxx/original/obj_xxx.png
```

### 资产派生文件

用于缩略图、转码预览、波形图、封面帧等可重新生成的派生文件。

```text
{env}/spaces/{space_id}/projects/{project_id}/assets/{asset_id}/variants/{variant}/{object_id}.{ext}
```

`variant` 第一版枚举：

| variant | 用途 |
| --- | --- |
| thumbnail | 图片或视频缩略图 |
| preview | 前端预览优化版本 |
| poster | 视频封面帧 |
| waveform | 音频波形图 |
| transcript | 音视频转写文本 |

### Agent 生成过程产物

用于 Agent run 中间产物或生成工具返回的原始输出。生成完成并确认保存为业务资产后，业务资产记录引用最终 asset key；Agent DB 只保存业务引用。

```text
{env}/spaces/{space_id}/projects/{project_id}/runs/{run_id}/artifacts/{artifact_id}/outputs/{object_id}.{ext}
```

### 公开作品快照媒体

公开作品必须使用独立快照媒体前缀，不直接把私有业务资产 key 作为公开详情字段返回。这样取消分享或后台下架时，可以只删除或换掉公开快照媒体，不影响源资产。

```text
{env}/public/works/{public_work_id}/snapshots/{snapshot_id}/media/{public_media_id}/{variant}.{ext}
```

`variant` 第一版枚举：

| variant | 用途 |
| --- | --- |
| original | 公开展示原始副本 |
| preview | 公开详情页预览版本 |
| thumbnail | 公开列表封面 |

### 临时上传

原则上第一版直接签发最终资产 key，不使用临时目录。确需分片暂存或导入中转时，使用 `tmp` 前缀，并由业务任务定时清理。

```text
{env}/tmp/uploads/{upload_intent_id}/{object_id}.{ext}
```

临时对象必须在 24 小时内清理，不允许进入作品公开快照。

## 前端直传流程

```text
frontend
  -> POST /api/assets/upload-intents
business api
  -> 校验登录态、空间、项目、文件类型、大小、项目归档状态
  -> 生成 asset_id、object_key、上传凭证、过期时间
frontend
  -> 使用上传凭证直接 PUT/POST 到 TOS object_key
frontend
  -> POST /api/assets/upload-intents/:upload_intent_id/confirm
business api
  -> 校验 object_key、etag、size、content_type、safety_evidence
  -> 创建或激活业务资产记录
```

## 上传签名约束

- 上传凭证有效期建议 5-15 分钟。
- 上传凭证只能写入单个 object key 或严格限定的 key 前缀。
- 上传凭证必须限制 Content-Type、Content-Length、checksum 或等价校验字段。
- 上传凭证不得允许覆盖非本 intent 的 object key。
- 项目为 `archived` 时不得签发上传凭证。
- 业务确认前，上传对象不能作为可用资产展示。

## 公开访问规则

- TOS bucket 为公共桶，公共 URL 格式为：`{TOS_BASE_URL}/{object_key}`。
- 业务 API 只在用户有权限或作品公开时返回对象 URL。
- 未公开资产即使存放在公共桶，也只能通过不可猜测 key 降低暴露概率；它不是存储层私有对象。
- 公开作品详情返回公开快照中的 `public_media_url` 或 `public_media_refs`，不返回源资产 object key。
- 取消分享或后台下架后，公开快照不可访问；第一版不支持 CDN 缓存失效，因此已泄露或已缓存的 CDN URL 不承诺即时失效。
- 已下架作品如需重新公开，必须生成新的公开快照媒体 object key，不能复用旧 key。

## 数据库保存口径

业务数据库可以保存：

- `bucket`
- `object_key`
- `base_url`
- `public_url`
- `content_type`
- `size_bytes`
- `checksum`
- `etag`
- `storage_status`
- `upload_intent_id`
- `created_by_user_id`
- `space_id`
- `project_id`

Agent 领域数据库不得保存 TOS AK/SK、上传签名、长期可用 URL 或公开快照状态；只保存业务 `asset_id` / `public_media_id` 引用和脱敏摘要。

## 验收标准

- [ ] 所有上传 object key 由后端生成，前端不能自造 key。
- [ ] object key 不包含原始文件名、手机号、邮箱、标题、提示词或可识别文案。
- [ ] 上传签名不包含 AK/SK，且限制 key、MIME、大小和有效期。
- [ ] 归档项目不能签发上传凭证，不能确认新资产。
- [ ] 公开作品使用 `public/works` 快照媒体前缀。
- [ ] 取消分享或后台下架后，公开列表和详情不可访问。
- [ ] 已下架作品重新公开时必须换 object key。
