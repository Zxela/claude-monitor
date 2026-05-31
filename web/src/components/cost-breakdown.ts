import { state } from '../state';
import { escapeHtml, sessionDisplayName } from '../utils';
import { COLORS } from '../colors';
import { dismiss as dismissBudget } from './budget-popover';
import '../styles/views.css';

let popover: HTMLElement | null = null;

/** Close the cost breakdown if open. Called by other dialogs to avoid overlap. */
export function dismiss(): void {
  if (popover) {
    popover.remove();
    popover = null;
  }
}

export function toggle(anchor: HTMLElement): void {
  if (popover) {
    popover.remove();
    popover = null;
    return;
  }
  dismissBudget();

  const stats = state.stats;
  if (!stats) return;

  const byModel = new Map<string, number>();
  for (const [model, cost] of Object.entries(stats.costByModel)) {
    if (model === '<synthetic>' || model === 'unknown') continue; // skip internal placeholders
    byModel.set(model, cost);
  }
  // Sum only the displayed (non-skipped) models. Using stats.totalCost as the
  // denominator left an unexplained empty wedge and made the legend dollars
  // fall short of the center total by the excluded synthetic/unknown cost.
  const displayedCost = [...byModel.values()].reduce((a, b) => a + b, 0);

  const totalInput = stats.inputTokens;
  const totalOutput = stats.outputTokens;
  const totalCache = stats.cacheReadTokens;

  const allSessions = Array.from(state.sessions.values());
  const top5 = [...allSessions].sort((a, b) => b.totalCost - a.totalCost).slice(0, 5);

  popover = document.createElement('div');
  popover.className = 'cost-breakdown';
  popover.setAttribute('role', 'dialog');
  popover.setAttribute('aria-modal', 'false');
  popover.setAttribute('aria-label', 'Cost breakdown');
  popover.addEventListener('click', (e) => e.stopPropagation());

  // Build content
  popover.innerHTML = `
    <div class="cb-header">Cost Breakdown</div>
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
  // Assign colors deterministically by descending-cost rank so every model gets
  // a distinct slice. The old static lookup mapped all unrecognized models
  // (e.g. current opus-4-7/4-8) to the same '#888' gray, making the two largest
  // slices visually indistinguishable.
  const PALETTE = [
    COLORS.purple, COLORS.cyan, COLORS.green, COLORS.orange,
    COLORS.yellow, COLORS.red, COLORS.user, COLORS.statusIdle,
  ];
  const colorFor = (i: number): string => PALETTE[i % PALETTE.length];
  const cx = 60,
    cy = 60,
    r = 45,
    inner = 25;
  let angle = -Math.PI / 2;
  const legend = popover.querySelector('.cb-legend')!;

  const sorted = [...byModel.entries()].sort((a, b) => b[1] - a[1]);
  for (const [i, [model, cost]] of sorted.entries()) {
    const slice = displayedCost > 0 ? (cost / displayedCost) * Math.PI * 2 : 0;
    const color = colorFor(i);
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.moveTo(cx, cy);
    ctx.arc(cx, cy, r, angle, angle + slice);
    ctx.closePath();
    ctx.fill();
    angle += slice;

    const shortName = model.replace('claude-', '').replace('-4-6', '').replace('-4-5-20251001', '');
    const pct = displayedCost > 0 ? ((cost / displayedCost) * 100).toFixed(0) : '0';
    legend.innerHTML += `<div style="font-size:10px;display:flex;align-items:center;gap:4px;margin:2px 0">
      <span style="width:8px;height:8px;border-radius:50%;background:${escapeHtml(color)};display:inline-block;flex-shrink:0"></span>
      <span style="color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0">${escapeHtml(shortName)}</span>
      <span style="color:var(--text-dim);margin-left:auto;flex-shrink:0">$${cost.toFixed(0)} (${pct}%)</span>
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
  // Center equals the legend sum (displayed models only) so the donut, legend
  // dollars, and center total all reconcile and percentages sum to 100%.
  ctx.fillText(`$${displayedCost.toFixed(0)}`, cx, cy + 4);

  // Token bars
  const totalTok = totalInput + totalOutput + totalCache;
  const bars = popover.querySelectorAll<HTMLElement>('.cb-bar-fill');
  const vals = popover.querySelectorAll<HTMLElement>('.cb-bar-val');
  if (totalTok > 0) {
    bars[0].style.width = `${((totalInput / totalTok) * 100).toFixed(0)}%`;
    bars[0].style.background = COLORS.user;
    bars[1].style.width = `${((totalOutput / totalTok) * 100).toFixed(0)}%`;
    bars[1].style.background = COLORS.orange;
    bars[2].style.width = `${((totalCache / totalTok) * 100).toFixed(0)}%`;
    bars[2].style.background = COLORS.green;
  }
  const fmt = (n: number) =>
    n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(1)}k` : String(n);
  vals[0].textContent = fmt(totalInput);
  vals[1].textContent = fmt(totalOutput);
  vals[2].textContent = fmt(totalCache);

  // Top 5
  const top5El = popover.querySelector('.cb-top5')!;
  for (const s of top5) {
    const name = sessionDisplayName(s);
    top5El.innerHTML += `<div style="display:flex;justify-content:space-between;font-size:10px;padding:1px 0">
      <span style="color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:150px">${escapeHtml(name)}</span>
      <span style="color:var(--yellow)">$${s.totalCost.toFixed(2)}</span>
    </div>`;
  }

  // Position below the anchor
  anchor.parentElement!.style.position = 'relative';
  anchor.parentElement!.appendChild(popover);

  // Close on outside click
  const closeHandler = () => {
    if (popover) {
      popover.remove();
      popover = null;
    }
    document.removeEventListener('click', closeHandler);
  };
  setTimeout(() => document.addEventListener('click', closeHandler), 0);
}
