// web/src/components/budget-popover.ts
import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import { loadSettings, saveSettings, getSettings, notify } from '../notifications';
import type { StatsWindow } from '../api';
import { dismiss as dismissCostBreakdown } from './cost-breakdown';
import {
  getSamples,
  getCurrentRate,
  getTokenRate,
  getProjectedDailyCost,
  getDepletionMinutes,
} from '../burn-rate';
import { formatTokens, sessionDisplayName, escapeHtml } from '../utils';
import '../styles/burn-rate.css';

let panel: HTMLElement | null = null;
let sparkCanvas: HTMLCanvasElement | null = null;
let refreshTimer: ReturnType<typeof setInterval> | null = null;

let banner: HTMLElement | null = null;
let costStatEl: HTMLElement | null = null;
let rateStatEl: HTMLElement | null = null;
let budgetNotificationSent = false;

export function dismiss(): void {
  closePanel();
}

function closePanel(): void {
  if (panel) {
    panel.remove();
    panel = null;
  }
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
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

  const savedWindow = localStorage.getItem('budget-window') as StatsWindow | null;
  if (savedWindow) {
    update({ statsWindow: savedWindow });
  }

  loadSettings();

  const openFromGear = (e: Event) => {
    e.stopPropagation();
    togglePanel(gearBtn);
  };

  gearBtn.addEventListener('click', openFromGear);
  gearBtn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openFromGear(e);
    }
  });

  // Make $/MIN stat also open the panel, anchored to the rate stat
  if (rateStatEl) {
    rateStatEl.style.cursor = 'pointer';
    rateStatEl.addEventListener('click', (e) => {
      e.stopPropagation();
      togglePanel(rateStatEl!);
    });
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
  panel.addEventListener('click', (e) => e.stopPropagation());

  renderPanelContent();

  const statEl = anchor.closest('.topbar-stat') as HTMLElement;
  statEl.style.position = 'relative';
  statEl.appendChild(panel);

  // Refresh dynamic values every 5 seconds (synced with sampling)
  refreshTimer = setInterval(updatePanelValues, 5000);
}

