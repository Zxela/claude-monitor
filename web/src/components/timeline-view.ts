// web/src/components/timeline-view.ts
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { escapeHtml, formatDurationSecs } from '../utils';
import '../styles/views.css';

export interface TimelineEvent {
  index: number;
  timestamp: string;
  type: string;
  role: string;
  contentPreview: string;
  toolName?: string;
  costUSD: number;
}

// A drawn bar: a run of consecutive events in the same lane. Built once by
// buildSpans() and consumed by BOTH draw() and getEventAt() so the hover
// hit-test geometry exactly matches what is rendered.
export interface Span {
  start: number;
  end: number;
  lane: number;
  color: string;
  label: string;
  events: TimelineEvent[];
}

// The drawing/hit-test coordinate frame. draw() and getEventAt() MUST use the
// same instance so a cursor over a drawn bar resolves to that bar.
export interface TimelineGeometry {
  t0: number; // timestamp (ms) of the first event = x-origin
  pixelsPerMs: number; // zoom
  offsetX: number; // horizontal pan
  laneH: number; // per-lane height
  topY: number; // y of the first lane
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
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view')) {
    if (state.view === 'timeline') {
      const sid = state.selectedSessionId;
      if (sid) {
        open(sid);
      } else {
        // No session selected — go back to list
        update({ view: 'list' });
      }
    } else {
      close();
    }
  }
}

async function open(sid: string): Promise<void> {
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
  window.removeEventListener('resize', resizeCanvas);
  document.removeEventListener('keydown', onKeyDown);
  canvas = null;
  ctx = null;
  tooltip = null;
  events = [];
}

