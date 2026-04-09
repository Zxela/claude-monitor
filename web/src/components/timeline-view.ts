// web/src/components/timeline-view.ts
import { escapeHtml, formatDurationSecs } from '../utils';
import '../styles/views.css';

interface TimelineEvent {
  index: number;
  timestamp: string;
  type: string;
  role: string;
  contentPreview: string;
  toolName?: string;
  costUSD: number;
}

const TYPE_COLORS: Record<string, string> = {
  user: '#5588ff',
  assistant: '#33dd99',
  tool_use: '#ddcc44',
  tool_result: '#55dddd',
  hook: '#bb88ee',
  error: '#ee5566',
  system: '#667788',
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
  // Clean up canvas event listeners to prevent accumulation
  if (canvas) {
    canvas.removeEventListener('mousedown', onMouseDown);
    canvas.removeEventListener('mousemove', onMouseMove);
    canvas.removeEventListener('mouseup', onMouseUp);
    canvas.removeEventListener('mouseleave', onMouseUp);
    canvas.removeEventListener('wheel', onWheel);
  }
  canvas = null;
  ctx = null;
  tooltip = null;
  events = [];
  window.removeEventListener('resize', resizeCanvas);
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
  window.removeEventListener('resize', resizeCanvas);
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
    if (span > 0 && container) {
      pixelsPerMs = (container.clientWidth - 40) / span;
    }
  }

  draw();
}

function resizeCanvas(): void {
  if (!canvas || !container) return;
  const dpr = window.devicePixelRatio || 1;
  const w = container.clientWidth;
  const h = container.clientHeight;
  canvas.width = w * dpr;
  canvas.height = h * dpr;
  canvas.style.width = w + 'px';
  canvas.style.height = h + 'px';
  ctx = canvas.getContext('2d');
  if (ctx) ctx.scale(dpr, dpr);
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
  if (!ctx || !canvas || !container || events.length === 0) return;
  const w = container.clientWidth, h = container.clientHeight;
  ctx.clearRect(0, 0, canvas.width, canvas.height);

  const laneH = (h - 30) / 3; // 30px for top time labels
  const topY = 30;

  // Draw lane backgrounds
  ctx.fillStyle = 'rgba(255,255,255,0.05)';
  for (let i = 0; i < 3; i++) {
    if (i % 2 === 0) ctx.fillRect(0, topY + i * laneH, w, laneH);
  }

  // Draw lane labels
  ctx.fillStyle = '#8888aa';
  ctx.font = '10px monospace';
  ctx.textAlign = 'left';
  for (let i = 0; i < LANE_LABELS.length; i++) {
    ctx.fillText(LANE_LABELS[i], 4, topY + i * laneH + 14);
  }

  if (events.length < 2) return;

  const t0 = new Date(events[0].timestamp).getTime();

  // Draw time axis labels
  ctx.fillStyle = '#8888aa';
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

  // Build spans: group consecutive events in the same lane into bars.
  // A span starts when an event enters a lane and ends when the next event
  // is in a different lane, or there's a gap > 2s in the same lane.
  interface Span { start: number; end: number; lane: number; color: string; label: string; events: TimelineEvent[]; }
  const spans: Span[] = [];
  let cur: Span | null = null;
  const GAP_THRESHOLD = 2000; // 2s gap breaks a span

  for (let i = 0; i < events.length; i++) {
    const evt = events[i];
    const ts = new Date(evt.timestamp).getTime();
    const lane = getLane(evt);
    // Duration: time until the next event in a different lane starts
    // (meaning this lane was "active" until then), or until the next
    // event in the same lane if it's close (continuation).
    let end = ts;
    if (i < events.length - 1) {
      const nextTs = new Date(events[i + 1].timestamp).getTime();
      // If the next global event is in a different lane, this lane was
      // active until that event started.
      if (getLane(events[i + 1]) !== lane) {
        end = nextTs;
      } else {
        // Same lane next — bar ends at a small fixed duration (point event).
        end = ts + Math.min(nextTs - ts, 500);
      }
    } else {
      end = ts + 500; // last event
    }

    if (cur && cur.lane === lane && ts - cur.end < GAP_THRESHOLD) {
      // Extend current span
      cur.end = end;
      cur.events.push(evt);
    } else {
      // Start new span
      if (cur) spans.push(cur);
      cur = { start: ts, end, lane, color: getColor(evt), label: evt.toolName || evt.role || '', events: [evt] };
    }
  }
  if (cur) spans.push(cur);

  // Draw spans
  for (const span of spans) {
    const x = (span.start - t0) * pixelsPerMs + offsetX;
    const barW = Math.max((span.end - span.start) * pixelsPerMs, 4); // min 4px
    const y = topY + span.lane * laneH + 4;
    const barH = laneH - 8;

    if (x + barW < 0 || x > w) continue;

    ctx.fillStyle = span.color;
    ctx.globalAlpha = 0.85;

    // Round corners for wider bars
    if (barW > 6) {
      roundRect(ctx, x, y, barW, barH, 2);
      ctx.fill();
    } else {
      ctx.fillRect(x, y, barW, barH);
    }
    ctx.globalAlpha = 1;

    // Label inside bar if wide enough
    if (barW > 40) {
      const label = span.events.length > 1 ? `${span.label} (${span.events.length})` : span.label;
      ctx.fillStyle = 'rgba(0,0,0,0.7)';
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
    const content = (evt.contentPreview || '').slice(0, 80);
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

function roundRect(c: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number): void {
  c.beginPath();
  c.moveTo(x + r, y);
  c.lineTo(x + w - r, y);
  c.quadraticCurveTo(x + w, y, x + w, y + r);
  c.lineTo(x + w, y + h - r);
  c.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
  c.lineTo(x + r, y + h);
  c.quadraticCurveTo(x, y + h, x, y + h - r);
  c.lineTo(x, y + r);
  c.quadraticCurveTo(x, y, x + r, y);
  c.closePath();
}
