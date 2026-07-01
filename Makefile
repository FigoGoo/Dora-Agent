SHELL := /usr/bin/env bash

.PHONY: active-contract-gate pr0-ci-gate go-test frontend-test admin-frontend-test

active-contract-gate:
	scripts/validate-active-contracts.sh

pr0-ci-gate:
	scripts/validate-pr0-ci-gate.sh

go-test:
	go test ./services/... ./internal/...

frontend-test:
	npm --prefix frontend test
	npm --prefix frontend run build

admin-frontend-test:
	pnpm --dir admin_frontend test
	pnpm --dir admin_frontend build