async function loadEvents(sid: string): Promise<void> {
  try {
    const res = await fetch(`/api/sessions/${sid}/replay`);
    const data = await res.json();
    // Drop events with zero/invalid timestamps. Synthetic meta-events
    // (permission-mode, file-history-snapshot, etc.) are emitted with the Go
    // zero-time "0001-01-01", which parses to ~-6.2e13ms and otherwise poisons
    // t0/span — making the waterfall an unreadable sliver for ~1/4 of sessions.
    const raw = (data.events ?? []) as TimelineEvent[];
    events = raw.filter((e) => {
      const t = new Date(e.timestamp).getTime();
      return Number.isFinite(t) && t > 0;
    });
  } catch (err) {
    console.error('Failed to load timeline events:', err);
  }
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'timeline-container';

  // Close / back button
  const closeBtn = document.createElement('button');
  closeBtn.className = 'timeline-close-btn';
  closeBtn.textContent = '\u2190 BACK';
  closeBtn.title = 'Close timeline (Escape)';
  closeBtn.addEventListener('click', () => {
    update({ view: 'list' });
  });
  wrapper.appendChild(closeBtn);

  canvas = document.createElement('canvas');
  canvas.setAttribute('role', 'img');
  canvas.setAttribute(
    'aria-label',
    'Session timeline — horizontal lanes show user, assistant, and tool activity over time',
  );
  canvas.textContent = 'Session timeline visualization';
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

  // Escape key to close
  document.removeEventListener('keydown', onKeyDown);
  document.addEventListener('keydown', onKeyDown);

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

function onKeyDown(e: KeyboardEvent): void {
  if (e.key === 'Escape' && state.view === 'timeline') {
    e.preventDefault();
    e.stopPropagation();
    update({ view: 'list' });
  }
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

// 2s gap in the same lane breaks a span. Exported so tests reference the same
// threshold the merge logic uses.
export const GAP_THRESHOLD = 2000;

// Build spans: group consecutive events in the same lane into bars.
// A span starts when an event enters a lane and ends when the next event
// is in a different lane, or there's a gap > GAP_THRESHOLD in the same lane.
// PURE over its `evts` argument — shared by draw() (rendering) and
// getEventAt() (hover hit-test) so the tooltip target always matches the drawn
// bar. Exported (taking events as an arg) so the merge/split geometry is
// testable without module state. Requires evts.length >= 2 for meaningful bars.
export function buildSpansFrom(evts: TimelineEvent[]): Span[] {
  const spans: Span[] = [];
  let cur: Span | null = null;

  for (let i = 0; i < evts.length; i++) {
    const evt = evts[i];
    const ts = new Date(evt.timestamp).getTime();
    const lane = getLane(evt);
    // Duration: time until the next event in a different lane starts
    // (meaning this lane was "active" until then), or until the next
    // event in the same lane if it's close (continuation).
    let end: number;
    if (i < evts.length - 1) {
      const nextTs = new Date(evts[i + 1].timestamp).getTime();
      // If the next global event is in a different lane, this lane was
      // active until that event started.
      if (getLane(evts[i + 1]) !== lane) {
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
      cur = {
        start: ts,
        end,
        lane,
        color: getColor(evt),
        label: evt.toolName || evt.role || '',
        events: [evt],
      };
    }
  }
  if (cur) spans.push(cur);
  return spans;
}

// Module wrapper: build spans from the current module-level events.
function buildSpans(): Span[] {
  return buildSpansFrom(events);
}

/**
 * PURE hit-test core: given a set of spans, the shared drawing geometry, and a
 * cursor at (mx, my), return the event under the cursor (the one whose own
 * timestamp maps nearest to mx within the hit span), or null if no bar is hit.
 * Uses byte-identical x/barW/y/barH math to draw() so hover matches the bar.
 * Exported so the geometry is testable without a canvas / module state.
 */
export function pickEventAt(
  spans: Span[],
  geom: TimelineGeometry,
  mx: number,
  my: number,
): TimelineEvent | null {
  const { t0, pixelsPerMs, offsetX, laneH, topY } = geom;
  for (const span of spans) {
    const x = (span.start - t0) * pixelsPerMs + offsetX;
    const barW = Math.max((span.end - span.start) * pixelsPerMs, 4); // min 4px
    const y = topY + span.lane * laneH + 4;
    const barH = laneH - 8;

    if (mx >= x && mx <= x + barW && my >= y && my <= y + barH) {
      // Return the event whose own timestamp is nearest the cursor for a
      // finer tooltip; falls back to the first event in the span.
      let best = span.events[0];
      let bestDist = Infinity;
      for (const evt of span.events) {
        const evtX = (new Date(evt.timestamp).getTime() - t0) * pixelsPerMs + offsetX;
        const dist = Math.abs(evtX - mx);
        if (dist < bestDist) {
          bestDist = dist;
          best = evt;
        }
      }
      return best;
    }
  }
  return null;
}

function draw(): void {
  if (!ctx || !canvas || !container || events.length === 0) return;
  const w = container.clientWidth,
    h = container.clientHeight;
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
  const step = Math.max(1000, Math.pow(10, Math.floor(Math.log10((1 / pixelsPerMs) * 100))));
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

  // Build the same spans used for hit-testing, then draw them.
  const spans = buildSpans();

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
  if (events.length < 2 || !container) return null;
  // Use logical (CSS) pixel dimensions to match mouse coordinates from
  // getBoundingClientRect(). canvas.height is DPR-scaled and must NOT be
  // used here — it would cause hit-testing to target the wrong lane on
  // HiDPI displays.
  const h = container.clientHeight;
  const geom: TimelineGeometry = {
    t0: new Date(events[0].timestamp).getTime(),
    pixelsPerMs,
    offsetX,
    laneH: (h - 30) / 3,
    topY: 30,
  };
  // Iterate the SAME spans draw() renders, using identical x/barW/y/barH
  // geometry (via pickEventAt), so the hover target lines up exactly with
  // the visible bar.
  return pickEventAt(buildSpans(), geom, mx, my);
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

function roundRect(
  c: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
): void {
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
