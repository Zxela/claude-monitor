#!/bin/sh
# Claude Code SessionStart hook — ensures claude-monitor is running.
# Installed by: claude-monitor hook install

# Check if already running
if curl -sf http://127.0.0.1:${CLAUDE_MONITOR_PORT:-7700}/health >/dev/null 2>&1; then
  exit 0
fi

# Start in background
nohup claude-monitor ${CLAUDE_MONITOR_ARGS:-} >/dev/null 2>&1 &

# Wait briefly for startup
for i in 1 2 3; do
  sleep 0.5
  if curl -sf http://127.0.0.1:${CLAUDE_MONITOR_PORT:-7700}/health >/dev/null 2>&1; then
    echo "claude-monitor started on http://127.0.0.1:${CLAUDE_MONITOR_PORT:-7700}"
    exit 0
  fi
done

echo "claude-monitor started in background"
