# 业务服务端开发规范

> 状态：已按 2026-07-13 评审意见修订
> 适用范围：`Dora-Agent/business` 独立 Go Module、`business/cmd/business-service` Runtime 和 `business/migrations`
> 不适用范围：Business Worker、Agent Service

## 1. 规范等级

- **MUST**：强制要求，违反时不得合并或发布。
- **SHOULD**：默认要求；偏离时必须在代码评审中说明原因。
- **MAY**：按业务场景选择。

本文中的“类”对应 Go 中的具名类型、结构体或接口。

## 2. 已确认的技术边界

1. 仓库根目录 `Dora-Agent/` 是多 Module 仓库容器，不是 Business Service Go Module 或 Runtime。
2. Business Service Module 根目录固定为 `Dora-Agent/business`，使用独立的 `business/go.mod`、`business/go.sum`、配置、构建产物和发布版本。
3. Business Service Runtime 入口固定在 `business/cmd/business-service`；Business Worker 位于 `Dora-Agent/worker`，Agent Service 位于 `Dora-Agent/agent`，三者分别使用独立 Go Module 和独立 Runtime。
4. Business Service 对外 HTTP 使用 Gin，内部 RPC 使用 Kitex + Thrift。
5. etcd 只用于服务注册发现，不作为业务配置中心。
6. Runtime 数据访问统一使用 GORM；Schema 统一由版本化 SQL Migration 管理。
7. PostgreSQL 使用 16，当前本地镜像为 `postgres:16.4-alpine`，与 `deploy/local/compose.yaml` 一致。
8. Redis 只用于缓存、限流、短期协调和异步任务唤醒。
9. PostgreSQL Job/Outbox 是异步任务的权威状态。
10. 当前系统没有 Tenant；用户分为普通用户和企业用户，企业用户不等于租户。
11. 应用生成 UUIDv7。
12. 积分使用 `bigint`；金额固定保留两位小数。
13. 严禁在数据库层面创建物理外键约束；表、Entity、GORM Model 和 DTO 可以并应按业务关系保留逻辑外键字段。
14. 数据库表、字段必须具有中文数据库 COMMENT。
15. 实体、业务类型和业务方法必须具有中文注释。
16. 前端接口和跨领域参数必须使用 DTO。
17. 单次请求、UseCase 或事务内严禁循环执行同一或同构 SQL；复杂查询使用 JOIN 等集合查询并映射为专用 DTO，或重构为固定数量的批量查询。

## 3. 技术选型

| 能力 | 选型 | 要求 |
| --- | --- | --- |
| Go | Go 1.26.3 | 本地使用 `/Users/figo/sdk/go1.26.3/bin/go` |
| HTTP | Gin | Handler 只处理协议边界 |
| RPC | Kitex + Thrift | IDL 优先，生成器与 Runtime 版本一致 |
| 注册发现 | etcd | 只负责注册发现 |
| 数据访问 | GORM + PostgreSQL Driver | Repository 之外不得直接使用 GORM |
| 数据库 | PostgreSQL 16 | 本地使用 `postgres:16.4-alpine` |
| 数据迁移 | golang-migrate + SQL | `business/migrations` 是 Business Schema 的唯一 Migration 目录，禁止服务启动时 AutoMigrate |
| 缓存 | Redis | 不作为业务最终事实来源 |
| 对象存储 | TOS + ObjectStore 接口 | 业务层不依赖厂商 SDK 类型 |
| 日志 | log/slog JSON | 全链路结构化日志 |
| 可观测性 | OpenTelemetry | Trace、Metric 统一经 OTLP 输出 |
| 测试 | testing + Testcontainers | 集成测试使用真实中间件 |

### 3.1 依赖版本

- `business/go.mod` 必须同时声明 `go 1.26` 和 `toolchain go1.26.3`。
- 依赖必须使用精确版本，禁止 `@latest`、分支版本和无说明的 pseudo-version。
- Kitex Runtime、Kitex 生成器和 thriftgo 必须成组升级。
- GORM Core 和 PostgreSQL Driver 必须成组验证。
- etcd Client 和注册发现插件必须完成注册、续租、重连和摘除兼容测试。
- `business/go.mod`、`business/go.sum` 必须提交；依赖变更后在 `business` Module 中执行 `go mod tidy` 和 `go mod verify`。
- `business/go.mod` 禁止保留本地路径 `replace`，不得使用仓库根或其他 Module 的 `go.mod` 代替 Business 依赖清单。
- 依赖大版本升级必须使用独立 PR，不得与普通功能开发混合。

## 4. 本地开发环境

### 4.1 Go SDK

从仓库根目录执行 Business Module 的启动、测试、生成代码和依赖维护命令时，默认使用：

~~~bash
/Users/figo/sdk/go1.26.3/bin/go -C business version
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C business test ./...
~~~

