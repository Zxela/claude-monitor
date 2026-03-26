# Navigation Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix release pipeline, add backend APIs for time-grouped sessions, migrate the frontend to Vite + TypeScript modules, and redesign session navigation with time-grouped lists and a working search.

**Architecture:** Go backend gets 3 new endpoints (`/api/sessions/grouped`, `/api/projects`, `/api/version`) and search improvements. The 4,100-line vanilla JS `index.html` is migrated to a Vite + TypeScript project under `web/`, built to `cmd/claude-monitor/static/` for Go embedding. Session list switches from flat to two-tier (Active Now + Timeline). Search gets a command-palette dropdown with highlighted results.

**Tech Stack:** Go 1.25, Vite 6, TypeScript 5, vanilla DOM (no framework), CSS variables

---

## Phase 1: Release / Build Fixes

### Task 1: Add version variable and --version flag

**Files:**
- Modify: `cmd/claude-monitor/main.go:1-88`

- [ ] **Step 1: Write the failing test**

Create a test that verifies the version variable exists and the version flag works:

```go
// cmd/claude-monitor/main_test.go
package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	// Build the binary
	cmd := exec.Command("go", "build", "-o", "/tmp/claude-monitor-test", "./")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	// Run with --version
	out, err := exec.Command("/tmp/claude-monitor-test", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %s\n%s", err, out)
	}
	output := strings.TrimSpace(string(out))
	if !strings.HasPrefix(output, "claude-monitor ") {
		t.Errorf("expected output starting with 'claude-monitor ', got %q", output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/claude-monitor && go test -run TestVersionFlag -v`
Expected: FAIL — no `--version` flag defined

- [ ] **Step 3: Add version variable and flag handling**

In `cmd/claude-monitor/main.go`, add after the package imports (line 30, before the `//go:embed` directive):

```go
// version is set by -ldflags at build time.
var version = "dev"
```

In the `main()` function, add after `flag.Parse()` (after line 88):

```go
	// Handle --version before any other initialization.
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("claude-monitor %s\n", version)
		os.Exit(0)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/claude-monitor && go test -run TestVersionFlag -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/claude-monitor/main.go cmd/claude-monitor/main_test.go
git commit -m "feat: add version variable and --version flag"
```

### Task 2: Add /api/version endpoint

**Files:**
- Modify: `cmd/claude-monitor/main.go:625-629` (after health endpoint)

- [ ] **Step 1: Write the failing test**

```go
// Add to cmd/claude-monitor/main_test.go
func TestVersionEndpoint(t *testing.T) {
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version=v1.0.0-test", "-o", "/tmp/claude-monitor-test", "./")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	// Start server on a random port
	server := exec.Command("/tmp/claude-monitor-test", "-port", "17701")
	if err := server.Start(); err != nil {
		t.Fatalf("start failed: %s", err)
	}
	defer server.Process.Kill()

	// Wait for server to be ready
	time.Sleep(2 * time.Second)

	resp, err := http.Get("http://localhost:17701/api/version")
	if err != nil {
		t.Fatalf("GET /api/version failed: %s", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %s", err)
	}
	if result["version"] != "v1.0.0-test" {
		t.Errorf("version: got %q, want %q", result["version"], "v1.0.0-test")
	}
}
```

Add `"encoding/json"`, `"net/http"`, and `"time"` to the test file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/claude-monitor && go test -run TestVersionEndpoint -v -timeout 30s`
Expected: FAIL — 404 on /api/version

- [ ] **Step 3: Add the endpoint**

In `cmd/claude-monitor/main.go`, add after the health check handler (after line 629):

```go
	// Version endpoint.
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": version})
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/claude-monitor && go test -run TestVersionEndpoint -v -timeout 30s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/claude-monitor/main.go cmd/claude-monitor/main_test.go
git commit -m "feat: add /api/version endpoint"
```

### Task 3: Fix CI Go version and release builds

**Files:**
- Modify: `.github/workflows/ci.yml:17,35`
- Modify: `.github/workflows/release-please.yml:42`

- [ ] **Step 1: Update ci.yml to use go-version-file**

In `.github/workflows/ci.yml`, replace both occurrences of:
```yaml
          go-version: '1.22'
```
with:
```yaml
          go-version-file: 'go.mod'
```

- [ ] **Step 2: Update release-please.yml Go version and build commands**

In `.github/workflows/release-please.yml`, replace:
```yaml
          go-version: '1.22'
```
with:
```yaml
          go-version-file: 'go.mod'
```

Replace the build step (lines 47-56) with:
```yaml
      - name: Build binaries
        run: |
          VERSION=${{ needs.release-please.outputs.tag_name }}
          LDFLAGS="-s -w -X main.version=${VERSION}"
          mkdir -p dist

          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o dist/claude-monitor-linux-amd64 ./cmd/claude-monitor
          CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o dist/claude-monitor-linux-arm64 ./cmd/claude-monitor
          CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="${LDFLAGS}" -o dist/claude-monitor-darwin-amd64 ./cmd/claude-monitor
          CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="${LDFLAGS}" -o dist/claude-monitor-darwin-arm64 ./cmd/claude-monitor

          # Create compressed archives that preserve execute permissions
          cd dist
          for bin in claude-monitor-*; do
            chmod +x "$bin"
            tar czf "${bin}.tar.gz" "$bin"
          done
          sha256sum *.tar.gz > checksums.txt
