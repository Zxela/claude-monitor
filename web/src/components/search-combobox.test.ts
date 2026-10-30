import { describe, it, expect, vi, beforeAll } from 'vitest';
import type { Event } from '../types';

vi.mock('../api', () => ({
  fetchSearchCombined: vi.fn(() => new Promise(() => {})),
}));

import { render } from './search';
import { state, update } from '../state';

const result = (id: number, sessionId: string) =>
  ({
    id,
    sessionId,
    type: 'message',
    role: 'user',
    contentPreview: `match ${id}`,
    costUSD: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    isError: false,
    timestamp: '2024-01-01T00:00:00Z',
  }) as Event;

const key = (el: HTMLElement, k: string) => {
  const e = new KeyboardEvent('keydown', {
    key: k,
    bubbles: true,
    cancelable: true,
  });
  el.dispatchEvent(e);
  return e;
};

describe('search combobox', () => {
  let input: HTMLInputElement;

  beforeAll(() => {
    Element.prototype.scrollIntoView = () => {};
    const box = document.createElement('div');
    box.innerHTML = '<input type="text" data-search />';
    document.body.appendChild(box);
    input = box.querySelector('input')!;
    render(box);
  });

  it('exposes combobox and listbox roles', () => {
    expect(input.getAttribute('role')).toBe('combobox');
    const list = document.getElementById(input.getAttribute('aria-controls')!);
    expect(list?.getAttribute('role')).toBe('listbox');
    expect(input.getAttribute('aria-expanded')).toBe('false');
  });

  it('moves through results with arrows and opens one with Enter', () => {
    input.value = 'match';
    update({ searchQuery: 'match', searchOpen: true });
    update({
      searchResults: [result(1, 'a'), result(2, 'b')],
      searchLoading: false,
    });
    expect(input.getAttribute('aria-expanded')).toBe('true');

    const opts = document.querySelectorAll<HTMLElement>('[role="option"]');
    expect(opts).toHaveLength(2);

    expect(key(input, 'ArrowDown').defaultPrevented).toBe(true);
    expect(input.getAttribute('aria-activedescendant')).toBe(opts[0].id);
    key(input, 'ArrowDown');
    expect(input.getAttribute('aria-activedescendant')).toBe(opts[1].id);
    expect(opts[1].getAttribute('aria-selected')).toBe('true');
    expect(opts[0].getAttribute('aria-selected')).toBe('false');
    key(input, 'ArrowUp');
    expect(input.getAttribute('aria-activedescendant')).toBe(opts[0].id);

    key(input, 'Enter');
    expect(state.selectedSessionId).toBe('a');
    expect(state.searchHighlightEventId).toBe(1);
    expect(input.getAttribute('aria-expanded')).toBe('false');
    expect(input.hasAttribute('aria-activedescendant')).toBe(false);
  });
});
