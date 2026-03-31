// web/src/components/graph-view.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { escapeHtml, sessionDisplayName } from '../utils';
import { getLastTool } from '../tool-tracker';
import '../styles/views.css';

interface GraphNode {
  id: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  radius: number;
  color: string;
  label: string;
  session: Session;
}

interface GraphEdge {
  source: string;
  target: string;
}

let container: HTMLElement | null = null;
let canvas: HTMLCanvasElement | null = null;
let tooltip: HTMLElement | null = null;
let ctx: CanvasRenderingContext2D | null = null;
let nodes: GraphNode[] = [];
let edges: GraphEdge[] = [];
let nodeMap = new Map<string, GraphNode>();
let animFrame: number | null = null;
let dragging: GraphNode | null = null;
let hovering: GraphNode | null = null;
let prevNodeIds = '';
let settledFrames = 0;
let graphMode: 'graph' | 'sequence' = 'graph';
const SETTLE_THRESHOLD = 0.1;
const SETTLE_FRAMES = 30;
const lastSeenErrors = new Map<string, number>();

function needsAttention(sess: Session): boolean {
  if (sess.status === 'waiting') return true;
  const lastSeen = lastSeenErrors.get(sess.id) ?? 0;
  return sess.errorCount > lastSeen;
}

function acknowledgeAttention(): void {
  for (const sess of state.sessions.values()) {
    lastSeenErrors.set(sess.id, sess.errorCount);
  }
}

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
      rebuildNodes();
      updateNodeColors();
    } else {
      show(); // re-render sequence list
    }
  }
}

