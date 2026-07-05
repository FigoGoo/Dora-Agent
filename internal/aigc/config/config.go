package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDeepSeekAPIKey  = "DORA_DEEPSEEK_API_KEY"
	EnvDeepSeekModel   = "DORA_DEEPSEEK_MODEL"
	EnvDeepSeekBaseURL = "DORA_DEEPSEEK_BASE_URL"
	EnvImage2APIKey    = "DORA_IMAGE2_API_KEY"
	EnvSeedanceAPIKey  = "DORA_SEEDANCE_API_KEY"
	EnvAgentHTTPAddr   = "AGENT_HTTP_ADDR"

	EnvBusinessDatabaseURL           = "BUSINESS_DATABASE_URL"
	EnvAgentDatabaseURL              = "AGENT_DATABASE_URL"
	EnvAgentGenerationRedisAddr      = "AGENT_GENERATION_REDIS_ADDR"
	EnvAgentGenerationRedisPassword  = "AGENT_GENERATION_REDIS_PASSWORD"
	EnvAgentGenerationRedisDB        = "AGENT_GENERATION_REDIS_DB"
	EnvAgentGenerationRedisListKey   = "AGENT_GENERATION_REDIS_LIST_KEY"
	EnvAgentRuntimeRedisMode         = "AGENT_RUNTIME_REDIS_MODE"
	EnvAgentRuntimeRedisAddr         = "AGENT_RUNTIME_REDIS_ADDR"
	EnvAgentRuntimeRedisPassword     = "AGENT_RUNTIME_REDIS_PASSWORD"
	EnvAgentRuntimeRedisDB           = "AGENT_RUNTIME_REDIS_DB"
	EnvAgentRuntimeRedisStreamMaxLen = "AGENT_RUNTIME_REDIS_STREAM_MAX_LEN"
	EnvTOSEndpoint                   = "TOS_ENDPOINT"
	EnvTOSBucket                     = "TOS_BUCKET"
	EnvTOSAccessKeyID                = "TOS_ACCESS_KEY_ID"
	EnvTOSSecretAccessKey            = "TOS_SECRET_ACCESS_KEY"
	EnvTOSRegion                     = "TOS_REGION"
	EnvTOSBaseURL                    = "TOS_BASE_URL"
	EnvTOSRequestTimeout             = "TOS_REQUEST_TIMEOUT"
	EnvTOSConnectTimeout             = "TOS_CONNECT_TIMEOUT"

	DefaultDeepSeekModel   = "deepseek-chat"
	DefaultDeepSeekBaseURL = "https://api.deepseek.com"
	DefaultAgentHTTPAddr   = ":18080"

	DefaultBusinessDatabaseURL           = "postgres://dora:dora_local_password@127.0.0.1:5432/dora_business?sslmode=disable"
	DefaultAgentDatabaseURL              = "postgres://dora:dora_local_password@127.0.0.1:5432/dora_agent?sslmode=disable"
	DefaultAgentGenerationRedisAddr      = "127.0.0.1:6379"
	DefaultAgentGenerationRedisDB        = 0
	DefaultAgentGenerationRedisListKey   = "dora:agent:generation_jobs"
	DefaultAgentRuntimeRedisMode         = "redis"
	DefaultAgentRuntimeRedisAddr         = "127.0.0.1:6379"
	DefaultAgentRuntimeRedisDB           = 0
	DefaultAgentRuntimeRedisStreamMaxLen = 1000
	DefaultTOSRequestTimeout             = 30 * time.Second
	DefaultTOSConnectTimeout             = 10 * time.Second
)

var (
	ErrMissingDeepSeekAPIKey = errors.New("deepseek api key is required")
)

type Config struct {
	DeepSeek DeepSeekConfig `json:"deepseek"`
	Image2   ProviderConfig `json:"image2"`
	Seedance ProviderConfig `json:"seedance"`
	Storage  StorageConfig  `json:"storage"`
	Runtime  RuntimeConfig  `json:"runtime"`
}

type DeepSeekConfig struct {
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	BaseURL string `json:"base_url"`
}

type ProviderConfig struct {
	APIKey string `json:"api_key"`
}