```

Replace the upload step (lines 60-70) with:
```yaml
      - name: Upload binaries to release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh release upload ${{ needs.release-please.outputs.tag_name }} \
            dist/*.tar.gz \
            dist/checksums.txt \
            --repo ${{ github.repository }}
```

- [ ] **Step 3: Verify YAML is valid**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); yaml.safe_load(open('.github/workflows/release-please.yml')); print('YAML valid')"` or if python3-yaml is not available: `cat .github/workflows/ci.yml | head -1 && cat .github/workflows/release-please.yml | head -1 && echo "Files exist and readable"`

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml .github/workflows/release-please.yml
git commit -m "fix: use go-version-file, add CGO_ENABLED=0, compress release assets"
```

### Task 4: Create install script

**Files:**
- Create: `install.sh`

- [ ] **Step 1: Write the install script**

```bash
#!/bin/sh
set -e

REPO="Zxela/claude-monitor"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) OS="darwin" ;;
  linux)  OS="linux" ;;
  *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64)  ARCH="arm64" ;;
  *)              echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY="claude-monitor-${OS}-${ARCH}"

# Get latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$TAG" ]; then
  echo "Error: could not determine latest release"
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${BINARY}.tar.gz"

# Download and extract
echo "Downloading claude-monitor ${TAG} (${OS}/${ARCH})..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "${TMPDIR}/${BINARY}.tar.gz"
tar xzf "${TMPDIR}/${BINARY}.tar.gz" -C "$TMPDIR"

# Install
mkdir -p "$INSTALL_DIR"
mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/claude-monitor"
chmod +x "${INSTALL_DIR}/claude-monitor"

# macOS: remove quarantine attribute
if [ "$OS" = "darwin" ]; then
  xattr -cr "${INSTALL_DIR}/claude-monitor" 2>/dev/null || true
fi

echo ""
echo "claude-monitor ${TAG} installed to ${INSTALL_DIR}/claude-monitor"

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "NOTE: ${INSTALL_DIR} is not in your PATH."
    echo "Add it with:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo ""
    echo "Or add that line to your ~/.bashrc or ~/.zshrc"
    ;;
esac

echo ""
echo "Run:  claude-monitor"
echo "Open: http://localhost:7700"
```

- [ ] **Step 2: Make it executable and verify syntax**

Run: `chmod +x install.sh && bash -n install.sh && echo "Syntax OK"`
Expected: "Syntax OK"

- [ ] **Step 3: Commit**

```bash
git add install.sh
git commit -m "feat: add install script with macOS quarantine fix"
```

---

## Phase 2: Backend API Changes

### Task 5: Add /api/sessions/grouped endpoint

**Files:**
- Modify: `cmd/claude-monitor/main.go:417-424` (after existing /api/sessions)

- [ ] **Step 1: Write the failing test**

```go
// Add to cmd/claude-monitor/main_test.go
func TestGroupedSessionsEndpoint(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", "/tmp/claude-monitor-test", "./")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	server := exec.Command("/tmp/claude-monitor-test", "-port", "17702")
	if err := server.Start(); err != nil {
		t.Fatalf("start failed: %s", err)
	}
	defer server.Process.Kill()
	time.Sleep(2 * time.Second)

	resp, err := http.Get("http://localhost:17702/api/sessions/grouped")
	if err != nil {
		t.Fatalf("GET failed: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var result map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %s", err)
	}

	// Verify all expected buckets exist
	for _, key := range []string{"active", "lastHour", "today", "yesterday", "thisWeek", "older"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing bucket: %s", key)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/claude-monitor && go test -run TestGroupedSessionsEndpoint -v -timeout 30s`
Expected: FAIL — 404 on /api/sessions/grouped

- [ ] **Step 3: Implement the endpoint**

In `cmd/claude-monitor/main.go`, add after the `/api/sessions` handler (after line 424):

```go
	// Time-bucketed sessions for the new navigation UI.
	mux.HandleFunc("/api/sessions/grouped", func(w http.ResponseWriter, r *http.Request) {
		sessions := sessionStore.All()
		now := time.Now()
		hourAgo := now.Add(-1 * time.Hour)
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		yesterdayStart := todayStart.Add(-24 * time.Hour)
		weekStart := todayStart.Add(-time.Duration(now.Weekday()) * 24 * time.Hour)

		type grouped struct {
			Active   []*session.Session `json:"active"`
			LastHour []*session.Session `json:"lastHour"`
			Today    []*session.Session `json:"today"`
			Yesterday []*session.Session `json:"yesterday"`
			ThisWeek []*session.Session `json:"thisWeek"`
			Older    []*session.Session `json:"older"`
		}
		g := grouped{
			Active:    []*session.Session{},
			LastHour:  []*session.Session{},
			Today:     []*session.Session{},
			Yesterday: []*session.Session{},
			ThisWeek:  []*session.Session{},
			Older:     []*session.Session{},
		}

		for _, s := range sessions {
			if s.IsActive {
				g.Active = append(g.Active, s)
				continue
			}
			la := s.LastActive
			switch {
			case la.After(hourAgo):
				g.LastHour = append(g.LastHour, s)
			case la.After(todayStart):
				g.Today = append(g.Today, s)
			case la.After(yesterdayStart):
				g.Yesterday = append(g.Yesterday, s)
			case la.After(weekStart):
				g.ThisWeek = append(g.ThisWeek, s)
			default:
				g.Older = append(g.Older, s)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(g)
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/claude-monitor && go test -run TestGroupedSessionsEndpoint -v -timeout 30s`
Expected: PASS

- [ ] **Step 5: Run all existing tests to check for regressions**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/claude-monitor/main.go cmd/claude-monitor/main_test.go
git commit -m "feat: add /api/sessions/grouped endpoint with time buckets"
```

### Task 6: Add /api/projects endpoint

**Files:**
- Modify: `cmd/claude-monitor/main.go` (after /api/sessions/grouped)

- [ ] **Step 1: Write the failing test**

```go
// Add to cmd/claude-monitor/main_test.go
func TestProjectsEndpoint(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", "/tmp/claude-monitor-test", "./")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	server := exec.Command("/tmp/claude-monitor-test", "-port", "17703")
	if err := server.Start(); err != nil {
		t.Fatalf("start failed: %s", err)
	}
	defer server.Process.Kill()
	time.Sleep(2 * time.Second)

	resp, err := http.Get("http://localhost:17703/api/projects")
	if err != nil {
		t.Fatalf("GET failed: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %s", err)
	}
	// With no sessions, should return empty array (not null)
	if result == nil {
		t.Error("expected empty array, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/claude-monitor && go test -run TestProjectsEndpoint -v -timeout 30s`
Expected: FAIL — 404 on /api/projects

- [ ] **Step 3: Implement the endpoint**

In `cmd/claude-monitor/main.go`, add after the `/api/sessions/grouped` handler:

```go
	// Distinct project names with session counts.
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		sessions := sessionStore.All()
		counts := make(map[string]int)
		for _, s := range sessions {
			name := s.ProjectName
			if name == "" {
				name = s.ProjectDir
			}
			counts[name]++
		}

		type projectEntry struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		result := make([]projectEntry, 0, len(counts))
		for name, count := range counts {
			result = append(result, projectEntry{Name: name, Count: count})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/claude-monitor && go test -run TestProjectsEndpoint -v -timeout 30s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/claude-monitor/main.go cmd/claude-monitor/main_test.go
git commit -m "feat: add /api/projects endpoint"
```

### Task 7: Improve search API with projectName field

**Files:**
- Modify: `cmd/claude-monitor/main.go:442-481` (search handler)

The search handler already includes `sessionName` but is missing `projectName`. The spec also wants results grouped by sessionId.

- [ ] **Step 1: Update the searchResult struct**

In `cmd/claude-monitor/main.go`, find the `searchResult` struct inside the search handler (around line 442) and add `ProjectName`:

```go
		type searchResult struct {
			SessionID   string `json:"sessionId"`
			SessionName string `json:"sessionName"`
			ProjectName string `json:"projectName"`
			parser.ParsedMessage
		}
```

- [ ] **Step 2: Update the result construction to include ProjectName**

In the loop where results are appended (around line 467), update:

```go
					results = append(results, searchResult{
						SessionID:     sess.ID,
						SessionName:   displayName,
						ProjectName:   sess.ProjectName,
						ParsedMessage: ev.ParsedMessage,
					})
```

- [ ] **Step 3: Run all tests**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/claude-monitor/main.go
git commit -m "feat: add projectName to search results"
```

---

## Phase 3: Frontend Scaffolding (Vite + TypeScript)

### Task 8: Initialize Vite project

**Files:**
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/vite.config.ts`
- Create: `web/index.html`
- Create: `web/src/main.ts`
- Modify: `.gitignore`

- [ ] **Step 1: Create package.json**

```json
{
  "name": "claude-monitor-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview"
  },
  "devDependencies": {
    "typescript": "~5.7.0",
    "vite": "~6.0.0"
  }
}
```

- [ ] **Step 2: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "isolatedModules": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "outDir": "./dist",
    "rootDir": "./src"
  },
  "include": ["src"]
}
```

- [ ] **Step 3: Create vite.config.ts**

```typescript
import { defineConfig } from 'vite';
import path from 'path';

export default defineConfig({
  root: '.',
  build: {
    outDir: path.resolve(__dirname, '../cmd/claude-monitor/static'),
    emptyDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:7700',
      '/ws': {
        target: 'ws://localhost:7700',
        ws: true,
      },
      '/health': 'http://localhost:7700',
    },
  },
});
```

- [ ] **Step 4: Create shell index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Claude Monitor</title>
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/main.ts"></script>
</body>
</html>
```

- [ ] **Step 5: Create minimal main.ts**

```typescript
// web/src/main.ts
import './styles/base.css';

const app = document.getElementById('app')!;
app.innerHTML = '<h1>Claude Monitor</h1><p>Vite + TypeScript scaffold working.</p>';

console.log('claude-monitor web: loaded');
```

- [ ] **Step 6: Create base.css with CSS variables from the existing theme**

```css
/* web/src/styles/base.css */
:root {
  --bg: #0d1117;
  --bg-card: #161b22;
  --bg-hover: #1c2333;
  --border: #30363d;
  --text: #c9d1d9;
  --text-dim: #8b949e;
  --green: #3fb950;
  --yellow: #d29922;
  --red: #f85149;
  --cyan: #58a6ff;
  --purple: #bc8cff;
  --orange: #f0883e;
  --font-mono: 'JetBrains Mono', 'Consolas', 'Monaco', monospace;
  --font-sans: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
}

