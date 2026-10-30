import { describe, it, expect, vi, beforeAll } from 'vitest';
import type { Stats } from '../types';

vi.mock('../api', () => ({
  fetchStats: vi.fn(() => new Promise(() => {})),
}));

import { render as renderBudget } from './budget-popover';
import { toggle as toggleCostBreakdown } from './cost-breakdown';
import { update } from '../state';

const key = (el: Element, k: string) =>
  el.dispatchEvent(
    new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }),
  );

describe('popover focus', () => {
  let gear: HTMLElement;
  let cost: HTMLElement;

  beforeAll(() => {
    // jsdom has no canvas; the popovers only draw on it.
    const ctx = new Proxy({}, { get: () => () => ({ addColorStop() {} }) });
    HTMLCanvasElement.prototype.getContext = (() =>
      ctx) as unknown as HTMLCanvasElement['getContext'];

    document.body.innerHTML = `
      <div class="topbar">
        <div class="topbar-stat">
          <span class="budget-gear" role="button" tabindex="0">⚙</span>
          <span data-stat="cost" role="button" tabindex="0">$0</span>
        </div>
      </div>
      <div id="app"></div>`;
    gear = document.querySelector('.budget-gear')!;
    cost = document.querySelector('[data-stat="cost"]')!;
    renderBudget(gear, cost, document.getElementById('app')!);
    update({
      stats: {
        totalCost: 1,
        inputTokens: 1,
        outputTokens: 1,
        cacheReadTokens: 1,
        cacheCreationTokens: 0,
        sessionCount: 1,
        agentCount: 0,
        activeSessions: 0,
        cacheHitPct: 0,
        costRate: 0,
        costByModel: { 'claude-opus': 1 },
        costByRepo: {},
      } as Stats,
    });
  });

  it('budget popover takes focus and Escape hands it back', () => {
    gear.focus();
    key(gear, 'Enter');
    const panel = document.querySelector<HTMLElement>('.burn-rate-panel')!;
    expect(panel.getAttribute('role')).toBe('dialog');
    expect(document.activeElement).toBe(panel);

    const toggle = panel.querySelector<HTMLElement>(
      '.burn-rate-settings-toggle',
    )!;
    expect(toggle.getAttribute('tabindex')).toBe('0');
    toggle.focus();
    key(toggle, 'Enter');
    expect(toggle.getAttribute('aria-expanded')).toBe('true');

    const set = panel.querySelector<HTMLElement>('.set-btn')!;
    panel.querySelector<HTMLInputElement>('input[type="number"]')!.value = '5';
    set.focus();
    set.click();
    expect(panel.contains(document.activeElement)).toBe(true);

    key(document.activeElement!, 'Escape');
    expect(document.querySelector('.burn-rate-panel')).toBeNull();
    expect(document.activeElement).toBe(gear);
  });

  it('cost breakdown takes focus and Escape hands it back', () => {
    cost.focus();
    toggleCostBreakdown(cost);
    const popover = document.querySelector<HTMLElement>('.cost-breakdown')!;
    expect(document.activeElement).toBe(popover);

    key(popover, 'Escape');
    expect(document.querySelector('.cost-breakdown')).toBeNull();
    expect(document.activeElement).toBe(cost);
  });
});