- MUST：不得依赖 PATH 中版本不明确的 `go`。
- SHOULD：项目脚本允许通过 `GO_BIN` 覆盖 Go 路径，便于 CI 和其他开发机使用。
- MUST：`business` Module 在 `GOWORK=off` 时能够独立构建和测试。
- MAY：仓库根目录使用 `go.work` 组合 `business`、`worker`、`agent` 方便本地联调，但不得改变 Module Root，也不得依靠它隐藏未声明的跨 Module 依赖。

### 4.2 Docker 中间件

本地 PostgreSQL、Redis、etcd 使用 Docker 启动，业务服务运行在宿主机：

| 中间件 | 本地基线 | 默认端口 |
| --- | --- | --- |
| PostgreSQL | `postgres:16.4-alpine` | 5432 |
| Redis | Redis 7 Alpine，Compose 锁定已验证版本 | 6379 |
| etcd | etcd 3.6.x，Compose 锁定已验证版本 | 2379 |

- MUST：禁止使用 `latest` 镜像。
- SHOULD：CI 和可复现环境在镜像 Tag 之外锁定 Digest。
- MUST：容器必须配置 Healthcheck；服务启动脚本等待健康状态，不能只等待端口开放。
- MUST：本地测试使用独立测试数据库，禁止清空开发数据库。
- MUST：本地账号、密码和端口只用于开发环境，不得复制到生产配置。
- MUST：连接信息通过环境变量或本地配置注入，禁止写死在 Go 代码中。

## 5. Module 与目录规范

仓库固定采用三个并列的独立 Go Module。Business Service 目录如下；`worker/` 和 `agent/` 不属于 Business Module：

~~~text
Dora-Agent/
├── business/                       # Business Service 独立 Go Module
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── business-service/
│   │       └── main.go
│   ├── api/
│   │   ├── openapi/
│   │   └── thrift/
│   ├── kitex_gen/
│   ├── internal/
│   │   ├── bootstrap/
│   │   ├── user/
│   │   ├── auth/
│   │   ├── credit/
│   │   ├── asset/
│   │   ├── adminconfig/
│   │   ├── outbox/
│   │   ├── transport/
│   │   │   ├── http/
│   │   │   └── rpc/
│   │   ├── adapter/
│   │   │   ├── postgres/
│   │   │   ├── redis/
│   │   │   ├── etcd/
│   │   │   └── tos/
│   │   └── platform/
│   │       ├── config/
│   │       ├── logging/
│   │       ├── observability/
│   │       ├── clock/
│   │       └── idgen/
│   ├── migrations/                 # Business Schema 的唯一 Migration 目录
│   ├── tests/
│   │   ├── integration/
│   │   └── contract/
│   └── deployments/
├── worker/                         # Business Worker 独立 Go Module
├── agent/                          # Agent Service 独立 Go Module
├── docs/
└── go.work                         # 可选，仅用于本地联调
~~~

### 5.1 目录职责

- `business/cmd/business-service` 是 Business Service 生产 Runtime 的唯一入口，只负责配置加载、依赖组装、启动和优雅关闭，不得包含业务逻辑。
- 业务代码按 `user`、`auth`、`credit`、`asset`、`adminconfig` 等业务能力组织。
- 禁止建立全局 `controller`、`service`、`repository`、`model` 大目录。
- 禁止创建 `common`、`utils`、`helper`、`misc`、`base` 等无明确边界的包。
- 生成代码集中存放并带 generated 标记，禁止手工修改。
- Business Service 不得 import Worker、Agent 或仓库根目录下其他 Module 的 `internal` 包。
- 跨 Module 交互使用 Thrift、HTTP、事件 DTO 或其他显式契约。
- 不为共享少量类型默认创建第四个公共 Go Module。
- `business/migrations` 只能维护 Business Module 明确拥有的表和 `business` Schema 对象；Worker、Agent 或仓库根目录不得重复创建或修改这些对象。
- 同一数据库表只能有一个 Migration Owner；Owner 是定义表生命周期、写入事务和数据不变量的 Module，不是碰巧读取或消费该表的 Runtime。

### 5.2 依赖方向

~~~text
transport  -> 业务能力包 -> 领域实体
adapter    -> 实现业务能力包定义的接口
bootstrap  -> 组装 transport、业务实现和 adapter
~~~

- 领域实体不得依赖 Gin、Kitex、GORM、Redis、TOS SDK 或生成代码。
- Repository 接口定义在消费它的业务包中。
- 首版使用构造函数手工注入，禁止 Service Locator 和可变全局实例。

## 6. Go 编码与中文注释规范

### 6.1 格式和命名

- 所有 Go 文件执行 `gofmt` 和 `goimports`。
- 包名使用简短、小写、单数、具有业务含义的名称。
- 缩写保持统一：`ID`、`URL`、`HTTP`、`RPC`、`DTO`。
- 文件名使用小写 `snake_case`。
- 接口保持最小化并定义在消费方，不为每个实现机械创建接口。
- 禁止用 `map[string]any` 代替已有稳定结构。
- TODO 格式为 `TODO(username): 中文说明和关联任务`，禁止无上下文 TODO。

