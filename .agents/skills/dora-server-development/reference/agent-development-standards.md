# Agent Service 开发规范

> 状态：已按 2026-07-14 需求基线同步并启用
> 适用范围：`Dora-Agent/agent` 独立 Go Module、`agent/cmd/agent-service` Runtime、`agent/migrations` 以及 Agent 对外契约
> 不适用范围：Business Service、Business Worker

## 1. 规范等级与术语

- **MUST**：强制要求，违反时不得合并或发布。
- **SHOULD**：默认要求；偏离时必须在设计或代码评审中说明原因、风险和替代保障。
- **MAY**：按业务场景选择。

本文中的“类”对应 Go 中的具名类型、结构体或接口。

本文使用以下术语：

- **主 Agent**：项目唯一的 `ChatModelAgent`，负责理解用户意图、加载 Skill、选择并调用高层 Graph Tool。
- **Graph Tool**：暴露给主 Agent 的高层场景能力，通常由 Eino Tool 包装一个启动时预编译的 `compose.Graph`。
- **ToolsNode**：Eino Graph 内执行模型 ToolCall 的节点，不等同于项目的 Graph Tool。
- **Skill**：由平台管理并在运行时动态加载的系统 Skill 或用户 Skill，不是仓库中的静态提示词文件。
- **Turn**：主 Agent 针对一个持久化输入完成的一次受预算约束的处理单元。
- **Run**：一次 Runner 执行及其恢复、回执和投影记录。
- **权威状态**：能够在 Redis 丢失、进程重启和消息重复时恢复正确业务结果的 PostgreSQL 持久化事实。

## 2. 已确认的架构边界

1. 仓库根目录 `Dora-Agent/` 是多 Module 仓库容器，不是 Agent Go Module 或生产 Runtime。
2. Agent Module 根目录固定为 `Dora-Agent/agent`，使用独立的 `agent/go.mod`、`agent/go.sum`、配置、构建产物和发布版本。
3. Agent Runtime 入口固定在 `agent/cmd/agent-service`；Business Service 位于 `business/`，Business Worker 位于 `worker/`，三者分别使用独立 Go Module 和独立 Runtime。
4. 项目只有一个主 `ChatModelAgent`，不使用子 Agent、DeepAgent、AgentAsTool 或多 Agent 编排。
5. 主 Agent 通过动态加载不同 Skill，并调用高层 Graph Tool，完成不同场景下的 AIGC 内容创作。
6. Agent 使用独立 PostgreSQL 数据库和 `agent` Schema，不得直连、跨 Schema 查询或复用 Business 数据库。
7. Agent 与 Business 交互统一使用 Kitex + Thrift RPC。普通业务查询或命令直接使用 RPC，不得在没有语义决策必要时机械包装为 Tool。
8. Agent PostgreSQL 是 Session、Input、Run、Turn、Checkpoint、Model/Tool 回执、Approval、Operation、Batch、Job、Outbox、Session Skill 快照和事件日志的权威来源；Skill 草稿/发布快照、Storyboard、Asset、Binding 和积分归 Business PostgreSQL。Redis 只用于缓存、短期协调和唤醒。
9. Eino Checkpoint 是技术执行恢复设施，不替代业务状态、审批状态、任务状态或事件日志。
10. Agent 只创建有界的异步 Operation、Batch、Job 和 Outbox 后返回 `accepted`；不得在 Graph 中等待 Worker 或外部生成 Provider 完成。
11. Worker 不调用 Agent、不选择 Skill、不决定 Prompt，也不恢复已经结束的 Graph 调用栈。
12. etcd 只负责服务注册发现，不作为业务配置中心、Prompt 中心或 Skill 存储。
13. 当前系统没有 Tenant；普通用户和企业用户都是 User 类型，不得引入无业务含义的 `tenant_id` 或 `TenantID`。
14. 应用生成 UUIDv7。
15. 严禁在数据库层面创建物理外键约束和数据库级联；表、Entity、GORM Model、DTO 中可以并应保留逻辑外键字段。
16. 数据库表和字段必须具有中文数据库 COMMENT。
17. 实体、业务类型、DTO 和方法必须具有中文注释；业务判断、状态流转、幂等、事务、重试和补偿还必须在对应代码处用中文说明原因。
18. 前端、RPC、Event、Session Input、Tool Intent/Result、Job Payload 和跨领域参数必须使用 DTO。
19. 单次请求、Turn、Graph、UseCase 或事务内严禁循环执行同一或仅参数不同的同构 SQL；复杂读取使用 JOIN/CTE/子查询并映射为专用 DTO，或使用固定数量的批量查询。

## 3. 技术选型与版本基线

| 能力 | 选型 | v1 要求 |
| --- | --- | --- |
| Go | Go 1.26.3 | `agent/go.mod` 声明 `go 1.26` 和 `toolchain go1.26.3` |
| Agent 框架 | Eino | 精确锁定 `github.com/cloudwego/eino v0.9.10` |
| 消息模型 | 经典 `*schema.Message` | v1 不使用 `AgenticMessage` |
| ChatModel | Eino DeepSeek Adapter | `github.com/cloudwego/eino-ext/components/model/deepseek v0.1.6` |
| HTTP | Gin | Handler 只处理协议边界 |
| RPC | Kitex + Thrift | IDL 优先，Agent 与 Business 通过 RPC 交互 |
| 数据访问 | GORM + PostgreSQL Driver | Repository 之外不得直接使用 GORM |
| 数据库 | PostgreSQL 16 | 本地镜像固定为 `postgres:16-alpine` |
| 数据迁移 | golang-migrate + SQL | `agent/migrations` 是 Agent Schema 唯一 Migration 目录 |
| 缓存与唤醒 | Redis | 不作为 Session、Run、Checkpoint 或任务真源 |
| 注册发现 | etcd | 只负责服务注册发现 |
| 日志 | `log/slog` JSON | 结构化、可脱敏、字段稳定 |
| 可观测性 | OpenTelemetry | Runner、Turn、Graph、Node、Model、Tool、RPC 可关联 |
| 测试 | `testing` + Testcontainers + Fake Model | CI 默认禁止调用付费模型 |

### 3.1 依赖管理

- `agent/go.mod`、`agent/go.sum` 必须提交，且 Agent Module 在 `GOWORK=off` 时能够独立构建和测试。
- 依赖使用精确版本，禁止 `@latest`、分支版本和无说明的 pseudo-version。
- Eino、Eino DeepSeek Adapter、Kitex Runtime、Kitex 生成器和 thriftgo 的升级必须使用独立 PR，并提供兼容性、回归和回滚说明。
- Eino 升级必须验证 ChatModelAgent、Middleware 顺序、Graph Compile、ToolsNode、Checkpoint/Interrupt、Stream 拼接和 Runner 事件语义。
- v1 使用经典 Message 路径；任何迁移到 `AgenticMessage`、AgenticModel 或 AgenticToolsNode 的提案必须先提交专项设计，不得渐进混用两套消息类型。
- `agent/go.mod` 禁止保留本地路径 `replace`，不得依赖仓库根或其他 Module 的 `go.mod` 隐式提供依赖。
- Eino Skill 和参考示例只用于理解框架能力；与本项目规范冲突时以本规范为准。示例中的 `@latest`、AgenticMessage、DeepAgent、AgentAsTool、内存/Redis 权威 Checkpoint、无类型 Map、内联 Prompt 或默认 ToolsNode 均不得覆盖本项目门禁。

## 4. 本地开发环境

### 4.1 Go SDK

从仓库根目录执行 Agent Module 命令时默认使用：

~~~bash
/Users/figo/sdk/go1.26.3/bin/go -C agent version
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C agent test ./...
~~~

- MUST：不得依赖 PATH 中版本不明确的 `go`。
- SHOULD：项目脚本允许通过 `GO_BIN` 覆盖 Go 路径，便于 CI 和其他开发机使用。
- MAY：仓库根目录使用 `go.work` 组合 `business`、`worker`、`agent` 进行本地联调，但不得依赖 `go.work` 隐藏跨 Module 依赖。

### 4.2 Docker 中间件

本地 PostgreSQL、Redis、etcd 使用 Docker 启动，Agent Service 运行在宿主机：

| 中间件 | 本地基线 | 默认端口 |
| --- | --- | --- |
| PostgreSQL | `postgres:16-alpine` | 5432 |
| Redis | Redis 7 Alpine，Compose 锁定已验证版本 | 6379 |
| etcd | etcd 3.6.x，Compose 锁定已验证版本 | 2379 |

- 禁止使用 `latest` 镜像；容器必须配置 Healthcheck。
- Agent 使用独立数据库，数据库连接的 `search_path` 必须明确包含 `agent`，SQL 中 SHOULD 显式使用 `agent.<table>`。
- 本地测试必须使用独立测试数据库和 Redis namespace，禁止清空开发数据库。
- DeepSeek API Key、数据库密码和其他 Secret 通过环境变量或 Secret Manager 注入，禁止写入代码、配置模板、测试快照和日志。

