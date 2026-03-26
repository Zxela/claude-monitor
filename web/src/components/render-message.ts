import type { ParsedMessage } from '../types';
import { escapeHtml } from '../utils';

export interface RenderOptions {
  showSessionId?: string;
}

type MessageType = 'user' | 'assistant' | 'tool' | 'result' | 'agent' | 'hook' | 'error' | 'system';

const TYPE_COLORS: Record<MessageType, string> = {
  user: '#5588ff',
  assistant: '#33dd99',
  tool: '#ddcc44',
  result: '#44cccc',
  agent: '#dd8844',
  hook: '#aa77dd',
  error: '#dd4455',
  system: '#666',
};

const TYPE_LABELS: Record<MessageType, string> = {
  user: 'USER',
  assistant: 'ASST',
  tool: 'TOOL',
  result: 'RESULT',
  agent: 'AGENT',
  hook: 'HOOK',
  error: 'ERROR',
  system: 'SYS',
};

export function detectType(msg: ParsedMessage): MessageType {
  if (msg.isError) return 'error';
  if (msg.hookEvent) return 'hook';
  if (msg.toolName && msg.role === 'assistant') return 'tool';
  if (msg.toolName && msg.role === 'tool') return 'result';
  if (msg.type === 'agent' || msg.type === 'agent-name') return 'agent';
  if (msg.role === 'user') return 'user';
  if (msg.role === 'assistant') return 'assistant';
  return 'system';
}

export function renderFeedEntry(msg: ParsedMessage, opts: RenderOptions = {}): HTMLElement {
  const type = detectType(msg);
  const el = document.createElement('div');
  el.className = `feed-entry type-${type}`;
  el.dataset.type = type;
  el.style.borderLeftColor = TYPE_COLORS[type];

  const time = formatTime(msg.timestamp);
  const label = TYPE_LABELS[type];
  const content = msg.contentText || msg.toolDetail || msg.toolName || '';
  const truncLen = type === 'result' ? 100 : 120;
  const truncated = content.length > truncLen ? content.substring(0, truncLen) + '...' : content;
  const hasMore = content.length > truncLen;

  let toolInfo = '';
  if (msg.toolName && type === 'tool') {
    toolInfo = `<span class="fe-tool">${escapeHtml(msg.toolName)}</span> `;
  }

  el.innerHTML = `
    <span class="fe-time">${time}</span>
    <span class="fe-type" style="color:${TYPE_COLORS[type]}">${label}</span>
    ${toolInfo}
    <span class="fe-content">${escapeHtml(truncated)}</span>
    ${hasMore ? '<span class="fe-expand">+</span>' : ''}
    ${opts.showSessionId ? `<span class="fe-sid">${opts.showSessionId}</span>` : ''}
  `;

  if (hasMore) {
    let expanded = false;
    const expandBtn = el.querySelector('.fe-expand')!;
    const contentEl = el.querySelector('.fe-content')!;
    expandBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      expanded = !expanded;
      contentEl.textContent = expanded ? content : truncated;
      expandBtn.textContent = expanded ? '−' : '+';
      el.classList.toggle('expanded', expanded);
    });
  }

  return el;
}

function formatTime(ts: string): string {
  if (!ts) return '—';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

