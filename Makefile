GO ?= go
MODULES := business agent worker
ENV_FILE ?= .env.local
COMPOSE := docker compose --env-file $(ENV_FILE) -f deploy/local/compose.yaml
TOOLS_DIR := $(CURDIR)/.local/tools
KITEX_VERSION := v0.16.2
THRIFTGO_VERSION := v0.4.5
MIGRATE_VERSION := v4.19.0
W0_ENV_FILE ?= .env.example

.PHONY: verify test test-smoke-contracts test-local-smoke-seeders vet race build test-frontend build-frontend check-frontend rpc-tools foundation-rpc-tools migration-tools generate-foundation-rpc generate-session-rpc generate-rpc check-generated check-migrations check-database-contracts local-up local-down local-reset migrate-up migrate-down seed-local-smoke-user foundation-smoke w0-smoke w0-browser-smoke w05-smoke w05-browser-smoke w1-smoke w1-browser-smoke run-business run-agent run-worker

verify:
	@for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) mod verify) || exit 1; done

test: test-smoke-contracts test-local-smoke-seeders
	@for module in $(MODULES); do (cd $$module && GOWORK=off $(GO) test ./...) || exit 1; done

test-smoke-contracts:
	@./scripts/tests/w1-smoke-mode-test.sh
	@./scripts/tests/smoke-secret-transport-test.sh

test-local-smoke-seeders:
	@cd business && GOWORK=off $(GO) test -tags localsmoke ./cmd/local-smoke-seeder ./cmd/local-smoke-reviewer-seeder

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

run-business:
	@set -a; . ./$(ENV_FILE); set +a; cd business && $(GO) run ./cmd/business-service

run-agent:
	@set -a; . ./$(ENV_FILE); set +a; cd agent && $(GO) run ./cmd/agent-service

run-worker:
	@set -a; . ./$(ENV_FILE); set +a; cd worker && $(GO) run ./cmd/business-worker
