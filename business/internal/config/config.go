// Package config 负责加载并校验 Business Service 启动配置。
package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
)

const serviceName = "dora-business-service"

// Config 是 Business Service 启动后不可变的完整配置。
type Config struct {
	// Service 保存服务身份和运行环境。
	Service ServiceConfig
	// HTTP 保存健康检查和后续业务接口的 HTTP Server 配置。
	HTTP HTTPConfig
	// Auth 保存版本化 Web Session、Cookie、CSRF 和请求体安全配置。
	Auth AuthConfig
	// Project 保存 Quick Create、Prompt 保护和 Outbox 重试预算。
	Project ProjectConfig
	// Skill 保存 W1 Skill Builder 请求体资源边界。
	Skill SkillConfig
	// AgentSessionRPC 保存 Business 调用 Agent Session RPC 的有界 Client 配置。
	AgentSessionRPC AgentSessionRPCConfig
	// AgentHTTP 保存同源 BFF 调用 Agent Workspace HTTP 的 Endpoint、超时和断言签名配置。
	AgentHTTP AgentHTTPConfig
	// ProjectDispatch 保存 Session Outbox 后台派发的租约、退避和轮询配置。
	ProjectDispatch ProjectDispatchConfig
	// RPC 保存 Foundation Kitex Server 的监听和资源边界配置。
	RPC RPCConfig
	// PostgreSQL 保存 Business 独立数据库连接和连接池配置。
	PostgreSQL PostgreSQLConfig
	// Redis 保存非权威缓存与唤醒连接配置。
	Redis RedisConfig
	// Etcd 保存服务注册连接和租约配置。
	Etcd EtcdConfig
	// ShutdownTimeout 是进程收到退出信号后的最大收尾时间。
	ShutdownTimeout time.Duration
}

// ServiceConfig 描述 Business Service 的稳定服务身份。
type ServiceConfig struct {
	// Name 是注册发现和日志使用的稳定服务名。
	Name string
	// Version 是构建时注入的服务版本。
	Version string
	// Environment 是 local、test、staging 或 production 等运行环境。
	Environment string
	// InstanceID 是本次进程实例标识，不是业务实体主键。
	InstanceID string
	// AdvertisedAddress 是其他服务可访问的注册地址。
	AdvertisedAddress string
}

// HTTPConfig 描述 Business HTTP Server 的监听和超时边界。
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

// AuthConfig 描述 W0 浏览器认证的会话有效期、Cookie 属性与 CSRF 密钥。
type AuthConfig struct {
	// SessionIdleTTL 会话空闲有效期。
	SessionIdleTTL time.Duration
	// SessionAbsoluteTTL 会话绝对有效期。
	SessionAbsoluteTTL time.Duration
	// CookieName HttpOnly 会话 Cookie 名称。
	CookieName string
	// CookieDomain 会话 Cookie 作用域，空值表示 Host-only Cookie。
	CookieDomain string
	// CookieSecure 是否仅允许 HTTPS 传输 Cookie。
	CookieSecure bool
	// CookieSameSite 是 strict、lax 或 none 之一。
	CookieSameSite string
	// CSRFSecret 是从 Base64 环境变量解码的 HMAC 密钥，不得进入日志。
	CSRFSecret []byte
	// MaxRequestBodyBytes 限制单次 Auth JSON 请求体字节数。
	MaxRequestBodyBytes int64
	// MaxConcurrentSessions 限制单个用户同时有效的浏览器会话数量。
	MaxConcurrentSessions int
	// LoginRateLimitMaxAttempts 是一个窗口内同一规范化邮箱允许的登录尝试次数。
	LoginRateLimitMaxAttempts int
	// LoginRateLimitWindow 是 Redis 登录限流计数窗口。
	LoginRateLimitWindow time.Duration
	// LoginRateLimitTimeout 限制单次 Redis 限流检查或重置时间。
	LoginRateLimitTimeout time.Duration
}

