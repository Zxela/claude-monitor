# Contributing to claude-monitor

## Prerequisites

- **Go** >= 1.26.5 (see note below)
- **Node.js** >= 22
- **npm**
- **make**

> The `go` directive in `go.mod` is the single source of truth for the toolchain:
> CI passes it to `actions/setup-go` via `go-version-file`, which installs that
> **exact** version. Dependabot does not manage Go toolchain versions, so this
> floor only moves when someone bumps it — worth reviewing when a Go security
> release lands. It is currently pinned to 1.26.5, which carries the fix for
> GO-2026-5856.

## Development Setup

```bash
git clone https://github.com/Zxela/claude-monitor.git
cd claude-monitor
make install   # install frontend dependencies
make dev       # start Go backend + Vite dev server
```

## Dev Architecture

- **Go backend** serves on `:7700` (REST API, WebSocket, static files)
- **Vite dev server** runs on `:5173` and proxies API/WS requests to the Go backend
- Frontend has **HMR** (hot module replacement) via Vite; the Go backend does **not** hot-reload (restart manually after Go changes)

## Code Style

- **Go**: formatted with `gofmt`
- **TypeScript**: vanilla DOM, no framework. Run `make lint` for type-checking via `tsc`

## Testing

```bash
make test              # Go tests
make lint              # Go vet + TypeScript type-check
cd web && npm test     # frontend tests (vitest)
```

The frontend has tests — 45 of them across 7 files — and CI runs them. They are
not wired into `make test`, which covers Go only.

## Git Hooks

Hooks are managed by [lefthook](https://lefthook.dev) and are opt-in per clone:

```bash
lefthook install
```

That installs a `pre-push` hook running the full validation suite in parallel
(Go build/vet/test, `go mod tidy -diff`, frontend `tsc` and tests) — about nine
seconds. There is no pre-commit hook; CI is the backstop, and pre-push catches
problems before anything leaves your machine.

## Commit Convention

Use [conventional commits](https://www.conventionalcommits.org/):

- `feat:` new feature
- `fix:` bug fix
- `refactor:` code restructuring
- `docs:` documentation changes
- `test:` test additions/changes
- `chore:` build, CI, tooling

## Pull Requests

- Keep PRs focused on a single change
- CI must pass before merge
- Describe what changed and why in the PR description
