import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { Session } from './types';

async function load() {
  vi.resetModules();
  return import('./state');
}

const session = (id: string): Session => ({ id }) as Session;

describe('state', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('notifies listeners with only the keys whose values changed', async () => {
    const { subscribe, update } = await load();
    const listener = vi.fn();
    subscribe(listener);

    update({ view: 'list', repoFilter: '/repo', searchQuery: 'x' });

    expect(listener).toHaveBeenCalledTimes(1);
    const [st, changed] = listener.mock.calls[0];
    expect([...changed].sort()).toEqual(['repoFilter', 'searchQuery']);
    expect(st.repoFilter).toBe('/repo');
  });

  it('does not notify when nothing changed', async () => {
    const { subscribe, update, state } = await load();
    const listener = vi.fn();
    subscribe(listener);

    update({ view: state.view, connected: state.connected });
    update({});

    expect(listener).not.toHaveBeenCalled();
  });

  it('stops notifying after unsubscribe, and a second unsubscribe is harmless', async () => {
    const { subscribe, update } = await load();
    const a = vi.fn();
    const b = vi.fn();
    const unsubA = subscribe(a);
    subscribe(b);

    unsubA();
    unsubA();
    update({ searchQuery: 'q' });

    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalledTimes(1);
  });

  it('still notifies later listeners when an earlier one unsubscribes mid-dispatch', async () => {
    const { subscribe, update } = await load();
    const second = vi.fn();
    const unsubFirst = subscribe(() => unsubFirst());
    subscribe(second);

    update({ searchQuery: 'q' });

    expect(second).toHaveBeenCalledTimes(1);
  });

  it('updateSession stores the session and notifies with "sessions"', async () => {
    const { subscribe, updateSession, state } = await load();
    const listener = vi.fn();
    subscribe(listener);

    updateSession(session('s1'));
    updateSession(session('s1'));

    expect(state.sessions.get('s1')?.id).toBe('s1');
    expect(listener).toHaveBeenCalledTimes(2);
    expect([...listener.mock.calls[0][1]]).toEqual(['sessions']);
  });

  it('does not notify when update() is given the same sessions map', async () => {
    const { subscribe, update, state } = await load();
    const listener = vi.fn();
    subscribe(listener);
    state.sessions.set('s1', session('s1'));

    update({ sessions: state.sessions });

    expect(listener).not.toHaveBeenCalled();
  });

  it('reads the initial stats window from localStorage, defaulting to today', async () => {
    expect((await load()).state.statsWindow).toBe('today');

    localStorage.setItem('claude-monitor-stats-window', 'week');
    expect((await load()).state.statsWindow).toBe('week');
  });
});