## 5. Module 与目录规范

仓库固定采用三个并列的独立 Go Module。Agent 代码按“中间件、Tool、Graph Tool、ChatModelAgent、Prompt”等功能包组织，不使用统一的 `domain/application/adapter` 套层：

~~~text
Dora-Agent/
├── business/                       # Business Service 独立 Go Module
├── worker/                         # Business Worker 独立 Go Module
├── agent/                          # Agent Service 独立 Go Module
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── agent-service/
│   │       └── main.go
│   ├── api/
│   │   ├── openapi/
│   │   ├── thrift/
│   │   ├── event/                  # 对外 Event Schema
│   │   └── job/                    # Worker 持久化消费契约
│   ├── kitex_gen/
│   ├── internal/
│   │   ├── bootstrap/              # 依赖装配和生命周期
│   │   ├── config/                 # 配置解析与校验
│   │   ├── chatmodel/              # DeepSeek ChatModel 创建和弹性策略
│   │   ├── chatmodelagent/         # 唯一主 ChatModelAgent
│   │   ├── middleware/             # Agent Middleware 及固定顺序
│   │   ├── prompt/                 # 主 Agent 与 Graph Tool 的版本化 Prompt
│   │   ├── tool/                   # Tool 公共契约、Schema、注册表和安全包装
│   │   ├── turncontext/            # 不可变可信 Turn/Command Context
│   │   ├── graphtool/              # 高层 Graph Tool；一个 Tool 一个功能包
│   │   │   └── analyzematerials/
│   │   │       ├── tool.go
│   │   │       ├── graph.go
│   │   │       ├── state.go
│   │   │       ├── dto.go
│   │   │       ├── node_validate.go
│   │   │       ├── node_chat_model.go
│   │   │       ├── node_validate_output.go
│   │   │       ├── node_persist.go
│   │   │       └── branch.go
│   │   ├── skill/                  # Business Skill Client、Session 快照和加载
│   │   ├── runtime/                # Runner、Session Lane、预算、恢复和取消
│   │   ├── session/                # Session、Input、Run、Turn
│   │   ├── checkpoint/             # PostgreSQL CheckpointStore
│   │   ├── receipt/                # Model/Tool 回执和冻结输出
│   │   ├── approval/               # HITL 审批
│   │   ├── event/                  # EventLog、Outbox、A2UI 投影
│   │   ├── operation/              # 异步 Operation/Batch/Job 契约
│   │   ├── businessrpc/            # Business Service Kitex Client
│   │   ├── httpapi/                # Gin Handler 和 HTTP DTO
│   │   ├── rpcserver/              # Agent 对外 Kitex 服务
│   │   ├── postgres/               # GORM 连接和事务基础设施
│   │   ├── redis/                  # 缓存和唤醒
│   │   ├── etcdregistry/           # etcd 注册发现
│   │   └── observability/          # 脱敏日志、Metric、Trace
│   ├── migrations/                 # Agent Schema 的唯一 Migration 目录
│   └── tests/
│       ├── integration/
│       ├── contract/
│       └── evaluation/
├── docs/
└── go.work                         # 可选，仅用于本地联调
~~~

### 5.1 包职责与依赖方向

- `agent/cmd/agent-service` 只负责进程入口、信号处理并调用 `bootstrap.Run`，不得直接组装依赖，也不得包含 Prompt、Graph Node 或业务逻辑。
- `bootstrap` 是 Composition Root，负责实例化具体 Graph Tool、注册 Tool、组装 Middleware、主 Agent 和 Runtime；只有该包可以同时知道所有具体实现。
- `chatmodelagent` 只创建唯一主 Agent，接收注入后的 ChatModel、Middleware 和 Tool 列表；不得直接依赖具体 Graph Tool、GORM、Redis 或 Business RPC。
- `middleware` 负责模型回执、历史修复、Tool 裁剪、摘要、Turn 上下文、动态 Skill 加载和异常转换；不得实现具体 AIGC 业务流程。
- `tool` 只定义公共 Tool 契约、元数据、Schema、Registry 和安全包装，不得引用具体 `graphtool/*`。
- `turncontext` 定义 Runtime 注入、Middleware 和 Graph Tool 读取的不可变可信上下文、私有 Context Key 和读取函数；不得依赖 GORM、RPC、Redis、具体 Graph Tool 或传输层，禁止各包自行使用字符串 Context Key。
- `graphtool/<name>` 实现一个完整高层能力，可以依赖 `tool`、`turncontext`、`prompt`、`approval`、`operation` 和消费方最小接口；不得依赖具体 DeepSeek/Kitex Client、`chatmodelagent`、`middleware`、`runtime` 或传输层。ChatModel 以 Eino `model.BaseChatModel` 或更小接口注入，Business RPC 接口定义在消费方，具体实现只由 `bootstrap` 装配。
- `runtime` 负责 Runner 生命周期、Session 串行、输入 Claim、Checkpoint/Resume、预算、取消和冻结输出；不得定义 Prompt 或具体 Graph Node。
- `skill` 负责从 Business 读取已发布 Skill、冻结 Session Skill 快照和运行时加载；Skill 草稿/发布与权限归 Business，不得在 Agent 重复实现，也不得注册 Tool。
- `prompt` 只保存版本化 Prompt 定义和构建函数，不访问数据库、Redis、RPC，不执行 Graph。
- `httpapi`、`rpcserver` 只调用 Runtime 或明确的功能服务，不得直接调用 Graph、ChatModel 或 GORM。
- Agent 不得 import `business/internal/**`、`worker/internal/**` 或仓库根目录其他 Module 的内部代码。
- 跨 Module 共享使用 Thrift、HTTP、Event DTO 或其他明确版本化契约，不为复用少量结构默认创建第四个公共 Module。

### 5.2 命名规则

- Go 包名使用简短、小写、单数名称；功能目录使用 `chatmodelagent`、`graphtool`，导出类型使用 `ChatModelAgent`、`GraphTool`。
- Graph Tool 功能包使用无下划线包名，如 `analyzematerials`；Agent 可见 Tool Key 使用稳定的 `snake_case`，如 `analyze_materials`。
- Tool Key、代码包、Graph Name 和设计文档必须一一对应，禁止别名漂移。
- 项目 `tool` 包与 Eino Tool 包同文件使用时，Eino 包统一使用清晰别名，例如 `einotool`。
- 禁止新增 `common`、`utils`、`helper`、`misc`、`base`、`manager` 等职责不明确的公共包。
- DTO 随边界归属，如 `tool/dto.go`、`businessrpc/dto.go`、`httpapi/dto.go`，禁止全局 DTO 大杂烩。
- Graph Tool 默认“一个 Tool 一个包”；节点不多时按 `node_*.go` 分文件，不为形式拆出容易产生循环依赖的 `nodes` 子包。

## 6. Go 编码、DTO 与查询规范

### 6.1 格式和类型

- 所有 Go 文件执行 `gofmt` 和 `goimports`。
- 文件名使用小写 `snake_case`；缩写保持 `ID`、`URL`、`HTTP`、`RPC`、`DTO`、`HITL`。
- 接口定义在消费方并保持最小化，不为每个实现机械创建接口。
- 构造函数必须校验必需依赖，禁止 Service Locator 和可变全局实例。
- 禁止使用 `map[string]any` 代替稳定 DTO、Graph State、Tool Intent、Tool Result、Session Input 或 Event。
- TODO 格式为 `TODO(username): 中文说明和关联任务`，禁止无上下文 TODO。
- 所有阻塞调用传播 `context.Context`；StreamReader 创建成功后必须立即安排 `Close`。

### 6.2 强制中文注释

以下非生成代码必须具有中文注释：

- Entity、GORM Model、值对象、DTO、Graph State、Tool Intent/Result、Session Input 和 Event；
- 主 Agent、Middleware、Graph Tool、Node、Skill、Runtime、Repository、RPC Client、Approval 和 Operation 等业务类型；
- 所有导出的类型、字段、函数、方法、变量和常量；
- 领域或业务功能包中的所有方法，包括未导出方法；
- 基础设施层中包含业务判断、状态迁移、一致性或安全含义的方法。

类型注释必须说明职责、生命周期和关键不变量；方法注释必须说明前置条件、主要结果、错误语义和副作用。仅有“创建对象”“处理请求”之类同义复述不合格。

业务判断、状态流转、幂等、事务、重试、补偿、版本兼容、权限和预算逻辑必须在对应代码附近额外使用中文注释说明：

- 为什么需要该判断或顺序；
- 它保护什么业务规则或一致性边界；
- 删除或调整后可能造成什么结果。

### 6.3 DTO 和边界转换

