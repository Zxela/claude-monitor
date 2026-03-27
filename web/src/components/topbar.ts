import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import { isSessionActive } from '../utils';
import { toggle as toggleCostBreakdown } from './cost-breakdown';
import '../styles/topbar.css';

let el: HTMLElement | null = null;
let searchInput: HTMLInputElement | null = null;
let renderAbort: AbortController | null = null;

let statActive: HTMLElement | null = null;
let statCost: HTMLElement | null = null;
let statWorking: HTMLElement | null = null;
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
    <div class="topbar-stat" title="Lifetime cost across all discovered sessions"><span>TOTAL SPEND</span> <span class="budget-gear" role="button" tabindex="0" aria-label="Open budget and notification settings">⚙</span> <span class="val yellow" data-stat="cost">$0</span></div>
    <div class="topbar-stat"><span>WORKING</span> <span class="val cyan" data-stat="working">0</span></div>
    <div class="topbar-stat" title="Weighted cache read percentage across all sessions"><span>CACHE HIT</span> <span class="val" data-stat="cache" style="color:var(--purple)">—</span></div>
    <div class="topbar-stat" title="Aggregate cost velocity across all active sessions"><span>$/MIN</span> <span class="val yellow" data-stat="rate">—</span></div>
    <div class="search-box">
      <input type="text" placeholder="Search all sessions..." data-search aria-label="Search sessions" />
    </div>
    <nav class="view-toggle" aria-label="View selection">
      <button class="view-btn active" data-view="list" aria-pressed="true">LIST</button>
      <button class="view-btn" data-view="graph" aria-pressed="false">GRAPH</button>
      <button class="view-btn" data-view="history" aria-pressed="false">HISTORY</button>
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

  stats.forEach(stat => collapsible.appendChild(stat));
  if (searchBox) collapsible.appendChild(searchBox);
  if (viewToggle) collapsible.appendChild(viewToggle);

  el.appendChild(collapsible);

  // Hamburger click handler
  hamburger.addEventListener('click', () => {
    const isOpen = collapsible.classList.toggle('open');
    hamburger.setAttribute('aria-expanded', String(isOpen));
  });

  // Escape key closes collapsible
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && collapsible.classList.contains('open')) {
      collapsible.classList.remove('open');
      hamburger.setAttribute('aria-expanded', 'false');
    }
  }, { signal });

  container.appendChild(el);

  statActive = el.querySelector('[data-stat="active"]');
  statCost = el.querySelector('[data-stat="cost"]');
  statWorking = el.querySelector('[data-stat="working"]');
  statCache = el.querySelector('[data-stat="cache"]');
  statRate = el.querySelector('[data-stat="rate"]');
  searchInput = el.querySelector('[data-search]');

  el.querySelectorAll<HTMLButtonElement>('.view-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const view = btn.dataset.view as AppState['view'];
      update({ view });
    });
  });

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

  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && document.activeElement !== searchInput) {
      e.preventDefault();
      searchInput!.focus();
    }
  }, { signal });

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
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('sessions')) {
    updateStats();
  }
  if (changed.has('view')) {
    el?.querySelectorAll<HTMLButtonElement>('.view-btn').forEach(btn => {
      const isActive = btn.dataset.view === state.view;
      btn.classList.toggle('active', isActive);
      btn.setAttribute('aria-pressed', String(isActive));
    });
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

function updateStats(): void {
  const sessions = Array.from(state.sessions.values());
  const active = sessions.filter(s => isSessionActive(s.lastActive) && !s.isSubagent);
  const working = active.filter(s => s.status === 'thinking' || s.status === 'tool_use');
  const totalCost = sessions.reduce((sum, s) => sum + s.totalCostUSD, 0);
  const totalRate = active.reduce((sum, s) => sum + s.costRate, 0);

  const totalInput = sessions.reduce((sum, s) => sum + s.inputTokens + s.cacheReadTokens + s.cacheCreationTokens, 0);
  const totalCacheRead = sessions.reduce((sum, s) => sum + s.cacheReadTokens, 0);
  const cacheHit = totalInput > 0 ? (totalCacheRead / totalInput * 100) : 0;

  setVal(statActive, String(active.length));
  setVal(statCost, `$${totalCost.toFixed(0)}`);
  setVal(statWorking, String(working.length));
  setVal(statCache, cacheHit > 0 ? `${cacheHit.toFixed(0)}%` : '—');
  setVal(statRate, totalRate > 0 ? `$${totalRate.toFixed(3)}/m` : '—');
}

export function focusSearch(): void {
  searchInput?.focus();
}
