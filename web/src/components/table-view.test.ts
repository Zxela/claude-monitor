import { describe, it, expect } from 'vitest';
import { render } from './table-view';
import { update } from '../state';
import type { Session } from '../types';

describe('table view', () => {
  it('renders a hostile session name as text, not markup', () => {
    const payload = '<img src=x onerror="window.__pwned=1">';
    const s = {
      id: 'abc"><b>id</b>',
      sessionName: payload,
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
    } as Session;
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    update({
      view: 'table',
      grouped: {
        active: [],
        lastHour: [s],
        today: [],
        yesterday: [],
        thisWeek: [],
        older: [],
      },
    });
    render(mount);

    expect(mount.querySelector('img')).toBeNull();
    expect(mount.querySelector('tbody b')).toBeNull();
    const cell = mount.querySelector('tbody td');
    expect(cell?.textContent).toBe(payload);
    expect(cell?.getAttribute('title')).toBe(payload);
    expect(
      mount.querySelector('tbody tr')?.getAttribute('data-session-id'),
    ).toBe(s.id);
  });
});
