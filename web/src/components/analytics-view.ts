import type { AppState } from '../state';
import type { TrendResult, RepoEntry } from '../types';
import type { TrendWindow } from '../api';
import { state, subscribe } from '../state';
import { fetchTrends, fetchRepos } from '../api';
import { formatTokens } from '../utils';
import { renderCards, destroyCards } from './analytics-cards';
import '../styles/analytics.css';

let container: HTMLElement | null = null;
let root: HTMLElement | null = null;
let currentWindow: TrendWindow = (localStorage.getItem('claude-monitor-analytics-window') as TrendWindow) || '7d';
let currentRepo: string | undefined;
let repos: RepoEntry[] = [];
let trendData: TrendResult | null = null;
let loaded = false;
let loading = false;

function getCardState(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem('claude-monitor-analytics-cards') || '{}');
  } catch { return {}; }
}

function setCardState(id: string, expanded: boolean): void {
  const s = getCardState();
  s[id] = expanded;
  localStorage.setItem('claude-monitor-analytics-cards', JSON.stringify(s));
}

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view')) {
    if (state.view === 'analytics') {
      show();
    } else {
      hide();
    }
  }
}

function show(): void {
  if (!container) return;

  // Remove any existing root
  if (root) { root.remove(); root = null; }

  root = document.createElement('div');
  root.className = 'analytics-view';

  // Toolbar
  const toolbar = document.createElement('div');
  toolbar.className = 'analytics-toolbar';

  const windowToggle = document.createElement('div');
  windowToggle.className = 'analytics-window-toggle';
  for (const w of ['24h', '7d', '30d'] as TrendWindow[]) {
    const btn = document.createElement('button');
    btn.className = `analytics-win-btn${currentWindow === w ? ' active' : ''}`;
    btn.textContent = w.toUpperCase();
    btn.addEventListener('click', () => {
      currentWindow = w;
      localStorage.setItem('claude-monitor-analytics-window', w);
      loadData();
      updateToolbar();
    });
    windowToggle.appendChild(btn);
  }
  toolbar.appendChild(windowToggle);

  const repoSelect = document.createElement('select');
  repoSelect.className = 'analytics-repo-filter';
  repoSelect.innerHTML = '<option value="">All repos</option>';
  for (const r of repos) {
    const opt = document.createElement('option');
    opt.value = r.id;
    opt.textContent = r.name;
    if (currentRepo === r.id) opt.selected = true;
    repoSelect.appendChild(opt);
  }
  repoSelect.addEventListener('change', () => {
    currentRepo = repoSelect.value || undefined;
    loadData();
  });
  toolbar.appendChild(repoSelect);
  root.appendChild(toolbar);

  // Summary row
  const summary = document.createElement('div');
  summary.className = 'analytics-summary';
  if (loading) summary.classList.add('loading');
  const s = trendData?.summary;
  summary.innerHTML = `
    <div class="analytics-stat">
      <div class="analytics-stat-val green">${s ? '$' + s.totalCost.toFixed(2) : '—'}</div>
      <div class="analytics-stat-label">TOTAL SPEND</div>
    </div>
    <div class="analytics-stat">
      <div class="analytics-stat-val blue">${s ? formatTokens(s.effectiveTokens) : '—'}</div>
      <div class="analytics-stat-label">EFFECTIVE TOKENS</div>
    </div>
    <div class="analytics-stat">
      <div class="analytics-stat-val orange">${s ? s.cacheHitPct.toFixed(0) + '%' : '—'}</div>
      <div class="analytics-stat-label">CACHE HIT</div>
    </div>
    <div class="analytics-stat">
      <div class="analytics-stat-val purple">${s ? String(s.sessionCount) : '—'}</div>
      <div class="analytics-stat-label">SESSIONS</div>
    </div>
  `;
  root.appendChild(summary);

  // Cards container
  const cardsContainer = document.createElement('div');
  cardsContainer.className = 'analytics-cards';
  root.appendChild(cardsContainer);

  if (trendData) {
    renderCards(cardsContainer, trendData, getCardState(), (id, expanded) => {
      setCardState(id, expanded);
    });
  }

  container.appendChild(root);

  if (!loaded) {
    loadData();
  }
}

function updateToolbar(): void {
  if (!root) return;
  root.querySelectorAll<HTMLButtonElement>('.analytics-win-btn').forEach(btn => {
    btn.classList.toggle('active', btn.textContent === currentWindow.toUpperCase());
  });
}

async function loadData(): Promise<void> {
  loading = true;
  if (root) {
    root.querySelector('.analytics-summary')?.classList.add('loading');
  }

  try {
    const [trends, repoList] = await Promise.all([
      fetchTrends(currentWindow, currentRepo),
      loaded ? Promise.resolve(repos) : fetchRepos(),
    ]);
    trendData = trends;
    repos = repoList;
    loaded = true;
  } catch (err) {
    console.error('Failed to load analytics:', err);
  } finally {
    loading = false;
  }

  if (state.view === 'analytics') {
    show();
  }
}

function hide(): void {
  destroyCards();
  if (root) {
    root.remove();
    root = null;
  }
}
