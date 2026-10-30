import { afterEach, describe, it, expect, vi } from 'vitest';
import { render } from './table-view';
import { update } from '../state';
import type { Session } from '../types';

const session = (id: string) =>
  ({
    id,
    totalCost: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    messageCount: 0,
    eventCount: 0,
    lastActive: '2024-01-01T00:00:00Z',
    isActive: false,
    startedAt: '2024-01-01T00:00:00Z',
    status: 'idle',
    costRate: 0,
    errorCount: 0,
    taskDescription: '',
  }) as Session;

const grouped = (n: number) => ({
  active: [],
  lastHour: Array.from({ length: n }, (_, i) => session(`s${i}`)),
  today: [],
  yesterday: [],
  thisWeek: [],
  older: [],
});

describe('table view re-render', () => {
  it('keeps the scroll position', () => {
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    update({ view: 'table', grouped: grouped(50) });
    render(mount);

    const scroller = mount.querySelector<HTMLElement>('.table-scroll')!;
    scroller.scrollTop = 120;
    scroller.scrollLeft = 80;
    update({ grouped: grouped(51) });

    expect(mount.querySelectorAll('tbody tr').length).toBe(51);
    const next = mount.querySelector<HTMLElement>('.table-scroll')!;
    expect(next.scrollTop).toBe(120);
    expect(next.scrollLeft).toBe(80);
  });

  afterEach(() => vi.useRealTimers());

  it('batches live session updates into one rebuild a second', () => {
    vi.useFakeTimers();
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    update({ view: 'table', grouped: grouped(3) });
    render(mount);

    const first = mount.querySelector('table');
    for (let i = 0; i < 5; i++) update({ sessions: new Map() });
    expect(vi.getTimerCount()).toBe(1);
    vi.advanceTimersByTime(999);
    expect(mount.querySelector('table')).toBe(first);
    vi.advanceTimersByTime(1);
    const second = mount.querySelector('table');
    expect(second).not.toBe(first);
    expect(vi.getTimerCount()).toBe(0);

    update({ sessions: new Map() });
    vi.advanceTimersByTime(1000);
    expect(mount.querySelector('table')).not.toBe(second);
  });
});
