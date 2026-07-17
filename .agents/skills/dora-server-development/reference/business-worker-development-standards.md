# Business Worker 开发规范

> 状态：已按 2026-07-13 评审意见修订
> 适用范围：`Dora-Agent/worker` 独立 Go Module、`worker/cmd/business-worker` Runtime 和 Worker 持久化契约
> 不适用范围：Business Service、Agent Service

## 1. 规范等级

- **MUST**：强制要求，违反时不得合并或发布。
- **SHOULD**：默认要求；偏离时必须在代码评审中说明原因。
- **MAY**：按业务场景选择。

本文中的“类”对应 Go 中的具名类型、结构体或接口。

## 2. 已确认的技术边界

1. 仓库根目录 `Dora-Agent/` 是多 Module 仓库容器，不是 Business Worker Go Module 或 Runtime。
2. Worker Module 根目录固定为 `Dora-Agent/worker`，使用独立的 `worker/go.mod`、`worker/go.sum`、Runtime、配置、构建产物和发布版本。
3. Worker 生产 Runtime 入口固定在 `worker/cmd/business-worker`；Business Service 位于 `Dora-Agent/business`，Agent Service 位于 `Dora-Agent/agent`，三者分别使用独立 Go Module 和独立 Runtime。
4. Worker 不得引用 Business Service、Agent Service 或仓库根其他 Module 的 `internal` 包。
5. Runtime 数据访问统一使用 GORM。
6. PostgreSQL Job/Outbox 是任务权威状态；Redis 只用于低延迟唤醒。
7. Worker 执行语义是 At-Least-Once + 幂等，禁止宣称 Exactly-Once。
8. PostgreSQL 使用 16，本地镜像为 `postgres:16.4-alpine`，与 `deploy/local/compose.yaml` 一致。
9. 本地使用 Docker 中的 PostgreSQL、Redis 和 etcd。
10. 本地 Go SDK 固定为 `/Users/figo/sdk/go1.26.3`。
11. 严禁在数据库层面创建物理外键约束；Job、Attempt、Outbox、Entity、GORM Model 和 DTO 应保留业务需要的逻辑外键字段。
12. 数据库表、字段必须具有中文数据库 COMMENT。
13. 实体、业务类型、Job Handler 和业务方法必须具有中文注释。
14. Job Payload、RPC 和跨领域参数必须使用 DTO。
15. 当前系统没有 Tenant；Worker 不得自行引入 `tenant_id`。
16. 单个 Claim Batch 或 Attempt 内严禁循环执行同一或同构 SQL；批量读取使用 JOIN/CTE/集合查询并映射为专用 DTO，或重构为固定数量查询。

## 3. 技术选型和本地基线

| 能力 | 选型 | 要求 |
| --- | --- | --- |
| Go | Go 1.26.3 | 本地使用指定绝对路径 |
| 数据访问 | GORM + PostgreSQL Driver | 所有访问经 Repository |
| 数据库 | PostgreSQL 16 | `postgres:16.4-alpine` |
| 唤醒 | Redis | 不存储任务权威状态 |
| 服务发现 | etcd | 作为 Business RPC Client 时使用 |
| RPC | Kitex + Thrift | 跨 Module 使用 DTO 契约 |
| 日志 | log/slog JSON | 任务上下文结构化输出 |
| 可观测性 | OpenTelemetry | Attempt 建立独立 Span |
| 测试 | testing + Testcontainers | 使用真实 PostgreSQL、Redis、etcd |

从仓库根目录执行 Worker Module 命令时：

~~~bash
/Users/figo/sdk/go1.26.3/bin/go -C worker version
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C worker test ./...
~~~

- 禁止依赖 PATH 中版本不明确的 `go`。
- 项目脚本 SHOULD 支持通过 `GO_BIN` 覆盖 Go 路径。
- `worker/go.mod` 必须声明 `go 1.26` 和 `toolchain go1.26.3`，`worker/go.mod`、`worker/go.sum` 必须提交。
- Worker Module 在 `GOWORK=off` 时必须能够独立构建和测试。
- 本地 MAY 使用仓库根 `go.work` 组合 `business`、`worker`、`agent` 联调，但不得改变 Module Root；`worker/go.mod` 禁止本地路径 `replace`。
- Redis、etcd Docker 镜像必须锁定已验证版本，禁止使用 `latest`。
- 本地测试使用独立测试数据库，禁止清空开发数据库。

## 4. 目录规范

