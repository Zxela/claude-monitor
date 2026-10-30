import { describe, it, expect, vi } from 'vitest';
import type { Session } from '../types';

const fetchSessionEvents = vi.fn(async () => [
  {
    id: 1,
    sessionId: 's1',
    role: 'user',
    type: 'user',
    contentPreview: 'hello',
    timestamp: '2026-01-01T00:00:00Z',
  },
]);
vi.mock('../api', () => ({
  fetchSessionEvents,
  fetchPinnedEvents: vi.fn(async () => []),
}));
vi.mock('../ws', () => ({ onMessage: () => {} }));

const tick = () => new Promise((r) => setTimeout(r, 10));

describe('feed panel reconnect', () => {
  it('reloads the selected session after the socket reconnects', async () => {
    const { state, update } = await import('../state');
    const { render } = await import('./feed-panel');
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    state.sessions.set('s1', { id: 's1', isActive: true } as Session);
    render(mount);

    update({ connected: true });
    update({ selectedSessionId: 's1' });
    await tick();
    const calls = fetchSessionEvents.mock.calls.length;

    update({ connected: false });
    update({ connected: true });
    await tick();
    expect(fetchSessionEvents.mock.calls.length).toBe(calls + 1);
    expect(mount.querySelectorAll('.feed-entry').length).toBe(1);
  });
});
