# Pixel Office Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a new `pixel-office` project merging pixel-corp's animated pixel office with claude-monitor's monitoring backend into a single Go binary serving a React frontend.

**Architecture:** Go backend (ported from claude-monitor) serves REST/WebSocket APIs and embeds a Vite-built React frontend (office engine ported from pixel-corp, dashboard panels rewritten as React components). Single binary, zero runtime dependencies.

**Tech Stack:** Go 1.25, React 19, TypeScript, Vite 7, Canvas 2D, WebSocket, SQLite, Docker API

**Source projects:**
- `/root/claude-monitor` — Go backend, vanilla JS frontend
- `/root/pixel-corp` — Node.js backend, React + Canvas frontend

**Target:** `/root/pixel-office`

**Key integration challenges:**
- pixel-corp uses `vscodeApi.ts` for communication — must be replaced with direct REST/WebSocket calls
- pixel-corp uses VS Code CSS theme variables (`--vscode-foreground`, etc.) — must define equivalents
- pixel-corp pushes assets from server → client — must switch to client-pull via `fetch()`
- Compiled TypeScript won't pass until stubs exist for replaced modules — use a `compat/` shim layer

---

## Phase 1: Project Scaffolding & Go Backend

### Task 1.1: Initialize repository and Go module

**Files:**
- Create: `/root/pixel-office/go.mod`
- Create: `/root/pixel-office/cmd/pixel-office/main.go` (minimal)
- Create: `/root/pixel-office/Makefile`
- Create: `/root/pixel-office/README.md`
- Create: `/root/pixel-office/CLAUDE.md`
- Create: `/root/pixel-office/.gitignore`

- [ ] **Step 1: Create repo directory and initialize git**

```bash
mkdir -p /root/pixel-office
cd /root/pixel-office
git init
```

- [ ] **Step 2: Create go.mod with dependencies**

```bash
cd /root/pixel-office
go mod init github.com/Zxela/pixel-office
go get github.com/fsnotify/fsnotify@v1.7.0
go get github.com/gorilla/websocket@v1.5.1
go get modernc.org/sqlite
```

- [ ] **Step 3: Create minimal main.go**

`cmd/pixel-office/main.go` — HTTP server on port 7700, `/health` endpoint only.

- [ ] **Step 4: Create Makefile**

```makefile
.PHONY: dev build test

dev:
	@echo "Starting Go backend and Vite dev server..."
	@cd frontend && npm run dev &
	@go run ./cmd/pixel-office/ --dev

build:
	cd frontend && npm run build
	rm -rf cmd/pixel-office/static && cp -r frontend/dist cmd/pixel-office/static
	go build -o pixel-office ./cmd/pixel-office/

test:
	go test ./...
	cd frontend && npm test 2>/dev/null || true
```

- [ ] **Step 5: Create .gitignore**

```
/pixel-office
/cmd/pixel-office/static/
/frontend/node_modules/
/frontend/dist/
*.db
.superpowers/
```

- [ ] **Step 6: Create CLAUDE.md** with project conventions

- [ ] **Step 7: Build and verify**

```bash
go build ./cmd/pixel-office/
```

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: initialize pixel-office project scaffold"
```

---

### Task 1.2: Port Go internal packages from claude-monitor

**Files:**
- Copy: `/root/claude-monitor/internal/` → `/root/pixel-office/internal/`
- Modify: all Go files — update module import paths
- Modify: `internal/session/session.go` — add `SeatID`, `CharacterSkin` fields

- [ ] **Step 1: Copy all internal packages**

```bash
cp -r /root/claude-monitor/internal/ /root/pixel-office/internal/
```

- [ ] **Step 2: Update import paths**

Find and replace `github.com/zxela-claude/claude-monitor` → `github.com/Zxela/pixel-office` in all `.go` files.

- [ ] **Step 3: Add office-related fields to Session struct**

In `internal/session/session.go`, add to `Session` struct:
```go
SeatID         string `json:"seatId,omitempty"`
CharacterSkin  int    `json:"characterSkin"`
```

- [ ] **Step 4: Run go mod tidy and tests**

```bash
go mod tidy && go test ./...
```

All existing tests should pass.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: port Go internal packages, add office session fields"
```