~~~text
Dora-Agent/
├── business/                       # Business Service 独立 Go Module
├── worker/                         # Business Worker 独立 Go Module
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── business-worker/
│   │       └── main.go
│   ├── internal/
│   │   ├── bootstrap/
│   │   ├── config/
│   │   ├── job/
│   │   │   ├── entity.go
│   │   │   ├── dto.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   ├── handler.go
│   │   │   ├── state.go
│   │   │   └── errors.go
│   │   ├── outbox/
│   │   ├── executor/
│   │   ├── handler/
│   │   ├── transport/
│   │   │   ├── health/
│   │   │   └── rpc/
│   │   ├── adapter/
│   │   │   ├── postgres/
│   │   │   ├── redis/
│   │   │   ├── rpc/
│   │   │   └── etcd/
│   │   └── platform/
│   │       ├── logging/
│   │       ├── observability/
│   │       ├── clock/
│   │       └── idgen/
│   ├── migrations/                 # 仅保存 Worker 明确拥有对象的 Migration
│   └── tests/
│       ├── integration/
│       └── contract/
├── agent/                          # Agent Service 独立 Go Module
├── docs/
└── go.work                         # 可选，仅用于本地联调
~~~

- `worker/cmd/business-worker` 是 Worker 生产 Runtime 的唯一入口，只负责配置、依赖组装、启动和优雅关闭，不包含任务处理逻辑。
- Job Handler 按业务能力命名，禁止堆入 `common`、`utils`、`helper`。
- GORM Model、领域实体、Job Payload DTO 和 RPC DTO 必须分开。
- Worker 不得复用 Business Service、Agent Service 或仓库根其他 Module 的 Entity、GORM Model、Repository、Service、UseCase 和内部 DTO，也不得通过复制 GORM Model 绕过边界访问未公开的数据表。
- Repository 接口定义在消费它的业务包中。
- 首版使用构造函数手工注入，禁止 Service Locator 和可变全局实例。
- `worker/migrations` 只能维护明确归属 Worker 的私有表或 Schema 对象；Worker Runtime 不得扫描或执行仓库根、`business/migrations` 或 `agent/migrations`。
- 同一数据库表只能有一个 Migration Owner；Owner 是定义表生命周期、写入事务和数据不变量的 Module，不是碰巧 Claim、发布或消费该表的 Runtime。
- Worker 消费 Business 或 Agent 创建的 Job/Outbox，不代表 Worker 获得该表的 Migration 所有权；跨 Module 持久化访问必须具有明确、版本化的消费契约。
- Outbox Owner 是把 Outbox 与领域 Aggregate 原子写入的生产者 Module；禁止两个 Module 同时维护同一张 Outbox 表。

## 5. Go 编码与中文注释规范

### 5.1 强制中文注释

以下非生成代码必须添加中文注释：

- 所有 Job、Attempt、Outbox 等实体和值对象；
- 所有 GORM 持久化 Model；
- 所有 Job Payload、RPC、事件和跨领域 DTO；
- 所有配置类型及字段；
- 所有 Job Service、Executor、Handler 和 Repository 接口；
- 所有导出的类型、函数、方法、变量和常量；
- 领域层和应用层的所有方法，包括未导出方法；
- 基础设施层中包含业务判断的方法。

类/具名类型和方法的中文注释必须满足：

- Job、Attempt、Outbox、DTO、Service、Executor、Handler 等类型说明职责、生命周期和关键业务不变量。
- 每个业务方法，无论导出与否，都说明用途、主要前置条件、成功结果和关键错误语义。
- Claim、Heartbeat、Complete、Retry、Cancel 等状态方法说明允许的源状态、目标状态和 Lease/Fencing 条件。
- 注释不能只写“处理任务”“执行逻辑”等无业务含义的描述。

导出标识符注释必须以标识符名称开头：

~~~go
// Job 表示一个需要由 Worker 异步执行的持久化任务。
type Job struct {
	// ID 是应用生成的 UUIDv7 任务标识。
	ID uuid.UUID

	// LeaseVersion 是当前租约版本，用于阻止过期 Worker 提交执行结果。
	LeaseVersion int64
}

// CompleteJob 仅允许当前租约持有者提交任务成功状态。
func (s *JobService) CompleteJob(ctx context.Context, jobID uuid.UUID, leaseVersion int64) error {
	// 只有租约版本一致时才能提交结果，防止过期 Worker 覆盖新 Worker 的执行结果。
	// 更新行数为零时表示租约已经丢失，本次执行结果不得继续提交。
	return nil
}
~~~

状态流转、幂等判断、租约判断、重试分类、取消竞争、补偿和对账分支必须使用中文注释说明：

1. 为什么需要该判断；
2. 条件满足时执行什么业务流程；
3. 条件不满足时如何处理；
4. 对数据一致性和重复执行的影响。

类和方法的中文注释不能替代方法体中的流程注释。状态流转、幂等、租约、重试和事务逻辑还必须在对应代码处描述业务原因与一致性影响，禁止只把代码逐行翻译成中文。修改业务逻辑必须同步修改注释。

