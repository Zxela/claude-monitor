# QA Issues — Frontend Testing

Tracking issues found during manual QA testing of all frontend features.

## Fixed

### 1. `t` keyboard shortcut missing handler
- **Test:** 12.7
- **Severity:** Low
- **Description:** Help overlay listed `t` as "Table view" shortcut but `main.ts` had no handler for `t`. Pressing `t` did nothing.
- **Fix:** Removed `t` entry from help-overlay.ts. Rebuilt frontend + Go binary.
- **Status:** Fixed

## Open

### 2. Invalid hash stuck on "Loading..."
- **Test:** 13.4
- **Severity:** Low
- **Description:** Navigating to `#session=nonexistent-id` shows "Loading..." permanently. The feed never shows a failure message or times out. The `loadRecentMessages` function in `feed-panel.ts` catches the error and shows "Failed to load messages", but only if `currentLoadSessionId` still matches — which it does, so likely the API returns a 200 with empty results rather than erroring, leaving the feed in an empty loaded state with no entries rendered.
- **File:** `web/src/components/feed-panel.ts:278-316`
- **Expected:** Should show "No events found" or similar after loading completes with empty results.

### 3. Rapid filter toggle state inconsistency
- **Test:** 22.5
- **Severity:** Very Low
- **Description:** Toggling all 8 feed filter buttons off then on rapidly didn't return to exact original state. The COMMAND filter (which starts inactive by default since it's not in the initial `feedTypeFilters` state) flipped. This is because COMMAND is not in the default state object in `state.ts` line 69 — the filter logic falls back to `state.feedTypeFilters[type] ?? true` which defaults to true for unknown types, but toggling creates an explicit `false` entry.
- **File:** `web/src/state.ts:69`, `web/src/components/feed-panel.ts:216-242`
- **Impact:** No real-world impact — users don't toggle all filters programmatically.

### 4. Compact card error badge not clickable
- **Test:** 2.14
- **Severity:** Low
- **Description:** The error count click-to-filter feature only works on expanded session cards (`.session-error-count` in `renderExpanded`), not on compact cards (`.compact-stat-err` in `renderCompact`). The compact card renders the error count as a plain span without a click handler.
- **File:** `web/src/components/session-card.ts:125-144` (expanded has handler), `232` (compact lacks handler)
- **Expected:** Both card variants should support clicking the error count to filter feed to errors.

## Design Notes (Not Bugs)

### Browser back/forward doesn't navigate between sessions
- **Test:** 21.2
- **Description:** `hash.ts` uses `history.replaceState` instead of `pushState`, so session switches replace the current history entry rather than creating new ones. Browser back goes to the previous page, not the previous session.
- **Rationale:** Intentional — a monitoring dashboard would create dozens of history entries from session browsing, making the back button useless for actual page navigation. The hash is designed for bookmarking/deep-linking, not navigation history.
- **Hash debounce (21.3):** Confirmed — 200ms debounce prevents history spam even if pushState were used.

### Sidebar uses compact card format only
- **Test:** 2.6
- **Description:** The sidebar renders all session cards using `renderCompact`, which shows cost, cost-rate, duration, model, errors, and timeAgo — but NOT tokens or cache%. The `renderExpanded` function exists in `session-card.ts` with those fields, but isn't used by the sidebar currently. Tokens and cache% are available in the history table view.
- **Rationale:** Compact cards keep the sidebar narrow and scannable. Full token/cache stats are available in history.

## Test Summary

- **Total test cases:** 242
- **Tested:** ~185
- **Passed:** ~181
- **Fixed:** 1 (help overlay `t` key)
- **Open issues:** 4 (all low/very-low severity)
- **Skipped:** ~57 (mostly error injection, canvas mouse interaction, and edge cases requiring specific server states)