### 6.2 强制中文注释

以下非生成代码必须添加中文注释：

1. 所有领域实体、值对象和持久化 Model。
2. 所有 DTO、Command、Query 和 Event 类型。
3. 所有业务 Service、UseCase、Handler 和 Repository 接口。
4. 上述类型中的所有字段。
5. 所有导出的类型、函数、方法、变量和常量。
6. 领域层、应用层中的所有方法，包括未导出方法。
7. 基础设施层中包含业务判断的方法。

类/具名类型和方法的中文注释必须满足：

- 每个实体、持久化 Model、DTO、业务 Service、UseCase、Handler 和其他业务具名类型都要说明职责、适用范围和关键业务不变量。
- 每个业务方法，无论导出与否，都要说明用途、主要前置条件、成功后的业务结果和关键错误语义。
- 构造方法要说明创建对象时建立的业务不变量；状态变更方法要说明允许的源状态和目标状态。
- 注释不能只写“处理数据”“执行操作”等无业务含义的描述。

导出标识符注释必须以标识符名称开头，同时使用中文说明：

~~~go
// User 用户实体，表示平台中的普通用户或企业用户。
type User struct {
	// ID 用户唯一标识，由应用生成 UUIDv7。
	ID uuid.UUID

	// Status 用户状态，决定用户是否能够访问受保护资源。
	Status UserStatus
}

// Disable 禁用用户，并阻止该用户继续访问受保护资源。
func (u *User) Disable() error {
	// 已注销用户已经进入不可恢复流程，禁止再次修改状态，避免恢复已清理的账号。
	if u.Status == UserStatusCancelled {
		return ErrUserCancelled
	}

	u.Status = UserStatusDisabled
	return nil
}
~~~

涉及以下内容时，必须在判断或流程入口使用中文注释说明业务意图：

- 状态流转和状态机分支；
- 权限和鉴权判断；
- 幂等命中、冲突和重放；
- 积分、金额、退款和冲正；
- 事务执行顺序；
- 降级、重试和补偿；
- 兼容旧 DTO、旧事件或旧数据库字段；
- 任何不满足条件时会改变业务结果的判断。

类和方法的中文注释不能替代方法体中的流程注释。业务判断、状态流转、幂等和事务逻辑必须在对应判断或流程代码处说明“为什么”和“一致性影响”，禁止逐行翻译代码。修改逻辑时必须同步更新注释；过期注释视为缺陷。生成代码和第三方代码不受中文注释要求约束。

### 6.3 Context、错误和并发

- `context.Context` 必须是调用链首参数，命名为 `ctx`。
- Context 不得存入业务结构体，不得传 `nil`。
- HTTP、RPC、数据库、Redis和对象存储全链路传播 Context，并设置超时。
- 请求处理中禁止用 `context.Background()` 启动脱离请求生命周期的业务任务；必须写入持久化 Job/Outbox。
- 错误增加上下文使用 `%w`，判断使用 `errors.Is/As`，禁止比较错误字符串。
- 领域层输出稳定错误码，不向外暴露 GORM、PostgreSQL、Redis 或 Kitex 原始错误。
- 业务失败禁止使用 panic。
- 错误在决定最终处理方式的边界记录一次，避免每层重复日志。
- 每个 goroutine 必须有 Owner、取消条件和等待回收路径。
- 所有并发、Channel、连接池和下游调用必须有界。
- 禁止 fire-and-forget goroutine。

## 7. Entity、Persistence Model 与 DTO

### 7.1 类型边界

- **Entity**：领域实体，承载业务状态和行为，不包含 JSON、GORM、Thrift Tag。
- **Persistence Model**：GORM 持久化模型，只存在于 PostgreSQL Adapter。
- **DTO**：前端接口、RPC、跨领域调用和事件的数据契约。
- **Command/Query**：单领域内部应用层输入，不得替代跨领域 DTO。

### 7.2 DTO 强制规则

- 暴露给前端的 Request 和 Response 必须使用 DTO。
- HTTP、RPC、事件和跨领域调用的输入输出必须使用 DTO。
- 禁止直接返回 Entity、GORM Model 或数据库查询 Record。
- Thrift 生成类型只属于协议层，进入业务层前必须显式转换。
- Entity、Persistence Model、DTO 之间必须使用显式 Mapper。
- DTO 不得包含 GORM Tag、数据库关联对象或领域行为。
- Create、Update、Patch、Query、Response 使用不同 DTO，禁止一个 DTO 覆盖所有场景。
- Patch 可选字段使用指针或明确 Optional 类型，区分“未传”和“传零值”。
- DTO 格式校验在 Transport 层完成；权限、状态、余额等业务规则仍由 Service/Entity 校验。
- 跨表复杂查询必须定义专用 QueryDTO/ReadDTO 接收结果，不得将 JOIN 结果扫描到领域 Entity 或直接返回 GORM Model。
- 密码摘要、Token 摘要、内部状态和逻辑删除字段不得进入前端 DTO。
- 金额 DTO 使用两位小数字符串，例如 `"12.30"`，禁止 JSON 浮点数。
- 时间 DTO 统一使用 UTC RFC3339。