### 5.2 通用编码

- 执行 `gofmt` 和 `goimports`。
- 包名使用简短、小写、单数业务名。
- 缩写保持 `ID`、`URL`、`RPC`、`DTO` 一致。
- `context.Context` 是调用链首参数，不得存入业务结构体。
- 外部 I/O 必须传递 Context 并显式设置 Deadline。
- 错误包装使用 `%w`，判断使用 `errors.Is/As`，禁止比较错误文本。
- 领域错误使用稳定 Error Code，不泄露 GORM、PostgreSQL、Redis 或 Kitex 原始错误。
- 每个 goroutine 必须有 Owner、取消条件和等待回收路径。
- 所有 Worker Pool、Channel、Prefetch 和下游调用必须有界。
- 禁止 fire-and-forget goroutine。

## 6. DTO 规范

- 持久化 Job Payload 必须使用类型化 DTO。
- HTTP、RPC、事件和跨领域调用必须使用 DTO。
- 禁止直接使用领域实体或 GORM Model 作为跨边界参数。
- 跨 Module RPC 以 Kitex/Thrift IDL 为契约来源；Worker 使用生成的公开 Contract 类型并在协议边界转换，禁止导入 `business/internal`、`agent/internal` 或仓库根 `internal` 下的 DTO。
- DTO 到领域实体、领域实体到 GORM Model 必须显式转换。
- 跨表复杂查询使用专用 QueryDTO/ReadDTO，禁止把 JOIN 结果扫描到领域 Entity 或直接返回 GORM Model。
- Job Payload DTO 必须包含 `schema_version`。
- 已存在存量任务后，Payload 字段不得直接改变语义；新增字段必须兼容旧版本，破坏性变化使用新 Schema Version。
- Payload 只保存稳定 ID 和执行必需的不可变快照。
- Payload 禁止保存密码、Token、AK/SK、大文件、二进制和短期签名 URL。
- 金额 DTO 使用两位小数字符串，禁止 JSON 浮点数。
- DTO 字段、类型和转换方法必须具有中文注释。

建议命名：

~~~text
CreateJobDTO
JobResultDTO
CreditSettlementPayloadDTO
AssetProcessPayloadDTO
JobDispatchedEventDTO
~~~

## 7. GORM 规范

- 统一使用 GORM PostgreSQL Driver。
- Repository 之外禁止直接使用 GORM。
- 业务层不得接收或返回 `*gorm.DB`。
- 所有数据库操作必须使用 `db.WithContext(ctx)`。
- 禁止服务启动和共享环境使用 `AutoMigrate`。
- Owner Module 的版本化 SQL Migration 是数据库结构的唯一事实来源；Worker 自有对象仅允许由 `worker/migrations` 维护。
- GORM 初始化设置 `DisableForeignKeyConstraintWhenMigrating: true`。
- 禁止嵌入 `gorm.Model`，ID、时间、状态和版本必须显式定义。
- GORM Model 必须保留业务需要的逻辑外键 ID；MAY 使用 `foreignKey`、`references` Tag 描述查询映射。
- GORM Model 禁止 `constraint` Tag，Association 不得创建数据库物理外键。
- `foreignKey`、`references`、Preload 和 Association 只能用于 Worker 自有表或明确发布给 Worker 的持久化消费契约；逻辑 `user_id`、`asset_id` 不授予 Worker 访问被引用业务表的权限。
- 禁止 `AllowGlobalUpdate` 和无条件 Update/Delete。
- 禁止使用语义不明确的 `Save` 做全字段更新；使用明确字段列表。
- 所有写操作检查 `Error`，条件状态更新必须检查 `RowsAffected == 1`。
- `gorm.ErrRecordNotFound` 转换为稳定领域错误。
- Job Claim、Fencing 条件更新和 `FOR UPDATE SKIP LOCKED` MAY 使用 Repository 内参数化 Raw SQL。
- 动态排序列、表名和字段名只能来自白名单。
- 列表查询必须显式选择必要字段，避免默认加载大型 Payload 或错误堆栈。
- 禁止在一个 Claim Batch 或 Attempt 的结果循环中执行相同或仅参数不同的同构 SQL，禁止 N+1。
- GORM Hook 只允许技术字段处理，业务状态机不得隐藏在 Hook 中。
- 底层 `sql.DB` 显式配置连接池并暴露连接池指标。

事务规则：

- 事务边界由 Job/Outbox Service 决定。
- 使用 `db.WithContext(ctx).Transaction(...)`。
- Claim、状态更新和 Attempt 写入使用短事务。
- 事务内禁止调用 RPC、HTTP、Redis、对象存储和外部 Provider。
- 可重试事务从完整事务入口重试，次数有限，回调内不得有非幂等外部副作用。

