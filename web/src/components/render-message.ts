import type { ParsedMessage } from '../types';
import { state, update } from '../state';
import { escapeHtml } from '../utils';

export interface RenderOptions {
  showSessionId?: string;
}

type MessageType = 'user' | 'assistant' | 'tool_use' | 'tool_result' | 'agent' | 'hook' | 'error' | 'command' | 'system';

/** Strip MCP prefixes from tool names to get a readable short name.
 *  e.g. "mcp__plugin_playwright_playwright__browser_click" → "browser_click"
 *       "mcp__slack__post_message" → "post_message" */
function shortToolName(name: string): string {
  if (!name) return name;
  // MCP tools: mcp__<server>__<action> or mcp__plugin_<name>_<name>__<action>
  const mcpMatch = name.match(/^mcp__(?:plugin_\w+_\w+|[\w]+)__(.+)$/);
  if (mcpMatch) return mcpMatch[1];
  return name;
}

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
  // Fallback: detect agent/tool_use from content preview when fields weren't persisted (streaming race)
  if (!msg.toolName && msg.role === 'assistant' && msg.contentPreview?.startsWith('[agent')) return 'agent';
  if (!msg.toolName && msg.role === 'assistant' && msg.contentPreview?.startsWith('[tool: ')) return 'tool_use';
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
  el.className = `feed-entry type-${type}${msg.isError ? ' is-error' : ''}${msg.isMeta ? ' is-meta' : ''}`;
  el.dataset.type = type;

  const time = formatTime(msg.timestamp);
  let rawText = msg.contentPreview || '';
  const detail = msg.toolDetail || '';
  // Backend sends full untruncated text in fullContent (contentPreview is capped at 200 chars)
  const fullText = msg.fullContent || rawText;

  // Strip redundant prefixes baked in by the backend parser
  rawText = rawText.replace(/^\[hook:\w+\]\s*/, '');
  // Only strip [tool: ...] prefix when we have toolName (avoids blanking tool_use entries that lack toolName in DB)
  if (msg.toolName) rawText = rawText.replace(/^\[tool:\s*\w+\]\s*/, '');
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
    // hookName has the full identifier like "PreToolUse:Bash" or "SessionStart:startup"
    // detail has the command if it's a shell hook (not "callback")
    const hookName = msg.hookName || '';
    const hookCmd = detail || '';
    if (hookCmd) {
      content = `${hookName} — ${truncate(hookCmd, 70)}`;
    } else {
      content = hookName || msg.hookEvent || rawText || 'hook';
    }
    contentClass = 'hook';
    fullContent = hookCmd || '';
  } else if (type === 'agent') {
    // Extract agent name/description from content preview if toolDetail is missing (streaming race)
    let agentLabel = detail;
    let agentBody = rawText;
    if (!agentLabel) {
      const m = rawText.match(/^\[agent(?::\s*([^\]]*))?\]\s*(.*)/);
      if (m) { agentLabel = m[1]?.trim() || ''; agentBody = m[2]?.trim() || ''; }
    }
    const display = agentLabel && agentBody ? `${agentLabel}: ${agentBody}` : agentLabel || agentBody;
    content = display ? `Agent: ${truncate(display, 80)}` : 'Agent';
    contentClass = 'tool';
    fullContent = rawText || detail;
  } else if (type === 'tool_use') {
    // Extract tool name from content preview if toolName field is missing (streaming race)
    let name = msg.toolName || '';
    if (!name) {
      const m = rawText.match(/^\[tool:\s*([^\]]+)\]/);
      if (m) { name = m[1].trim(); rawText = rawText.replace(m[0], '').trim(); }
    }
    const short = shortToolName(name);
    // Try to extract a readable summary from the JSON params
    const summary = extractToolSummary(name, fullText) || detail || rawText;
    content = short && summary ? `${short}: ${truncate(summary, 90)}` : short || truncate(summary, 90);
    contentClass = 'tool';
    // Prefer full JSON input (from fullContent) for expand, fall back to detail/rawText
    fullContent = formatToolExpand(name, fullText) || detail || rawText;
  } else if (type === 'tool_result') {
    content = truncate(rawText || detail, 60);
    contentClass = 'result';
    // Agent completion results carry agent-level stats
    if (msg.agentDurationMs != null || msg.agentTokens != null) {
      const parts: string[] = [];
      if (msg.agentDurationMs != null) parts.push(`${(msg.agentDurationMs / 1000).toFixed(1)}s`);
      if (msg.agentTokens != null) parts.push(`${msg.agentTokens} tok`);
      if (msg.agentToolUseCount != null) parts.push(`${msg.agentToolUseCount} calls`);
      if (parts.length) content += ` [${parts.join(' · ')}]`;
    }
  } else if (type === 'error') {
    content = truncate(rawText || detail || 'Error', 120);
  } else if (type === 'system' && msg.subtype === 'turn_duration') {
    const turnParts: string[] = [];
    if (msg.durationMs != null) {
      const secs = msg.durationMs / 1000;
      turnParts.push(secs >= 60 ? `${(secs / 60).toFixed(1)}min` : `${secs.toFixed(1)}s`);
    }
    if (msg.turnMessageCount != null) turnParts.push(`${msg.turnMessageCount} messages`);
    content = turnParts.length ? `Turn completed \u2014 ${turnParts.join(', ')}` : 'Turn completed';
    contentClass = 'turn-sep';
    fullContent = '';
  } else {
    content = truncate(rawText, 120);
    if (!content && type === 'assistant') content = '[thinking...]';
    if (type === 'system') contentClass = 'dim';
  }

  const hasMore = fullContent.length > content.length;
  const isAgentEntry = type === 'agent' && (msg.isAgent || msg.contentPreview?.startsWith('[agent'));

  // Build metadata badges (duration, interrupted, truncated)
  let badges = '';
  if (msg.durationMs != null && msg.durationMs > 0) {
    const dur = msg.durationMs >= 1000 ? `${(msg.durationMs / 1000).toFixed(1)}s` : `${msg.durationMs}ms`;
    badges += `<span class="fe-duration">${dur}</span>`;
  }
  if (msg.interrupted) badges += '<span class="fe-badge fe-interrupted">interrupted</span>';
  if (msg.truncated) badges += '<span class="fe-badge fe-truncated">truncated</span>';

  el.innerHTML =
    `<span class="fe-time">${time}</span>` +
    `<span class="fe-type ${type}">[${type}]</span>` +
    `<span class="fe-content ${contentClass}">${escapeHtml(content)}${hasMore ? '<button class="fe-expand" aria-label="Expand content" type="button">+</button>' : ''}${isAgentEntry ? '<span class="fe-navigate" title="Go to subagent">→</span>' : ''}</span>` +
    badges +
    (opts.showSessionId ? `<span class="fe-sid" title="${escapeHtml(opts.showSessionId)}">${escapeHtml(opts.showSessionId.slice(0, 12))}</span>` : '');

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