- 前端请求/响应、Thrift、Event、Session Input、Approval、Tool Schema、Tool Intent/Result、Job Payload、跨协议和跨领域参数必须使用有类型 DTO；普通包内调用使用职责明确的具名类型，不机械添加 DTO 后缀。
- 禁止向前端、模型、RPC 或 Event 暴露 Entity、GORM Model、内部 Checkpoint 结构和原始 AgentEvent。
- 模型可填写的 Tool Intent 与服务端注入的可信 Command Context 必须分开；`user_id`、权限、预算、资源版本、幂等基键等可信字段不得出现在模型可控 Schema 中。
- 协议 DTO、业务 DTO、持久化 Model 之间使用显式 Mapper，禁止隐式 JSON 往返或反射复制关键字段。
- DTO 必须声明长度、数量、枚举、精度和版本边界；未知枚举、未知版本和未知字段按契约失败关闭。

### 6.4 SQL 和 Repository

- GORM 只能出现在 Repository 实现和 `postgres` 初始化代码中。
- 单次请求、Turn、Graph、UseCase、事务或批次内禁止在循环中执行同一或仅参数不同的同构 SQL，禁止 N+1。
- 复杂读取优先使用一次 JOIN/CTE/子查询并映射为专用查询 DTO；确实不适合 JOIN 时，使用固定数量的批量查询和内存映射。
- Repository 方法需要支持批量读取、批量写入和稳定排序；分页必须具有唯一 Tie-breaker。
- 关键更新必须检查 `RowsAffected`，用 Expected Version、Lease Version 或 Fencing Token 防止并发覆盖。

## 7. 主 Agent 与 ChatModel 规范

### 7.1 单主 Agent

- 生产环境只允许一个主 `ChatModelAgent` Profile 和一套受版本管理的主 Agent 配置。
- 禁止引入子 Agent、DeepAgent、AgentAsTool、Agent handoff 或多 Agent 路由。
- 不得把 Skill 当作 Agent 实例；Skill 只提供受控指令和场景知识。
- 新场景优先新增 Skill，或修改草稿并重新发布；只有需要模型可选择的高层能力时才新增 Graph Tool。
- 主 Agent 只能调用启动时注册且审核通过的 Tool 白名单，Skill、用户输入和运行时配置均不得动态新增 Tool。

### 7.2 DeepSeek ChatModel

- v1 统一使用 Eino 普通 DeepSeek ChatModel Adapter 和经典 `*schema.Message`。
- Model、BaseURL、超时、Token、温度、并发、费用和弹性策略通过运行配置注入，不得散落在 Prompt、Node 或业务代码中。
- API Key 不得进入 Tool Schema、Graph State、Checkpoint、日志或 Trace。
- `WithTools` 返回的 ToolCallingChatModel 视为不可变装配结果并可并发复用；禁止每次请求重复绑定相同 Tool 集合。
- Reasoning Content 只用于受控运行判断，不得持久化到普通日志、Trace、Session Message、Event 或前端响应。
- 模型瞬时重试、Failover 和超时只能由模型调用层的单一 Owner 执行；Graph、Runtime 和 HTTP 层不得叠加重试同一次模型调用。

### 7.3 Prompt

- 主 Agent Prompt 和每个 ChatModel Node Prompt 必须具有稳定 `prompt_key`、语义版本和内容摘要。
- Prompt 使用 Eino ChatTemplate 或等价的有类型构建器；稳定结构禁止在业务代码中拼接大段字符串。
- Prompt 变量必须有类型、白名单和长度限制；来自用户、Skill、RPC 和数据库的文本必须分隔来源，防止把数据误当系统指令。
- Prompt 变更视为行为变更，PR 必须说明影响场景、评测结果和回滚版本。
- 不得在 Prompt 中硬编码 Secret、费用、权限、Provider 选择、状态机规则或数据库写入规则。
- ChatModel 输出是候选值，必须由独立的确定性 Validator 校验后才能进入状态迁移或副作用节点。

## 8. Middleware 规范

Middleware 必须由 `internal/middleware` 集中定义、显式排序并通过顺序测试。禁止由功能包临时插入不可见的中间件。

最低顺序不变量：

1. Model Receipt 必须在历史归一化之前记录/重放外层模型调用结果。
2. ToolCall 历史修复必须先于 Tool Reduction。
3. Tool Reduction 必须先于 Summarization。
4. 临时 Turn Context 必须在 Summarization 之后注入，避免被摘要或跨 Turn 泄漏。
5. Session 创建时从 Business 加载当前 published Skill 并冻结 Session Skill Snapshot；所有 Turn、技术重试和恢复只读取该快照，新发布只影响新会话。
6. Tool Exception Middleware 将内部错误转换为稳定 Tool 错误码，不得把堆栈、SQL、Provider 原文或 Secret 返回给模型。

每个 Middleware 必须说明：

- 生效阶段、顺序依赖和是否修改 Message；
- 是否读取或写入 PostgreSQL；
- 幂等键、失败语义和恢复方式；
- 日志与 Trace 允许记录的字段；
- 单元测试和与相邻 Middleware 的组合测试。

当前项目默认不启用 Filesystem、Shell、通用 HTTP、Plan-Task、Agents.md Runtime Middleware 和动态 Tool Search；如确有需求，必须先完成安全专项设计和用户评审。

## 9. Runtime Skill 规范

### 9.1 Skill 来源和生命周期

- Runtime 不读取仓库静态 Skill 文件。开发协作使用的 `.agents/skills/**` 与业务运行时 Skill 是两类完全不同的资产。
- 系统 Skill 由管理端创建和管理；用户 Skill 由用户端创建和管理。
- Skill 草稿和发布快照存放在 Business PostgreSQL；Agent 只保存 Session Skill Snapshot 和加载 receipt。
- 产品状态只允许 `draft` 和 `published`，不提供版本明细、版本列表或版本切换。Business 内部保留 `publication_revision`、digest、发布时间和操作人用于 CAS、审计和回执，但不得包装成产品版本功能。
- 每个 Skill 聚合保留一个可编辑草稿和一个当前发布快照；发布原子替换当前发布快照，失败不影响旧发布内容。
- Eino Skill Middleware 使用固定的 Session Snapshot Backend 和内部 Loader；Loader 只把会话冻结内容以内联方式注入当前主 Agent，不属于业务 Graph Tool 白名单，不产生用户可见 ToolRun，也不能执行业务副作用。
- Skill 的上下文模式只允许 inline，必须拒绝 `fork` 和 `fork_with_context`，防止通过 Skill 变相创建子 Agent。
- 同一 Session Skill Snapshot 在一个 Turn 内最多成功加载一次；Loader 调用仍消耗该 Turn 的迭代、Tool、时间和 Token 预算，不得成为绕过硬预算的内部通道。
- 新 Session 按确定性规则加载当前 published 系统 Skill 与当前用户 Skill，并冻结 Skill ID、`publication_revision`、内容摘要、优先级和加载顺序。
- 同一 Session 的所有 Turn、重试、Resume 和回执重放必须使用冻结快照；发布变化只对发布后创建的新 Session 生效。
- 没有有效 Skill 时不注入空 Skill 指令、占位 Loader 或虚构能力。

### 9.2 发布和权限

- 当前发布快照在一次发布事务内不可变；编辑只修改草稿，下一次发布原子替换当前快照。历史 Session 始终保留其 Session Skill Snapshot。
- Skill 发布前必须完成 Schema、长度、注入风险、敏感信息、权限越界和 Prompt 冲突检查。
- 系统 Skill 的发布必须经过管理端审核；用户私有 Skill 至少经过自动安全检查。若产品开放跨用户共享或公共发布，默认还应经过管理端审核，偏离时必须先完成专项安全评审。
- Skill 脚本默认禁用；Skill 不得包含或触发 Shell、宿主文件读写、通用 HTTP、任意 SQL 或动态代码执行。
- Skill 不能新增 Tool、扩展 Tool 权限或风险等级、绕过 HITL、提高预算、选择 Provider、决定价格或扣费、改变业务状态机。
- 系统 Skill 与用户 Skill 使用不同 namespace；冲突优先级、最大加载数量和排序规则必须在配置与测试中保持确定性。
- Skill 内容不得成为权限判断依据；身份、资源权限和版本由服务端可信上下文与确定性节点验证。

## 10. Tool 与 Graph Tool 规范

### 10.1 是否应成为 Tool

只有当主 Agent 需要根据自然语言语义在多个高层能力之间选择，且该能力具有清晰输入输出、权限、预算、幂等和错误契约时，才设计为 Tool。

以下能力默认不包装为 Agent Tool：

- 普通 Business RPC 查询或命令；
- Provider SDK、对象存储、计费、积分、数据库 CRUD；
- 通用 HTTP、Shell、宿主文件系统、任意 SQL；
- 只被单个 Graph 确定性调用且无需模型选择的内部函数。