type StorageConfig struct {
	BusinessDatabaseURL string                `json:"business_database_url"`
	AgentDatabaseURL    string                `json:"agent_database_url"`
	GenerationRedis     GenerationRedisConfig `json:"generation_redis"`
	RuntimeRedis        RuntimeRedisConfig    `json:"runtime_redis"`
	TOS                 TOSConfig             `json:"tos"`
}

type RuntimeConfig struct {
	HTTPAddr string `json:"http_addr"`
}

type RedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password,omitempty"`
	DB       int    `json:"db"`
}

type GenerationRedisConfig struct {
	RedisConfig
	ListKey string `json:"list_key"`
}

type RuntimeRedisConfig struct {
	RedisConfig
	Mode         string `json:"mode"`
	StreamMaxLen int64  `json:"stream_max_len"`
}

type TOSConfig struct {
	Endpoint        string        `json:"endpoint"`
	Bucket          string        `json:"bucket"`
	AccessKeyID     string        `json:"access_key_id"`
	SecretAccessKey string        `json:"secret_access_key"`
	Region          string        `json:"region"`
	BaseURL         string        `json:"base_url"`
	RequestTimeout  time.Duration `json:"request_timeout"`
	ConnectTimeout  time.Duration `json:"connect_timeout"`
}

type SanitizedConfig struct {
	DeepSeek SanitizedDeepSeekConfig `json:"deepseek"`
	Image2   CredentialStatus        `json:"image2"`
	Seedance CredentialStatus        `json:"seedance"`
	Storage  SanitizedStorageConfig  `json:"storage"`
	Runtime  RuntimeConfig           `json:"runtime"`
}

type SanitizedDeepSeekConfig struct {
	APIKey  CredentialStatus `json:"api_key"`
	Model   string           `json:"model"`
	BaseURL string           `json:"base_url"`
}

type CredentialStatus struct {
	Configured bool   `json:"configured"`
	Env        string `json:"env"`
}

type SanitizedStorageConfig struct {
	BusinessDatabaseURL CredentialStatus               `json:"business_database_url"`
	AgentDatabaseURL    CredentialStatus               `json:"agent_database_url"`
	GenerationRedis     SanitizedGenerationRedisConfig `json:"generation_redis"`
	RuntimeRedis        SanitizedRuntimeRedisConfig    `json:"runtime_redis"`
	TOS                 SanitizedTOSConfig             `json:"tos"`
}

type SanitizedRedisConfig struct {
	Addr     string           `json:"addr"`
	Password CredentialStatus `json:"password"`
	DB       int              `json:"db"`
}

type SanitizedGenerationRedisConfig struct {
	SanitizedRedisConfig
	ListKey string `json:"list_key"`
}

type SanitizedRuntimeRedisConfig struct {
	SanitizedRedisConfig
	Mode         string `json:"mode"`
	StreamMaxLen int64  `json:"stream_max_len"`
}

type SanitizedTOSConfig struct {
	Endpoint        string           `json:"endpoint"`
	Bucket          string           `json:"bucket"`
	AccessKeyID     CredentialStatus `json:"access_key_id"`
	SecretAccessKey CredentialStatus `json:"secret_access_key"`
	Region          string           `json:"region"`
	BaseURL         string           `json:"base_url"`
	RequestTimeout  string           `json:"request_timeout"`
	ConnectTimeout  string           `json:"connect_timeout"`
}

