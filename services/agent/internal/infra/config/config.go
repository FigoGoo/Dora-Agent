package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/configsource"
)

var DefaultEnvFiles = []string{".env.example", ".env.local"}

type AgentConfig struct {
	AppEnv                   string
	AppName                  string
	LogLevel                 string
	HTTPPort                 int
	HTTPAddr                 string
	DatabaseURL              string
	ServiceName              string
	BusinessServiceName      string
	KitexRegistry            string
	EtcdEndpoints            []string
	EtcdNamespace            string
	KitexTimeout             time.Duration
	SSEEnabled               bool
	WSEnabled                bool
	SSEHeartbeatSeconds      int
	EventReplayPageSize      int
	EventReplayMaxPageSize   int
	ConfigSource             string
	DefaultConfigVersion     string
	ToolAllowlist            []string
	MemoryEnabled            bool
	ToolDefaultTimeout       time.Duration
	SafetyPolicyVersion      string
	ModelAdapter             string
	GenerationQueue          string
	GenerationRedisAddress   string
	GenerationRedisPassword  string
	GenerationRedisDB        int
	GenerationRedisListKey   string
	GenerationWorkers        int
	GenerationRecoveryAge    time.Duration
	RuntimeRedisMode         string
	RuntimeRedisAddress      string
	RuntimeRedisPassword     string
	RuntimeRedisDB           int
	RuntimeRedisStreamMaxLen int64
	DeepSeekAPIKey           string
	DeepSeekBaseURL          string
	DeepSeekModel            string
	DeepSeekMaxTokens        int
	RouterMode               string
}

func Load() (AgentConfig, error) {
	return LoadFrom(DefaultEnvFiles...)
}

func LoadFrom(paths ...string) (AgentConfig, error) {
	return loadFromWithEtcdLoader(paths, nil)
}

