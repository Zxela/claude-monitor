# Schema Redesign — Data Model v2

Status: IMPLEMENTED (feature/data-model-v2 branch)

## Decisions

1. **"Message" = conversation turn only** (assistant/user/human). All JSONL lines are "events."
2. **`projectName` replaced by `repo_id`** — repo identity from git remote, separated from session display name.
3. **Repo identity** = normalized git remote URL resolved at ingest, fallbacks: git toplevel basename → cwd basename. Cached per cwd.
4. **Costs are per-session, fully independent** — parent does NOT aggregate child costs. Stats must include children.
5. **Parent-child is the only relationship** — derived from file path. Team agents and subagents are identical at the JSONL level.
6. **Store all JSONL lines** as events (not just conversation turns) for full audit trail.
7. **Dedup to final state** per message_id (streaming chunks replaced, not appended).
8. **Tiered content storage** — hot/warm/cold with configurable retention (default 30/90 days).
9. **Denormalized aggregates on sessions** — maintained by Go, source of truth is events table.

## Schema

```sql
-- Repo identity (stable across worktrees/machines)
CREATE TABLE repos (
    id          TEXT PRIMARY KEY,  -- normalized git remote or fallback
    name        TEXT NOT NULL,     -- human-readable ("claude-monitor")
    url         TEXT,              -- full remote URL, NULL for local-only
    first_seen  TEXT NOT NULL      -- RFC3339
);

-- Session = one Claude Code conversation
CREATE TABLE sessions (
    id                    TEXT PRIMARY KEY,
    repo_id               TEXT REFERENCES repos(id),
    parent_id             TEXT,  -- self-ref for subagents
    session_name          TEXT,  -- custom title or agent name
    task_description      TEXT,
    cwd                   TEXT,
    branch                TEXT,
    model                 TEXT,
    started_at            TEXT,
    ended_at              TEXT,
    -- denormalized aggregates (maintained by Go on each event)
    total_cost            REAL DEFAULT 0,
    input_tokens          INTEGER DEFAULT 0,
    output_tokens         INTEGER DEFAULT 0,
    cache_read_tokens     INTEGER DEFAULT 0,
    cache_creation_tokens INTEGER DEFAULT 0,
    message_count         INTEGER DEFAULT 0,  -- conversation turns only
    event_count           INTEGER DEFAULT 0,  -- all JSONL lines
    error_count           INTEGER DEFAULT 0
);
CREATE INDEX idx_sessions_repo    ON sessions(repo_id);
CREATE INDEX idx_sessions_parent  ON sessions(parent_id);
CREATE INDEX idx_sessions_ended   ON sessions(ended_at DESC);

-- Every JSONL line, deduped to final state per message_id
CREATE TABLE events (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id            TEXT NOT NULL REFERENCES sessions(id),
    uuid                  TEXT,      -- JSONL uuid
    message_id            TEXT,      -- JSONL message.id (for dedup via streaming)
    type                  TEXT,      -- assistant, user, progress, custom-title, etc.
    role                  TEXT,
    content_preview       TEXT,      -- 200 char truncated (always kept)
    tool_name             TEXT,
    tool_detail           TEXT,
    cost_usd              REAL DEFAULT 0,
    input_tokens          INTEGER DEFAULT 0,
    output_tokens         INTEGER DEFAULT 0,
    cache_read_tokens     INTEGER DEFAULT 0,
    cache_creation_tokens INTEGER DEFAULT 0,
    model                 TEXT,
    is_error              BOOLEAN DEFAULT 0,
    stop_reason           TEXT,
    hook_event            TEXT,
    hook_name             TEXT,
    tool_use_id           TEXT,
    for_tool_use_id       TEXT,
    is_agent              BOOLEAN DEFAULT 0,
    timestamp             TEXT NOT NULL
);
CREATE INDEX idx_events_session   ON events(session_id, timestamp);
CREATE INDEX idx_events_tool      ON events(tool_name);
CREATE INDEX idx_events_timestamp ON events(timestamp);
CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, message_id)
    WHERE message_id IS NOT NULL;

-- Full content, separate table for tiered retention
CREATE TABLE event_content (
    event_id    INTEGER PRIMARY KEY REFERENCES events(id),
    tier        TEXT DEFAULT 'hot',  -- hot | warm (cold = row deleted)
    content     TEXT,                -- full text (hot tier)
    compressed  BLOB                 -- gzip bytes (warm tier)
);

-- FTS5 on always-available fields (survives all retention tiers)
CREATE VIRTUAL TABLE events_fts USING fts5(
    content_preview, tool_name, tool_detail,
    content=events,
    content_rowid=id
);
```

