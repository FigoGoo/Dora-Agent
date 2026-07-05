package storage

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
)

func OpenPostgres(ctx context.Context, dsn string) (*gorm.DB, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("load postgres sql db: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

func OpenAgentPostgres(ctx context.Context, cfg aigcconfig.Config) (*gorm.DB, error) {
	return OpenPostgres(ctx, cfg.Normalize().Storage.AgentDatabaseURL)
}

func OpenBusinessPostgres(ctx context.Context, cfg aigcconfig.Config) (*gorm.DB, error) {
	return OpenPostgres(ctx, cfg.Normalize().Storage.BusinessDatabaseURL)
}