func loadFromWithEtcdLoader(paths []string, loader configsource.EtcdLoader) (AgentConfig, error) {
	values, err := configsource.LoadLayered(context.Background(), paths, configsource.LayeredOptions{
		ServiceNameKey:     "AGENT_SERVICE_NAME",
		DefaultServiceName: "dora.agent",
		AllowedKeys:        agentEtcdConfigKeys,
		EtcdLoader:         loader,
	})
	if err != nil {
		return AgentConfig{}, err
	}

	databaseURL, err := values.Required("AGENT_DATABASE_URL")
	if err != nil {
		return AgentConfig{}, err
	}
	httpPort, err := values.Int("AGENT_HTTP_PORT", 18080)
	if err != nil {
		return AgentConfig{}, err
	}
	httpAddr, err := values.Required("AGENT_HTTP_ADDR")
	if err != nil {
		return AgentConfig{}, err
	}
	sseEnabled, err := values.Bool("AGENT_SSE_ENABLED", true)
	if err != nil {
		return AgentConfig{}, err
	}
	wsEnabled, err := values.Bool("AGENT_WS_ENABLED", true)
	if err != nil {
		return AgentConfig{}, err
	}
	heartbeat, err := values.Int("AGENT_SSE_HEARTBEAT_SECONDS", 15)
	if err != nil {
		return AgentConfig{}, err
	}
	replayPageSize, err := values.Int("AGENT_EVENT_REPLAY_PAGE_SIZE", 10)
	if err != nil {
		return AgentConfig{}, err
	}
	replayMaxPageSize, err := values.Int("AGENT_EVENT_REPLAY_MAX_PAGE_SIZE", 100)
	if err != nil {
		return AgentConfig{}, err
	}
	kitexTimeout, err := values.Milliseconds("KITEX_TIMEOUT_MS", 3*time.Second)
	if err != nil {
		return AgentConfig{}, err
	}
	toolTimeout, err := values.Milliseconds("AGENT_TOOL_DEFAULT_TIMEOUT_MS", 120*time.Second)
	if err != nil {
		return AgentConfig{}, err
	}
	generationRedisDB, err := values.Int("AGENT_GENERATION_REDIS_DB", 0)
	if err != nil {
		return AgentConfig{}, err
	}
	runtimeRedisDB, err := values.Int("AGENT_RUNTIME_REDIS_DB", 0)
	if err != nil {
		return AgentConfig{}, err
	}
	runtimeRedisStreamMaxLen, err := values.Int("AGENT_RUNTIME_REDIS_STREAM_MAX_LEN", 1000)
	if err != nil {
		return AgentConfig{}, err
	}
	generationWorkers, err := values.Int("AGENT_GENERATION_WORKERS", 1)
	if err != nil {
		return AgentConfig{}, err
	}
	generationRecoveryAge, err := values.Duration("AGENT_GENERATION_RECOVERY_STALE_AFTER", 5*time.Minute)
	if err != nil {
		return AgentConfig{}, err
	}
	deepSeekMaxTokens, err := values.Int("DEEPSEEK_MAX_TOKENS", 2048)
	if err != nil {
		return AgentConfig{}, err
	}
	memoryEnabled, err := values.Bool("AGENT_MEMORY_ENABLED", true)
	if err != nil {
		return AgentConfig{}, err
	}
	if replayPageSize <= 0 || replayMaxPageSize < replayPageSize {
		return AgentConfig{}, fmt.Errorf("invalid agent event replay page size: page=%d max=%d", replayPageSize, replayMaxPageSize)
	}
	generationQueue := strings.ToLower(strings.TrimSpace(values.String("AGENT_GENERATION_QUEUE", "inline")))
	if generationQueue == "" {
		generationQueue = "inline"
	}
	if generationQueue != "inline" && generationQueue != "redis" {
		return AgentConfig{}, fmt.Errorf("AGENT_GENERATION_QUEUE must be inline or redis")
	}
	runtimeRedisMode := strings.ToLower(strings.TrimSpace(values.String("AGENT_RUNTIME_REDIS_MODE", "memory")))
	if runtimeRedisMode == "" {
		runtimeRedisMode = "memory"
	}
	if runtimeRedisMode != "memory" && runtimeRedisMode != "redis" {
		return AgentConfig{}, fmt.Errorf("AGENT_RUNTIME_REDIS_MODE must be memory or redis")
	}
	runtimeRedisAddress := strings.TrimSpace(values.String("AGENT_RUNTIME_REDIS_ADDR", ""))
	if runtimeRedisMode == "redis" && runtimeRedisAddress == "" {
		return AgentConfig{}, fmt.Errorf("AGENT_RUNTIME_REDIS_ADDR is required when AGENT_RUNTIME_REDIS_MODE=redis")
	}
	if generationWorkers <= 0 {
		generationWorkers = 1
	}
	if runtimeRedisStreamMaxLen <= 0 {
		runtimeRedisStreamMaxLen = 1000
	}
	modelAdapter := strings.ToLower(strings.TrimSpace(values.String("AGENT_MODEL_ADAPTER", "local")))
	if modelAdapter == "" {
		modelAdapter = "local"
	}
	if modelAdapter != "local" && modelAdapter != "deepseek" {
		return AgentConfig{}, fmt.Errorf("AGENT_MODEL_ADAPTER must be local or deepseek")
	}
	routerMode := strings.ToLower(strings.TrimSpace(values.String("AGENT_ROUTER_MODE", "mock")))
	if routerMode == "" {
		routerMode = "mock"
	}
	if routerMode != "mock" && routerMode != "llm" {
		return AgentConfig{}, fmt.Errorf("AGENT_ROUTER_MODE must be mock or llm")
	}

	return AgentConfig{
		AppEnv:                   values.String("APP_ENV", "local"),
		AppName:                  values.String("APP_NAME", "dora-agent"),
		LogLevel:                 values.String("LOG_LEVEL", "info"),
		HTTPPort:                 httpPort,
		HTTPAddr:                 httpAddr,
		DatabaseURL:              databaseURL,
		ServiceName:              values.String("AGENT_SERVICE_NAME", "dora.agent"),
		BusinessServiceName:      values.String("BUSINESS_SERVICE_NAME", "dora.business"),
		KitexRegistry:            values.String("KITEX_REGISTRY", "etcd"),
		EtcdEndpoints:            values.CSV("ETCD_ENDPOINTS"),
		EtcdNamespace:            values.String("ETCD_NAMESPACE", ""),
		KitexTimeout:             kitexTimeout,
		SSEEnabled:               sseEnabled,
		WSEnabled:                wsEnabled,
		SSEHeartbeatSeconds:      heartbeat,
		EventReplayPageSize:      replayPageSize,
		EventReplayMaxPageSize:   replayMaxPageSize,
		ConfigSource:             values.String("AGENT_CONFIG_SOURCE", "postgres"),
		DefaultConfigVersion:     values.String("AGENT_DEFAULT_CONFIG_VERSION", "local-dev"),
		ToolAllowlist:            values.CSV("AGENT_TOOL_ALLOWLIST"),
		MemoryEnabled:            memoryEnabled,
		ToolDefaultTimeout:       toolTimeout,
		SafetyPolicyVersion:      values.String("AGENT_SAFETY_POLICY_VERSION", "local-v1"),
		ModelAdapter:             modelAdapter,
		GenerationQueue:          generationQueue,
		GenerationRedisAddress:   values.String("AGENT_GENERATION_REDIS_ADDR", ""),
		GenerationRedisPassword:  values.String("AGENT_GENERATION_REDIS_PASSWORD", ""),
		GenerationRedisDB:        generationRedisDB,
		GenerationRedisListKey:   values.String("AGENT_GENERATION_REDIS_LIST_KEY", "dora:agent:generation_jobs"),
		GenerationWorkers:        generationWorkers,
		GenerationRecoveryAge:    generationRecoveryAge,
		RuntimeRedisMode:         runtimeRedisMode,
		RuntimeRedisAddress:      runtimeRedisAddress,
		RuntimeRedisPassword:     values.String("AGENT_RUNTIME_REDIS_PASSWORD", ""),
		RuntimeRedisDB:           runtimeRedisDB,
		RuntimeRedisStreamMaxLen: int64(runtimeRedisStreamMaxLen),
		DeepSeekAPIKey:           values.String("DEEPSEEK_API_KEY", ""),
		DeepSeekBaseURL:          values.String("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekModel:            values.String("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekMaxTokens:        deepSeekMaxTokens,
		RouterMode:               routerMode,
	}, nil
}

var agentEtcdConfigKeys = []string{
	"APP_ENV",
	"APP_NAME",
	"LOG_LEVEL",
	"AGENT_HTTP_PORT",
	"AGENT_HTTP_ADDR",
	"BUSINESS_SERVICE_NAME",
	"KITEX_REGISTRY",
	"KITEX_TIMEOUT_MS",
	"AGENT_SSE_ENABLED",
	"AGENT_WS_ENABLED",
	"AGENT_SSE_HEARTBEAT_SECONDS",
	"AGENT_EVENT_REPLAY_PAGE_SIZE",
	"AGENT_EVENT_REPLAY_MAX_PAGE_SIZE",
	"AGENT_CONFIG_SOURCE",
	"AGENT_DEFAULT_CONFIG_VERSION",
	"AGENT_TOOL_ALLOWLIST",
	"AGENT_MEMORY_ENABLED",
	"AGENT_TOOL_DEFAULT_TIMEOUT_MS",
	"AGENT_SAFETY_POLICY_VERSION",
	"AGENT_MODEL_ADAPTER",
	"AGENT_ROUTER_MODE",
	"AGENT_GENERATION_QUEUE",
	"AGENT_GENERATION_REDIS_ADDR",
	"AGENT_GENERATION_REDIS_DB",
	"AGENT_GENERATION_REDIS_LIST_KEY",
	"AGENT_GENERATION_WORKERS",
	"AGENT_GENERATION_RECOVERY_STALE_AFTER",
	"AGENT_RUNTIME_REDIS_MODE",
	"AGENT_RUNTIME_REDIS_ADDR",
	"AGENT_RUNTIME_REDIS_DB",
	"AGENT_RUNTIME_REDIS_STREAM_MAX_LEN",
}
