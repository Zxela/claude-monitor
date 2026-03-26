import type { ParsedMessage } from '../types';
import { escapeHtml } from '../utils';

export interface RenderOptions {
  showSessionId?: string;
}

type MessageType = 'user' | 'assistant' | 'tool_use' | 'tool_result' | 'agent' | 'hook' | 'error' | 'system';

export function detectType(msg: ParsedMessage): MessageType {
  if (msg.isError) return 'error';
  if (msg.hookEvent) return 'hook';
  if (msg.toolName && msg.role === 'assistant') return 'tool_use';
  if (msg.toolName && msg.role === 'tool') return 'tool_result';
  if (msg.type === 'agent' || msg.type === 'agent-name') return 'agent';
  if (msg.role === 'user') return 'user';
  if (msg.role === 'assistant') return 'assistant';
  return 'system';
}

export function renderFeedEntry(msg: ParsedMessage, opts: RenderOptions = {}): HTMLElement {
  const type = detectType(msg);
  const el = document.createElement('div');
  el.className = `feed-entry type-${type}${msg.isError ? ' is-error' : ''}`;
  el.dataset.type = type;

  const time = formatTime(msg.timestamp);
  const text = msg.contentText || '';
  const detail = msg.toolDetail || '';

  // Build content the same way as the old HTML:
  // Tools: "[ToolName] detail or text"
  // Hooks: raw text (hookEvent is shown in type label)
  // Agents: "[agent: detail]" or "[agent] text"
  // Everything else: just the text
  let content = '';
  let contentClass = '';

  if (type === 'hook') {
    content = text;
    contentClass = 'hook';
  } else if (type === 'agent') {
    content = detail ? `[agent: ${detail}]` : truncate(text, 80);
    contentClass = 'tool';
  } else if (type === 'tool_use') {
    const name = msg.toolName || '';
    if (detail) {
      content = `[${name}] ${truncate(detail, 80)}`;
    } else {
      content = `[${name}] ${truncate(text, 80)}`;
    }
    contentClass = 'tool';
  } else if (type === 'tool_result') {
    content = truncate(text || detail, 100);
    contentClass = 'result';
  } else if (type === 'error') {
    content = truncate(text, 120);
  } else {
    content = truncate(text, 120);
    if (type === 'system') contentClass = 'dim';
  }

  const hasMore = text.length > 120 || detail.length > 80;
  const fullContent = text || detail;

  el.innerHTML =
    `<span class="fe-time">${time}</span>` +
    `<span class="fe-type ${type}">[${type}]</span>` +
    `<span class="fe-content ${contentClass}">${escapeHtml(content)}${hasMore ? '<span class="fe-expand">+</span>' : ''}</span>` +
    (opts.showSessionId ? `<span class="fe-sid">${escapeHtml(opts.showSessionId.slice(0, 6))}</span>` : '');

  if (hasMore) {
    let expanded = false;
    const expandBtn = el.querySelector('.fe-expand') as HTMLElement;
    const contentEl = el.querySelector('.fe-content') as HTMLElement;
    expandBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      expanded = !expanded;
      if (expanded) {
        contentEl.textContent = fullContent;
        contentEl.style.whiteSpace = 'pre-wrap';
        contentEl.style.overflow = 'visible';
        expandBtn.textContent = '−';
        el.classList.add('expanded');
      } else {
        contentEl.textContent = content;
        contentEl.style.whiteSpace = '';
        contentEl.style.overflow = '';
        expandBtn.textContent = '+';
        el.classList.remove('expanded');
      }
    });
  }

  return el;
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.substring(0, n) + '…' : s;
}

function formatTime(ts: string): string {
  if (!ts) return '—';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}
