import { describe, it, expect } from 'vitest';
import {
  clampSpeed,
  stepDelayFor,
  renderUpToCount,
  MIN_STEP_DELAY,
  MAX_STEP_DELAY,
} from './replay';

// These guard the pure pacing logic of the replay scrubber fix (Phase 3 nit c):
// the clamp bounds, divide-by-speed, fallback branches, and the inclusive
// "seek fully right shows the LAST event" count semantics. A future refactor
// that re-introduces the off-by-one or breaks the clamp must fail here.

describe('replay clampSpeed', () => {
  it('passes through valid positive speeds', () => {
    expect(clampSpeed('2')).toBe(2);
    expect(clampSpeed(0.5)).toBe(0.5);
    expect(clampSpeed('4')).toBe(4);
  });

  it('falls back to 1 for non-positive / non-finite / null', () => {
    expect(clampSpeed('0')).toBe(1);
    expect(clampSpeed(-3)).toBe(1);
    expect(clampSpeed(null)).toBe(1);
    expect(clampSpeed(undefined)).toBe(1);
    expect(clampSpeed('nonsense')).toBe(1);
  });
});

describe('replay stepDelayFor', () => {
  // index/len picked so we are on the "interior" branch (0 < index < len).
  const idx = 1;
  const len = 3;

  it('clamps a huge gap down to MAX_STEP_DELAY', () => {
    // 30-minute idle gap must not freeze the replay.
    const thirtyMinutes = 30 * 60 * 1000;
    expect(stepDelayFor(0, thirtyMinutes, 1, idx, len)).toBe(MAX_STEP_DELAY);
    expect(MAX_STEP_DELAY).toBe(2000);
  });

  it('clamps a tiny gap up to MIN_STEP_DELAY', () => {
    expect(stepDelayFor(0, 1, 1, idx, len)).toBe(MIN_STEP_DELAY);
    expect(MIN_STEP_DELAY).toBe(30);
  });

  it('divides the real inter-event gap by speed', () => {
    // gap=400ms, speed=2 -> 200ms (inside [30, 2000], so not clamped).
    expect(stepDelayFor(0, 400, 2, idx, len)).toBe(200);
    // gap=400ms, speed=0.5 -> 800ms.
    expect(stepDelayFor(0, 400, 0.5, idx, len)).toBe(800);
  });

  it('falls back to max(50, 200/speed) at the boundaries and for bad gaps', () => {
    // index <= 0 (first event has no predecessor)
    expect(stepDelayFor(NaN, 0, 1, 0, len)).toBe(200);
    // index >= len (past the end)
    expect(stepDelayFor(0, 0, 1, len, len)).toBe(200);
    // non-positive gap (events out of order / identical timestamps)
    expect(stepDelayFor(500, 500, 1, idx, len)).toBe(200);
    expect(stepDelayFor(500, 100, 1, idx, len)).toBe(200);
    // NaN gap (unparseable timestamp)
    expect(stepDelayFor(NaN, 100, 1, idx, len)).toBe(200);
    // fallback honours speed: 200/4 = 50 (the floor)
    expect(stepDelayFor(NaN, 0, 4, 0, len)).toBe(50);
    // fallback floor at 50: 200/8 would be 25, clamped up to 50
    expect(stepDelayFor(NaN, 0, 8, 0, len)).toBe(50);
  });
});

describe('replay renderUpToCount (off-by-one / inclusive-last guard)', () => {
  it('is inclusive of the last event when seeking fully right', () => {
    // The whole point of the fix: renderUpTo(len) must show all len events,
    // i.e. the count equals len (indices 0..len-1 rendered).
    expect(renderUpToCount(5, 5)).toBe(5);
    expect(renderUpToCount(1, 1)).toBe(1);
  });

  it('returns 0 (placeholder) at the far-left position', () => {
    expect(renderUpToCount(0, 5)).toBe(0);
  });

  it('clamps over-large and negative requests into [0, len]', () => {
    expect(renderUpToCount(99, 5)).toBe(5);
    expect(renderUpToCount(-1, 5)).toBe(0);
    expect(renderUpToCount(3, 5)).toBe(3);
  });

  it('handles a non-finite request and an empty manifest', () => {
    expect(renderUpToCount(NaN, 5)).toBe(0);
    expect(renderUpToCount(3, 0)).toBe(0);
  });
});