function renderPanelContent(): void {
  if (!panel) return;

  const rate = getCurrentRate();
  const tokenRate = getTokenRate();
  const totalCost = state.stats?.totalCost ?? 0;
  const projected = getProjectedDailyCost(totalCost);
  const samples = getSamples();

  const activeSessions = Array.from(state.sessions.values()).filter((s) => s.isActive);
  const prevSettings = panel.querySelector('.burn-rate-settings') as HTMLElement | null;
  const settingsOpen = prevSettings !== null && prevSettings.style.display !== 'none';

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
  toggle.textContent = (settingsOpen ? '\u25BC' : '\u25B6') + ' Budget Settings';
  panel.appendChild(toggle);

  // Settings section
  const settingsEl = document.createElement('div');
  settingsEl.className = 'burn-rate-settings';
  settingsEl.style.display = settingsOpen ? '' : 'none';
  settingsEl.innerHTML = `
    <div class="burn-rate-budget-window">
      <label for="budget-window-select">Budget window</label>
      <select id="budget-window-select" class="budget-window-select">
        <option value="today" ${state.statsWindow === 'today' ? 'selected' : ''}>Today</option>
        <option value="week" ${state.statsWindow === 'week' ? 'selected' : ''}>This week</option>
        <option value="month" ${state.statsWindow === 'month' ? 'selected' : ''}>This month</option>
        <option value="all" ${state.statsWindow === 'all' ? 'selected' : ''}>All time</option>
      </select>
    </div>
    <input type="number" step="1" placeholder="Budget (USD)" value="${state.budgetThreshold ?? ''}" />
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
    toggle.textContent = (hidden ? '\u25BC' : '\u25B6') + ' Budget Settings';
  });

  const input = settingsEl.querySelector('input[type="number"]') as HTMLInputElement;
  settingsEl.querySelector('.set-btn')!.addEventListener('click', () => {
    const val = parseFloat(input.value);
    if (!isNaN(val) && val > 0) {
      localStorage.setItem('budget', String(val));
      budgetNotificationSent = false;
      update({ budgetThreshold: val, budgetDismissed: false });
      renderPanelContent();
    }
  });
  settingsEl.querySelector('.clear-btn')!.addEventListener('click', () => {
    localStorage.removeItem('budget');
    budgetNotificationSent = false;
    update({ budgetThreshold: null, budgetDismissed: false });
    renderPanelContent();
  });
  settingsEl.querySelector('.notif-budget')?.addEventListener('change', (e) => {
    const s = getSettings();
    s.budget = (e.target as HTMLInputElement).checked;
    saveSettings(s);
  });
  settingsEl.querySelector('.notif-error')?.addEventListener('change', (e) => {
    const s = getSettings();
    s.error = (e.target as HTMLInputElement).checked;
    saveSettings(s);
  });
  settingsEl.querySelector('.budget-window-select')?.addEventListener('change', (e) => {
    const win = (e.target as HTMLSelectElement).value as StatsWindow;
    localStorage.setItem('budget-window', win);
    update({ statsWindow: win, budgetDismissed: false });
    budgetNotificationSent = false;
  });
}

function updatePanelValues(): void {
  if (!panel) return;

  const rate = getCurrentRate();
  const tokenRate = getTokenRate();
  const totalCost = state.stats?.totalCost ?? 0;
  const projected = getProjectedDailyCost(totalCost);
  const samples = getSamples();

  // Update burn rate value
  const rateVal = panel.querySelector('.burn-rate-value');
  if (rateVal) rateVal.textContent = `$${rate.toFixed(3)}/min`;

  // Update metrics
  const metricValues = panel.querySelectorAll('.burn-rate-metric-value');
  if (metricValues[0]) metricValues[0].textContent = `$${projected.toFixed(2)}`;
  if (metricValues[1]) metricValues[1].textContent = `${formatTokens(Math.round(tokenRate))}/min`;

  // Redraw sparkline
  drawSparkline(samples);

  // Update active sessions
  const activeSessions = Array.from(state.sessions.values()).filter((s) => s.isActive);
  const sessSection = panel.querySelector('.burn-rate-sessions');
  if (sessSection) {
    const rows = sessSection.querySelectorAll('.burn-rate-session-row');
    // If count changed, do a targeted rebuild of just the sessions section
    if (rows.length !== activeSessions.length) {
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
    } else {
      rows.forEach((row, i) => {
        const sess = activeSessions[i];
        const rateEl = row.querySelector('.burn-rate-session-rate');
        if (rateEl) rateEl.textContent = `$${sess.costRate.toFixed(3)}/min`;
      });
    }
  }

  // Update budget progress
  if (state.budgetThreshold) {
    const budget = state.budgetThreshold;
    const pct = Math.min(100, (totalCost / budget) * 100);
    const barColor = pct >= 100 ? '#ff6b6b' : pct >= 80 ? '#ffa64a' : '#4ae68a';
    const depletionMin = getDepletionMinutes(budget, totalCost);
    const fill = panel.querySelector('.burn-rate-budget-fill') as HTMLElement | null;
    if (fill) {
      fill.style.width = `${pct}%`;
      fill.style.background = barColor;
    }
    const budgetText = panel.querySelector('.burn-rate-budget-text');
    if (budgetText)
      budgetText.innerHTML = `<span>${pct.toFixed(0)}%</span><span>$${totalCost.toFixed(2)} / $${budget}</span>`;
    const depletion = panel.querySelector('.burn-rate-depletion');
    if (depletion)
      depletion.textContent =
        depletionMin !== null ? `Depletes in ~${formatMinutes(depletionMin)} at current rate` : '';
  }
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

  const rates = samples.map((s) => s.costRate);
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
        budgetNotificationSent = false;
      });
      if (!budgetNotificationSent) {
        budgetNotificationSent = true;
        notify(
          'budget',
          'Budget Exceeded',
          `Total spend $${total.toFixed(0)} exceeds $${state.budgetThreshold}`,
        );
      }
    } else {
      banner.className = 'budget-banner hidden';
    }
  } else {
    costStatEl.classList.remove('over-budget');
    banner.className = 'budget-banner hidden';
    budgetNotificationSent = false;
  }
}
