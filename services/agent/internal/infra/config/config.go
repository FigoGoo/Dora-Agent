package config

import (
	"context"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/configsource"
)

var DefaultEnvFiles = []string{".env.example", ".env.local"}

type AgentConfig struct {
	AppEnv                 string
	AppName                string
	LogLevel               string
	HTTPPort               int
	HTTPAddr               string
	DatabaseURL            string
	ServiceName            string
	BusinessServiceName    string
	KitexRegistry          string
	EtcdEndpoints          []string
	EtcdNamespace          string
	KitexTimeout           time.Duration
	SSEEnabled             bool
	WSEnabled              bool
	SSEHeartbeatSeconds    int
	EventReplayPageSize    int
	EventReplayMaxPageSize int
	ConfigSource           string
	DefaultConfigVersion   string
	ToolAllowlist          []string
	MemoryEnabled          bool
	ToolDefaultTimeout     time.Duration
	SafetyPolicyVersion    string
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
	memoryEnabled, err := values.Bool("AGENT_MEMORY_ENABLED", true)
	if err != nil {
		return AgentConfig{}, err
	}
	if replayPageSize <= 0 || replayMaxPageSize < replayPageSize {
		return AgentConfig{}, fmt.Errorf("invalid agent event replay page size: page=%d max=%d", replayPageSize, replayMaxPageSize)
	}

	return AgentConfig{
		AppEnv:                 values.String("APP_ENV", "local"),
		AppName:                values.String("APP_NAME", "dora-agent"),
		LogLevel:               values.String("LOG_LEVEL", "info"),
		HTTPPort:               httpPort,
		HTTPAddr:               httpAddr,
		DatabaseURL:            databaseURL,
		ServiceName:            values.String("AGENT_SERVICE_NAME", "dora.agent"),
		BusinessServiceName:    values.String("BUSINESS_SERVICE_NAME", "dora.business"),
		KitexRegistry:          values.String("KITEX_REGISTRY", "none"),
		EtcdEndpoints:          values.CSV("ETCD_ENDPOINTS"),
		EtcdNamespace:          values.String("ETCD_NAMESPACE", ""),
		KitexTimeout:           kitexTimeout,
		SSEEnabled:             sseEnabled,
		WSEnabled:              wsEnabled,
		SSEHeartbeatSeconds:    heartbeat,
		EventReplayPageSize:    replayPageSize,
		EventReplayMaxPageSize: replayMaxPageSize,
		ConfigSource:           values.String("AGENT_CONFIG_SOURCE", "postgres"),
		DefaultConfigVersion:   values.String("AGENT_DEFAULT_CONFIG_VERSION", "local-dev"),
		ToolAllowlist:          values.CSV("AGENT_TOOL_ALLOWLIST"),
		MemoryEnabled:          memoryEnabled,
		ToolDefaultTimeout:     toolTimeout,
		SafetyPolicyVersion:    values.String("AGENT_SAFETY_POLICY_VERSION", "local-v1"),
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
}
