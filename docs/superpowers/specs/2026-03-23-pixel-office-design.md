# Pixel Office — Design Spec

**Goal:** Merge pixel-corp (animated pixel office) and claude-monitor (real-time monitoring dashboard) into a single app where AI agents are visualized as characters in a customizable pixel office, with full monitoring/reporting capabilities accessible via overlay panels.

**Repo:** `pixel-office` (private, sibling directory to both source projects)

---

## Architecture Overview

Single Go binary serving a React frontend. The pixel office canvas is the primary view, filling the entire viewport. A minimal floating top bar shows aggregate stats. Clicking an agent character opens a slide-out feed panel from the right. Additional views (table, history) are accessible via top bar toggles as full-screen overlays.

**Backend:** Go — handles JSONL file watching, session state, WebSocket broadcasting, REST APIs, SQLite history persistence, and Docker container discovery. Embeds the built React frontend via `//go:embed`.

**Frontend:** React + TypeScript + Vite — pixel office canvas rendering engine (ported from pixel-corp), dashboard panels (rewritten as React components from claude-monitor's vanilla JS), shared state via React hooks + WebSocket.

**Build:** `make build` runs `npm run build` (Vite → `frontend/dist/`) then `go build` (embeds `frontend/dist/` into binary). Single binary output, zero runtime dependencies.

---

## Project Structure

```
pixel-office/
├── cmd/pixel-office/
│   ├── main.go                  # Go entry point, HTTP/WS server, routes
│   └── static/                  # go:embed target (Makefile copies frontend/dist here before go build)
├── internal/
│   ├── session/                 # Session store, aggregation (from claude-monitor)
│   ├── parser/                  # JSONL line parser (from claude-monitor)
│   ├── watcher/                 # File watcher with fsnotify + polling (from claude-monitor)
│   ├── hub/                     # WebSocket hub (from claude-monitor)
│   ├── docker/                  # Docker container discovery (from claude-monitor)
│   ├── store/                   # SQLite history persistence (from claude-monitor)
│   └── replay/                  # Session replay reader + SSE stream (from claude-monitor)
├── frontend/
│   ├── src/
│   │   ├── App.tsx              # Root component, view state, WebSocket connection
│   │   ├── main.tsx             # React entry point
│   │   ├── index.css            # Global styles, CSS variables, pixel font
│   │   ├── constants.ts         # Grid, animation, rendering parameters
│   │   ├── types.ts             # Shared TypeScript types
│   │   │
│   │   ├── office/              # Pixel office engine (from pixel-corp)
│   │   │   ├── engine/          # Game loop, office state, character FSM, renderer
│   │   │   ├── sprites/         # Sprite data, caching, colorization
│   │   │   ├── layout/          # Layout serializer, furniture catalog, tile map, pathfinding
│   │   │   ├── editor/          # Layout editor (floor/wall/furniture tools, undo/redo)
│   │   │   └── components/      # OfficeCanvas, ToolOverlay
│   │   │
│   │   ├── dashboard/           # Monitoring panels (from claude-monitor, rewritten as React)
│   │   │   ├── FeedPanel.tsx    # Slide-out live feed with type/error filtering
│   │   │   ├── SearchPanel.tsx  # Cross-session search with highlighting
│   │   │   ├── TableView.tsx    # Dense sortable comparison table
│   │   │   ├── HistoryView.tsx  # SQLite-backed historical sessions table
│   │   │   ├── ReplayPanel.tsx  # Session replay with scrubber + SSE
│   │   │   └── FeedEntry.tsx    # Single feed entry component
│   │   │
│   │   ├── components/          # Shared UI components
│   │   │   ├── TopBar.tsx       # Floating stats bar (active, cost, $/min, cache)
│   │   │   ├── AgentLabels.tsx  # Name/status labels above characters (needs React state)
│   │   │   ├── BottomToolbar.tsx # Editor/settings/project controls
│   │   │   ├── SettingsModal.tsx # Sound, notifications, budget, layout export/import
│   │   │   └── ZoomControls.tsx # Canvas zoom +/-
│   │   │
│   │   └── hooks/               # React hooks
│   │       ├── useWebSocket.ts  # WebSocket connection, reconnect, message dispatch
│   │       ├── useSessionState.ts # Session store, derived stats
│   │       ├── useOfficeState.ts  # Office layout, character positions
│   │       └── useEditorState.ts  # Editor tools, undo/redo
│   │
│   ├── public/
│   │   ├── assets/              # Sprites, tilesets, fonts (including licensed assets)
│   │   │   ├── characters/      # Character sprite sheets (6 skins, PNG)
│   │   │   ├── office_tileset_16x16.png  # Licensed office tileset (private repo)
│   │   │   ├── floors.png       # Floor tile patterns (colorizable)
│   │   │   ├── walls.png        # Wall auto-tile pieces (4x4 bitmask grid)
│   │   │   └── default-layout.json
│   │   └── fonts/
│   │       └── FSPixelSansUnicode-Regular.ttf
│   │
│   ├── vite.config.ts
│   ├── tsconfig.json
│   └── package.json
│
├── go.mod
├── go.sum
├── Makefile                     # dev, build, test targets
├── README.md
└── CLAUDE.md
```

---

## Views & Navigation

### 1. Office View (default, always present)

Full-viewport HTML5 Canvas rendering the pixel office. Characters represent active agent sessions. Characters animate based on agent state:

- **TYPE** (2 frames, 0.3s): agent is actively working (tool_use, thinking)
- **IDLE** (static): agent is waiting or inactive
- **WALK** (3 frames, 0.15s): idle agents wander between tasks

Character behaviors ported from pixel-corp:
- BFS pathfinding on walkable tiles
- Seat assignment (click character → click seat)
- Wander AI (random walks during idle, return to seat)
- Matrix spawn/despawn effect on session start/end
- Permission bubble (amber ⋯) when agent needs user confirmation
- Completion chime (Web Audio, toggleable)

**Agent labels** float above characters: name, status dot (green=active, yellow=waiting), current tool.

### 2. Floating Top Bar

Semi-transparent bar over the top of the canvas. Shows:
- Brand: ◆ pixel-office
- ACTIVE count (green)
- TOTAL SPEND (yellow)
- $/MIN velocity (yellow)
- CACHE HIT % (purple)
- Budget gear icon (opens budget alert config)
- View toggles: FEED | TABLE | HISTORY
- Search icon (opens search overlay)

### 3. Feed Panel (slide-out, right side)

Opens when:
- User clicks an agent character in the office
- User clicks FEED toggle in top bar (shows multi-session feed)

Contents:
- Session header: agent name, type, cost, tokens, model, duration
- Action buttons: REPLAY, STOP (Docker only)
- Live message feed with type filtering (user/assistant/tool_use/agent/tool_result/hook/errors)
- Feed filter buttons with solo mode (shift+click) and ALL toggle
- Error count clickable to filter to errors only
- Expandable entries for long content
- Multi-session mode when no agent selected

### 4. Table View (overlay)

Full-screen overlay (hides office) with dense sortable table of all current sessions. Columns: Name, Status, Cost, $/min, Duration, Tokens, Cache%, Messages, Errors, Model. Click headers to sort, click row to select agent (returns to office + opens feed panel).

### 5. History View (overlay)

Full-screen overlay showing SQLite-backed historical session data. Sortable table: Date, Name, Cost, Duration, Tokens, Messages, Errors, Model. Click row to open replay.

### 6. Replay Panel

Replaces feed panel contents when activated. Scrubber, play/pause, speed control, SSE-streamed event playback.

### 7. Office Editor

Activated via bottom toolbar button. Overlays editor controls:
- Tool palette: Select, Floor Paint, Wall Paint, Furniture Place, Eyedropper, Erase
- Color sliders — Hue, Saturation, Brightness, Contrast (HSBC)
- Furniture catalog browser
- Undo/redo (Ctrl+Z/Y)
- Grid resize (expand by clicking border)

Persisted to `~/.pixel-office/layout.json`. Export/import via settings modal.

---

## Backend (Go)

### Packages (ported from claude-monitor)

All `internal/` packages port directly with minimal changes:

| Package | Source | Changes |
|---------|--------|---------|
| `session` | claude-monitor | Add `SeatID`, `CharacterSkin` fields for office state |
| `parser` | claude-monitor | No changes — JSONL parsing is identical |
| `watcher` | claude-monitor | No changes |
| `hub` | claude-monitor | No changes |
| `docker` | claude-monitor | No changes |
| `store` | claude-monitor | No changes |
| `replay` | claude-monitor | No changes |

### New: Layout persistence

The Go server also manages office layout and seat assignment persistence:

- `GET /api/layout` — returns current office layout JSON
- `PUT /api/layout` — saves layout (from editor)
- `GET /api/settings` — returns user settings (sound, notifications)
- `PUT /api/settings` — saves settings

Layout stored at `~/.pixel-office/layout.json`. Settings at `~/.pixel-office/settings.json`.

### API Routes

All existing claude-monitor routes carry over:

| Route | Method | Purpose |
|-------|--------|---------|
| `/` | GET | Serve React frontend |
| `/ws` | WS | WebSocket for real-time events |
| `/api/sessions` | GET | All current sessions |
| `/api/search` | GET | Cross-session search |
| `/api/sessions/{id}/recent` | GET | Recent messages for session |
| `/api/sessions/{id}/replay` | GET | Replay manifest |
| `/api/sessions/{id}/replay/stream` | GET | SSE replay stream |
| `/api/sessions/{id}/stop` | POST | Stop Docker container |
| `/api/history` | GET | Historical sessions |
| `/api/layout` | GET/PUT | Office layout |
| `/api/settings` | GET/PUT | User settings |
| `/health` | GET | Health check |

### WebSocket Protocol

Same as claude-monitor:
```json
{
  "event": "session_new" | "session_update" | "message",
  "session": { /* Session object */ },
  "message": { /* ParsedMessage, optional */ }
}
```

Frontend maps `session_new` to character spawn (with matrix effect), session going inactive to idle animation, `message` events to feed entries and character state updates.

---

## Frontend (React)

### State Management

No external state library. React hooks + context:

- `useWebSocket()` — manages WebSocket connection, exponential backoff reconnect, dispatches messages to subscribers
- `useSessionState()` — maintains session map, derives stats (active count, total cost, cost rate, cache %), handles WebSocket session/message events
- `useOfficeState()` — manages OfficeState instance (imperative, from pixel-corp), syncs with session state changes (add/remove characters, update animations)
- `useEditorState()` — editor tool selection, undo/redo stack, layout mutations

### Office Engine (from pixel-corp)

The canvas rendering engine ports largely intact:

- **Game loop**: `requestAnimationFrame` with delta time capping
- **OfficeState**: Imperative class managing layout, characters, seats, pathfinding
- **Character FSM**: IDLE → WALK → TYPE states, driven by session status from WebSocket
- **Renderer**: Canvas 2D drawing — tiles, furniture, characters, labels, bubbles
- **Sprite system**: PNG → RGBA → hex array → cached offscreen canvas per zoom level
- **Colorization**: Dual-mode (Colorize/Adjust) for floor tiles and furniture
- **Auto-tiling**: Wall bitmask system (16 variants from 4×4 grid)

Key adaptation: pixel-corp maps Claude Code tool events to character states. We keep this mapping but drive it from our Go backend's parsed messages rather than raw JSONL:

| Backend Status | Character State | Animation |
|---------------|----------------|-----------|
| `thinking` | TYPE | Typing frames |
| `tool_use` | TYPE | Typing frames (or Reading frames for Read/Grep/Glob/WebFetch/WebSearch) |
| `waiting` | IDLE | Standing, will wander |
| `idle` | IDLE | Standing, will wander |
| `""` (new session) | IDLE | Standing, transitions on first message |

Reading vs typing animation: pixel-corp distinguishes `READING_TOOLS` (Read, Grep, Glob, WebFetch, WebSearch) from other tools. The `toolName` field on the parsed message determines which animation variant to use within the TYPE state.

### Asset Loading (adaptation from pixel-corp)

Pixel-corp loads sprites via the VS Code extension host or Node.js server, pushing binary PNG data over WebSocket. In pixel-office, assets are static files served by the Go backend:

- Character PNGs, floor/wall tiles, and the licensed tileset are in `frontend/public/assets/`
- On startup, the frontend fetches these via HTTP (`fetch('/assets/characters/char_0.png')`) and converts PNG → RGBA → SpriteData using an in-browser PNG decoder (canvas `drawImage` + `getImageData`)
- Furniture sprites are defined inline in `spriteData.ts` (hex color arrays) — no change needed
- The furniture catalog is built dynamically from loaded asset data at runtime via `buildDynamicCatalog()`
- The `vscodeApi.ts` abstraction layer is replaced: all `vscode.postMessage()` calls become HTTP REST calls (`fetch('/api/layout')`, `fetch('/api/settings')`) or WebSocket messages

This replaces pixel-corp's server-push model with a client-pull model. Simpler, and the Go backend doesn't need to understand PNG files.

### Dropped Components

- `DebugView.tsx` — development-only, not ported (can be recreated if needed)
- `SessionInspector.tsx` — replaced by the FeedPanel which provides richer session inspection

### Dashboard Panels (rewritten from claude-monitor)

Claude-monitor's vanilla JS views rewritten as React components:

- **FeedPanel**: Slide-out panel, renders feed entries as React components instead of innerHTML. Type filter state in React, no DOM querying for visibility toggling.
- **FeedEntry**: Individual entry component with expand/collapse, error highlighting, tool grouping.
- **SearchPanel**: Search input + results list with match highlighting, back-to-results navigation.
- **TableView**: Sortable table with column click handlers, React-managed sort state.
- **HistoryView**: Paginated history table, fetches from `/api/history`.
- **ReplayPanel**: Scrubber + controls, EventSource for SSE stream, keyboard shortcuts (Space/R/arrows).

---

## Character ↔ Session Mapping

When a new session appears via WebSocket (`session_new` event):

1. Assign a character skin (round-robin from 6 skins, hue shift after exhausted)
2. Find nearest free seat (Manhattan distance from office center, or parent's seat for subagents)
3. Spawn character at seat position with matrix effect
4. Character enters TYPE state if session is active

When session goes inactive:
1. Character transitions to IDLE
2. Begins wandering behavior (random walks, return to seat periodically)
3. After extended inactivity (configurable, e.g., 10 minutes), despawn with matrix effect

Subagent handling:
- Subagents spawn near their parent character's seat
- Linked visually (same palette + hue shift, name label shows type from meta.json)
- Despawn when subagent session ends

---

## Persistence

| Data | Location | Format |
|------|----------|--------|
| Office layout | `~/.pixel-office/layout.json` | JSON (tiles, furniture, grid size) |
| User settings | `~/.pixel-office/settings.json` | JSON (sound, notifications, budget) |
| Seat assignments | `~/.pixel-office/settings.json` | Map of projectDir → seatID (stable across session restarts) |
| Session history | `~/.pixel-office/history.db` | SQLite |
| Session data | `~/.claude/projects/` | JSONL (read-only, Claude Code owns these) |

---

## Dev Workflow

```bash
make dev      # Starts Go backend (port 7700) + Vite dev server (port 5173) concurrently
              # Vite proxies /api/* and /ws to Go backend
              # Hot module reload for React, auto-rebuild for Go

make build    # npm run build → go build → single binary at ./pixel-office

make test     # go test ./... && npm test
```

---

## What's NOT included

- Force-directed graph view (office replaces it)
- Timeline waterfall view (replay covers this)
- Inline pixel sprites in session cards (entire app is pixel art)
- Asset import pipeline scripts (licensed assets included directly)
- VS Code extension mode (standalone web app only)
- Node.js backend (replaced by Go)

---

## Migration Notes

### From pixel-corp
- Port `webview-ui/src/office/` engine code (canvas, characters, layout, editor, sprites)
- Port `webview-ui/src/hooks/useExtensionMessages.ts` → adapt to our WebSocket format
- Port `webview-ui/public/assets/` (characters, furniture, floors, walls, font)
- Port `webview-ui/src/components/` (BottomToolbar, SettingsModal, ZoomControls)
- Drop: VS Code extension scaffolding, Node.js server, asset import pipeline

### From claude-monitor
- Port all `internal/` Go packages as-is
- Port `cmd/claude-monitor/main.go` → `cmd/pixel-office/main.go` with new routes
- Rewrite frontend views as React components (not ported — cleaner to rebuild)
- Port: session model, parser, watcher, hub, docker, store, replay
- Drop: single HTML file frontend, inline sprites, graph view, timeline view