普通 RPC 应由 Graph 的 Query/Command Node 或明确的应用服务直接调用。只有模型确实需要在受控语义空间中选择该能力时，才允许 Tool 化，并必须说明普通 RPC 不足以表达该选择的原因。

### 10.2 Tool 契约

- 每个 Tool 使用稳定 Tool Key、版本化 JSON Schema、有类型 Intent DTO 和 Result DTO。
- Tool 描述只说明能力、前置条件和结果，不暴露内部 Prompt、SQL、RPC 地址或安全实现。
- Tool Schema 只包含模型可填写字段；可信 `user_id`、角色、会话、预算、资源版本和幂等上下文由服务端注入。
- Tool 输入执行严格解码：拒绝未知字段、超长文本、非法枚举、超量数组和精度溢出。
- Tool Result 使用稳定状态，例如 `completed`、`accepted`、`waiting_user`、`failed`，不得让模型从自由文本猜测业务状态。
- Tool 的空操作、重复调用、冲突和失败也必须产生可重放回执。
- Tool Registry 启动时构建并失败关闭；注册重复 Key、缺设计文档、Schema 不合法或风险元数据缺失时，Runtime 不得就绪。

### 10.3 当前 AIGC Graph Tool 白名单

结合当前产品需求基线，v1 主 Agent 只允许注册以下六个高层 Graph Tool：

| Tool Key | 主要职责 | 是否创建异步生成任务 |
| --- | --- | --- |
| `plan_creation_spec` | 规划创作规格和候选方案 | 否 |
| `analyze_materials` | 读取获授权素材的真实内容并形成带引用的受控分析结果 | 否 |
| `plan_storyboard` | 规划故事板、动态元素、依赖及 Prompt/Asset Slot，不负责最终 Prompt 生成 | 否 |
| `generate_media` | 在故事板模式或独立模式创建媒体生成 Operation/Batch/Job | 是 |
| `write_prompts` | 在故事板模式或独立模式生成、改写并版本化 Prompt，不调用媒体 Provider | 否 |
| `assemble_output` | 规划版本化剪辑时间线，并为预览或最终成片创建装配 Operation/Batch/Job | 是 |

- 该白名单属于当前项目 v1 约束，不是 Eino 的通用限制。
- 该表只统计 Agent-facing 业务 Tool；Eino Skill Middleware 的固定内部 Loader 只负责 inline 加载已冻结 Skill，不属于业务 Tool Registry，不得产生业务副作用或用户可见 ToolRun。
- Image、Video、Audio、Assembly、Billing、Provider、对象存储和业务 CRUD 不单独暴露给主 Agent。
- 新增、删除或重命名 Graph Tool 必须先完成独立设计文档、Tool Registry、安全和预算评审，并同步更新本节、主 Agent Prompt、契约测试和评测集。
- Skill、用户输入、外部配置和数据库数据不得覆盖该白名单。

## 11. Graph Tool 设计文档门禁

### 11.1 一工具一文档

每个 Agent-facing Graph Tool 必须具有独立中文设计文档，固定路径：

~~~text
docs/design/agent/graphtool/<tool_key>-design.md
~~~

强制规则：

- 新增 Graph Tool 的独立设计文档必须先审核通过，再开始实现；默认使用独立设计 PR。修改既有 Graph Tool 时，设计变更可以与实现位于同一 PR，但必须先完成设计部分评审并在合并前保持同步。
- Tool Key、代码目录、Graph Name、设计文档一一对应。
- 禁止用一份“大而全”的 Agent 设计替代每个 Graph Tool 的独立设计。
- Graph 拓扑、Node、State、Branch、输入输出、Prompt、状态机、幂等、HITL 或异步边界变化时，必须在同一 PR 更新设计文档。
- 文档必须同时包含 Mermaid 流程图和业务状态机图，二者不能互相替代；图、Node 表和状态迁移表必须一致。
- 没有 ChatModel Node 的 Graph Tool 必须解释为何它仍应是 Graph Tool，而不是普通 Tool、RPC 或 Command。

### 11.2 设计文档强制内容

每份 Graph Tool 设计文档至少包含：

1. Tool Key、中文名称、Graph Name、文档版本/状态、代码目录、负责人和使用场景。
2. 目标、非目标、Agent/Business/Worker 边界和最终权威数据源。
3. 模型可填写 Intent DTO、服务端可信上下文、Graph Input、Graph Output、Capability Result 和错误码。
4. 有类型 Graph State、字段来源、Owner、读写节点、是否持久化、是否进入 Checkpoint、序列化名称/版本、敏感等级和不变量。
5. 包含 START、所有终点、失败路径，以及适用的 Branch、HITL、异步边界、循环、取消和退出条件的 Mermaid 流程图；不适用项必须明确标注“不适用”及原因，禁止为满足模板虚构流程。
6. Node 清单、稳定 Node Key、Dora 业务类型、Eino 实现、单一职责、输入输出、State 读写、副作用、风险、调用模式、预算配置、回执、Checkpoint 兼容性、稳定错误码、重试 Owner 和失败流向。
7. 独立业务状态机图、状态定义和状态迁移表；流程节点不得冒充业务状态。
8. 每个 ChatModel Node 的必要性、Prompt 版本、输入消息、严格输出 Schema、Candidate DTO、Validator、纠错、回执、预算和降级。
9. Branch 条件、循环上限、并行度、Join 规则和部分失败语义。
10. Graph 级幂等键、Semantic Digest、同键重放/冲突、事务、Expected Version/Fence、Unknown Outcome 和补偿 Owner。
11. 风险等级、HITL 时点、Approval 绑定对象、权限、日志/Trace/Checkpoint 脱敏。
12. Graph、Node、Branch、状态机、幂等、并发、失败恢复、预算和契约测试清单。

Node Key 是 Trace、Receipt、Checkpoint 和历史诊断使用的稳定标识；重命名属于兼容性变更，必须说明旧数据处理策略。Node 清单至少使用以下字段：

| Node Key | 中文名称 | 业务分类 | Eino 实现 | 单一职责 | 输入/输出 | State 读写 | 副作用/风险 | Invoke/Stream | 预算/回执 | 错误码/失败目标 | Checkpoint |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

状态迁移表必须标明状态机 Owner、权威表/来源和执行方，至少使用以下字段：

| Aggregate/Owner | 权威来源 | 原状态 | 触发事件 | 执行方 | Guard/动作 | 目标状态 | 终态/可重试 | 事务/幂等键 | Fence/版本/Outbox | 失败处理 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

## 12. Graph 与 Node 实现规范

### 12.1 Graph 结构

- `graph.go` 只负责注册 Node、Edge、Branch、Compile 选项和拓扑校验，不得包含巨型业务执行 Lambda。
- Graph 在服务启动时 Compile 并复用，禁止按用户、Skill 或请求动态 Compile。
- Graph Input、Output 和 Local State 使用具名类型；禁止以无约束 `map[string]any` 作为主契约。
- Graph 使用 `compose.WithGenLocalState` 为每次调用创建 State；Node 通过类型对齐的 `WithStatePreHandler`、`WithStatePostHandler`、对应 Stream Handler 或 `compose.ProcessState` 读写，禁止使用全局变量承载请求状态。
- Local State 只属于单次 Graph 调用，不是 PostgreSQL 业务真源，也不得保存不必要的 Secret、完整 Prompt 或 Reasoning。
- 每个 State 字段必须有明确 Owner Node，原则上单写多读；并行 Node 不得无合并规则写同一字段。
- 模型生成值进入 Candidate 字段，经独立 Validator 后才能进入 Command 字段。
- 无环 DAG 必须在 Compile 时使用 `compose.WithNodeTriggerMode(compose.AllPredecessor)`；Fan-in 还必须另外使用不同 `WithOutputKey`、`compose.RegisterValuesMergeFunc[T]` 或显式 typed Join 定义合并，不能用值合并替代触发语义。
- 含循环 Graph 使用 `AnyPredecessor`，必须说明退出条件并配置 `compose.WithMaxRunSteps`；不得把 DAG 的 AllPredecessor 规则与循环 Graph 混用。
- `AddGraphNode` 接收未 Compile 的 `compose.AnyGraph`，由顶层 Compile 递归编译；SubGraph 的 Graph Name、Trigger Mode、MaxRunSteps 和 Interrupt 选项通过 `compose.WithGraphCompileOptions` 显式传入，不能假定继承父 Graph 配置。
- SubGraph 只用于边界清晰、可独立测试和复用的有界流程，禁止为目录分层制造无界嵌套。
- 启用 Checkpoint 时，自定义 State、Interrupt Data 和 Resume Data 使用稳定 `schema.RegisterName` 注册；序列化名称变更必须提供旧 Checkpoint 兼容或失效策略。

### 12.2 Node 类型与职责

