import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { Session, Stats } from '../types';

const mocks = vi.hoisted(() => ({
  fetchStats: vi.fn(),
  notify: vi.fn(),
}));
vi.mock('../api', () => ({ fetchStats: mocks.fetchStats }));
vi.mock('../notifications', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../notifications')>()),
  notify: mocks.notify,
}));

const stats = (over: Partial<Stats> = {}): Stats =>
  ({ totalCost: 0, costRate: 0, ...over }) as Stats;

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

let dismissPanel: (() => void) | undefined;

async function setup() {
  vi.resetModules();
  document.body.innerHTML = `
    <div class="topbar">
      <div class="topbar-stat"><span class="cost"></span></div>
      <div class="topbar-stat" data-stat="rate"><span class="rate"></span></div>
      <div class="topbar-stat"><button class="gear"></button></div>
    </div>
    <div id="banner-mount"></div>`;
  const st = await import('../state');
  const { render, dismiss } = await import('./budget-popover');
  dismissPanel = dismiss;
  const gear = document.querySelector<HTMLElement>('.gear')!;
  const costEl = document.querySelector<HTMLElement>('.cost')!;
  const rateStat = document.querySelector<HTMLElement>('[data-stat="rate"]')!;
  render(gear, costEl, document.getElementById('banner-mount')!);
  const banner = document.querySelector<HTMLElement>('.budget-banner')!;
  const panel = () => document.querySelector<HTMLElement>('.burn-rate-panel');
  return { ...st, gear, costEl, rateStat, banner, panel };
}

beforeEach(() => {
  localStorage.clear();
  mocks.fetchStats.mockReset().mockResolvedValue(stats());
  mocks.notify.mockReset();
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null);
});

afterEach(() => {
  dismissPanel?.();
  vi.useRealTimers();
});

describe('budget banner', () => {
  it('restores the saved budget and window on render', async () => {
    localStorage.setItem('budget', '25');
    localStorage.setItem('budget-window', 'month');
    const { state } = await setup();

    expect(state.budgetThreshold).toBe(25);
    expect(state.statsWindow).toBe('month');
  });

  it('flags the cost and notifies once when spend reaches the budget', async () => {
    const { update, costEl, banner } = await setup();
    update({ budgetThreshold: 10 });
    expect(banner.classList.contains('hidden')).toBe(true);

    update({ stats: stats({ totalCost: 12 }) });
    expect(costEl.classList.contains('over-budget')).toBe(true);
    expect(banner.classList.contains('hidden')).toBe(false);
    expect(banner.textContent).toContain('Budget exceeded: $12 / $10');

    update({ stats: stats({ totalCost: 13 }) });
    expect(mocks.notify).toHaveBeenCalledTimes(1);
    expect(mocks.notify).toHaveBeenCalledWith(
      'budget',
      'Budget Exceeded',
      'Total spend $12 exceeds $10',
    );
  });

  it('hides the banner on dismiss but keeps the cost flagged', async () => {
    const { update, costEl, banner, state } = await setup();
    update({ budgetThreshold: 10, stats: stats({ totalCost: 12 }) });

    banner.querySelector('button')!.click();

    expect(state.budgetDismissed).toBe(true);
    expect(banner.classList.contains('hidden')).toBe(true);
    expect(costEl.classList.contains('over-budget')).toBe(true);

    update({ stats: stats({ totalCost: 14 }) });
    expect(banner.classList.contains('hidden')).toBe(true);
    expect(mocks.notify).toHaveBeenCalledTimes(1);
  });

  it('clears the warning when spend drops below the budget, and notifies again next time', async () => {
    const { update, costEl, banner } = await setup();
    update({ budgetThreshold: 10, stats: stats({ totalCost: 12 }) });

    update({ stats: stats({ totalCost: 5 }) });
    expect(costEl.classList.contains('over-budget')).toBe(false);
    expect(banner.classList.contains('hidden')).toBe(true);

    update({ stats: stats({ totalCost: 11 }) });
    expect(mocks.notify).toHaveBeenCalledTimes(2);
  });

  it('clears the warning when the budget is removed', async () => {
    const { update, costEl, banner } = await setup();
    update({ budgetThreshold: 10, stats: stats({ totalCost: 12 }) });

    update({ budgetThreshold: null });

    expect(costEl.classList.contains('over-budget')).toBe(false);
    expect(banner.classList.contains('hidden')).toBe(true);
  });
});