// ProjectConfig 描述 W0 Project Quick Create 的请求边界、重试预算和 Prompt 加密配置。
type ProjectConfig struct {
	// MaxRequestBodyBytes 限制 Quick Create JSON 请求体，需容纳 64 KiB Prompt 与 JSON 开销。
	MaxRequestBodyBytes int64
	// MaxOutboxAttempts 是单个 Agent Session 初始化命令允许开始的最大尝试次数。
	MaxOutboxAttempts int32
	// PromptProtectionKey 是从 Base64 环境变量解码的 32 字节 AES-256 密钥。
	PromptProtectionKey []byte
	// PromptProtectionKeyVersion 是持久化的非秘密密钥版本引用。
	PromptProtectionKeyVersion string
	// PromptProtectionPreviousKey 是轮换窗口内只用于读取在途 Outbox 的可选旧根密钥。
	PromptProtectionPreviousKey []byte
	// PromptProtectionPreviousKeyVersion 是旧根密钥对应的持久化版本引用。
	PromptProtectionPreviousKeyVersion string
	// SkillSnapshotV2Enabled 控制显式 project_quick_create.v2 新流量，默认关闭且不影响 v1。
	SkillSnapshotV2Enabled bool
	// AgentSessionV2CapabilityConfirmed 是部署侧确认所有 Agent 实例均支持 V2 与相同/更大 limits 的门禁。
	AgentSessionV2CapabilityConfirmed bool
	// SkillSnapshotLimitsProfile 固定为 session_skill_snapshot_limits.v1。
	SkillSnapshotLimitsProfile string
	// SkillSnapshotLimits 是 Business Producer 加密和发送前执行的有效资源剖面。
	SkillSnapshotLimits projectskillbinding.LimitsV1
}

// SkillConfig 描述 W1 Skill Builder 结构化定义请求体边界。
type SkillConfig struct {
	// MaxRequestBodyBytes 限制 Create 与 Draft Replace 的完整 JSON 请求体。
	MaxRequestBodyBytes int64
}

// AgentSessionRPCConfig 描述 Business 调用 Agent Session RPC 的连接和请求超时。
type AgentSessionRPCConfig struct {
	// ConnectTimeout 限制 Kitex 建立单次连接的时间。
	ConnectTimeout time.Duration
	// RequestTimeout 限制单次 Ensure 或 Query 的总时间。
	RequestTimeout time.Duration
	// AuthSecret 是 Business 与 Agent 共享、用于逐请求 HMAC 身份证明的 32 字节密钥。
	AuthSecret []byte
}

// AgentHTTPConfig 描述 Business BFF 到 Agent Workspace HTTP 的固定内部连接和身份断言配置。
type AgentHTTPConfig struct {
	// BaseURL 是 Agent HTTP 内部基地址，不得包含用户信息、Query 或业务 Path。
	BaseURL string
	// RequestTimeout 限制普通 Snapshot 总时长以及 SSE 建连响应头等待，不截断已建立事件流。
	RequestTimeout time.Duration
	// AssertionKeyVersion 是当前 Active HMAC 密钥版本，Agent 轮换期同时接受上一版本。
	AssertionKeyVersion string
	// AssertionSecret 是独立于 Cookie、CSRF 与 Session RPC 的 32 字节 HMAC 密钥。
	AssertionSecret []byte
	// AssertionTTL 是每次内部请求的一次性身份断言有效期。
	AssertionTTL time.Duration
}

// ProjectDispatchConfig 描述 Project Session Outbox 单实例派发边界。
type ProjectDispatchConfig struct {
	// LeaseDuration 是一次 Ensure、Unknown Outcome Query 与原命令重试共享的短租约预算。
	LeaseDuration time.Duration
	// RetryDelay 是未确认结果释放后的有限重试间隔。
	RetryDelay time.Duration
	// PollInterval 是无到期命令或单次派发错误后的轮询间隔。
	PollInterval time.Duration
}

// RPCConfig 描述 Business Foundation RPC Server 的监听和超时边界。
type RPCConfig struct {
	// ListenAddress 是 Kitex Server 在本机绑定的地址。
	ListenAddress string
	// AdvertisedAddress 是写入 etcd、供其他 Runtime 访问的 RPC 地址。
	AdvertisedAddress string
	// ReadWriteTimeout 限制单次 RPC 连接读写时间。
	ReadWriteTimeout time.Duration
	// MaxConnectionIdleTime 限制空闲 RPC 连接保留时间。
	MaxConnectionIdleTime time.Duration
}