查询和批处理规则：

- 单次 Claim Batch、Attempt 或 Handler 调用内，禁止 `for/range` 中逐个执行 `First/Find/Take/Raw/Scan/Association`。
- JOIN、CTE、子查询和聚合查询仅允许覆盖 Worker 自有表或明确发布给 Worker 的持久化 Read Model/消费表，并映射到具有中文注释的专用 QueryDTO/ReadDTO。
- 用户、鉴权、积分、费用、业务资产等 Business Service 权威状态默认通过 Kitex/Thrift 批量 RPC 获取，或通过版本化 Event 同步到 Worker 自有 Read Model；禁止跨 Module 直接 JOIN Business Service 的业务操作表。
- 批量 RPC 和 Read Model 查询次数不得随 Job 数量增长，禁止把数据库 N+1 转移为 RPC N+1。
- 禁止 `SELECT *` 连表查询；显式选择字段和别名，避免同名字段覆盖及大型 Payload、敏感字段被读取。
- 一对多 JOIN 会造成行数膨胀时，允许固定数量的批量查询并在内存中组装 DTO；查询次数不得随 Job 数量增长。
- 参数量过大时使用 PostgreSQL Array/`unnest`、CTE、临时输入表或限制 Claim Batch，禁止循环分批执行同一 SQL。
- 定时 Claim、PostgreSQL 兜底轮询和 Lease Heartbeat 是跨时间的 Worker 生命周期操作，不属于 N+1；每次执行仍必须是有界集合操作，内部不得再按 Job 逐条查询。

## 8. 数据库设计规范

### 8.1 禁止数据库物理外键，保留逻辑外键

以下规则均为 MUST：

- Job、Attempt、Outbox 以及 Worker 访问的所有表严禁创建数据库物理外键约束。
- Migration SQL 禁止定义 `FOREIGN KEY`、`REFERENCES`、`ON DELETE CASCADE` 和 `ON UPDATE CASCADE`。
- `job_id`、`aggregate_id`、`user_id`、`asset_id` 等逻辑外键字段是允许且必要的，必须在表、Entity、GORM Model 和相关 DTO 中按业务需要保留。
- 逻辑外键字段必须明确数据类型、可空语义、中文 COMMENT，并按查询、Claim、清理或巡检需要建立普通索引。
- GORM MAY 使用 `foreignKey`、`references` Tag 描述 Preload/Association 查询映射，但不得依靠它保证数据完整性。
- GORM Model 禁止生成数据库外键或数据库级联的 `constraint` Tag。
- Worker 自有表之间的引用存在性、删除顺序和状态合法性由 Worker 业务代码负责；指向 Business 或 Agent 对象的逻辑 ID 通过公开 RPC、Event 或持久化消费契约校验，禁止调用其他 Module 的内部 Repository。
- 禁止数据库自动级联；业务需要联动取消、失效、删除或清理时，由业务代码通过事务、Outbox 或幂等补偿显式执行。
- 必须提供孤儿数据检测、告警和修复能力。
- CI 静态扫描 Migration DDL，并查询 `pg_constraint` 断言不存在 `contype = 'f'`；该检查不禁止 Go/GORM 中的逻辑外键字段和查询映射 Tag。

禁止数据库物理外键不等于禁止逻辑外键。主键、唯一约束、`NOT NULL`、`CHECK` 和普通索引仍应使用。

### 8.2 中文数据库 COMMENT

- 每张 Worker 表必须具有非空中文 `COMMENT ON TABLE`。
- 每个字段必须具有非空中文 `COMMENT ON COLUMN`。
- Job 状态字段必须说明全部状态值。
- Lease 字段必须说明时间单位、Owner 和 Fencing 语义。
- 逻辑关联 ID 必须说明关联对象以及“不设置数据库物理外键约束”。
- 金额字段必须说明单位、精度和币种。
- 创建表或新增字段时，COMMENT 必须放在同一个 Up Migration。
- GORM `comment` Tag 不能替代数据库 COMMENT。
- CI 在空库 Migration 后检查 `pg_description` 中的中文说明。

### 8.3 类型和通用规则

- 表、字段、索引和约束使用 `snake_case`。
- ID 由应用生成 UUIDv7，数据库类型为 `uuid`。
- 时间点统一使用 `timestamptz`，数据库连接时区为 UTC。
- Lease 和调度判断使用 PostgreSQL 当前时间，避免不同实例本地时钟偏差。
- 积分使用 `bigint`。
- 金额按字段明确选择“`bigint` 最小货币单位”或“`numeric(p,2)`”，同一业务含义不得混用。
- Go 层使用 Money/Decimal 值对象，禁止 `float32` 和 `float64`。
- 当前没有 Tenant，Worker 表不增加 `tenant_id`。
- Job、Attempt、Outbox 使用状态和归档策略，不使用软删除。
- JSONB 只用于 Payload、Result Snapshot 和扩展元数据；状态、重试次数、Lease、调度时间必须独立成列。