下表中的 `guard/query/transform/validator/command/dispatch/parallel_join` 是 Dora 业务分类，不是 Eino 提供的专用 Node API；它们统一以 `compose.InvokableLambda` 等 Lambda 配合 `AddLambdaNode` 实现。

| Dora 业务分类 | Eino 实现 | 约束 |
| --- | --- | --- |
| `guard` | `AddLambdaNode` | 权限、版本、预算和前置条件必须确定性执行 |
| `query` | `AddLambdaNode` | PostgreSQL/RPC 批量只读，禁止循环 SQL |
| `transform` | `AddLambdaNode` | 标准化、Diff、Hash、聚合，无副作用 |
| `prompt` | `AddChatTemplateNode` | Prompt Key 和版本可追踪 |
| `inference` | `AddChatModelNode` | 语义理解、规划、创意候选；不直接写业务数据 |
| `validator` | `AddLambdaNode` | 严格解析、精确集合校验、拒绝未知字段、失败关闭 |
| `branch` | `AddBranch` | 只依据确定性字段，未知分支失败关闭 |
| `parallel_join` | 并行 Edge + typed `AddLambdaNode`/Merge | DAG 使用 AllPredecessor，并显式定义合并和部分失败语义 |
| `command` | `AddLambdaNode` | 调用业务服务或 Repository，明确事务、幂等和 Fence |
| `dispatch` | `AddLambdaNode` | 原子创建 Operation/Batch/Job/Outbox 后返回 `accepted` |
| `interrupt` | Lambda 返回 Interrupt 或 Compile Interrupt 选项 | 仅确需恢复 Graph 调用栈时使用，必须配 CheckpointStore |
| `subgraph` | `AddGraphNode` + `WithGraphCompileOptions` | 传入未 Compile Graph；有类型、可复用、有界 |
| `tool_execution` | `AddToolsNode` | 默认不用；固定白名单和调用上限 |

实施规则：

- 每个 Node 只承担一种主要职责。查询、Prompt、模型推理、解析、校验、分支、写入和派发必须分开。
- Graph Input、Output 和 State 保持具名类型；ChatTemplate 的 `map[string]any` 输入只允许由专用 Transform Node 在 Prompt 边界生成，Key、类型和长度必须固定，禁止把该 Map 当作跨 Node 通用状态。
- ChatModel 调用原则上必须使用 `AddChatModelNode`，禁止把 `ChatModel.Generate/Stream` 隐藏在普通 Lambda 中。
- Prompt 组装优先使用 `AddChatTemplateNode`，禁止把版本化 Prompt 与业务写入混在同一 Node。
- 经典 ChatModel Node 输入是 `[]*schema.Message`、输出是 `*schema.Message`；后继 Validator 负责严格解析 Candidate DTO，不能把 Message 直接传给 Command。
- ChatModel 只负责语义理解、内容规划和生成候选；不得决定身份权限、价格、扣费、Provider、幂等键、版本 Fence、状态合法性和最终数据库写入。
- ChatModel 输出后必须经过独立确定性 Validator；Validator 失败不得进入 Command 或 Dispatch。
- Branch 只读取确定性字段或校验结果，不得直接以模型自由文本作为路由条件。
- `ToolsNode` 默认禁用；只有 Graph 内模型确需在固定的低风险子能力白名单中选择时才允许使用。普通 RPC、CRUD、HTTP、Shell、文件和 Provider 调用不得包装进 ToolsNode。
- 使用 ToolsNode 的设计必须列出绑定的 ToolInfo 白名单、Model→Branch→ToolsNode→Model 拓扑、ToolCallID/Tool Result 配对、Unknown Tool 失败关闭、最大 ToolCall/Graph Step、幂等回执，并显式决定 `ToolsNodeConfig.ExecuteSequentially`；不得依赖默认并行语义。
- 当前使用经典 `schema.Message`，需要 Tool Node 时使用经典 ToolsNode，不使用 AgenticToolsNode。
- Interrupt 不是独立 Eino Node API。设计必须明确使用 Lambda 返回 `compose.Interrupt`/`compose.StatefulInterrupt`，还是 Compile 的 `WithInterruptBeforeNodes`/`WithInterruptAfterNodes`，并同时配置 PostgreSQL CheckpointStore 和稳定 Resume 类型。
- 任何 StreamReader 创建成功后必须在同一作用域立即安排 `Close`，取消和读取错误必须传播。

### 12.3 状态机

- Graph 流程节点、Tool 调用结果、Approval、Operation、Batch 和 Job 是不同状态空间，必须分别设计和测试，禁止压平为一张状态机。
- 每份文档至少提供 Graph Tool 调用/回执状态机：`completed`、`accepted`、`waiting_user`、`failed`；`accepted` 表示异步任务已原子派发且本次 Graph 调用已经结束，不代表 Operation 存在名为 accepted 的运行状态。
- 涉及 HITL 时另提供 Approval 状态机，至少区分 `pending`、`approved`、`rejected`、`expired`。`waiting_user` 通常结束 Graph 并持久化 Approval；只有专项设计明确使用 Checkpoint/Resume 时才保留可恢复调用栈。
- 涉及异步执行时，Operation、Batch、Job 分别提供 Owner 明确的 PostgreSQL 状态机；Graph Local State 不得用来表示跨进程异步状态。
- 每个状态机必须明确初态、终态、可重试状态、合法迁移、禁止迁移和并发冲突，状态常量、DTO 枚举、数据库值和设计文档保持一一映射。
- 每个迁移必须声明触发事件、Guard、事务边界、幂等键、Expected Version/Fence 和失败处理。

## 13. Runner、Session 与 Turn 规范

### 13.1 Runner 入口

- 生产调用必须通过 Eino Runner 和项目的持久化 Session Lane，禁止 HTTP/RPC Handler 直接调用 `agent.Run`、Graph 或 ChatModel。
- HTTP/RPC 只负责鉴权、校验 DTO、持久化输入和唤醒 Processor，不得持有长事务等待模型。
- 所有 Session 输入先持久化再唤醒；Redis 唤醒丢失时，PostgreSQL 扫描能够恢复处理。
- Session 内必须串行处理，同一时间只有一个有效 Owner；多实例使用数据库 Lease 和 Fencing Token 防止并发提交。
- InputID、TurnID、RunID 使用稳定 UUIDv7；同一输入技术重试必须复用原 ID，不得生成第二个业务 Turn。
- 已开始的输入遵循 Head-of-Line，不得让后续输入越过未决的前序输入破坏上下文因果。

### 13.2 持久化输入类型

运行时至少使用有类型的输入，而不是伪造 User Message：

- `UserMessage`：用户提交的新消息。
- `ResumeRequested`：恢复明确的 Runner/Graph Checkpoint。
- `ApprovalContinuationResult`：可信审批决定及绑定版本。
- `BatchContinuationResult`：Worker 批次终态的可信快照。

- 系统输入必须标记可信来源和稳定 SourceID；不得把 Worker 结果或审批结果拼成用户文本。
- 同一 SourceID 重复投递只产生一个有效输入；重复处理重放已冻结结果。
- 不需要 Agent 解释的 Worker 成功结果直接确定性投影和提交，不为“说一句成功”额外调用模型。

### 13.3 Turn 上下文

- Session 创建时冻结 Skill 快照；每个新 Turn 只冻结消息边界、Prompt 版本、Tool Registry 版本、预算和可信 Command Context。
- 同一 Turn 的重试和 Resume 不得包含冻结边界之后的新消息；后续消息进入后续 Turn。
- 上下文裁剪必须保留完整因果 Run、Assistant ToolCall 与对应 Tool Result 配对，禁止留下孤立 Tool Message。
- 摘要是上下文压缩结果，不是业务真源；摘要生成失败不得删除原始可恢复消息边界。
- Runner/模型完整输出必须先拼接并冻结，再执行业务投影；投影失败时重放冻结输出，不得重新调用模型或 Tool。

## 14. Checkpoint、回执与幂等

### 14.1 Checkpoint

- Eino `CheckpointStore` 使用 PostgreSQL 实现，Redis 和进程内存不得作为生产 Checkpoint 权威来源。
- Checkpoint ID 必须绑定 Session、Input、Turn、Run 和 Graph/Node 版本，禁止跨用户、跨资源复用。
- 自定义 State 类型持久化前必须使用稳定序列化名称注册；名称或字段兼容性变化必须有迁移和旧 Checkpoint 处理策略。
- Checkpoint 只保存恢复所需的最小技术状态，不保存完整 Reasoning，不代替 Approval、Operation、Batch、Job 或 Tool Result。
- HITL 优先使用持久化 Approval 和新的可信 Continuation Turn；只有确需恢复原 Graph 调用栈时才使用 Interrupt/Resume。
- Resume 必须校验映射 ID、版本、审批绑定、Owner 和 Fence；输入参数不得覆盖已冻结 Checkpoint State。

