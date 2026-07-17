GO ?= go
MODULES := business agent worker
ENV_FILE ?= .env.local
COMPOSE := docker compose --env-file $(ENV_FILE) -f deploy/local/compose.yaml
TOOLS_DIR := $(CURDIR)/.local/tools
KITEX_VERSION := v0.16.2
THRIFTGO_VERSION := v0.4.5
MIGRATE_VERSION := v4.19.0
W0_ENV_FILE ?= .env.example

.PHONY: verify test test-smoke-contracts test-document-single-source test-user-message-runtime-smoke test-analyze-materials-runtime-smoke test-plan-storyboard-runtime-smoke test-write-prompts-runtime-smoke test-trial-basic test-local-smoke-seeders vet race build test-frontend build-frontend check-frontend rpc-tools foundation-rpc-tools migration-tools generate-foundation-rpc generate-session-rpc generate-rpc check-generated check-migrations check-database-contracts local-up local-down local-reset migrate-up migrate-down seed-local-smoke-user foundation-smoke w0-smoke w0-browser-smoke w05-smoke w05-browser-smoke w1-smoke w1-browser-smoke plan-spec-preview-smoke user-message-runtime-smoke analyze-materials-runtime-smoke plan-storyboard-runtime-smoke write-prompts-runtime-smoke trial-basic run-business run-agent run-worker

verify:
	@for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) mod verify) || exit 1; done

test: test-smoke-contracts test-local-smoke-seeders
	@for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) test ./...) || exit 1; done

test-smoke-contracts:
	@./scripts/tests/document-single-source-test.sh
	@./scripts/tests/w1-smoke-mode-test.sh
	@./scripts/tests/smoke-secret-transport-test.sh
	@./scripts/tests/plan-spec-preview-smoke-test.sh
	@./scripts/tests/user-message-runtime-smoke-test.sh
	@./scripts/tests/analyze-materials-runtime-v2-smoke-test.sh
	@./scripts/tests/plan-storyboard-runtime-v2-smoke-test.sh
	@./scripts/tests/write-prompts-runtime-v2-smoke-test.sh
	@./scripts/tests/trial-basic-test.sh

test-document-single-source:
	@./scripts/tests/document-single-source-test.sh

test-user-message-runtime-smoke:
	@./scripts/tests/user-message-runtime-smoke-test.sh

test-analyze-materials-runtime-smoke:
	@./scripts/tests/analyze-materials-runtime-v2-smoke-test.sh

test-plan-storyboard-runtime-smoke:
	@./scripts/tests/plan-storyboard-runtime-v2-smoke-test.sh

test-write-prompts-runtime-smoke:
	@./scripts/tests/write-prompts-runtime-v2-smoke-test.sh

test-trial-basic:
	@./scripts/tests/trial-basic-test.sh

test-local-smoke-seeders:
	@cd business && GOWORK=off $(GO) test -tags localsmoke ./cmd/local-smoke-seeder ./cmd/local-smoke-reviewer-seeder
	@cd business && GOWORK=off $(GO) test -tags localsmoke ./cmd/local-smoke-analyze-materials-fixture
	@cd agent && GOWORK=off $(GO) test -tags localsmoke ./cmd/local-smoke-snapshot-verifier ./cmd/local-smoke-user-message-legacy-seeder ./cmd/local-smoke-analyze-materials-authority

vet:
	@for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) vet ./...) || exit 1; done

race:
	@for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) test -race ./...) || exit 1; done

build:
	@mkdir -p .local/bin
	@cd business && GOWORK=off $(GO) build -o ../.local/bin/business-service ./cmd/business-service
	@cd agent && GOWORK=off $(GO) build -o ../.local/bin/agent-service ./cmd/agent-service
	@cd worker && GOWORK=off $(GO) build -o ../.local/bin/business-worker ./cmd/business-worker

test-frontend:
	@cd frontend && npm test

build-frontend:
	@cd frontend && npm run build

check-frontend: test-frontend build-frontend

rpc-tools foundation-rpc-tools:
	@mkdir -p $(TOOLS_DIR)
	@GOBIN=$(TOOLS_DIR) GOWORK=off $(GO) install github.com/cloudwego/kitex/tool/cmd/kitex@$(KITEX_VERSION)
	@GOBIN=$(TOOLS_DIR) GOWORK=off $(GO) install github.com/cloudwego/thriftgo@$(THRIFTGO_VERSION)

migration-tools:
	@mkdir -p $(TOOLS_DIR)
	@GOBIN=$(TOOLS_DIR) GOWORK=off $(GO) install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

generate-foundation-rpc:
	@KITEX_BIN=$(TOOLS_DIR)/kitex THRIFTGO_BIN=$(TOOLS_DIR)/thriftgo ./scripts/generate-foundation-rpc.sh

generate-session-rpc:
	@KITEX_BIN=$(TOOLS_DIR)/kitex THRIFTGO_BIN=$(TOOLS_DIR)/thriftgo ./scripts/generate-session-rpc.sh

