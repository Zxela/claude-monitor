import { describe, it, expect, beforeAll } from 'vitest';
import { render } from './table-view';
import { state, update } from '../state';
import type { Session } from '../types';

const session = (id: string, totalCost: number) =>
  ({
    id,
    totalCost,
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

const grouped = (sessions: Session[]) => ({
  active: [],
  lastHour: sessions,
  today: [],
  yesterday: [],
  thisWeek: [],
  older: [],
});

const key = (el: HTMLElement, k: string) => {
  const e = new KeyboardEvent('keydown', {
    key: k,
    bubbles: true,
    cancelable: true,
  });
  el.dispatchEvent(e);
  return e;
};

describe('table view keyboard', () => {
  let mount: HTMLElement;
  const th = (k: string) =>
    mount.querySelector<HTMLElement>(`th[data-sort="${k}"]`)!;
  const row = (id: string) =>
    mount.querySelector<HTMLElement>(`tr[data-session-id="${id}"]`)!;

  beforeAll(() => {
    mount = document.createElement('div');
    document.body.appendChild(mount);
    update({
      view: 'table',
      grouped: grouped([session('a', 1), session('b', 2)]),
    });
    render(mount);
  });

  it('sorts from a focused header with Enter and Space', () => {
    expect(th('cost').getAttribute('tabindex')).toBe('0');
    expect(th('cost').getAttribute('aria-sort')).toBe('none');
    expect(th('lastActive').getAttribute('aria-sort')).toBe('descending');

    th('cost').focus();
    expect(key(th('cost'), 'Enter').defaultPrevented).toBe(true);
    expect(th('cost').getAttribute('aria-sort')).toBe('descending');
    expect(document.activeElement).toBe(th('cost'));

    key(th('cost'), ' ');
    expect(th('cost').getAttribute('aria-sort')).toBe('ascending');
    expect(document.activeElement).toBe(th('cost'));
  });

  it('keeps a focused row focused across a re-render', () => {
    row('b').focus();
    expect(document.activeElement).toBe(row('b'));
    update({ grouped: grouped([session('a', 1), session('b', 3)]) });
    expect(document.activeElement).toBe(row('b'));
  });

  it('opens the focused row with Enter', () => {
    row('a').focus();
    key(row('a'), 'Enter');
    expect(state.selectedSessionId).toBe('a');
    expect(state.view).toBe('list');
  });
});
