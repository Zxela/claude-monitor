# Burn Rate Gauge — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand the budget popover into a "Cost Intelligence" panel with real-time burn rate sparkline, projected daily cost, token velocity, per-session breakdown, and budget progress.

**Architecture:** New `burn-rate.ts` module samples cost/token rates every 5 seconds into a ring buffer. The existing `budget-popover.ts` is rewritten to display burn rate data, sparkline, and per-session breakdown alongside the existing budget settings. All frontend-only — no backend changes.

**Tech Stack:** TypeScript, Canvas 2D (sparkline), existing state/session infrastructure

---

### Task 1: Burn Rate Sampling Module

**Files:**
- Create: `web/src/burn-rate.ts`

- [ ] **Step 1: Create the burn-rate module with types and ring buffer**

Create `web/src/burn-rate.ts`:

```typescript
import { state } from './state';

export interface BurnRateSample {
  timestamp: number;
  costRate: number;
  tokenRate: number;
  totalCost: number;
}

const CAPACITY = 120; // 10 minutes at 5-second intervals
const INTERVAL_MS = 5000;

let samples: BurnRateSample[] = [];
let timer: ReturnType<typeof setInterval> | null = null;
let prevTokenTotal = 0;
let prevTimestamp = 0;

function getActiveSessionTotals(): { costRate: number; tokenTotal: number; totalCost: number } {
  let costRate = 0;
  let tokenTotal = 0;
  let totalCost = 0;
  for (const sess of state.sessions.values()) {
    totalCost += sess.totalCost;
    if (sess.isActive) {
      costRate += sess.costRate;
    }
    tokenTotal += sess.inputTokens + sess.cacheReadTokens + sess.cacheCreationTokens + sess.outputTokens;
  }
  return { costRate, tokenTotal, totalCost };
}

function sample(): void {
  const now = Date.now();
  const { costRate, tokenTotal, totalCost } = getActiveSessionTotals();

  let tokenRate = 0;
  if (prevTimestamp > 0 && now > prevTimestamp) {
    const deltaTokens = tokenTotal - prevTokenTotal;
    const deltaMinutes = (now - prevTimestamp) / 60000;
    if (deltaMinutes > 0 && deltaTokens >= 0) {
      tokenRate = deltaTokens / deltaMinutes;
    }
  }

  prevTokenTotal = tokenTotal;
  prevTimestamp = now;

  samples.push({ timestamp: now, costRate, tokenRate, totalCost });
  if (samples.length > CAPACITY) {
    samples = samples.slice(samples.length - CAPACITY);
  }
}

export function startSampling(): void {
  if (timer) return;
  sample(); // initial sample
  timer = setInterval(sample, INTERVAL_MS);
}

export function stopSampling(): void {
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
}

export function getSamples(): BurnRateSample[] {
  return samples;
}

export function getCurrentRate(): number {
  if (samples.length === 0) return 0;
  return samples[samples.length - 1].costRate;
}

export function getTokenRate(): number {
  if (samples.length === 0) return 0;
  return samples[samples.length - 1].tokenRate;
}

export function getProjectedDailyCost(currentTotalCost: number): number {
  const rate = getCurrentRate(); // $/min
  if (rate <= 0) return currentTotalCost;
  const now = new Date();
  const endOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
  const remainingMinutes = (endOfDay.getTime() - now.getTime()) / 60000;
  return currentTotalCost + rate * remainingMinutes;
}

export function getDepletionMinutes(budget: number, spent: number): number | null {
  const rate = getCurrentRate(); // $/min
  if (rate <= 0 || budget <= spent) return null;
  return (budget - spent) / rate;
}
```

- [ ] **Step 2: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add web/src/burn-rate.ts
git commit -m "feat: add burn rate sampling module with ring buffer"
```

---

### Task 2: Start Sampling at App Init

**Files:**
- Modify: `web/src/main.ts`

- [ ] **Step 1: Add import and start sampling**

Add import at the top of `web/src/main.ts` (after line 18):
```typescript
import { startSampling } from './burn-rate';
```

Add at the end of the `init()` function, before the closing `}` (after `initOnboarding();` on line 175):
```typescript
  startSampling();