### 14.2 Model Receipt

- 外层主 Agent 模型调用以 `(turn_id, model_call_ordinal)` 作为稳定唯一键，执行 first-write-wins。
- 流式输出完整拼接、校验后再冻结；回执存在时直接重放，不得再次请求模型。
- Graph Tool 内部 ChatModel Node 使用独立 Receipt namespace，至少绑定 Graph Tool Key、Graph Run、Node Key、Prompt Version 和调用序号。
- 同键不同请求摘要返回冲突并停止，不得覆盖旧回执。

### 14.3 Tool Receipt

- Tool 调用必须具有稳定 ToolCallID、Tool Key、Schema Version、语义摘要和幂等基键。
- 同键同语义返回原结果；同键不同语义返回稳定 Conflict。
- `accepted`、`waiting_user`、no-op、失败和取消都必须落回执，不能只记录成功。
- RPC Unknown Outcome 时先按原幂等键查询 Business/Operation 权威状态，禁止立即使用新键重试。
- 模型、Graph、Runtime、Worker 和 Provider 之间必须只有一个重试 Owner，禁止多层叠加重试同一副作用。

## 15. 数据库与 GORM 规范

### 15.1 数据所有权

- Agent 使用独立数据库和 `agent` Schema，只维护 Agent 明确拥有的数据。
- Agent 不得直连 Business 数据库，不得复制 Business GORM Model 绕过 RPC，不得跨库 JOIN Business 表。
- Business 用户、鉴权、Skill 草稿/发布、Storyboard/Revision、Element/Slot、Asset、Binding、积分、费用和管理配置由 Business Service 负责；Agent 通过版本化 RPC DTO 访问。
- Agent Graph Tool 原子创建的 AIGC Operation、Batch、Job 和对应 Outbox 归 Agent Module 所有，表和 Migration 位于 Agent 数据库的 `agent` Schema 与 `agent/migrations`；Worker 的 Claim、执行和终态更新不转移 Migration Owner。
- Worker 只有在 `agent/api/job` 或其他明确公共契约中发布表、允许操作、状态/version 语义和兼容策略后，才可直接消费 Agent 持久化任务；Worker 使用最小权限数据库账号，只能访问契约允许的对象和动作。
- Worker 不得 import `agent/internal/**`。Job Payload、Batch Continuation、Event 和持久化消费 Schema 必须落在 `agent/api/job`、`agent/api/event`、共享 IDL 或等价公开版本化目录；生产方和消费方执行契约测试，禁止复制两份未版本化结构。
- 同一表只有一个 Migration Owner。Owner 是定义表生命周期、写入事务和数据不变量的 Module，不是碰巧消费或查询它的 Runtime。
- Agent 创建并拥有的 Session、Session Skill Snapshot、Checkpoint、Receipt、Approval、Event、Operation、Batch、Job 或 Outbox 表由 `agent/migrations` 管理；跨 Module 消费不转移所有权。

### 15.2 Schema 和 Migration

- Schema 只能通过 `agent/migrations` 中不可变、版本化 SQL Migration 变更；启动时禁止 `AutoMigrate`。
- 已发布 Migration 禁止修改；修正使用新的前向 Migration。
- 每张表和每个字段必须通过 SQL 添加中文 COMMENT，说明业务含义、单位、状态语义和敏感等级。
- 严禁创建 `FOREIGN KEY`、`REFERENCES` 约束、`ON DELETE CASCADE`、`ON UPDATE CASCADE` 或其他数据库级联行为。
- 允许并要求按业务关系保留 `user_id`、`session_id`、`turn_id`、`operation_id` 等逻辑外键字段，并建立必要索引。
- GORM 可以使用 `foreignKey`、`references` 描述查询映射，但禁止使用 `constraint` 创建物理约束。
- 逻辑关联完整性、删除、归档和级联处理由业务代码显式完成，必须有中文注释、幂等和测试。
- 当前没有 Tenant，不得预留 `tenant_id`。
- 主键使用应用生成的 UUIDv7；时间使用 `timestamptz` 并存 UTC。
- 状态、幂等键、业务唯一键、Lease、Fence 和 Outbox 扫描字段必须有与查询相匹配的唯一或组合索引。

### 15.3 事务与金额

- Repository 之外不得调用 GORM；所有 GORM 调用传播 Context。
- 事务应短小，只包含必要数据库读写；禁止在事务中等待 ChatModel、RPC、Provider、Redis、对象存储或用户审批。
- 领域状态与本 Module Outbox 必须在同一数据库事务提交。
- 关键状态迁移使用条件更新、Expected Version 或 Fence，并检查 `RowsAffected`。
- 积分使用 `bigint`/Go `int64`；金额按字段明确使用最小货币单位 `bigint` 或 `numeric(p,2)`，固定保留两位小数，禁止 `float32/float64`。
- Agent 不自行决定价格；生成积分由 Graph 确定性节点在派发前调用 Business `PrepareGeneration` 原子直接扣除。Agent 只保存 `preparation_id/ledger_entry_ids/charged_points` 引用，Worker 不产生费用事实。

## 16. RPC、Worker 与异步边界

### 16.1 Business RPC

- Agent 调用 Business 使用 Kitex + Thrift 和 etcd 服务发现；禁止引用 Business 内部包。
- RPC IDL 是契约源，已发布字段号不得复用；破坏性变化使用新方法或版本。
- RPC 请求和响应在边界映射为 Agent DTO，禁止把 Kitex 生成类型扩散到 Graph State、Entity 和 Tool Schema。
- 调用必须设置超时、取消、重试分类和稳定幂等键；Unknown Outcome 先查询原操作结果。
- 无必要不把 RPC 包装成 Tool。主 Agent 不需要做语义选择的业务调用，应由确定性 Query/Command Node 完成。

### 16.2 Worker 边界

- Graph Tool 必须先调用 Business `PrepareGeneration` 并确认 `charged + element/asset/ledger IDs`，再创建有界 Operation、Batch、Job 和 Outbox 并返回 `accepted`；不得轮询或阻塞等待 Worker。
- Worker 从 PostgreSQL 权威状态 Claim，使用 At-Least-Once + 幂等执行；Redis 仅唤醒。
- Worker 对 Agent-owned Operation/Batch/Job/Outbox 的访问必须遵守公开、版本化的持久化消费契约、最小数据库权限和 Agent Migration Owner 规则；Worker 自有表仍由 `worker/migrations` 管理。
- Worker 不调用主 Agent、不加载 Skill、不决定 Prompt、不重新规划或拥有故事板，不直写 Storyboard/Binding/Asset/积分表。
- Worker 只负责队列、并发/lease、Provider、媒体校验、技术重试、TOS 上传和调用 Business `FinalizeGeneration`；Storyboard Binding 由 Business 按 `binding_mode` 可选执行。
- Worker Batch terminal Outbox 经 Agent Inbox 转换为版本化、持久化 `BatchContinuationResult` 进入 Session Lane；它不是原 Graph 函数返回，也不恢复旧 Graph。
- 确定性的成功投影、状态收尾和费用快照处理不依赖模型；只有确需用户解释或下一步语义选择时才创建 Agent Turn。
- Agent 不重试 Worker 或 Provider；Worker 按执行状态恢复，Business `PrepareGeneration/FinalizeGeneration` 按稳定幂等键和 receipt 恢复。系统不支持退款。

## 17. HITL、安全与权限

### 17.1 Tool 风险等级

每个 Tool 必须声明风险等级、权限、最大影响范围和是否强制 HITL。以下操作一律视为高风险并在副作用前强制 HITL：

- 对外发布、公开分享或发送内容；
- 最终扣费、不可逆资产变更或超出普通预算的生成；
- 删除、覆盖、批量变更或不可逆操作；
- 其他由产品或安全评审指定的高风险能力。

### 17.2 Approval

- 用户在自然语言中说“确认”“同意”不构成审批。
- Approval 必须是 PostgreSQL 中的正式记录，绑定 ApprovalID、用户、资源 ID、资源版本、动作、规范化参数摘要、费用/影响摘要、过期时间和版本。
- 审批决定使用稳定 Decision ID 和幂等键；重复批准、拒绝、过期和版本变化必须确定性处理。
- 执行前再次校验权限、资源版本、Approval 版本和摘要；任何不匹配都必须失败关闭并重新发起审批。
- 高风险 Tool 不得通过 Skill、Prompt、模型自由文本或管理员配置绕过 HITL。
- 当前六个 AIGC Graph Tool 默认使用持久化 Approval 加新的可信 Continuation Turn，不在审核点保留原 Graph 调用栈；若某个独立 Graph Tool 设计确需 Interrupt/Checkpoint/Resume，必须先完成 outer/inner Checkpoint、Mapping、Version/Fence、Fallback 和恢复测试评审。

