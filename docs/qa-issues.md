# QA Issues — Frontend Testing

Tracking issues found during manual QA testing of all frontend features.

## Fixed

### 1. `t` keyboard shortcut missing handler
- **Test:** 12.7
- **Severity:** Low
- **Description:** Help overlay listed `t` as "Table view" shortcut but `main.ts` had no handler for `t`. Pressing `t` did nothing.
- **Fix:** Added `t` keyboard handler in `main.ts` to toggle table/list view, keeping help overlay and behavior aligned.
- **Status:** Fixed

## Open

None — all issues resolved.

## Fixed (batch 2)

### 2. Invalid hash stuck on "Loading..."
- **Test:** 13.4
- **Severity:** Low
- **Description:** Navigating to `#session=nonexistent-id` shows "Loading..." permanently. The API returns 200 with empty results, leaving the feed in a loaded state with no entries rendered.
- **Fix:** Added empty-result check after dedup/merge in `loadRecentMessages` — shows "No events found" when merged array is empty.
- **Status:** Fixed

### 3. Rapid filter toggle state inconsistency
- **Test:** 22.5
- **Severity:** Very Low
- **Description:** COMMAND filter flipped state on rapid toggle because it had no explicit entry in the default `feedTypeFilters` state, falling back to `?? true`.
- **Fix:** Added explicit `command: false` to the default `feedTypeFilters` in `state.ts`.
- **Status:** Fixed

### 4. Compact card error badge not clickable
- **Test:** 2.14
- **Severity:** Low
- **Description:** Error count click-to-filter only worked on expanded cards, not compact cards.
- **Fix:** Added click/keydown handler to `.compact-stat-err` in `renderCompact`, matching the expanded card pattern.
- **Status:** Fixed

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
- **Fixed:** 4 (help overlay `t` key, invalid hash loading, filter toggle, compact error badge)
- **Open issues:** 0
- **Skipped:** ~57 (mostly error injection, canvas mouse interaction, and edge cases requiring specific server states)
