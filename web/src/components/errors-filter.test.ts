import { describe, it, expect } from 'vitest';
import { errorsOnlyFilters } from './session-card';
import { detectType } from './render-message';
import type { Event } from '../types';

// Minimal Event factory — only the fields detectType inspects.
function makeEvent(over: Partial<Event>): Event {
  return {
    id: 1,
    sessionId: 's',
    type: 'message',
    role: 'user',
    contentPreview: '',
    costUSD: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    isError: false,
    timestamp: '2024-01-01T00:00:00Z',
    ...over,
  };
}

describe('errors-only feed filter (regression guard for the command leak)', () => {
  // Mirrors feed-panel.ts applyFilters() / appendMessage(): visibility is
  // `filters[type] ?? true`. The bug was that the errors-only map omitted
  // 'command', so command entries defaulted to visible (true) and leaked.
  const isVisible = (filters: Record<string, boolean>, type: string): boolean =>
    filters[type] ?? true;

  it('includes command as an explicit false key', () => {
    expect(errorsOnlyFilters().command).toBe(false);
  });

  it('includes system as an explicit false key (the detectType fallback)', () => {
    expect(errorsOnlyFilters().system).toBe(false);
  });

  it('hides a system-typed entry under errors-only (same leak class as command)', () => {
    const filters = errorsOnlyFilters();
    // detectType()'s fallback is 'system'; applyFilters() also maps entries
    // with no dataset.type to 'system' (`entry.dataset.type || 'system'`).
    expect(isVisible(filters, 'system')).toBe(false);
  });

  it('hides a command-typed entry under errors-only', () => {
    const filters = errorsOnlyFilters();
    const commandEvent = makeEvent({
      role: 'user',
      contentPreview: '<command-name>/clear</command-name>',
    });
    expect(detectType(commandEvent)).toBe('command');
    expect(isVisible(filters, 'command')).toBe(false);
  });

  it('shows an error-typed entry under errors-only', () => {
    const filters = errorsOnlyFilters();
    const errorEvent = makeEvent({ isError: true, contentPreview: 'boom' });
    expect(detectType(errorEvent)).toBe('error');
    expect(isVisible(filters, 'error')).toBe(true);
  });

  it('hides all non-error standard types', () => {
    const filters = errorsOnlyFilters();
    for (const t of [
      'user',
      'assistant',
      'tool_use',
      'tool_result',
      'agent',
      'hook',
      'command',
      'system',
    ]) {
      expect(isVisible(filters, t)).toBe(false);
    }
  });
});