```

- [ ] **Step 2: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add web/src/main.ts
git commit -m "feat: start burn rate sampling on app init"
```

---

### Task 3: Rewrite Budget Popover as Cost Intelligence Panel

**Files:**
- Modify: `web/src/components/budget-popover.ts`
- Create: `web/src/styles/burn-rate.css`

This is the main task — replacing the simple budget popover with the full cost intelligence panel.

- [ ] **Step 1: Create burn-rate.css**

Create `web/src/styles/burn-rate.css`:

```css
.burn-rate-panel {
  position: absolute;
  top: 100%;
  right: 0;
  z-index: 100;
  width: 340px;
  background: var(--bg-secondary, #16162a);
  border: 1px solid var(--border, #2a2a4a);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.4);
  font-family: monospace;
  font-size: 12px;
  color: var(--text-primary, #e0e0e0);
  overflow: hidden;
}

.burn-rate-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding: 12px 16px 4px;
}
.burn-rate-label {
  font-size: 10px;
  color: var(--text-secondary, #888);
  letter-spacing: 1px;
}
.burn-rate-value {
  font-size: 18px;
  font-weight: bold;
  color: #4ae68a;
}

.burn-rate-sparkline {
  padding: 4px 16px 0;
}
.burn-rate-sparkline canvas {
  width: 100%;
  height: 50px;
  display: block;
}
.burn-rate-sparkline-labels {
  display: flex;
  justify-content: space-between;
  font-size: 9px;
  color: var(--text-dim, #555);
  padding: 0 0 8px;
}

.burn-rate-metrics {
  padding: 8px 16px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border-top: 1px solid var(--border, #2a2a4a);
}
.burn-rate-metric {
  display: flex;
  justify-content: space-between;
}
.burn-rate-metric-label {
  color: var(--text-secondary, #888);
}
.burn-rate-metric-value {
  color: var(--text-primary, #e0e0e0);
}

.burn-rate-sessions {
  padding: 8px 16px;
  border-top: 1px solid var(--border, #2a2a4a);
}
.burn-rate-sessions-title {
  font-size: 9px;
  color: var(--text-dim, #555);
  letter-spacing: 1px;
  margin-bottom: 6px;
}
.burn-rate-session-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 3px 0;
}
.burn-rate-session-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-right: 8px;
}
.burn-rate-session-rate {
  color: #4ae68a;
  white-space: nowrap;
}
.burn-rate-session-model {
  margin-left: 8px;
  font-size: 10px;
  color: var(--text-dim, #666);
}

.burn-rate-budget {
  padding: 8px 16px;
  border-top: 1px solid var(--border, #2a2a4a);
}
.burn-rate-budget-bar {
  height: 6px;
  background: var(--bg-tertiary, #2a2a4a);
  border-radius: 3px;
  overflow: hidden;
  margin: 6px 0;
}
.burn-rate-budget-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.3s;
}
.burn-rate-budget-text {
  display: flex;
  justify-content: space-between;
  font-size: 10px;
  color: var(--text-secondary, #888);
}
.burn-rate-depletion {
  font-size: 10px;
  color: var(--text-secondary, #888);
  margin-top: 4px;
}

.burn-rate-settings-toggle {
  padding: 8px 16px;
  border-top: 1px solid var(--border, #2a2a4a);
  cursor: pointer;
  font-size: 10px;
  color: var(--text-dim, #666);
  letter-spacing: 1px;
}
.burn-rate-settings-toggle:hover {
  color: var(--text-secondary, #888);
}
.burn-rate-settings {
  padding: 0 16px 12px;
}
.burn-rate-settings input[type="number"] {
  background: var(--bg-tertiary, #2a2a4a);
  border: 1px solid var(--border, #2a2a4a);
  border-radius: 4px;
  color: var(--text-primary, #e0e0e0);
  padding: 4px 8px;
  font-family: monospace;
  font-size: 12px;
  width: 100%;
  margin-bottom: 6px;
}
.burn-rate-settings label {
  display: block;
  margin: 3px 0;
  cursor: pointer;
  font-size: 10px;
  color: var(--text-dim, #666);
}
.burn-rate-settings-actions {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}
.burn-rate-settings-actions button {
  padding: 3px 10px;
  border: 1px solid var(--border, #2a2a4a);
  border-radius: 4px;
  background: var(--bg-tertiary, #2a2a4a);
  color: var(--text-primary, #e0e0e0);
  font-family: monospace;
  font-size: 11px;
  cursor: pointer;
}
.burn-rate-settings-actions button:hover {
  background: var(--bg-hover, #3a3a5a);
}
```

