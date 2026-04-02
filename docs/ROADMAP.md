# Roadmap — Feature Ideas

Captured 2026-03-30. Ranked by viral potential and user impact.

## Tier 1 — Viral Potential

### Session Autopsy
Auto-generated markdown summary when a session ends: what was accomplished, files changed, commands run, total cost, errors. Exportable. Turns monitoring into a knowledge base. The "I paste these into standup" feature.

### MCP Server Mode
Expose claude-monitor as an MCP server so Claude Code can query its own usage mid-session. "How much have I spent today?" "What was my most expensive session?" Splitrail does this — it's a unique integration story.

### Agent Swarm HQ
Upgrade the existing graph view with live status badges (blocked/idle/working), attention notifications when agents need input. We already have the subagent hierarchy and graph — nobody else has this in a web dashboard.

### Burn Rate Gauge
Real-time depletion estimate: "At current rate, you'll hit your limit in 2h 14m." The #1 pain point from the March 2026 rate drain incident. Claude HUD got 9k stars partly on this. Challenge: Claude doesn't expose plan limits, so this requires either manual config or inference from throttling patterns.

## Tier 2 — Strong Stickiness

### Cost Forecast / Budget Guardian
Daily budget with projected spend based on current trajectory. Browser notifications at 50%/80%/100% of daily/weekly budget. "This saved me from a $400 day."

### Productivity Score
Lines changed, files modified, commits created, tests passing — correlated with cost. "This session cost $12 and produced 847 lines across 23 files." ROI justification for managers. Data partially available from JSONL (tool results contain file changes).

### Slack Webhook Notifications
Configurable webhooks for: agent needs input, budget exceeded, session completed with summary. Teams already use Slack + Claude Code together.

## Tier 3 — Growth / Distribution

### Claude Code Statusline Plugin
Burn rate + agent count + budget status embedded in Claude Code's native statusline. How Claude HUD got 9k stars — no browser tab needed.

### GitHub Actions Integration
Post cost/productivity summary as a PR comment: "This PR was developed across 3 Claude sessions costing $28.40 total."

### VS Code / Cursor Sidebar
Embed session status in the editor sidebar. Splitrail has this.

### Multi-AI Tool Support
Track Codex CLI, Gemini CLI, Copilot alongside Claude Code. Splitrail's main play — but big scope.

## Competitive Landscape

| Capability | claude-monitor | ccusage | splitrail | claude-hud | cctop |
|---|---|---|---|---|---|
| Live session feed | **Yes** | No | No | No | No |
| Agent hierarchy/graph | **Yes** | No | No | No | No |
| Session replay | **Yes** | No | No | No | No |
| Analytics/trends | **Yes** | Yes (tables) | No | No | No |
| Docker auto-discovery | **Yes** | No | No | No | No |
| Rate limit tracking | No | Yes (5h blocks) | No | Yes | No |
| Terminal statusline | No | Yes (beta) | No | **Yes** | No |
| MCP server | No | Yes | **Yes** | No | No |
| Cross-machine sync | No | No | **Yes** (cloud) | No | No |
| VS Code extension | No | No | **Yes** | No | No |
| Multi-AI-tool support | No | Partial | **Yes** (7+) | No | Partial |
| Click-to-jump | No | No | No | No | **Yes** |