* { margin: 0; padding: 0; box-sizing: border-box; }

body {
  font-family: var(--font-mono);
  background: var(--bg);
  color: var(--text);
  overflow: hidden;
  height: 100vh;
}

#app {
  height: 100vh;
  display: flex;
  flex-direction: column;
}
```

- [ ] **Step 7: Update .gitignore**

Add to `.gitignore`:

```
web/node_modules/
web/dist/
```

- [ ] **Step 8: Install dependencies and verify build**

Run:
```bash
cd web && npm install && npm run build
```
Expected: Build succeeds, output in `cmd/claude-monitor/static/`

- [ ] **Step 9: Verify Go embed still works**

Run:
```bash
cd /root/claude-monitor && go build -o /tmp/cm-test ./cmd/claude-monitor
```
Expected: Build succeeds

- [ ] **Step 10: Commit**

```bash
git add web/ .gitignore
git commit -m "feat: initialize Vite + TypeScript frontend scaffold"
```

### Task 9: Create TypeScript types and API client

**Files:**
- Create: `web/src/types.ts`
- Create: `web/src/api.ts`

- [ ] **Step 1: Create types.ts**

These types mirror the Go structs in `internal/session/session.go` and `internal/store/sqlite.go`:

```typescript
// web/src/types.ts

export interface Session {
  id: string;
  projectDir: string;
  projectName: string;
  sessionName?: string;
  filePath: string;
  totalCostUSD: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  cacheHitPct: number;
  messageCount: number;
  lastActive: string; // ISO 8601
  isActive: boolean;
  startedAt: string;
  status: 'idle' | 'thinking' | 'tool_use' | 'waiting';
  parentId?: string;
  children?: string[];
  cwd?: string;
  gitBranch?: string;
  model?: string;
  costRate: number;
  errorCount: number;
  isSubagent?: boolean;
  taskDescription: string;
}

export interface GroupedSessions {
  active: Session[];
  lastHour: Session[];
  today: Session[];
  yesterday: Session[];
  thisWeek: Session[];
  older: Session[];
}

export interface ProjectEntry {
  name: string;
  count: number;
}

export interface SearchResult {
  sessionId: string;
  sessionName: string;
  projectName: string;
  type: string;
  role: string;
  contentText: string;
  toolName?: string;
  timestamp: string;
  messageId?: string;
  costUSD: number;
  isError: boolean;
}

export interface HistoryRow {
  id: string;
  projectName: string;
  sessionName: string;
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  messageCount: number;
  errorCount: number;
  startedAt: string;
  endedAt: string;
  durationSeconds: number;
  model: string;
  cwd: string;
  gitBranch: string;
  taskDescription: string;
}

export interface WsEvent {
  event: 'session_new' | 'message';
  session: Session;
  message?: ParsedMessage;
}

export interface ParsedMessage {
  type: string;
  role: string;
  contentText: string;
  toolName?: string;
  toolDetail?: string;
  timestamp: string;
  messageId?: string;
  costUSD: number;
  isError: boolean;
  model?: string;
  hookEvent?: string;
}
```

- [ ] **Step 2: Create api.ts**

```typescript
// web/src/api.ts
import type { GroupedSessions, ProjectEntry, SearchResult, Session, HistoryRow } from './types';

const BASE = '';

export async function fetchSessions(): Promise<Session[]> {
  const res = await fetch(`${BASE}/api/sessions`);
  return res.json();
}

export async function fetchGroupedSessions(): Promise<GroupedSessions> {
  const res = await fetch(`${BASE}/api/sessions/grouped`);
  return res.json();
}

export async function fetchProjects(): Promise<ProjectEntry[]> {
  const res = await fetch(`${BASE}/api/projects`);
  return res.json();
}

export async function fetchSearch(query: string, limit = 50): Promise<SearchResult[]> {
  const res = await fetch(`${BASE}/api/search?q=${encodeURIComponent(query)}&limit=${limit}`);
  return res.json();
}

export async function fetchHistory(limit = 50, offset = 0): Promise<HistoryRow[]> {
  const res = await fetch(`${BASE}/api/history?limit=${limit}&offset=${offset}`);
  return res.json();
}

export async function fetchRecentMessages(sessionId: string): Promise<unknown[]> {
  const res = await fetch(`${BASE}/api/sessions/${sessionId}/recent`);
  return res.json();
}