建议命名：

~~~text
CreateUserRequest
CreateUserResponse
UserDTO
CreditChangeDTO
AssetCreatedEventDTO
~~~

## 8. HTTP、RPC 与服务注册

### 8.1 HTTP

- API 路径统一使用 `/api/v1` 前缀。
- 资源路径使用复数名词，JSON 字段使用 `snake_case`。
- Handler 只负责 DTO 绑定、格式校验、鉴权、调用 Service 和错误映射。
- Handler 禁止直接调用 GORM。
- 成功响应返回资源 DTO；错误响应返回稳定 `code`、`message`、`request_id` 和可选 `details`。
- 列表接口使用 Cursor Pagination，禁止大表深度 Offset。
- 积分、费用、资产变更和异步任务创建必须支持幂等键。
- `http.Server` 必须配置 Header、Read、Write、Idle Timeout 和请求体上限。
- Gin 使用 `gin.New()` 并显式安装 Recovery、Trace、鉴权和日志中间件。
- Trusted Proxies 必须显式配置；无反向代理时设为空。
- CORS 使用明确 Allowlist，禁止生产环境全开放。

### 8.2 Kitex 与 Thrift

- Thrift IDL 是 RPC 契约唯一来源。
- IDL 和生成代码必须纳入版本管理，生成代码禁止手工修改。
- Kitex Runtime、Kitex CLI、thriftgo 必须锁定兼容版本。
- 已发布字段编号禁止复用。
- 新字段必须兼容旧消费者；不得直接改变已有字段语义。
- RPC DTO 进入业务层前必须转换。
- Client 必须显式配置连接和请求超时。
- 写操作默认禁用框架自动重试；只有明确幂等并受到重试预算约束时才可启用。

### 8.3 etcd

- etcd 只用于服务注册发现，不保存业务配置。
- 服务完成配置、数据库等必要检查后再注册。
- 注册失败时 Readiness 不得成功。
- 注册地址必须是其他实例可访问的 Advertised Address。
- 禁止注册 `127.0.0.1`、`localhost` 和 `0.0.0.0`。
- 关闭时先停止接收流量，再注销服务，最后关闭资源。
- 本地使用 Docker etcd；生产使用 TLS、独立账号和最小权限。
- TLS 文件读取失败必须阻止启动，禁止降级为明文连接。

## 9. GORM 数据访问规范

### 9.1 基础规则

- Runtime 数据访问统一使用 GORM PostgreSQL Driver。
- Repository 之外禁止直接调用 GORM。
- 业务层不得接收或返回 `*gorm.DB`。
- 每次数据库操作必须调用 `db.WithContext(ctx)`。
- 禁止在服务启动时或共享环境中调用 `AutoMigrate`。
- `business/migrations` 中的版本化 SQL Migration 是 Business Schema 的唯一事实来源。
- GORM 初始化必须设置 `DisableForeignKeyConstraintWhenMigrating: true`，但该设置不能替代 Migration 检查。
- 禁止嵌入 `gorm.Model`；ID、时间、版本和删除字段必须显式声明。
- GORM Model 必须保留业务需要的逻辑外键 ID 字段；MAY 使用 `foreignKey`、`references` Tag 描述查询映射。
- GORM Model 禁止使用生成数据库外键或数据库级联行为的 `constraint` Tag。
- 禁止开启 `AllowGlobalUpdate`。
- 禁止无条件 Update/Delete。
- 谨慎使用 `Save`；更新使用 `Select`、`Updates` 或明确字段列表，避免零值遗漏和全字段覆盖。
- 所有写操作检查 `Error`；关键条件更新还必须检查 `RowsAffected`。
- `gorm.ErrRecordNotFound` 必须转换为稳定领域错误。
- 排序字段、表名和字段名必须来自白名单，禁止拼接用户输入。
- Raw SQL 只能位于 Repository，且必须参数化。
- Preload 必须评审并验证 SQL 数量固定，不得在循环中对每条记录调用 Preload/Association 或其他查询。
- 大结果集必须分页或分批处理。
- GORM Hook 只能处理技术性字段，禁止隐藏业务状态流转或外部调用。
- GORM Logger 接入 slog；生产日志不得输出敏感 SQL 参数。
- 底层 `sql.DB` 必须显式配置连接池大小、连接生命周期和空闲时间。

### 9.2 事务