### 17.3 默认禁用能力

- 通用 HTTP Client Tool；
- 宿主文件系统读写 Tool；
- Shell、脚本和动态代码执行；
- 任意 SQL Tool；
- 动态 Tool Search 或从 Skill 注册 Tool；
- 未审核的 MCP/外部 Tool。

如需启用，必须先提交威胁模型、权限、网络/文件沙箱、凭据隔离、审计、HITL 和故障恢复专项设计，并取得用户评审。

## 18. 预算、限流与取消

- 唯一 Agent Profile 必须声明迭代次数、模型调用数、Tool 调用数、输入/输出/总 Token、总时间、Node 超时、并发、费用和异步任务数量的硬预算。
- 具体数值放在版本化运行配置中，不写死在本规范、Prompt、Skill 或业务代码中。
- 预算在 Turn 开始时冻结；Skill、用户输入和 Tool Result 不得提高预算。
- 主 Agent、Graph、Node、模型、Tool 和外部 RPC 共享父级 Deadline；子调用不得突破剩余预算。
- 重试消耗同一预算，不得通过重新进入 Middleware、Graph 或 Tool 重置计数。
- 循环必须配置最大步骤和预算耗尽结果；并行必须有固定上限，禁止无界 goroutine、Channel、批次和 Provider 并发。
- 用户主动“重新生成”是新的业务操作和新幂等语义，不得与技术重试混淆。
- 取消必须传播到 Runner、Graph、Model、RPC 和可取消的 Tool；已经提交的外部副作用按 Operation 状态继续收尾或补偿，不能假装回滚。

## 19. Event、A2UI 与前端输出

- PostgreSQL Session Event Log 是对话事件和 UI 投影的权威日志，按 Session 使用单调递增序号并 append-once。
- Redis 只发送“有新事件”的提示，不承载唯一事件内容或消费进度。
- SSE/流式接口从客户端最后确认序号重放，再切换实时通知；断线重连不得丢失终态。
- 原始 AgentEvent、Eino Event、Checkpoint、Graph State 和 Provider Payload 不得直接暴露给前端。
- A2UI 是权威状态的版本化投影，不是业务真源；事件必须使用严格 DTO、版本、白名单组件和失败关闭解析。
- A2UI 必须位于独立 `agent/internal/a2ui/{protocol,component,action,registry,validator,projector,publisher}` 包；前端使用独立 `features/aigc/a2ui/` 包。
- 所有交互组件组合 Card 公共结构；Card 具备安全 Markdown 展示能力。白名单至少覆盖单选、多选、提交按钮、输入框、多图片/视频/音频、纵向步骤、Tool Renderer 和 Status Renderer。
- Worker 不创建 A2UI；Worker/Business 领域事件先进入 Agent Inbox，再由确定性 Projector 更新创作页聊天框中的 Card。未知版本、组件或 Action 必须失败关闭或安全不可交互降级。
- 同一聚合的投影携带 Aggregate Version，拒绝乱序回滚；重复事件按稳定 EventID 去重。
- 模型输出先完整冻结，再进行确定性 A2UI 投影；投影失败重放同一冻结输出，不重新调用模型。
- Operation/Batch/Job 的底层状态不得在聊天中展开为大量内部卡片；前端只展示审核通过的高层 Capability 状态和必要资源刷新指令。

## 20. 错误、重试与补偿

- 对外错误使用稳定错误码、可重试标记和安全中文信息，禁止返回堆栈、SQL、内部地址、Provider 原文、Prompt 或 Secret。
- 至少区分：参数错误、权限拒绝、版本冲突、预算耗尽、模型结构错误、模型瞬时失败、RPC Unknown Outcome、等待审批、异步接受、Worker 终态失败和不可恢复业务错误。
- 重试必须有单一 Owner、最大次数、退避和随机抖动；未知结果必须先核对权威状态。
- Validator 失败默认关闭，不得让非法模型输出进入 Command；有限纠错次数耗尽后返回稳定错误。
- 事务失败不发布 Outbox；Outbox 重投不重复领域副作用。
- 补偿责任在设计文档中明确归属 Agent Graph、Business、Worker、Provider 或人工处置，禁止多层同时补偿。
- 进程崩溃恢复必须优先重放 Checkpoint/Receipt/冻结输出，不得默认重新调用模型、Tool 或外部副作用。

## 21. 日志、Trace 与数据保护

### 21.1 默认禁止记录

普通日志和 Trace 禁止保存：

- 完整 System/User Prompt、完整会话和摘要原文；
- 完整 Tool Input/Output、RPC Payload、Provider Payload；
- Checkpoint 原文、Graph State 原文；
- Model Reasoning/思维链；
- API Key、Token、Cookie、Authorization、签名 URL 和数据库凭据；
- 不必要的个人信息、企业素材和生成内容。

### 21.2 允许记录的最小字段

允许使用脱敏或摘要后的：

- RequestID、SessionID、InputID、TurnID、RunID、GraphRunID、ToolCallID、OperationID；
- Tool Key、Graph/Node Key、Prompt/Skill/Schema Version；
- 状态、稳定错误码、耗时、Token 数、费用数值、重试次数；
- 输入/输出长度、数量、内容 Hash 和安全分类。

确因业务回放需要保存的会话内容、Tool Payload、Checkpoint 或模型原文必须进入独立受控存储，并具有：

- 明确目的和数据分类；
- 独立访问控制与审计；
- 传输和静态加密；
- 可配置保留期限和到期删除；
- 查询、导出和删除流程；
- 与普通日志、Trace 隔离。

## 22. 配置、注册发现与生命周期

- 配置按环境注入并在启动时一次性校验；缺少必需配置、预算、Tool 风险元数据或数据库 Schema 时失败启动。
- etcd 只用于 Agent 注册和 Business/其他内部 RPC 服务发现；不得存放 Prompt、Skill、预算、开关和业务配置。
- 配置热更新不得改变正在执行 Turn 的 Prompt、Tool Registry 和预算快照；Skill 发布只对新 Session 生效，已有 Session 始终使用冻结快照。
- 健康检查区分 Liveness 和 Readiness。数据库不可用、Migration 未就绪、Tool Registry/Graph Compile 失败时不得 Ready。
- 启动顺序：加载并校验配置 → 连接 PostgreSQL/Redis/etcd → 校验 Schema → 创建 ChatModel → Compile Graph → 构建 Tool Registry → 组装 Middleware/主 Agent/Runner → 启动 Processor 和传输层 → 注册 etcd。
- 优雅退出顺序：先从 etcd 摘除并停止接收新输入 → 停止 Claim → 等待有界 Drain → 保存可恢复状态 → 关闭 HTTP/RPC、Redis、PostgreSQL 和观测组件。

## 23. 测试与评测规范

### 23.1 单元测试

- 每个确定性 Node、Validator、Branch、状态迁移、幂等摘要、DTO Mapper 和 Middleware 必须有表驱动测试。
- 每个 Branch 的所有目标和默认失败分支至少一条测试。
- Fake ChatModel 覆盖正常、缺字段、多字段、非法枚举、超长、流中断、超时和有限纠错耗尽。
- Tool Schema 测试确认可信上下文字段未暴露给模型。
- Middleware 测试确认顺序、消息配对、摘要边界、Skill 冻结和异常脱敏。
- Stream 测试确认 Reader 关闭、取消传播和拼接结果冻结。

### 23.2 Graph Tool 测试

- Graph 能在启动阶段 Compile，实际 Node/Edge/Branch 与设计文档一致。
- ChatModel Node、Validator Node 和 Command Node 必须分离并可独立测试。
- 每条状态迁移和非法迁移、同键重放、同键异义冲突、并发 Fence 和事务回滚均有测试。
- 异步 Tool 只创建 Operation/Job/Outbox 并返回 `accepted`，测试不得等待 Worker。
- 高风险 Tool 覆盖批准、拒绝、过期、重复决定、资源版本变化和摘要冲突。
- 覆盖 RPC Unknown Outcome、Checkpoint/Receipt 恢复、进程重启、预算耗尽、循环上限和部分失败。
- 覆盖批量查询并断言无循环 SQL、N+1 和不受限扫描。

### 23.3 集成、契约与评测

- 集成测试使用 Testcontainers 启动真实 PostgreSQL 16、Redis 和 etcd；不得以 SQLite 代替 PostgreSQL 行为。
- PostgreSQL 测试验证空库 Up、上一版本升级、索引、中文 COMMENT、无物理外键和并发锁语义。
- Agent/Business RPC、Worker terminal event/Continuation、Tool Schema、Event/A2UI 和 Skill 发布快照使用契约测试。
- 评测集覆盖每个 Skill 和 Graph Tool 的正常意图、拒绝、歧义、Prompt Injection、越权、预算和回归场景。
- CI 默认使用 Fake Model 或录制的受控响应，禁止默认调用付费 DeepSeek；真实模型评测必须显式触发、设置费用上限并脱敏数据。
- 新增 Prompt、Skill 规则、模型参数、Graph 拓扑或 Tool Schema 时必须提供基线对比，不能只以“能运行”验收。

