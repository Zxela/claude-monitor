import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import { fetchStats } from '../api';
import type { StatsWindow } from '../api';
import { toggle as toggleCostBreakdown } from './cost-breakdown';
import { getAttentionCount } from '../attention';
import '../styles/topbar.css';

let el: HTMLElement | null = null;
let searchInput: HTMLInputElement | null = null;
let renderAbort: AbortController | null = null;
let statsTimer: ReturnType<typeof setInterval> | null = null;

let statActive: HTMLElement | null = null;
let statCost: HTMLElement | null = null;
let statCache: HTMLElement | null = null;
let statRate: HTMLElement | null = null;

export function render(container: HTMLElement): void {
  // Abort previous document-level listeners to prevent accumulation on re-render
  if (renderAbort) renderAbort.abort();
  renderAbort = new AbortController();
  const signal = renderAbort.signal;

  el = document.createElement('div');
  el.className = 'topbar';
  el.innerHTML = `
    <div class="topbar-brand">
      <span class="brand-diamond">◆</span>
      CLAUDE MONITOR
    </div>
    <div class="topbar-stat"><span>ACTIVE</span> <span class="val green" data-stat="active">0</span></div>
    <div class="topbar-stat" title="Total cost across all sessions"><span>TOTAL SPEND</span> <span class="budget-gear" role="button" tabindex="0" aria-label="Open budget and notification settings">⚙</span> <span class="val yellow" data-stat="cost">$0</span>
      <div class="window-toggle">
        <button class="win-btn" data-window="today">TODAY</button>
        <button class="win-btn" data-window="week">WEEK</button>
        <button class="win-btn" data-window="month">MONTH</button>
        <button class="win-btn" data-window="all">ALL</button>
      </div>
    </div>
    <div class="topbar-stat" title="Weighted cache read percentage across all sessions"><span>CACHE HIT</span> <span class="val" data-stat="cache" style="color:var(--purple)">—</span></div>
    <div class="topbar-stat rate-stat" title="Aggregate cost velocity across all active sessions"><span>$/MIN</span> <span class="val yellow" data-stat="rate">—</span></div>
    <div class="search-box">
      <input type="text" placeholder="Search all sessions..." data-search aria-label="Search sessions" />
    </div>
    <nav class="view-toggle" aria-label="View selection">
      <button class="view-btn active" data-view="list" aria-pressed="true">LIST</button>
      <button class="view-btn" data-view="graph" aria-pressed="false">GRAPH</button>
      <button class="view-btn" data-view="history" aria-pressed="false">HISTORY</button>
      <button class="view-btn" data-view="analytics" aria-pressed="false">ANALYTICS</button>
    </nav>
  `;
  // Add hamburger button (outside collapsible)
  const hamburger = document.createElement('button');
  hamburger.className = 'topbar-hamburger';
  hamburger.innerHTML = '☰';
  hamburger.setAttribute('aria-label', 'Toggle menu');
  hamburger.setAttribute('aria-expanded', 'false');
  el.appendChild(hamburger);

  // Wrap stats, search, and view toggle in collapsible div
  const collapsible = document.createElement('div');
  collapsible.className = 'topbar-collapsible';

  const stats = el.querySelectorAll('.topbar-stat');
  const searchBox = el.querySelector('.search-box');
  const viewToggle = el.querySelector('.view-toggle');

  stats.forEach((stat) => collapsible.appendChild(stat));
  if (searchBox) collapsible.appendChild(searchBox);
  if (viewToggle) collapsible.appendChild(viewToggle);

  el.appendChild(collapsible);

  // Hamburger click handler
  hamburger.addEventListener('click', () => {
    const isOpen = collapsible.classList.toggle('open');
    hamburger.setAttribute('aria-expanded', String(isOpen));
    // Toggle mobile sessions panel
    const sessionsPanel = document.querySelector('.sessions-panel');
    if (sessionsPanel) {
      sessionsPanel.classList.toggle('open', isOpen);
    }
  });

  // Escape key closes collapsible
  document.addEventListener(
    'keydown',
    (e) => {
      if (e.key === 'Escape' && collapsible.classList.contains('open')) {
        collapsible.classList.remove('open');
        hamburger.setAttribute('aria-expanded', 'false');
        const sessionsPanel = document.querySelector('.sessions-panel');
        if (sessionsPanel) sessionsPanel.classList.remove('open');
      }
    },
    { signal },
  );

  container.appendChild(el);

  statActive = el.querySelector('[data-stat="active"]');
  statCost = el.querySelector('[data-stat="cost"]');
  statCache = el.querySelector('[data-stat="cache"]');
  statRate = el.querySelector('[data-stat="rate"]');
  searchInput = el.querySelector('[data-search]');

  el.querySelectorAll<HTMLButtonElement>('.view-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      const view = btn.dataset.view as AppState['view'];
      update({ view });
    });
  });

  el.querySelectorAll<HTMLButtonElement>('.win-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      const win = btn.dataset.window as StatsWindow;
      localStorage.setItem('claude-monitor-stats-window', win);
      update({ statsWindow: win });
      refreshStats();
    });
  });
  updateWindowButtons();

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

  document.addEventListener(
    'keydown',
    (e) => {
      if (e.key === '/' && document.activeElement !== searchInput) {
        e.preventDefault();
        searchInput!.focus();
      }
    },
    { signal },
  );

  subscribe(onStateChange);

  // Cost breakdown popover on clicking the cost value
  const costVal = el.querySelector('[data-stat="cost"]');
  if (costVal) {
    const costEl = costVal as HTMLElement;
    costEl.style.cursor = 'pointer';
    costEl.setAttribute('role', 'button');
    costEl.setAttribute('tabindex', '0');
    costEl.setAttribute('aria-label', 'Total spend — click to view cost breakdown');
    costEl.addEventListener('click', (e) => {
      e.stopPropagation();
      toggleCostBreakdown(costEl);
    });
    costEl.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        e.stopPropagation();
        toggleCostBreakdown(costEl);
      }
    });
  }

  refreshStats();
  if (statsTimer) clearInterval(statsTimer);
  statsTimer = setInterval(refreshStats, 5000);
}

