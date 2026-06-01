import { describe, it, expect } from 'vitest';
import { detectType, renderFeedEntry } from './render-message';
import type { Event } from '../types';

// Minimal Event factory — only the fields detectType/renderFeedEntry inspect.
function makeEvent(over: Partial<Event>): Event {
  return {
    id: 1,
    sessionId: 's',
    type: 'message',
    role: 'assistant',
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

describe('skill invocation rendering', () => {
  it('classifies a Skill tool_use as its own type (not a generic tool_use)', () => {
    const ev = makeEvent({
      role: 'assistant',
      toolName: 'Skill',
      toolDetail: 'commit-commands:commit',
    });
    expect(detectType(ev)).toBe('skill');
  });

  it('does NOT classify ordinary tools as skill', () => {
    expect(detectType(makeEvent({ role: 'assistant', toolName: 'Bash' }))).toBe('tool_use');
    expect(detectType(makeEvent({ role: 'assistant', toolName: 'Read' }))).toBe('tool_use');
  });

  it('falls back to the [skill:] preview when toolName is missing (streaming race)', () => {
    const ev = makeEvent({
      role: 'assistant',
      toolName: '',
      contentPreview: '[skill: triage-issue]',
    });
    expect(detectType(ev)).toBe('skill');
  });

  it('renders the skill name with the distinct skill content class and chip', () => {
    const ev = makeEvent({ role: 'assistant', toolName: 'Skill', toolDetail: 'review-pr' });
    const el = renderFeedEntry(ev);
    expect(el.classList.contains('type-skill')).toBe(true);
    const chip = el.querySelector('.fe-type');
    expect(chip?.textContent).toBe('[skill]');
    const content = el.querySelector('.fe-content');
    expect(content?.classList.contains('skill')).toBe(true);
    expect(content?.textContent).toContain('review-pr');
  });
});
