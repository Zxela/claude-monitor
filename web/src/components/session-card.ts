// web/src/components/session-card.ts
import type { Session } from '../types';
import { state, update } from '../state';
import { escapeHtml, escapeAttr, formatTokens, formatDurationSecs, timeAgo, sessionDisplayName, stripInternalTags } from '../utils';
import { getLastTool } from '../tool-tracker';

function getCostTier(cost: number): string {
  if (cost < 0.50) return 'cost-tier-low';
  if (cost < 2) return 'cost-tier-mid';
  if (cost < 5) return 'cost-tier-high';
  return 'cost-tier-extreme';
}

function getDotClass(session: Session): string {
  if (!session.isActive) return 'dot-idle';
  if (session.status === 'thinking') return 'dot-thinking';
  if (session.status === 'tool_use') return 'dot-tool';
  return 'dot-active';
}


export function renderExpanded(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');
  if (!!session.parentId) el.classList.add('subagent');
  if (session.isActive) el.classList.add('active-card');

  const dotClass = getDotClass(session);

  const displayName = sessionDisplayName(session);
  const statusClass = `status-${session.status}`;

  const dotLabel = session.isActive
    ? (session.status === 'thinking' ? 'Status: thinking' : session.status === 'tool_use' ? 'Status: using tool' : 'Status: active')
    : 'Status: idle';

  el.setAttribute('role', 'button');
  el.setAttribute('tabindex', '0');
  el.setAttribute('aria-label', `Session: ${displayName}`);

  el.innerHTML = `
    <div class="session-card-content">
      <div class="session-row1">
        <span class="session-dot ${dotClass}" aria-label="${dotLabel}" role="img"></span>
        <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
        ${session.model ? `<span class="model">${escapeHtml(session.model.replace('claude-', '').replace('-4-6', ''))}</span>` : ''}
        ${session.status !== 'idle'
          ? `<span class="session-status-badge ${statusClass}">${session.status === 'tool_use' ? 'TOOL' : escapeHtml(session.status.toUpperCase())}</span>`
          : session.isActive
            ? '<span class="session-status-badge status-live">LIVE</span>'
            : ''}
      </div>
      <div class="session-task-desc" title="${escapeAttr(stripInternalTags(session.taskDescription || ''))}">${escapeHtml(truncate(stripInternalTags(session.taskDescription || ''), 80))}</div>
      <div class="session-stats">
        <span class="cost ${getCostTier(session.totalCost)}">$${session.totalCost.toFixed(2)}</span>
        ${session.costRate > 0 ? `<span class="cost-rate">$${session.costRate.toFixed(3)}/min</span>` : ''}
      </div>
      <div class="session-card-details">
        <div class="session-stats">
          <span class="tok">${formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens + session.cacheCreationTokens)} tok</span>
          <span class="cache">${Math.round((session.cacheReadTokens / Math.max(1, session.inputTokens + session.cacheReadTokens + session.cacheCreationTokens)) * 100).toFixed(0)}%</span>
          ${session.errorCount > 0 ? `<span class="session-error-count">${session.errorCount} err</span>` : ''}
        </div>
        <div class="session-meta">
          <span class="model">${escapeHtml((session.model || '').replace('claude-', '').replace('-4-6', ''))}</span>
          <span>${timeAgo(session.lastActive)}</span>
          <span class="duration">${formatDuration(session.startedAt, session.lastActive)}</span>
        </div>
        ${session.status === 'tool_use' ? `<div class="session-current-tool">${escapeHtml(getLastTool(session.id) || '')}</div>` : ''}
      </div>
    </div>
  `;

  // Card click: select session
  const selectSession = () => {
    const updates: Record<string, unknown> = { selectedSessionId: session.id === state.selectedSessionId ? null : session.id };
    if (state.view !== 'list') updates.view = 'list';
    update(updates);
  };
  el.addEventListener('click', selectSession);
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      selectSession();
    }
  });

  container.appendChild(el);

  // Error count click — filter feed to errors only
  const errEl = el.querySelector('.session-error-count');
  if (errEl) {
    errEl.setAttribute('role', 'button');
    errEl.setAttribute('tabindex', '0');
    const filterErrors = (e: Event) => {
      e.stopPropagation();
      update({
        selectedSessionId: session.id,
        feedTypeFilters: { user: false, assistant: false, tool_use: false, tool_result: false, agent: false, hook: false, error: true },
      });
    };
    errEl.addEventListener('click', filterErrors);
    errEl.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
        e.preventDefault();
        filterErrors(e);
      }
    });
  }

  return el;
}