### 8.4 Migration

- Worker Runtime 启动时禁止执行 AutoMigrate 或任何 Migration。
- Worker 自有对象的 Migration 必须位于 `worker/migrations`，使用独立命令和 DDL 账号执行；Worker 作为消费者时不得复制 Owner Module 的 Migration。
- 已进入共享环境的 Migration 禁止修改。
- Worker 只能迁移自己明确拥有的表；跨 Module Schema 变更必须在 Owner Module 提交 Migration，并由 Worker 执行消费契约测试。
- 生产遵循 Expand → Backfill → Switch → Contract。
- 大表索引使用独立非事务 Migration。
- 生产禁止 `DROP ... CASCADE`。
- CI 验证空库完整 Up、上一版本升级、无数据库物理外键和中文 COMMENT。

## 9. Job、Attempt 与 Outbox

### 9.1 Job 最小字段

~~~text
id
job_type
schema_version
payload
status
priority
available_at
attempt_count
max_attempts
max_elapsed_at
idempotency_key
lease_owner
lease_version
lease_expires_at
cancel_requested_at
last_error_code
last_error_message
created_at
updated_at
~~~

### 9.2 Attempt 最小字段

~~~text
id
job_id
attempt_no
worker_id
lease_version
status
started_at
finished_at
error_code
error_message
created_at
~~~

### 9.3 Outbox 最小字段

~~~text
id
event_type
schema_version
aggregate_type
aggregate_id
payload
status
available_at
attempt_count
lease_owner
lease_version
lease_expires_at
published_at
created_at
updated_at
~~~

- `job_id`、`aggregate_id` 是逻辑外键字段，不创建数据库物理外键约束。
- Job `idempotency_key` 必须有唯一约束。
- Attempt 使用 `(job_id, attempt_no)` 唯一约束。
- Claim 查询索引至少覆盖状态、可执行时间、优先级和稳定排序字段。
- Lease Recovery 查询索引至少覆盖状态和 Lease 到期时间。
- Attempt 和状态历史采用追加模式，不覆盖历史执行事实。

## 10. 状态机

推荐状态：

~~~text
pending
running
retry_wait
succeeded
dead
cancelled
reconciling
~~~

主要流转：

~~~text
pending     -> running
retry_wait  -> running
running     -> succeeded
running     -> retry_wait
running     -> dead
running     -> reconciling
pending     -> cancelled
retry_wait  -> cancelled
reconciling -> succeeded / retry_wait / dead
~~~

- 所有状态流转必须使用带旧状态的条件更新。
- `succeeded`、`dead`、`cancelled` 是终态，禁止原地重置为 Pending。
- 人工重放必须创建新 Job，并记录原 Job ID、操作人、原因和时间。
- Running Job 收到取消请求时先记录 `cancel_requested_at`，Handler 在安全点检查。
- 已发生外部副作用但结果未知时进入 `reconciling`，禁止直接重试或标记取消。

## 11. Claim、Lease 与 Fencing

- Claim 使用短事务和 `FOR UPDATE SKIP LOCKED` 或等价原子条件更新。
- 每次 Claim 数量必须受配置限制。
- Claim 成功时设置 `lease_owner`、递增 `lease_version`、写入 `lease_expires_at` 并创建 Attempt。
- Claim 事务提交后才能执行外部调用。
- Lease 时间判断使用 PostgreSQL 时间。
- Heartbeat 间隔 SHOULD 不大于 Lease TTL 的三分之一。
- Heartbeat、成功、失败、重试更新必须匹配：

~~~text
job_id + status=running + lease_owner + lease_version
~~~

- 条件更新 `RowsAffected == 0` 表示 Lease 已丢失。
- Lease 丢失后立即取消 Attempt Context，不得提交任务结果或覆盖新 Worker 状态。
- 已发生外部副作用时进入核对流程。
- Attempt Timeout 和 Lease TTL 分别配置，不能用 Lease 代替业务超时。
- Heartbeat 独立于 Handler 主执行流程，不能被长时间 Provider 调用阻塞。

## 12. 幂等、Outbox 与 Redis

### 12.1 幂等