// PostgreSQLConfig 描述 Business PostgreSQL 连接和连接池。
type PostgreSQLConfig struct {
	// DSN 是 Business 独立数据库连接串，必须由环境安全注入。
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

// RedisConfig 描述 Business Redis 连接。
type RedisConfig struct {
	// Address 是 Redis 地址，Redis 仅用于缓存和唤醒。
	Address string
	// Password 是 Redis 凭据，不得进入日志。
	Password string
	// DB 是 Business 使用的 Redis 逻辑库编号。
	DB int
	// PingTimeout 是启动探针的 Redis 超时。
	PingTimeout time.Duration
}

// EtcdConfig 描述 Business 服务注册连接。
type EtcdConfig struct {
	// Endpoints 是 etcd 节点地址集合。
	Endpoints []string
	// DialTimeout 是建立 etcd 连接的最大时间。
	DialTimeout time.Duration
	// LeaseTTL 是服务注册租约有效期。
	LeaseTTL time.Duration
}

// Load 从环境变量加载 Business Service 配置并执行完整校验。
func Load(version string) (Config, error) {
	cookieSecure, err := strconv.ParseBool(envOrDefault("BUSINESS_AUTH_COOKIE_SECURE", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_AUTH_COOKIE_SECURE must be true or false")
	}
	skillSnapshotV2Enabled, err := strconv.ParseBool(envOrDefault("BUSINESS_PROJECT_SKILL_SNAPSHOT_V2_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_PROJECT_SKILL_SNAPSHOT_V2_ENABLED must be true or false")
	}
	agentSessionV2CapabilityConfirmed, err := strconv.ParseBool(envOrDefault("BUSINESS_AGENT_SESSION_V2_CAPABILITY_CONFIRMED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_AGENT_SESSION_V2_CAPABILITY_CONFIRMED must be true or false")
	}
	csrfSecret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("BUSINESS_AUTH_CSRF_SECRET_BASE64")))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_AUTH_CSRF_SECRET_BASE64 must be valid standard Base64")
	}
	promptProtectionKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("BUSINESS_PROJECT_PROMPT_KEY_BASE64")))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_PROJECT_PROMPT_KEY_BASE64 must be valid standard Base64")
	}
	var promptProtectionPreviousKey []byte
	promptProtectionPreviousKeyRaw := strings.TrimSpace(os.Getenv("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_BASE64"))
	if promptProtectionPreviousKeyRaw != "" {
		promptProtectionPreviousKey, err = base64.StdEncoding.DecodeString(promptProtectionPreviousKeyRaw)
		if err != nil {
			return Config{}, fmt.Errorf("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_BASE64 must be valid standard Base64")
		}
	}
	agentSessionRPCAuthSecret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64")))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64 must be valid standard Base64")
	}
	agentHTTPAssertionSecret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64")))
	if err != nil {
		return Config{}, fmt.Errorf("BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64 must be valid standard Base64")
	}
	cfg := Config{
		Service: ServiceConfig{
			Name:              serviceName,
			Version:           strings.TrimSpace(version),
			Environment:       envOrDefault("DORA_ENV", "local"),
			InstanceID:        strings.TrimSpace(os.Getenv("BUSINESS_INSTANCE_ID")),
			AdvertisedAddress: strings.TrimSpace(os.Getenv("BUSINESS_ADVERTISED_ADDRESS")),
		},
		HTTP: HTTPConfig{
			Address:        envOrDefault("BUSINESS_HTTP_ADDR", ":18081"),
			HeaderTimeout:  mustDuration("BUSINESS_HTTP_HEADER_TIMEOUT", "5s"),
			ReadTimeout:    mustDuration("BUSINESS_HTTP_READ_TIMEOUT", "15s"),
			WriteTimeout:   mustDuration("BUSINESS_HTTP_WRITE_TIMEOUT", "30s"),
			IdleTimeout:    mustDuration("BUSINESS_HTTP_IDLE_TIMEOUT", "60s"),
			MaxHeaderBytes: mustPositiveInt("BUSINESS_HTTP_MAX_HEADER_BYTES", 1<<20),
		},
		Auth: AuthConfig{
			SessionIdleTTL:     mustDuration("BUSINESS_AUTH_SESSION_IDLE_TTL", "30m"),
			SessionAbsoluteTTL: mustDuration("BUSINESS_AUTH_SESSION_ABSOLUTE_TTL", "24h"),
			CookieName:         envOrDefault("BUSINESS_AUTH_COOKIE_NAME", "dora_session"),
			CookieDomain:       strings.TrimSpace(os.Getenv("BUSINESS_AUTH_COOKIE_DOMAIN")),
			CookieSecure:       cookieSecure,
			CookieSameSite:     strings.ToLower(envOrDefault("BUSINESS_AUTH_COOKIE_SAME_SITE", "lax")),
			CSRFSecret:         csrfSecret,
			MaxRequestBodyBytes: int64(mustPositiveInt(
				"BUSINESS_AUTH_MAX_REQUEST_BODY_BYTES", 4096,
			)),
			MaxConcurrentSessions:     mustPositiveInt("BUSINESS_AUTH_MAX_CONCURRENT_SESSIONS", 5),
			LoginRateLimitMaxAttempts: mustPositiveInt("BUSINESS_AUTH_LOGIN_RATE_LIMIT_MAX_ATTEMPTS", 10),
			LoginRateLimitWindow:      mustDuration("BUSINESS_AUTH_LOGIN_RATE_LIMIT_WINDOW", "15m"),
			LoginRateLimitTimeout:     mustDuration("BUSINESS_AUTH_LOGIN_RATE_LIMIT_TIMEOUT", "500ms"),
		},
		Project: ProjectConfig{
			MaxRequestBodyBytes: int64(mustPositiveInt("BUSINESS_PROJECT_MAX_REQUEST_BODY_BYTES", 409600)),
			MaxOutboxAttempts:   int32(mustPositiveInt("BUSINESS_PROJECT_MAX_OUTBOX_ATTEMPTS", 5)),
			PromptProtectionKey: promptProtectionKey,
			PromptProtectionKeyVersion: strings.TrimSpace(
				os.Getenv("BUSINESS_PROJECT_PROMPT_KEY_VERSION"),
			),
			PromptProtectionPreviousKey: promptProtectionPreviousKey,
			PromptProtectionPreviousKeyVersion: strings.TrimSpace(
				os.Getenv("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_VERSION"),
			),
			SkillSnapshotV2Enabled:            skillSnapshotV2Enabled,
			AgentSessionV2CapabilityConfirmed: agentSessionV2CapabilityConfirmed,
			SkillSnapshotLimitsProfile:        envOrDefault("BUSINESS_SKILL_SNAPSHOT_LIMITS_PROFILE_VERSION", "session_skill_snapshot_limits.v1"),
			SkillSnapshotLimits: projectskillbinding.LimitsV1{
				MaxItems:                      mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_ITEMS", 16),
				MaxRuntimeContentBytesPerItem: mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_RUNTIME_CONTENT_BYTES_PER_ITEM", 64*1024),
				MaxTotalRuntimeContentBytes:   mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_TOTAL_RUNTIME_CONTENT_BYTES", 256*1024),
				MaxSnapshotMetadataBytes:      mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_METADATA_BYTES", 128*1024),
				MaxExamplesPerItem:            mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_EXAMPLES_PER_ITEM", 16),
				MaxStarterPromptsPerItem:      mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_STARTER_PROMPTS_PER_ITEM", 16),
				MaxOutboxPlaintextBytes:       mustPositiveInt("BUSINESS_SKILL_SNAPSHOT_MAX_OUTBOX_PLAINTEXT_BYTES", 2*1024*1024),
			},
		},
		Skill: SkillConfig{
			MaxRequestBodyBytes: int64(mustPositiveInt("BUSINESS_SKILL_MAX_REQUEST_BODY_BYTES", 524288)),
		},
		AgentSessionRPC: AgentSessionRPCConfig{
			ConnectTimeout: mustDuration("BUSINESS_AGENT_RPC_CONNECT_TIMEOUT", "2s"),
			RequestTimeout: mustDuration("BUSINESS_AGENT_RPC_REQUEST_TIMEOUT", "3s"),
			AuthSecret:     agentSessionRPCAuthSecret,
		},
		AgentHTTP: AgentHTTPConfig{
			BaseURL:        strings.TrimSpace(os.Getenv("BUSINESS_AGENT_HTTP_BASE_URL")),
			RequestTimeout: mustDuration("BUSINESS_AGENT_HTTP_REQUEST_TIMEOUT", "5s"),
			AssertionKeyVersion: strings.TrimSpace(
				os.Getenv("BUSINESS_AGENT_HTTP_ASSERTION_KEY_VERSION"),
			),
			AssertionSecret: agentHTTPAssertionSecret,
			AssertionTTL:    mustDuration("BUSINESS_AGENT_HTTP_ASSERTION_TTL", "30s"),
		},
		ProjectDispatch: ProjectDispatchConfig{
			LeaseDuration: mustDuration("BUSINESS_PROJECT_DISPATCH_LEASE_DURATION", "15s"),
			RetryDelay:    mustDuration("BUSINESS_PROJECT_DISPATCH_RETRY_DELAY", "1s"),
			PollInterval:  mustDuration("BUSINESS_PROJECT_DISPATCH_POLL_INTERVAL", "250ms"),
		},
		RPC: RPCConfig{
			ListenAddress:         envOrDefault("BUSINESS_RPC_LISTEN_ADDR", ":19081"),
			AdvertisedAddress:     strings.TrimSpace(os.Getenv("BUSINESS_RPC_ADVERTISED_ADDRESS")),
			ReadWriteTimeout:      mustDuration("BUSINESS_RPC_READ_WRITE_TIMEOUT", "10s"),
			MaxConnectionIdleTime: mustDuration("BUSINESS_RPC_MAX_CONN_IDLE_TIME", "5m"),
		},
		PostgreSQL: PostgreSQLConfig{
			DSN:             strings.TrimSpace(os.Getenv("BUSINESS_DATABASE_URL")),
			MaxOpenConns:    mustPositiveInt("BUSINESS_DB_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    mustPositiveInt("BUSINESS_DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: mustDuration("BUSINESS_DB_CONN_MAX_LIFETIME", "30m"),
			ConnMaxIdleTime: mustDuration("BUSINESS_DB_CONN_MAX_IDLE_TIME", "5m"),
			PingTimeout:     mustDuration("BUSINESS_DB_PING_TIMEOUT", "3s"),
		},
		Redis: RedisConfig{
			Address:     strings.TrimSpace(os.Getenv("BUSINESS_REDIS_ADDR")),
			Password:    os.Getenv("BUSINESS_REDIS_PASSWORD"),
			DB:          mustNonNegativeInt("BUSINESS_REDIS_DB", 0),
			PingTimeout: mustDuration("BUSINESS_REDIS_PING_TIMEOUT", "3s"),
		},
		Etcd: EtcdConfig{
			Endpoints:   splitNonEmpty(os.Getenv("BUSINESS_ETCD_ENDPOINTS")),
			DialTimeout: mustDuration("BUSINESS_ETCD_DIAL_TIMEOUT", "5s"),
			LeaseTTL:    mustDuration("BUSINESS_ETCD_LEASE_TTL", "15s"),
		},
		ShutdownTimeout: mustDuration("BUSINESS_SHUTDOWN_TIMEOUT", "20s"),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 校验必填连接、服务注册地址和所有有界资源参数。
func (c Config) Validate() error {
	if c.Service.Version == "" {
		return fmt.Errorf("business service version is required")
	}
	if c.Service.InstanceID == "" {
		return fmt.Errorf("BUSINESS_INSTANCE_ID is required")
	}
	if err := validateAdvertisedAddress(c.Service.AdvertisedAddress); err != nil {
		return fmt.Errorf("BUSINESS_ADVERTISED_ADDRESS: %w", err)
	}
	if err := validateListenAddress(c.RPC.ListenAddress); err != nil {
		return fmt.Errorf("BUSINESS_RPC_LISTEN_ADDR: %w", err)
	}
	if err := validateAdvertisedAddress(c.RPC.AdvertisedAddress); err != nil {
		return fmt.Errorf("BUSINESS_RPC_ADVERTISED_ADDRESS: %w", err)
	}
	if strings.TrimSpace(c.PostgreSQL.DSN) == "" {
		return fmt.Errorf("BUSINESS_DATABASE_URL is required")
	}
	if strings.TrimSpace(c.Redis.Address) == "" {
		return fmt.Errorf("BUSINESS_REDIS_ADDR is required")
	}
	if len(c.Etcd.Endpoints) == 0 {
		return fmt.Errorf("BUSINESS_ETCD_ENDPOINTS is required")
	}
	if c.HTTP.HeaderTimeout <= 0 || c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 ||
		c.HTTP.IdleTimeout <= 0 || c.HTTP.MaxHeaderBytes <= 0 {
		return fmt.Errorf("business HTTP limits and timeouts must be positive")
	}
	if c.Auth.SessionIdleTTL <= 0 || c.Auth.SessionAbsoluteTTL <= 0 || c.Auth.SessionIdleTTL > c.Auth.SessionAbsoluteTTL {
		return fmt.Errorf("business auth session TTLs are invalid")
	}
	if !validCookieName(c.Auth.CookieName) {
		return fmt.Errorf("BUSINESS_AUTH_COOKIE_NAME is invalid")
	}
	if !validCookieDomain(c.Auth.CookieDomain) {
		return fmt.Errorf("BUSINESS_AUTH_COOKIE_DOMAIN is invalid")
	}
	if c.Auth.CookieSameSite != "strict" && c.Auth.CookieSameSite != "lax" && c.Auth.CookieSameSite != "none" {
		return fmt.Errorf("BUSINESS_AUTH_COOKIE_SAME_SITE must be strict, lax, or none")
	}
	if c.Auth.CookieSameSite == "none" && !c.Auth.CookieSecure {
		return fmt.Errorf("BUSINESS_AUTH_COOKIE_SECURE must be true when SameSite is none")
	}
	if strings.EqualFold(c.Service.Environment, "production") && !c.Auth.CookieSecure {
		return fmt.Errorf("BUSINESS_AUTH_COOKIE_SECURE must be true in production")
	}
	if len(c.Auth.CSRFSecret) < 32 || len(c.Auth.CSRFSecret) > 64 {
		return fmt.Errorf("BUSINESS_AUTH_CSRF_SECRET_BASE64 must decode to 32-64 bytes")
	}
	if c.Auth.MaxRequestBodyBytes < 256 || c.Auth.MaxRequestBodyBytes > 65536 {
		return fmt.Errorf("BUSINESS_AUTH_MAX_REQUEST_BODY_BYTES must be between 256 and 65536")
	}
	if c.Auth.MaxConcurrentSessions < 1 || c.Auth.MaxConcurrentSessions > 100 {
		return fmt.Errorf("BUSINESS_AUTH_MAX_CONCURRENT_SESSIONS must be between 1 and 100")
	}
	if c.Auth.LoginRateLimitMaxAttempts < 1 || c.Auth.LoginRateLimitMaxAttempts > 1000 {
		return fmt.Errorf("BUSINESS_AUTH_LOGIN_RATE_LIMIT_MAX_ATTEMPTS must be between 1 and 1000")
	}
	if c.Auth.LoginRateLimitWindow < time.Second || c.Auth.LoginRateLimitWindow > 24*time.Hour {
		return fmt.Errorf("BUSINESS_AUTH_LOGIN_RATE_LIMIT_WINDOW must be between 1s and 24h")
	}
	if c.Auth.LoginRateLimitTimeout <= 0 || c.Auth.LoginRateLimitTimeout > 5*time.Second {
		return fmt.Errorf("BUSINESS_AUTH_LOGIN_RATE_LIMIT_TIMEOUT must be between 1ns and 5s")
	}
	if c.Project.MaxRequestBodyBytes < 1024 || c.Project.MaxRequestBodyBytes > 524288 {
		return fmt.Errorf("BUSINESS_PROJECT_MAX_REQUEST_BODY_BYTES must be between 1024 and 524288")
	}
	if c.Project.MaxOutboxAttempts < 1 || c.Project.MaxOutboxAttempts > 100 {
		return fmt.Errorf("BUSINESS_PROJECT_MAX_OUTBOX_ATTEMPTS must be between 1 and 100")
	}
	if c.Skill.MaxRequestBodyBytes < 4096 || c.Skill.MaxRequestBodyBytes > 1048576 {
		return fmt.Errorf("BUSINESS_SKILL_MAX_REQUEST_BODY_BYTES must be between 4096 and 1048576")
	}
	if len(c.Project.PromptProtectionKey) != 32 {
		return fmt.Errorf("BUSINESS_PROJECT_PROMPT_KEY_BASE64 must decode to exactly 32 bytes")
	}
	if !validKeyVersion(c.Project.PromptProtectionKeyVersion) {
		return fmt.Errorf("BUSINESS_PROJECT_PROMPT_KEY_VERSION is invalid")
	}
	if (len(c.Project.PromptProtectionPreviousKey) == 0) != (c.Project.PromptProtectionPreviousKeyVersion == "") {
		return fmt.Errorf("BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_VERSION and BUSINESS_PROJECT_PROMPT_PREVIOUS_KEY_BASE64 must be provided together")
	}
	if len(c.Project.PromptProtectionPreviousKey) != 0 {
		if len(c.Project.PromptProtectionPreviousKey) != 32 ||
			!validKeyVersion(c.Project.PromptProtectionPreviousKeyVersion) ||
			c.Project.PromptProtectionPreviousKeyVersion == c.Project.PromptProtectionKeyVersion ||
			bytes.Equal(c.Project.PromptProtectionPreviousKey, c.Project.PromptProtectionKey) {
			return fmt.Errorf("BUSINESS_PROJECT_PROMPT_PREVIOUS key pair is invalid")
		}
	}
	if c.Project.SkillSnapshotLimitsProfile != "session_skill_snapshot_limits.v1" {
		return fmt.Errorf("BUSINESS_SKILL_SNAPSHOT_LIMITS_PROFILE_VERSION is unsupported")
	}
	if err := c.Project.SkillSnapshotLimits.Validate(); err != nil {
		return fmt.Errorf("business Skill Snapshot limits are invalid: %w", err)
	}
	if c.Project.SkillSnapshotV2Enabled && !c.Project.AgentSessionV2CapabilityConfirmed {
		return fmt.Errorf("BUSINESS_AGENT_SESSION_V2_CAPABILITY_CONFIRMED must be true when Skill Snapshot v2 is enabled")
	}
	if c.AgentSessionRPC.ConnectTimeout <= 0 || c.AgentSessionRPC.RequestTimeout <= 0 {
		return fmt.Errorf("business Agent Session RPC timeouts must be positive")
	}
	if len(c.AgentSessionRPC.AuthSecret) != 32 {
		return fmt.Errorf("BUSINESS_AGENT_SESSION_RPC_AUTH_SECRET_BASE64 must decode to exactly 32 bytes")
	}
	if err := validateAgentHTTPBaseURL(c.AgentHTTP.BaseURL, c.Service.Environment); err != nil {
		return fmt.Errorf("BUSINESS_AGENT_HTTP_BASE_URL: %w", err)
	}
	if c.AgentHTTP.RequestTimeout <= 0 || c.AgentHTTP.RequestTimeout > 30*time.Second {
		return fmt.Errorf("BUSINESS_AGENT_HTTP_REQUEST_TIMEOUT must be between 1ns and 30s")
	}
	if !validKeyVersion(c.AgentHTTP.AssertionKeyVersion) {
		return fmt.Errorf("BUSINESS_AGENT_HTTP_ASSERTION_KEY_VERSION is invalid")
	}
	if len(c.AgentHTTP.AssertionSecret) != 32 {
		return fmt.Errorf("BUSINESS_AGENT_HTTP_ASSERTION_SECRET_BASE64 must decode to exactly 32 bytes")
	}
	if c.AgentHTTP.AssertionTTL < time.Millisecond || c.AgentHTTP.AssertionTTL > 60*time.Second || c.AgentHTTP.AssertionTTL%time.Millisecond != 0 {
		return fmt.Errorf("BUSINESS_AGENT_HTTP_ASSERTION_TTL must be whole milliseconds between 1ms and 60s")
	}
	if c.ProjectDispatch.LeaseDuration <= 3*c.AgentSessionRPC.RequestTimeout {
		return fmt.Errorf("BUSINESS_PROJECT_DISPATCH_LEASE_DURATION must exceed three Agent RPC request timeouts")
	}
	if c.ProjectDispatch.RetryDelay <= 0 || c.ProjectDispatch.PollInterval <= 0 {
		return fmt.Errorf("business Project dispatch retry and poll intervals must be positive")
	}
	if c.RPC.ReadWriteTimeout <= 0 || c.RPC.MaxConnectionIdleTime <= 0 {
		return fmt.Errorf("business RPC timeouts must be positive")
	}
	if c.PostgreSQL.MaxOpenConns <= 0 || c.PostgreSQL.MaxIdleConns <= 0 ||
		c.PostgreSQL.ConnMaxLifetime <= 0 || c.PostgreSQL.ConnMaxIdleTime <= 0 ||
		c.PostgreSQL.PingTimeout <= 0 {
		return fmt.Errorf("business PostgreSQL pool limits and timeouts must be positive")
	}
	if c.PostgreSQL.MaxIdleConns > c.PostgreSQL.MaxOpenConns {
		return fmt.Errorf("BUSINESS_DB_MAX_IDLE_CONNS must not exceed BUSINESS_DB_MAX_OPEN_CONNS")
	}
	if c.Redis.DB < 0 || c.Redis.PingTimeout <= 0 {
		return fmt.Errorf("business Redis DB and timeout are invalid")
	}
	if c.Etcd.DialTimeout <= 0 {
		return fmt.Errorf("BUSINESS_ETCD_DIAL_TIMEOUT must be positive")
	}
	if c.Etcd.LeaseTTL < 3*time.Second {
		return fmt.Errorf("BUSINESS_ETCD_LEASE_TTL must be at least 3s")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("BUSINESS_SHUTDOWN_TIMEOUT must be positive")
	}
	return nil
}

// validateAgentHTTPBaseURL 校验内部 Endpoint 只包含 Scheme 与 Host，生产环境强制 HTTPS。
func validateAgentHTTPBaseURL(raw string, environment string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if strings.EqualFold(environment, "production") && parsed.Scheme != "https" {
		return fmt.Errorf("must use https in production")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("must not contain user info, query, fragment, or path")
	}
	if parsed.RawPath != "" || strings.TrimSpace(raw) != raw {
		return fmt.Errorf("must use canonical URL encoding")
	}
	return nil
}

// validKeyVersion 与 Agent Verifier 共用唯一语义：首字节为小写字母或数字，后续才允许 ._-。
func validKeyVersion(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	for index, char := range []byte(version) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			(index > 0 && (char == '.' || char == '_' || char == '-')) {
			continue
		}
		return false
	}
	return true
}

// validCookieName 校验 Cookie 名称只包含 RFC Token 安全字符，防止响应头注入。
func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char <= 0x20 || char >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", char) {
			return false
		}
	}
	return true
}

