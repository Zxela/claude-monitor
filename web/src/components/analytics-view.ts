import type { AppState } from '../state';
import type { TrendResult, RepoEntry, ToolUsage } from '../types';
import type { TrendWindow } from '../api';
import { state, subscribe } from '../state';
import { fetchTrends, fetchRepos, fetchToolUsage } from '../api';
import { formatTokens } from '../utils';
import { renderCards, renderToolUsageCard, destroyCards } from './analytics-cards';
import '../styles/analytics.css';

let container: HTMLElement | null = null;
let root: HTMLElement | null = null;
// Validate the persisted token: an unrecognized value would 400 on every
// fetch and leave the analytics page permanently blank.
const TREND_WINDOWS: TrendWindow[] = ['today', 'week', 'month', '24h', '7d', '30d'];
const storedWindow = localStorage.getItem('claude-monitor-analytics-window') as TrendWindow;
let currentWindow: TrendWindow = TREND_WINDOWS.includes(storedWindow) ? storedWindow : '7d';
let currentRepo: string | undefined;
let repos: RepoEntry[] = [];
let trendData: TrendResult | null = null;
let toolUsage: ToolUsage | null = null;
let loaded = false;
let loading = false;

function getCardState(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem('claude-monitor-analytics-cards') || '{}');
  } catch {
    return {};
  }
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
  if (root) {
    root.remove();
    root = null;
  }

  root = document.createElement('div');
  root.className = 'analytics-view';

  // Toolbar
  const toolbar = document.createElement('div');
  toolbar.className = 'analytics-toolbar';

  const windowToggle = document.createElement('div');
  windowToggle.className = 'analytics-window-toggle';
  // Calendar tokens share definitions with the topbar window toggle (local
  // midnight / ISO-week Monday / 1st of month), so TODAY here and TODAY in the
  // topbar always agree; the rolling tokens remain for trailing-window views.
  for (const w of TREND_WINDOWS) {
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
    // Hide phantom subagent-worktree repos (id 'agent-<hash>'); they are not
    // real projects and only pollute the filter. Fall back to id when the
    // name is empty so no option renders as a blank label.
    if (r.id.startsWith('agent-')) continue;
    const opt = document.createElement('option');
    opt.value = r.id;
    opt.textContent = r.name || r.id;
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
      <div class="analytics-stat-label">TOTAL SPEND (${currentWindow.toUpperCase()})</div>
    </div>
    <div class="analytics-stat">
      <div class="analytics-stat-val blue">${s ? formatTokens(s.effectiveTokens) : '—'}</div>
      <div class="analytics-stat-label">EFFECTIVE TOKENS</div>
    </div>
    <div class="analytics-stat">
      <div class="analytics-stat-val orange">${s ? s.cacheHitPct.toFixed(0) + '%' : '—'}</div>
      <div class="analytics-stat-label">CACHE HIT</div>
    </div>
    <div class="analytics-stat" title="Top-level sessions, plus workflow/subagent runs as a separate count">
      <div class="analytics-stat-val purple">${s ? String(s.sessionCount) : '—'}</div>
      <div class="analytics-stat-label">SESSIONS${s && s.agentCount > 0 ? ` (+${s.agentCount} AGENTS)` : ''}</div>
    </div>`;
  root.appendChild(summary);

  // Cards container
  const cardsContainer = document.createElement('div');
  cardsContainer.className = 'analytics-cards';
  root.appendChild(cardsContainer);

  if (trendData) {
    if (trendData.buckets.length === 0) {
      // Zero-data window/repo combo: rendering Chart.js over empty buckets
      // produces blank plots with a meaningless 0..1 axis and a stray legend.
      // Show an explicit empty state instead.
      destroyCards();
      cardsContainer.innerHTML = '<div class="analytics-empty">No activity in this period</div>';
    } else {
      renderCards(cardsContainer, trendData, getCardState(), (id, expanded) => {
        setCardState(id, expanded);
      });
      // Tool & Skill Usage is a non-chart card appended after the chart cards;
      // renderCards clears the container, so it must be (re)added here each render.
      const cs = getCardState();
      const tuExpanded = cs['tool-skill-usage'] !== undefined ? cs['tool-skill-usage'] : true;
      renderToolUsageCard(cardsContainer, toolUsage, tuExpanded, (expanded) => {
        setCardState('tool-skill-usage', expanded);
      });
    }
  }

  container.appendChild(root);

  if (!loaded) {
    loadData();
  }
}

function updateToolbar(): void {
  if (!root) return;
  root.querySelectorAll<HTMLButtonElement>('.analytics-win-btn').forEach((btn) => {
    btn.classList.toggle('active', btn.textContent === currentWindow.toUpperCase());
  });
}

async function loadData(): Promise<void> {
  loading = true;
  if (root) {
    root.querySelector('.analytics-summary')?.classList.add('loading');
  }

  try {
    const [trends, usage, repoList] = await Promise.all([
      fetchTrends(currentWindow, currentRepo),
      fetchToolUsage(currentWindow, currentRepo),
      loaded ? Promise.resolve(repos) : fetchRepos(),
    ]);
    trendData = trends;
    toolUsage = usage;
    repos = repoList;
    loaded = true;
  } catch (err) {
    console.error('Failed to load analytics:', err);
    loaded = true; // prevent retry loop on persistent errors
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