export function renderCompact(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card-compact';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');
  if (!!session.parentId) el.classList.add('subagent');
  if (session.isActive) el.classList.add('active-card');

  const dotClass = getDotClass(session);

  const displayName = sessionDisplayName(session);

  const compactDotLabel = session.isActive
    ? (session.status === 'thinking' ? 'Status: thinking' : session.status === 'tool_use' ? 'Status: using tool' : 'Status: active')
    : 'Status: idle';

  el.setAttribute('role', 'button');
  el.setAttribute('tabindex', '0');
  el.setAttribute('aria-label', `Session: ${displayName}`);

  const compactStatusClass = `status-${session.status}`;

  el.innerHTML = `
    <div class="compact-row">
      <span class="session-dot ${dotClass}" aria-label="${compactDotLabel}" role="img"></span>
      <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
      ${session.model ? `<span class="model">${escapeHtml(session.model.replace('claude-', '').replace('-4-6', ''))}</span>` : ''}
      ${session.status !== 'idle'
        ? `<span class="session-status-badge ${compactStatusClass}">${session.status === 'tool_use' ? 'TOOL' : escapeHtml(session.status.toUpperCase())}</span>`
        : ''}
    </div>
    ${session.taskDescription ? `<div class="compact-task-desc" title="${escapeAttr(stripInternalTags(session.taskDescription))}">${escapeHtml(truncate(stripInternalTags(session.taskDescription), 60))}</div>` : ''}
    <div class="compact-meta">
      <span class="cost ${getCostTier(session.totalCost)}">$${session.totalCost.toFixed(2)}</span>
      ${session.costRate > 0 ? `<span class="cost-rate">$${session.costRate.toFixed(3)}/min</span>` : ''}
      <span class="duration">${timeAgo(session.lastActive)}</span>
      <span class="duration">${formatDuration(session.startedAt, session.lastActive)}</span>
      ${session.errorCount > 0 ? `<span class="compact-stat-err">${session.errorCount} err</span>` : ''}
    </div>
  `;

  const selectCompactSession = () => {
    const updates: Record<string, unknown> = { selectedSessionId: session.id === state.selectedSessionId ? null : session.id };
    if (state.view !== 'list') updates.view = 'list';
    update(updates);
  };
  el.addEventListener('click', selectCompactSession);
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      selectCompactSession();
    }
  });

  // Error count click — filter feed to errors only
  const errEl = el.querySelector('.compact-stat-err');
  if (errEl) {
    errEl.setAttribute('role', 'button');
    errEl.setAttribute('tabindex', '0');
    (errEl as HTMLElement).style.cursor = 'pointer';
    const filterErrors = (e: Event) => {
      e.stopPropagation();
      update({
        selectedSessionId: session.id,
        feedTypeFilters: { user: false, assistant: false, tool_use: false, tool_result: false, agent: false, hook: false, error: true },
      });
    };
    errEl.addEventListener('click', filterErrors);
    errEl.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
        e.preventDefault();
        filterErrors(e);
      }
    });
  }

  container.appendChild(el);

  return el;
}

export function renderDot(session: Session): HTMLElement {
  const dot = document.createElement('button');
  dot.type = 'button';
  dot.className = 'sidebar-dot';
  dot.dataset.sessionId = session.id;
  dot.classList.add(getDotClass(session));

  dot.title = sessionDisplayName(session);

  dot.addEventListener('click', () => {
    const updates: Record<string, unknown> = { selectedSessionId: session.id };
    if (state.view !== 'list') updates.view = 'list';
    update(updates);
  });

  return dot;
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.substring(0, n) + '...' : s;
}

function formatDuration(startedAt: string, lastActive: string): string {
  if (!startedAt) return '';
  const start = new Date(startedAt).getTime();
  const end = lastActive ? new Date(lastActive).getTime() : Date.now();
  const secs = Math.floor((end - start) / 1000);
  return formatDurationSecs(secs);
}
