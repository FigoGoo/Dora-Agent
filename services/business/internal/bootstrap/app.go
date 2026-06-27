package bootstrap

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/config"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/logger"
	businesshttp "github.com/FigoGoo/Dora-Agent/services/business/internal/transport/http"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/server"
	etcd "github.com/kitex-contrib/registry-etcd"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type App struct {
	Config     config.BusinessConfig
	Logger     *slog.Logger
	DB         *gorm.DB
	Kitex      server.Server
	HTTPServer *http.Server
}

func New(cfg config.BusinessConfig) (*App, error) {
	log := logger.New(os.Stdout, "business", cfg.AppEnv, cfg.LogLevel)
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open business database: %w", err)
	}
	kitexServer, err := NewKitexServer(cfg, rpc.NewUnimplementedHandler())
	if err != nil {
		return nil, err
	}

	var httpServer *http.Server
	if cfg.HTTPEnabled {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("get business sql database: %w", err)
		}
		router := businesshttp.NewRouter(businesshttp.RouterOptions{
			Logger: log,
			Ready:  sqlDB.PingContext,
		})
		httpServer = &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		}
	}

	return &App{
		Config:     cfg,
		Logger:     log,
		DB:         db,
		Kitex:      kitexServer,
		HTTPServer: httpServer,
	}, nil
}

func NewKitexServer(cfg config.BusinessConfig, handler *rpc.UnimplementedHandler) (server.Server, error) {
	addr, err := net.ResolveTCPAddr("tcp", cfg.KitexAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve business kitex addr: %w", err)
	}

	opts := []server.Option{
		server.WithServiceAddr(addr),
		server.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{ServiceName: cfg.ServiceName}),
	}
	switch strings.ToLower(strings.TrimSpace(cfg.KitexRegistry)) {
	case "", "none":
	case "etcd":
		if len(cfg.EtcdEndpoints) == 0 {
			return nil, fmt.Errorf("ETCD_ENDPOINTS is required when KITEX_REGISTRY=etcd")
		}
		registryOptions := []etcd.Option{}
		if cfg.EtcdNamespace != "" {
			registryOptions = append(registryOptions, etcd.WithEtcdServicePrefix(cfg.EtcdNamespace))
		}
		registry, err := etcd.NewEtcdRegistry(cfg.EtcdEndpoints, registryOptions...)
		if err != nil {
			return nil, fmt.Errorf("create etcd registry: %w", err)
		}
		opts = append(opts, server.WithRegistry(registry))
	default:
		return nil, fmt.Errorf("unsupported KITEX_REGISTRY=%s", cfg.KitexRegistry)
	}

	svr := server.NewServer(opts...)
	if err := rpc.RegisterAll(svr, handler); err != nil {
		return nil, fmt.Errorf("register business kitex services: %w", err)
	}
	return svr, nil
}
