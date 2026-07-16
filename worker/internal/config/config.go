// Package config 负责加载并校验 Business Worker 启动配置。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const serviceName = "dora-business-worker"

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