func LoadFromEnv() Config {
	return Config{
		DeepSeek: DeepSeekConfig{
			APIKey:  env(EnvDeepSeekAPIKey),
			Model:   envOrDefault(EnvDeepSeekModel, DefaultDeepSeekModel),
			BaseURL: envOrDefault(EnvDeepSeekBaseURL, DefaultDeepSeekBaseURL),
		},
		Image2: ProviderConfig{
			APIKey: env(EnvImage2APIKey),
		},
		Seedance: ProviderConfig{
			APIKey: env(EnvSeedanceAPIKey),
		},
		Storage: StorageConfig{
			BusinessDatabaseURL: envOrDefault(EnvBusinessDatabaseURL, DefaultBusinessDatabaseURL),
			AgentDatabaseURL:    envOrDefault(EnvAgentDatabaseURL, DefaultAgentDatabaseURL),
			GenerationRedis: GenerationRedisConfig{
				RedisConfig: RedisConfig{
					Addr:     envOrDefault(EnvAgentGenerationRedisAddr, DefaultAgentGenerationRedisAddr),
					Password: env(EnvAgentGenerationRedisPassword),
					DB:       intEnvOrDefault(EnvAgentGenerationRedisDB, DefaultAgentGenerationRedisDB),
				},
				ListKey: envOrDefault(EnvAgentGenerationRedisListKey, DefaultAgentGenerationRedisListKey),
			},
			RuntimeRedis: RuntimeRedisConfig{
				RedisConfig: RedisConfig{
					Addr:     envOrDefault(EnvAgentRuntimeRedisAddr, DefaultAgentRuntimeRedisAddr),
					Password: env(EnvAgentRuntimeRedisPassword),
					DB:       intEnvOrDefault(EnvAgentRuntimeRedisDB, DefaultAgentRuntimeRedisDB),
				},
				Mode:         envOrDefault(EnvAgentRuntimeRedisMode, DefaultAgentRuntimeRedisMode),
				StreamMaxLen: int64(intEnvOrDefault(EnvAgentRuntimeRedisStreamMaxLen, DefaultAgentRuntimeRedisStreamMaxLen)),
			},
			TOS: TOSConfig{
				Endpoint:        env(EnvTOSEndpoint),
				Bucket:          env(EnvTOSBucket),
				AccessKeyID:     env(EnvTOSAccessKeyID),
				SecretAccessKey: env(EnvTOSSecretAccessKey),
				Region:          env(EnvTOSRegion),
				BaseURL:         env(EnvTOSBaseURL),
				RequestTimeout:  durationEnvOrDefault(EnvTOSRequestTimeout, DefaultTOSRequestTimeout),
				ConnectTimeout:  durationEnvOrDefault(EnvTOSConnectTimeout, DefaultTOSConnectTimeout),
			},
		},
		Runtime: RuntimeConfig{
			HTTPAddr: envOrDefault(EnvAgentHTTPAddr, DefaultAgentHTTPAddr),
		},
	}
}

func (c Config) Normalize() Config {
	c.DeepSeek.APIKey = strings.TrimSpace(c.DeepSeek.APIKey)
	c.DeepSeek.Model = valueOrDefault(c.DeepSeek.Model, DefaultDeepSeekModel)
	c.DeepSeek.BaseURL = valueOrDefault(c.DeepSeek.BaseURL, DefaultDeepSeekBaseURL)
	c.Image2.APIKey = strings.TrimSpace(c.Image2.APIKey)
	c.Seedance.APIKey = strings.TrimSpace(c.Seedance.APIKey)
	c.Storage.BusinessDatabaseURL = valueOrDefault(c.Storage.BusinessDatabaseURL, DefaultBusinessDatabaseURL)
	c.Storage.AgentDatabaseURL = valueOrDefault(c.Storage.AgentDatabaseURL, DefaultAgentDatabaseURL)
	c.Storage.GenerationRedis.Addr = valueOrDefault(c.Storage.GenerationRedis.Addr, DefaultAgentGenerationRedisAddr)
	c.Storage.GenerationRedis.Password = strings.TrimSpace(c.Storage.GenerationRedis.Password)
	c.Storage.GenerationRedis.ListKey = valueOrDefault(c.Storage.GenerationRedis.ListKey, DefaultAgentGenerationRedisListKey)
	c.Storage.RuntimeRedis.Addr = valueOrDefault(c.Storage.RuntimeRedis.Addr, DefaultAgentRuntimeRedisAddr)
	c.Storage.RuntimeRedis.Password = strings.TrimSpace(c.Storage.RuntimeRedis.Password)
	c.Storage.RuntimeRedis.Mode = strings.ToLower(valueOrDefault(c.Storage.RuntimeRedis.Mode, DefaultAgentRuntimeRedisMode))
	if c.Storage.RuntimeRedis.StreamMaxLen <= 0 {
		c.Storage.RuntimeRedis.StreamMaxLen = DefaultAgentRuntimeRedisStreamMaxLen
	}
	c.Storage.TOS.Endpoint = strings.TrimSpace(c.Storage.TOS.Endpoint)
	c.Storage.TOS.Bucket = strings.TrimSpace(c.Storage.TOS.Bucket)
	c.Storage.TOS.AccessKeyID = strings.TrimSpace(c.Storage.TOS.AccessKeyID)
	c.Storage.TOS.SecretAccessKey = strings.TrimSpace(c.Storage.TOS.SecretAccessKey)
	c.Storage.TOS.Region = strings.TrimSpace(c.Storage.TOS.Region)
	c.Storage.TOS.BaseURL = strings.TrimRight(strings.TrimSpace(c.Storage.TOS.BaseURL), "/")
	if c.Storage.TOS.RequestTimeout <= 0 {
		c.Storage.TOS.RequestTimeout = DefaultTOSRequestTimeout
	}
	if c.Storage.TOS.ConnectTimeout <= 0 {
		c.Storage.TOS.ConnectTimeout = DefaultTOSConnectTimeout
	}
	c.Runtime.HTTPAddr = valueOrDefault(c.Runtime.HTTPAddr, DefaultAgentHTTPAddr)
	return c
}

