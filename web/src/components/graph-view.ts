// web/src/components/graph-view.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { escapeHtml } from '../utils';
import '../styles/views.css';

function sessionDisplayName(sess: Session): string {
  return sess.sessionName || sess.projectName || sess.id.slice(0, 8);
}

interface DagNode {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
  color: string;
  label: string;
  costLabel: string;
  session: Session;
  isActive: boolean;
}

interface DagEdge {
  source: string;
  target: string;
}

// Canvas state
let container: HTMLElement | null = null;
let canvas: HTMLCanvasElement | null = null;
let tooltip: HTMLElement | null = null;
let ctx: CanvasRenderingContext2D | null = null;
let nodes: DagNode[] = [];
let edges: DagEdge[] = [];
let nodeMap = new Map<string, DagNode>();
let prevNodeIds = '';
let graphMode: 'graph' | 'sequence' = 'graph';

// Viewport transform (pan & zoom)
let panX = 0;
let panY = 0;
let zoom = 1;
let isPanning = false;
let panStartX = 0;
let panStartY = 0;
let panStartPanX = 0;
let panStartPanY = 0;

// Hover state
let hoveredNode: DagNode | null = null;

// Animation
let animFrame: number | null = null;

// Layout constants
const NODE_HEIGHT = 32;
const MIN_NODE_WIDTH = 60;
const MAX_NODE_WIDTH = 200;
const ROW_GAP = 12;
const PIXELS_PER_SECOND = 3;
const LEFT_PADDING = 40;
const TOP_PADDING = 40;

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view')) {
    if (state.view === 'graph') {
      show();
    } else {
      hide();
    }
  }
  if (changed.has('sessions') && state.view === 'graph') {
    if (graphMode === 'graph') {
      rebuildDag();
      drawFrame();
    } else {
      show(); // re-render sequence list
    }
  }
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'graph-container';

  // Mode toggle
  const toggle = document.createElement('div');
  toggle.className = 'graph-mode-toggle';
  for (const mode of ['graph', 'sequence'] as const) {
    const btn = document.createElement('button');
    btn.className = `graph-mode-btn${graphMode === mode ? ' active' : ''}`;
    btn.textContent = mode === 'graph' ? 'Graph' : 'Sequence';
    btn.addEventListener('click', () => {
      graphMode = mode;
      show();
    });
    toggle.appendChild(btn);
  }
  wrapper.appendChild(toggle);

  if (graphMode === 'graph') {
    canvas = document.createElement('canvas');
    const visibleCount = Array.from(state.sessions.values())
      .filter(s => s.isActive || (Date.now() - new Date(s.lastActive).getTime()) < 120_000).length;
    canvas.setAttribute('role', 'img');
    canvas.setAttribute('aria-label', `Session graph: ${visibleCount} sessions displayed`);
    wrapper.appendChild(canvas);

    tooltip = document.createElement('div');
    tooltip.className = 'graph-tooltip';
    wrapper.appendChild(tooltip);

    container.appendChild(wrapper);

    resizeCanvas();
    window.addEventListener('resize', resizeCanvas);
    canvas.addEventListener('mousedown', onMouseDown);
    canvas.addEventListener('mousemove', onMouseMove);
    canvas.addEventListener('mouseup', onMouseUp);
    canvas.addEventListener('mouseleave', onMouseLeave);
    canvas.addEventListener('click', onClick);
    canvas.addEventListener('wheel', onWheel, { passive: false });

    // Reset viewport
    panX = 0;
    panY = 0;
    zoom = 1;
    prevNodeIds = '';

    rebuildDag();
    drawFrame();
  } else {
    container.appendChild(wrapper);
    renderSequence(wrapper);
  }
}

