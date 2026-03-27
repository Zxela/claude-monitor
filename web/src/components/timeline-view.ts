// web/src/components/timeline-view.ts
import { formatDurationSecs } from '../utils';
import '../styles/views.css';
import '../styles/feed.css';

interface TimelineEvent {
  index: number;
  timestamp: string;
  type: string;
  role: string;
  contentText: string;
  toolName?: string;
  costUSD: number;
}

const TYPE_COLORS: Record<string, string> = {
  user: '#5588ff',
  assistant: '#33dd99',
  tool_use: '#ddcc44',
  tool_result: '#44cccc',
  hook: '#aa77dd',
  error: '#dd4455',
  system: '#444',
};

const PREVIEW_LEN = 120;

let container: HTMLElement | null = null;
let events: TimelineEvent[] = [];

export function render(mount: HTMLElement): void {
  container = mount;
}

export async function open(sid: string): Promise<void> {
  await loadEvents(sid);
  show();
}

export function close(): void {
  events = [];
}

async function loadEvents(sid: string): Promise<void> {
  try {
    const res = await fetch(`/api/sessions/${sid}/replay`);
    const data = await res.json();
    events = data.events ?? [];
  } catch (err) {
    console.error('Failed to load timeline events:', err);
  }
}

function resolveType(evt: TimelineEvent): string {
  if (evt.toolName && evt.role === 'assistant') return 'tool_use';
  if (evt.toolName && evt.role === 'tool') return 'tool_result';
  if (evt.type === 'error') return 'error';
  return evt.role || evt.type || 'system';
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  const h = String(d.getHours()).padStart(2, '0');
  const m = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${h}:${m}:${s}`;
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'vtimeline-container';

  if (events.length === 0) {
    wrapper.innerHTML = '<div class="vtimeline-empty">No timeline events</div>';
    container.appendChild(wrapper);
    return;
  }

  for (let i = 0; i < events.length; i++) {
    // Render gap between entries
    if (i > 0) {
      const prevTs = new Date(events[i - 1].timestamp).getTime();
      const curTs = new Date(events[i].timestamp).getTime();
      const gapMs = curTs - prevTs;
      if (gapMs > 0) {
        const gapH = Math.min(40, Math.max(2, Math.log(gapMs / 100) * 6));
        const gap = document.createElement('div');
        gap.className = 'vtimeline-gap';
        gap.style.height = `${gapH}px`;
        if (gapMs > 2000) {
          const label = document.createElement('span');
          label.className = 'vtimeline-gap-label';
          label.textContent = formatDurationSecs(gapMs / 1000);
          gap.appendChild(label);
        }
        wrapper.appendChild(gap);
      }
    }

    const evt = events[i];
    const evtType = resolveType(evt);
    const color = TYPE_COLORS[evtType] || TYPE_COLORS.system;
    const content = evt.contentText || '';
    const needsExpand = content.length > PREVIEW_LEN;

    const entry = document.createElement('div');
    entry.className = 'vtimeline-entry';
    entry.style.borderLeftColor = color;

    const timeEl = document.createElement('span');
    timeEl.className = 'vtimeline-time';
    timeEl.textContent = formatTime(evt.timestamp);

    const badge = document.createElement('span');
    badge.className = 'vtimeline-badge';
    badge.style.background = color;
    badge.textContent = evt.toolName || evtType;

    const contentEl = document.createElement('span');
    contentEl.className = 'vtimeline-content';
    contentEl.textContent = needsExpand ? content.slice(0, PREVIEW_LEN) + '...' : content;

    entry.appendChild(timeEl);
    entry.appendChild(badge);
    entry.appendChild(contentEl);

    if (needsExpand) {
      const btn = document.createElement('span');
      btn.className = 'fe-expand';
      btn.textContent = '+';
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const expanded = entry.classList.toggle('expanded');
        btn.textContent = expanded ? '-' : '+';
        contentEl.textContent = expanded
          ? content
          : content.slice(0, PREVIEW_LEN) + '...';
        contentEl.appendChild(btn);
      });
      contentEl.appendChild(btn);
    }

    if (evt.costUSD > 0) {
      const cost = document.createElement('span');
      cost.className = 'vtimeline-cost';
      cost.textContent = `$${evt.costUSD.toFixed(4)}`;
      entry.appendChild(cost);
    }

    wrapper.appendChild(entry);
  }

  container.appendChild(wrapper);
}
