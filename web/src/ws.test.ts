import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { GroupedSessions, Session, WsEvent } from './types';

const { fetchGroupedSessions } = vi.hoisted(() => ({
  fetchGroupedSessions: vi.fn(),
}));
vi.mock('./api', () => ({ fetchGroupedSessions }));

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  close = vi.fn(() => this.onclose?.());
  constructor(public url: string) {
    MockWebSocket.instances.push(this);
  }
  send(msg: unknown) {
    this.onmessage?.({
      data: typeof msg === 'string' ? msg : JSON.stringify(msg),
    });
  }
}

const sockets = MockWebSocket.instances;
const latest = () => sockets[sockets.length - 1];
const session = (id: string): Session => ({ id }) as Session;
const emptyGroups = (): GroupedSessions => ({
  active: [],
  lastHour: [],
  today: [],
  yesterday: [],
  thisWeek: [],
  older: [],
});

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

async function load() {
  vi.resetModules();
  const st = await import('./state');
  const ws = await import('./ws');
  return { ...st, ...ws };
}

beforeEach(() => {
  vi.useFakeTimers();
  sockets.length = 0;
  vi.stubGlobal('WebSocket', MockWebSocket);
  fetchGroupedSessions.mockReset();
  fetchGroupedSessions.mockResolvedValue(emptyGroups());
  document.body.innerHTML = '<div id="app"></div>';
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('ws connect', () => {
  it('opens a socket on /ws for the current host', async () => {
    const { connect } = await load();
    connect();
    expect(sockets).toHaveLength(1);
    expect(latest().url).toBe(`ws://${location.host}/ws`);
  });

  it('marks connected and reconciles every session group on open', async () => {
    const groups = emptyGroups();
    groups.active = [session('a')];
    groups.today = [session('t')];
    groups.older = [session('o')];
    fetchGroupedSessions.mockResolvedValue(groups);
    const { connect, state } = await load();

    connect();
    latest().onopen?.();
    expect(state.connected).toBe(true);
    await flush();

    expect([...state.sessions.keys()].sort()).toEqual(['a', 'o', 't']);
    expect(state.grouped).toBe(groups);
  });

  it('stays connected when the reconcile fetch fails', async () => {
    fetchGroupedSessions.mockRejectedValue(new Error('down'));
    const { connect, state } = await load();

    connect();
    latest().onopen?.();
    await flush();

    expect(state.connected).toBe(true);
    expect(state.grouped).toBeNull();
  });
});

describe('ws reconnect', () => {
  it('marks disconnected on close and reconnects after 2s', async () => {
    const { connect, state } = await load();
    connect();
    latest().onopen?.();

    latest().onclose?.();
    expect(state.connected).toBe(false);

    vi.advanceTimersByTime(1999);
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(2);
  });

  it('doubles the delay on consecutive failures and caps it at 30s', async () => {
    const { connect } = await load();
    connect();

    const delays: number[] = [];
    for (let i = 0; i < 6; i++) {
      const before = sockets.length;
      latest().onclose?.();
      let waited = 0;
      while (sockets.length === before && waited < 60_000) {
        vi.advanceTimersByTime(1000);
        waited += 1000;
      }
      delays.push(waited);
    }

    expect(delays).toEqual([2000, 4000, 8000, 16000, 30000, 30000]);
  });

  it('resets the backoff after a successful open', async () => {
    const { connect } = await load();
    connect();
    latest().onclose?.();
    vi.advanceTimersByTime(2000);
    latest().onclose?.();
    vi.advanceTimersByTime(4000);
    expect(sockets).toHaveLength(3);

    latest().onopen?.();
    latest().onclose?.();
    vi.advanceTimersByTime(2000);
    expect(sockets).toHaveLength(4);
  });

  it('closes the socket on error so the reconnect path runs', async () => {
    const { connect, state } = await load();
    connect();
    latest().onopen?.();

    latest().onerror?.();
    expect(sockets[0].close).toHaveBeenCalled();
    expect(state.connected).toBe(false);

    vi.advanceTimersByTime(2000);
    expect(sockets).toHaveLength(2);
  });
});

describe('ws message dispatch', () => {
  it('counts events, stores the session and calls every handler', async () => {
    const { connect, onMessage, state } = await load();
    const h1 = vi.fn();
    const h2 = vi.fn();
    onMessage(h1);
    onMessage(h2);
    connect();

    const msg: WsEvent = { event: 'session_update', session: session('s1') };
    latest().send(msg);

    expect(state.eventCount).toBe(1);
    expect(state.sessions.get('s1')?.id).toBe('s1');
    expect(h1).toHaveBeenCalledWith(msg);
    expect(h2).toHaveBeenCalledWith(msg);
  });

  it('drops unparseable messages with a warning', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { connect, onMessage, state } = await load();
    const handler = vi.fn();
    onMessage(handler);
    connect();

    latest().send('{not json');

    expect(warn).toHaveBeenCalled();
    expect(handler).not.toHaveBeenCalled();
    expect(state.eventCount).toBe(0);
    warn.mockRestore();
  });

  it('turns update_available into update state without counting it as an event', async () => {
    const { connect, onMessage, state } = await load();
    const handler = vi.fn();
    onMessage(handler);
    connect();

    latest().send({ event: 'update_available', version: '9.9.9', url: 'u' });

    expect(state.updateVersion).toBe('9.9.9');
    expect(state.updateUrl).toBe('u');
    expect(state.eventCount).toBe(0);
    expect(handler).not.toHaveBeenCalled();
  });
});

describe('ws dropped-events banner', () => {
  const banner = () =>
    document.querySelector<HTMLElement>('#app [role="alert"]');

  it('shows the dropped count without dispatching to handlers', async () => {
    const { connect, onMessage, state } = await load();
    const handler = vi.fn();
    onMessage(handler);
    connect();

    latest().send({ type: 'dropped_events', delta: 3, count: 3 });

    expect(banner()?.style.display).toBe('flex');
    expect(banner()?.textContent).toContain('3 event(s) were dropped');
    expect(handler).not.toHaveBeenCalled();
    expect(state.eventCount).toBe(0);
  });

  it('stays dismissed until more events are dropped', async () => {
    const { connect } = await load();
    connect();
    latest().send({ type: 'dropped_events', delta: 3, count: 3 });

    banner()!.querySelector('button')!.click();
    expect(banner()?.style.display).toBe('none');

    latest().send({ type: 'dropped_events', delta: 0, count: 3 });
    expect(banner()?.style.display).toBe('none');

    latest().send({ type: 'dropped_events', delta: 2, count: 5 });
    expect(banner()?.style.display).toBe('flex');
    expect(banner()?.textContent).toContain('2 event(s) were dropped');
    expect(document.querySelectorAll('#app [role="alert"]')).toHaveLength(1);
  });
});
