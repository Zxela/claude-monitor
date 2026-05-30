import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { init } from './hash';
import { state, update } from './state';

describe('hash routing', () => {
  beforeEach(() => {
    // state.ts is a module singleton — reset the keys hash.ts cares about.
    update({ selectedSessionId: null, view: 'list' });
    location.hash = '';
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('restores selectedSessionId and view from a valid hash via hashchange', () => {
    init();
    location.hash = '#session=abc123&view=graph';
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    expect(state.selectedSessionId).toBe('abc123');
    expect(state.view).toBe('graph');
  });

  it('rejects an invalid view from the hash (allow-list)', () => {
    update({ view: 'list' });
    init();
    location.hash = '#view=bogus';
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    // Invalid view is dropped; state.view stays at its prior value.
    expect(state.view).toBe('list');
  });

  it('writes the hash after the debounce when state.view changes', () => {
    vi.useFakeTimers();
    init();
    update({ view: 'graph' });
    // Debounced 200ms — nothing written yet.
    expect(location.hash).toBe('');
    vi.advanceTimersByTime(200);
    expect(location.hash).toBe('#view=graph');
  });
});