export async function fetchVersion(): Promise<string> {
  const res = await fetch(`${BASE}/api/version`);
  const data = await res.json();
  return data.version;
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/types.ts web/src/api.ts
git commit -m "feat: add TypeScript types and API client"
```

### Task 10: Create state management module

**Files:**
- Create: `web/src/state.ts`

- [ ] **Step 1: Create state.ts**

```typescript
// web/src/state.ts
import type { Session, GroupedSessions, SearchResult, ProjectEntry } from './types';

export interface AppState {
  // Sessions
  sessions: Map<string, Session>;
  grouped: GroupedSessions | null;
  projects: ProjectEntry[];

  // UI state
  selectedSessionId: string | null;
  view: 'list' | 'graph' | 'history' | 'table';
  projectFilter: string | null;

  // Search
  searchQuery: string;
  searchResults: SearchResult[];
  searchLoading: boolean;
  searchOpen: boolean;

  // Connection
  connected: boolean;
  eventCount: number;

  // Version
  version: string;
}

type Listener = (state: AppState, changedKeys: Set<string>) => void;

const listeners: Listener[] = [];

export const state: AppState = {
  sessions: new Map(),
  grouped: null,
  projects: [],
  selectedSessionId: null,
  view: 'list',
  projectFilter: null,
  searchQuery: '',
  searchResults: [],
  searchLoading: false,
  searchOpen: false,
  connected: false,
  eventCount: 0,
  version: '',
};

export function subscribe(listener: Listener): () => void {
  listeners.push(listener);
  return () => {
    const idx = listeners.indexOf(listener);
    if (idx >= 0) listeners.splice(idx, 1);
  };
}

export function update(changes: Partial<AppState>): void {
  const changedKeys = new Set<string>();
  for (const [key, value] of Object.entries(changes)) {
    if ((state as Record<string, unknown>)[key] !== value) {
      (state as Record<string, unknown>)[key] = value;
      changedKeys.add(key);
    }
  }
  if (changedKeys.size > 0) {
    for (const listener of listeners) {
      listener(state, changedKeys);
    }
  }
}

export function updateSession(session: Session): void {
  state.sessions.set(session.id, session);
  // Notify with 'sessions' as changed key
  for (const listener of listeners) {
    listener(state, new Set(['sessions']));
  }
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/state.ts
git commit -m "feat: add centralized state management module"
```

### Task 11: Create WebSocket client

**Files:**
- Create: `web/src/ws.ts`

- [ ] **Step 1: Create ws.ts**

```typescript
// web/src/ws.ts
import type { WsEvent } from './types';
import { update, updateSession } from './state';

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

type MessageHandler = (event: WsEvent) => void;
const handlers: MessageHandler[] = [];

export function onMessage(handler: MessageHandler): void {
  handlers.push(handler);
}

export function connect(): void {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${protocol}//${location.host}/ws`;

  ws = new WebSocket(url);

  ws.onopen = () => {
    update({ connected: true });
    console.log('WebSocket connected');
  };

  ws.onclose = () => {
    update({ connected: false });
    ws = null;
    // Auto-reconnect after 2 seconds
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 2000);
  };

  ws.onerror = () => {
    ws?.close();
  };

  ws.onmessage = (e) => {
    const event: WsEvent = JSON.parse(e.data);
    update({ eventCount: (import('./state').then(m => m.state.eventCount) as unknown as number) + 1 });

    // Update session in state
    if (event.session) {
      updateSession(event.session);
    }

    // Notify handlers
    for (const handler of handlers) {
      handler(event);
    }
  };
}
```

Wait — the `eventCount` increment above is wrong (async import in sync context). Fix:

```typescript
// web/src/ws.ts
import type { WsEvent } from './types';
import { state, update, updateSession } from './state';

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

type MessageHandler = (event: WsEvent) => void;
const handlers: MessageHandler[] = [];

export function onMessage(handler: MessageHandler): void {
  handlers.push(handler);
}

export function connect(): void {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${protocol}//${location.host}/ws`;

  ws = new WebSocket(url);

  ws.onopen = () => {
    update({ connected: true });
  };

  ws.onclose = () => {
    update({ connected: false });
    ws = null;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 2000);
  };

  ws.onerror = () => {
    ws?.close();
  };

  ws.onmessage = (e) => {
    const event: WsEvent = JSON.parse(e.data);
    update({ eventCount: state.eventCount + 1 });

    if (event.session) {
      updateSession(event.session);
    }

    for (const handler of handlers) {
      handler(event);
    }
  };
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/ws.ts
git commit -m "feat: add WebSocket client with auto-reconnect"
```

---

## Phase 4: Core UI Components

### Task 12: Create top bar component

**Files:**
- Create: `web/src/components/topbar.ts`
- Create: `web/src/styles/topbar.css`

- [ ] **Step 1: Create topbar.css**

```css
/* web/src/styles/topbar.css */
.topbar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 8px 16px;
  background: var(--bg-card);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  overflow-x: auto;
}

.topbar-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  color: var(--cyan);
  white-space: nowrap;
}

.brand-diamond {
  color: var(--green);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.topbar-stat {
  display: flex;
  flex-direction: column;
  align-items: center;
  font-size: 11px;
  color: var(--text-dim);
  white-space: nowrap;
}

.topbar-stat .val {
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
}

.topbar-stat .val.green { color: var(--green); }
.topbar-stat .val.yellow { color: var(--yellow); }
.topbar-stat .val.cyan { color: var(--cyan); }

.search-box {
  margin-left: auto;
  position: relative;
}

.search-box input {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 13px;
  padding: 6px 12px;
  width: 240px;
  outline: none;
}

.search-box input:focus {
  border-color: var(--cyan);
}

.view-toggle {
  display: flex;
  gap: 2px;
}

.view-btn {
  background: none;
  border: 1px solid var(--border);
  color: var(--text-dim);
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 4px 10px;
  cursor: pointer;
}

.view-btn:hover { color: var(--text); }
.view-btn.active {
  background: var(--border);
  color: var(--text);
}
```

- [ ] **Step 2: Create topbar.ts**

```typescript
// web/src/components/topbar.ts
import { state, subscribe, update } from '../state';
import '../styles/topbar.css';

let el: HTMLElement | null = null;
let searchInput: HTMLInputElement | null = null;

// Stat element references for fast updates
let statActive: HTMLElement | null = null;
let statCost: HTMLElement | null = null;
let statWorking: HTMLElement | null = null;
let statCache: HTMLElement | null = null;
let statRate: HTMLElement | null = null;

export function render(container: HTMLElement): void {
  el = document.createElement('div');
  el.className = 'topbar';
  el.innerHTML = `
    <div class="topbar-brand">
      <span class="brand-diamond">◆</span>
      CLAUDE MONITOR
    </div>
    <div class="topbar-stat">ACTIVE <span class="val green" data-stat="active">0</span></div>
    <div class="topbar-stat">TOTAL SPEND <span class="val yellow" data-stat="cost">$0</span></div>
    <div class="topbar-stat">WORKING <span class="val green" data-stat="working">0</span></div>
    <div class="topbar-stat">CACHE HIT <span class="val cyan" data-stat="cache">0%</span></div>
    <div class="topbar-stat">$/MIN <span class="val yellow" data-stat="rate">$0/m</span></div>
    <div class="search-box">
      <input type="text" placeholder="Search all sessions..." data-search />
    </div>
    <div class="view-toggle">
      <button class="view-btn active" data-view="list">LIST</button>
      <button class="view-btn" data-view="graph">GRAPH</button>
      <button class="view-btn" data-view="history">HISTORY</button>
      <button class="view-btn" data-view="table">TABLE</button>
    </div>
  `;
  container.appendChild(el);

  // Cache references
  statActive = el.querySelector('[data-stat="active"]');
  statCost = el.querySelector('[data-stat="cost"]');
  statWorking = el.querySelector('[data-stat="working"]');
  statCache = el.querySelector('[data-stat="cache"]');
  statRate = el.querySelector('[data-stat="rate"]');
  searchInput = el.querySelector('[data-search]');

  // View toggle buttons
  el.querySelectorAll<HTMLButtonElement>('.view-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const view = btn.dataset.view as AppState['view'];
      update({ view });
    });
  });

  // Search input
  searchInput!.addEventListener('input', () => {
    update({ searchQuery: searchInput!.value, searchOpen: searchInput!.value.length > 0 });
  });
  searchInput!.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      searchInput!.value = '';
      searchInput!.blur();
      update({ searchQuery: '', searchOpen: false, searchResults: [] });
    }
  });

  // Keyboard shortcut: / to focus search
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && document.activeElement !== searchInput) {
      e.preventDefault();
      searchInput!.focus();
    }
  });

  subscribe(onStateChange);
}

