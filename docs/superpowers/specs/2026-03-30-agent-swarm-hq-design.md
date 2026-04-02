# Agent Swarm HQ

**Date:** 2026-03-30
**Status:** Approved
**Branch:** `feat/agent-swarm-hq`

## Summary

Upgrade graph and sequence views with live agent status awareness — status-colored nodes with pulsing attention rings, attention badges in sequence list, and a count badge on the GRAPH tab button when agents need attention.

## What "Needs Attention" Means

An agent needs attention when:
- Its status is `waiting` (waiting for user input/permission)
- Its error count increased since last seen (new error, not historical)

## Changes

### 1. Graph View — Status-Aware Nodes

Node colors by status:
- `thinking` — yellow (#ffcc00) with subtle glow
- `tool_use` — blue (#4488ff)
- `waiting` — amber (#ffa64a) with pulsing ring animation
- `idle` (active but idle) — green (#00ff88)
- Inactive/done — gray (#44445a)
- New error — red pulsing ring (#ff6b6b) overlay, regardless of status

Pulsing ring: animated via `globalAlpha` oscillation in the render loop. Nodes needing attention pulse between 0.3 and 1.0 alpha on a 1-second cycle.

Tooltip upgrade — show:
- Session name (already shown)
- Status badge (THINKING / TOOL / WAITING / IDLE)
- Current tool name (when in tool_use)
- Cost: $X.XX ($Y.YYY/min)
- Error count (if > 0, in red)
- Token count

### 2. Sequence List — Attention Badges

- Sessions needing attention get a badge: amber "WAITING" or red "ERROR"
- Attention sessions sort to the top of the list
- Show current tool name for tool_use sessions (from tool-tracker)
- Add cost rate to each entry

### 3. GRAPH Tab Badge

- When agents need attention and view !== 'graph', show count on GRAPH button: "GRAPH (2)"
- Subscribe to session changes, compute attention count
- Clear badge text when switching to graph view

## Attention Tracking

Track error counts to detect "new errors":
- Module-level `Map<string, number>` storing last-seen error count per session
- On each session update, compare current errorCount to stored value
- If current > stored, mark as needing attention
- When user views graph, update stored counts (acknowledge)

No state.ts changes needed — attention is computed locally in graph-view.ts.

## Files

- Modify: `web/src/components/graph-view.ts` — status colors, pulsing animation, tooltip upgrade, attention tracking, sequence list badges
- Modify: `web/src/components/topbar.ts` — attention badge on GRAPH button

## Out of Scope

- Browser notifications for agent attention (existing error notifications cover this)
- Sound alerts
- Agent Swarm as a separate view (enhances existing graph view)
