import type { Session } from '../types';
import { state, update } from '../state';

export function renderExpanded(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');
  if (session.status === 'thinking' || session.status === 'tool_use') el.classList.add('pulse');

  const displayName = session.sessionName || session.projectName || session.id;
  const statusClass = `status-${session.status}`;
  const duration = formatDuration(session.startedAt, session.lastActive);

  el.innerHTML = `
    <div>
      <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
      <span class="status-badge ${statusClass}">${session.status.toUpperCase()}</span>
    </div>
    <div class="session-task" title="${escapeAttr(session.taskDescription)}">${escapeHtml(session.taskDescription || '')}</div>
    <div class="session-stats">
      <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
      ${session.costRate > 0 ? `<span class="cost">$${session.costRate.toFixed(3)}/min</span>` : ''}
      <span>${formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens)} tok</span>
      <span class="cache">${session.cacheHitPct.toFixed(0)}%</span>
      ${session.errorCount > 0 ? `<span class="err">${session.errorCount} err</span>` : ''}
    </div>
    <div class="session-stats">
      <span>${session.model || ''}</span>
      <span>${duration}</span>
    </div>
  `;

  el.addEventListener('click', () => {
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  });

  container.appendChild(el);
  return el;
}

export function renderCompact(session: Session, container: HTMLElement): HTMLElement {
  const el = document.createElement('div');
  el.className = 'session-card-compact';
  el.dataset.sessionId = session.id;

  if (session.id === state.selectedSessionId) el.classList.add('selected');

  const displayName = session.sessionName || session.projectName || session.id;
  const duration = formatDuration(session.startedAt, session.lastActive);

  el.innerHTML = `
    <span class="session-name" title="${escapeAttr(displayName)}">${escapeHtml(displayName)}</span>
    <span class="cost">$${session.totalCostUSD.toFixed(2)}</span>
    <span class="duration">${duration}</span>
    ${session.model ? `<span class="model">${session.model.replace('claude-', '').replace('-4-6', '')}</span>` : ''}
  `;

  el.addEventListener('click', () => {
    update({ selectedSessionId: session.id === state.selectedSessionId ? null : session.id });
  });

  container.appendChild(el);
  return el;
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