function onStateChange(_state: typeof state, changed: Set<string>): void {
  if (changed.has('sessions')) {
    updateStats();
  }
  if (changed.has('view')) {
    el?.querySelectorAll<HTMLButtonElement>('.view-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.view === state.view);
    });
  }
}

function updateStats(): void {
  const sessions = Array.from(state.sessions.values());
  const active = sessions.filter(s => s.isActive);
  const working = active.filter(s => s.status === 'thinking' || s.status === 'tool_use');
  const totalCost = sessions.reduce((sum, s) => sum + s.totalCostUSD, 0);
  const totalRate = active.reduce((sum, s) => sum + s.costRate, 0);

  // Weighted cache hit %
  const totalInput = sessions.reduce((sum, s) => sum + s.inputTokens + s.cacheReadTokens + s.cacheCreationTokens, 0);
  const totalCacheRead = sessions.reduce((sum, s) => sum + s.cacheReadTokens, 0);
  const cacheHit = totalInput > 0 ? (totalCacheRead / totalInput * 100) : 0;

  if (statActive) statActive.textContent = String(active.length);
  if (statCost) statCost.textContent = `$${totalCost.toFixed(0)}`;
  if (statWorking) statWorking.textContent = String(working.length);
  if (statCache) statCache.textContent = `${cacheHit.toFixed(0)}%`;
  if (statRate) statRate.textContent = `$${totalRate.toFixed(3)}/m`;
}

type AppState = typeof state;

