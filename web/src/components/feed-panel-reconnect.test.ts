import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { Event, Session, WsEvent } from '../types';

const ev = (contentPreview: string, timestamp: string): Event =>
  ({
    id: 1,
    sessionId: 's1',
    role: 'user',
    type: 'user',
    contentPreview,
    timestamp,
  }) as Event;

const hello = ev('hello', '2026-01-01T00:00:00Z');
const fetchSessionEvents = vi.fn(async (_id: string, _last?: number) => [
  hello,
]);
let wsHandler: ((e: WsEvent) => void) | null = null;
vi.mock('../api', () => ({
  fetchSessionEvents,
  fetchPinnedEvents: vi.fn(async () => []),
}));
vi.mock('../ws', () => ({
  onMessage: (h: (e: WsEvent) => void) => {
    wsHandler = h;
  },
}));

const tick = () => new Promise((r) => setTimeout(r, 10));

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

async function setup() {
  vi.resetModules();
  fetchSessionEvents.mockClear();
  const { state, update } = await import('../state');
  const { render } = await import('./feed-panel');
  const mount = document.createElement('div');
  document.body.appendChild(mount);
  state.sessions.set('s1', { id: 's1', isActive: true } as Session);
  render(mount);
  return { state, update, mount };
}

const entries = (mount: HTMLElement) =>
  Array.from(mount.querySelectorAll('.feed-entry'));

describe('feed panel reconnect', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('reloads the selected session after the socket reconnects', async () => {
    const { update, mount } = await setup();
    update({ connected: true });
    update({ selectedSessionId: 's1' });
    await tick();
    const calls = fetchSessionEvents.mock.calls.length;

    update({ connected: false });
    update({ connected: true });
    await tick();
    expect(fetchSessionEvents.mock.calls.length).toBe(calls + 1);
    expect(entries(mount).length).toBe(1);
  });

  it('backfills sessions that became active during the outage', async () => {
    const { state, update } = await setup();
    update({ connected: true });
    update({ grouped: {} as never });
    await tick();

    update({ connected: false });
    update({ connected: true });
    state.sessions.set('s2', { id: 's2', isActive: true } as Session);
    fetchSessionEvents.mockClear();
    update({ grouped: {} as never });
    await tick();
    expect(fetchSessionEvents.mock.calls.map((c) => c[0]).sort()).toEqual([
      's1',
      's2',
    ]);
  });

  it('discards an older load for the same session', async () => {
    const { update, mount } = await setup();
    const first = deferred<Event[]>();
    fetchSessionEvents.mockImplementationOnce(() => first.promise);
    update({ connected: true });
    update({ selectedSessionId: 's1' });

    update({ connected: false });
    update({ connected: true });
    await tick();
    first.resolve([hello]);
    await tick();
    expect(entries(mount).length).toBe(1);
  });

  it('shows a live event once when the backfill also returns it', async () => {
    const { state, update, mount } = await setup();
    update({ connected: true });
    update({ selectedSessionId: 's1' });
    await tick();

    const later = ev('later', '2026-01-01T00:00:05Z');
    const pending = deferred<Event[]>();
    fetchSessionEvents.mockImplementationOnce(() => pending.promise);
    update({ connected: false });
    update({ connected: true });
    wsHandler!({
      event: 'event',
      session: state.sessions.get('s1')!,
      data: later,
    });
    pending.resolve([hello, later]);
    await tick();
    expect(entries(mount).map((e) => e.textContent?.includes('later'))).toEqual(
      [false, true],
    );
  });
});
