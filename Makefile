SHELL := /usr/bin/env bash

.PHONY: active-contract-gate development-ci-gate release-full-http-smoke release-browser-smoke go-test frontend-test admin-frontend-test

active-contract-gate:
	scripts/validate-active-contracts.sh

development-ci-gate:
	scripts/validate-development-ci-gate.sh

release-browser-smoke:
	scripts/validate-release-browser-smoke.sh

release-full-http-smoke:
	scripts/validate-release-full-http-smoke.sh

go-test:
	go test ./services/... ./internal/...

frontend-test:
	npm --prefix frontend test
	npm --prefix frontend run build

admin-frontend-test:
	pnpm --dir admin_frontend test
	pnpm --dir admin_frontend build
