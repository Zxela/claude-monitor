import { describe, it, expect, vi, afterEach } from 'vitest';
import { render } from './graph-view';
import { state, update } from '../state';
import type { Session } from '../types';

const waiting: Session = {
  id: 's1',
  totalCost: 0,
  inputTokens: 0,
  outputTokens: 0,
  cacheReadTokens: 0,
  cacheCreationTokens: 0,
  messageCount: 0,
  eventCount: 0,
  lastActive: new Date().toISOString(),
  isActive: true,
  startedAt: new Date().toISOString(),
  status: 'waiting',
  needsAttention: true,
  costRate: 0,
  errorCount: 0,
  taskDescription: '',
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('graph view animation', () => {
  it('stops the frame loop for an attention node under reduced motion', () => {
    let pending: FrameRequestCallback | null = null;
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      pending = cb;
      return 1;
    });
    vi.stubGlobal('cancelAnimationFrame', () => {
      pending = null;
    });
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)',
    }));
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null);

    state.sessions.set(waiting.id, waiting);
    render(document.createElement('div'));
    update({ view: 'graph' });

    let frames = 0;
    while (pending && frames < 500) {
      const cb: FrameRequestCallback = pending;
      pending = null;
      cb(frames * 16);
      frames++;
    }
    expect(pending).toBeNull();
  });

  it('repaints a settled graph after a window resize', () => {
    let pending: FrameRequestCallback | null = null;
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      pending = cb;
      return 1;
    });
    vi.stubGlobal('cancelAnimationFrame', () => {
      pending = null;
    });
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)',
    }));
    const calls: Record<string, ReturnType<typeof vi.fn>> = {};
    const ctx = new Proxy({} as Record<string | symbol, unknown>, {
      get: (t, p) => {
        if (!(p in t)) t[p] = calls[String(p)] = vi.fn();
        return t[p];
      },
    });
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(
      ctx as unknown as CanvasRenderingContext2D,
    );

    update({ view: 'list' });
    state.sessions.set(waiting.id, waiting);
    render(document.createElement('div'));
    update({ view: 'graph' });

    let frames = 0;
    while (pending && frames < 500) {
      const cb: FrameRequestCallback = pending;
      pending = null;
      cb(frames * 16);
      frames++;
    }
    expect(pending).toBeNull();

    calls.arc.mockClear();
    window.dispatchEvent(new Event('resize'));
    expect(calls.arc).toHaveBeenCalled();
  });
});
