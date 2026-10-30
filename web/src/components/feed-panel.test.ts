import { describe, it, expect, vi } from 'vitest';
import type { Session } from '../types';

vi.mock('../api', () => ({
  fetchSessionEvents: vi.fn(async () => [
    {
      id: 1,
      sessionId: 's1',
      role: 'user',
      type: 'user',
      contentPreview: 'hello world',
      timestamp: '2026-01-01T00:00:00Z',
    },
  ]),
  fetchPinnedEvents: vi.fn(async () => []),
}));
vi.mock('../ws', () => ({ onMessage: () => {} }));

const tick = () => new Promise((r) => setTimeout(r, 10));

describe('feed panel', () => {
  it('reloads session history after a view round-trip', async () => {
    const { state, update } = await import('../state');
    const { render } = await import('./feed-panel');
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    state.sessions.set('s1', { id: 's1', isActive: false } as Session);
    render(mount);

    update({ selectedSessionId: 's1' });
    await tick();
    expect(mount.querySelectorAll('.feed-entry').length).toBe(1);

    update({ view: 'timeline' });
    mount.innerHTML = '';
    update({ view: 'list' });
    await tick();
    expect(mount.querySelectorAll('.feed-entry').length).toBe(1);
  });
});
