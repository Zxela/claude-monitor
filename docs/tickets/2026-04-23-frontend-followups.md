# Frontend Follow-up Tickets (2026-04-23)

This ticket bundle tracks four QA-driven improvements and assigns each one to a virtual agent track for implementation.

## Ticket CM-201 — Typo cleanup in QA plan
- **Type:** Docs typo
- **Scope:** `docs/frontend-qa-test-plan.md`
- **Issue:** "DEselects" should be "deselects".
- **Agent:** Agent-Docs-1
- **Status:** ✅ Done

## Ticket CM-202 — Global shortcut guard should include `<select>` / editable controls
- **Type:** Bug fix
- **Scope:** `web/src/main.ts`
- **Issue:** Global keydown shortcuts were suppressed for input/textarea only; shortcuts could fire while focused in repo filter `<select>`.
- **Agent:** Agent-Frontend-Keys
- **Status:** ✅ Done

## Ticket CM-203 — QA issue log mismatch for `t` shortcut fix
- **Type:** Documentation discrepancy
- **Scope:** `docs/qa-issues.md`, `docs/frontend-qa-test-plan.md`
- **Issue:** Historical note said `t` help entry was removed, but app behavior supports `t` table shortcut.
- **Agent:** Agent-Docs-2
- **Status:** ✅ Done

## Ticket CM-204 — Improve manual keyboard test coverage for editable controls
- **Type:** Test plan improvement
- **Scope:** `docs/frontend-qa-test-plan.md`
- **Issue:** Keyboard test case was focused on search input only and omitted select/other editable controls.
- **Agent:** Agent-QA-Plan
- **Status:** ✅ Done

## Agent Spin-up Summary
- Spawned 4 parallel work tracks (Docs-1, Frontend-Keys, Docs-2, QA-Plan).
- All tracks completed and merged in this changeset.
