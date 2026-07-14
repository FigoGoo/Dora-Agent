// Package config 负责加载并校验 Agent Service 启动配置。
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/skill"
)

const serviceName = "dora-agent-service"

// Config 是 Agent Service 启动后不可变的完整配置。
type Config struct {
	// Service 保存服务身份和运行环境。
	Service ServiceConfig
	// HTTP 保存健康检查和后续业务接口的 HTTP Server 配置。
	HTTP HTTPConfig
	// RPC 保存 Agent Session Kitex Server 的监听、注册地址和资源边界。
	RPC RPCConfig
	// SessionRPCAuth 保存 Business→Agent Session RPC 的服务身份认证参数。
	SessionRPCAuth SessionRPCAuthConfig
	// PostgreSQL 保存 Agent 独立数据库连接和连接池配置。
	PostgreSQL PostgreSQLConfig
	// Redis 保存非权威缓存与临时状态连接配置。
	Redis RedisConfig
	// Etcd 保存服务注册连接和租约配置。
	Etcd EtcdConfig
	// BusinessRPC 保存 Agent 调用 Business Foundation RPC 的有界 Client 配置。
	BusinessRPC BusinessRPCConfig
	// ContentProtection 保存 Session Prompt 加密密钥与非秘密版本引用。
	ContentProtection ContentProtectionConfig
	// SkillSnapshotLimits 保存 Agent 接收 Session Skill Snapshot 的版本化资源剖面。
	SkillSnapshotLimits skill.LimitsProfileV1
	// HTTPIdentity 保存 Business→Agent 用户级 HTTP 身份断言校验参数。
	HTTPIdentity HTTPIdentityConfig
	// Workspace 保存一次一致性工作台快照的集合上限。
	Workspace WorkspaceConfig
	// SSE 保存 EventLog 流式补读、心跳和连接预算。
	SSE SSEConfig
	// ShutdownTimeout 是进程收到退出信号后的最大收尾时间。
	ShutdownTimeout time.Duration
}

// ServiceConfig 描述 Agent Service 的稳定服务身份。
type ServiceConfig struct {
	// Name 是注册发现和日志使用的稳定服务名。
	Name string
	// Version 是构建时注入的服务版本。
	Version string
	// Environment 是 local、test、staging 或 production 等运行环境。
	Environment string
	// InstanceID 是本次进程实例标识。
	InstanceID string
	// AdvertisedAddress 是其他服务可访问的注册地址。
	AdvertisedAddress string
}

// HTTPConfig 描述 Agent HTTP Server 的监听和超时边界。
type HTTPConfig struct {
	// Address 是 HTTP Server 监听地址。
	Address string
	// HeaderTimeout 限制请求头读取时间。
	HeaderTimeout time.Duration
	// ReadTimeout 限制完整请求读取时间。
	ReadTimeout time.Duration
	// WriteTimeout 限制响应写出时间。
	WriteTimeout time.Duration
	// IdleTimeout 限制 Keep-Alive 空闲连接时间。
	IdleTimeout time.Duration
	// MaxHeaderBytes 限制请求头大小。
	MaxHeaderBytes int
}

// RPCConfig 描述 Agent Session RPC Server 的监听、注册地址和超时边界。
type RPCConfig struct {
	// ListenAddress 是 Kitex Server 在本机绑定的地址。
	ListenAddress string
	// AdvertisedAddress 是写入 etcd、供 Business Runtime 访问的 RPC 地址。
	AdvertisedAddress string
	// ReadWriteTimeout 限制单次 RPC 连接读写时间。
	ReadWriteTimeout time.Duration
	// MaxConnectionIdleTime 限制空闲 RPC 连接保留时间。
	MaxConnectionIdleTime time.Duration
}

// SessionRPCAuthConfig 描述 Business→Agent Session RPC 的 HMAC 服务身份认证边界。
// SharedSecret 仅在进程内用于签名校验，禁止进入日志、Trace、RPC DTO 或配置输出。
type SessionRPCAuthConfig struct {
	// SharedSecret 是严格 Base64 解码后的 32 字节服务间共享密钥。
	SharedSecret []byte
	// MaxClockSkew 是签名签发时间允许偏离 Agent 当前时间的最大窗口。
	MaxClockSkew time.Duration
}