func (c Config) ValidateDeepSeek() error {
	c = c.Normalize()
	if c.DeepSeek.APIKey == "" {
		return fmt.Errorf("%w: set %s", ErrMissingDeepSeekAPIKey, EnvDeepSeekAPIKey)
	}
	return nil
}

func (c Config) Sanitized() SanitizedConfig {
	c = c.Normalize()
	return SanitizedConfig{
		DeepSeek: SanitizedDeepSeekConfig{
			APIKey:  credentialStatus(c.DeepSeek.APIKey, EnvDeepSeekAPIKey),
			Model:   c.DeepSeek.Model,
			BaseURL: c.DeepSeek.BaseURL,
		},
		Image2:   credentialStatus(c.Image2.APIKey, EnvImage2APIKey),
		Seedance: credentialStatus(c.Seedance.APIKey, EnvSeedanceAPIKey),
		Storage: SanitizedStorageConfig{
			BusinessDatabaseURL: credentialStatus(c.Storage.BusinessDatabaseURL, EnvBusinessDatabaseURL),
			AgentDatabaseURL:    credentialStatus(c.Storage.AgentDatabaseURL, EnvAgentDatabaseURL),
			GenerationRedis: SanitizedGenerationRedisConfig{
				SanitizedRedisConfig: SanitizedRedisConfig{
					Addr:     c.Storage.GenerationRedis.Addr,
					Password: credentialStatus(c.Storage.GenerationRedis.Password, EnvAgentGenerationRedisPassword),
					DB:       c.Storage.GenerationRedis.DB,
				},
				ListKey: c.Storage.GenerationRedis.ListKey,
			},
			RuntimeRedis: SanitizedRuntimeRedisConfig{
				SanitizedRedisConfig: SanitizedRedisConfig{
					Addr:     c.Storage.RuntimeRedis.Addr,
					Password: credentialStatus(c.Storage.RuntimeRedis.Password, EnvAgentRuntimeRedisPassword),
					DB:       c.Storage.RuntimeRedis.DB,
				},
				Mode:         c.Storage.RuntimeRedis.Mode,
				StreamMaxLen: c.Storage.RuntimeRedis.StreamMaxLen,
			},
			TOS: SanitizedTOSConfig{
				Endpoint:        c.Storage.TOS.Endpoint,
				Bucket:          c.Storage.TOS.Bucket,
				AccessKeyID:     credentialStatus(c.Storage.TOS.AccessKeyID, EnvTOSAccessKeyID),
				SecretAccessKey: credentialStatus(c.Storage.TOS.SecretAccessKey, EnvTOSSecretAccessKey),
				Region:          c.Storage.TOS.Region,
				BaseURL:         c.Storage.TOS.BaseURL,
				RequestTimeout:  c.Storage.TOS.RequestTimeout.String(),
				ConnectTimeout:  c.Storage.TOS.ConnectTimeout.String(),
			},
		},
		Runtime: c.Runtime,
	}
}

func credentialStatus(value string, envName string) CredentialStatus {
	return CredentialStatus{
		Configured: strings.TrimSpace(value) != "",
		Env:        envName,
	}
}

func env(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func envOrDefault(name string, fallback string) string {
	return valueOrDefault(os.Getenv(name), fallback)
}

func intEnvOrDefault(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func durationEnvOrDefault(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func valueOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