## 24. 与现有设计文档的关系

实施 Agent 或 AIGC Graph Tool 前，除本规范外还需要按改动范围读取：

- `docs/aigc-chatmodelagent-demo-design.md`
- `docs/aigc-tool-storyboard-design.md`
- `docs/aigc-worker-design.md`
- `docs/aigc-a2ui-design.md`
- 对应的 `docs/design/agent/graphtool/<tool_key>-design.md`

当前 `master` 是生产版重构基线，尚未建立 `business/`、`agent/`、`worker/` 三个生产 Module，也没有可运行的服务端 Runtime 或 Migration。现有 AIGC 设计文档同时描述历史分支实现和目标形态，实施和评审必须明确区分，禁止把历史能力或目标项表述为当前分支已实现。特别是 Redis Streams、独立 Stage Ledger、持久化 ToolOperationResult、确定性 PostBatchContinuation Graph、统一 Projector/Inbox、统一最终事务等目标能力，在当前分支的代码和 Migration 实际存在前不得作为当前能力验收。

旧设计中的 `internal/aigc/**`、根 Module 等路径以及 `main` 分支实现只可作为迁移参考，不能覆盖本规范确认的 `agent/internal/**`、`business/**`、`worker/**` 三 Module 布局，也不得整分支恢复为生产基线。修改相关能力时必须把涉及的代码路径和契约映射到新的功能包结构，并同步修正文档。设计文档中的“已实现”声明必须用当前分支的代码、Migration 和测试核验，不能仅凭文字继承。

设计文档与本规范冲突时按以下已确认边界处理：

- 删除或替换无业务含义的 Tenant/TenantID；
- 文档中的外键关系均视为逻辑外键，不得创建数据库物理外键；
- Checkpoint 权威来源使用 PostgreSQL，不使用 Redis；
- 用户、鉴权、Skill 草稿/发布、Storyboard、Binding、Asset、积分、费用和管理配置留在 Business，通过 RPC 交互；
- Worker 只负责生成/TOS/Business Finalize，terminal event 经 Agent Inbox 进入 Session Lane；
- 当前实现、目标设计和本次变更范围必须在 PR 中明确标注。

## 25. Commit 与 PR 规范

### 25.1 Commit 格式

~~~text
<type>(<scope>): <中文摘要>
~~~

允许的 Type：

~~~text
feat fix refactor perf test docs build ci chore revert
~~~

常用 Scope：

~~~text
agent runtime middleware skill tool graphtool prompt model checkpoint approval event rpc storage migration observability
~~~

示例：

~~~text
feat(graphtool): 增加素材分析图工具
fix(runtime): 修复同一会话并发提交问题
docs(agent): 补充故事板图工具状态机
~~~

### 25.2 提交与 PR 门禁

- 一个 Commit 只处理一个明确目的；禁止提交无法构建或测试的中间状态。
- IDL/OpenAPI、DTO 和生成代码保持同一变更；禁止手工修改生成代码。
- 新增 Graph Tool 的实现必须基于已审核的独立设计文档；实现 PR 同时提供 Prompt、Schema、Registry 和测试。修改既有 Graph Tool 时，设计文档与代码在同一 PR 同步更新。
- Migration 与依赖代码在同一 PR；已发布 Migration 禁止修改。
- Eino、模型 Adapter、Kitex 或 GORM 升级使用独立 PR。
- 禁止提交 Secret、`.env`、日志、数据库文件、Checkpoint、会话导出、模型原始响应和构建产物。
- PR 必须说明：行为影响、Skill/Prompt/Tool/Graph 契约、Node/状态机变化、数据库和回滚、幂等/重试/HITL、预算、安全、数据保留、当前与目标能力边界、测试和评测证据。

## 26. 合并前检查清单

- [ ] Agent Module Root 固定为 `Dora-Agent/agent`，具有独立 `agent/go.mod`、`agent/go.sum`，并能在 `GOWORK=off` 下独立构建。
- [ ] Agent 生产 Runtime 入口仅位于 `agent/cmd/agent-service`。
- [ ] 代码按 `middleware`、`tool`、`graphtool`、`chatmodelagent`、`prompt` 等功能包组织，未套用统一分层目录。
- [ ] 项目只有一个主 ChatModelAgent，未引入子 Agent、DeepAgent、AgentAsTool 或多 Agent。
- [ ] Eino 锁定 v0.9.10，使用经典 `*schema.Message` 和普通 DeepSeek Adapter。
- [ ] 每个 Graph Tool 都有独立中文设计文档，且 Tool Key、代码包、Graph Name、文档一一对应。
- [ ] Graph Tool 文档同时包含流程图、Node 清单/类型、Graph State、业务状态机和状态迁移表。
- [ ] `graph.go` 只组装拓扑；ChatModel 使用 ChatModel Node，输出经独立 Validator 后才进入副作用。
- [ ] 无环 Graph 使用 AllPredecessor 且另定义 typed Fan-in；循环 Graph 使用 AnyPredecessor 和 MaxRunSteps；ToolsNode、SubGraph 均有显式有界配置。
- [ ] Tool 调用结果、Approval、Operation、Batch、Job 使用各自 Owner 明确的状态机，没有压平混用。
- [ ] 当前 Tool Registry 只包含审核通过的 v1 高层能力；Skill 和外部配置不能动态新增 Tool。
- [ ] 普通 Business RPC 未被无必要包装为 Tool，Agent 未直连 Business 数据库。
- [ ] Runtime Skill 只从 Business published snapshot 加载并以内联模式注入；产品仅有 draft/published，Session 快照冻结且新发布只影响新会话，未使用 `fork`/`fork_with_context`。
- [ ] Skill 脚本、通用 HTTP、宿主文件、Shell、任意 SQL 和动态 Tool Search 保持禁用。
- [ ] 生产调用统一经过 Runner 和持久化 Session Lane，输入先落 PostgreSQL 再唤醒。
- [ ] 同一 Session 串行执行，InputID/TurnID 稳定，Lease/Fence 和 Head-of-Line 规则正确。
- [ ] Skill 在 Session 内冻结；Prompt、Tool Registry、消息边界和预算在 Turn 内冻结，恢复不混入后续消息或新 Skill 发布。
- [ ] PostgreSQL 是 Session、Run、Checkpoint、Receipt、Approval、Operation、Batch、Job、Outbox 和 Event 权威来源；Redis 只缓存和唤醒。
- [ ] Model/Tool 输出先冻结再投影；投影失败不会重新调用模型或 Tool。
- [ ] Agent 数据只位于独立数据库和 `agent` Schema，Migration 只位于 `agent/migrations`。
- [ ] Agent-owned Operation/Batch/Job/Outbox 的 Worker 消费使用公开版本化契约、契约测试和最小数据库权限。
- [ ] Storyboard/Binding/Asset/积分只归 Business；Worker 只经 Finalize RPC 请求 Asset/可选 Binding，terminal Outbox 经 Agent Inbox 进入 Session Lane。
- [ ] Runtime 数据访问统一使用 GORM，启动时未使用 AutoMigrate。
- [ ] Migration 和实际 Schema 不含物理外键或数据库级联；逻辑外键、索引和业务完整性校验完整。
- [ ] 所有新表和字段都有中文数据库 COMMENT，当前实现没有 Tenant 字段。
- [ ] Entity、Model、DTO、业务类型和方法具有中文注释；业务判断、状态、幂等和事务代码处说明了原因。
- [ ] 前端、RPC、Event、Tool、Session Input 和 Job Payload 均使用 DTO，未暴露 Entity/GORM Model/原始 AgentEvent。
- [ ] 没有循环执行同一或同构 SQL，没有 N+1；复杂查询映射为专用 DTO。
- [ ] 高风险 Tool 在副作用前使用正式 Approval，不能用自然语言或 Skill 绕过。
- [ ] 硬预算由版本化配置提供，循环、重试和并发不会重置或突破预算。
- [ ] 普通日志和 Trace 未保存完整 Prompt、会话、Tool Payload、Checkpoint 或 Reasoning。
- [ ] Worker terminal event 通过 Outbox/Agent Inbox/持久化 Continuation 进入 Session Lane，Agent 不等待或重试 Worker/Provider。
- [ ] A2UI 使用独立后端/前端包、Card Markdown 基类和白名单组件；Worker 不生成 A2UI，未知组件/Action 失败关闭。
- [ ] 当前实现与目标设计已明确区分，未把目标能力表述为已实现。
- [ ] Unit、Graph、Integration、Contract、Race、安全和 Evaluation 门禁已通过。
- [ ] Commit 和 PR 描述符合本规范。