---

### Task 1.3: Port main.go with full backend + layout/settings APIs

**Files:**
- Modify: `/root/pixel-office/cmd/pixel-office/main.go`

- [ ] **Step 1: Port main.go from claude-monitor**

Copy `cmd/claude-monitor/main.go` contents. Update:
- Import paths to `github.com/Zxela/pixel-office/internal/*`
- History DB path: `~/.pixel-office/history.db`
- Remove `//go:embed static` (frontend not built yet)
- Serve placeholder HTML at `/`

- [ ] **Step 2: Add layout and settings API routes**

New routes:
- `GET /api/layout` — read `~/.pixel-office/layout.json`, return contents or `{}`
- `PUT /api/layout` — atomic write (temp file + rename) to `~/.pixel-office/layout.json`
- `GET /api/settings` — read `~/.pixel-office/settings.json`, return contents or `{}`
- `PUT /api/settings` — atomic write to `~/.pixel-office/settings.json`

Helper: `ensureDir(path)` creates directory if missing.

- [ ] **Step 3: Add `--dev` flag for Vite proxy**

When `--dev` is passed, serve a redirect to `http://localhost:5173` from `/` instead of embedded static files. This enables Vite HMR during development.

- [ ] **Step 4: Build and test all routes with curl**

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: full Go backend with layout/settings APIs and dev mode"
```

---

## Phase 2: Frontend Scaffolding & Office Engine

### Task 2.1: Initialize React frontend with Vite

**Files:**
- Create: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `frontend/tsconfig.app.json`
- Create: `frontend/index.html`, `frontend/src/main.tsx`, `frontend/src/App.tsx`
- Create: `frontend/src/index.css`

- [ ] **Step 1: Create package.json**

Dependencies: `react@^19`, `react-dom@^19`. Dev: `@vitejs/plugin-react`, `vite@^7`, `typescript@~5.9`, `@types/react`, `@types/react-dom`.

- [ ] **Step 2: Create vite.config.ts**

Key settings:
- `build.outDir: '../cmd/pixel-office/static'`
- `build.emptyOutDir: true`
- `base: './'`
- `server.proxy`: `/api` and `/ws` → `http://localhost:7700`