describe('budget popover panel', () => {
  it('opens from the gear or the $/MIN stat and closes on an outside click', async () => {
    const { gear, rateStat, panel } = await setup();

    gear.click();
    expect(panel()?.getAttribute('role')).toBe('dialog');
    expect(panel()!.parentElement).toBe(gear.closest('.topbar-stat'));

    panel()!.click();
    expect(panel()).not.toBeNull();

    document.body.click();
    expect(panel()).toBeNull();

    rateStat.click();
    expect(panel()!.parentElement).toBe(rateStat);

    rateStat.click();
    expect(panel()).toBeNull();
  });

  it('opens from the keyboard', async () => {
    const { gear, panel } = await setup();
    gear.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    expect(panel()).not.toBeNull();
  });

  it("projects from today's spend, not the windowed total", async () => {
    mocks.fetchStats.mockResolvedValue(stats({ totalCost: 7 }));
    const { gear, panel, update } = await setup();
    update({ statsWindow: 'all', stats: stats({ totalCost: 900 }) });
    await flush();

    gear.click();

    expect(mocks.fetchStats).toHaveBeenCalledWith('today');
    const metric = panel()!.querySelector('.burn-rate-metric-value');
    expect(metric?.textContent).toBe('$7.00');
  });

  it('lists active sessions with their rate', async () => {
    const { gear, panel, state } = await setup();
    state.sessions.set('a', {
      id: 'a',
      sessionName: 'alpha',
      isActive: true,
      costRate: 0.25,
      model: 'claude-opus-4-6',
    } as Session);
    state.sessions.set('b', { id: 'b', isActive: false } as Session);

    gear.click();

    const rows = panel()!.querySelectorAll('.burn-rate-session-row');
    expect(rows).toHaveLength(1);
    expect(rows[0].textContent).toContain('alpha');
    expect(rows[0].textContent).toContain('$0.250/min');
    expect(rows[0].textContent).toContain('opus');
  });

  it('shows budget progress coloured by how close spend is', async () => {
    const { gear, panel, update } = await setup();
    update({ budgetThreshold: 100, stats: stats({ totalCost: 85 }) });

    gear.click();

    const fill = panel()!.querySelector<HTMLElement>('.burn-rate-budget-fill')!;
    expect(fill.style.width).toBe('85%');
    expect(fill.style.background).toBe('rgb(255, 166, 74)');
    const text = panel()!.querySelector('.burn-rate-budget-text')!.textContent;
    expect(text?.replace(/\s+/g, ' ').trim()).toBe('85% $85.00 / $100');
  });

  it('refreshes values every 5s while open and stops after closing', async () => {
    vi.useFakeTimers();
    const { gear, panel, update } = await setup();
    update({ budgetThreshold: 100, stats: stats({ totalCost: 10 }) });
    gear.click();
    const callsAfterOpen = mocks.fetchStats.mock.calls.length;

    update({ stats: stats({ totalCost: 50 }) });
    vi.advanceTimersByTime(5000);
    expect(
      panel()!.querySelector<HTMLElement>('.burn-rate-budget-fill')!.style
        .width,
    ).toBe('50%');
    expect(mocks.fetchStats.mock.calls.length).toBe(callsAfterOpen + 1);

    document.body.click();
    vi.advanceTimersByTime(20000);
    expect(mocks.fetchStats.mock.calls.length).toBe(callsAfterOpen + 1);
  });
});

describe('budget popover settings', () => {
  async function openSettings() {
    const ctx = await setup();
    ctx.gear.click();
    ctx
      .panel()!
      .querySelector<HTMLElement>('.burn-rate-settings-toggle')!
      .click();
    const q = <T extends Element>(sel: string) =>
      ctx.panel()!.querySelector<T>(sel)!;
    return { ...ctx, q };
  }

  it('saves a positive budget and keeps the settings open', async () => {
    const { q, state } = await openSettings();
    q<HTMLInputElement>('input[type="number"]').value = '42';
    q<HTMLButtonElement>('.set-btn').click();

    expect(state.budgetThreshold).toBe(42);
    expect(localStorage.getItem('budget')).toBe('42');
    expect(q<HTMLElement>('.burn-rate-settings').style.display).toBe('');
    expect(q('.burn-rate-budget')).not.toBeNull();
  });

  it('ignores a zero, negative or empty budget', async () => {
    const { q, state } = await openSettings();
    const input = q<HTMLInputElement>('input[type="number"]');
    for (const v of ['0', '-5', '']) {
      input.value = v;
      q<HTMLButtonElement>('.set-btn').click();
    }
    expect(state.budgetThreshold).toBeNull();
    expect(localStorage.getItem('budget')).toBeNull();
  });

  it('clears the budget', async () => {
    localStorage.setItem('budget', '20');
    const { q, state } = await openSettings();

    q<HTMLButtonElement>('.clear-btn').click();

    expect(state.budgetThreshold).toBeNull();
    expect(localStorage.getItem('budget')).toBeNull();
    expect(q('.burn-rate-budget')).toBeNull();
  });

  it('persists the budget window', async () => {
    const { q, state } = await openSettings();
    const select = q<HTMLSelectElement>('.budget-window-select');

    select.value = 'week';
    select.dispatchEvent(new Event('change'));

    expect(state.statsWindow).toBe('week');
    expect(localStorage.getItem('budget-window')).toBe('week');
  });

  it('persists the notification toggles', async () => {
    const { q } = await openSettings();
    const box = q<HTMLInputElement>('.notif-error');

    box.checked = false;
    box.dispatchEvent(new Event('change'));

    expect(JSON.parse(localStorage.getItem('notif-settings')!)).toEqual({
      budget: true,
      error: false,
    });
  });
});
