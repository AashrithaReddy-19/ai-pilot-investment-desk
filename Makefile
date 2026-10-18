# One set of lint and test commands for everyone (B1). Run from the repository root.
#
#   make test        every unit test suite (engine, gateway, frontend)
#   make lint        formatting, vet and type checks; golangci-lint when it is installed
#   make fmt         rewrite Go formatting
#
# Gateway store tests need a disposable Postgres:  TEST_DATABASE_URL=postgres://... make test-backend
# (without it they are skipped, not failed).

PYTHON ?= decision-engine/venv/bin/python

.PHONY: test test-engine test-backend test-frontend lint lint-engine lint-backend lint-frontend fmt contracts

test: test-engine test-backend test-frontend

test-engine:
	cd decision-engine && $(abspath $(PYTHON)) -m pytest -q

test-backend:
	cd backend && go test -race ./internal/...

test-frontend:
	cd frontend && npm test

lint: lint-engine lint-backend lint-frontend

lint-engine:
	cd decision-engine && $(abspath $(PYTHON)) -m compileall -q app scripts tests

lint-backend:
	@cd backend && unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	cd backend && go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then cd backend && golangci-lint run ./...; else echo "golangci-lint not installed: skipped (config in backend/.golangci.yml)"; fi

lint-frontend:
	cd frontend && npm run typecheck

fmt:
	cd backend && gofmt -w cmd internal

# Regenerate the engine contract examples after an intended engine output change
contracts:
	cd decision-engine && $(abspath $(PYTHON)) scripts/build_contracts.py