- [ ] **Step 3: Create tsconfig** (port from pixel-corp's tsconfig.app.json)

- [ ] **Step 4: Create minimal App.tsx** rendering "Pixel Office loading..."

- [ ] **Step 5: Install dependencies and verify**

```bash
cd frontend && npm install && npm run dev &
sleep 3 && curl -s http://localhost:5173/ | head -c 200
kill %1
```

- [ ] **Step 6: Verify production build puts files in cmd/pixel-office/static/**

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: initialize React frontend with Vite"
```

---

### Task 2.2: Port office engine, assets, and create compatibility shim

This is the biggest porting task. We copy the entire office engine, all assets, and create a `vscodeApi.ts` compatibility shim so TypeScript compiles without rewriting every import immediately.

**Files:**
- Copy: pixel-corp `webview-ui/src/office/` → `frontend/src/office/` (ALL subdirectories including index.ts barrel files)
- Copy: pixel-corp `webview-ui/public/assets/` → `frontend/public/assets/` (characters/, floors.png, walls.png, default-layout.json)
- Copy: pixel-corp `webview-ui/src/fonts/` → `frontend/public/fonts/` (served as static asset)
- Copy: pixel-corp `webview-ui/src/constants.ts` → `frontend/src/constants.ts`
- Copy: pixel-corp `webview-ui/src/notificationSound.ts` → `frontend/src/notificationSound.ts`
- Copy: licensed tileset if present
- Create: `frontend/src/vscodeApi.ts` — compatibility shim (REST/WebSocket instead of VS Code API)
- Create: `frontend/src/hooks/useExtensionMessages.ts` — stub that provides the same interface as pixel-corp's but wired to our WebSocket
- Merge: pixel-corp CSS into `frontend/src/index.css` — include pixel font @font-face, pixel theme variables, AND define equivalents for `--vscode-*` CSS variables

- [ ] **Step 1: Copy office engine (all subdirectories with index.ts files)**

```bash
cp -r /root/pixel-corp/webview-ui/src/office/ /root/pixel-office/frontend/src/office/
```

Verify index.ts files copied: `engine/index.ts`, `sprites/index.ts`, `layout/index.ts`, `editor/index.ts`, `components/index.ts`.

- [ ] **Step 2: Copy assets and fonts**

```bash
cp -r /root/pixel-corp/webview-ui/public/assets/ /root/pixel-office/frontend/public/assets/
mkdir -p /root/pixel-office/frontend/public/fonts
cp /root/pixel-corp/webview-ui/src/fonts/FSPixelSansUnicode-Regular.ttf /root/pixel-office/frontend/public/fonts/
cp /root/pixel-corp/assets/office_tileset_16x16.png /root/pixel-office/frontend/public/assets/ 2>/dev/null || true
```

- [ ] **Step 3: Copy constants and notification sound**

```bash
cp /root/pixel-corp/webview-ui/src/constants.ts /root/pixel-office/frontend/src/
cp /root/pixel-corp/webview-ui/src/notificationSound.ts /root/pixel-office/frontend/src/
```

- [ ] **Step 4: Create vscodeApi.ts compatibility shim**

This replaces pixel-corp's `vscodeApi.ts`. The exported `vscode` object provides the same `postMessage()` interface but routes to REST/WebSocket:

```typescript
// frontend/src/vscodeApi.ts
// Compatibility shim — replaces VS Code API with REST + WebSocket

let ws: WebSocket | null = null;
const messageQueue: unknown[] = [];

function connect() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${protocol}//${window.location.host}/ws`);
  ws.onopen = () => { messageQueue.forEach(m => ws!.send(JSON.stringify(m))); messageQueue.length = 0; };
  ws.onmessage = (e) => { window.dispatchEvent(new MessageEvent('message', { data: JSON.parse(e.data) })); };
  ws.onclose = () => { ws = null; setTimeout(connect, 3000); };
}
connect();

export const vscode = {
  postMessage(msg: unknown) {
    const m = msg as Record<string, unknown>;
    // Route specific message types to REST endpoints
    if (m.type === 'saveLayout') {
      fetch('/api/layout', { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(m.layout) });
    } else if (m.type === 'saveSettings') {
      fetch('/api/settings', { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify(m.settings) });
    } else if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    } else {
      messageQueue.push(msg);
    }
  }
};
```

This lets ALL existing pixel-corp code that does `vscode.postMessage(...)` work without changes.

- [ ] **Step 5: Create useExtensionMessages stub**

Create `frontend/src/hooks/useExtensionMessages.ts` that exports the same interface as pixel-corp's hook but receives data from our WebSocket format instead of VS Code message events. This is a stub — it will be progressively replaced in Phase 3, but it allows TypeScript to compile now.

Study pixel-corp's `useExtensionMessages.ts` (436 lines) to understand its return type and implement a minimal version that:
- Connects via the vscodeApi shim
- Listens for `window` message events (dispatched by our shim)
- Returns the same state shape (agents, projects, layout, settings, etc.)

- [ ] **Step 6: Merge CSS with VS Code variable equivalents**

In `frontend/src/index.css`, merge pixel-corp's CSS and add:
```css
:root {
  /* Pixel-corp theme (keep these) */
  --pixel-bg: #1e1e2e;
  --pixel-border: #4a4a6a;
  --pixel-accent: #5a8cff;
  --pixel-green: #5ac88c;
  /* ... all pixel-corp vars ... */

  /* VS Code variable equivalents (for ported components) */
  --vscode-foreground: #e0e0e0;
  --vscode-charts-yellow: #ddcc44;
  --vscode-charts-blue: #5a8cff;
  --vscode-charts-green: #5ac88c;
  --vscode-charts-red: #dd4455;
  --vscode-editor-background: #1e1e2e;
  /* ... map all --vscode-* vars used in ported components ... */
}
```

Also update @font-face to point to `public/fonts/` path.

- [ ] **Step 7: Copy UI components**

```bash
mkdir -p /root/pixel-office/frontend/src/components
cp /root/pixel-corp/webview-ui/src/components/AgentLabels.tsx /root/pixel-office/frontend/src/components/
cp /root/pixel-corp/webview-ui/src/components/BottomToolbar.tsx /root/pixel-office/frontend/src/components/
cp /root/pixel-corp/webview-ui/src/components/SettingsModal.tsx /root/pixel-office/frontend/src/components/
cp /root/pixel-corp/webview-ui/src/components/ZoomControls.tsx /root/pixel-office/frontend/src/components/
```

- [ ] **Step 8: Copy editor hooks**

```bash
cp /root/pixel-corp/webview-ui/src/hooks/useEditorActions.ts /root/pixel-office/frontend/src/hooks/
cp /root/pixel-corp/webview-ui/src/hooks/useEditorKeyboard.ts /root/pixel-office/frontend/src/hooks/
```

- [ ] **Step 9: Verify TypeScript compiles**

```bash
cd /root/pixel-office/frontend && npx tsc --noEmit
```

Fix remaining import errors. The vscodeApi shim and useExtensionMessages stub should cover all external dependencies. Fix any `--vscode-*` CSS variables missed in Step 6.

- [ ] **Step 10: Commit**

```bash
git add -A && git commit -m "feat: port office engine, assets, components with vscodeApi shim"
```

---

## Phase 3: Wire Office to Backend

### Task 3.1: Create WebSocket hook and session state

**Files:**
- Create: `frontend/src/hooks/useWebSocket.ts`
- Create: `frontend/src/hooks/useSessionState.ts`
- Create: `frontend/src/types.ts`

- [ ] **Step 1: Define TypeScript types**

`types.ts` with interfaces matching Go backend JSON:
- `Session` — mirrors `session.Session` struct (including `seatId`, `characterSkin`)
- `ParsedMessage` — mirrors `parser.ParsedMessage`
- `BroadcastEvent` — `{event: string, session: Session, message?: ParsedMessage}`

- [ ] **Step 2: Create useWebSocket hook**

React hook:
- Connects to `ws://host/ws`
- Auto-reconnect with exponential backoff (1s base, 30s max)
- Returns `{connected, messages, send}` where messages is a callback dispatch