function renderSequence(wrapper: HTMLElement): void {
  const now = Date.now();
  const threshold = 120_000;

  const sessions = Array.from(state.sessions.values())
    .filter(s => s.isActive || (now - new Date(s.lastActive).getTime()) < threshold)
    .sort((a, b) => new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime());

  const list = document.createElement('div');
  list.className = 'sequence-list';

  if (sessions.length === 0) {
    const empty = document.createElement('div');
    empty.style.cssText = 'padding: 24px; text-align: center; color: var(--text-dim, #666); font-size: 12px;';
    empty.textContent = 'No active sessions';
    list.appendChild(empty);
  }

  for (const sess of sessions) {
    const depth = sess.parentId ? 1 : 0;
    const entry = document.createElement('div');
    entry.className = `sequence-entry${sess.isActive ? ' sequence-active' : ''}`;
    entry.style.paddingLeft = `${12 + depth * 24}px`;

    const time = sess.startedAt ? new Date(sess.startedAt).toLocaleTimeString() : '';
    const name = sessionDisplayName(sess);
    const cost = `$${sess.totalCostUSD.toFixed(2)}`;
    const statusClass = sess.isActive
      ? (sess.status === 'thinking' ? 'thinking' : sess.status === 'tool_use' ? 'tool-use' : 'active')
      : 'idle';
    const statusText = sess.isActive ? sess.status.replace('_', ' ') : 'done';

    entry.innerHTML = `
      <span class="sequence-time">${time}</span>
      <span class="sequence-dot ${statusClass}"></span>
      ${depth > 0 ? '<span class="sequence-connector">└─</span>' : ''}
      <span class="sequence-name">${escapeHtml(name)}</span>
      <span class="sequence-cost">${cost}</span>
      <span class="sequence-status">${statusText}</span>
    `;

    entry.style.cursor = 'pointer';
    entry.addEventListener('click', () => {
      update({ selectedSessionId: sess.id, view: 'list' });
    });
    list.appendChild(entry);
  }

  wrapper.appendChild(list);
}

function hide(): void {
  stopAnimation();
  window.removeEventListener('resize', resizeCanvas);
  canvas = null;
  ctx = null;
  tooltip = null;
}

function resizeCanvas(): void {
  if (!canvas || !container) return;
  canvas.width = container.clientWidth;
  canvas.height = container.clientHeight;
  ctx = canvas.getContext('2d');
  drawFrame();
}

function getVisibleSessions(): Session[] {
  const now = Date.now();
  const threshold = 120_000;

  const visible: Session[] = [];
  for (const sess of state.sessions.values()) {
    const lastActive = new Date(sess.lastActive).getTime();
    if (sess.isActive || (now - lastActive) < threshold) {
      visible.push(sess);
    }
  }

  // Include parents of visible nodes
  const visibleIds = new Set(visible.map(s => s.id));
  for (const sess of visible) {
    if (sess.parentId && !visibleIds.has(sess.parentId)) {
      const parent = state.sessions.get(sess.parentId);
      if (parent) {
        visible.push(parent);
        visibleIds.add(parent.id);
      }
    }
  }

  return visible;
}

function statusColor(sess: Session): string {
  if (!sess.isActive) return '#44445a';
  if (sess.status === 'thinking') return '#d29922';
  if (sess.status === 'tool_use') return '#58a6ff';
  return '#3fb950';
}

function rebuildDag(): void {
  const visibleSessions = getVisibleSessions();
  const visibleIds = new Set(visibleSessions.map(s => s.id));

  const nodeIds = [...visibleIds].sort().join(',');
  if (nodeIds === prevNodeIds) return;
  prevNodeIds = nodeIds;

  if (visibleSessions.length === 0) {
    nodes = [];
    edges = [];
    nodeMap = new Map();
    return;
  }

  // Build edges
  edges = [];
  for (const sess of visibleSessions) {
    if (sess.parentId && visibleIds.has(sess.parentId)) {
      edges.push({ source: sess.parentId, target: sess.id });
    }
  }

  // Find the global time range
  const timestamps = visibleSessions.map(s => new Date(s.startedAt).getTime());
  const minTime = Math.min(...timestamps);

  // Build children map
  const childrenMap = new Map<string, Session[]>();
  for (const sess of visibleSessions) {
    if (sess.parentId && visibleIds.has(sess.parentId)) {
      const children = childrenMap.get(sess.parentId) || [];
      children.push(sess);
      childrenMap.set(sess.parentId, children);
    }
  }

  // Find roots (no parentId or parent not in visible set)
  const roots = visibleSessions
    .filter(s => !s.parentId || !visibleIds.has(s.parentId))
    .sort((a, b) => new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime());

  // Layout: assign X from timestamp, Y from row assignment
  // Use a simple greedy row packing algorithm
  nodes = [];

  // Track which rows are occupied at each time range (for stacking siblings)
  const rowEndTimes: number[] = []; // rowEndTimes[row] = the rightmost X end for that row

  function assignRow(startX: number, width: number): number {
    // Find the first row where this node fits
    for (let r = 0; r < rowEndTimes.length; r++) {
      if (startX >= rowEndTimes[r] + ROW_GAP) {
        rowEndTimes[r] = startX + width;
        return r;
      }
    }
    // New row
    const r = rowEndTimes.length;
    rowEndTimes.push(startX + width);
    return r;
  }

  function layoutSession(sess: Session): void {
    const startMs = new Date(sess.startedAt).getTime();
    const endMs = new Date(sess.lastActive).getTime();
    const durationSec = Math.max(0, (endMs - startMs) / 1000);

    const x = LEFT_PADDING + ((startMs - minTime) / 1000) * PIXELS_PER_SECOND;
    const rawWidth = durationSec * PIXELS_PER_SECOND;
    const width = Math.max(MIN_NODE_WIDTH, Math.min(MAX_NODE_WIDTH, rawWidth));

    const row = assignRow(x, width);

    const y = TOP_PADDING + row * (NODE_HEIGHT + ROW_GAP);
    const name = sessionDisplayName(sess);
    const label = name.substring(0, Math.floor(width / 6)); // ~6px per char at 10px monospace
    const costLabel = `$${sess.totalCostUSD.toFixed(2)}`;

    const node: DagNode = {
      id: sess.id,
      x, y, width, height: NODE_HEIGHT,
      color: statusColor(sess),
      label,
      costLabel,
      session: sess,
      isActive: sess.isActive,
    };
    nodes.push(node);

    // Layout children
    const children = childrenMap.get(sess.id);
    if (children) {
      children
        .sort((a, b) => new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime())
        .forEach(child => layoutSession(child));
    }
  }

  for (const root of roots) {
    layoutSession(root);
  }

  // Rebuild cached nodeMap
  nodeMap = new Map(nodes.map(n => [n.id, n]));
}