- [ ] **Step 2: Rewrite budget-popover.ts**

Replace the entire contents of `web/src/components/budget-popover.ts`:

```typescript
// web/src/components/budget-popover.ts
import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import { loadSettings, saveSettings, getSettings, notify } from '../notifications';
import { dismiss as dismissCostBreakdown } from './cost-breakdown';
import { getSamples, getCurrentRate, getTokenRate, getProjectedDailyCost, getDepletionMinutes } from '../burn-rate';
import { formatTokens, sessionDisplayName, escapeHtml } from '../utils';
import '../styles/burn-rate.css';

let panel: HTMLElement | null = null;
let sparkCanvas: HTMLCanvasElement | null = null;
let refreshTimer: ReturnType<typeof setInterval> | null = null;

let banner: HTMLElement | null = null;
let costStatEl: HTMLElement | null = null;
let rateStatEl: HTMLElement | null = null;

export function dismiss(): void {
  closePanel();
}

function closePanel(): void {
  if (panel) { panel.remove(); panel = null; }
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
  sparkCanvas = null;
}

export function render(gearBtn: HTMLElement, costEl: HTMLElement, bannerMount: HTMLElement): void {
  costStatEl = costEl;

  // Find the $/MIN stat element for click binding
  const rateStat = gearBtn.closest('.topbar')?.querySelector('[data-stat="rate"]');
  rateStatEl = rateStat as HTMLElement | null;

  banner = document.createElement('div');
  banner.className = 'budget-banner hidden';
  bannerMount.prepend(banner);

  const saved = localStorage.getItem('budget');
  if (saved) {
    update({ budgetThreshold: parseFloat(saved) });
  }

  loadSettings();

  const openPanel = (e: Event) => {
    e.stopPropagation();
    togglePanel(gearBtn);
  };

  gearBtn.addEventListener('click', openPanel);
  gearBtn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openPanel(e); }
  });

  // Make $/MIN stat also open the panel
  if (rateStatEl) {
    rateStatEl.style.cursor = 'pointer';
    rateStatEl.addEventListener('click', openPanel);
  }

  document.addEventListener('click', () => closePanel());

  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('stats') || changed.has('budgetThreshold') || changed.has('budgetDismissed')) {
    checkBudget();
  }
}

function togglePanel(anchor: HTMLElement): void {
  if (panel) {
    closePanel();
    return;
  }
  dismissCostBreakdown();

  panel = document.createElement('div');
  panel.className = 'burn-rate-panel';
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', 'Cost intelligence');
  panel.addEventListener('click', e => e.stopPropagation());

  renderPanelContent();

  anchor.closest('.topbar-stat')!.style.position = 'relative';
  anchor.closest('.topbar-stat')!.appendChild(panel);

  // Refresh panel content every 5 seconds (synced with sampling)
  refreshTimer = setInterval(renderPanelContent, 5000);
}

function renderPanelContent(): void {
  if (!panel) return;

  const rate = getCurrentRate();
  const tokenRate = getTokenRate();
  const totalCost = state.stats?.totalCost ?? 0;
  const projected = getProjectedDailyCost(totalCost);
  const samples = getSamples();

  const activeSessions = Array.from(state.sessions.values()).filter(s => s.isActive);
  const settingsOpen = panel.querySelector('.burn-rate-settings')?.style.display !== 'none'
    && panel.querySelector('.burn-rate-settings') !== null;

  panel.innerHTML = '';

  // Header
  const header = document.createElement('div');
  header.className = 'burn-rate-header';
  header.innerHTML = `
    <span class="burn-rate-label">BURN RATE</span>
    <span class="burn-rate-value">$${rate.toFixed(3)}/min</span>
  `;
  panel.appendChild(header);

  // Sparkline
  const sparkSection = document.createElement('div');
  sparkSection.className = 'burn-rate-sparkline';
  sparkCanvas = document.createElement('canvas');
  sparkCanvas.width = 308;
  sparkCanvas.height = 50;
  sparkSection.appendChild(sparkCanvas);
  const labels = document.createElement('div');
  labels.className = 'burn-rate-sparkline-labels';
  labels.innerHTML = '<span>10 min</span><span>now</span>';
  sparkSection.appendChild(labels);
  panel.appendChild(sparkSection);
  drawSparkline(samples);

  // Metrics
  const metrics = document.createElement('div');
  metrics.className = 'burn-rate-metrics';
  metrics.innerHTML = `
    <div class="burn-rate-metric">
      <span class="burn-rate-metric-label">PROJECTED TODAY</span>
      <span class="burn-rate-metric-value">$${projected.toFixed(2)}</span>
    </div>
    <div class="burn-rate-metric">
      <span class="burn-rate-metric-label">TOKEN VELOCITY</span>
      <span class="burn-rate-metric-value">${formatTokens(Math.round(tokenRate))}/min</span>
    </div>
  `;
  panel.appendChild(metrics);

  // Active sessions
  if (activeSessions.length > 0) {
    const sessSection = document.createElement('div');
    sessSection.className = 'burn-rate-sessions';
    sessSection.innerHTML = `<div class="burn-rate-sessions-title">ACTIVE SESSIONS</div>`;
    for (const sess of activeSessions) {
      const row = document.createElement('div');
      row.className = 'burn-rate-session-row';
      const model = (sess.model || '').replace('claude-', '').replace('-4-6', '');
      row.innerHTML = `
        <span class="burn-rate-session-name">${escapeHtml(sessionDisplayName(sess))}</span>
        <span class="burn-rate-session-rate">$${sess.costRate.toFixed(3)}/min</span>
        <span class="burn-rate-session-model">${escapeHtml(model)}</span>
      `;
      sessSection.appendChild(row);
    }
    panel.appendChild(sessSection);
  }

  // Budget progress (only when threshold set)
  if (state.budgetThreshold) {
    const budget = state.budgetThreshold;
    const pct = Math.min(100, (totalCost / budget) * 100);
    const depletionMin = getDepletionMinutes(budget, totalCost);
    const barColor = pct >= 100 ? '#ff6b6b' : pct >= 80 ? '#ffa64a' : '#4ae68a';

    const budgetSection = document.createElement('div');
    budgetSection.className = 'burn-rate-budget';
    budgetSection.innerHTML = `
      <div class="burn-rate-budget-bar">
        <div class="burn-rate-budget-fill" style="width:${pct}%;background:${barColor}"></div>
      </div>
      <div class="burn-rate-budget-text">
        <span>${pct.toFixed(0)}%</span>
        <span>$${totalCost.toFixed(2)} / $${budget}</span>
      </div>
      ${depletionMin !== null ? `<div class="burn-rate-depletion">Depletes in ~${formatMinutes(depletionMin)} at current rate</div>` : ''}
    `;
    panel.appendChild(budgetSection);
  }

  // Settings toggle
  const toggle = document.createElement('div');
  toggle.className = 'burn-rate-settings-toggle';
  toggle.textContent = (settingsOpen ? '▼' : '▶') + ' Budget Settings';
  panel.appendChild(toggle);

  // Settings section
  const settingsEl = document.createElement('div');
  settingsEl.className = 'burn-rate-settings';
  settingsEl.style.display = settingsOpen ? '' : 'none';
  settingsEl.innerHTML = `
    <input type="number" step="1" placeholder="Daily budget (USD)" value="${state.budgetThreshold ?? ''}" />
    <div class="burn-rate-settings-actions">
      <button class="set-btn">Set</button>
      <button class="clear-btn">Clear</button>
    </div>
    <label><input type="checkbox" class="notif-budget" ${getSettings().budget ? 'checked' : ''} /> Budget exceeded alerts</label>
    <label><input type="checkbox" class="notif-error" ${getSettings().error ? 'checked' : ''} /> Agent error alerts</label>
  `;
  panel.appendChild(settingsEl);

  // Event listeners
  toggle.addEventListener('click', () => {
    const hidden = settingsEl.style.display === 'none';
    settingsEl.style.display = hidden ? '' : 'none';
    toggle.textContent = (hidden ? '▼' : '▶') + ' Budget Settings';
  });

  const input = settingsEl.querySelector('input[type="number"]') as HTMLInputElement;
  settingsEl.querySelector('.set-btn')!.addEventListener('click', () => {
    const val = parseFloat(input.value);
    if (!isNaN(val) && val > 0) {
      localStorage.setItem('budget', String(val));
      update({ budgetThreshold: val, budgetDismissed: false });
      renderPanelContent();
    }
  });
  settingsEl.querySelector('.clear-btn')!.addEventListener('click', () => {
    localStorage.removeItem('budget');
    update({ budgetThreshold: null, budgetDismissed: false });
    renderPanelContent();
  });
  settingsEl.querySelector('.notif-budget')?.addEventListener('change', (e) => {
    const s = getSettings(); s.budget = (e.target as HTMLInputElement).checked; saveSettings(s);
  });
  settingsEl.querySelector('.notif-error')?.addEventListener('change', (e) => {
    const s = getSettings(); s.error = (e.target as HTMLInputElement).checked; saveSettings(s);
  });
}

function drawSparkline(samples: { timestamp: number; costRate: number }[]): void {
  if (!sparkCanvas) return;
  const ctx = sparkCanvas.getContext('2d');
  if (!ctx) return;

  const w = sparkCanvas.width;
  const h = sparkCanvas.height;
  ctx.clearRect(0, 0, w, h);

  if (samples.length < 2) {
    ctx.fillStyle = '#555';
    ctx.font = '10px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('Collecting data...', w / 2, h / 2);
    return;
  }

  const rates = samples.map(s => s.costRate);
  const maxRate = Math.max(...rates, 0.001);
  const minRate = Math.min(...rates);
  const range = maxRate - minRate || 0.001;

  const points: [number, number][] = samples.map((s, i) => [
    (i / (samples.length - 1)) * w,
    h - 4 - ((s.costRate - minRate) / range) * (h - 8),
  ]);

  // Fill
  ctx.beginPath();
  ctx.moveTo(points[0][0], h);
  for (const [x, y] of points) ctx.lineTo(x, y);
  ctx.lineTo(points[points.length - 1][0], h);
  ctx.closePath();
  const grad = ctx.createLinearGradient(0, 0, 0, h);
  grad.addColorStop(0, 'rgba(74, 230, 138, 0.2)');
  grad.addColorStop(1, 'rgba(74, 230, 138, 0)');
  ctx.fillStyle = grad;
  ctx.fill();

  // Line
  ctx.beginPath();
  ctx.moveTo(points[0][0], points[0][1]);
  for (let i = 1; i < points.length; i++) ctx.lineTo(points[i][0], points[i][1]);
  ctx.strokeStyle = '#4ae68a';
  ctx.lineWidth = 1.5;
  ctx.stroke();
}

function formatMinutes(min: number): string {
  if (min < 60) return `${Math.round(min)}m`;
  const h = Math.floor(min / 60);
  const m = Math.round(min % 60);
  return `${h}h ${m}m`;
}

function checkBudget(): void {
  if (!state.budgetThreshold || !costStatEl || !banner) return;

  const total = state.stats?.totalCost ?? 0;

  if (total >= state.budgetThreshold) {
    costStatEl.classList.add('over-budget');
    if (!state.budgetDismissed) {
      banner.className = 'budget-banner';
      banner.innerHTML = `Budget exceeded: $${total.toFixed(0)} / $${state.budgetThreshold}
        <button style="background:none;border:none;color:var(--red);cursor:pointer;font-family:var(--font-mono)" aria-label="Dismiss budget warning">✕</button>`;
      banner.querySelector('button')!.addEventListener('click', () => {
        update({ budgetDismissed: true });
      });
      notify('budget', 'Budget Exceeded', `Total spend $${total.toFixed(0)} exceeds $${state.budgetThreshold}`);
    } else {
      banner.className = 'budget-banner hidden';
    }
  } else {
    costStatEl.classList.remove('over-budget');
    banner.className = 'budget-banner hidden';
  }
}
```

