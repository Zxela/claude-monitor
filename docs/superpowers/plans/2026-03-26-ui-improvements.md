# UI Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three independent UI fixes: compact card stats, agent sequence view, responsive hamburger header.

**Architecture:** All frontend-only changes in TypeScript and CSS. No backend modifications needed.

**Tech Stack:** TypeScript, vanilla DOM, CSS

---

### Task 1: Compact card stats + sidebar width

**Files:**
- Modify: `web/src/components/session-card.ts` (renderCompact function)
- Modify: `web/src/styles/sessions.css` (width values)

- [ ] **Step 1: Change sidebar width from 270px to 283px**

In `web/src/styles/sessions.css`, find and replace all instances of `270px` with `283px` in the `.sessions-panel` rule.

- [ ] **Step 2: Add stats line to renderCompact**

In `web/src/components/session-card.ts`, find the `renderCompact` function. Currently it renders a single-line flex layout with: dot, name, chevron, cost, timeAgo, duration, model.

Add a second line below the main line showing: token count, cache hit %, error count (if > 0), cost rate. Use the same dim styling as the expanded card stats.

After the main compact content div, add a stats span:

```typescript
// Add stats line
const stats = document.createElement('div');
stats.className = 'compact-stats';
const totalTokens = sess.inputTokens + sess.outputTokens + sess.cacheReadTokens;
let statsHtml = `<span class="compact-stat">${formatTokens(totalTokens)} tok</span>`;
if (sess.cacheHitPct > 0) {
  statsHtml += `<span class="compact-stat">${Math.round(sess.cacheHitPct)}% cache</span>`;
}
if (sess.errorCount > 0) {
  statsHtml += `<span class="compact-stat compact-stat-err">${sess.errorCount} err</span>`;
}
if (sess.costRate > 0) {
  statsHtml += `<span class="compact-stat">$${sess.costRate.toFixed(2)}/min</span>`;
}
stats.innerHTML = statsHtml;
card.appendChild(stats);
```

- [ ] **Step 3: Add CSS for compact stats**

In `web/src/styles/sessions.css`, add:

