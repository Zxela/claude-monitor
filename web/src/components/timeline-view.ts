// web/src/components/timeline-view.ts
import { escapeHtml, formatDurationSecs } from '../utils';
import '../styles/views.css';

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

const LANE_LABELS = ['User', 'Assistant', 'Tools'];

let container: HTMLElement | null = null;
let canvas: HTMLCanvasElement | null = null;
let ctx: CanvasRenderingContext2D | null = null;
let tooltip: HTMLElement | null = null;
let events: TimelineEvent[] = [];

// View state
let offsetX = 0;
let pixelsPerMs = 0.05; // zoom level
let isDragging = false;
let dragStartX = 0;
let dragStartOffset = 0;

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

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'timeline-container';

  canvas = document.createElement('canvas');
  wrapper.appendChild(canvas);

  tooltip = document.createElement('div');
  tooltip.className = 'timeline-tooltip';
  wrapper.appendChild(tooltip);

  container.appendChild(wrapper);

  resizeCanvas();
  window.addEventListener('resize', resizeCanvas);
  canvas.addEventListener('mousedown', onMouseDown);
  canvas.addEventListener('mousemove', onMouseMove);
  canvas.addEventListener('mouseup', onMouseUp);
  canvas.addEventListener('mouseleave', onMouseUp);
  canvas.addEventListener('wheel', onWheel, { passive: false });

  // Reset view
  offsetX = 0;
  if (events.length > 1) {
    const t0 = new Date(events[0].timestamp).getTime();
    const t1 = new Date(events[events.length - 1].timestamp).getTime();
    const span = t1 - t0;
    if (span > 0 && canvas) {
      pixelsPerMs = (canvas.width - 40) / span;
    }
  }

  draw();
}

function resizeCanvas(): void {
  if (!canvas || !container) return;
  canvas.width = container.clientWidth;
  canvas.height = container.clientHeight;
  ctx = canvas.getContext('2d');
  draw();
}

function getLane(evt: TimelineEvent): number {
  if (evt.role === 'user') return 0;
  if (evt.toolName || evt.type === 'tool_use' || evt.type === 'tool_result') return 2;
  return 1; // assistant, hook, system, etc.
}

function getColor(evt: TimelineEvent): string {
  if (evt.toolName && evt.role === 'assistant') return TYPE_COLORS.tool_use;
  if (evt.toolName && evt.role === 'tool') return TYPE_COLORS.tool_result;
  return TYPE_COLORS[evt.role] || TYPE_COLORS[evt.type] || TYPE_COLORS.system;
}

function draw(): void {
  if (!ctx || !canvas || events.length === 0) return;
  const w = canvas.width, h = canvas.height;
  ctx.clearRect(0, 0, w, h);

  const laneH = (h - 30) / 3; // 30px for top time labels
  const topY = 30;

  // Draw lane backgrounds
  ctx.fillStyle = 'rgba(255,255,255,0.02)';
  for (let i = 0; i < 3; i++) {
    if (i % 2 === 0) ctx.fillRect(0, topY + i * laneH, w, laneH);
  }

  // Draw lane labels
  ctx.fillStyle = '#44445a';
  ctx.font = '10px monospace';
  ctx.textAlign = 'left';
  for (let i = 0; i < LANE_LABELS.length; i++) {
    ctx.fillText(LANE_LABELS[i], 4, topY + i * laneH + 14);
  }

  if (events.length < 2) return;

  const t0 = new Date(events[0].timestamp).getTime();

  // Draw time axis labels
  ctx.fillStyle = '#44445a';
  ctx.font = '9px monospace';
  ctx.textAlign = 'center';
  const step = Math.max(1000, Math.pow(10, Math.floor(Math.log10(1 / pixelsPerMs * 100))));
  for (let t = 0; ; t += step) {
    const x = t * pixelsPerMs + offsetX;
    if (x > w) break;
    if (x < 0) continue;
    ctx.fillText(formatDurationSecs(t / 1000), x, 14);
    ctx.strokeStyle = 'rgba(255,255,255,0.05)';
    ctx.beginPath();
    ctx.moveTo(x, 20);
    ctx.lineTo(x, h);
    ctx.stroke();
  }

  // Draw events as bars
  for (let i = 0; i < events.length; i++) {
    const evt = events[i];
    const ts = new Date(evt.timestamp).getTime();
    const nextTs = i < events.length - 1 ? new Date(events[i + 1].timestamp).getTime() : ts + 1000;
    const duration = Math.max(nextTs - ts, 200); // min 200ms for visibility

    const x = (ts - t0) * pixelsPerMs + offsetX;
    const barW = Math.max(duration * pixelsPerMs, 4);
    const lane = getLane(evt);
    const y = topY + lane * laneH + 4;
    const barH = laneH - 8;

    if (x + barW < 0 || x > w) continue; // off screen

    ctx.fillStyle = getColor(evt);
    ctx.globalAlpha = 0.7;
    ctx.fillRect(x, y, barW, barH);
    ctx.globalAlpha = 1;

    // Label inside bar if wide enough
    if (barW > 40) {
      const label = evt.toolName || evt.role || '';
      ctx.fillStyle = '#000';
      ctx.font = '9px monospace';
      ctx.textAlign = 'left';
      ctx.fillText(label.slice(0, Math.floor(barW / 6)), x + 3, y + barH / 2 + 3);
    }
  }
}

