import { describe, it, expect } from 'vitest';
import {
  sessionDisplayName,
  effectiveInputTokens,
  formatTokens,
  formatDurationSecs,
} from './utils';

describe('sessionDisplayName', () => {
  it('prefers sessionName over everything', () => {
    expect(
      sessionDisplayName({ sessionName: 'My Session', cwd: '/home/user/repo', id: 'abcdef123456' }),
    ).toBe('My Session');
  });

  it('falls back to cwd basename when no sessionName', () => {
    expect(sessionDisplayName({ cwd: '/home/user/my-repo', id: 'abcdef123456' })).toBe('my-repo');
  });

  it('strips trailing slashes from cwd before taking basename', () => {
    expect(sessionDisplayName({ cwd: '/home/user/my-repo/', id: 'abcdef123456' })).toBe('my-repo');
  });

  it('falls back to first 8 chars of id when no sessionName/cwd', () => {
    expect(sessionDisplayName({ id: 'abcdef1234567890' })).toBe('abcdef12');
  });
});

describe('effectiveInputTokens', () => {
  it('sums input + cacheRead + cacheCreation', () => {
    expect(
      effectiveInputTokens({ inputTokens: 10, cacheReadTokens: 100, cacheCreationTokens: 5 }),
    ).toBe(115);
  });
});

describe('formatTokens', () => {
  it('renders small counts verbatim', () => {
    expect(formatTokens(999)).toBe('999');
  });
  it('renders thousands with k suffix', () => {
    expect(formatTokens(1500)).toBe('1.5k');
  });
  it('renders millions with M suffix', () => {
    expect(formatTokens(1_500_000)).toBe('1.5M');
  });
});

describe('formatDurationSecs', () => {
  it('renders sub-minute as seconds', () => {
    expect(formatDurationSecs(59)).toBe('59s');
  });
  it('renders 90s as 1m', () => {
    expect(formatDurationSecs(90)).toBe('1m');
  });
  it('renders 3700s as 1h1m', () => {
    expect(formatDurationSecs(3700)).toBe('1h1m');
  });
});
