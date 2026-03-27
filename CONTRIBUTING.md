# Contributing to claude-monitor

## Prerequisites

- **Go** >= 1.25
- **Node.js** >= 18
- **npm**
- **make**

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
make test    # Go tests
make lint    # Go vet + TypeScript type-check
```

There are no frontend tests yet.

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