- 事务边界由 Application/Business Service 决定。
- 使用 `db.WithContext(ctx).Transaction(...)`。
- Repository 不得在调用者无感知时创建相互独立的事务。
- 事务内禁止执行 HTTP、RPC、Redis、对象存储等外部 I/O。
- 积分、费用、资产状态和 Outbox 等强一致修改必须处于同一事务。
- 行锁使用 GORM Locking Clause 或 Repository 内参数化 SQL。
- 更新状态、版本和 Lease 等条件写必须检查 `RowsAffected == 1`。
- 事务重试必须从完整事务入口执行、次数有限，并保证回调不包含非幂等外部副作用。

### 9.3 查询、JOIN 与 DTO

- 在一次 HTTP/RPC 请求、UseCase、事务或业务批处理中，严禁在 `for/range` 循环中反复执行同一 SQL，或只替换 ID、状态等参数后执行同构 SQL。
- 禁止 `for items { db.First/Find/Take/Raw/Scan/... }` 等逐条查询；禁止任何形式的 N+1。
- 涉及多表展示或跨对象读取时，优先使用一次参数化 JOIN、CTE、子查询或聚合查询，并通过明确 `Select` 和字段别名扫描到专用 QueryDTO/ReadDTO。
- 复杂查询 DTO 必须具有中文类型、字段和映射方法注释；DTO 只承载查询结果，不承载 GORM 持久化行为。
- 禁止 `SELECT *` 连表查询；必须显式选择字段，避免同名列覆盖、敏感字段泄漏和无用数据传输。
- 一对多 JOIN 可能造成行数膨胀时，允许使用固定数量的批量查询，例如一次主查询加一次 `WHERE id IN (...)` 查询，再在内存中组装 DTO；查询次数不得随结果行数增长。
- 如果单次批量参数过大，优先使用 PostgreSQL Array/`unnest`、CTE、临时输入表或调整接口分页；不得简单改成循环分批执行同一 SQL。
- GORM Preload 只有在 SQL 数量固定、数据量有界并经过测试时才可使用；禁止在结果循环中调用 Association 查询。
- Repository 返回专用查询 DTO 或领域需要的结果，不得把复杂连表的 GORM Model 图直接暴露给 Service、Handler 或前端。
- Worker 定时轮询、Lease Heartbeat 和经审批的离线 Migration/Backfill 属于生命周期或运维任务，不按 N+1 认定，但仍必须有界、可取消并具有独立规范。

## 10. 数据库设计规范

### 10.1 Schema、命名和通用字段

- 业务表使用 `business` Schema；该 Schema 由 `Dora-Agent/business` Module 独占维护，SQL 和 GORM `TableName()` 显式包含 Schema。
- 表名使用单数 `snake_case`，例如 `user_account`、`credit_ledger_entry`。
- 主键统一为 `id`，逻辑关联字段统一为 `<entity>_id`。
- 时间点字段使用 `_at`，日期使用 `_date`，数量使用 `_count`。
- 主键由应用生成 UUIDv7，数据库类型为 `uuid`。
- 真实时间点统一使用 `timestamptz`，应用和数据库连接时区统一为 UTC。
- `NOT NULL` 为默认；可空字段必须有明确业务含义。
- 核心状态使用英文稳定代码，并使用 `CHECK` 限制合法值。
- JSONB 只保存扩展元数据、外部响应和事件快照；核心查询字段必须独立成列。
- 当前没有 Tenant，禁止预留无业务语义的 `tenant_id`。
- 用户类型和用户状态分开；普通用户与企业用户只是用户类型，不自动引入租户或组织模型。

### 10.2 禁止数据库物理外键，保留逻辑外键

以下规则均为 MUST：

- “物理外键”是 PostgreSQL Schema 中由 `FOREIGN KEY / REFERENCES` 建立并由数据库强制维护的约束；本项目禁止创建此类约束。
- “逻辑外键”是表字段、Entity、GORM Model 或 DTO 中表示业务关联的 ID，例如 `user_id`、`asset_id`、`job_id`；本项目允许并要求按真实业务关系定义此类字段。
- Migration SQL 禁止定义 `FOREIGN KEY`、`REFERENCES`、`ON DELETE CASCADE` 和 `ON UPDATE CASCADE` 等数据库外键约束或数据库级联。
- `user_id uuid NOT NULL` 这类逻辑关联字段是允许的；`user_id uuid REFERENCES business.user_account(id)` 是禁止的。
- 逻辑外键字段统一使用 `<entity>_id` 命名，并明确数据类型、可空语义、中文 COMMENT 和必要普通索引。
- Entity、GORM Model 和跨边界 DTO 可以包含逻辑外键 ID；不得因为禁止数据库外键而删除业务关联字段。
- GORM MAY 使用 `foreignKey`、`references` Tag 描述 Preload/Association 的查询映射，但不得依靠它保证数据完整性。
- GORM 禁止使用会创建数据库约束或数据库级联的 `constraint` Tag。
- 引用存在性、状态合法性和删除前检查由业务 Service 在事务中完成。
- 禁止数据库自动级联；业务需要联动删除、失效或清理时，允许由业务代码通过事务、Outbox 或幂等补偿显式实现，并使用中文注释说明顺序和失败处理。
- 必须提供孤儿数据和逻辑引用完整性的巡检、告警及修复能力。
- 核心数据 SHOULD 通过状态变化或软删除管理，降低并发物理删除产生悬挂引用的风险。

