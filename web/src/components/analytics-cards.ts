import { Chart, COLORS, DARK_THEME } from '../chart-config';
import { formatTokens, escapeHtml } from '../utils';
import type { TrendResult, ToolUsage, ToolUsageEntry } from '../types';

type CardState = Record<string, boolean>;
type OnToggle = (cardId: string, expanded: boolean) => void;

interface CardDef {
  id: string;
  title: string;
  subtitle: string;
  defaultExpanded: boolean;
  render: (canvas: HTMLCanvasElement, data: TrendResult) => Chart;
}

function darkTheme(): Record<string, unknown> {
  return JSON.parse(JSON.stringify(DARK_THEME));
}

// Short, readable axis label for a session bar: the session name/prompt trimmed
// to one line, falling back to a short id when unnamed.
function sessionLabel(name: string, id: string): string {
  const trimmed = (name || '').replace(/\s+/g, ' ').trim();
  if (trimmed) return trimmed.length > 36 ? trimmed.slice(0, 35) + '…' : trimmed;
  return id.length > 12 ? id.slice(0, 12) : id;
}

const CARD_DEFS: CardDef[] = [
  {
    id: 'cost-trend',
    title: 'Cost Over Time',
    subtitle: 'area line chart',
    defaultExpanded: true,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      const values = data.buckets.map((b) => b.cost);
      const opts = darkTheme() as any;
      return new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'Cost ($)',
              data: values,
              borderColor: COLORS.green,
              backgroundColor: COLORS.green + '33',
              fill: true,
              tension: 0.3,
              pointRadius: 2,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'token-consumption',
    title: 'Token Consumption',
    subtitle: 'stacked bar',
    defaultExpanded: true,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      const opts = darkTheme() as any;
      opts.scales.x.stacked = true;
      opts.scales.y.stacked = true;
      opts.scales.y.ticks = { ...opts.scales.y.ticks, callback: (v: number) => formatTokens(v) };
      return new Chart(canvas, {
        type: 'bar',
        data: {
          labels,
          datasets: [
            {
              label: 'Cache Read',
              data: data.buckets.map((b) => b.cacheReadTokens),
              backgroundColor: COLORS.green,
            },
            {
              label: 'Input',
              data: data.buckets.map((b) => b.inputTokens),
              backgroundColor: COLORS.blue,
            },
            {
              label: 'Cache Create',
              data: data.buckets.map((b) => b.cacheCreationTokens),
              backgroundColor: COLORS.purple,
            },
            {
              label: 'Output',
              data: data.buckets.map((b) => b.outputTokens),
              backgroundColor: COLORS.orange,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'cost-by-session',
    title: 'Cost by Session',
    subtitle: 'horizontal bar · subagents rolled into their session',
    defaultExpanded: true,
    render(canvas, data) {
      const rows = data.bySession ?? [];
      const labels = rows.map((s) => sessionLabel(s.sessionName, s.sessionId));
      const values = rows.map((s) => s.cost);
      const opts = darkTheme() as any;
      opts.indexAxis = 'y';
      return new Chart(canvas, {
        type: 'bar',
        data: {
          labels,
          datasets: [
            {
              label: 'Cost ($)',
              data: values,
              backgroundColor: COLORS.green,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'cost-by-repo',
    title: 'Cost by Repo',
    // Project attribution is approximate: a session's entire cost is booked to
    // the project it started in, so a run spanning multiple projects is not
    // split. Cost by Session (above) is the exact unit.
    subtitle: '⚠ approximate — whole-session cost booked to its starting project',
    defaultExpanded: false,
    render(canvas, data) {
      const labels = data.byRepo.map((r) => r.repoName || r.repoId);
      const values = data.byRepo.map((r) => r.cost);
      const opts = darkTheme() as any;
      opts.indexAxis = 'y';
      return new Chart(canvas, {
        type: 'bar',
        data: {
          labels,
          datasets: [
            {
              label: 'Cost ($)',
              data: values,
              backgroundColor: COLORS.blue,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'model-mix',
    title: 'Model Mix',
    subtitle: 'doughnut',
    defaultExpanded: false,
    render(canvas, data) {
      // Drop internal placeholders ('<synthetic>') and unknown/empty models so
      // real (e.g. opus) spend is not bucketed under a fake model label — and
      // so this view stays consistent with the cost-breakdown popover.
      const entries = data.byModel.filter(
        (m) => m.model && m.model !== '<synthetic>' && m.model !== 'unknown',
      );
      const labels = entries.map((m) => m.model);
      const values = entries.map((m) => m.cost);
      const colors = [
        COLORS.green,
        COLORS.blue,
        COLORS.purple,
        COLORS.orange,
        COLORS.red,
        COLORS.yellow,
      ];
      const opts = darkTheme() as any;
      // Doughnut doesn't use x/y scales
      delete opts.scales;
      return new Chart(canvas, {
        type: 'doughnut',
        data: {
          labels,
          datasets: [
            {
              data: values,
              backgroundColor: colors.slice(0, labels.length),
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'cache-efficiency',
    title: 'Cache Efficiency Over Time',
    subtitle: 'line chart',
    defaultExpanded: false,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      const values = data.buckets.map((b) => b.cacheHitPct);
      const opts = darkTheme() as any;
      opts.scales.y.min = 0;
      opts.scales.y.max = 100;
      return new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'Cache Hit %',
              data: values,
              borderColor: COLORS.orange,
              backgroundColor: COLORS.orange + '33',
              fill: true,
              tension: 0.3,
              pointRadius: 2,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'session-cost-dist',
    title: 'Session Cost Distribution',
    subtitle: 'multi-line',
    defaultExpanded: false,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      const opts = darkTheme() as any;
      return new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            // Plot null (a line gap) for buckets with no top-level sessions so
            // the per-session lines do not falsely dive to $0 in hours that
            // only contain subagent/child spend (sessionCount === 0).
            {
              label: 'Average',
              data: data.buckets.map((b) => (b.sessionCount > 0 ? b.avgSessionCost : null)),
              borderColor: COLORS.blue,
              tension: 0.3,
              pointRadius: 2,
              spanGaps: false,
            },
            {
              label: 'Median',
              data: data.buckets.map((b) => (b.sessionCount > 0 ? b.medianSessionCost : null)),
              borderColor: COLORS.green,
              tension: 0.3,
              pointRadius: 2,
              spanGaps: false,
            },
            {
              label: 'P95',
              data: data.buckets.map((b) => (b.sessionCount > 0 ? b.p95SessionCost : null)),
              borderColor: COLORS.red,
              tension: 0.3,
              pointRadius: 2,
              spanGaps: false,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'tokens-per-session',
    title: 'Tokens per Session',
    subtitle: 'line chart',
    defaultExpanded: false,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      // Plot null (a line gap) for buckets with no top-level sessions so the
      // line does not falsely dive to 0 in subagent-only hours.
      const values = data.buckets.map((b) => (b.sessionCount > 0 ? b.avgSessionTokens : null));
      const opts = darkTheme() as any;
      opts.scales.y.ticks = { ...opts.scales.y.ticks, callback: (v: number) => formatTokens(v) };
      return new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'Avg Tokens/Session',
              data: values,
              borderColor: COLORS.purple,
              backgroundColor: COLORS.purple + '33',
              fill: true,
              tension: 0.3,
              pointRadius: 2,
              spanGaps: false,
            },
          ],
        },
        options: opts,
      });
    },
  },
  {
    id: 'output-input-ratio',
    title: 'Output / Input Ratio',
    subtitle: 'line chart',
    defaultExpanded: false,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      const values = data.buckets.map((b) => b.outputInputRatio);
      const opts = darkTheme() as any;
      return new Chart(canvas, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'Output/Input',
              data: values,
              borderColor: COLORS.yellow,
              backgroundColor: COLORS.yellow + '33',
              fill: true,
              tension: 0.3,
              pointRadius: 2,
            },
          ],
        },
        options: opts,
      });
    },
  },
];

const charts = new Map<string, Chart>();

export function renderCards(
  container: HTMLElement,
  data: TrendResult,
  cardState: CardState,
  onToggle: OnToggle,
): void {
  destroyCards();
  container.innerHTML = '';

  for (const def of CARD_DEFS) {
    const expanded = cardState[def.id] !== undefined ? cardState[def.id] : def.defaultExpanded;

    const card = document.createElement('div');
    card.className = 'analytics-card';
    card.dataset.cardId = def.id;

    const header = document.createElement('div');
    header.className = 'analytics-card-header';
    header.setAttribute('role', 'button');
    header.setAttribute('tabindex', '0');
    header.setAttribute('aria-expanded', String(expanded));
    header.innerHTML = `
      <span class="analytics-card-toggle">${expanded ? '▼' : '▶'}</span>
      <span class="analytics-card-title">${def.title}</span>
      <span class="analytics-card-subtitle">${def.subtitle}</span>
    `;

    const body = document.createElement('div');
    body.className = 'analytics-card-body';
    body.style.display = expanded ? '' : 'none';

    const canvas = document.createElement('canvas');
    canvas.height = 200;
    body.appendChild(canvas);

    card.appendChild(header);
    card.appendChild(body);
    container.appendChild(card);

    // Create chart if expanded
    if (expanded) {
      const chart = def.render(canvas, data);
      charts.set(def.id, chart);
    }

    header.addEventListener('click', () => {
      const isExpanded = body.style.display !== 'none';
      if (isExpanded) {
        // Collapse
        body.style.display = 'none';
        header.querySelector('.analytics-card-toggle')!.textContent = '▶';
        header.setAttribute('aria-expanded', 'false');
        const existing = charts.get(def.id);
        if (existing) {
          existing.destroy();
          charts.delete(def.id);
        }
        onToggle(def.id, false);
      } else {
        // Expand
        body.style.display = '';
        header.querySelector('.analytics-card-toggle')!.textContent = '▼';
        header.setAttribute('aria-expanded', 'true');
        const chart = def.render(canvas, data);
        charts.set(def.id, chart);
        onToggle(def.id, true);
      }
    });
    header.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        header.click();
      }
    });
  }
}

export function destroyCards(): void {
  for (const chart of charts.values()) {
    chart.destroy();
  }
  charts.clear();
}

// SKILL_COLOR matches the feed's skill accent so the surfaces feel related.
const SKILL_COLOR = '#e879f9';
const TOOL_COLOR = COLORS.yellow;

/** Render the (non-chart) "Tool & Skill Usage" card: two ranked lists of
 *  invocation counts with error counts, skills broken out separately. Appended
 *  after the chart cards by analytics-view; manages its own collapse toggle. */
export function renderToolUsageCard(
  container: HTMLElement,
  usage: ToolUsage | null,
  expanded: boolean,
  onToggle: (expanded: boolean) => void,
): void {
  const card = document.createElement('div');
  card.className = 'analytics-card';
  card.dataset.cardId = 'tool-skill-usage';

  const header = document.createElement('div');
  header.className = 'analytics-card-header';
  header.setAttribute('role', 'button');
  header.setAttribute('tabindex', '0');
  header.setAttribute('aria-expanded', String(expanded));
  header.innerHTML = `
    <span class="analytics-card-toggle">${expanded ? '▼' : '▶'}</span>
    <span class="analytics-card-title">Tool &amp; Skill Usage</span>
    <span class="analytics-card-subtitle">invocations &amp; errors</span>
  `;

  const body = document.createElement('div');
  body.className = 'analytics-card-body';
  body.style.display = expanded ? '' : 'none';
  body.appendChild(buildToolUsageBody(usage));

  card.appendChild(header);
  card.appendChild(body);
  container.appendChild(card);

  header.addEventListener('click', () => {
    const isExpanded = body.style.display !== 'none';
    body.style.display = isExpanded ? 'none' : '';
    header.querySelector('.analytics-card-toggle')!.textContent = isExpanded ? '▶' : '▼';
    header.setAttribute('aria-expanded', String(!isExpanded));
    onToggle(!isExpanded);
  });
  header.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      header.click();
    }
  });
}

function buildToolUsageBody(usage: ToolUsage | null): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'tool-usage';
  const skills = usage?.skills ?? [];
  const tools = usage?.tools ?? [];
  if (skills.length === 0 && tools.length === 0) {
    wrap.innerHTML = '<div class="analytics-empty">No tool or skill activity in this period</div>';
    return wrap;
  }
  wrap.appendChild(buildUsageSection('Skills', skills, SKILL_COLOR, 'No skills invoked'));
  wrap.appendChild(buildUsageSection('Tools', tools, TOOL_COLOR, 'No tools used'));
  return wrap;
}

function buildUsageSection(
  title: string,
  entries: ToolUsageEntry[],
  color: string,
  emptyText: string,
): HTMLElement {
  const section = document.createElement('div');
  section.className = 'tool-usage-section';

  const heading = document.createElement('div');
  heading.className = 'tool-usage-section-title';
  heading.textContent = `${title} (${entries.length})`;
  section.appendChild(heading);

  if (entries.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'tool-usage-empty';
    empty.textContent = emptyText;
    section.appendChild(empty);
    return section;
  }

  const max = Math.max(...entries.map((e) => e.uses), 1);
  for (const e of entries) {
    const row = document.createElement('div');
    row.className = 'tool-usage-row';
    // Floor at 2% so a 1-use bar is still visible.
    const pct = Math.max(2, Math.round((e.uses / max) * 100));
    const errBadge =
      e.errors > 0
        ? `<span class="tool-usage-err" title="${e.errors} invocation(s) errored">${e.errors} err</span>`
        : '';
    row.innerHTML =
      `<span class="tool-usage-name" title="${escapeHtml(e.name)}">${escapeHtml(e.name)}</span>` +
      `<span class="tool-usage-bar-wrap"><span class="tool-usage-bar" style="width:${pct}%;background:${color}"></span></span>` +
      `<span class="tool-usage-count">${e.uses}</span>` +
      errBadge;
    section.appendChild(row);
  }
  return section;
}
