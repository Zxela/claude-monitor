import { describe, it, expect } from 'vitest';
import { renderPrLinks } from './pr-links';
import { sessionDisplayName } from '../utils';

describe('renderPrLinks', () => {
  it('renders each link as an anchor that opens in a new tab', () => {
    const el = renderPrLinks([
      {
        number: 14793,
        url: 'https://git.example.com/group/repo/-/merge_requests/14793',
        repository: 'group/repo',
      },
      { number: 12, url: 'https://github.com/o/r/pull/12' },
    ]);
    const anchors = el?.querySelectorAll('a') ?? [];
    expect(anchors).toHaveLength(2);
    expect(anchors[0].getAttribute('href')).toBe(
      'https://git.example.com/group/repo/-/merge_requests/14793',
    );
    expect(anchors[0].textContent).toBe('PR #14793');
    expect(anchors[0].title).toBe('group/repo #14793');
    expect(anchors[0].target).toBe('_blank');
    expect(anchors[0].rel).toBe('noopener noreferrer');
  });

  it('keeps markup in fields as text', () => {
    const el = renderPrLinks([
      {
        number: 1,
        url: 'https://example.com/"><img src=x onerror=alert(1)>',
        repository: '<b>evil</b>',
      },
    ]);
    expect(el?.querySelector('img')).toBeNull();
    expect(el?.querySelector('b')).toBeNull();
    expect(el?.querySelector('a')?.title).toBe('<b>evil</b> #1');
  });

  it('drops non-http URLs', () => {
    expect(
      renderPrLinks([{ number: 1, url: 'javascript:alert(1)' }]),
    ).toBeNull();
    expect(renderPrLinks(undefined)).toBeNull();
  });
});

describe('sessionDisplayName', () => {
  it('prefers an explicit name over the AI title, and the AI title over cwd', () => {
    const base = { id: 'abcdef123456', cwd: '/home/me/project' };
    expect(
      sessionDisplayName({ ...base, sessionName: 'mine', aiTitle: 'ai' }),
    ).toBe('mine');
    expect(sessionDisplayName({ ...base, aiTitle: 'ai' })).toBe('ai');
    expect(sessionDisplayName(base)).toBe('project');
  });
});