## Retention tiers

Configurable via settings. Defaults:

| Tier | Age | event_content state | Searchable via |
|------|-----|---------------------|----------------|
| Hot  | < 30 days | content TEXT populated | FTS5 + direct scan |
| Warm | 30–90 days | content NULL, compressed BLOB (gzip) | FTS5 (preview only) + decompress scan |
| Cold | > 90 days | row deleted | FTS5 (preview + tool_detail only) |

events and sessions tables are never pruned.

## Dedup strategy

Events with a `message_id` use INSERT ... ON CONFLICT(session_id, message_id) DO UPDATE
to replace streaming chunks with the final state. Events without a message_id
(progress, hooks, metadata) insert as separate rows.

## Cost aggregation

- Session aggregates maintained by Go code on each event insert.
- Stats endpoint sums across sessions (including children).
- No more `WHERE parent_id = ''` filter — all sessions included in totals.
- Per-repo cost: `SELECT SUM(total_cost) FROM sessions WHERE repo_id = ?`

## Repo identity resolution

At ingest, when a new `cwd` is seen:
1. `git -C <cwd> remote get-url origin` → normalize URL → repo id
2. Fallback: `git -C <cwd> rev-parse --show-toplevel` → basename
3. Fallback: `basename(cwd)`
4. Container sessions (git unavailable): `label / basename(cwd)`

Cached per cwd in event processor. Upserted into repos table.

### cwd → repo cache (persistent)

```sql
CREATE TABLE cwd_repos (
    cwd     TEXT PRIMARY KEY,
    repo_id TEXT REFERENCES repos(id)
);
```

- On startup: load cwd_repos into in-memory map (instant repo resolution)
- On new cwd: git resolve (2s timeout) → upsert repos + cwd_repos → cache in memory
- Cache-clear endpoint: `DELETE /api/cache/repos` (clears table + memory)
- No TTL — mapping is stable. Manual clear for the rare case of a path reuse.

## Pipeline architecture

```
                                   ┌→ Broadcast (immediate, from memory)
Parse → Resolve → Apply Session → ─┤
                                   └→ Buffer → Batch Persist (1-2s flush)
```

| Stage | Input | Output | Responsibility |
|-------|-------|--------|----------------|
| Parse | JSONL line bytes | Event struct | Unmarshal, extract content, compute cost |
| Resolve Repo | cwd + label | Repo | Git remote → normalized ID, cached |
| Apply Session | Event + Repo | Session + isNew | In-memory aggregates, parent detection |
| Broadcast | Event + Session | — | WebSocket + SSE push (immediate) |
| Buffer → Batch Persist | Buffered events | — | Micro-batch INSERT, session UPDATE (1-2s) |

Micro-batch flush: single transaction per batch containing all event INSERTs,
event_content INSERTs, FTS5 updates, and session aggregate UPDATEs.

## Go types

```
parser.Event          — one parsed JSONL line (renamed from ParsedMessage)
session.Session       — live in-memory state (runtime fields + aggregates)
store.SessionRow      — DB representation of a session
store.EventRow        — DB representation of an event
repo.Repo             — repo identity (ID, Name, URL)
```

## Migration strategy

Drop existing `session_history` table. Start fresh with new schema.
Re-ingest from JSONL files on disk (watcher bootstrap handles this).
Historical data with polluted `projectName` is not worth migrating.

## Frontend types

Single unified `Session` type replaces both `Session` and `HistoryRow`:

```typescript
interface Repo {
  id: string;          // "github.com/Zxela/claude-monitor"
  name: string;        // "claude-monitor"
  url?: string;        // full remote URL
}

interface Session {
  id: string;
  repo: Repo;
  parentId?: string;
  children?: string[];
  sessionName?: string;
  taskDescription: string;
  cwd?: string;
  branch?: string;
  model?: string;
  startedAt: string;
  endedAt?: string;
  durationSeconds?: number;
  // aggregates
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  messageCount: number;     // conversation turns only
  eventCount: number;       // all JSONL lines
  errorCount: number;
  // runtime (live sessions only)
  isActive: boolean;
  status: 'idle' | 'thinking' | 'tool_use' | 'waiting';
  costRate: number;
}

interface Event {
  id: number;
  sessionId: string;
  type: string;
  role: string;
  contentPreview: string;
  fullContent?: string;     // from event_content, null if warm/cold
  toolName?: string;
  toolDetail?: string;
  costUSD: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  model?: string;
  isError: boolean;
  stopReason?: string;
  hookEvent?: string;
  hookName?: string;
  toolUseId?: string;
  forToolUseId?: string;
  isAgent?: boolean;
  timestamp: string;
}

interface Stats {
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  sessionCount: number;
  activeSessions: number;
  cacheHitPct: number;
  costRate: number;
  costByModel: Record<string, number>;
  costByRepo: Record<string, number>;
}

interface SearchResult {
  event: Event;
  sessionId: string;
  sessionName?: string;
  repoName: string;
}

interface StorageInfo {
  totalSizeBytes: number;
  hotContentBytes: number;
  warmContentBytes: number;
  eventCount: number;
  retentionDays: { hot: number; warm: number; };
}
```

