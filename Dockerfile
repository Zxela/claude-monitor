# Stage 1: build frontend
FROM node:22-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN mkdir -p /app/cmd/claude-monitor/static && npm run build

# Stage 2: build Go binary
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/cmd/claude-monitor/static/ ./cmd/claude-monitor/static/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o claude-monitor ./cmd/claude-monitor

# Stage 3: minimal runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=builder /app/claude-monitor .
RUN chown -R app:app /app
USER app
EXPOSE 7700
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://localhost:7700/health || exit 1
# Mount your .claude directories via volumes:
# docker run -v ~/.claude:/home/app/.claude:ro -p 7700:7700 claude-monitor
ENTRYPOINT ["./claude-monitor"]
CMD ["--port", "7700"]