function refreshStats(): void {
  fetchStats(state.statsWindow)
    .then((stats) => {
      update({ stats });
    })
    .catch(() => {});
}

function updateWindowButtons(): void {
  el?.querySelectorAll<HTMLButtonElement>('.win-btn').forEach((btn) => {
    btn.classList.toggle('active', btn.dataset.window === state.statsWindow);
  });
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('stats') || changed.has('budgetThreshold') || changed.has('statsWindow')) {
    updateStats();
  }
  if (changed.has('statsWindow')) {
    updateWindowButtons();
  }
  if (changed.has('view')) {
    el?.querySelectorAll<HTMLButtonElement>('.view-btn').forEach((btn) => {
      const isActive = btn.dataset.view === state.view;
      btn.classList.toggle('active', isActive);
      btn.setAttribute('aria-pressed', String(isActive));
    });
  }
  if (changed.has('sessions') || changed.has('view')) {
    const graphBtn = el?.querySelector('[data-view="graph"]');
    if (graphBtn) {
      const attnCount = getAttentionCount();
      graphBtn.textContent = attnCount > 0 ? `GRAPH (${attnCount})` : 'GRAPH';
    }
  }
}

function setVal(el: HTMLElement | null, text: string): void {
  if (!el || el.textContent === text) return;
  el.textContent = text;
  // Flash animation on change
  el.classList.remove('flash');
  void (el as HTMLElement).offsetWidth; // force reflow
  el.classList.add('flash');
}

const WINDOW_LABELS: Record<string, string> = {
  today: 'today',
  week: 'this week',
  month: 'this month',
  all: 'all time',
};

function updateStats(): void {
  const stats = state.stats;
  if (!stats) return;
  // Count only top-level active sessions (no subagents) to match sidebar
  const activeTopLevel = Array.from(state.sessions.values()).filter(
    (s) => s.isActive && !s.parentId,
  ).length;
  setVal(statActive, String(activeTopLevel));

  // Show window label in muted text when a budget is set
  if (statCost) {
    const costStr = `$${stats.totalCost.toFixed(0)}`;
    if (state.budgetThreshold) {
      const label = WINDOW_LABELS[state.statsWindow] ?? state.statsWindow;
      if (statCost.childNodes.length === 1 && statCost.firstChild?.nodeType === Node.TEXT_NODE) {
        // First time adding the label span — rebuild
        statCost.textContent = '';
      }
      // Find or create cost text node and label span
      let textNode = statCost.querySelector('.cost-amount');
      let labelSpan = statCost.querySelector('.cost-window-label');
      if (!textNode) {
        textNode = document.createElement('span');
        textNode.className = 'cost-amount';
        statCost.textContent = '';
        statCost.appendChild(textNode);
      }
      if (!labelSpan) {
        labelSpan = document.createElement('span');
        labelSpan.className = 'cost-window-label';
        statCost.appendChild(labelSpan);
      }
      if (textNode.textContent !== costStr) {
        textNode.textContent = costStr;
        // Flash animation on change
        statCost.classList.remove('flash');
        void statCost.offsetWidth;
        statCost.classList.add('flash');
      }
      if (labelSpan.textContent !== ` ${label}`) {
        labelSpan.textContent = ` ${label}`;
      }
    } else {
      // No budget — plain text, remove label span if present
      const labelSpan = statCost.querySelector('.cost-window-label');
      if (labelSpan) labelSpan.remove();
      const textNode = statCost.querySelector('.cost-amount');
      if (textNode) {
        if (textNode.textContent !== costStr) {
          statCost.textContent = '';
          statCost.textContent = costStr;
          statCost.classList.remove('flash');
          void statCost.offsetWidth;
          statCost.classList.add('flash');
        }
      } else {
        setVal(statCost, costStr);
      }
    }
  }

  setVal(statCache, stats.cacheHitPct > 0 ? `${stats.cacheHitPct.toFixed(0)}%` : '—');
  setVal(statRate, stats.costRate > 0 ? `$${stats.costRate.toFixed(3)}/min` : '—');
}

export function focusSearch(): void {
  searchInput?.focus();
}
