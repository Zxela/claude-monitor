// web/src/components/session-card.ts
import type { Session } from '../types';
import { state, update } from '../state';

// Track which parent sessions have their children expanded
export const expandedParents = new Set<string>();

export function renderExpanded(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');
  if (session.isSubagent) el.classList.add('subagent');

  const dotClass = session.isActive
    ? (session.status === 'thinking' ? 'dot-thinking' : session.status === 'tool_use' ? 'dot-tool' : 'dot-active')
    : 'dot-idle';

  const displayName = session.sessionName || session.projectName || session.id;
  const statusClass = `status-${session.status}`;
  const childCount = session.children?.length ?? 0;
  const isExpanded = expandedParents.has(session.id);

  el.innerHTML = `
    <div class="session-card-content">
      <div class="session-row1">
        <span class="session-dot ${dotClass}"></span>
        <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
        ${childCount > 0 ? `<span class="subagent-chevron">${isExpanded ? '▾' : '▸'} ${childCount}</span>` : ''}
        <span class="session-status-badge ${statusClass}">${session.status === 'tool_use' ? 'TOOL' : session.status.toUpperCase()}</span>
      </div>
      <div class="session-task-desc" title="${escapeAttr(session.taskDescription)}">${escapeHtml(truncate(session.taskDescription || '', 80))}</div>
      <div class="session-stats">
        <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
        ${session.costRate > 0 ? `<span class="cost-rate">$${session.costRate.toFixed(3)}/min</span>` : ''}
        <span class="tok">${formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens)} tok</span>
        <span class="cache">${session.cacheHitPct.toFixed(0)}%</span>
        ${session.errorCount > 0 ? `<span class="session-error-count">${session.errorCount} err</span>` : ''}
      </div>
      <div class="session-meta">
        <span class="model">${(session.model || '').replace('claude-', '').replace('-4-6', '')}</span>
        <span>${timeAgo(session.lastActive)}</span>
        <span class="duration">${formatDuration(session.startedAt, session.lastActive)}</span>
      </div>
    </div>
  `;

  // Chevron click: toggle expand only (don't select)
  const chevron = el.querySelector('.subagent-chevron');
  if (chevron) {
    chevron.addEventListener('click', (e) => {
      e.stopPropagation();
      if (expandedParents.has(session.id)) {
        expandedParents.delete(session.id);
      } else {
        expandedParents.add(session.id);
      }
      // Trigger re-render via state change
      update({ selectedSessionId: state.selectedSessionId });
    });
  }

  // Card click: select session (and auto-expand if has children)
  el.addEventListener('click', () => {
    if (childCount > 0 && !expandedParents.has(session.id)) {
      expandedParents.add(session.id);
    }
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  });

  container.appendChild(el);

  // Render children if expanded
  if (childCount > 0 && isExpanded && session.children) {
    for (const childId of session.children) {
      const child = state.sessions.get(childId);
      if (child) {
        renderExpanded(child, container);
      }
    }
  }

  return el;
}

export function renderCompact(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card-compact';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');
  if (session.isSubagent) el.classList.add('subagent');

  const dotClass = session.isActive
    ? (session.status === 'thinking' ? 'dot-thinking' : session.status === 'tool_use' ? 'dot-tool' : 'dot-active')
    : 'dot-idle';

  const displayName = session.sessionName || session.projectName || session.id;
  const childCount = session.children?.length ?? 0;
  const isExpanded = expandedParents.has(session.id);

  el.innerHTML = `
    <span class="session-dot ${dotClass}"></span>
    <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
    ${childCount > 0 ? `<span class="subagent-chevron">${isExpanded ? '▾' : '▸'} ${childCount}</span>` : ''}
    <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
    <span class="duration">${timeAgo(session.lastActive)}</span>
    <span class="duration">${formatDuration(session.startedAt, session.lastActive)}</span>
    ${session.model ? `<span class="model">${session.model.replace('claude-', '').replace('-4-6', '')}</span>` : ''}
  `;

  // Chevron click
  const chevron = el.querySelector('.subagent-chevron');
  if (chevron) {
    chevron.addEventListener('click', (e) => {
      e.stopPropagation();
      if (expandedParents.has(session.id)) {
        expandedParents.delete(session.id);
      } else {
        expandedParents.add(session.id);
      }
      update({ selectedSessionId: state.selectedSessionId });
    });
  }

  el.addEventListener('click', () => {
    if (childCount > 0 && !expandedParents.has(session.id)) {
      expandedParents.add(session.id);
    }
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  });

  container.appendChild(el);

  // Render children if expanded
  if (childCount > 0 && isExpanded && session.children) {
    for (const childId of session.children) {
      const child = state.sessions.get(childId);
      if (child) {
        renderCompact(child, container);
      }
    }
  }

  return el;
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.substring(0, n) + '...' : s;
}

function timeAgo(ts: string): string {
  if (!ts) return '';
  const secs = Math.floor((Date.now() - new Date(ts).getTime()) / 1000);
  if (secs < 0) return 'now';
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
  return `${Math.floor(secs / 86400)}d ago`;
}

function formatDuration(startedAt: string, lastActive: string): string {
  if (!startedAt) return '';
  const start = new Date(startedAt).getTime();
  const end = lastActive ? new Date(lastActive).getTime() : Date.now();
  const secs = Math.floor((end - start) / 1000);
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  return `${h}h${m}m`;
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

function escapeHtml(s: string): string {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

function escapeAttr(s: string): string {
  return s.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