function show(): void {
  if (!container) return;
  acknowledgeAttention();
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'graph-container';

  // Mode toggle
  const toggle = document.createElement('div');
  toggle.className = 'graph-mode-toggle';
  for (const mode of ['graph', 'sequence'] as const) {
    const btn = document.createElement('button');
    btn.className = `graph-mode-btn${graphMode === mode ? ' active' : ''}`;
    btn.textContent = mode === 'graph' ? 'GRAPH' : 'SEQUENCE';
    btn.addEventListener('click', () => {
      graphMode = mode;
      show();
    });
    toggle.appendChild(btn);
  }
  wrapper.appendChild(toggle);

  if (graphMode === 'graph') {
    canvas = document.createElement('canvas');
    wrapper.appendChild(canvas);

    tooltip = document.createElement('div');
    tooltip.className = 'graph-tooltip';
    wrapper.appendChild(tooltip);

    container.appendChild(wrapper);

    resizeCanvas();
    window.removeEventListener('resize', resizeCanvas);
    window.addEventListener('resize', resizeCanvas);
    canvas.addEventListener('mousedown', onMouseDown);
    canvas.addEventListener('mousemove', onMouseMove);
    canvas.addEventListener('mouseup', onMouseUp);
    canvas.addEventListener('click', onClick);

    rebuildNodes();
    startAnimation();
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
    .sort((a, b) => {
      const aAttn = needsAttention(a) ? 1 : 0;
      const bAttn = needsAttention(b) ? 1 : 0;
      if (aAttn !== bAttn) return bAttn - aAttn;
      return new Date(a.startedAt).getTime() - new Date(b.startedAt).getTime();
    });

  const list = document.createElement('div');
  list.className = 'sequence-list';

  if (sessions.length === 0) {
    const empty = document.createElement('div');
    empty.style.cssText = 'padding: 24px; text-align: center; color: var(--text-dim, #666); font-size: 10px; letter-spacing: 1px; opacity: 0.4;';
    empty.textContent = 'NO ACTIVE SESSIONS';
    list.appendChild(empty);
  }

  for (const sess of sessions) {
    const depth = sess.parentId ? 1 : 0;
    const entry = document.createElement('div');
    entry.className = `sequence-entry${sess.isActive ? ' sequence-active' : ''}`;
    entry.style.paddingLeft = `${12 + depth * 24}px`;

    const time = sess.startedAt ? new Date(sess.startedAt).toLocaleTimeString() : '';
    const name = sessionDisplayName(sess);
    const cost = `$${sess.totalCost.toFixed(2)}`;
    const statusClass = sess.isActive
      ? (sess.status === 'thinking' ? 'thinking' : sess.status === 'tool_use' ? 'tool-use' : 'active')
      : 'idle';
    const statusText = sess.isActive ? sess.status.replace('_', ' ') : 'done';
    const attn = needsAttention(sess);
    const attnBadge = attn
      ? (sess.status === 'waiting' ? '<span style="color:#ffa64a;font-size:9px;margin-left:6px">WAITING</span>'
        : '<span style="color:#ff6b6b;font-size:9px;margin-left:6px">ERROR</span>')
      : '';
    const toolInfo = sess.status === 'tool_use' ? (getLastTool(sess.id) || '') : '';

    entry.innerHTML = `
      <span class="sequence-time">${time}</span>
      <span class="sequence-dot ${statusClass}"></span>
      ${depth > 0 ? '<span class="sequence-connector">└─</span>' : ''}
      <span class="sequence-name">${escapeHtml(name)}${attnBadge}</span>
      ${toolInfo ? `<span style="color:#4488ff;font-size:10px">${escapeHtml(toolInfo)}</span>` : ''}
      <span class="sequence-cost">${cost}${sess.costRate > 0 ? ` <span style="color:#888;font-size:9px">$${sess.costRate.toFixed(3)}/m</span>` : ''}</span>
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
  const dpr = window.devicePixelRatio || 1;
  const w = container.clientWidth;
  const h = container.clientHeight;
  canvas.width = w * dpr;
  canvas.height = h * dpr;
  canvas.style.width = w + 'px';
  canvas.style.height = h + 'px';
  ctx = canvas.getContext('2d');
  if (ctx) ctx.scale(dpr, dpr);
}

function rebuildNodes(): void {
  if (!canvas) return;
  const now = Date.now();
  const threshold = 120_000; // 120 seconds

  const visibleSessions: Session[] = [];
  for (const sess of state.sessions.values()) {
    const lastActive = new Date(sess.lastActive).getTime();
    if (sess.isActive || (now - lastActive) < threshold) {
      visibleSessions.push(sess);
    }
  }

  // Include parents of visible nodes
  const visibleIds = new Set(visibleSessions.map(s => s.id));
  for (const sess of visibleSessions) {
    if (sess.parentId && !visibleIds.has(sess.parentId)) {
      const parent = state.sessions.get(sess.parentId);
      if (parent) {
        visibleSessions.push(parent);
        visibleIds.add(parent.id);
      }
    }
  }

  const nodeIds = [...visibleIds].sort().join(',');
  if (nodeIds === prevNodeIds) return;
  prevNodeIds = nodeIds;

  const oldNodes = new Map(nodes.map(n => [n.id, n]));
  const cx = (canvas?.width ?? 800) / 2;
  const cy = (canvas?.height ?? 600) / 2;

  nodes = visibleSessions.map(sess => {
    const old = oldNodes.get(sess.id);
    const radius = Math.min(30, Math.max(8, Math.log(sess.totalCost + 1) * 5 + 8));
    const color = !sess.isActive ? '#44445a'
      : sess.status === 'thinking' ? '#ffcc00'
      : sess.status === 'tool_use' ? '#4488ff'
      : sess.status === 'waiting' ? '#ffa64a'
      : '#00ff88';
    const label = (sessionDisplayName(sess)).substring(0, 16);

    return {
      id: sess.id,
      x: old?.x ?? cx + (Math.random() - 0.5) * 200,
      y: old?.y ?? cy + (Math.random() - 0.5) * 200,
      vx: old?.vx ?? 0,
      vy: old?.vy ?? 0,
      radius, color, label, session: sess,
    };
  });

  // Rebuild cached nodeMap
  nodeMap = new Map(nodes.map(n => [n.id, n]));

  edges = [];
  for (const sess of visibleSessions) {
    if (sess.parentId && visibleIds.has(sess.parentId)) {
      edges.push({ source: sess.parentId, target: sess.id });
    }
  }

  // Reset idle detection and restart animation
  settledFrames = 0;
  if (animFrame === null && state.view === 'graph') {
    startAnimation();
  }
}

function updateNodeColors(): void {
  let needsRedraw = false;
  for (const n of nodes) {
    const sess = state.sessions.get(n.id);
    if (!sess) continue;
    n.session = sess;
    const newColor = !sess.isActive ? '#44445a'
      : sess.status === 'thinking' ? '#ffcc00'
      : sess.status === 'tool_use' ? '#4488ff'
      : sess.status === 'waiting' ? '#ffa64a'
      : '#00ff88';
    if (n.color !== newColor) {
      n.color = newColor;
      needsRedraw = true;
    }
    n.radius = Math.min(30, Math.max(8, Math.log(sess.totalCost + 1) * 5 + 8));
  }
  // If any node needs attention, ensure animation is running
  const anyAttention = nodes.some(n => needsAttention(n.session));
  if (anyAttention && animFrame === null && state.view === 'graph') {
    settledFrames = 0;
    startAnimation();
  } else if (needsRedraw) {
    draw();
  }
}

function simulate(): void {
  if (!canvas) return;
  const w = canvas.width;
  const h = canvas.height;

  // Repulsion
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i], b = nodes[j];
      const dx = b.x - a.x, dy = b.y - a.y;
      const dist = Math.max(1, Math.sqrt(dx * dx + dy * dy));
      const force = 2000 / (dist * dist);
      const fx = (dx / dist) * force, fy = (dy / dist) * force;
      a.vx -= fx; a.vy -= fy;
      b.vx += fx; b.vy += fy;
    }
  }

  // Attraction along edges (use cached nodeMap)
  for (const edge of edges) {
    const a = nodeMap.get(edge.source), b = nodeMap.get(edge.target);
    if (!a || !b) continue;
    const dx = b.x - a.x, dy = b.y - a.y;
    const dist = Math.sqrt(dx * dx + dy * dy);
    const force = (dist - 100) * 0.01;
    const fx = (dx / dist) * force, fy = (dy / dist) * force;
    a.vx += fx; a.vy += fy;
    b.vx -= fx; b.vy -= fy;
  }

  // Center gravity + damping + bounds
  const cx = w / 2, cy = h / 2;
  for (const n of nodes) {
    n.vx += (cx - n.x) * 0.001;
    n.vy += (cy - n.y) * 0.001;
    n.vx *= 0.9;
    n.vy *= 0.9;
    if (n !== dragging) {
      n.x += n.vx;
      n.y += n.vy;
    }
    n.x = Math.max(n.radius, Math.min(w - n.radius, n.x));
    n.y = Math.max(n.radius, Math.min(h - n.radius, n.y));
  }

  // Idle detection: check if all velocities are below threshold
  const allSettled = nodes.every(n => Math.abs(n.vx) < SETTLE_THRESHOLD && Math.abs(n.vy) < SETTLE_THRESHOLD);
  const anyAttention = nodes.some(n => needsAttention(n.session));
  if (allSettled && !anyAttention) {
    settledFrames++;
  } else if (!allSettled) {
    settledFrames = 0;
  }
}

function draw(): void {
  if (!ctx || !canvas) return;
  ctx.clearRect(0, 0, canvas.width, canvas.height);

  // Edges (use cached nodeMap)
  ctx.strokeStyle = 'rgba(100,100,140,0.3)';
  ctx.lineWidth = 1;
  for (const edge of edges) {
    const a = nodeMap.get(edge.source), b = nodeMap.get(edge.target);
    if (!a || !b) continue;
    ctx.beginPath();
    ctx.moveTo(a.x, a.y);
    ctx.lineTo(b.x, b.y);
    ctx.stroke();
  }

  // Nodes
  for (const n of nodes) {
    ctx.globalAlpha = n === hovering ? 1.0 : 0.7;
    ctx.fillStyle = n.color;
    ctx.beginPath();
    ctx.arc(n.x, n.y, n.radius + (n === hovering ? 2 : 0), 0, Math.PI * 2);
    ctx.fill();
    if (n === hovering) {
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 2;
      ctx.stroke();
    }
    ctx.globalAlpha = 1.0;

    // Pulsing ring for nodes that need attention
    if (needsAttention(n.session)) {
      const pulse = (Math.sin(Date.now() / 500) + 1) / 2;
      ctx.globalAlpha = 0.3 + pulse * 0.7;
      const ringColor = n.session.errorCount > (lastSeenErrors.get(n.id) ?? 0) ? '#ff6b6b' : '#ffa64a';
      ctx.strokeStyle = ringColor;
      ctx.lineWidth = 3;
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.radius + 6 + pulse * 2, 0, Math.PI * 2);
      ctx.stroke();
      ctx.globalAlpha = 1.0;
    }

    // Label
    ctx.fillStyle = '#aaa';
    ctx.font = '10px monospace';
    ctx.textAlign = 'center';
    ctx.fillText(n.label, n.x, n.y + n.radius + 14);

    // Cost
    if (n.session.totalCost > 0.01) {
      ctx.fillStyle = '#888';
      ctx.font = '8px monospace';
      ctx.fillText(`$${n.session.totalCost.toFixed(2)}`, n.x, n.y + n.radius + 24);
    }
  }
}

function getNodeAt(x: number, y: number): GraphNode | null {
  for (const n of nodes) {
    const dx = x - n.x, dy = y - n.y;
    if (dx * dx + dy * dy < (n.radius + 4) * (n.radius + 4)) return n;
  }
  return null;
}

function onMouseDown(e: MouseEvent): void {
  const rect = canvas!.getBoundingClientRect();
  dragging = getNodeAt(e.clientX - rect.left, e.clientY - rect.top);
  if (dragging) {
    // Restart animation on drag
    settledFrames = 0;
    if (animFrame === null) {
      startAnimation();
    }
  }
}

function onMouseMove(e: MouseEvent): void {
  if (!canvas || !tooltip) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left, my = e.clientY - rect.top;

  if (dragging) {
    dragging.x = mx;
    dragging.y = my;
    dragging.vx = 0;
    dragging.vy = 0;
    return;
  }

  const node = getNodeAt(mx, my);
  hovering = node;
  canvas.style.cursor = node ? 'pointer' : 'default';

  if (node) {
    const sess = node.session;
    const statusText = sess.isActive ? sess.status.replace('_', ' ').toUpperCase() : 'DONE';
    const statusClass = sess.status === 'waiting' ? 'color:#ffa64a'
      : sess.status === 'thinking' ? 'color:#ffcc00'
      : sess.status === 'tool_use' ? 'color:#4488ff'
      : 'color:#888';
    const toolName = sess.status === 'tool_use' ? (getLastTool(sess.id) || '') : '';
    const errHtml = sess.errorCount > 0 ? `<div style="color:#ff6b6b">${sess.errorCount} errors</div>` : '';

    tooltip.innerHTML = `
      <div><b>${escapeHtml(node.label)}</b> <span style="${statusClass};font-size:9px">${statusText}</span></div>
      ${toolName ? `<div style="color:#4488ff;font-size:10px">${escapeHtml(toolName)}</div>` : ''}
      <div>$${sess.totalCost.toFixed(2)}${sess.costRate > 0 ? ` ($${sess.costRate.toFixed(3)}/min)` : ''}</div>
      ${errHtml}`;
    tooltip.style.left = `${mx + 15}px`;
    tooltip.style.top = `${my + 15}px`;
    tooltip.classList.add('visible');
  } else {
    tooltip.classList.remove('visible');
  }
}

function onMouseUp(): void {
  dragging = null;
}

function onClick(e: MouseEvent): void {
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const node = getNodeAt(e.clientX - rect.left, e.clientY - rect.top);
  if (node) {
    update({ selectedSessionId: node.id, view: 'list' });
  }
}

function startAnimation(): void {
  function loop() {
    simulate();
    draw();
    // Stop if settled for enough frames
    if (settledFrames >= SETTLE_FRAMES) {
      animFrame = null;
      return;
    }
    animFrame = requestAnimationFrame(loop);
  }
  animFrame = requestAnimationFrame(loop);
}

function stopAnimation(): void {
  if (animFrame !== null) {
    cancelAnimationFrame(animFrame);
    animFrame = null;
  }
}