- 每种 JobType 必须书面定义稳定幂等键生成规则。
- 幂等依赖数据库唯一约束或外部系统幂等键，禁止只做“先查后写”。
- 外部系统支持幂等键时，透传稳定 Job ID 或业务幂等键。
- 重复 Job 必须返回已有结果或确认已有执行，不重复产生副作用。
- 请求可能成功但响应丢失时归类为 `unknown_outcome`，先查询或对账。
- Worker 重启、Redis 重复唤醒、Lease 过期接管都不得重复扣积分、扣款或创建资产。

### 12.2 Outbox

- 业务状态和 Outbox 写入处于同一 PostgreSQL 事务。
- Outbox Publisher 使用与 Job 相同的 Claim、Lease 和 Fencing 规则。
- 消息发送成功后，再通过条件更新写入 `published_at` 和终态。
- 发送成功但数据库状态更新失败时允许重复发送，消费端必须幂等。
- Outbox 清理只处理确认发布且超过保留期的数据。
- Redis 不可用不得回滚已经成功提交的业务事务。

### 12.3 Redis 唤醒

- Redis 只发送“可能有任务可执行”的唤醒信号。
- 收到唤醒后仍从 PostgreSQL Claim Job。
- Redis 消息丢失不得导致任务永久滞留；Worker 必须周期性轮询 PostgreSQL。
- 轮询间隔增加随机抖动，避免多实例同时查询。
- Redis 不可用时 Worker 降级为 PostgreSQL 轮询模式。
- Redis Key 使用统一前缀和明确 TTL，禁止保存密码、Token 和完整 Payload。
- 若未来使用具有 ACK 的消息系统，必须先持久化任务状态，再 ACK。

## 13. 重试、Dead 和对账

错误分类：

| 分类 | 处理 |
| --- | --- |
| `retryable` | 写入 `retry_wait` 和下一执行时间 |
| `permanent` | 进入 `dead` |
| `unknown_outcome` | 进入 `reconciling` |
| `cancelled` | 进入取消流程，不计普通失败 |
| `lease_lost` | 立即停止，不提交结果 |
| `internal` | 有限重试，超限进入 `dead` |

- 同时限制最大尝试次数和最大总执行时间。
- 使用指数退避加 Full Jitter。
- 下游返回合法 `Retry-After` 时优先遵守。
- 下一执行时间持久化到 `available_at`；禁止占用 Worker Slot 长时间 Sleep。
- Worker、Kitex、HTTP Client、Provider SDK 只能有一个明确的主要重试责任方。
- 参数、权限、配置缺失等永久错误不得热循环重试。
- Dead Job 保存脱敏 Error Code、最后错误摘要和 Attempt 历史。
- Dead Job 数量和增长率必须告警。
- 人工重放受权限控制，创建新 Job 并记录审计，不重置原 Job。

## 14. Worker Pool、取消与优雅退出

### 14.1 有界并发

- 使用固定大小 Worker Pool 或有界 Semaphore。
- 禁止每个 Job 无限制创建 goroutine。
- 必须配置全局并发、单 JobType 并发、单 Provider 并发、Claim Batch 和数据库连接池。
- 并发上限同时受数据库连接池、RPC 连接、下游限流和内存预算约束。
- 每个 goroutine 都有 Owner、Cancel 和 Wait 路径。
- Panic 只允许在 Attempt 执行边界统一 Recover；记录脱敏堆栈并进入明确错误策略。

### 14.2 取消

- 取消命令只对非终态 Job 做条件更新。
- 记录取消请求时间、原因和操作人。
- Handler 在领取后、外部副作用前和 Heartbeat 时检查取消。
- 下游支持 Cancel 时调用其取消接口；不支持时进入等待或对账。
- 已经完成不可逆副作用时，仍必须记录事实和完成费用结算，不能伪造已取消。
- 取消与成功竞争通过旧状态、版本和 Lease 条件更新串行化。

### 14.3 优雅退出

收到 SIGTERM/SIGINT 后按顺序执行：

1. Readiness 置为 False。
2. 停止 Claim 新 Job 和 Outbox。
3. 停止接收新的 Redis 唤醒。
4. 在 Shutdown Budget 内等待已运行 Attempt。
5. 等待期间继续 Heartbeat。
6. 超时后取消 Attempt Context。
7. 未完成任务不得标记成功。
8. 所有 Attempt 停止后关闭数据库、Redis、etcd 和日志资源。

- Worker 不提供 RPC 服务时不注册为 etcd Service。
- Worker 作为 Business RPC Client 时通过 etcd 发现服务。
- 关闭时不得中断正在提交终态的短事务。

## 15. 配置规范

配置必须类型化、启动后不可变，并在启动阶段完成校验。至少包括：

