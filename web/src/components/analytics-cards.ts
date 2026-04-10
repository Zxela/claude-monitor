import { Chart, COLORS, DARK_THEME } from '../chart-config';
import { formatTokens } from '../utils';
import type { TrendResult } from '../types';

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
    id: 'cost-by-repo',
    title: 'Cost by Repo',
    subtitle: 'horizontal bar',
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
      const labels = data.byModel.map((m) => m.model);
      const values = data.byModel.map((m) => m.cost);
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
            {
              label: 'Average',
              data: data.buckets.map((b) => b.avgSessionCost),
              borderColor: COLORS.blue,
              tension: 0.3,
              pointRadius: 2,
            },
            {
              label: 'Median',
              data: data.buckets.map((b) => b.medianSessionCost),
              borderColor: COLORS.green,
              tension: 0.3,
              pointRadius: 2,
            },
            {
              label: 'P95',
              data: data.buckets.map((b) => b.p95SessionCost),
              borderColor: COLORS.red,
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
    id: 'tokens-per-session',
    title: 'Tokens per Session',
    subtitle: 'line chart',
    defaultExpanded: false,
    render(canvas, data) {
      const labels = data.buckets.map((b) => b.date);
      const values = data.buckets.map((b) => b.avgSessionTokens);
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
