import { describe, it, expect } from 'vitest';
import { needsAttention } from './attention';
import type { Session } from './types';

const session = (over: Partial<Session>): Session =>
  ({ id: 's', status: 'idle', needsAttention: false, errorCount: 0, ...over }) as Session;

describe('needsAttention', () => {
  it('follows the server flag rather than the status string', () => {
    expect(needsAttention(session({ needsAttention: true, status: 'waiting' }))).toBe(true);
    expect(needsAttention(session({ status: 'waiting' }))).toBe(false);
  });

  it('flags errors the viewer has not seen', () => {
    expect(needsAttention(session({ errorCount: 1 }))).toBe(true);
  });
});