禁止数据库物理外键不等于禁止逻辑外键，也不等于禁止其他约束。主键、唯一约束、`NOT NULL`、`CHECK` 和普通索引仍必须用于保证数据正确性。

CI 必须同时执行：

1. 静态扫描 Migration DDL，禁止物理外键定义；该检查不针对 Go/GORM 中的逻辑外键字段和查询映射 Tag。
2. 集成测试查询 `pg_constraint`，断言业务 Schema 不存在 `contype = 'f'`。

### 10.3 表、字段中文数据库说明

- 每张表必须有非空中文 `COMMENT ON TABLE`。
- 每个字段，包括 `id`、`created_at` 等公共字段，必须有非空中文 `COMMENT ON COLUMN`。
- 中文说明必须描述业务含义；金额说明单位和精度，状态说明代码值。
- 逻辑关联 ID 必须说明关联对象以及“不设置数据库物理外键约束”。
- 创建表或新增字段时，必须在同一个 Up Migration 中添加 COMMENT。
- GORM `comment` Tag 不能替代数据库 COMMENT。
- CI 在空库执行 Migration 后查询 `pg_description`，验证所有业务表和字段存在包含中文字符的说明。

示例：

~~~sql
CREATE TABLE business.user_account (
    id uuid NOT NULL,
    user_type varchar(32) NOT NULL,
    status varchar(32) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_user_account PRIMARY KEY (id),
    CONSTRAINT ck_user_account__user_type
        CHECK (user_type IN ('personal', 'enterprise'))
);

COMMENT ON TABLE business.user_account
    IS '用户账户表，保存平台普通用户和企业用户的基础状态';
COMMENT ON COLUMN business.user_account.id
    IS '用户唯一标识，由业务服务生成 UUIDv7';
COMMENT ON COLUMN business.user_account.user_type
    IS '用户类型：personal-普通用户，enterprise-企业用户';
COMMENT ON COLUMN business.user_account.status
    IS '用户状态，使用业务定义的稳定英文状态代码';
COMMENT ON COLUMN business.user_account.created_at
    IS '用户账户创建时间，使用 UTC 时间';
~~~

### 10.4 积分和金额

- 积分使用 `bigint`，按最小不可分单位存储。
- 金额按字段明确选择“`bigint` 最小货币单位（分）”或“`numeric(p,2)`”两种策略；同一业务含义不得混用。
- 使用 `bigint` 时字段名或中文 COMMENT 必须明确单位为“分”；使用 `numeric` 时小数位固定为 2。
- Go 层使用统一 Money/Decimal 值对象，禁止使用 `float32`、`float64`。
- HTTP/JSON 金额使用两位小数字符串。
- 金额必须同时记录币种；字段 COMMENT 必须说明单位、精度和币种语义。
- 禁止使用 PostgreSQL `money`、`real`、`double precision` 保存金额。
- 积分和费用变更必须写入追加式账本。
- 账本记录必须具有稳定业务引用或幂等键唯一约束。
- 余额更新、账本记录和 Outbox 写入必须处于同一事务。
- 账本记录不得修改或删除；纠错使用冲正记录。

### 10.5 索引、软删除和版本

- 索引必须对应已知查询，不为每个字段默认建索引。
- 不重复创建主键或唯一约束已经提供的索引。
- 逻辑关联字段按查询和清理路径建立普通索引。
- 列表查询使用稳定唯一排序和 Keyset Pagination。
- 只有明确需要恢复的实体才使用软删除。
- 软删除字段统一为 `deleted_at timestamptz NULL`，不同时维护 `is_deleted`。
- 活动态唯一性使用部分唯一索引。
- 账本、审计、Outbox 等追加数据不使用软删除。
- 存在并发修改的聚合根使用显式 `version bigint` 和 CAS 更新。

### 10.6 Migration

- Business Migration 文件必须位于 `business/migrations`；禁止把仓库根目录、`worker/migrations` 或 `agent/migrations` 作为 Business Migration 来源。
- Migration 文件命名为 `YYYYMMDDHHMMSS_description.up.sql` 和对应 Down 文件。
- 已进入任何共享环境的 Migration 禁止修改；修复必须新增 Migration。
- 服务启动只检查 Schema 兼容性，禁止执行 AutoMigrate。
- Migration 使用独立命令或部署 Job 和独立 DDL 账号执行，执行入口必须显式加载 `business/migrations`。
- 生产变更遵循 Expand → Backfill → Switch → Contract。
- 大批量回填使用可暂停、可恢复、有 Checkpoint 的后台任务，不放入单个长事务。
- `CREATE INDEX CONCURRENTLY` 放在独立非事务 Migration。
- 生产 Migration 禁止直接使用 `DROP ... CASCADE`。
- 任意两个 Module 不得修改同一张表；表 Owner 必须在首次创建时明确，并记录在 Owner Module 的 Migration 与契约说明中。

