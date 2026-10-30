import { describe, it, expect, vi, beforeAll } from 'vitest';
import type { Session } from './types';

vi.mock('./ws', () => ({ connect: vi.fn(), onMessage: vi.fn() }));

import { state, update } from './state';

const session = (id: string, minsAgo: number) =>
  ({
    id,
    totalCost: 1,
    inputTokens: 10,
    outputTokens: 10,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    messageCount: 5,
    eventCount: 5,
    lastActive: new Date(Date.now() - minsAgo * 60_000).toISOString(),
    isActive: false,
    startedAt: new Date(Date.now() - 3600_000).toISOString(),
    status: 'idle',
    costRate: 0,
    errorCount: 0,
    taskDescription: '',
  }) as Session;

const key = (el: Element, k: string) => {
  const e = new KeyboardEvent('keydown', {
    key: k,
    bubbles: true,
    cancelable: true,
  });
  el.dispatchEvent(e);
  return e;
};

const card = (id: string) =>
  document.querySelector<HTMLElement>(
    `#sessions-mount [data-session-id="${id}"]`,
  )!;

describe('global arrow keys', () => {
  beforeAll(async () => {
    globalThis.fetch = vi.fn(() => new Promise<Response>(() => {}));
    HTMLCanvasElement.prototype.getContext = (() =>
      null) as unknown as HTMLCanvasElement['getContext'];
    Element.prototype.scrollIntoView = () => {};
    document.body.innerHTML = `
      <div id="app">
        <header id="topbar-mount"></header>
        <nav id="sessions-mount"></nav>
        <main id="feed-mount"></main>
        <span id="conn-dot"></span><span id="conn-indicator"></span>
        <span id="sb-host"></span><span id="sb-events"></span>
        <span id="sb-version"></span>
      </div>`;
    await import('./main');
    for (const s of [session('a', 1), session('b', 2), session('c', 3)])
      state.sessions.set(s.id, s);
    update({ renderVersion: state.renderVersion + 1 });
  });

  it('leaves arrows alone outside the session list', () => {
    expect(key(document.body, 'ArrowDown').defaultPrevented).toBe(false);
    expect(key(document.body, 'ArrowUp').defaultPrevented).toBe(false);
    const scroller = document.createElement('div');
    document.getElementById('feed-mount')!.appendChild(scroller);
    expect(key(scroller, 'ArrowDown').defaultPrevented).toBe(false);
    expect(state.focusedSessionId).toBeNull();
  });

  it('moves focus between sidebar cards', () => {
    card('a').focus();
    expect(key(card('a'), 'ArrowDown').defaultPrevented).toBe(true);
    expect(state.focusedSessionId).toBe('b');
    expect(document.activeElement).toBe(card('b'));
    key(card('b'), 'ArrowUp');
    expect(document.activeElement).toBe(card('a'));
  });

  it('selects a card once on Enter', () => {
    update({ selectedSessionId: null, focusedSessionId: 'a' });
    card('a').focus();
    key(card('a'), 'Enter');
    expect(state.selectedSessionId).toBe('a');
    expect(document.activeElement).toBe(card('a'));
  });

  it('keeps arrow navigation working after the list rebuilds', () => {
    card('b').focus();
    update({ renderVersion: state.renderVersion + 1 });
    expect(document.activeElement).toBe(card('b'));
    key(card('b'), 'ArrowDown');
    expect(document.activeElement).toBe(card('c'));
  });

  it('skips cards in collapsed time groups', () => {
    const active = { ...session('z', 0), isActive: true };
    state.sessions.set('z', active);
    state.sessions.set('d', session('d', 60 * 24 * 30));
    update({ selectedSessionId: null });
    document
      .querySelector<HTMLElement>('#sessions-mount [data-filter="all"]')!
      .click();
    const header = (label: string) =>
      Array.from(
        document.querySelectorAll<HTMLElement>(
          '#sessions-mount .time-group-header',
        ),
      ).find((h) => h.textContent?.startsWith(label))!;
    header('LAST HOUR').click();
    header('OLDER').click();

    card('z').focus();
    key(card('z'), 'ArrowDown');
    expect(state.focusedSessionId).toBe('d');
    expect(document.activeElement).toBe(card('d'));
    key(card('d'), 'ArrowUp');
    expect(state.focusedSessionId).toBe('z');
  });
});