- [ ] **Step 3: Create useSessionState hook**

Manages session store:
- `sessions: Record<string, Session>`
- `selectedSession: string | null`
- Derived stats: `activeCount`, `totalCost`, `costRate`, `cacheHitPct`, `workingCount`
- Handles WS events: `session_new`, `session_update`, `message`
- Fetches `/api/sessions` on mount

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: WebSocket hook and session state management"
```

---

### Task 3.2: Asset loading — replace server-push with client-pull

**Files:**
- Create: `frontend/src/hooks/useAssetLoader.ts`

- [ ] **Step 1: Create useAssetLoader hook**

On mount:
- Fetch character PNGs (`/assets/characters/char_0.png` through `char_5.png`) via `fetch()`
- Decode PNGs using canvas: `new Image()` → draw to offscreen canvas → `getImageData()` → RGBA buffer
- Convert RGBA to SpriteData format (hex color arrays) matching pixel-corp's `CharacterDirectionSprites` type
- Fetch `floors.png`, `walls.png` similarly
- Call pixel-corp's `setCharacterTemplates()`, `setFloorSprites()`, `setWallSprites()` on OfficeState
- Call `buildDynamicCatalog()` with loaded asset data
- Fetch `default-layout.json` as fallback layout
- Return `{loaded: boolean, error: string | null}`

Note: Web Audio API for notification sound requires user gesture to initialize. The `notificationSound.ts` module handles this by creating AudioContext on first canvas mousedown.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: client-side asset loading from static files"
```

---

### Task 3.3: Create useOfficeState — bridge sessions to characters

**Files:**
- Create: `frontend/src/hooks/useOfficeState.ts`
- Modify: `frontend/src/App.tsx` — wire everything together

