.PHONY: build dev clean test lint web migrate migrate-status migrate-rollback migrate-create

# Build frontend then Go binary
build: web
	go build -o claude-monitor ./cmd/claude-monitor

# Build and run with live reload (frontend dev server proxies to Go backend)
dev:
	@echo "Starting Go backend on :7700..."
	@go build -o /tmp/claude-monitor-dev ./cmd/claude-monitor && \
		/tmp/claude-monitor-dev -port 7700 &
	@echo "Starting Vite dev server on :5173..."
	@cd web && npm run dev

# Build frontend only (clean stale assets first)
web:
	rm -rf cmd/claude-monitor/static/assets
	cd web && npm run build

# Install frontend dependencies
install:
	cd web && npm ci

# Run all Go tests
test:
	go test ./... -count=1 -v

# Type-check frontend
typecheck:
	cd web && npx tsc --noEmit

# Run Go vet + frontend type check
lint: typecheck
	go vet ./...

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
