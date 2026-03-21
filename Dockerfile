# Stage 1: build
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o claude-monitor ./cmd/claude-monitor

# Stage 2: minimal runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/claude-monitor .
EXPOSE 7700
# Mount your .claude directories via volumes:
# docker run -v ~/.claude:/home/node/.claude:ro -p 7700:7700 claude-monitor
ENTRYPOINT ["./claude-monitor"]
CMD ["--port", "7700"]