/** Extract a short, human-readable one-line summary from tool_use JSON params. */
function extractToolSummary(toolName: string, jsonStr: string): string {
  if (!jsonStr) return '';
  try {
    const obj = JSON.parse(jsonStr);
    // Standard Claude tools
    if (toolName === 'Bash' && obj.description) return obj.description;
    if (toolName === 'Bash' && obj.command) return `$ ${obj.command.slice(0, 100)}`;
    if ((toolName === 'Read' || toolName === 'Write') && obj.file_path) return obj.file_path;
    if (toolName === 'Edit' && obj.file_path) return obj.file_path;
    if (toolName === 'Grep' && obj.pattern) return `/${obj.pattern}/${obj.path ? ' in ' + obj.path : ''}`;
    if (toolName === 'Glob' && obj.pattern) return obj.pattern;
    if (toolName === 'Agent' && obj.description) return obj.description;
    if (toolName === 'Skill' && obj.skill) return obj.skill;
    // MCP / generic tools — try common param patterns
    if (obj.url) return obj.url;
    if (obj.query) return obj.query;
    if (obj.element && obj.ref) return `${obj.element} [${obj.ref}]`;
    if (obj.expression) return truncate(obj.expression, 80);
    if (obj.function) return truncate(obj.function, 80);
    if (obj.filename) return obj.filename;
    if (obj.file_path) return obj.file_path;
    if (obj.command) return truncate(obj.command, 80);
    if (obj.description) return obj.description;
    // If it's a simple object with 1-2 keys, show them compactly
    const keys = Object.keys(obj);
    if (keys.length <= 2) return keys.map(k => `${k}: ${String(obj[k]).slice(0, 40)}`).join(', ');
    return '';
  } catch {
    return '';
  }
}

/** Format the full JSON input for tool_use expansion.
 *  For Bash: shows description + actual command.
 *  For other tools: shows pretty-printed JSON fields. */
function formatToolExpand(toolName: string, jsonStr: string): string {
  if (!jsonStr) return '';
  try {
    const obj = JSON.parse(jsonStr);
    if (toolName === 'Bash' && obj.command) {
      const desc = obj.description ? `# ${obj.description}\n` : '';
      return `${desc}$ ${obj.command}`;
    }
    // For file tools, show path prominently
    if ((toolName === 'Read' || toolName === 'Write' || toolName === 'Edit') && obj.file_path) {
      const parts = [`file: ${obj.file_path}`];
      if (obj.pattern) parts.push(`pattern: ${obj.pattern}`);
      if (obj.old_string) parts.push(`old: ${obj.old_string.slice(0, 200)}`);
      if (obj.new_string) parts.push(`new: ${obj.new_string.slice(0, 200)}`);
      return parts.join('\n');
    }
    if ((toolName === 'Grep' || toolName === 'Glob') && obj.pattern) {
      const parts = [`pattern: ${obj.pattern}`];
      if (obj.path) parts.push(`path: ${obj.path}`);
      if (obj.glob) parts.push(`glob: ${obj.glob}`);
      return parts.join('\n');
    }
    // Default: compact JSON
    return jsonStr;
  } catch {
    return jsonStr;
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
