//go:build localsmoke

// Package main 提供仅允许 local 环境执行的正常 Argon2id Smoke 账号准备命令。
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/clock"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/business/internal/localseed"
	"github.com/FigoGoo/Dora-Agent/business/internal/postgres"
)

// output 是命令成功时写到标准输出的非敏感稳定结果。
type output struct {
	// Status 准备完成时固定为 ready。
	Status string `json:"status"`
	// UserID 是创建或重放的本地 Smoke 用户 UUIDv7。
	UserID string `json:"user_id"`
	// Created 表示本次是否首次写入账号。
	Created bool `json:"created"`
}

// main 执行本地 Seeder，并用固定安全错误阻止 DSN、密码或 SQL 细节进入终端。
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "本地冒烟账号准备失败")
		os.Exit(1)
	}
}

// run 校验 local 环境，打开有界数据库连接，并通过正式 User Repository 幂等准备账号。
func run() error {
	if strings.TrimSpace(os.Getenv("DORA_ENV")) != "local" {
		return localseed.ErrLocalEnvironmentRequired
	}
	dsn := strings.TrimSpace(os.Getenv("BUSINESS_DATABASE_URL"))
	if !localseed.IsSafeLocalBusinessDSN(dsn) {
		return errors.New("unsafe local Business DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := postgres.Open(ctx, config.PostgreSQLConfig{
		DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1,
		ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 3 * time.Second,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	repository, err := postgres.NewUserRepository(client)
	if err != nil {
		return err
	}
	seeder, err := localseed.New(
		repository, idgen.UUIDv7{}, clock.System{}, rand.Reader,
		localseed.Argon2idHasher{}, auth.Argon2idVerifier{},
		localseed.Config{
			Environment: os.Getenv("DORA_ENV"),
			Email:       os.Getenv("DORA_SMOKE_USER_EMAIL"),
			Password:    os.Getenv("DORA_SMOKE_USER_PASSWORD"),
			DisplayName: os.Getenv("DORA_SMOKE_USER_DISPLAY_NAME"),
		},
	)
	if err != nil {
		return err
	}
	result, err := seeder.Ensure(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(output{Status: "ready", UserID: result.UserID, Created: result.Created})
}
