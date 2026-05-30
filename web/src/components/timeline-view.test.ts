import { describe, it, expect } from 'vitest';
import {
  buildSpansFrom,
  pickEventAt,
  GAP_THRESHOLD,
  type TimelineEvent,
  type TimelineGeometry,
} from './timeline-view';

// Guards the timeline hover hit-test fix (Phase 3 nit b): draw() and the hover
// hit-test must share IDENTICAL span geometry. buildSpansFrom() is the single
// source of truth for both, and pickEventAt() applies the exact x/barW/y/barH
// math draw() uses. These tests assert the merge/split rules and that a cursor
// over a drawn bar resolves to that span (and the nearest-timestamp event).

const T0 = Date.parse('2024-01-01T00:00:00.000Z');

function ev(over: Partial<TimelineEvent> & { tsMs: number }): TimelineEvent {
  const { tsMs, ...rest } = over;
  return {
    index: 0,
    timestamp: new Date(tsMs).toISOString(),
    type: 'message',
    role: 'assistant',
    contentPreview: '',
    costUSD: 0,
    ...rest,
  };
}

describe('timeline buildSpansFrom (merge/split geometry)', () => {
  it('merges consecutive same-lane events within GAP_THRESHOLD into one span', () => {
    // Two assistant (lane 1) events 1s apart — under the 2s threshold.
    const events = [ev({ tsMs: T0, role: 'assistant' }), ev({ tsMs: T0 + 1000, role: 'assistant' })];
    const spans = buildSpansFrom(events);
    expect(spans).toHaveLength(1);
    expect(spans[0].lane).toBe(1);
    expect(spans[0].events).toHaveLength(2);
  });

  it('splits same-lane events separated by more than GAP_THRESHOLD', () => {
    // Gap of GAP_THRESHOLD + 1ms between the END of span 1 and the next event.
    // First event (same-lane next) ends at ts + min(gap, 500) = T0 + 500.
    // Second event at T0 + 500 + GAP_THRESHOLD + 1 -> ts - cur.end > threshold.
    const events = [
      ev({ tsMs: T0, role: 'assistant' }),
      ev({ tsMs: T0 + 500 + GAP_THRESHOLD + 1, role: 'assistant' }),
    ];
    const spans = buildSpansFrom(events);
    expect(spans).toHaveLength(2);
    expect(spans[0].events).toHaveLength(1);
    expect(spans[1].events).toHaveLength(1);
  });

  it('puts different lanes in separate spans', () => {
    // user -> lane 0, assistant -> lane 1, tool -> lane 2.
    const events = [
      ev({ tsMs: T0, role: 'user' }),
      ev({ tsMs: T0 + 100, role: 'assistant' }),
      ev({ tsMs: T0 + 200, role: 'tool', type: 'tool_result' }),
    ];
    const spans = buildSpansFrom(events);
    expect(spans).toHaveLength(3);
    expect(spans.map((s) => s.lane)).toEqual([0, 1, 2]);
  });
});

describe('timeline pickEventAt (hover hit-test)', () => {
  // A simple frame: x-origin at t0, 1px per ms, no pan, lanes 100px tall.
  const geom: TimelineGeometry = {
    t0: T0,
    pixelsPerMs: 1,
    offsetX: 0,
    laneH: 100,
    topY: 30,
  };

  it('resolves a cursor inside a drawn bar to that span', () => {
    // One assistant span (lane 1) spanning T0..T0+500 -> x in [0, 500].
    const events = [ev({ tsMs: T0, role: 'assistant' }), ev({ tsMs: T0 + 800, role: 'user' })];
    const spans = buildSpansFrom(events);
    // lane 1 bar: y = 30 + 1*100 + 4 = 134, height = 100 - 8 = 92 -> [134, 226].
    const hit = pickEventAt(spans, geom, 50, 150);
    expect(hit).not.toBeNull();
    expect(hit?.role).toBe('assistant');
  });

  it('returns null when the cursor is outside every bar', () => {
    const events = [ev({ tsMs: T0, role: 'assistant' }), ev({ tsMs: T0 + 800, role: 'user' })];
    const spans = buildSpansFrom(events);
    // y=10 is above topY (30); no lane there.
    expect(pickEventAt(spans, geom, 50, 10)).toBeNull();
    // x far to the right of any bar.
    expect(pickEventAt(spans, geom, 99999, 150)).toBeNull();
  });

  it('returns the event whose timestamp is nearest the cursor within the span', () => {
    // Two assistant events merged into one span; cursor near the second event.
    const events = [
      ev({ tsMs: T0, role: 'assistant', contentPreview: 'first' }),
      ev({ tsMs: T0 + 400, role: 'assistant', contentPreview: 'second' }),
    ];
    const spans = buildSpansFrom(events);
    expect(spans).toHaveLength(1);
    // Cursor at x=390 (lane 1, y=150) is nearest the second event (x=400).
    const near2 = pickEventAt(spans, geom, 390, 150);
    expect(near2?.contentPreview).toBe('second');
    // Cursor at x=10 is nearest the first event (x=0).
    const near1 = pickEventAt(spans, geom, 10, 150);
    expect(near1?.contentPreview).toBe('first');
  });
});