~~~text
WORKER_INSTANCE_ID
DATABASE_DSN
REDIS_ADDR
ETCD_ENDPOINTS
WORKER_CONCURRENCY
WORKER_CLAIM_BATCH_SIZE
WORKER_POLL_INTERVAL
WORKER_LEASE_TTL
WORKER_HEARTBEAT_INTERVAL
WORKER_ATTEMPT_TIMEOUT
WORKER_MAX_ATTEMPTS
WORKER_SHUTDOWN_TIMEOUT
~~~

- Duration 必须带明确单位。
- 启动时校验 Heartbeat Interval、Lease TTL 和 Attempt Timeout 的关系。
- 配置错误直接阻止启动。
- Secret 不进入仓库、日志、DTO 和 Job Payload。
- 本地、测试和生产使用同一配置结构，只改变配置值。

## 16. 日志、指标和追踪

日志至少包含：

~~~text
service
version
env
instance_id
trace_id
job_id
job_type
attempt_no
lease_version
duration_ms
error_code
~~~

- 日志统一为 slog JSON。
- 禁止记录完整 Job Payload、密码、Token、个人敏感信息和外部凭证。
- 错误只在最终处理边界记录一次。
- Job ID 可进入日志和 Trace，但不得作为 Metric Label。
- 每个 Attempt 建立独立 Span，并通过 Link 关联上游 Trace。
- OTel Collector 不可用不得阻塞任务执行。

指标至少包括：

- 待执行任务数量和最老任务等待时间；
- Claim 数量和耗时；
- 当前运行数和并发上限；
- 成功、失败、重试、Dead、Cancelled 数量；
- Lease Lost 和 Heartbeat Failure；
- Outbox Lag；
- 按有限 JobType 聚合的执行耗时。

## 17. 安全规范

- Worker Runtime 数据库账号使用最小权限，默认只访问 Worker 自有表；确需消费跨 Module 表时，只能访问 Owner 明确发布的 Job、Outbox、Inbox 或只读 Read Model，并遵守版本化持久化契约，禁止访问 Business Service 的普通业务操作表。
- Worker Migration 使用独立 DDL 账号，该账号只能修改 `worker/migrations` 声明且归 Worker 所有的数据库对象。
- RPC、Redis、etcd 和 PostgreSQL 生产环境使用认证和 TLS。
- 日志和错误信息必须脱敏。
- Job Payload 只保存执行所需最小数据。
- 外部回调必须校验签名、时间戳和重放窗口。
- 禁止提交 `.env`、数据库文件、日志、二进制、密钥和本地调试数据。

## 18. 测试与质量门禁

必须覆盖：

- 相同幂等键并发创建；
- 同一 Job 重复唤醒和重复执行；
- 多 Worker 并发 Claim 不重复；
- Lease 过期后新 Worker 接管；
- 旧 Fencing Token 提交失败；
- Heartbeat 失败和 Lease Lost；
- 外部请求成功但响应丢失；
- 任务状态提交后进程崩溃；
- Redis 不可用时 PostgreSQL 轮询恢复；
- 重试退避、最大次数和最大总耗时；
- Running 与 Cancel 竞争；
- 优雅关闭期间停止 Claim 并 Drain；
- Outbox 重复发送；
- 无数据库物理外键条件下的逻辑外键校验和孤儿数据检测；
- Migration 无数据库物理外键且所有表字段有中文 COMMENT。

测试要求：

- Clock、ID Generator 和随机源可注入，禁止测试依赖真实长时间 Sleep。
- Repository 集成测试使用真实 `postgres:16.4-alpine`，禁止使用 SQLite。
- 批量查询测试验证处理 1、10、100 个 Job 时 SQL 次数固定，不随 Job 数量增长。
- 复杂 JOIN/CTE 查询测试覆盖 DTO 映射、空逻辑关联、一对多去重和分页。
- Redis、etcd 集成测试使用 Docker 或 Testcontainers。
- Payload 解析、状态机和幂等键生成增加 Fuzz Test。
- 并发核心代码执行 Race Detector。

从仓库根目录对 Worker Module 执行本地验证时，关闭 Workspace 依赖以证明 Module 可独立构建：

~~~bash
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C worker mod verify
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C worker vet ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C worker test -shuffle=on -count=1 ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C worker test -race ./...
GOWORK=off /Users/figo/sdk/go1.26.3/bin/go -C worker build ./cmd/business-worker
~~~

Worker CI Job 的工作目录必须是 `worker`，或者所有命令显式使用 `go -C worker`；不得在仓库根 Module 上执行后误报 Worker 门禁已经通过。

质量门禁还包括 `gofmt`、`goimports`、`golangci-lint`、`govulncheck`、代码生成无差异、Migration 完整性、外键检查和中文 COMMENT 检查。

## 19. Git 提交与评审规范

Commit 格式：

~~~text
<type>(<scope>): <中文摘要>
~~~

允许 Type：

