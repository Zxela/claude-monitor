import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import '../styles/topbar.css';

let el: HTMLElement | null = null;
let searchInput: HTMLInputElement | null = null;

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
    <div class="topbar-stat">TOTAL SPEND <span class="budget-gear" style="cursor:pointer">⚙</span> <span class="val yellow" data-stat="cost">$0</span></div>
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
  });

  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
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

  const totalInput = sessions.reduce((sum, s) => sum + s.inputTokens + s.cacheReadTokens + s.cacheCreationTokens, 0);
  const totalCacheRead = sessions.reduce((sum, s) => sum + s.cacheReadTokens, 0);
  const cacheHit = totalInput > 0 ? (totalCacheRead / totalInput * 100) : 0;

  if (statActive) statActive.textContent = String(active.length);
  if (statCost) statCost.textContent = `$${totalCost.toFixed(0)}`;
  if (statWorking) statWorking.textContent = String(working.length);
  if (statCache) statCache.textContent = `${cacheHit.toFixed(0)}%`;
  if (statRate) statRate.textContent = `$${totalRate.toFixed(3)}/m`;
}

export function focusSearch(): void {
  searchInput?.focus();
}