## 11. 业务数据专项规范

### 11.1 用户和鉴权

- 密码使用 Argon2id，保存算法、参数、盐和版本；禁止保存明文或可逆密码。
- Refresh Token、API Key 等服务端只保存摘要。
- 用户邮箱、手机号等标准化值使用独立字段和唯一约束。
- 身份凭证字段不得进入普通用户查询或前端 DTO。
- 用户状态变化必须记录操作人、原因、时间和审计事件。
- JWT 必须固定允许算法并校验 Issuer、Audience、Expiration 和 Not-Before。

### 11.2 资产

- 文件本体存入对象存储，数据库只保存 Object Key、Version、Checksum、Size、MIME 等元数据。
- 业务层只依赖项目级 ObjectStore 接口，不依赖 TOS SDK 类型。
- Bucket 默认私有，下载使用短期签名 URL。
- 数据库和 Job Payload 禁止长期保存短期签名 URL。
- 上传必须限制大小、媒体类型和对象前缀，并校验 Checksum。

### 11.3 管理端配置

- 动态配置必须带版本、状态、发布时间、操作人和审计记录。
- 配置先完成全量校验，再原子发布不可变快照。
- 禁止在 Runtime 中逐字段修改共享配置对象。
- Secret 不进入普通业务配置表或管理端明文响应。
- etcd 不承载管理端动态业务配置。

## 12. Outbox 生产侧规范

- 需要异步执行的业务变更与 Outbox 写入必须处于同一 PostgreSQL 事务。
- PostgreSQL 是 Job/Outbox 权威状态，Redis 只提供低延迟唤醒。
- Outbox Event 必须包含事件 ID、类型、Schema Version、聚合 ID、发生时间和必要快照。
- Event Payload 禁止包含密码、Token、Secret、大文件和临时签名 URL。
- 事务提交前禁止发布 Redis 唤醒消息。
- Redis 发布失败不得回滚已经成功提交的业务事务，由 Outbox 重试恢复。
- Worker 的领取、Lease、Fencing、Retry 和 Dead 状态遵守独立 Worker 开发规范。

## 13. 配置、安全、日志和可观测性

- 配置解析为类型化、启动后不可变的结构体。
- 必填项、Duration、连接池、并发数和 Endpoint 在启动阶段完成校验。
- 配置错误必须阻止启动，禁止使用危险默认值掩盖错误。
- Secret 来自环境变量、Secret Manager 或挂载文件，不进入 Git。
- `.env.example` 只能包含占位值。
- 日志统一使用 slog JSON。
- 日志至少包含 `service`、`version`、`env`、`instance_id`、`request_id`、`trace_id`、`error_code`。
- 密码、Token、Cookie、完整个人信息和原始敏感 Payload 禁止记录。
- 用户输入按不可信数据处理，清理换行和控制字符。
- User ID、Asset ID 等高基数值不得作为 Metric Label。
- OTel Collector 不可用不得阻塞业务请求。
- Readiness 检查是否能继续接收新流量；Liveness 只检查进程是否仍能推进。
- 管理端配置、用户状态、鉴权、积分、费用和资产操作必须记录审计事件。

## 14. 测试与质量门禁

### 14.1 测试规则

- Domain/Application 单元测试不得依赖真实网络。
- Clock、ID Generator 和随机源必须可注入。
- Repository 集成测试使用真实 `postgres:16.4-alpine`，禁止使用 SQLite 替代 PostgreSQL。
- Redis、etcd 集成测试使用本地 Docker 或 Testcontainers。
- 本地测试使用独立测试数据库。
- 金额测试覆盖两位小数、边界、舍入和禁止浮点转换。
- GORM 测试覆盖零值更新、全表更新保护、事务回滚和 `RowsAffected`。
- 列表和复杂查询测试必须验证返回 1、10、100 条数据时 SQL 次数保持固定，不随记录数增长。
- JOIN/CTE 查询测试必须验证字段别名、DTO 映射、分页、空关联和一对多去重。
- 并发测试覆盖幂等请求、积分变更、乐观锁和 Outbox 原子性。
- 无数据库物理外键测试覆盖逻辑外键写前校验、显式联动处理和孤儿数据巡检。
- API 和 Thrift 做契约兼容测试。
- 解析器、DTO 和幂等键生成适合增加 Fuzz Test。

### 14.2 本地与 CI 命令

从仓库根目录对 Business Module 执行本地验证时，使用指定 Go SDK，并关闭 Workspace 依赖以证明 Module 可独立构建：

~~~bash
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C business mod verify
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C business vet ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C business test -shuffle=on -count=1 ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C business test -race ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C business build ./cmd/business-service
~~~