function drawFrame(): void {
  if (!ctx || !canvas) return;
  ctx.clearRect(0, 0, canvas.width, canvas.height);

  ctx.save();
  ctx.translate(panX, panY);
  ctx.scale(zoom, zoom);

  drawEdges();
  drawNodes();

  ctx.restore();
}

function drawEdges(): void {
  if (!ctx) return;

  ctx.lineWidth = 1.5 / zoom;

  for (const edge of edges) {
    const src = nodeMap.get(edge.source);
    const tgt = nodeMap.get(edge.target);
    if (!src || !tgt) continue;

    const x1 = src.x + src.width;
    const y1 = src.y + src.height / 2;
    const x2 = tgt.x;
    const y2 = tgt.y + tgt.height / 2;

    // Bezier curve
    const cpOffset = Math.min(Math.abs(x2 - x1) * 0.4, 60);
    ctx.strokeStyle = 'rgba(100,100,140,0.4)';
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.bezierCurveTo(x1 + cpOffset, y1, x2 - cpOffset, y2, x2, y2);
    ctx.stroke();

    // Arrowhead: compute tangent angle at the target end of the bezier
    const arrowSize = 5 / zoom;
    const angle = Math.atan2(y2 - y1, cpOffset);

    ctx.fillStyle = 'rgba(100,100,140,0.4)';
    ctx.beginPath();
    ctx.moveTo(x2, y2);
    ctx.lineTo(x2 - arrowSize * Math.cos(angle - 0.4), y2 - arrowSize * Math.sin(angle - 0.4));
    ctx.lineTo(x2 - arrowSize * Math.cos(angle + 0.4), y2 - arrowSize * Math.sin(angle + 0.4));
    ctx.closePath();
    ctx.fill();
  }
}

function drawNodes(): void {
  if (!ctx) return;

  for (const n of nodes) {
    const isHovered = n === hoveredNode;

    // Glow for active nodes
    if (n.isActive) {
      ctx.shadowColor = n.color;
      ctx.shadowBlur = isHovered ? 12 : 6;
    }

    // Rounded rectangle
    const r = 6;
    ctx.fillStyle = n.color;
    ctx.globalAlpha = isHovered ? 1.0 : 0.8;
    ctx.beginPath();
    ctx.moveTo(n.x + r, n.y);
    ctx.lineTo(n.x + n.width - r, n.y);
    ctx.arcTo(n.x + n.width, n.y, n.x + n.width, n.y + r, r);
    ctx.lineTo(n.x + n.width, n.y + n.height - r);
    ctx.arcTo(n.x + n.width, n.y + n.height, n.x + n.width - r, n.y + n.height, r);
    ctx.lineTo(n.x + r, n.y + n.height);
    ctx.arcTo(n.x, n.y + n.height, n.x, n.y + n.height - r, r);
    ctx.lineTo(n.x, n.y + r);
    ctx.arcTo(n.x, n.y, n.x + r, n.y, r);
    ctx.closePath();
    ctx.fill();

    // Reset shadow
    ctx.shadowColor = 'transparent';
    ctx.shadowBlur = 0;
    ctx.globalAlpha = 1.0;

    // Hover border
    if (isHovered) {
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 1.5 / zoom;
      ctx.stroke();
    }

    // Label text inside node
    ctx.fillStyle = '#fff';
    ctx.font = '10px monospace';
    ctx.textAlign = 'left';
    ctx.textBaseline = 'middle';

    const textX = n.x + 6;
    const textY = n.y + n.height / 2;
    const maxTextWidth = n.width - 12;

    // Combine label + cost if there's room
    const combined = n.label + ' ' + n.costLabel;
    if (ctx.measureText(combined).width <= maxTextWidth) {
      ctx.fillText(n.label, textX, textY);
      // Cost in dimmer color
      const labelWidth = ctx.measureText(n.label + ' ').width;
      ctx.fillStyle = 'rgba(255,255,255,0.6)';
      ctx.fillText(n.costLabel, textX + labelWidth, textY);
    } else {
      // Just the label, truncated
      ctx.fillText(n.label, textX, textY, maxTextWidth);
    }
  }
}