// PostgreSQLConfig 描述 Agent PostgreSQL 连接和连接池。
type PostgreSQLConfig struct {
	// DSN 是 Agent 独立数据库连接串。
	DSN string
	// MaxOpenConns 是连接池最大打开连接数。
	MaxOpenConns int
	// MaxIdleConns 是连接池最大空闲连接数。
	MaxIdleConns int
	// ConnMaxLifetime 是单连接最大复用时间。
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime 是空闲连接最大保留时间。
	ConnMaxIdleTime time.Duration
	// PingTimeout 是启动探针的数据库超时。
	PingTimeout time.Duration
}

// RedisConfig 描述 Agent 非权威缓存和临时状态连接。
type RedisConfig struct {
	// Address 是 Redis 地址。
	Address string
	// Password 是 Redis 凭据，不得进入日志。
	Password string
	// DB 是 Agent 使用的 Redis 逻辑库编号。
	DB int
	// PingTimeout 是启动探针的 Redis 超时。
	PingTimeout time.Duration
}

// EtcdConfig 描述 Agent 服务注册连接。
type EtcdConfig struct {
	// Endpoints 是 etcd 节点地址集合。
	Endpoints []string
	// DialTimeout 是建立 etcd 连接的最大时间。
	DialTimeout time.Duration
	// LeaseTTL 是服务注册租约有效期。
	LeaseTTL time.Duration
}

// BusinessRPCConfig 描述 Agent 调用 Business Foundation RPC 的超时和启动重试预算。
type BusinessRPCConfig struct {
	// ConnectTimeout 限制 Kitex 建立单次连接的时间。
	ConnectTimeout time.Duration
	// RequestTimeout 限制单次 Probe RPC 的时间。
	RequestTimeout time.Duration
	// StartupTimeout 是 Agent 等待 Business Foundation RPC 就绪的总预算。
	StartupTimeout time.Duration
	// ProbeInterval 是总预算内两次只读 Probe 之间的间隔。
	ProbeInterval time.Duration
}

// ContentProtectionConfig 保存启动时冻结的 AES-256-GCM 密钥材料和密钥版本。
// Key 仅供进程内加密适配器使用，禁止进入日志、Trace、RPC 或普通配置输出。
type ContentProtectionConfig struct {
	// Key 是从 Base64 环境变量解码得到的 32 字节 AES-256 密钥。
	Key []byte
	// KeyVersion 是持久化在 Message 旁的非秘密密钥版本引用。
	KeyVersion string
	// PreviousKey 是轮换窗口内仅用于历史正文读取的可选 32 字节密钥。
	PreviousKey []byte
	// PreviousKeyVersion 是 PreviousKey 对应的非秘密版本引用。
	PreviousKeyVersion string
}

// HTTPIdentityConfig 描述 Business 签发的短期用户身份断言校验边界。
// Secret 字段只在进程内用于 HMAC 校验，禁止进入日志、Trace、HTTP DTO 或配置输出。
type HTTPIdentityConfig struct {
	// ActiveKeyVersion 是当前接受的身份断言密钥版本。
	ActiveKeyVersion string
	// ActiveSecret 是当前身份断言 HMAC-SHA256 的 32 字节密钥。
	ActiveSecret []byte
	// PreviousKeyVersion 是轮换窗口内同时接受的旧密钥版本。
	PreviousKeyVersion string
	// PreviousSecret 是 PreviousKeyVersion 对应的 32 字节旧密钥。
	PreviousSecret []byte
	// MaxClockSkew 是断言签发时间允许领先本机时钟的最大偏差。
	MaxClockSkew time.Duration
	// ReplayTimeout 是 Redis 一次性 Nonce 占有的最大调用时间。
	ReplayTimeout time.Duration
}

// WorkspaceConfig 描述一次完整 Workspace Snapshot 的有界集合规模。
type WorkspaceConfig struct {
	// MaxMessages 是单次 Snapshot 允许返回的最大 Message 数。
	MaxMessages int
	// MaxInputs 是单次 Snapshot 允许返回的最大 Input 数。
	MaxInputs int
}

