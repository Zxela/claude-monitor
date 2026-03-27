import { state } from '../state';
import { escapeHtml } from '../utils';
import { COLORS } from '../colors';
import '../styles/views.css';

let popover: HTMLElement | null = null;
let cbFilter: 'all' | 'today' = 'all';

export function toggle(anchor: HTMLElement): void {
  if (popover) { popover.remove(); popover = null; return; }

  const allSessions = Array.from(state.sessions.values());
  const todayStr = new Date().toDateString();
  const sessions = cbFilter === 'today'
    ? allSessions.filter(s => new Date(s.startedAt).toDateString() === todayStr)
    : allSessions;

  // Aggregate by model
  const byModel = new Map<string, number>();
  let totalInput = 0, totalOutput = 0, totalCache = 0;
  for (const s of sessions) {
    const model = s.model || 'unknown';
    byModel.set(model, (byModel.get(model) || 0) + s.totalCostUSD);
    totalInput += s.inputTokens;
    totalOutput += s.outputTokens;
    totalCache += s.cacheReadTokens;
  }

  // Top 5 costliest
  const top5 = [...sessions].sort((a, b) => b.totalCostUSD - a.totalCostUSD).slice(0, 5);
  const totalCost = sessions.reduce((s, x) => s + x.totalCostUSD, 0);

  popover = document.createElement('div');
  popover.className = 'cost-breakdown';
  popover.setAttribute('role', 'dialog');
  popover.setAttribute('aria-modal', 'false');
  popover.setAttribute('aria-label', 'Cost breakdown');
  popover.addEventListener('click', e => e.stopPropagation());

  // Build content
  popover.innerHTML = `
    <div class="cb-header">Cost Breakdown</div>
    <div class="cb-filter" style="display:flex;gap:4px;padding:0 8px 6px">
      <button class="cb-filter-btn${cbFilter === 'all' ? ' active' : ''}" data-filter="all" style="font-family:var(--font-mono);font-size:9px;padding:2px 8px;background:none;border:1px solid var(--border);color:var(--text-dim);cursor:pointer;border-radius:2px">ALL</button>
      <button class="cb-filter-btn${cbFilter === 'today' ? ' active' : ''}" data-filter="today" style="font-family:var(--font-mono);font-size:9px;padding:2px 8px;background:none;border:1px solid var(--border);color:var(--text-dim);cursor:pointer;border-radius:2px">TODAY</button>
    </div>
    <div class="cb-row">
      <canvas class="cb-chart" width="120" height="120" role="img" aria-label="Cost by model donut chart"></canvas>
      <div class="cb-legend"></div>
    </div>
    <div class="cb-section">Tokens</div>
    <div class="cb-tokens">
      <div class="cb-bar-row"><span>Input</span><div class="cb-bar"><div class="cb-bar-fill" data-type="input"></div></div><span class="cb-bar-val"></span></div>
      <div class="cb-bar-row"><span>Output</span><div class="cb-bar"><div class="cb-bar-fill" data-type="output"></div></div><span class="cb-bar-val"></span></div>
      <div class="cb-bar-row"><span>Cache</span><div class="cb-bar"><div class="cb-bar-fill" data-type="cache"></div></div><span class="cb-bar-val"></span></div>
    </div>
    <div class="cb-section">Top Sessions</div>
    <div class="cb-top5"></div>
  `;

  // Draw donut chart
  const canvas = popover.querySelector<HTMLCanvasElement>('.cb-chart')!;
  const ctx = canvas.getContext('2d')!;
  const colors: Record<string, string> = {
    'claude-opus-4-6': COLORS.purple,
    'claude-sonnet-4-6': COLORS.cyan,
    'claude-haiku-4-5-20251001': COLORS.green,
    'unknown': COLORS.statusIdle,
  };
  const cx = 60, cy = 60, r = 45, inner = 25;
  let angle = -Math.PI / 2;
  const legend = popover.querySelector('.cb-legend')!;

  for (const [model, cost] of [...byModel.entries()].sort((a, b) => b[1] - a[1])) {
    const slice = totalCost > 0 ? (cost / totalCost) * Math.PI * 2 : 0;
    const color = colors[model] || '#888';
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.moveTo(cx, cy);
    ctx.arc(cx, cy, r, angle, angle + slice);
    ctx.closePath();
    ctx.fill();
    angle += slice;

    const shortName = model.replace('claude-', '').replace('-4-6', '').replace('-4-5-20251001', '');
    const pct = totalCost > 0 ? ((cost / totalCost) * 100).toFixed(0) : '0';
    legend.innerHTML += `<div style="font-size:10px;display:flex;align-items:center;gap:4px;margin:2px 0">
      <span style="width:8px;height:8px;border-radius:50%;background:${color};display:inline-block"></span>
      <span style="color:var(--text)">${shortName}</span>
      <span style="color:var(--text-dim);margin-left:auto">$${cost.toFixed(0)} (${pct}%)</span>
    </div>`;
  }
  // Cut out inner circle for donut
  ctx.fillStyle = COLORS.bgDeep;
  ctx.beginPath();
  ctx.arc(cx, cy, inner, 0, Math.PI * 2);
  ctx.fill();
  // Center text
  ctx.fillStyle = COLORS.text;
  ctx.font = 'bold 12px monospace';
  ctx.textAlign = 'center';
  ctx.fillText(`$${totalCost.toFixed(0)}`, cx, cy + 4);

  // Token bars
  const totalTok = totalInput + totalOutput + totalCache;
  const bars = popover.querySelectorAll<HTMLElement>('.cb-bar-fill');
  const vals = popover.querySelectorAll<HTMLElement>('.cb-bar-val');
  if (totalTok > 0) {
    bars[0].style.width = `${(totalInput / totalTok * 100).toFixed(0)}%`;
    bars[0].style.background = COLORS.user;
    bars[1].style.width = `${(totalOutput / totalTok * 100).toFixed(0)}%`;
    bars[1].style.background = COLORS.orange;
    bars[2].style.width = `${(totalCache / totalTok * 100).toFixed(0)}%`;
    bars[2].style.background = COLORS.green;
  }
  const fmt = (n: number) => n >= 1e6 ? `${(n/1e6).toFixed(1)}M` : n >= 1e3 ? `${(n/1e3).toFixed(1)}k` : String(n);
  vals[0].textContent = fmt(totalInput);
  vals[1].textContent = fmt(totalOutput);
  vals[2].textContent = fmt(totalCache);

  // Top 5
  const top5El = popover.querySelector('.cb-top5')!;
  for (const s of top5) {
    const name = s.sessionName || s.projectName || s.id.slice(0, 12);
    top5El.innerHTML += `<div style="display:flex;justify-content:space-between;font-size:10px;padding:1px 0">
      <span style="color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:150px">${escapeHtml(name)}</span>
      <span style="color:var(--yellow)">$${s.totalCostUSD.toFixed(2)}</span>
    </div>`;
  }

  // Filter button click handlers
  popover.querySelectorAll<HTMLButtonElement>('.cb-filter-btn').forEach(btn => {
    if (btn.dataset.filter === cbFilter) {
      btn.style.borderColor = 'var(--cyan)';
      btn.style.color = 'var(--text)';
    }
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      const newFilter = btn.dataset.filter as 'all' | 'today';
      if (newFilter === cbFilter) return;
      cbFilter = newFilter;
      // Re-render: close and reopen
      popover?.remove();
      popover = null;
      toggle(anchor);
    });
  });

  // Position below the anchor
  anchor.parentElement!.style.position = 'relative';
  anchor.parentElement!.appendChild(popover);

  // Close on outside click
  const closeHandler = () => { if (popover) { popover.remove(); popover = null; } document.removeEventListener('click', closeHandler); };
  setTimeout(() => document.addEventListener('click', closeHandler), 0);
}
