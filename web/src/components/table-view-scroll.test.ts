import { describe, it, expect } from 'vitest';
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
    update({ grouped: grouped(51) });

    expect(mount.querySelectorAll('tbody tr').length).toBe(51);
    expect(mount.querySelector<HTMLElement>('.table-scroll')!.scrollTop).toBe(
      120,
    );
  });
});