export function focusSearch(): void {
  searchInput?.focus();
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/topbar.ts web/src/styles/topbar.css
git commit -m "feat: add top bar component with stats and search"
```

### Task 13: Create session card component

**Files:**
- Create: `web/src/components/session-card.ts`
- Create: `web/src/styles/sessions.css`

- [ ] **Step 1: Create sessions.css**

```css
/* web/src/styles/sessions.css */
.sessions-panel {
  width: 280px;
  min-width: 280px;
  border-right: 1px solid var(--border);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.active-section {
  border-bottom: 2px solid var(--green);
  padding-bottom: 4px;
}

.active-section-header {
  padding: 8px 12px 4px;
  font-size: 11px;
  font-weight: 700;
  color: var(--green);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.time-group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px 4px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-dim);
  cursor: pointer;
  user-select: none;
}

.time-group-header:hover { color: var(--text); }

.time-group-count {
  background: var(--border);
  border-radius: 8px;
  padding: 1px 6px;
  font-size: 10px;
}

.time-group-collapsed .time-group-items { display: none; }

.show-all-btn {
  padding: 4px 12px;
  font-size: 11px;
  color: var(--cyan);
  cursor: pointer;
  background: none;
  border: none;
  font-family: var(--font-mono);
}

/* Expanded card (active sessions) */
.session-card {
  padding: 8px 12px;
  border-left: 3px solid transparent;
  cursor: pointer;
  transition: background 0.15s;
}

.session-card:hover { background: var(--bg-hover); }
.session-card.selected { background: var(--bg-hover); border-left-color: var(--cyan); }
.session-card.pulse { border-left-color: var(--green); }

.session-card .session-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-card .session-task {
  font-size: 11px;
  color: var(--text-dim);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-card .session-stats {
  display: flex;
  gap: 8px;
  margin-top: 4px;
  font-size: 11px;
  color: var(--text-dim);
}

.session-card .session-stats .cost { color: var(--yellow); }
.session-card .session-stats .cache { color: var(--cyan); }
.session-card .session-stats .err { color: var(--red); }

.status-badge {
  display: inline-block;
  font-size: 9px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 3px;
  text-transform: uppercase;
  vertical-align: middle;
  margin-left: 6px;
}

.status-thinking { background: #1a3a5c; color: var(--cyan); }
.status-tool_use { background: #2a1a3a; color: var(--purple); }
.status-waiting { background: #3a2a1a; color: var(--orange); }
.status-idle { background: #1a2a1a; color: var(--green); }

/* Compact card (timeline sessions) */
.session-card-compact {
  padding: 4px 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}

.session-card-compact:hover { background: var(--bg-hover); }
.session-card-compact.selected { background: var(--bg-hover); border-left: 3px solid var(--cyan); }

.session-card-compact .session-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-card-compact .cost { color: var(--yellow); font-size: 11px; }
.session-card-compact .duration { color: var(--text-dim); font-size: 11px; }
.session-card-compact .model {
  font-size: 9px;
  color: var(--text-dim);
  background: var(--border);
  padding: 1px 4px;
  border-radius: 2px;
}

.project-filter {
  padding: 4px 12px;
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.filter-pill {
  font-size: 10px;
  padding: 2px 8px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: none;
  color: var(--text-dim);
  cursor: pointer;
  font-family: var(--font-mono);
}

.filter-pill:hover { color: var(--text); border-color: var(--text-dim); }
.filter-pill.active { background: var(--border); color: var(--text); }
```

- [ ] **Step 2: Create session-card.ts**

```typescript
// web/src/components/session-card.ts
import type { Session } from '../types';
import { state, update } from '../state';

export function renderExpanded(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) {
    el.classList.add('selected');
  }
  if (session.status === 'thinking' || session.status === 'tool_use') {
    el.classList.add('pulse');
  }

  const displayName = session.sessionName || session.projectName || session.id;
  const statusClass = `status-${session.status}`;
  const duration = formatDuration(session.startedAt, session.lastActive);

  el.innerHTML = `
    <div>
      <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
      <span class="status-badge ${statusClass}">${session.status.toUpperCase()}</span>
    </div>
    <div class="session-task" title="${escapeAttr(session.taskDescription)}">${escapeHtml(session.taskDescription || '')}</div>
    <div class="session-stats">
      <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
      ${session.costRate > 0 ? `<span class="cost">$${session.costRate.toFixed(3)}/min</span>` : ''}
      <span>${formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens)} tok</span>
      <span class="cache">${session.cacheHitPct.toFixed(0)}%</span>
      ${session.errorCount > 0 ? `<span class="err">${session.errorCount} err</span>` : ''}
    </div>
    <div class="session-stats">
      <span>${session.model || ''}</span>
      <span>${duration}</span>
    </div>
  `;

  el.addEventListener('click', () => {
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  });

  container.appendChild(el);
  return el;
}

export function renderCompact(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card-compact';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) {
    el.classList.add('selected');
  }

  const displayName = session.sessionName || session.projectName || session.id;
  const duration = formatDuration(session.startedAt, session.lastActive);

  el.innerHTML = `
    <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
    <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
    <span class="duration">${duration}</span>
    ${session.model ? `<span class="model">${session.model.replace('claude-', '').replace('-4-6', '')}</span>` : ''}
  `;

  el.addEventListener('click', () => {
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  });

  container.appendChild(el);
  return el;
}

function formatDuration(startedAt: string, lastActive: string): string {
  if (!startedAt) return '';
  const start = new Date(startedAt).getTime();
  const end = lastActive ? new Date(lastActive).getTime() : Date.now();
  const secs = Math.floor((end - start) / 1000);
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  return `${h}h${m}m`;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

function escapeHtml(s: string): string {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

function escapeAttr(s: string): string {
  return s.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/session-card.ts web/src/styles/sessions.css
git commit -m "feat: add session card component (expanded + compact variants)"
```

### Task 14: Create session list component with time grouping

**Files:**
- Create: `web/src/components/session-list.ts`

- [ ] **Step 1: Create session-list.ts**

```typescript
// web/src/components/session-list.ts
import type { GroupedSessions, Session } from '../types';
import { state, subscribe } from '../state';
import { fetchGroupedSessions } from '../api';
import { renderExpanded, renderCompact } from './session-card';
import '../styles/sessions.css';

let el: HTMLElement | null = null;
const MAX_VISIBLE = 15;
const expandedGroups = new Set<string>(); // tracks "show all" state

export function render(container: HTMLElement): void {
  el = document.createElement('div');
  el.className = 'sessions-panel';
  container.appendChild(el);

  // Initial fetch
  refresh();

  // Refresh every 5 seconds for time bucket re-sorting
  setInterval(refresh, 5000);

  subscribe(onStateChange);
}

async function refresh(): Promise<void> {
  try {
    const grouped = await fetchGroupedSessions();
    renderGrouped(grouped);
  } catch (err) {
    console.error('Failed to fetch grouped sessions:', err);
  }
}

function onStateChange(_state: typeof state, changed: Set<string>): void {
  if (changed.has('selectedSessionId') || changed.has('sessions') || changed.has('projectFilter')) {
    // Re-render with current data; sessions may have updated via WebSocket
    refresh();
  }
}

function renderGrouped(grouped: GroupedSessions): void {
  if (!el) return;
  el.innerHTML = '';

  const filter = state.projectFilter;

  // Active Now section
  const activeSessions = applyFilter(grouped.active, filter);
  if (activeSessions.length > 0) {
    const header = document.createElement('div');
    header.className = 'active-section-header';
    header.textContent = `ACTIVE NOW (${activeSessions.length})`;
    el.appendChild(header);

    const section = document.createElement('div');
    section.className = 'active-section';
    sortByLastActive(activeSessions);
    for (const sess of activeSessions) {
      renderExpanded(sess, section);
    }
    el.appendChild(section);
  }

  // Timeline groups
  const groups: [string, string, Session[]][] = [
    ['lastHour', 'Last hour', grouped.lastHour],
    ['today', 'Today', grouped.today],
    ['yesterday', 'Yesterday', grouped.yesterday],
    ['thisWeek', 'This week', grouped.thisWeek],
    ['older', 'Older', grouped.older],
  ];

  for (const [key, label, sessions] of groups) {
    const filtered = applyFilter(sessions, filter);
    if (filtered.length === 0) continue;

    sortByLastActive(filtered);

    const group = document.createElement('div');
    const isCollapsed = filtered.length > MAX_VISIBLE && !expandedGroups.has(key);

    const header = document.createElement('div');
    header.className = 'time-group-header';
    header.innerHTML = `
      <span>${label}</span>
      <span class="time-group-count">${filtered.length}</span>
    `;
    header.addEventListener('click', () => {
      group.classList.toggle('time-group-collapsed');
    });
    group.appendChild(header);

    const items = document.createElement('div');
    items.className = 'time-group-items';
    const visibleSessions = isCollapsed ? filtered.slice(0, MAX_VISIBLE) : filtered;
    for (const sess of visibleSessions) {
      renderCompact(sess, items);
    }

    if (isCollapsed && filtered.length > MAX_VISIBLE) {
      const showAll = document.createElement('button');
      showAll.className = 'show-all-btn';
      showAll.textContent = `Show all ${filtered.length} sessions`;
      showAll.addEventListener('click', (e) => {
        e.stopPropagation();
        expandedGroups.add(key);
        refresh();
      });
      items.appendChild(showAll);
    }

    group.appendChild(items);
    el.appendChild(group);
  }
}

function applyFilter(sessions: Session[], projectFilter: string | null): Session[] {
  if (!projectFilter) return sessions;
  return sessions.filter(s => s.projectName === projectFilter || s.sessionName === projectFilter);
}

function sortByLastActive(sessions: Session[]): void {
  sessions.sort((a, b) => new Date(b.lastActive).getTime() - new Date(a.lastActive).getTime());
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/session-list.ts
git commit -m "feat: add time-grouped session list component"
```

### Task 15: Create search component

**Files:**
- Create: `web/src/components/search.ts`
- Create: `web/src/styles/feed.css` (includes search dropdown styles)

- [ ] **Step 1: Create feed.css with search dropdown styles**

```css
/* web/src/styles/feed.css */
.search-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  max-height: 60vh;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-top: none;
  border-radius: 0 0 6px 6px;
  z-index: 100;
  box-shadow: 0 8px 24px rgba(0,0,0,0.4);
}

.search-dropdown-hidden { display: none; }

.search-status {
  padding: 12px 16px;
  font-size: 12px;
  color: var(--text-dim);
}

.search-result {
  padding: 8px 16px;
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  transition: background 0.1s;
}

.search-result:hover { background: var(--bg-hover); }

.search-result-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.search-result-session {
  font-size: 12px;
  font-weight: 600;
  color: var(--text);
}

.search-result-project {
  font-size: 11px;
  color: var(--text-dim);
}

.search-result-time {
  font-size: 10px;
  color: var(--text-dim);
  margin-left: auto;
}

.search-result-body {
  font-size: 12px;
  color: var(--text-dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.search-result-body mark {
  background: rgba(210, 153, 34, 0.3);
  color: var(--yellow);
  border-radius: 2px;
  padding: 0 2px;
}

.search-type-badge {
  font-size: 9px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 3px;
  text-transform: uppercase;
}

.search-type-badge.user { background: #1a3a1a; color: var(--green); }
.search-type-badge.assistant { background: #1a1a3a; color: var(--cyan); }
.search-type-badge.tool { background: #2a1a3a; color: var(--purple); }
.search-type-badge.error { background: #3a1a1a; color: var(--red); }

.search-group-more {
  padding: 4px 16px 8px;
  font-size: 11px;
  color: var(--cyan);
  cursor: pointer;
}

/* Feed panel styles */
.feed-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.feed-header {
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.feed-content {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.feed-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-dim);
  font-size: 13px;
}
```

- [ ] **Step 2: Create search.ts**

```typescript
// web/src/components/search.ts
import type { SearchResult } from '../types';
import { state, subscribe, update } from '../state';
import { fetchSearch } from '../api';
import '../styles/feed.css';

let dropdown: HTMLElement | null = null;
let debounceTimer: ReturnType<typeof setTimeout> | null = null;

export function render(searchBoxEl: HTMLElement): void {
  dropdown = document.createElement('div');
  dropdown.className = 'search-dropdown search-dropdown-hidden';
  searchBoxEl.appendChild(dropdown);

  subscribe(onStateChange);
}

function onStateChange(_state: typeof state, changed: Set<string>): void {
  if (changed.has('searchQuery')) {
    if (state.searchQuery.length === 0) {
      hideDropdown();
      return;
    }
    showDropdown();
    update({ searchLoading: true });

    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(async () => {
      try {
        const results = await fetchSearch(state.searchQuery);
        update({ searchResults: results, searchLoading: false });
      } catch {
        update({ searchResults: [], searchLoading: false });
      }
    }, 300);
  }

  if (changed.has('searchResults') || changed.has('searchLoading')) {
    renderResults();
  }

  if (changed.has('searchOpen')) {
    if (!state.searchOpen) hideDropdown();
  }
}

function showDropdown(): void {
  dropdown?.classList.remove('search-dropdown-hidden');
}

function hideDropdown(): void {
  dropdown?.classList.add('search-dropdown-hidden');
}

function renderResults(): void {
  if (!dropdown) return;

  if (state.searchLoading) {
    dropdown.innerHTML = '<div class="search-status">Searching...</div>';
    return;
  }

  if (state.searchResults.length === 0 && state.searchQuery.length > 0) {
    dropdown.innerHTML = `<div class="search-status">No results for "${escapeHtml(state.searchQuery)}"</div>`;
    return;
  }

  // Group results by session
  const groups = new Map<string, { name: string; project: string; results: SearchResult[] }>();
  for (const r of state.searchResults) {
    let group = groups.get(r.sessionId);
    if (!group) {
      group = { name: r.sessionName, project: r.projectName, results: [] };
      groups.set(r.sessionId, group);
    }
    group.results.push(r);
  }

  dropdown.innerHTML = '';
  for (const [sessionId, group] of groups) {
    const visibleResults = group.results.slice(0, 3);
    const remaining = group.results.length - visibleResults.length;

    for (const result of visibleResults) {
      const el = document.createElement('div');
      el.className = 'search-result';
      el.innerHTML = `
        <div class="search-result-header">
          <span class="search-result-session">${escapeHtml(group.name)}</span>
          <span class="search-result-project">${escapeHtml(group.project)}</span>
          <span class="search-type-badge ${badgeClass(result)}">${badgeLabel(result)}</span>
          <span class="search-result-time">${formatTime(result.timestamp)}</span>
        </div>
        <div class="search-result-body">${highlightMatch(result.contentText, state.searchQuery)}</div>
      `;
      el.addEventListener('click', () => {
        update({ selectedSessionId: sessionId, searchOpen: false, searchQuery: '' });
        const searchInput = document.querySelector<HTMLInputElement>('[data-search]');
        if (searchInput) searchInput.value = '';
      });
      dropdown.appendChild(el);
    }

    if (remaining > 0) {
      const more = document.createElement('div');
      more.className = 'search-group-more';
      more.textContent = `${remaining} more matches in this session`;
      dropdown.appendChild(more);
    }
  }
}

function badgeClass(r: SearchResult): string {
  if (r.isError) return 'error';
  if (r.toolName) return 'tool';
  return r.role || 'assistant';
}

function badgeLabel(r: SearchResult): string {
  if (r.isError) return 'error';
  if (r.toolName) return 'tool';
  return r.role || 'assistant';
}

function formatTime(ts: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

function highlightMatch(text: string, query: string): string {
  if (!text || !query) return escapeHtml(text || '');
  const truncated = text.length > 200 ? text.substring(0, 200) + '...' : text;
  const escaped = escapeHtml(truncated);
  const re = new RegExp(`(${escapeRegex(query)})`, 'gi');
  return escaped.replace(re, '<mark>$1</mark>');
}

function escapeHtml(s: string): string {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/search.ts web/src/styles/feed.css
git commit -m "feat: add search component with command-palette dropdown"
```

---

## Phase 5: Wire Everything Together

### Task 16: Wire main.ts with all components

**Files:**
- Modify: `web/src/main.ts`
- Modify: `web/index.html`

- [ ] **Step 1: Update index.html with the app structure**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Claude Monitor</title>
  <link rel="preconnect" href="https://fonts.googleapis.com" />
  <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Inter:wght@400;600&display=swap" rel="stylesheet" />
</head>
<body>
  <div id="app">
    <div id="topbar-mount"></div>
    <div id="main" style="display:flex; flex:1; overflow:hidden;">
      <div id="sessions-mount"></div>
      <div id="feed-mount" style="flex:1; display:flex; flex-direction:column;">
        <div class="feed-empty">Select a session to view its feed</div>
      </div>
    </div>
    <div id="statusbar" style="display:flex; align-items:center; gap:12px; padding:4px 12px; font-size:11px; color:var(--text-dim); border-top:1px solid var(--border);">
      <span id="conn-indicator">CONNECTED</span>
      <span id="sb-host"></span>
      <span id="sb-events">0 EVENTS</span>
      <span style="margin-left:auto" id="sb-version"></span>
    </div>
  </div>
  <script type="module" src="/src/main.ts"></script>
</body>
</html>
```

- [ ] **Step 2: Update main.ts to wire components**

```typescript
// web/src/main.ts
import './styles/base.css';
import { state, subscribe, update } from './state';
import { connect } from './ws';
import { fetchGroupedSessions, fetchVersion } from './api';
import { render as renderTopbar } from './components/topbar';
import { render as renderSessionList } from './components/session-list';
import { render as renderSearch } from './components/search';

// Mount components
const topbarMount = document.getElementById('topbar-mount')!;
const sessionsMount = document.getElementById('sessions-mount')!;

renderTopbar(topbarMount);
renderSessionList(sessionsMount);

// Attach search dropdown to the search box in the topbar
const searchBox = topbarMount.querySelector<HTMLElement>('.search-box');
if (searchBox) {
  renderSearch(searchBox);
}

// Status bar updates
const connIndicator = document.getElementById('conn-indicator')!;
const sbHost = document.getElementById('sb-host')!;
const sbEvents = document.getElementById('sb-events')!;
const sbVersion = document.getElementById('sb-version')!;

sbHost.textContent = location.host;

subscribe((_state, changed) => {
  if (changed.has('connected')) {
    connIndicator.textContent = state.connected ? 'CONNECTED' : 'DISCONNECTED';
    connIndicator.style.color = state.connected ? 'var(--green)' : 'var(--red)';
  }
  if (changed.has('eventCount')) {
    sbEvents.textContent = `${state.eventCount} EVENTS`;
  }
  if (changed.has('version')) {
    sbVersion.textContent = `CLAUDE MONITOR ${state.version}`;
  }
});

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
  // Don't capture when typing in an input
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;

  switch (e.key) {
    case 'g':
      update({ view: state.view === 'graph' ? 'list' : 'graph' });
      break;
    case 'Escape':
      update({ selectedSessionId: null, searchOpen: false });
      break;
  }
});

// Bootstrap
async function init() {
  try {
    const version = await fetchVersion();
    update({ version });
  } catch {
    update({ version: 'dev' });
  }

  // Load initial sessions into state
  try {
    const grouped = await fetchGroupedSessions();
    const allSessions = [
      ...grouped.active,
      ...grouped.lastHour,
      ...grouped.today,
      ...grouped.yesterday,
      ...grouped.thisWeek,
      ...grouped.older,
    ];
    for (const sess of allSessions) {
      state.sessions.set(sess.id, sess);
    }
    update({ grouped });
  } catch (err) {
    console.error('Failed to load sessions:', err);
  }

  // Connect WebSocket
  connect();
}

init();
```

- [ ] **Step 3: Verify TypeScript compiles and Vite builds**

Run:
```bash
cd web && npx tsc --noEmit && npm run build
```
Expected: No errors, build output in `cmd/claude-monitor/static/`

- [ ] **Step 4: Test the full app**

Run:
```bash
cd /root/claude-monitor && go build -o /tmp/cm-test ./cmd/claude-monitor && /tmp/cm-test -port 17710 &
sleep 2
curl -s http://localhost:17710/ | head -5
kill %1
```
Expected: HTML response from the new Vite-built frontend

- [ ] **Step 5: Commit**

```bash
git add web/index.html web/src/main.ts
git commit -m "feat: wire all components into main entry point"
```

### Task 17: Update CI to build frontend

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release-please.yml`

- [ ] **Step 1: Add Node.js + frontend build step to ci.yml**

Add after the Go setup step and before the Build step:

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version: '22'

      - name: Install and build frontend
        run: cd web && npm ci && npm run build
```

Also remove the "Check embedded HTML in sync" lint step since we no longer have dual HTML files.

- [ ] **Step 2: Add frontend build to release-please.yml**

Add before the "Build binaries" step:

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version: '22'

      - name: Build frontend
        run: cd web && npm ci && npm run build
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml .github/workflows/release-please.yml
git commit -m "ci: add frontend build step to CI and release workflows"
```

### Task 18: Clean up old static HTML

**Files:**
- Delete: `static/index.html`
- Delete: `cmd/claude-monitor/static/index.html` (replaced by Vite build output)

- [ ] **Step 1: Verify Vite build output exists**

Run: `ls -la cmd/claude-monitor/static/`
Expected: `index.html` and `assets/` from Vite build

- [ ] **Step 2: Remove old static files from git tracking**

The old `static/index.html` (root-level copy) is no longer needed. The `cmd/claude-monitor/static/` directory now contains Vite build output.

Run:
```bash
git rm static/index.html
```

- [ ] **Step 3: Update .gitignore to exclude Vite build output**

Add to `.gitignore`:
```
cmd/claude-monitor/static/
```

Note: The Vite build output must be generated at build/release time, not committed. The Go embed will reference the build output.

Actually — this changes the build model. The Go `go:embed` requires the files to exist at `go build` time. Two options:
1. Commit the build output (simpler, current approach for the single HTML)
2. Always build frontend first (CI does this, local devs need to too)

Option 2 is cleaner. Update the README or add a Makefile. For now, keep the Vite output in `.gitignore` and ensure CI builds frontend before Go.

- [ ] **Step 4: Commit**

```bash
git rm static/index.html
git add .gitignore
git commit -m "chore: remove old static HTML, frontend now built from web/"
```

---

## Not Yet Covered (Follow-Up Tasks)

The following components exist in the current 4,100-line `index.html` but are not ported in this plan. They should be ported as follow-up tasks after the core navigation overhaul ships:

- **Feed panel** (`web/src/components/feed-panel.ts`) — live message stream for selected session
- **Graph view** (`web/src/components/graph-view.ts`) — D3 force-directed agent dependency graph
- **Table view** (`web/src/components/table-view.ts`) — dense sortable table overlay
- **History view** (`web/src/components/history-view.ts`) — SQLite-backed historical sessions
- **Replay panel** (`web/src/components/replay.ts`) — session replay with scrubber and SSE streaming
- **Budget popover** — budget alert settings
- **Help overlay** — keyboard shortcut reference (`?`)

The current plan delivers: release fixes, new backend APIs, Vite + TS scaffold, time-grouped session list, and working search. The feed panel placeholder in `index.html` shows "Select a session to view its feed" until ported.

---

## Phase 6: Final Integration Test

### Task 19: End-to-end verification

- [ ] **Step 1: Build and run the full app**

```bash
cd /root/claude-monitor
cd web && npm ci && npm run build && cd ..
go build -o /tmp/cm-final ./cmd/claude-monitor
/tmp/cm-final -port 17720 &
sleep 2
```

- [ ] **Step 2: Verify all API endpoints**

```bash
curl -s http://localhost:17720/health | jq .
curl -s http://localhost:17720/api/version | jq .
curl -s http://localhost:17720/api/sessions/grouped | jq 'keys'
curl -s http://localhost:17720/api/projects | jq '.[0:3]'
curl -s "http://localhost:17720/api/search?q=test&limit=5" | jq 'length'
```

- [ ] **Step 3: Verify frontend loads**

```bash
curl -s http://localhost:17720/ | grep -o '<title>.*</title>'
```
Expected: `<title>Claude Monitor</title>`

- [ ] **Step 4: Run all Go tests**

```bash
go test ./... -count=1 -v
```
Expected: All PASS

- [ ] **Step 5: Clean up**

```bash
kill %1
```

- [ ] **Step 6: Final commit if any remaining changes**

```bash
git status
# If clean, no commit needed
```
