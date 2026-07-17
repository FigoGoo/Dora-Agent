// Package config 负责加载并校验 Business Worker 启动配置。
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const serviceName = "dora-business-worker"

const (
	// MediaRuntimeProfileV3Preview1 是 Worker 唯一允许启用的本地媒体 Preview Profile。
	MediaRuntimeProfileV3Preview1 = "media.runtime.v3preview1"
	// defaultMediaMaxResponseBytes 是 Business 严格 JSON 响应的默认读取上限。
	defaultMediaMaxResponseBytes = 64 * 1024
	// defaultMediaMaxPNGBytes 是固定 640x360 PNG 的默认字节预算。
	defaultMediaMaxPNGBytes = 2 * 1024 * 1024
	// defaultMediaMaxMP4Bytes 是固定 2 秒 MP4 的默认字节预算。
	defaultMediaMaxMP4Bytes = 16 * 1024 * 1024
)

// Config 是 Business Worker 启动后不可变的完整配置。
type Config struct {
	// Service 保存 Worker 身份和运行环境。
	Service ServiceConfig
	// HTTP 保存健康检查 Server 配置。
	HTTP HTTPConfig
	// PostgreSQL 保存 Worker 独立数据库连接配置。
	PostgreSQL PostgreSQLConfig
	// Redis 保存非权威唤醒连接配置。
	Redis RedisConfig
	// Etcd 保存服务发现依赖配置。
	Etcd EtcdConfig
	// Worker 保存执行器并发、租约和尝试边界。
	Worker WorkerConfig
	// MediaRuntime 保存 local-only 媒体 Job Consumer、Business Client 和产物预算。
	MediaRuntime MediaRuntimeConfig
	// ShutdownTimeout 是进程收到退出信号后的最大收尾时间。
	ShutdownTimeout time.Duration
}

// ServiceConfig 描述 Worker 的稳定服务身份。
type ServiceConfig struct {
	// Name 是日志使用的稳定服务名。
	Name string
	// Version 是构建时注入的服务版本。
	Version string
	// Environment 是 local、test、staging 或 production 等运行环境。
	Environment string
	// InstanceID 是本次 Worker 进程实例标识。
	InstanceID string
}