- [ ] **Step 1: Create useOfficeState hook**

Bridges session data to OfficeState:
- Instantiates `OfficeState` imperatively
- On new session → `officeState.addAgent(id, palette, seatId)` with matrix spawn effect
- On status change → update character state:
  - `thinking`/`tool_use` → TYPE (use READING_TOOLS list from pixel-corp's `toolUtils.ts` to choose reading vs typing animation)
  - `waiting`/`idle`/`""` → IDLE (will wander)
- On session inactive >10min → `officeState.removeAgent(id)` with matrix despawn
- **Subagent handling**: when `session.isSubagent && session.parentId`:
  - Spawn near parent character's seat (Manhattan distance to nearest free seat)
  - Use same palette as parent + hue shift
  - Label shows agent type from meta.json (e.g., "Explore", "code-reviewer")
- Fetches layout from `/api/layout` on mount (falls back to default-layout.json)
- Seat assignment persistence: key by `projectDir` (stable across session restarts, not ephemeral session ID). Save to `/api/settings` on change.

- [ ] **Step 2: Wire everything in App.tsx**

Replace placeholder with full composition:
```tsx
function App() {
  const { connected } = useWebSocket();
  const { sessions, selectedSession, selectSession, stats } = useSessionState();
  const { loaded } = useAssetLoader();
  const { officeState } = useOfficeState(sessions);

  if (!loaded) return <div>Loading assets...</div>;

  return (
    <>
      <OfficeCanvas officeState={officeState} onAgentClick={selectSession} />
      <AgentLabels officeState={officeState} sessions={sessions} />
      <ZoomControls />
      <BottomToolbar />
      {/* TopBar and panels added in Phase 4 */}
    </>
  );
}
```

- [ ] **Step 3: Test end-to-end**

Run Go backend + Vite dev server. Open browser. Verify:
- Office canvas renders with floor, walls, furniture
- If Claude sessions are active, characters appear and animate
- Characters walk when idle, type when active

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: wire office to backend, characters spawn from live sessions"
```

---

## Phase 4: Dashboard Panels

### Task 4.1: Floating top bar

**Files:**
- Create: `frontend/src/components/TopBar.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Create TopBar component**

Semi-transparent floating bar over canvas:
- Brand: ◆ pixel-office
- Stats: ACTIVE, TOTAL SPEND, $/MIN, CACHE HIT
- Budget gear icon
- View toggles: FEED | TABLE | HISTORY
- Search icon
- Style: `position:fixed; top:0; background:rgba(15,15,24,0.85); backdrop-filter:blur(4px)`

- [ ] **Step 2: Wire into App with view state**

`currentView` state: `'office' | 'table' | 'history'`
`feedOpen` state: boolean

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: floating top bar with stats and view toggles"
```

---

### Task 4.2: Feed panel (slide-out)

**Files:**
- Create: `frontend/src/dashboard/FeedPanel.tsx`
- Create: `frontend/src/dashboard/FeedEntry.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Create FeedEntry component**

Props: `message`, `sessionName?`, `isMultiSession`. Renders timestamp, type badge, content, expand/collapse. Red highlight for `isError`. Tool grouping for tool_use/tool_result pairs.

- [ ] **Step 2: Create FeedPanel component**

Slide-out from right (~40% width). Features:
- Session header with name, cost, tokens, model
- Action buttons: REPLAY, STOP
- Feed filter buttons with solo mode (shift+click), ALL toggle
- Error count clickable → solo errors filter
- Fetches `/api/sessions/{id}/recent` on session select
- Multi-session mode when no session selected (shows WS messages from all sessions)
- Auto-scroll with lock/unlock

- [ ] **Step 3: Wire into App**

- Click agent in office → selectSession → open feed
- Click FEED in top bar → open multi-session feed
- Click X or canvas → close feed
- WS messages stream into feed in real-time

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: slide-out feed panel with filtering"
```

---

### Task 4.3: Search overlay

**Files:**
- Create: `frontend/src/dashboard/SearchPanel.tsx`

- [ ] **Step 1: Create SearchPanel**

Overlay: search input (300ms debounce), fetches `/api/search?q=...`, renders results as FeedEntry with `<mark>` highlighting. Click result → select session → close search. "Back to results" button.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: cross-session search overlay"
```

---

### Task 4.4: Table view

**Files:**
- Create: `frontend/src/dashboard/TableView.tsx`

- [ ] **Step 1: Create TableView**

Full-screen overlay. Sortable table: Name, Status, Cost, $/min, Duration, Tokens, Cache%, Messages, Errors, Model. Click row → return to office + open feed.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: sortable comparison table view"
```

---

### Task 4.5: History view

**Files:**
- Create: `frontend/src/dashboard/HistoryView.tsx`

- [ ] **Step 1: Create HistoryView**

Full-screen overlay. Fetches `/api/history`. Sortable table: Date, Name, Cost, Duration, Tokens, Messages, Errors, Model. Click row → open replay.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: historical sessions view"
```

---

### Task 4.6: Replay panel

**Files:**
- Create: `frontend/src/dashboard/ReplayPanel.tsx`

- [ ] **Step 1: Create ReplayPanel**

Replaces feed content. Manifest from `/api/sessions/{id}/replay`. Scrubber, play/pause, speed (0.5x–4x). SSE via `EventSource`. Keyboard: Space=play/pause, R=restart, arrows=step.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: session replay with scrubber and SSE"
```

---

## Phase 5: Integration & Polish

### Task 5.1: Office editor integration

- [ ] **Step 1: Wire editor toggle** in BottomToolbar → show EditorToolbar overlay
- [ ] **Step 2: Save layout** to `/api/layout` on changes (debounced 500ms)
- [ ] **Step 3: Load layout** from `/api/layout` on mount, fallback to `default-layout.json`
- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: office layout editor with persistence"
```

---

### Task 5.2: Settings, budget, notifications

- [ ] **Step 1: Adapt SettingsModal** — sound toggle, notification toggles, budget threshold, layout export/import. Save to `/api/settings`.
- [ ] **Step 2: Create useNotifications hook** — browser Notification API, watch for budget exceeded and agent errors.
- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: settings, budget alerts, browser notifications"
```

---

### Task 5.3: Keyboard shortcuts

- [ ] **Step 1: Create useKeyboardShortcuts hook**

Global: `/`=search, Esc=close, `f`=feed, `t`=table, `h`=history, `e`=editor, `?`=help, Up/Down=navigate agents.
Replay active: Space=play/pause, `r`=restart, Left/Right=step.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: keyboard shortcuts"
```

---

### Task 5.4: Responsive layout

- [ ] **Step 1: Add CSS media queries**

`@media (max-width: 1024px)`: feed panel full-width, top bar wraps.
`@media (max-width: 768px)`: two-row top bar, horizontal scroll on tables.

- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "feat: responsive layout"
```

---

### Task 5.5: Production build and deploy

- [ ] **Step 1: Add embed directive to main.go**

```go
//go:embed static
var staticFiles embed.FS
```

Makefile `build` copies `frontend/dist/` → `cmd/pixel-office/static/` then runs `go build`.

- [ ] **Step 2: Full build test**

```bash
make build
./pixel-office &
curl http://localhost:7700/health
# Open browser — verify office + dashboard work
```

- [ ] **Step 3: Create private GitHub repo and push**

```bash
gh repo create Zxela/pixel-office --private --source=. --push
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: production build with embedded frontend"
git push
```

---

## Execution Summary

| Phase | Tasks | Milestone |
|-------|-------|-----------|
| 1 | 1.1–1.3 | Go backend running, all APIs work (test with curl) |
| 2 | 2.1–2.2 | React frontend compiles with ported office engine |
| 3 | 3.1–3.3 | **First end-to-end:** office renders, characters spawn from live sessions |
| 4 | 4.1–4.6 | All dashboard panels: top bar, feed, search, table, history, replay |
| 5 | 5.1–5.5 | Editor, settings, shortcuts, responsive, production binary |

Each phase produces working, testable software. Phase 3 is the critical integration milestone.