- [ ] **Step 3: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add web/src/components/budget-popover.ts web/src/styles/burn-rate.css
git commit -m "feat: expand budget popover into cost intelligence panel with burn rate"
```

---

### Task 4: Make $/MIN Stat Clickable in Topbar

**Files:**
- Modify: `web/src/components/topbar.ts`
- Modify: `web/src/main.ts`

- [ ] **Step 1: Add a CSS class to the $/MIN stat for click targeting**

In `web/src/components/topbar.ts`, find line 40:
```html
<div class="topbar-stat" title="Aggregate cost velocity across all active sessions"><span>$/MIN</span> <span class="val yellow" data-stat="rate">—</span></div>
```

Change to:
```html
<div class="topbar-stat rate-stat" title="Aggregate cost velocity across all active sessions"><span>$/MIN</span> <span class="val yellow" data-stat="rate">—</span></div>
```

- [ ] **Step 2: Update main.ts to pass the rate stat to budget popover**

In `web/src/main.ts`, find the budget popover setup (lines 42-46):
```typescript
const gearBtn = topbarMount.querySelector<HTMLElement>('.budget-gear');
const costStat = topbarMount.querySelector<HTMLElement>('[data-stat="cost"]');
if (gearBtn && costStat) {
  renderBudget(gearBtn, costStat, document.getElementById('app')!);
}
```

The budget-popover already finds the rate stat via `gearBtn.closest('.topbar')?.querySelector('[data-stat="rate"]')` — no main.ts change needed beyond ensuring the topbar renders first (which it already does).

- [ ] **Step 3: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add web/src/components/topbar.ts
git commit -m "feat: add rate-stat class for click targeting"
```

---

### Task 5: Full Build and Smoke Test

**Files:** None (verification only)

- [ ] **Step 1: Full build**

Run: `make build`
Expected: Clean build

- [ ] **Step 2: Run test suite**

Run: `make test`
Expected: All tests pass

- [ ] **Step 3: Manual smoke test**

Run: `./claude-monitor`
Open: `http://localhost:7700`

Verify:
1. Click the ⚙ gear icon — cost intelligence panel opens with burn rate, sparkline, metrics
2. Click the $/MIN stat — same panel opens
3. Sparkline shows "Collecting data..." initially, then draws a line after ~10 seconds
4. Per-session list shows active sessions with individual rates
5. Projected today updates as burn rate changes
6. Token velocity shows a value when sessions are active
7. Budget section appears when a threshold is set
8. Budget Settings collapses/expands
9. Setting/clearing budget threshold still works
10. Panel closes on Escape and click-outside
11. Pressing `a` to switch to Analytics and back doesn't break anything

- [ ] **Step 4: Commit any fixes found during smoke test**

```bash
git add -A
git commit -m "fix: address issues found during burn rate smoke test"
```

(Skip if no fixes needed.)
