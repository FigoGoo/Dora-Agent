package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/asset"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetcommit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/modelconfig"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/config"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/logger"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	businesshttp "github.com/FigoGoo/Dora-Agent/services/business/internal/transport/http"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/transport/rpc"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/server"
	etcd "github.com/kitex-contrib/registry-etcd"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type App struct {
	Config       config.BusinessConfig
	Logger       *slog.Logger
	DB           *gorm.DB
	Account      *accountspace.App
	Admin        *admin.App
	Project      *project.App
	Model        *modelconfig.App
	Tool         *toolpolicy.App
	Skill        *skillcatalog.App
	Dictionary   *assetdict.App
	Credit       *credit.App
	Asset        *asset.App
	Commit       *assetcommit.App
	Work         *work.App
	Notification *notification.App
	Kitex        server.Server
	HTTPServer   *http.Server
}

func New(cfg config.BusinessConfig) (*App, error) {
	log := logger.New(os.Stdout, "business", cfg.AppEnv, cfg.LogLevel)
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open business database: %w", err)
	}
	repo := businesscore.New(db)
	guard := idempotency.NewGuard(db, 24*time.Hour, 30*time.Second)
	auditWriter := auditlog.NewGormWriter(db)
	accountApp := accountspace.New(repo, guard, auditWriter)
	adminApp := admin.New(repo, guard, auditWriter)
	projectApp := project.New(repo, guard, auditWriter)
	modelApp := modelconfig.New(repo)
	toolApp := toolpolicy.New(repo)
	skillApp := skillcatalog.New(repo)
	dictionaryApp := assetdict.New(repo)
	notificationApp := notification.New(repo, guard)
	skillApp.SetNotificationService(notificationApp)
	skillApp.SetDictionary(dictionaryApp)
	creditApp := credit.New(repo, guard, auditWriter)
	assetApp := asset.New(repo, guard, auditWriter, asset.TOSOptions{
		Env: cfg.AppEnv, Bucket: cfg.TOS.Bucket, BaseURL: cfg.TOS.BaseURL,
		Endpoint: cfg.TOS.Endpoint, Region: cfg.TOS.Region,
		AccessKeyID: cfg.TOS.AccessKeyID, SecretAccessKey: cfg.TOS.SecretAccessKey,
	})
	commitVerifier, err := assetcommit.NewTOSHeadObjectVerifier(cfg.TOS.Endpoint, cfg.TOS.Region, cfg.TOS.AccessKeyID, cfg.TOS.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("create tos object verifier: %w", err)
	}
	commitApp := assetcommit.New(repo, guard, auditWriter, commitVerifier)
	workApp := work.New(repo, guard, auditWriter, work.Options{
		PublicWebBaseURL: cfg.PublicWebBaseURL, TOSBaseURL: cfg.TOS.BaseURL, Env: cfg.AppEnv, Notification: notificationApp,
	})
	if _, err := adminApp.BootstrapInitialAdmin(contextBackground(), admin.BootstrapInput{
		Account:             cfg.AdminBootstrapAccount,
		PasswordHash:        cfg.AdminBootstrapPasswordHash,
		CredentialSecretRef: cfg.AdminBootstrapSecretRef,
		TraceID:             "business-bootstrap",
	}); err != nil {
		return nil, fmt.Errorf("bootstrap initial admin: %w", err)
	}

	kitexServer, err := NewKitexServer(cfg, rpc.NewHandler(accountApp, projectApp, adminApp, modelApp, toolApp, skillApp, dictionaryApp, creditApp, assetApp, commitApp, workApp, notificationApp))
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
			Logger:       log,
			Ready:        sqlDB.PingContext,
			AccountSpace: accountApp,
			Admin:        adminApp,
			Project:      projectApp,
			Model:        modelApp,
			Tool:         toolApp,
			Skill:        skillApp,
			Dictionary:   dictionaryApp,
			Credit:       creditApp,
			Asset:        assetApp,
			Commit:       commitApp,
			Work:         workApp,
			Notification: notificationApp,
		})
		httpServer = &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		}
	}

	return &App{
		Config:       cfg,
		Logger:       log,
		DB:           db,
		Account:      accountApp,
		Admin:        adminApp,
		Project:      projectApp,
		Model:        modelApp,
		Tool:         toolApp,
		Skill:        skillApp,
		Dictionary:   dictionaryApp,
		Credit:       creditApp,
		Asset:        assetApp,
		Commit:       commitApp,
		Work:         workApp,
		Notification: notificationApp,
		Kitex:        kitexServer,
		HTTPServer:   httpServer,
	}, nil
}

func NewKitexServer(cfg config.BusinessConfig, handler *rpc.Handler) (server.Server, error) {
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

func contextBackground() context.Context {
	return context.Background()
}