function getEventAt(mx: number, my: number): TimelineEvent | null {
  if (events.length < 2 || !canvas) return null;
  const h = canvas.height;
  const laneH = (h - 30) / 3;
  const topY = 30;
  const t0 = new Date(events[0].timestamp).getTime();

  for (let i = 0; i < events.length; i++) {
    const evt = events[i];
    const ts = new Date(evt.timestamp).getTime();
    const nextTs = i < events.length - 1 ? new Date(events[i + 1].timestamp).getTime() : ts + 1000;
    const duration = Math.max(nextTs - ts, 200);
    const x = (ts - t0) * pixelsPerMs + offsetX;
    const barW = Math.max(duration * pixelsPerMs, 4);
    const lane = getLane(evt);
    const y = topY + lane * laneH + 4;
    const barH = laneH - 8;

    if (mx >= x && mx <= x + barW && my >= y && my <= y + barH) {
      return evt;
    }
  }
  return null;
}

function onMouseDown(e: MouseEvent): void {
  isDragging = true;
  dragStartX = e.clientX;
  dragStartOffset = offsetX;
}

function onMouseMove(e: MouseEvent): void {
  if (!canvas || !tooltip) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left;
  const my = e.clientY - rect.top;

  if (isDragging) {
    offsetX = dragStartOffset + (e.clientX - dragStartX);
    draw();
    return;
  }

  const evt = getEventAt(mx, my);
  if (evt) {
    canvas.style.cursor = 'pointer';
    const time = new Date(evt.timestamp).toLocaleTimeString();
    const content = (evt.contentText || '').slice(0, 80);
    tooltip.innerHTML = `<div><b>${time}</b> [${evt.type || evt.role}]</div>
      ${evt.toolName ? `<div>${escapeHtml(evt.toolName)}</div>` : ''}
      <div style="color:var(--text-dim)">${escapeHtml(content)}</div>
      ${evt.costUSD > 0 ? `<div style="color:var(--yellow)">$${evt.costUSD.toFixed(4)}</div>` : ''}`;
    tooltip.style.left = `${mx + 15}px`;
    tooltip.style.top = `${my + 15}px`;
    tooltip.classList.add('visible');
  } else {
    canvas.style.cursor = 'grab';
    tooltip.classList.remove('visible');
  }
}

function onMouseUp(): void {
  isDragging = false;
}

function onWheel(e: WheelEvent): void {
  e.preventDefault();
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left;

  if (e.ctrlKey || e.metaKey) {
    // Zoom
    const zoomFactor = e.deltaY > 0 ? 0.8 : 1.25;
    const oldPxPerMs = pixelsPerMs;
    pixelsPerMs *= zoomFactor;
    pixelsPerMs = Math.max(0.001, Math.min(1, pixelsPerMs));
    // Keep the point under cursor stable
    offsetX = mx - (mx - offsetX) * (pixelsPerMs / oldPxPerMs);
  } else {
    // Pan
    offsetX -= e.deltaY;
  }
  draw();
}