Business CI Job 的工作目录必须是 `business`，或者所有命令显式使用 `go -C business`；不得在仓库根 Module 上执行后误报 Business 门禁已经通过。

PR/主分支质量门禁：

1. gofmt、goimports 无差异。
2. `go mod tidy` 后无差异。
3. `go mod verify`。
4. `go vet ./...`。
5. `golangci-lint run`。
6. `go test -shuffle=on -count=1 ./...`。
7. `go test -race ./...`。
8. `govulncheck ./...`。
9. `go build ./cmd/business-service`。
10. IDL/OpenAPI 重新生成后无差异。
11. 空库完整执行 `business/migrations`。
12. 从上一发布版本升级成功。
13. 对 `business/migrations` 静态扫描，确认不存在数据库外键。
14. PostgreSQL 实库中不存在物理外键约束。
15. 所有业务表和字段具有中文数据库 COMMENT。
16. 列表和批处理查询不存在循环 SQL/N+1，复杂查询返回专用 DTO。

## 15. Git 提交与评审规范

### 15.1 Commit 格式

~~~text
<type>(<scope>): <中文摘要>
~~~

允许的 Type：

~~~text
feat fix refactor perf test docs build ci chore revert
~~~

常用 Scope：

~~~text
user auth credit asset adminconfig api rpc storage migration observability
~~~

示例：

~~~text
feat(user): 增加企业用户创建能力
fix(credit): 修复重复请求导致的积分扣减
refactor(auth): 重构访问令牌校验流程
~~~

### 15.2 提交规则

- 一个 Commit 只处理一个明确、可解释的变更目的。
- 禁止提交无法构建或无法测试的中间状态。
- IDL/OpenAPI 源文件和对应生成代码必须处于同一变更。
- Migration 与依赖它的代码必须在同一 PR 完整提供。
- 已发布 Migration 禁止修改。
- `business/go.mod`、`business/go.sum` 变更必须说明原因。
- 禁止把全仓格式化与功能修改混在同一个 Commit。
- 禁止提交 Secret、`.env`、日志、数据库文件、覆盖率文件和构建产物。
- 禁止使用 `--no-verify` 绕过质量门禁后直接合并。

### 15.3 PR 说明

PR 必须说明：

- 变更目的和业务影响；
- 影响的 API、RPC、DTO 和兼容性；
- 数据库表、索引、Migration 和回滚方式；
- 是否涉及幂等、积分、金额、权限或数据修复；
- 是否涉及异步任务和 Outbox；
- 新增或修改的日志、指标和告警；
- 已完成的单元、集成、契约和并发测试。

## 16. 合并前检查清单

- [ ] Business Module Root 固定为 `Dora-Agent/business`，具有独立的 `business/go.mod`、`business/go.sum`，并能在 `GOWORK=off` 下独立构建。
- [ ] Business 生产 Runtime 入口仅位于 `business/cmd/business-service`。
- [ ] Business Migration 仅位于 `business/migrations`，且只维护 Business Module 拥有的数据库对象。
- [ ] 本地命令使用 `/Users/figo/sdk/go1.26.3/bin/go`。
- [ ] PostgreSQL 本地基线为 `postgres:16.4-alpine`。
- [ ] Runtime 数据访问统一使用 GORM。
- [ ] 未使用服务启动 AutoMigrate。
- [ ] Migration 和实际 Schema 未创建任何数据库物理外键约束。
- [ ] 业务需要的逻辑外键字段已在表、Entity、GORM Model 或 DTO 中正确保留，并具有中文 COMMENT、必要索引和业务完整性校验。
- [ ] 所有新表和新字段都有中文数据库 COMMENT。
- [ ] 实体、持久化 Model、DTO、业务类型和业务方法具有中文注释。
- [ ] 业务判断和流程分支用中文说明了业务原因与一致性影响。
- [ ] 单次请求、UseCase 和事务内没有循环执行同一或同构 SQL，没有 N+1。
- [ ] 复杂跨表查询使用 JOIN/CTE/集合查询或固定数量批量查询，并映射为具有中文注释的专用 DTO。
- [ ] 前端、RPC、事件及跨领域参数均使用 DTO。
- [ ] 未直接暴露 Entity 或 GORM Model。
- [ ] GORM 调用均传播 Context。
- [ ] 写操作检查 Error，关键更新检查 RowsAffected。
- [ ] 金额未使用浮点数，数据库固定两位小数。
- [ ] 当前实现没有无业务意义的 Tenant 字段。
- [ ] 用户类型能够区分普通用户和企业用户。
- [ ] etcd 只用于服务注册发现。
- [ ] 异步业务变更和 Outbox 在同一事务。
- [ ] Redis 不作为业务最终事实来源。
- [ ] 日志、DTO 和 Event 不包含 Secret 或敏感字段。
- [ ] Migration、测试、Lint、Race、Vulnerability 和构建门禁通过。
- [ ] Commit 和 PR 描述符合本规范。
