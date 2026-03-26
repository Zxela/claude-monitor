// web/src/components/session-card.ts
import type { Session } from '../types';
import { state, update } from '../state';
import { escapeHtml, escapeAttr, formatTokens, formatDurationSecs, timeAgo } from '../utils';
import { getLastTool } from '../tool-tracker';

// Track which parent sessions have their children expanded
export const expandedParents = new Set<string>();
// Track which parents show idle subagents (hidden by default)
const showIdleChildren = new Set<string>();

export function renderExpanded(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');
  if (session.isSubagent) el.classList.add('subagent');
  if (session.isActive) el.classList.add('active-card');

  const dotClass = session.isActive
    ? (session.status === 'thinking' ? 'dot-thinking' : session.status === 'tool_use' ? 'dot-tool' : 'dot-active')
    : 'dot-idle';

  const displayName = session.sessionName || session.projectName || session.id;
  const statusClass = `status-${session.status}`;
  const childCount = session.children?.length ?? 0;
  const isExpanded = expandedParents.has(session.id);

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
        ${childCount > 0 ? `<span class="subagent-chevron" role="button" tabindex="0" aria-label="${isExpanded ? 'Collapse subagents' : 'Expand subagents'}">${isExpanded ? '▾' : '▸'} ${childCount}</span>` : ''}
        ${session.status !== 'idle'
          ? `<span class="session-status-badge ${statusClass}">${session.status === 'tool_use' ? 'TOOL' : session.status.toUpperCase()}</span>`
          : session.isActive
            ? '<span class="session-status-badge status-live">LIVE</span>'
            : ''}
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
      ${session.status === 'tool_use' ? `<div class="session-current-tool">${escapeHtml(getLastTool(session.id) || '')}</div>` : ''}
    </div>
  `;

  // Chevron click: toggle expand only (don't select)
  const chevron = el.querySelector('.subagent-chevron');
  if (chevron) {
    const toggleExpand = (e: Event) => {
      e.stopPropagation();
      if (expandedParents.has(session.id)) {
        expandedParents.delete(session.id);
      } else {
        expandedParents.add(session.id);
      }
      // Trigger re-render via state change
      update({ renderVersion: state.renderVersion + 1 });
    };
    chevron.addEventListener('click', toggleExpand);
    chevron.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
        e.preventDefault();
        toggleExpand(e);
      }
    });
  }

  // Card click: select session (and auto-expand if has children)
  const selectSession = () => {
    if (childCount > 0 && !expandedParents.has(session.id)) {
      expandedParents.add(session.id);
    }
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
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

  // Render children if expanded
  if (childCount > 0 && isExpanded && session.children) {
    const showIdle = showIdleChildren.has(session.id);
    const children = session.children
      .map(id => state.sessions.get(id))
      .filter((c): c is Session => !!c);
    const activeChildren = children.filter(c => c.status !== 'idle');
    const idleChildren = children.filter(c => c.status === 'idle');

    for (const child of activeChildren) {
      renderExpanded(child, container);
    }

    if (showIdle) {
      for (const child of idleChildren) {
        renderExpanded(child, container);
      }
    }

    // Show toggle for idle subagents if there are any
    if (idleChildren.length > 0) {
      const toggle = document.createElement('div');
      toggle.className = 'idle-toggle';
      toggle.setAttribute('role', 'button');
      toggle.setAttribute('tabindex', '0');
      toggle.textContent = showIdle
        ? `Hide ${idleChildren.length} idle`
        : `Show ${idleChildren.length} idle`;
      const toggleIdle = (e: Event) => {
        e.stopPropagation();
        if (showIdleChildren.has(session.id)) {
          showIdleChildren.delete(session.id);
        } else {
          showIdleChildren.add(session.id);
        }
        update({ renderVersion: state.renderVersion + 1 });
      };
      toggle.addEventListener('click', toggleIdle);
      toggle.addEventListener('keydown', (e) => {
        if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
          e.preventDefault();
          toggleIdle(e);
        }
      });
      container.appendChild(toggle);
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

  const compactDotLabel = session.isActive
    ? (session.status === 'thinking' ? 'Status: thinking' : session.status === 'tool_use' ? 'Status: using tool' : 'Status: active')
    : 'Status: idle';

  el.setAttribute('role', 'button');
  el.setAttribute('tabindex', '0');
  el.setAttribute('aria-label', `Session: ${displayName}`);

  el.innerHTML = `
    <div class="compact-row">
      <span class="session-dot ${dotClass}" aria-label="${compactDotLabel}" role="img"></span>
      <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
      ${childCount > 0 ? `<span class="subagent-chevron" role="button" tabindex="0" aria-label="${isExpanded ? 'Collapse subagents' : 'Expand subagents'}">${isExpanded ? '▾' : '▸'} ${childCount}</span>` : ''}
      <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
      <span class="duration">${timeAgo(session.lastActive)}</span>
      <span class="duration">${formatDuration(session.startedAt, session.lastActive)}</span>
      ${session.model ? `<span class="model">${session.model.replace('claude-', '').replace('-4-6', '')}</span>` : ''}
    </div>
  `;

  // Chevron click
  const compactChevron = el.querySelector('.subagent-chevron');
  if (compactChevron) {
    const toggleCompactExpand = (e: Event) => {
      e.stopPropagation();
      if (expandedParents.has(session.id)) {
        expandedParents.delete(session.id);
      } else {
        expandedParents.add(session.id);
      }
      update({ renderVersion: state.renderVersion + 1 });
    };
    compactChevron.addEventListener('click', toggleCompactExpand);
    compactChevron.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
        e.preventDefault();
        toggleCompactExpand(e);
      }
    });
  }

  const selectCompactSession = () => {
    if (childCount > 0 && !expandedParents.has(session.id)) {
      expandedParents.add(session.id);
    }
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  };
  el.addEventListener('click', selectCompactSession);
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      selectCompactSession();
    }
  });

  // Stats line
  const totalTokens = session.inputTokens + session.outputTokens + session.cacheReadTokens;
  const statsDiv = document.createElement('div');
  statsDiv.className = 'compact-stats';
  const statParts: string[] = [];
  statParts.push(`<span>${formatTokens(totalTokens)} tok</span>`);
  if (session.cacheHitPct > 0) {
    statParts.push(`<span>${Math.round(session.cacheHitPct)}% cache</span>`);
  }
  if (session.errorCount > 0) {
    statParts.push(`<span class="compact-stat-err">${session.errorCount} err</span>`);
  }
  if (session.costRate > 0) {
    statParts.push(`<span>$${session.costRate.toFixed(2)}/min</span>`);
  }
  statsDiv.innerHTML = statParts.join('');
  el.appendChild(statsDiv);

  container.appendChild(el);

  // Render children if expanded
  if (childCount > 0 && isExpanded && session.children) {
    const showIdle = showIdleChildren.has(session.id);
    const children = session.children
      .map(id => state.sessions.get(id))
      .filter((c): c is Session => !!c);
    const activeChildren = children.filter(c => c.status !== 'idle');
    const idleChildren = children.filter(c => c.status === 'idle');

    for (const child of activeChildren) {
      renderCompact(child, container);
    }

    if (showIdle) {
      for (const child of idleChildren) {
        renderCompact(child, container);
      }
    }

    if (idleChildren.length > 0) {
      const toggle = document.createElement('div');
      toggle.className = 'idle-toggle';
      toggle.setAttribute('role', 'button');
      toggle.setAttribute('tabindex', '0');
      toggle.textContent = showIdle
        ? `Hide ${idleChildren.length} idle`
        : `Show ${idleChildren.length} idle`;
      const toggleIdle = (e: Event) => {
        e.stopPropagation();
        if (showIdleChildren.has(session.id)) {
          showIdleChildren.delete(session.id);
        } else {
          showIdleChildren.add(session.id);
        }
        update({ renderVersion: state.renderVersion + 1 });
      };
      toggle.addEventListener('click', toggleIdle);
      toggle.addEventListener('keydown', (e) => {
        if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
          e.preventDefault();
          toggleIdle(e);
        }
      });
      container.appendChild(toggle);
    }
  }

  return el;
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