~~~text
feat fix refactor perf test docs build ci chore revert
~~~

常用 Scope：

~~~text
worker job outbox storage redis rpc migration observability
~~~

示例：

~~~text
feat(job): 增加任务租约和过期接管机制
fix(outbox): 修复发送成功后重复发布问题
test(worker): 增加旧租约提交失败测试
~~~

提交要求：

- 一个 Commit 只处理一个明确变更目的。
- 禁止提交无法构建或无法测试的中间状态。
- IDL、DTO 和生成代码处于同一变更。
- Migration 与依赖代码在同一 PR 完整提供。
- 已发布 Migration 禁止修改。
- `worker/go.mod`、`worker/go.sum` 变更必须说明原因。
- 禁止把全仓格式化与功能修改混合。
- 禁止提交 Secret、本地配置、日志、覆盖率和构建产物。
- 禁止使用 `--no-verify` 绕过质量门禁后直接合并。

PR 必须说明：

- Job 幂等键及重复执行策略；
- Retry、Unknown Outcome 和 Reconcile 策略；
- 状态机、Lease 或 Fencing 是否变化；
- Migration、兼容性和回滚方式；
- 对存量 Payload 和滚动发布的影响；
- 指标、日志和告警变化；
- 故障窗口和已完成测试。

## 20. 发布规范

- Worker 从 `worker/cmd/business-worker` 独立构建，使用独立版本、独立发布和独立回滚。
- 构建产物写入版本、Commit SHA 和构建时间。
- Worker 自有 Schema 变更时，先执行 `worker/migrations` 中的兼容性 Expand Migration，再发布新 Worker；消费其他 Module Schema 时由 Owner Module 先发布兼容变更。
- 新旧 Worker 并存期间必须兼容存量 Payload 和状态。
- 删除字段、状态或旧 Payload 解析前，必须确认没有存量任务依赖。
- 发布、停止和回滚前执行优雅 Drain，禁止直接终止仍在运行的任务。
- 破坏性 Contract Migration 只能在旧版本完全下线和数据核查后执行。

## 21. 合并前检查清单

- [ ] Worker Module Root 固定为 `Dora-Agent/worker`，具有独立的 `worker/go.mod`、`worker/go.sum`，并能在 `GOWORK=off` 下独立构建。
- [ ] Worker 生产 Runtime 入口仅位于 `worker/cmd/business-worker`。
- [ ] Worker 自有 Migration 仅位于 `worker/migrations`，没有复制或修改其他 Module 拥有的 Migration。
- [ ] Worker 未导入 Business、Agent 或仓库根其他 Module 的 `internal` 包，也未绕过公开契约访问其普通业务表。
- [ ] 本地使用 `/Users/figo/sdk/go1.26.3/bin/go`。
- [ ] PostgreSQL 使用 `postgres:16.4-alpine`。
- [ ] Runtime 数据访问统一使用 GORM，未使用 AutoMigrate。
- [ ] Migration 和实际 Schema 未创建任何数据库物理外键约束。
- [ ] Job、Attempt、Outbox 及其他业务需要的逻辑外键字段已正确保留，并具有中文 COMMENT、必要索引和业务完整性校验。
- [ ] 所有表和字段具有中文数据库 COMMENT。
- [ ] 实体、持久化 Model、DTO、业务类型和方法具有中文注释。
- [ ] 业务判断和流程分支说明了业务原因与一致性影响。
- [ ] 单个 Claim Batch 和 Attempt 内没有循环执行同一或同构 SQL，没有 N+1。
- [ ] 复杂查询使用 JOIN/CTE/集合查询或固定数量批量查询，并映射为具有中文注释的专用 DTO。
- [ ] Job Payload、RPC 和跨领域参数使用 DTO。
- [ ] Job Payload 带 `schema_version`。
- [ ] 当前实现没有 `tenant_id`。
- [ ] Job Handler 定义了幂等键和重复执行策略。
- [ ] Claim 使用 Lease 和 Fencing Token。
- [ ] 状态提交检查 Lease Owner 和 Lease Version。
- [ ] Retry 有分类、次数上限、时间上限和随机退避。
- [ ] Unknown Outcome 具有核对流程。
- [ ] Redis 仅用于唤醒，PostgreSQL 轮询能够兜底。
- [ ] Outbox 与业务状态处于同一事务。
- [ ] Worker Pool、Channel 和下游调用均为有界并发。
- [ ] 优雅退出停止 Claim 并继续 Heartbeat。
- [ ] 测试覆盖重复执行、崩溃、Lease 接管、取消竞争和 Redis 故障。
- [ ] 日志不包含敏感数据或完整 Payload。
- [ ] Commit、PR、Migration 和质量门禁符合本规范。