// HTTPConfig 描述 Worker 健康检查 Server 的监听和资源边界。
type HTTPConfig struct {
	// Address 是健康检查 Server 监听地址。
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

// PostgreSQLConfig 描述 Worker 独立 PostgreSQL 连接和连接池。
type PostgreSQLConfig struct {
	// DSN 是 Worker 独立数据库连接串。
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

// RedisConfig 描述 Worker 非权威唤醒连接。
type RedisConfig struct {
	// Address 是 Redis 地址。
	Address string
	// Password 是 Redis 凭据，不得进入日志。
	Password string
	// DB 是 Worker 使用的 Redis 逻辑库编号。
	DB int
	// PingTimeout 是启动探针的 Redis 超时。
	PingTimeout time.Duration
}

// EtcdConfig 描述 Worker 服务发现依赖探针。
type EtcdConfig struct {
	// Endpoints 是 etcd 节点地址集合。
	Endpoints []string
	// DialTimeout 是建立 etcd 连接的最大时间。
	DialTimeout time.Duration
}

// WorkerConfig 定义后续持久化 Job 执行器的并发、租约和超时边界。
type WorkerConfig struct {
	// Concurrency 是单实例最大并行尝试数。
	Concurrency int
	// ClaimBatchSize 是单次允许领取的最大任务数。
	ClaimBatchSize int
	// PollInterval 是 PostgreSQL 恢复扫描间隔。
	PollInterval time.Duration
	// LeaseTTL 是任务所有权租约时长。
	LeaseTTL time.Duration
	// HeartbeatInterval 是持有任务后的续租周期。
	HeartbeatInterval time.Duration
	// AttemptTimeout 是单次执行尝试的总超时。
	AttemptTimeout time.Duration
	// MaxAttempts 是进入人工处置前的最大尝试次数。
	MaxAttempts int
}

// MediaRuntimeConfig 定义 media.runtime.v3preview1 的本地依赖和硬预算。
//
// Profile 为空时整个切片关闭且 Bootstrap 不建立任何额外连接；未知非空值一律失败关闭。
type MediaRuntimeConfig struct {
	// Profile 必须为空或精确等于 media.runtime.v3preview1。
	Profile string
	// ObjectRoot 是与 Business 共享的绝对、非符号链接、0700 本地对象根。
	ObjectRoot string
	// AgentConsumerDSN 是只具备 Agent Preview View/Function 权限的 loopback PostgreSQL URL。
	AgentConsumerDSN string
	// AgentMaxOpenConns 是 Agent Consumer 连接池最大连接数。
	AgentMaxOpenConns int
	// AgentMaxIdleConns 是 Agent Consumer 连接池最大空闲连接数。
	AgentMaxIdleConns int
	// AgentConnMaxLifetime 是 Agent Consumer 单连接最大复用时间。
	AgentConnMaxLifetime time.Duration
	// AgentConnMaxIdleTime 是 Agent Consumer 空闲连接最大保留时间。
	AgentConnMaxIdleTime time.Duration
	// AgentPingTimeout 是 Agent Consumer 启动 Ping 和契约探针超时。
	AgentPingTimeout time.Duration
	// BusinessBaseURL 是仅允许 loopback HTTP 的 Business 内部媒体端点根 URL。
	BusinessBaseURL string
	// FFMPEGPath 是非符号链接绝对 ffmpeg 可执行文件路径。
	FFMPEGPath string
	// FFprobePath 是非符号链接绝对 ffprobe 可执行文件路径。
	FFprobePath string
	// AgentCallTimeout 是单次 Agent PostgreSQL 函数调用上限。
	AgentCallTimeout time.Duration
	// BusinessCallTimeout 是单次 Business 内部 HTTP 调用上限。
	BusinessCallTimeout time.Duration
	// MaxResponseBytes 是 Business JSON 响应最大字节数。
	MaxResponseBytes int64
	// MaxPNGBytes 是允许提交 Finalize 的 PNG 最大字节数。
	MaxPNGBytes int64
	// MaxMP4Bytes 是允许提交 Finalize 的 MP4 最大字节数。
	MaxMP4Bytes int64
	// StderrLimitBytes 是 ffmpeg/ffprobe 单次诊断保留上限。
	StderrLimitBytes int64
	// RetryBaseDelay 是 retry_wait Full Jitter 的初始上限。
	RetryBaseDelay time.Duration
	// RetryMaxDelay 是 retry_wait Full Jitter 的最大上限。
	RetryMaxDelay time.Duration
}

// Enabled 报告 Worker 是否显式启用完整媒体 Job Runtime。
func (c MediaRuntimeConfig) Enabled() bool {
	return c.Profile == MediaRuntimeProfileV3Preview1
}

// Load 从环境变量加载 Business Worker 配置并执行完整校验。
func Load(version string) (Config, error) {
	cfg := Config{
		Service: ServiceConfig{
			Name: serviceName, Version: strings.TrimSpace(version), Environment: envOrDefault("DORA_ENV", "local"),
			InstanceID: strings.TrimSpace(os.Getenv("WORKER_INSTANCE_ID")),
		},
		HTTP: HTTPConfig{
			Address:        envOrDefault("WORKER_HTTP_ADDR", ":18083"),
			HeaderTimeout:  mustDuration("WORKER_HTTP_HEADER_TIMEOUT", "5s"),
			ReadTimeout:    mustDuration("WORKER_HTTP_READ_TIMEOUT", "10s"),
			WriteTimeout:   mustDuration("WORKER_HTTP_WRITE_TIMEOUT", "10s"),
			IdleTimeout:    mustDuration("WORKER_HTTP_IDLE_TIMEOUT", "60s"),
			MaxHeaderBytes: mustPositiveInt("WORKER_HTTP_MAX_HEADER_BYTES", 1<<20),
		},
		PostgreSQL: PostgreSQLConfig{
			DSN:             strings.TrimSpace(os.Getenv("WORKER_DATABASE_URL")),
			MaxOpenConns:    mustPositiveInt("WORKER_DB_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    mustPositiveInt("WORKER_DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: mustDuration("WORKER_DB_CONN_MAX_LIFETIME", "30m"),
			ConnMaxIdleTime: mustDuration("WORKER_DB_CONN_MAX_IDLE_TIME", "5m"),
			PingTimeout:     mustDuration("WORKER_DB_PING_TIMEOUT", "3s"),
		},
		Redis: RedisConfig{
			Address: strings.TrimSpace(os.Getenv("WORKER_REDIS_ADDR")), Password: os.Getenv("WORKER_REDIS_PASSWORD"),
			DB: mustNonNegativeInt("WORKER_REDIS_DB", 2), PingTimeout: mustDuration("WORKER_REDIS_PING_TIMEOUT", "3s"),
		},
		Etcd: EtcdConfig{
			Endpoints:   splitNonEmpty(os.Getenv("WORKER_ETCD_ENDPOINTS")),
			DialTimeout: mustDuration("WORKER_ETCD_DIAL_TIMEOUT", "5s"),
		},
		Worker: WorkerConfig{
			Concurrency:       mustPositiveInt("WORKER_CONCURRENCY", 8),
			ClaimBatchSize:    mustPositiveInt("WORKER_CLAIM_BATCH_SIZE", 8),
			PollInterval:      mustDuration("WORKER_POLL_INTERVAL", "1s"),
			LeaseTTL:          mustDuration("WORKER_LEASE_TTL", "30s"),
			HeartbeatInterval: mustDuration("WORKER_HEARTBEAT_INTERVAL", "5s"),
			AttemptTimeout:    mustDuration("WORKER_ATTEMPT_TIMEOUT", "2m"),
			MaxAttempts:       mustPositiveInt("WORKER_MAX_ATTEMPTS", 3),
		},
		MediaRuntime: MediaRuntimeConfig{
			Profile:              strings.TrimSpace(os.Getenv("DORA_WORKER_MEDIA_RUNTIME_PROFILE")),
			ObjectRoot:           strings.TrimSpace(os.Getenv("DORA_WORKER_MEDIA_OBJECT_ROOT")),
			AgentConsumerDSN:     strings.TrimSpace(os.Getenv("DORA_WORKER_AGENT_CONSUMER_DSN")),
			AgentMaxOpenConns:    mustPositiveInt("DORA_WORKER_AGENT_DB_MAX_OPEN_CONNS", 4),
			AgentMaxIdleConns:    mustPositiveInt("DORA_WORKER_AGENT_DB_MAX_IDLE_CONNS", 2),
			AgentConnMaxLifetime: mustDuration("DORA_WORKER_AGENT_DB_CONN_MAX_LIFETIME", "10m"),
			AgentConnMaxIdleTime: mustDuration("DORA_WORKER_AGENT_DB_CONN_MAX_IDLE_TIME", "2m"),
			AgentPingTimeout:     mustDuration("DORA_WORKER_AGENT_DB_PING_TIMEOUT", "3s"),
			BusinessBaseURL:      strings.TrimSpace(os.Getenv("DORA_WORKER_BUSINESS_BASE_URL")),
			FFMPEGPath:           strings.TrimSpace(os.Getenv("DORA_WORKER_FFMPEG_PATH")),
			FFprobePath:          strings.TrimSpace(os.Getenv("DORA_WORKER_FFPROBE_PATH")),
			AgentCallTimeout:     mustDuration("DORA_WORKER_MEDIA_AGENT_CALL_TIMEOUT", "2s"),
			BusinessCallTimeout:  mustDuration("DORA_WORKER_MEDIA_BUSINESS_CALL_TIMEOUT", "5s"),
			MaxResponseBytes:     mustPositiveInt64("DORA_WORKER_MEDIA_MAX_RESPONSE_BYTES", defaultMediaMaxResponseBytes),
			MaxPNGBytes:          mustPositiveInt64("DORA_WORKER_MEDIA_MAX_PNG_BYTES", defaultMediaMaxPNGBytes),
			MaxMP4Bytes:          mustPositiveInt64("DORA_WORKER_MEDIA_MAX_MP4_BYTES", defaultMediaMaxMP4Bytes),
			StderrLimitBytes:     mustPositiveInt64("DORA_WORKER_MEDIA_STDERR_LIMIT_BYTES", 16*1024),
			RetryBaseDelay:       mustDuration("DORA_WORKER_MEDIA_RETRY_BASE_DELAY", "1s"),
			RetryMaxDelay:        mustDuration("DORA_WORKER_MEDIA_RETRY_MAX_DELAY", "30s"),
		},
		ShutdownTimeout: mustDuration("WORKER_SHUTDOWN_TIMEOUT", "30s"),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 校验所有必填依赖、有界资源与租约时间关系。
func (c Config) Validate() error {
	if c.Service.Version == "" {
		return fmt.Errorf("business worker version is required")
	}
	if c.Service.InstanceID == "" {
		return fmt.Errorf("WORKER_INSTANCE_ID is required")
	}
	if strings.TrimSpace(c.PostgreSQL.DSN) == "" {
		return fmt.Errorf("WORKER_DATABASE_URL is required")
	}
	if strings.TrimSpace(c.Redis.Address) == "" {
		return fmt.Errorf("WORKER_REDIS_ADDR is required")
	}
	if len(c.Etcd.Endpoints) == 0 {
		return fmt.Errorf("WORKER_ETCD_ENDPOINTS is required")
	}
	if c.HTTP.HeaderTimeout <= 0 || c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 ||
		c.HTTP.IdleTimeout <= 0 || c.HTTP.MaxHeaderBytes <= 0 {
		return fmt.Errorf("worker HTTP limits and timeouts must be positive")
	}
	if c.PostgreSQL.MaxOpenConns <= 0 || c.PostgreSQL.MaxIdleConns <= 0 ||
		c.PostgreSQL.ConnMaxLifetime <= 0 || c.PostgreSQL.ConnMaxIdleTime <= 0 || c.PostgreSQL.PingTimeout <= 0 {
		return fmt.Errorf("worker PostgreSQL pool limits and timeouts must be positive")
	}
	if c.PostgreSQL.MaxIdleConns > c.PostgreSQL.MaxOpenConns {
		return fmt.Errorf("WORKER_DB_MAX_IDLE_CONNS must not exceed WORKER_DB_MAX_OPEN_CONNS")
	}
	if c.Redis.DB < 0 || c.Redis.PingTimeout <= 0 || c.Etcd.DialTimeout <= 0 {
		return fmt.Errorf("worker Redis or etcd configuration is invalid")
	}
	if c.Worker.Concurrency <= 0 || c.Worker.ClaimBatchSize <= 0 || c.Worker.PollInterval <= 0 ||
		c.Worker.LeaseTTL <= 0 || c.Worker.HeartbeatInterval <= 0 || c.Worker.AttemptTimeout <= 0 ||
		c.Worker.MaxAttempts <= 0 {
		return fmt.Errorf("worker execution limits and timeouts must be positive")
	}
	if c.Worker.ClaimBatchSize > c.Worker.Concurrency {
		return fmt.Errorf("WORKER_CLAIM_BATCH_SIZE must not exceed WORKER_CONCURRENCY")
	}
	if c.Worker.HeartbeatInterval > c.Worker.LeaseTTL/3 {
		return fmt.Errorf("WORKER_HEARTBEAT_INTERVAL must not exceed one third of WORKER_LEASE_TTL")
	}
	if c.Worker.AttemptTimeout <= c.Worker.LeaseTTL {
		return fmt.Errorf("WORKER_ATTEMPT_TIMEOUT must exceed WORKER_LEASE_TTL")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("WORKER_SHUTDOWN_TIMEOUT must be positive")
	}
	if err := c.validateMediaRuntime(); err != nil {
		return err
	}
	return nil
}

// validateMediaRuntime 在 Profile 开启时校验 local-only 隔离、loopback 依赖、文件根和全部执行预算。
func (c Config) validateMediaRuntime() error {
	media := c.MediaRuntime
	if media.Profile == "" {
		return nil
	}
	if media.Profile != MediaRuntimeProfileV3Preview1 {
		return fmt.Errorf("DORA_WORKER_MEDIA_RUNTIME_PROFILE is unsupported")
	}
	if c.Service.Environment != "local" {
		return fmt.Errorf("media runtime requires DORA_ENV=local")
	}
	if err := validateLoopbackAddress(c.HTTP.Address); err != nil {
		return fmt.Errorf("WORKER_HTTP_ADDR must be loopback for media runtime: %w", err)
	}
	if err := validateLoopbackPostgresURL(c.PostgreSQL.DSN); err != nil {
		return fmt.Errorf("WORKER_DATABASE_URL must be loopback for media runtime: %w", err)
	}
	if err := validateLoopbackAddress(c.Redis.Address); err != nil {
		return fmt.Errorf("WORKER_REDIS_ADDR must be loopback for media runtime: %w", err)
	}
	for _, endpoint := range c.Etcd.Endpoints {
		if err := validateLoopbackEndpoint(endpoint); err != nil {
			return fmt.Errorf("WORKER_ETCD_ENDPOINTS must be loopback for media runtime: %w", err)
		}
	}
	if err := validateObjectRoot(media.ObjectRoot); err != nil {
		return fmt.Errorf("DORA_WORKER_MEDIA_OBJECT_ROOT is invalid: %w", err)
	}
	if err := validateLoopbackPostgresURL(media.AgentConsumerDSN); err != nil {
		return fmt.Errorf("DORA_WORKER_AGENT_CONSUMER_DSN must be a loopback PostgreSQL URL: %w", err)
	}
	if err := validateLoopbackBaseURL(media.BusinessBaseURL); err != nil {
		return fmt.Errorf("DORA_WORKER_BUSINESS_BASE_URL must be a loopback HTTP URL: %w", err)
	}
	if !filepath.IsAbs(media.FFMPEGPath) || !filepath.IsAbs(media.FFprobePath) {
		return fmt.Errorf("DORA_WORKER_FFMPEG_PATH and DORA_WORKER_FFPROBE_PATH must be absolute")
	}
	if media.AgentMaxOpenConns <= 0 || media.AgentMaxIdleConns <= 0 ||
		media.AgentMaxIdleConns > media.AgentMaxOpenConns || media.AgentConnMaxLifetime <= 0 ||
		media.AgentConnMaxIdleTime <= 0 || media.AgentPingTimeout <= 0 {
		return fmt.Errorf("media Agent Consumer pool limits and timeouts are invalid")
	}
	if media.AgentCallTimeout <= 0 || media.AgentCallTimeout >= c.Worker.HeartbeatInterval ||
		media.BusinessCallTimeout <= 0 || media.BusinessCallTimeout >= c.Worker.AttemptTimeout ||
		media.MaxResponseBytes < 4096 || media.MaxResponseBytes > 1024*1024 ||
		media.MaxPNGBytes <= 0 || media.MaxMP4Bytes < media.MaxPNGBytes ||
		media.StderrLimitBytes < 1024 || media.StderrLimitBytes > 64*1024 ||
		media.RetryBaseDelay <= 0 || media.RetryMaxDelay < media.RetryBaseDelay ||
		media.RetryMaxDelay >= c.Worker.AttemptTimeout {
		return fmt.Errorf("media runtime budgets are invalid")
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

// mustPositiveInt64 读取正整数预算；非法或非正值返回 0 交由统一 Validate 失败关闭。
func mustPositiveInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
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

// validateObjectRoot 校验共享对象根已经存在、是绝对非符号链接目录且权限精确为 0700。
func validateObjectRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("object root must be absolute")
	}
	info, err := os.Lstat(filepath.Clean(root))
	if err != nil {
		return fmt.Errorf("inspect object root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("object root must be a non-symlink 0700 directory")
	}
	return nil
}

// validateLoopbackPostgresURL 只接受带 loopback Host 的 postgres/postgresql URL DSN。
func validateLoopbackPostgresURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		return fmt.Errorf("invalid PostgreSQL URL")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("PostgreSQL host is not loopback")
	}
	return nil
}

// validateLoopbackBaseURL 只接受无凭据、无 Query/Fragment、根 Path 的 loopback HTTP URL。
func validateLoopbackBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid Business base URL")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("Business host is not loopback")
	}
	return nil
}

// validateLoopbackEndpoint 校验可带 http/https scheme 的 etcd 地址仍指向 loopback。
func validateLoopbackEndpoint(raw string) error {
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || !isLoopbackHost(parsed.Hostname()) {
			return fmt.Errorf("endpoint is not loopback")
		}
		return nil
	}
	return validateLoopbackAddress(raw)
}

// validateLoopbackAddress 校验 host:port 地址显式绑定 loopback，拒绝空 Host 的全接口监听。
func validateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" || !isLoopbackHost(host) {
		return fmt.Errorf("address is not explicit loopback host:port")
	}
	return nil
}

// isLoopbackHost 只信任 localhost 字面量或 net.ParseIP 确认的 loopback IP，不做外部 DNS 解析。
func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(host))
	return parsed != nil && parsed.IsLoopback()
}