// SSEConfig 描述 EventLog SSE 的批量、轮询、写入与并发边界。
type SSEConfig struct {
	// BatchSize 是单次 PostgreSQL 补读最多加载的持久事件数。
	BatchSize int
	// PollInterval 是通知丢失时从 PostgreSQL 恢复的轮询周期。
	PollInterval time.Duration
	// HeartbeatInterval 是 SSE Comment 心跳周期。
	HeartbeatInterval time.Duration
	// MaxConnectionDuration 是单条 SSE 连接的配置侧最长存活时间。
	MaxConnectionDuration time.Duration
	// FrameWriteTimeout 是单帧写入和 Flush 的最长时间。
	FrameWriteTimeout time.Duration
	// MaxEventBytes 是单条持久事件编码后的最大字节数。
	MaxEventBytes int
	// MaxConnections 是单实例并发 SSE 连接总上限。
	MaxConnections int
	// MaxConnectionsPerUser 是单个 Business User 的并发 SSE 上限。
	MaxConnectionsPerUser int
	// MaxConnectionsPerSession 是单个 Agent Session 的并发 SSE 上限。
	MaxConnectionsPerSession int
}

// Load 从环境变量加载 Agent Service 配置并执行完整校验。
func Load(version string) (Config, error) {
	cfg := Config{
		Service: ServiceConfig{
			Name:              serviceName,
			Version:           strings.TrimSpace(version),
			Environment:       envOrDefault("DORA_ENV", "local"),
			InstanceID:        strings.TrimSpace(os.Getenv("AGENT_INSTANCE_ID")),
			AdvertisedAddress: strings.TrimSpace(os.Getenv("AGENT_ADVERTISED_ADDRESS")),
		},
		HTTP: HTTPConfig{
			Address:        envOrDefault("AGENT_HTTP_ADDR", ":18082"),
			HeaderTimeout:  mustDuration("AGENT_HTTP_HEADER_TIMEOUT", "5s"),
			ReadTimeout:    mustDuration("AGENT_HTTP_READ_TIMEOUT", "15s"),
			WriteTimeout:   mustDuration("AGENT_HTTP_WRITE_TIMEOUT", "30s"),
			IdleTimeout:    mustDuration("AGENT_HTTP_IDLE_TIMEOUT", "60s"),
			MaxHeaderBytes: mustPositiveInt("AGENT_HTTP_MAX_HEADER_BYTES", 1<<20),
		},
		RPC: RPCConfig{
			ListenAddress:         envOrDefault("AGENT_RPC_LISTEN_ADDR", ":19082"),
			AdvertisedAddress:     strings.TrimSpace(os.Getenv("AGENT_RPC_ADVERTISED_ADDRESS")),
			ReadWriteTimeout:      mustDuration("AGENT_RPC_READ_WRITE_TIMEOUT", "10s"),
			MaxConnectionIdleTime: mustDuration("AGENT_RPC_MAX_CONN_IDLE_TIME", "5m"),
		},
		SessionRPCAuth: SessionRPCAuthConfig{
			SharedSecret: decodeBase64Secret(os.Getenv("AGENT_SESSION_RPC_AUTH_SECRET_BASE64")),
			MaxClockSkew: mustDuration("AGENT_SESSION_RPC_AUTH_MAX_CLOCK_SKEW", "30s"),
		},
		PostgreSQL: PostgreSQLConfig{
			DSN:             strings.TrimSpace(os.Getenv("AGENT_DATABASE_URL")),
			MaxOpenConns:    mustPositiveInt("AGENT_DB_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    mustPositiveInt("AGENT_DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: mustDuration("AGENT_DB_CONN_MAX_LIFETIME", "30m"),
			ConnMaxIdleTime: mustDuration("AGENT_DB_CONN_MAX_IDLE_TIME", "5m"),
			PingTimeout:     mustDuration("AGENT_DB_PING_TIMEOUT", "3s"),
		},
		Redis: RedisConfig{
			Address:     strings.TrimSpace(os.Getenv("AGENT_REDIS_ADDR")),
			Password:    os.Getenv("AGENT_REDIS_PASSWORD"),
			DB:          mustNonNegativeInt("AGENT_REDIS_DB", 1),
			PingTimeout: mustDuration("AGENT_REDIS_PING_TIMEOUT", "3s"),
		},
		Etcd: EtcdConfig{
			Endpoints:   splitNonEmpty(os.Getenv("AGENT_ETCD_ENDPOINTS")),
			DialTimeout: mustDuration("AGENT_ETCD_DIAL_TIMEOUT", "5s"),
			LeaseTTL:    mustDuration("AGENT_ETCD_LEASE_TTL", "15s"),
		},
		BusinessRPC: BusinessRPCConfig{
			ConnectTimeout: mustDuration("AGENT_BUSINESS_RPC_CONNECT_TIMEOUT", "2s"),
			RequestTimeout: mustDuration("AGENT_BUSINESS_RPC_REQUEST_TIMEOUT", "3s"),
			StartupTimeout: mustDuration("AGENT_BUSINESS_RPC_STARTUP_TIMEOUT", "15s"),
			ProbeInterval:  mustDuration("AGENT_BUSINESS_RPC_PROBE_INTERVAL", "250ms"),
		},
		ContentProtection: ContentProtectionConfig{
			Key:                decodeBase64Secret(os.Getenv("AGENT_CONTENT_KEY_BASE64")),
			KeyVersion:         strings.TrimSpace(os.Getenv("AGENT_CONTENT_KEY_VERSION")),
			PreviousKey:        decodeOptionalBase64Secret(os.Getenv("AGENT_CONTENT_PREVIOUS_KEY_BASE64")),
			PreviousKeyVersion: strings.TrimSpace(os.Getenv("AGENT_CONTENT_PREVIOUS_KEY_VERSION")),
		},
		SkillSnapshotLimits: skill.LimitsProfileV1{
			ProfileVersion:                 envOrDefault("AGENT_SKILL_SNAPSHOT_LIMITS_PROFILE_VERSION", "session_skill_snapshot_limits.v1"),
			MaxItems:                       mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_ITEMS", 16),
			MaxRuntimeContentBytesPerItem:  mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_RUNTIME_CONTENT_BYTES_PER_ITEM", 64*1024),
			MaxTotalRuntimeContentBytes:    mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_TOTAL_RUNTIME_CONTENT_BYTES", 256*1024),
			MaxSnapshotMetadataBytes:       mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_METADATA_BYTES", 128*1024),
			MaxExamplesPerItem:             mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_EXAMPLES_PER_ITEM", 16),
			MaxStarterPromptsPerItem:       mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_STARTER_PROMPTS_PER_ITEM", 16),
			MaxAllowedGraphToolKeysPerItem: mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_ALLOWED_GRAPH_TOOL_KEYS_PER_ITEM", 6),
			MaxPublicToolRefsPerItem:       mustNonNegativeInt("AGENT_SKILL_SNAPSHOT_MAX_PUBLIC_TOOL_REFS_PER_ITEM", 0),
			MaxRPCRequestBytes:             mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_RPC_REQUEST_BYTES", 2*1024*1024),
			MaxOutboxPlaintextBytes:        mustPositiveInt("AGENT_SKILL_SNAPSHOT_MAX_OUTBOX_PLAINTEXT_BYTES", 2*1024*1024),
		},
		HTTPIdentity: HTTPIdentityConfig{
			ActiveKeyVersion:   strings.TrimSpace(os.Getenv("AGENT_HTTP_ASSERTION_ACTIVE_KEY_VERSION")),
			ActiveSecret:       decodeBase64Secret(os.Getenv("AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64")),
			PreviousKeyVersion: strings.TrimSpace(os.Getenv("AGENT_HTTP_ASSERTION_PREVIOUS_KEY_VERSION")),
			PreviousSecret:     decodeOptionalBase64Secret(os.Getenv("AGENT_HTTP_ASSERTION_PREVIOUS_SECRET_BASE64")),
			MaxClockSkew:       mustDuration("AGENT_HTTP_ASSERTION_MAX_CLOCK_SKEW", "5s"),
			ReplayTimeout:      mustDuration("AGENT_HTTP_ASSERTION_REPLAY_TIMEOUT", "500ms"),
		},
		Workspace: WorkspaceConfig{
			MaxMessages: mustPositiveInt("AGENT_WORKSPACE_MAX_MESSAGES", 100),
			MaxInputs:   mustPositiveInt("AGENT_WORKSPACE_MAX_INPUTS", 100),
		},
		SSE: SSEConfig{
			BatchSize:                mustPositiveInt("AGENT_SSE_BATCH_SIZE", 100),
			PollInterval:             mustDuration("AGENT_SSE_POLL_INTERVAL", "1s"),
			HeartbeatInterval:        mustDuration("AGENT_SSE_HEARTBEAT_INTERVAL", "10s"),
			MaxConnectionDuration:    mustDuration("AGENT_SSE_MAX_CONNECTION_DURATION", "25s"),
			FrameWriteTimeout:        mustDuration("AGENT_SSE_FRAME_WRITE_TIMEOUT", "5s"),
			MaxEventBytes:            mustPositiveInt("AGENT_SSE_MAX_EVENT_BYTES", 64<<10),
			MaxConnections:           mustPositiveInt("AGENT_SSE_MAX_CONNECTIONS", 1000),
			MaxConnectionsPerUser:    mustPositiveInt("AGENT_SSE_MAX_CONNECTIONS_PER_USER", 5),
			MaxConnectionsPerSession: mustPositiveInt("AGENT_SSE_MAX_CONNECTIONS_PER_SESSION", 2),
		},
		ShutdownTimeout: mustDuration("AGENT_SHUTDOWN_TIMEOUT", "20s"),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 校验必填连接、服务注册地址和所有有界资源参数。
func (c Config) Validate() error {
	if c.Service.Version == "" {
		return fmt.Errorf("agent service version is required")
	}
	if c.Service.InstanceID == "" {
		return fmt.Errorf("AGENT_INSTANCE_ID is required")
	}
	if err := validateAdvertisedAddress(c.Service.AdvertisedAddress); err != nil {
		return fmt.Errorf("AGENT_ADVERTISED_ADDRESS: %w", err)
	}
	if err := validateListenAddress(c.RPC.ListenAddress); err != nil {
		return fmt.Errorf("AGENT_RPC_LISTEN_ADDR: %w", err)
	}
	if err := validateAdvertisedAddress(c.RPC.AdvertisedAddress); err != nil {
		return fmt.Errorf("AGENT_RPC_ADVERTISED_ADDRESS: %w", err)
	}
	if strings.TrimSpace(c.PostgreSQL.DSN) == "" {
		return fmt.Errorf("AGENT_DATABASE_URL is required")
	}
	if strings.TrimSpace(c.Redis.Address) == "" {
		return fmt.Errorf("AGENT_REDIS_ADDR is required")
	}
	if len(c.Etcd.Endpoints) == 0 {
		return fmt.Errorf("AGENT_ETCD_ENDPOINTS is required")
	}
	if c.HTTP.HeaderTimeout <= 0 || c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 ||
		c.HTTP.IdleTimeout <= 0 || c.HTTP.MaxHeaderBytes <= 0 {
		return fmt.Errorf("agent HTTP limits and timeouts must be positive")
	}
	if c.RPC.ReadWriteTimeout <= 0 || c.RPC.MaxConnectionIdleTime <= 0 {
		return fmt.Errorf("agent RPC timeouts must be positive")
	}
	if len(c.SessionRPCAuth.SharedSecret) != 32 {
		// 不回显原始环境变量或解码错误，避免服务认证 Secret 进入启动日志。
		return fmt.Errorf("AGENT_SESSION_RPC_AUTH_SECRET_BASE64 must decode to exactly 32 bytes")
	}
	if c.SessionRPCAuth.MaxClockSkew <= 0 || c.SessionRPCAuth.MaxClockSkew > 5*time.Minute {
		return fmt.Errorf("AGENT_SESSION_RPC_AUTH_MAX_CLOCK_SKEW must be between 1ns and 5m")
	}
	if c.PostgreSQL.MaxOpenConns <= 0 || c.PostgreSQL.MaxIdleConns <= 0 ||
		c.PostgreSQL.ConnMaxLifetime <= 0 || c.PostgreSQL.ConnMaxIdleTime <= 0 ||
		c.PostgreSQL.PingTimeout <= 0 {
		return fmt.Errorf("agent PostgreSQL pool limits and timeouts must be positive")
	}
	if c.PostgreSQL.MaxIdleConns > c.PostgreSQL.MaxOpenConns {
		return fmt.Errorf("AGENT_DB_MAX_IDLE_CONNS must not exceed AGENT_DB_MAX_OPEN_CONNS")
	}
	if c.Redis.DB < 0 || c.Redis.PingTimeout <= 0 {
		return fmt.Errorf("agent Redis DB and timeout are invalid")
	}
	if c.Etcd.DialTimeout <= 0 {
		return fmt.Errorf("AGENT_ETCD_DIAL_TIMEOUT must be positive")
	}
	if c.Etcd.LeaseTTL < 3*time.Second {
		return fmt.Errorf("AGENT_ETCD_LEASE_TTL must be at least 3s")
	}
	if c.BusinessRPC.ConnectTimeout <= 0 || c.BusinessRPC.RequestTimeout <= 0 ||
		c.BusinessRPC.StartupTimeout <= 0 || c.BusinessRPC.ProbeInterval <= 0 {
		return fmt.Errorf("agent Business RPC timeouts must be positive")
	}
	if c.BusinessRPC.RequestTimeout > c.BusinessRPC.StartupTimeout {
		return fmt.Errorf("AGENT_BUSINESS_RPC_REQUEST_TIMEOUT must not exceed AGENT_BUSINESS_RPC_STARTUP_TIMEOUT")
	}
	if c.BusinessRPC.ProbeInterval >= c.BusinessRPC.StartupTimeout {
		return fmt.Errorf("AGENT_BUSINESS_RPC_PROBE_INTERVAL must be less than AGENT_BUSINESS_RPC_STARTUP_TIMEOUT")
	}
	if len(c.ContentProtection.Key) != 32 {
		// 不回显原始环境变量或解码错误，避免 Secret 进入启动日志。
		return fmt.Errorf("AGENT_CONTENT_KEY_BASE64 must decode to exactly 32 bytes")
	}
	if err := c.SkillSnapshotLimits.Validate(); err != nil {
		return fmt.Errorf("agent Skill Snapshot limits are invalid: %w", err)
	}
	if !validKeyVersion(c.ContentProtection.KeyVersion) {
		return fmt.Errorf("AGENT_CONTENT_KEY_VERSION must contain 1 to 64 bytes")
	}
	if (len(c.ContentProtection.PreviousKey) == 0) != (c.ContentProtection.PreviousKeyVersion == "") {
		return fmt.Errorf("AGENT_CONTENT_PREVIOUS_KEY_VERSION and AGENT_CONTENT_PREVIOUS_KEY_BASE64 must be provided together")
	}
	if len(c.ContentProtection.PreviousKey) != 0 {
		if len(c.ContentProtection.PreviousKey) != 32 {
			return fmt.Errorf("AGENT_CONTENT_PREVIOUS_KEY_BASE64 must decode to exactly 32 bytes")
		}
		if !validKeyVersion(c.ContentProtection.PreviousKeyVersion) || c.ContentProtection.PreviousKeyVersion == c.ContentProtection.KeyVersion {
			return fmt.Errorf("AGENT_CONTENT_PREVIOUS_KEY_VERSION must be distinct and contain 1 to 64 bytes")
		}
	}
	if len(c.HTTPIdentity.ActiveSecret) != 32 {
		return fmt.Errorf("AGENT_HTTP_ASSERTION_ACTIVE_SECRET_BASE64 must decode to exactly 32 bytes")
	}
	if !validKeyVersion(c.HTTPIdentity.ActiveKeyVersion) {
		return fmt.Errorf("AGENT_HTTP_ASSERTION_ACTIVE_KEY_VERSION is invalid")
	}
	if (len(c.HTTPIdentity.PreviousSecret) == 0) != (c.HTTPIdentity.PreviousKeyVersion == "") {
		return fmt.Errorf("AGENT_HTTP_ASSERTION_PREVIOUS_KEY_VERSION and AGENT_HTTP_ASSERTION_PREVIOUS_SECRET_BASE64 must be provided together")
	}
	if len(c.HTTPIdentity.PreviousSecret) != 0 {
		if len(c.HTTPIdentity.PreviousSecret) != 32 || !validKeyVersion(c.HTTPIdentity.PreviousKeyVersion) ||
			c.HTTPIdentity.PreviousKeyVersion == c.HTTPIdentity.ActiveKeyVersion {
			return fmt.Errorf("AGENT_HTTP_ASSERTION_PREVIOUS key pair is invalid")
		}
	}
	if c.HTTPIdentity.MaxClockSkew <= 0 || c.HTTPIdentity.MaxClockSkew > 5*time.Second ||
		c.HTTPIdentity.ReplayTimeout <= 0 || c.HTTPIdentity.ReplayTimeout > 5*time.Second {
		return fmt.Errorf("agent HTTP assertion skew or replay timeout is invalid")
	}
	if c.Workspace.MaxMessages <= 0 || c.Workspace.MaxMessages > 100 ||
		c.Workspace.MaxInputs <= 0 || c.Workspace.MaxInputs > 100 {
		return fmt.Errorf("agent Workspace collection limits must be between 1 and 100")
	}
	if c.SSE.BatchSize <= 0 || c.SSE.BatchSize > 1000 || c.SSE.PollInterval <= 0 ||
		c.SSE.PollInterval > 30*time.Second || c.SSE.HeartbeatInterval <= 0 ||
		c.SSE.HeartbeatInterval >= c.HTTP.IdleTimeout || c.SSE.MaxConnectionDuration <= 0 ||
		c.SSE.MaxConnectionDuration > time.Minute || c.SSE.FrameWriteTimeout <= 0 ||
		c.SSE.FrameWriteTimeout >= c.SSE.HeartbeatInterval || c.SSE.MaxEventBytes <= 0 ||
		c.SSE.MaxEventBytes > 1<<20 || c.SSE.MaxConnections <= 0 || c.SSE.MaxConnections > 100000 ||
		c.SSE.MaxConnectionsPerUser <= 0 || c.SSE.MaxConnectionsPerUser > c.SSE.MaxConnections ||
		c.SSE.MaxConnectionsPerSession <= 0 || c.SSE.MaxConnectionsPerSession > c.SSE.MaxConnectionsPerUser {
		return fmt.Errorf("agent SSE resource limits are invalid")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("AGENT_SHUTDOWN_TIMEOUT must be positive")
	}
	return nil
}

// validateListenAddress 校验本机监听地址具有合法端口，允许省略 Host 以绑定本地所有网卡。
func validateListenAddress(address string) error {
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		return fmt.Errorf("must be a valid host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

// validateAdvertisedAddress 拒绝无法被其他实例访问的本机回环和通配注册地址。
func validateAdvertisedAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("must be a valid host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	plainHost := strings.Trim(strings.ToLower(host), "[]")
	if plainHost == "localhost" || plainHost == "0.0.0.0" || plainHost == "::" {
		return fmt.Errorf("must not use localhost or wildcard host")
	}
	if ip := net.ParseIP(plainHost); ip != nil && ip.IsLoopback() {
		return fmt.Errorf("must not use loopback address")
	}
	return nil
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func mustDuration(key string, fallback string) time.Duration {
	parsed, err := time.ParseDuration(envOrDefault(key, fallback))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func mustPositiveInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func mustNonNegativeInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return -1
	}
	return parsed
}

func splitNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// decodeBase64Secret 严格解码敏感配置；非法或空值返回 nil，由统一启动校验给出不含 Secret 的错误。
func decodeBase64Secret(raw string) []byte {
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return decoded
}

// decodeOptionalBase64Secret 对空配置保留 nil，非空配置仍使用严格 Base64 解码。
func decodeOptionalBase64Secret(raw string) []byte {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return decodeBase64Secret(raw)
}

// validKeyVersion 校验密钥版本只有一种 ASCII 规范表示，避免同一 kid 产生多种签名语义。
func validKeyVersion(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, character := range []byte(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			(index > 0 && (character == '.' || character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}
