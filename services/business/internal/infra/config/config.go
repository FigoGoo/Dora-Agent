package config

import (
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/internal/envconfig"
)

var DefaultEnvFiles = []string{".env.example", ".env.local"}

type BusinessConfig struct {
	AppEnv                     string
	AppName                    string
	LogLevel                   string
	DatabaseURL                string
	KitexPort                  int
	KitexAddr                  string
	KitexRegistry              string
	KitexTimeout               time.Duration
	EtcdEndpoints              []string
	EtcdNamespace              string
	ServiceName                string
	HTTPEnabled                bool
	HTTPPort                   int
	HTTPAddr                   string
	PublicWebBaseURL           string
	AdminBootstrapAccount      string
	AdminBootstrapPasswordHash string
	AdminBootstrapSecretRef    string
	TOS                        TOSConfig
	TLS                        TLSConfig
	SecretEncryptionKeyRef     string
	CORSAllowedOrigins         []string
}

type TOSConfig struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	BaseURL         string
	RequestTimeout  time.Duration
	ConnectTimeout  time.Duration
}

type TLSConfig struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	AccessKeySecret string
	ProjectID       string
	TopicID         string
}

func Load() (BusinessConfig, error) {
	return LoadFrom(DefaultEnvFiles...)
}

func LoadFrom(paths ...string) (BusinessConfig, error) {
	values, err := envconfig.Load(paths...)
	if err != nil {
		return BusinessConfig{}, err
	}

	databaseURL, err := values.Required("BUSINESS_DATABASE_URL")
	if err != nil {
		return BusinessConfig{}, err
	}
	serviceName, err := values.Required("BUSINESS_SERVICE_NAME")
	if err != nil {
		return BusinessConfig{}, err
	}
	port, err := values.Int("BUSINESS_KITEX_PORT", 19001)
	if err != nil {
		return BusinessConfig{}, err
	}
	httpPort, err := values.Int("BUSINESS_HTTP_PORT", 19080)
	if err != nil {
		return BusinessConfig{}, err
	}
	httpEnabled, err := values.Bool("BUSINESS_HTTP_ENABLED", true)
	if err != nil {
		return BusinessConfig{}, err
	}
	httpAddr := values.String("BUSINESS_HTTP_ADDR", "")
	if httpEnabled && httpAddr == "" {
		return BusinessConfig{}, fmt.Errorf("BUSINESS_HTTP_ADDR is required when BUSINESS_HTTP_ENABLED=true")
	}
	kitexTimeout, err := values.Milliseconds("KITEX_TIMEOUT_MS", 3*time.Second)
	if err != nil {
		return BusinessConfig{}, err
	}
	requestTimeout, err := values.Duration("TOS_REQUEST_TIMEOUT", 30*time.Second)
	if err != nil {
		return BusinessConfig{}, err
	}
	connectTimeout, err := values.Duration("TOS_CONNECT_TIMEOUT", 10*time.Second)
	if err != nil {
		return BusinessConfig{}, err
	}

	return BusinessConfig{
		AppEnv:                     values.String("APP_ENV", "local"),
		AppName:                    values.String("APP_NAME", "dora-agent"),
		LogLevel:                   values.String("LOG_LEVEL", "info"),
		DatabaseURL:                databaseURL,
		KitexPort:                  port,
		KitexAddr:                  fmt.Sprintf(":%d", port),
		KitexRegistry:              values.String("KITEX_REGISTRY", "none"),
		KitexTimeout:               kitexTimeout,
		EtcdEndpoints:              values.CSV("ETCD_ENDPOINTS"),
		EtcdNamespace:              values.String("ETCD_NAMESPACE", ""),
		ServiceName:                serviceName,
		HTTPEnabled:                httpEnabled,
		HTTPPort:                   httpPort,
		HTTPAddr:                   httpAddr,
		PublicWebBaseURL:           values.String("PUBLIC_WEB_BASE_URL", ""),
		AdminBootstrapAccount:      values.String("ADMIN_BOOTSTRAP_ACCOUNT", ""),
		AdminBootstrapPasswordHash: values.String("ADMIN_BOOTSTRAP_PASSWORD_HASH", ""),
		AdminBootstrapSecretRef:    values.String("ADMIN_BOOTSTRAP_CREDENTIAL_SECRET_REF", ""),
		TOS: TOSConfig{
			Endpoint:        values.String("TOS_ENDPOINT", ""),
			Bucket:          values.String("TOS_BUCKET", ""),
			AccessKeyID:     values.String("TOS_ACCESS_KEY_ID", ""),
			SecretAccessKey: values.String("TOS_SECRET_ACCESS_KEY", ""),
			Region:          values.String("TOS_REGION", ""),
			BaseURL:         values.String("TOS_BASE_URL", ""),
			RequestTimeout:  requestTimeout,
			ConnectTimeout:  connectTimeout,
		},
		TLS: TLSConfig{
			Endpoint:        values.String("VOLC_TLS_ENDPOINT", ""),
			Region:          values.String("VOLC_TLS_REGION", ""),
			AccessKeyID:     values.String("VOLC_TLS_ACCESS_KEY_ID", ""),
			AccessKeySecret: values.String("VOLC_TLS_ACCESS_KEY_SECRET", ""),
			ProjectID:       values.String("VOLC_TLS_PROJECT_ID", ""),
			TopicID:         values.String("VOLC_TLS_TOPIC_ID", ""),
		},
		SecretEncryptionKeyRef: values.String("SECRET_ENCRYPTION_KEY_REF", ""),
		CORSAllowedOrigins:     values.CSV("CORS_ALLOWED_ORIGINS"),
	}, nil
}
