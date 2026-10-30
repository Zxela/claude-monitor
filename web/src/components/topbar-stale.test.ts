import { describe, it, expect, vi } from 'vitest';

const pending: {
  resolve: (v: unknown) => void;
  reject: (e: unknown) => void;
}[] = [];
vi.mock('../api', () => ({
  fetchStats: () =>
    new Promise((resolve, reject) => pending.push({ resolve, reject })),
}));
vi.mock('./cost-breakdown', () => ({ toggle: () => {} }));
vi.mock('../attention', () => ({ getAttentionCount: () => 0 }));

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('topbar stale stats', () => {
  it('flags failures on the stats and ignores an outdated one', async () => {
    const { render } = await import('./topbar');
    const mount = document.createElement('div');
    render(mount);
    const topbar = mount.querySelector<HTMLElement>('.topbar')!;
    const stat = mount.querySelector<HTMLElement>('.topbar-stat')!;
    const baseTitle = stat.title;

    pending[0].reject(new Error('down'));
    await tick();
    expect(topbar.classList.contains('stats-stale')).toBe(true);
    expect(stat.title).toContain('out of date');

    mount.querySelector<HTMLButtonElement>('.win-btn')!.click();
    mount.querySelector<HTMLButtonElement>('.win-btn')!.click();
    pending[2].resolve(null);
    await tick();
    pending[1].reject(new Error('slow'));
    await tick();
    expect(topbar.classList.contains('stats-stale')).toBe(false);
    expect(stat.title).toBe(baseTitle);
  });
});
