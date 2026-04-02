# Burn Rate Gauge

**Date:** 2026-03-30
**Status:** Approved
**Branch:** `feat/agent-swarm-burn-rate`

## Summary

Expand the existing budget popover into a "Cost Intelligence" panel showing real-time burn rate with a 10-minute sparkline, projected daily cost, token velocity, per-session breakdown, and budget progress with depletion estimate. No new backend endpoints — all computed from existing session data.

## Motivation

Users on Max/Team plans can't see how fast they're consuming their quota. The topbar shows a static $/min number but provides no trend, no projection, and no relationship to budget. The March 2026 rate drain incident showed that users need proactive awareness of consumption velocity.

## Design Decisions

- **Merge into budget popover** rather than a separate panel. One "cost intelligence" panel avoids two similar popovers competing for attention. Budget settings become a collapsible section within.
- **Session-based burn rate** rather than rolling window. Matches how users think ("this session is burning through tokens") and uses existing `costRate` data.
- **Frontend-only computation** — no new API endpoints. Burn rate samples collected from `state.sessions` every 5 seconds. Ring buffer holds 10 minutes of history.
- **Canvas sparkline** rather than Chart.js — a 300x60 polyline is simpler and avoids loading Chart.js into the topbar's critical path.

## UI Layout

Panel triggered by clicking the $/MIN or cost stat in the topbar. Replaces the current budget popover.

```
┌─────────────────────────────────────────┐
│  BURN RATE              $0.109/min      │
│  ╭─────────────────────────────────╮    │
│  │  ▁▂▃▅▇▅▃▄▅▆▇█▇▆▅▄▃▂▁▂▃▅▇     │    │
│  ╰─────────────────────────────────╯    │
│  10 min                          now    │
│                                         │
│  PROJECTED TODAY    $14.20              │
│  TOKEN VELOCITY     8.2k tok/min       │
│                                         │
│  ─── ACTIVE SESSIONS ───────────────── │
│  claude-monitor   $0.085/min  opus     │
│  battle           $0.024/min  sonnet   │
│                                         │
│  ─── BUDGET ────────────────────────── │
│  ██████████░░░░░░░░░░  47%  $14/$30   │
│  Depletes in ~3h 12m at current rate   │
│                                         │
│  ▶ Budget Settings                     │
│    Threshold: [$30     ]               │
│    Alerts:    [✓] Browser notifications │
└─────────────────────────────────────────┘
```

- Sparkline: small Canvas polyline (300x60), no Chart.js
- Budget section only visible when a threshold is configured
- Budget Settings collapsed by default, expands inline
- Panel closes on click outside or Escape
- Per-session list shows each active session with individual $/min and model

## Components

### `web/src/burn-rate.ts` — Sampling Module

Ring buffer that collects burn rate samples every 5 seconds from active sessions.

**Types:**
```typescript
interface BurnRateSample {
  timestamp: number;       // Date.now()
  costRate: number;        // aggregate $/min across active sessions
  tokenRate: number;       // aggregate tokens/min across active sessions
  totalCost: number;       // current total spend (from stats or session sum)
}
```

**Ring buffer:**
- Capacity: 120 entries (10 minutes at 5-second intervals)
- Oldest entries rotate out when full

**Exported functions:**
- `startSampling()` — begins the 5-second interval. Called once at app init. Reads from `state.sessions` to compute aggregate costRate and tokenRate.
- `stopSampling()` — clears the interval (for cleanup)
- `getSamples()` — returns all samples in chronological order
- `getCurrentRate()` — returns latest sample's costRate (or 0 if no samples)
- `getTokenRate()` — returns latest sample's tokenRate (or 0)
- `getProjectedDailyCost(currentTotalCost: number)` — extrapolates: `currentTotalCost + (currentRate * remainingMinutesToday)`
- `getDepletionEstimate(budget: number, spent: number)` — returns minutes until budget exhausted: `(budget - spent) / currentRate * 60`. Returns null if rate is 0 or budget not set.

**Token rate computation:**
- Sum effective input + output tokens across all active sessions
- Track token delta between consecutive samples: `(currentTokens - previousTokens) / intervalSeconds * 60`
- Uses `session.inputTokens + session.cacheReadTokens + session.cacheCreationTokens + session.outputTokens` as the total

### `web/src/components/budget-popover.ts` — Expanded Panel

Modify the existing budget popover to include burn rate display.

**Current behavior preserved:**
- Budget threshold input
- Currency display
- Browser notification toggle
- Notification trigger logic

**New sections added (above budget settings):**
1. **Header** — "BURN RATE" label + current $/min value
2. **Sparkline** — Canvas element (300x60) drawing a polyline from `getSamples()`. Green when stable/declining, orange when rising. X-axis: 10 min → now. Y-axis: auto-scaled to sample range.
3. **Metrics row** — Projected today + token velocity
4. **Active sessions list** — each active session: name, $/min, model badge
5. **Budget progress** — progress bar + percentage + "depletes in X" (only when budget configured)
6. **Budget Settings** — existing config, collapsed by default with ▶ toggle

**Sparkline rendering:**
- Create canvas on panel open, destroy on close
- Draw polyline through sample points with `ctx.lineTo()`
- Fill area below line with semi-transparent gradient
- Redraw every 5 seconds while panel is open (sync with sampling)

**Panel trigger:**
- Currently the gear icon (⚙) opens the budget popover
- Keep that trigger, plus make the $/MIN stat clickable to open the same panel
- Panel position: anchored below the topbar stat area

## Data Flow

```
state.sessions (live) ──► burn-rate.ts (5s sampling) ──► ring buffer
                                                              │
topbar stat click ──► budget-popover.ts ──► reads ring buffer │
                          │                                    │
                          ├── sparkline canvas ◄──────────────┘
                          ├── projected cost (computation)
                          ├── token velocity (computation)
                          ├── per-session list (from state.sessions)
                          └── budget progress (from state.stats + threshold)
```

No new API calls. No backend changes. All reactive to existing state.

## Testing

### Unit Tests (burn-rate.ts)
- Ring buffer: add 120+ samples, verify oldest dropped, getSamples returns chronological order
- getCurrentRate: returns 0 when empty, returns latest value when populated
- getProjectedDailyCost: correct extrapolation at various times of day
- getDepletionEstimate: correct for active burn, returns null for zero rate, returns null when no budget

### Manual Testing
- Open panel with 0, 1, and 3+ active sessions
- Verify sparkline updates every 5 seconds
- Verify projected cost changes as sessions start/stop
- Verify budget progress bar matches actual spend vs threshold
- Verify depletion estimate updates in real time
- Verify panel closes on Escape and click-outside
- Verify budget settings still work (threshold input, notification toggle)

## Out of Scope

- Backend API changes
- Rate limit tracking (JSONL doesn't contain rate limit events)
- Historical burn rate (only tracks current session lifetime)
- Agent Swarm HQ (separate feature, ships after this)