generate-rpc: generate-foundation-rpc generate-session-rpc

check-generated: generate-rpc
	@git diff --exit-code -- business/kitex_gen agent/kitex_gen

check-migrations:
	@./scripts/check-migrations.sh

check-database-contracts: migration-tools
	@set -a; . ./$(ENV_FILE); set +a; GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/check-database-contracts.sh all

local-up:
	@$(COMPOSE) up -d
	@ENV_FILE=$(ENV_FILE) ./scripts/wait-for-local-infra.sh

local-down:
	@$(COMPOSE) down

local-reset:
	@$(COMPOSE) down --volumes --remove-orphans

migrate-up:
	@set -a; . ./$(ENV_FILE); set +a; ./scripts/migrate.sh business up
	@set -a; . ./$(ENV_FILE); set +a; ./scripts/migrate.sh agent up
	@set -a; . ./$(ENV_FILE); set +a; ./scripts/migrate.sh worker up

migrate-down:
	@set -a; . ./$(ENV_FILE); set +a; ./scripts/migrate.sh worker down
	@set -a; . ./$(ENV_FILE); set +a; ./scripts/migrate.sh agent down
	@set -a; . ./$(ENV_FILE); set +a; ./scripts/migrate.sh business down

seed-local-smoke-user:
	@set -a; . ./$(ENV_FILE); set +a; cd business && GOWORK=off $(GO) run -tags localsmoke ./cmd/local-smoke-seeder

foundation-smoke: build
	@ENV_FILE=$(ENV_FILE) ./scripts/smoke-foundation.sh

w0-smoke: migration-tools build
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/smoke-w0-transport.sh

w0-browser-smoke: migration-tools build check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate W0_RUN_BROWSER_SMOKE=1 ./scripts/smoke-w0-transport.sh

# W0.5 在兼容既有 W0 命令的同时，显式标记 Workspace Snapshot/SSE 门禁。
w05-smoke: w0-smoke

w05-browser-smoke: w0-browser-smoke

# W1-C2 canonical Evidence 必须包含 @w1-real-review 真实浏览器链路。
# 保留 w1-smoke 作为兼容命令，但它与 w1-browser-smoke 执行同一完整门禁。
w1-smoke: w1-browser-smoke

w1-browser-smoke: migration-tools build check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate W1_RUN_SKILL_SMOKE=1 W1_RUN_BROWSER_SMOKE=1 ./scripts/smoke-w0-transport.sh

# V1 canonical Trial：正式 local Runtime/FakeModel、真实 PG/Redis/etcd/Chromium 与零增量证据屏障。
plan-spec-preview-smoke: migration-tools check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/smoke-plan-spec-preview.sh

# 方案 A canonical Trial：强制关闭 CreationSpec Preview、开启 User Message Runtime，
# 由宿主机 Business/Agent/Vite 直连 Docker 暴露的 PG/Redis/etcd 并执行真实 Chromium。
user-message-runtime-smoke: migration-tools check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/smoke-user-message-runtime.sh

# Analyze Materials 单 Tool Development Preview：宿主机 Runtime 直连既有中间件端口，
# 使用 Go localsmoke 权威 Helper、Vite 与 Chromium，不访问容器控制面。
analyze-materials-runtime-smoke: migration-tools check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/analyze-materials-runtime-v2-smoke.sh

# Plan Storyboard M4 canonical Trial：宿主机分阶段切换 CreationSpec/Storyboard Profile，
# 直连已映射的 PG/Redis/etcd 并用 Chromium 验证 SSE、硬刷新和 Agent 重连。
plan-storyboard-runtime-smoke: migration-tools check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/plan-storyboard-runtime-v2-smoke.sh

# Write Prompts M4 canonical Trial：复用权威 Storyboard Preview Source，
# 直连已映射的 PG/Redis/etcd 并验证 exact-set、Receipt、SSE、硬刷新和 Agent 重连。
write-prompts-runtime-smoke: migration-tools check-frontend
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/write-prompts-runtime-v2-smoke.sh

# 六工具统一 MVP：使用三个独立 Test DB、一个 Agent/Coordinator、真实 Worker、Chromium、PNG/MP4 与 Workspace V5。
trial-basic: test-trial-basic
	@test -x $(TOOLS_DIR)/migrate || $(MAKE) migration-tools
	@ENV_FILE=$(W0_ENV_FILE) GO_BIN=$(GO) MIGRATE_BIN=$(TOOLS_DIR)/migrate ./scripts/trial-basic.sh

run-business:
	@set -a; . ./$(ENV_FILE); set +a; cd business && $(GO) run ./cmd/business-service

run-agent:
	@set -a; . ./$(ENV_FILE); set +a; cd agent && $(GO) run ./cmd/agent-service

run-worker:
	@set -a; . ./$(ENV_FILE); set +a; cd worker && $(GO) run ./cmd/business-worker