```css
.compact-stats {
  display: flex;
  gap: 8px;
  padding: 1px 10px 2px 18px;
  font-size: 9px;
  color: var(--text-dim, #666);
}

.compact-stat-err {
  color: var(--red, #ff4444);
}
```

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`

- [ ] **Step 5: Commit**

```bash
git add web/src/components/session-card.ts web/src/styles/sessions.css
git commit -m "feat: show token/cache/error stats in compact cards, widen sidebar to 283px"
```

---

### Task 2: Agent sequence view in graph panel

**Files:**
- Modify: `web/src/components/graph-view.ts`
- Modify: `web/src/styles/views.css`

- [ ] **Step 1: Add Graph/Sequence toggle to graph view**

In `graph-view.ts`, when the graph view is shown, add a toggle bar at the top with two buttons: "Graph" and "Sequence". Track which mode is active with a module-level variable `let graphMode: 'graph' | 'sequence' = 'graph'`.

When "Graph" is selected, show the existing canvas-based force graph. When "Sequence" is selected, hide the canvas and show a vertical chronological list.

- [ ] **Step 2: Implement sequence rendering**

The sequence view shows all visible sessions (active + recent) in chronological order by `startedAt`. Each entry is a div with:
- Timestamp (HH:MM:SS)
- Status indicator dot (same colors as graph nodes)
- Session/agent name
- Cost
- Duration or "active" if still running
- Indentation based on depth (children indented under parents via parentId)

Use the same session data already available in the graph view (it reads from `state.sessions`).

```typescript
function renderSequence(container: HTMLElement): void {
  const sessions = Array.from(state.sessions.values())
    .filter(s => s.isActive || (Date.now() - new Date(s.lastActive).getTime()) < 120000)
    .sort((a, b) => new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime());

  const list = document.createElement('div');
  list.className = 'sequence-list';

  for (const sess of sessions) {
    const depth = sess.parentId ? 1 : 0; // flatten to 1 level
    const entry = document.createElement('div');
    entry.className = `sequence-entry${sess.isActive ? ' sequence-active' : ''}`;
    entry.style.paddingLeft = `${12 + depth * 24}px`;

    const time = new Date(sess.startedAt).toLocaleTimeString();
    const name = sess.sessionName || sess.projectName || sess.id.slice(0, 8);
    const cost = `$${sess.totalCostUSD.toFixed(2)}`;
    const statusClass = sess.isActive
      ? (sess.status === 'thinking' ? 'thinking' : sess.status === 'tool_use' ? 'tool-use' : 'active')
      : 'idle';

    entry.innerHTML = `
      <span class="sequence-time">${time}</span>
      <span class="sequence-dot ${statusClass}"></span>
      ${depth > 0 ? '<span class="sequence-connector">└─</span>' : ''}
      <span class="sequence-name">${name}</span>
      <span class="sequence-cost">${cost}</span>
      <span class="sequence-status">${sess.isActive ? sess.status : 'done'}</span>
    `;

    entry.addEventListener('click', () => {
      update({ selectedSessionId: sess.id, view: 'list' });
    });
    entry.style.cursor = 'pointer';
    list.appendChild(entry);
  }

  container.appendChild(list);
}
```

- [ ] **Step 3: Add CSS for sequence view and toggle**

In `web/src/styles/views.css`, add:

```css
/* Graph/Sequence toggle */
.graph-mode-toggle {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 10;
  display: flex;
  gap: 0;
  border: 1px solid var(--border, #333);
  border-radius: 4px;
  overflow: hidden;
}

.graph-mode-btn {
  padding: 3px 10px;
  font-size: 10px;
  font-family: var(--font-mono, monospace);
  background: var(--bg-card, #1a1a2e);
  color: var(--text-dim, #666);
  border: none;
  cursor: pointer;
  letter-spacing: 0.5px;
}

.graph-mode-btn.active {
  background: var(--bg-hover, #2a2a3e);
  color: var(--cyan, #00d4ff);
}

/* Sequence view */
.sequence-list {
  padding: 8px 0;
  overflow-y: auto;
  height: 100%;
}

.sequence-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px;
  font-size: 12px;
  color: var(--text, #ccc);
  border-left: 2px solid transparent;
}

.sequence-entry:hover {
  background: var(--bg-hover, #2a2a3e);
}

.sequence-active {
  border-left-color: var(--cyan, #00d4ff);
}

.sequence-time {
  color: var(--text-dim, #666);
  font-size: 10px;
  min-width: 70px;
}

.sequence-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.sequence-dot.thinking { background: #ffcc00; }
.sequence-dot.tool-use { background: #4488ff; }
.sequence-dot.active { background: #00ff88; }
.sequence-dot.idle { background: #44445a; }

.sequence-connector {
  color: var(--text-dim, #666);
  font-size: 10px;
}

.sequence-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sequence-cost {
  color: var(--yellow, #ffcc00);
  font-size: 11px;
}

.sequence-status {
  font-size: 9px;
  color: var(--text-dim, #666);
  text-transform: uppercase;
  min-width: 50px;
  text-align: right;
}
```

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`

- [ ] **Step 5: Commit**

```bash
git add web/src/components/graph-view.ts web/src/styles/views.css
git commit -m "feat: add agent sequence view with Graph/Sequence toggle

Shows chronological list of agent interactions with status dots,
timestamps, costs, and parent-child indentation. Toggle between
force-directed graph and sequential timeline."
```

---

### Task 3: Responsive hamburger header

**Files:**
- Modify: `web/src/components/topbar.ts`
- Modify: `web/src/styles/base.css`

- [ ] **Step 1: Add hamburger button to topbar**

In `web/src/components/topbar.ts`, in the render function, add a hamburger button element to the topbar HTML. Place it after the brand span:

```typescript
const hamburger = document.createElement('button');
hamburger.className = 'topbar-hamburger';
hamburger.innerHTML = '☰';
hamburger.setAttribute('aria-label', 'Toggle menu');
hamburger.setAttribute('aria-expanded', 'false');
```

Wrap the stats, search, and view toggle in a div with class `topbar-collapsible`. Add click handler on hamburger to toggle the `open` class on this wrapper and update `aria-expanded`.

```typescript
hamburger.addEventListener('click', () => {
  const collapsible = container.querySelector('.topbar-collapsible')!;
  const isOpen = collapsible.classList.toggle('open');
  hamburger.setAttribute('aria-expanded', String(isOpen));
});
```

Add Escape key listener to close:
```typescript
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    const collapsible = container.querySelector('.topbar-collapsible');
    if (collapsible?.classList.contains('open')) {
      collapsible.classList.remove('open');
      hamburger.setAttribute('aria-expanded', 'false');
    }
  }
});
```

- [ ] **Step 2: Add responsive CSS for hamburger**

In `web/src/styles/base.css`, update the media queries:

```css
/* Hamburger — hidden by default, shown on mobile */
.topbar-hamburger {
  display: none;
  background: none;
  border: none;
  color: var(--fg, #c9d1d9);
  font-size: 20px;
  cursor: pointer;
  padding: 0 8px;
  line-height: 1;
}

@media (max-width: 768px) {
  .topbar-hamburger {
    display: block;
  }

  .topbar-collapsible {
    display: none;
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    background: var(--bg-card, #1a1a2e);
    border-bottom: 1px solid var(--border, #333);
    padding: 8px;
    z-index: 100;
    flex-direction: column;
    gap: 8px;
  }

  .topbar-collapsible.open {
    display: flex;
  }

  .topbar {
    position: relative;
    flex-wrap: nowrap;
  }
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`

- [ ] **Step 4: Commit**

```bash
git add web/src/components/topbar.ts web/src/styles/base.css
git commit -m "feat: responsive hamburger menu for topbar on mobile

Stats, search, and view buttons collapse into a dropdown panel
below 768px. Hamburger toggles visibility, Escape closes it."
```