Removed types: `HistoryRow` (merged into Session), `GroupedSessions` (frontend derives from Session[])
Removed fields: `projectDir`, `projectName`, `filePath`, `isSubagent`, `cacheHitPct` (derived in frontend)

## API surface

```
GET  /api/sessions                 — all sessions, flat (active + historical)
                                     ?active=true    — active only
                                     ?repo={id}      — filter by repo
                                     ?group=activity — bucket by activity period
                                     ?limit=50&offset=0
GET  /api/sessions/{id}            — single session
GET  /api/sessions/{id}/events     — events for a session (paginated)
                                     ?last=50        — most recent N
                                     ?type=assistant  — filter by type
                                     ?tool=Bash       — filter by tool
GET  /api/sessions/{id}/replay     — events with timing for playback (from DB)

GET  /api/repos                    — all repos with cost rollups
GET  /api/repos/{id}/stats         — per-repo cost/usage stats
GET  /api/repos/{id}/sessions      — sessions for a repo

GET  /api/stats?window=            — aggregate stats (all sessions incl. children)
                                     window: all | today | week | month

GET  /api/search?q=               — FTS5 search (preview + tool_detail)
GET  /api/search/full?q=          — full content scan (event_content, slower)

GET  /api/storage                  — DB size, tier breakdown, retention config
DELETE /api/cache/repos            — clear cwd → repo cache

WebSocket /ws                      — real-time events (unchanged)
```

Removed endpoints: `/api/history` (merged into /api/sessions),
`/api/projects` (replaced by /api/repos),
`/api/sessions/grouped` (query param on /api/sessions)

## WebSocket events

Every JSONL line triggers a lightweight session update. Meaningful events
additionally carry the full event payload.

```
{"event": "session_new",    "session": Session}
{"event": "session_update", "session": Session}
{"event": "event",          "session": Session, "data": Event}
```

- `session_update` — sent on EVERY JSONL line (cost/status/tokens ticking)
- `event` — sent for all lines EXCEPT noise (exclusion list below)
- `session_new` — first event for a session

Excluded from full event broadcast (session_update still sent):
- `progress` lines without hook data (streaming chunks)
- `system`, `file-history-snapshot`, `custom-title`, `agent-name`

Unknown/new types default to sending — safer than accidentally hiding.

```go
skipDetail := (event.Type == "progress" && event.HookEvent == "") ||
              event.Type == "system" ||
              event.Type == "file-history-snapshot" ||
              event.Type == "custom-title" ||
              event.Type == "agent-name"
```

## Settings storage

```sql
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Defaults inserted on schema creation
INSERT INTO settings VALUES ('retention_hot_days', '30');
INSERT INTO settings VALUES ('retention_warm_days', '90');
```

Settings live in the DB (travels with the data). CLI flags can override per-run.

```
GET  /api/settings              — current settings
PUT  /api/settings/{key}        — update a setting
```

## Bootstrap

On first startup (or after schema reset), the watcher reads all existing JSONL
files on disk and processes them through the full pipeline.

- One transaction per file (~758 files, ~100 events each)
- Git resolution cached after first hit per unique cwd (~30 calls)
- Progress logged to stdout: `Bootstrapping: 127/758 sessions...`
- HTTP server starts after bootstrap completes
- Estimated time: 2-5 minutes for current data volume

```go
for _, file := range files {
    events := parseAllLines(file)
    repo := resolveRepo(events[0].CWD)
    for _, event := range events {
        applySession(event, repo)
    }
    batchPersist(events)
    log.Printf("Bootstrap: %d/%d sessions", i+1, len(files))
}
```

## Storage estimates (heavy usage, 10 sessions/day)

| Period | Without compression | With tiered retention |
|--------|--------------------|-----------------------|
| 1 month | 39 MB | 39 MB |
| 1 year | 476 MB | ~177 MB |
| 3 years | 1.4 GB | ~500 MB |
</content>
</invoke>