// validCookieDomain 校验可选 Cookie Domain 不含端口、路径、空白或控制字符。
func validCookieDomain(domain string) bool {
	if domain == "" {
		return true
	}
	if strings.ContainsAny(domain, ":/\\") || strings.TrimSpace(domain) != domain {
		return false
	}
	plain := strings.TrimPrefix(domain, ".")
	if plain == "" || strings.HasPrefix(plain, ".") || strings.HasSuffix(plain, ".") {
		return false
	}
	for _, char := range plain {
		if !(char == '-' || char == '.' || char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z') {
			return false
		}
	}
	return true
}

// validateListenAddress 校验本机监听地址具有合法端口，允许省略 Host 以绑定全部本地网卡。
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

// validateAdvertisedAddress 确保注册地址可被其他实例访问，拒绝本机回环和通配地址。
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

// envOrDefault 返回已去除首尾空白的环境变量；空值时使用非敏感默认值。
func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// mustDuration 解析带单位的时长；配置非法时保留零值并由启动校验统一失败。
func mustDuration(key string, fallback string) time.Duration {
	value := envOrDefault(key, fallback)
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

// mustPositiveInt 解析正整数配置；非法值返回零并由启动校验失败。
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

// mustNonNegativeInt 解析非负整数配置；非法值返回负一并由启动校验失败。
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

// splitNonEmpty 按逗号拆分配置并移除空白项。
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