// --- Hit testing ---

function screenToWorld(sx: number, sy: number): { x: number; y: number } {
  return {
    x: (sx - panX) / zoom,
    y: (sy - panY) / zoom,
  };
}

function getNodeAt(sx: number, sy: number): DagNode | null {
  const { x, y } = screenToWorld(sx, sy);
  // Iterate in reverse so top-drawn nodes are picked first
  for (let i = nodes.length - 1; i >= 0; i--) {
    const n = nodes[i];
    if (x >= n.x && x <= n.x + n.width && y >= n.y && y <= n.y + n.height) {
      return n;
    }
  }
  return null;
}

// --- Event handlers ---

function onMouseDown(e: MouseEvent): void {
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left;
  const my = e.clientY - rect.top;

  const node = getNodeAt(mx, my);
  if (!node) {
    // Start panning
    isPanning = true;
    panStartX = e.clientX;
    panStartY = e.clientY;
    panStartPanX = panX;
    panStartPanY = panY;
    canvas.style.cursor = 'grabbing';
  }
}

function onMouseMove(e: MouseEvent): void {
  if (!canvas || !tooltip) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left;
  const my = e.clientY - rect.top;

  if (isPanning) {
    panX = panStartPanX + (e.clientX - panStartX);
    panY = panStartPanY + (e.clientY - panStartY);
    drawFrame();
    return;
  }

  const node = getNodeAt(mx, my);
  if (node !== hoveredNode) {
    hoveredNode = node;
    drawFrame();
  }

  canvas.style.cursor = node ? 'pointer' : 'grab';

  if (node) {
    const sess = node.session;
    const name = sessionDisplayName(sess);
    tooltip.innerHTML = `<div><b>${escapeHtml(name)}</b></div>
      <div>$${sess.totalCostUSD.toFixed(2)} · ${sess.messageCount} msgs</div>
      <div>${sess.status} · ${sess.model || '?'}</div>`;
    tooltip.style.left = `${mx + 15}px`;
    tooltip.style.top = `${my + 15}px`;
    tooltip.classList.add('visible');
  } else {
    tooltip.classList.remove('visible');
  }
}

function onMouseUp(): void {
  isPanning = false;
  if (canvas) canvas.style.cursor = 'grab';
}

function onMouseLeave(): void {
  isPanning = false;
  hoveredNode = null;
  if (tooltip) tooltip.classList.remove('visible');
  drawFrame();
}

function onClick(e: MouseEvent): void {
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const node = getNodeAt(e.clientX - rect.left, e.clientY - rect.top);
  if (node) {
    update({ selectedSessionId: node.id, view: 'list' });
  }
}

function onWheel(e: WheelEvent): void {
  e.preventDefault();
  if (!canvas) return;

  if (e.ctrlKey || e.metaKey) {
    // Zoom
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;

    const zoomFactor = e.deltaY > 0 ? 0.9 : 1.1;
    const newZoom = Math.max(0.2, Math.min(5, zoom * zoomFactor));

    // Zoom toward cursor
    panX = mx - (mx - panX) * (newZoom / zoom);
    panY = my - (my - panY) * (newZoom / zoom);
    zoom = newZoom;
  } else {
    // Horizontal scroll
    panX -= e.deltaX || e.deltaY;
  }

  drawFrame();
}

function stopAnimation(): void {
  if (animFrame !== null) {
    cancelAnimationFrame(animFrame);
    animFrame = null;
  }
}
