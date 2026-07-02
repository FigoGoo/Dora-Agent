SHELL := /usr/bin/env bash

.PHONY: fixture-gate go-test regression frontend-test admin-frontend-test

fixture-gate:
	python3 scripts/validate-fixtures.py

go-test:
	go test -count=1 ./services/... ./internal/...

regression:
	scripts/validate-regression.sh

frontend-test:
	npm --prefix frontend test
	npm --prefix frontend run build

admin-frontend-test:
	pnpm --dir admin_frontend test
	pnpm --dir admin_frontend build
