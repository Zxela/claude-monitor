.PHONY: build dev clean test lint web

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

# Build frontend only
web:
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
