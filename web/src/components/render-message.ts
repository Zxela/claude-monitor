import type { ParsedMessage } from '../types';
import { state, update } from '../state';
import { escapeHtml } from '../utils';

export interface RenderOptions {
  showSessionId?: string;
}

type MessageType = 'user' | 'assistant' | 'tool_use' | 'tool_result' | 'agent' | 'hook' | 'error' | 'command' | 'system';

// Detect slash commands in user messages by looking for command XML tags.
const COMMAND_RE = /<command-name>\s*\/?([^<]+)<\/command-name>/;
const COMMAND_MSG_RE = /<command-message>([^<]*)<\/command-message>/;
const COMMAND_CAVEAT_RE = /<local-command-caveat>[\s\S]*?<\/local-command-caveat>\s*/;

export function detectType(msg: ParsedMessage): MessageType {
  if (msg.isError) return 'error';
  if (msg.hookEvent) return 'hook';
  if (msg.role === 'user' && msg.contentPreview && COMMAND_RE.test(msg.contentPreview)) return 'command';
  // Caveat-only messages (preceding actual command) are also commands
  if (msg.role === 'user' && msg.contentPreview && msg.contentPreview.includes('<local-command-caveat>') && !COMMAND_RE.test(msg.contentPreview)) return 'command';
  // Agent tool calls show as 'agent' type, not 'tool_use'
  if (msg.isAgent && msg.role === 'assistant') return 'agent';
  if (msg.toolName && msg.role === 'assistant') return 'tool_use';
  if (msg.forToolUseId) return 'tool_result';
  if (msg.type === 'agent' || msg.type === 'agent-name') return 'agent';
  if (msg.role === 'user') return 'user';
  if (msg.role === 'assistant') return 'assistant';
  return 'system';
}

/** Extract a clean display string from a command message's raw XML content. */
function formatCommand(raw: string): string {
  // Strip the caveat boilerplate
  let text = raw.replace(COMMAND_CAVEAT_RE, '').trim();
  const nameMatch = text.match(COMMAND_RE);
  const msgMatch = text.match(COMMAND_MSG_RE);
  const name = nameMatch ? nameMatch[1].trim() : '';
  const body = msgMatch ? msgMatch[1].trim() : '';
  if (name && body && body !== name && !name.endsWith(body)) {
    return `/${name} ${body}`;
  }
  return name ? `/${name}` : text;
}

export function renderFeedEntry(msg: ParsedMessage, opts: RenderOptions = {}): HTMLElement {
  const type = detectType(msg);
  const el = document.createElement('div');
  el.className = `feed-entry type-${type}${msg.isError ? ' is-error' : ''}`;
  el.dataset.type = type;

  const time = formatTime(msg.timestamp);
  let rawText = msg.contentPreview || '';
  const detail = msg.toolDetail || '';
  // Backend sends full untruncated text in fullContent (contentPreview is capped at 200 chars)
  const fullText = msg.fullContent || rawText;

  // Strip redundant prefixes baked in by the backend parser
  rawText = rawText.replace(/^\[hook:\w+\]\s*/, '');
  rawText = rawText.replace(/^\[tool:\s*\w+\]\s*/, '');
  // Strip internal XML tags (e.g. <local-command-caveat>)
  rawText = rawText.replace(/<[^>]+>/g, '').trim();

  // Build display content
  let content = '';
  let contentClass = '';
  let fullContent = fullText || detail;

  if (type === 'command') {
    content = formatCommand(fullText || rawText);
    contentClass = 'command';
    fullContent = ''; // no expand needed — command is fully shown
  } else if (type === 'hook') {
    content = rawText || msg.hookEvent || '';
    contentClass = 'hook';
  } else if (type === 'agent') {
    const body = detail || rawText;
    content = body ? `Agent: ${truncate(body, 80)}` : 'Agent';
    contentClass = 'tool';
    fullContent = rawText || detail;
  } else if (type === 'tool_use') {
    const name = msg.toolName || '';
    const body = detail || rawText;
    content = name ? `${name}: ${truncate(body, 80)}` : truncate(body, 80);
    contentClass = 'tool';
    fullContent = body;
  } else if (type === 'tool_result') {
    content = truncate(rawText || detail, 100);
    contentClass = 'result';
  } else if (type === 'error') {
    content = truncate(rawText || detail || 'Error', 120);
  } else {
    content = truncate(rawText, 120);
    if (!content && type === 'assistant') content = '[thinking...]';
    if (type === 'system') contentClass = 'dim';
  }

  const hasMore = fullContent.length > content.length;
  const isAgentEntry = type === 'agent' && msg.isAgent;

  el.innerHTML =
    `<span class="fe-time">${time}</span>` +
    `<span class="fe-type ${type}">[${type}]</span>` +
    `<span class="fe-content ${contentClass}">${escapeHtml(content)}${hasMore ? '<button class="fe-expand" aria-label="Expand content" type="button">+</button>' : ''}${isAgentEntry ? '<span class="fe-navigate" title="Go to subagent">→</span>' : ''}</span>` +
    (opts.showSessionId ? `<span class="fe-sid">${escapeHtml(opts.showSessionId.slice(0, 6))}</span>` : '');

  if (hasMore) {
    let expanded = false;
    const expandBtn = el.querySelector('.fe-expand') as HTMLElement;
    const contentEl = el.querySelector('.fe-content') as HTMLElement;
    expandBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      expanded = !expanded;
      const navBtn = contentEl.querySelector('.fe-navigate');
      if (expanded) {
        contentEl.textContent = fullContent;
        contentEl.appendChild(expandBtn);
        if (navBtn) contentEl.appendChild(navBtn);
        expandBtn.textContent = '−';
        el.classList.add('expanded');
      } else {
        contentEl.textContent = content;
        contentEl.appendChild(expandBtn);
        if (navBtn) contentEl.appendChild(navBtn);
        expandBtn.textContent = '+';
        el.classList.remove('expanded');
      }
    });
  }

  // Agent entries: click navigate arrow to go to the subagent session
  if (isAgentEntry) {
    const navBtn = el.querySelector('.fe-navigate');
    if (navBtn) {
      navBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        navigateToSubagent(msg);
      });
    }
  }

  return el;
}

/** Find the child session spawned by this agent call and navigate to it. */
function navigateToSubagent(msg: ParsedMessage): void {
  const parentId = state.selectedSessionId;
  if (!parentId) return;

  const parent = state.sessions.get(parentId);
  if (!parent?.children?.length) return;

  const msgTime = msg.timestamp ? new Date(msg.timestamp).getTime() : 0;

  // Find the child session closest in time to this agent call
  let bestChild: string | null = null;
  let bestDelta = Infinity;

  for (const childId of parent.children) {
    const child = state.sessions.get(childId);
    if (!child) continue;
    const childStart = new Date(child.startedAt).getTime();
    const delta = Math.abs(childStart - msgTime);
    if (delta < bestDelta) {
      bestDelta = delta;
      bestChild = childId;
    }
  }

  if (bestChild) {
    update({ selectedSessionId: bestChild });
  }
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
