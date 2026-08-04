.PHONY: build dev clean test lint validate-spec web migrate migrate-status migrate-rollback migrate-create

# Version from release-please manifest (fallback to "dev")
VERSION ?= $(shell cat .release-please-manifest.json 2>/dev/null | grep -o '"[^"]*"$$' | tr -d '"' || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

# web/ sits inside this Go module, so `./...` also matches any Go source an npm
# package happens to vendor (eslint pulled in flatted/golang, which was being
# built, vetted and tested as part of this project). Resolve the package list
# instead of hardcoding directories, so a new top-level package is picked up
# automatically rather than silently skipped.
GOPKGS = $(shell go list ./... | grep -v '/node_modules/')

# Build frontend then Go binary
build: web
	go build -ldflags="$(LDFLAGS)" -o claude-monitor ./cmd/claude-monitor

# Build and run with live reload (frontend dev server proxies to Go backend)
dev:
	@echo "Starting Go backend on :7700..."
	@go build -ldflags="$(LDFLAGS)" -o /tmp/claude-monitor-dev ./cmd/claude-monitor && \
		/tmp/claude-monitor-dev -port 7700 & \
		GO_PID=$$!; \
		trap "kill $$GO_PID 2>/dev/null; exit" INT TERM EXIT; \
		echo "Starting Vite dev server on :5173..."; \
		cd web && npm run dev; \
		kill $$GO_PID 2>/dev/null

# Build frontend only (clean stale assets first, install deps if needed)
web:
	rm -rf cmd/claude-monitor/static/assets
	@test -d web/node_modules || (echo "Installing frontend dependencies..." && cd web && npm ci)
	cd web && npm run build

# Install frontend dependencies
install:
	cd web && npm ci

# Run all Go tests
test:
	go test $(GOPKGS) -count=1 -v

# Type-check frontend
typecheck:
	cd web && npx tsc --noEmit

# Run Go vet + frontend type check
lint: typecheck
	go vet $(GOPKGS)

# Check api/openapi.yaml against a running server. Boots its own instance on a
# scratch HOME (empty database), so it never touches ~/.claude-monitor.
# Requires: python3 with pyyaml + jsonschema.
validate-spec:
	@go build -o /tmp/claude-monitor-spec ./cmd/claude-monitor
	@HOME_DIR=$$(mktemp -d); mkdir -p "$$HOME_DIR/.claude/projects"; \
	HOME="$$HOME_DIR" /tmp/claude-monitor-spec -port 7799 >/tmp/claude-monitor-spec.log 2>&1 & \
	SRV=$$!; \
	for _ in $$(seq 1 40); do curl -sf http://127.0.0.1:7799/health >/dev/null 2>&1 && break; sleep 1; done; \
	python3 api/validate_spec.py http://127.0.0.1:7799 api/openapi.yaml; \
	STATUS=$$?; kill $$SRV 2>/dev/null; exit $$STATUS

# Clean build artifacts
clean:
	rm -f claude-monitor
	rm -rf cmd/claude-monitor/static/assets
	rm -f cmd/claude-monitor/static/index.html

# --- Migration commands ---

migrate:
	go run ./cmd/claude-monitor migrate

migrate-status:
	go run ./cmd/claude-monitor migrate status

migrate-rollback:
	go run ./cmd/claude-monitor migrate rollback

# Usage: make migrate-create NAME=add_parent_id
migrate-create:
	@if [ -z "$(NAME)" ]; then echo "Usage: make migrate-create NAME=add_parent_id"; exit 1; fi
	@NEXT=$$(ls internal/store/migrations/[0-9]*.go 2>/dev/null | wc -l | tr -d ' '); \
	NEXT=$$((NEXT + 1)); \
	FILE=$$(printf "internal/store/migrations/%03d_%s.go" $$NEXT "$(NAME)"); \
	sed "s/{{.Version}}/$$NEXT/g; s/{{.Name}}/$(NAME)/g" internal/store/migrations/template.go.tmpl > "$$FILE"; \
	echo "Created $$FILE"
