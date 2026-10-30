import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { Session } from '../types';

const NOW = new Date(2026, 2, 13, 15, 0, 0); // Friday
const MIN = 60_000;
const HOUR = 60 * MIN;
const ago = (ms: number) => new Date(NOW.getTime() - ms).toISOString();

function makeSession(over: Partial<Session> & { id: string }): Session {
  return {
    sessionName: over.id,
    totalCost: 1,
    inputTokens: 10,
    outputTokens: 10,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    messageCount: 5,
    eventCount: 5,
    lastActive: ago(10 * MIN),
    isActive: false,
    startedAt: ago(2 * HOUR),
    status: 'idle',
    costRate: 0,
    errorCount: 0,
    taskDescription: '',
    ...over,
  };
}

async function setup(sessions: Session[]) {
  vi.resetModules();
  const st = await import('../state');
  for (const s of sessions) st.state.sessions.set(s.id, s);
  const { render } = await import('./session-list');
  const mount = document.createElement('div');
  document.body.appendChild(mount);
  render(mount);
  return { ...st, mount };
}

const cardIds = (root: ParentNode) =>
  [...root.querySelectorAll<HTMLElement>('.session-card-compact')].map(
    (c) => c.dataset.sessionId,
  );
const activeIds = (root: ParentNode) =>
  cardIds(
    root.querySelector('.active-section') ?? document.createElement('div'),
  );
const groupLabels = (root: ParentNode) =>
  [...root.querySelectorAll('.time-group-header > span:first-child')].map(
    (s) => s.textContent,
  );
const groupByLabel = (root: ParentNode, label: string) =>
  [...root.querySelectorAll<HTMLElement>('.time-group-header')].find(
    (h) => h.firstElementChild?.textContent === label,
  )?.parentElement ?? null;
const filterBtn = (root: ParentNode, f: string) =>
  root.querySelector<HTMLButtonElement>(`button[data-filter="${f}"]`)!;
const count = (root: ParentNode, f: string) =>
  root.querySelector(`#fc-${f}`)?.textContent;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
  document.body.innerHTML = '';
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('session list grouping', () => {
  const sessions = () => [
    makeSession({ id: 'live', isActive: true, lastActive: ago(5_000) }),
    makeSession({ id: 'hour', lastActive: ago(10 * MIN) }),
    makeSession({ id: 'today', lastActive: ago(3 * HOUR) }),
    makeSession({ id: 'yday', lastActive: ago(20 * HOUR) }),
    makeSession({ id: 'week', lastActive: ago(3 * 24 * HOUR) }),
    makeSession({ id: 'old', lastActive: ago(30 * 24 * HOUR) }),
  ];

  it('shows active sessions and only last-hour/today groups under RECENT', async () => {
    const { mount } = await setup(sessions());

    expect(mount.querySelector('.active-section-header')?.textContent).toBe(
      'ACTIVE NOW (1)',
    );
    expect(activeIds(mount)).toEqual(['live']);
    expect(groupLabels(mount)).toEqual(['LAST HOUR', 'TODAY']);
    expect(cardIds(mount)).toEqual(['live', 'hour', 'today']);
    expect(count(mount, 'active')).toBe('1');
    expect(count(mount, 'recent')).toBe('3');
    expect(count(mount, 'all')).toBe('6');
  });

  it('shows every group under ALL, with the older groups collapsed', async () => {
    const { mount } = await setup(sessions());
    filterBtn(mount, 'all').click();

    expect(groupLabels(mount)).toEqual([
      'LAST HOUR',
      'TODAY',
      'YESTERDAY',
      'THIS WEEK',
      'OLDER',
    ]);
    expect(filterBtn(mount, 'all').getAttribute('aria-pressed')).toBe('true');
    expect(filterBtn(mount, 'recent').getAttribute('aria-pressed')).toBe(
      'false',
    );
    for (const label of ['YESTERDAY', 'THIS WEEK', 'OLDER']) {
      const group = groupByLabel(mount, label)!;
      expect(group.classList.contains('time-group-collapsed')).toBe(true);
      expect(
        group
          .querySelector('.time-group-header')!
          .getAttribute('aria-expanded'),
      ).toBe('false');
    }
    expect(
      groupByLabel(mount, 'TODAY')!.classList.contains('time-group-collapsed'),
    ).toBe(false);
  });

  it('starts THIS WEEK on the ISO-week Monday, like the server', async () => {
    const { mount } = await setup([
      makeSession({ id: 'monday', lastActive: ago(4 * 24 * HOUR) }),
      makeSession({ id: 'sunday', lastActive: ago(5 * 24 * HOUR) }),
    ]);
    filterBtn(mount, 'all').click();
    for (const label of ['THIS WEEK', 'OLDER']) {
      groupByLabel(mount, label)!
        .querySelector<HTMLElement>('.time-group-header')!
        .click();
    }

    expect(cardIds(groupByLabel(mount, 'THIS WEEK')!)).toEqual(['monday']);
    expect(cardIds(groupByLabel(mount, 'OLDER')!)).toEqual(['sunday']);
  });

  it('shows only the active section under ACTIVE, or an empty message', async () => {
    const { mount } = await setup(sessions());
    filterBtn(mount, 'active').click();
    expect(groupLabels(mount)).toEqual([]);
    expect(cardIds(mount)).toEqual(['live']);

    const empty = await setup([makeSession({ id: 'hour' })]);
    filterBtn(empty.mount, 'active').click();
    expect(empty.mount.querySelector('.time-group-empty')?.textContent).toBe(
      'NO ACTIVE SESSIONS',
    );
  });

  it('switches filter with the 1/2/3 keys but not while typing in a field', async () => {
    const { mount } = await setup(sessions());
    const press = (key: string, target: EventTarget = document) =>
      target.dispatchEvent(
        new KeyboardEvent('keydown', { key, bubbles: true }),
      );

    press('3');
    expect(groupLabels(mount)).toHaveLength(5);
    expect(filterBtn(mount, 'all').classList.contains('active')).toBe(true);

    press('1');
    expect(groupLabels(mount)).toEqual([]);
    expect(filterBtn(mount, 'active').getAttribute('aria-pressed')).toBe(
      'true',
    );

    const input = document.createElement('input');
    document.body.appendChild(input);
    press('2', input);
    expect(groupLabels(mount)).toEqual([]);

    press('2');
    expect(groupLabels(mount)).toEqual(['LAST HOUR', 'TODAY']);
  });

  it('toggles a group open and closed from its header', async () => {
    const { mount } = await setup(sessions());
    const group = groupByLabel(mount, 'LAST HOUR')!;
    const header = group.querySelector<HTMLElement>('.time-group-header')!;

    header.click();
    expect(group.classList.contains('time-group-collapsed')).toBe(true);
    expect(header.getAttribute('aria-expanded')).toBe('false');

    header.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    expect(group.classList.contains('time-group-collapsed')).toBe(false);
    expect(header.getAttribute('aria-expanded')).toBe('true');
  });
});

describe('session list filtering', () => {
  it('hides trivial sessions from time groups but not from ACTIVE NOW', async () => {
    const trivial = {
      totalCost: 0,
      inputTokens: 0,
      outputTokens: 0,
      messageCount: 1,
    };
    const { mount } = await setup([
      makeSession({ id: 'idle-trivial', ...trivial }),
      makeSession({
        id: 'live-trivial',
        ...trivial,
        isActive: true,
        lastActive: ago(1_000),
      }),
      makeSession({ id: 'real' }),
    ]);

    expect(cardIds(mount)).toEqual(['live-trivial', 'real']);
  });

  it('shows only sessions matching the repo filter by cwd or name', async () => {
    const { mount, update } = await setup([
      makeSession({ id: 'a', cwd: '/src/alpha' }),
      makeSession({ id: 'b', cwd: '/src/beta', lastActive: ago(11 * MIN) }),
      makeSession({
        id: 'named',
        sessionName: '/src/alpha',
        lastActive: ago(12 * MIN),
      }),
    ]);

    update({ repoFilter: '/src/alpha' });

    expect(cardIds(mount)).toEqual(['a', 'named']);
    expect(count(mount, 'all')).toBe('2');
  });

  it('ACTIVE count honours the repo filter', async () => {
    const { mount, update } = await setup([
      makeSession({
        id: 'a',
        cwd: '/a',
        isActive: true,
        lastActive: ago(1_000),
      }),
      makeSession({
        id: 'b',
        cwd: '/b',
        isActive: true,
        lastActive: ago(1_000),
      }),
    ]);

    update({ repoFilter: '/a' });

    expect(activeIds(mount)).toEqual(['a']);
    expect(count(mount, 'active')).toBe('1');
  });

  it('RECENT count matches the cards RECENT shows', async () => {
    const { mount } = await setup([
      makeSession({ id: 'hour', lastActive: ago(10 * MIN) }),
      makeSession({ id: 'morning', lastActive: ago(6 * HOUR) }),
    ]);

    expect(cardIds(mount)).toEqual(['hour', 'morning']);
    expect(count(mount, 'recent')).toBe('2');
  });

  it('truncates a long group and expands it with SHOW ALL', async () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      makeSession({ id: `s${i}`, lastActive: ago((i + 1) * MIN) }),
    );
    const { mount } = await setup(many);

    expect(cardIds(mount)).toHaveLength(15);
    const btn = mount.querySelector<HTMLButtonElement>('.show-all-btn')!;
    expect(btn.textContent).toBe('SHOW ALL 20 SESSIONS');

    btn.click();
    expect(cardIds(mount)).toHaveLength(20);
    expect(mount.querySelector('.show-all-btn')).toBeNull();

    filterBtn(mount, 'all').click();
    expect(cardIds(mount)).toHaveLength(15);
  });
});

describe('session list selection', () => {
  it('selects a session on click and deselects on a second click', async () => {
    const { mount, state } = await setup([
      makeSession({ id: 'a' }),
      makeSession({ id: 'b', lastActive: ago(11 * MIN) }),
    ]);
    const card = (id: string) =>
      mount.querySelector<HTMLElement>(`[data-session-id="${id}"]`)!;

    card('b').click();
    expect(state.selectedSessionId).toBe('b');
    expect(card('b').classList.contains('selected')).toBe(true);
    expect(card('a').classList.contains('selected')).toBe(false);

    card('b').click();
    expect(state.selectedSessionId).toBeNull();
    expect(mount.querySelector('.selected')).toBeNull();
  });

  it('switches to ALL and expands the group when an older session is selected', async () => {
    const { mount, update } = await setup([
      makeSession({ id: 'hour' }),
      makeSession({ id: 'yday', lastActive: ago(20 * HOUR) }),
    ]);
    expect(cardIds(mount)).toEqual(['hour']);

    update({ selectedSessionId: 'yday' });

    expect(filterBtn(mount, 'all').getAttribute('aria-pressed')).toBe('true');
    const group = groupByLabel(mount, 'YESTERDAY')!;
    expect(group.classList.contains('time-group-collapsed')).toBe(false);
    expect(
      group
        .querySelector('[data-session-id="yday"]')!
        .classList.contains('selected'),
    ).toBe(true);
  });

  it('shows subagents inline only while their family is selected', async () => {
    const { mount, update, state } = await setup([
      makeSession({ id: 'parent', children: ['kid'] }),
      makeSession({ id: 'kid', parentId: 'parent', lastActive: ago(11 * MIN) }),
    ]);
    expect(cardIds(mount)).toEqual(['parent']);

    update({ selectedSessionId: 'parent' });
    expect(cardIds(mount)).toEqual(['parent', 'kid']);
    expect(
      mount
        .querySelector('[data-session-id="kid"]')!
        .classList.contains('subagent'),
    ).toBe(true);

    update({
      collapsedSubagents: new Set(['parent']),
      renderVersion: state.renderVersion + 1,
    });
    expect(cardIds(mount)).toEqual(['parent']);
  });

  it('marks the keyboard-focused card', async () => {
    const { mount, update } = await setup([
      makeSession({ id: 'a' }),
      makeSession({ id: 'b', lastActive: ago(11 * MIN) }),
    ]);

    update({ focusedSessionId: 'b' });

    const focused = mount.querySelectorAll<HTMLElement>('.focused');
    expect([...focused].map((c) => c.dataset.sessionId)).toEqual(['b']);
  });
});

describe('session list liveness', () => {
  it('moves a stale server-active session out of ACTIVE NOW on the 5s refresh', async () => {
    const live = makeSession({
      id: 'live',
      isActive: true,
      lastActive: ago(0),
    });
    const { mount, state } = await setup([live]);
    expect(activeIds(mount)).toEqual(['live']);

    vi.advanceTimersByTime(35_000);

    expect(activeIds(mount)).toEqual([]);
    expect(groupLabels(mount)).toEqual(['LAST HOUR']);
    expect(state.sessions.get('live')!.isActive).toBe(true);
  });

  it('keeps a stale parent active as waiting while a child is still running', async () => {
    const { mount } = await setup([
      makeSession({
        id: 'parent',
        isActive: true,
        status: 'thinking',
        lastActive: ago(2 * MIN),
        children: ['kid'],
      }),
      makeSession({
        id: 'kid',
        parentId: 'parent',
        isActive: true,
        lastActive: ago(1_000),
      }),
    ]);

    expect(activeIds(mount)).toEqual(['parent']);
    expect(
      mount.querySelector('[data-session-id="parent"] .session-status-badge')
        ?.textContent,
    ).toBe('WAITING');
  });

  it('coalesces bursts of session updates and catches up on the refresh', async () => {
    const { mount, updateSession } = await setup([makeSession({ id: 'a' })]);

    updateSession(makeSession({ id: 'b', lastActive: ago(11 * MIN) }));
    updateSession(makeSession({ id: 'c', lastActive: ago(12 * MIN) }));
    expect(cardIds(mount)).toEqual(['a', 'b']);

    vi.advanceTimersByTime(5_000);
    expect(cardIds(mount)).toEqual(['a', 'b', 'c']);
  });

  it('collapses the sidebar from its toggle', async () => {
    const { mount, state } = await setup([makeSession({ id: 'a' })]);
    const toggle = mount.querySelector<HTMLButtonElement>('.sidebar-toggle')!;

    toggle.click();
    expect(state.sidebarCollapsed).toBe(true);
    expect(
      mount.querySelector('.sessions-panel')!.classList.contains('collapsed'),
    ).toBe(true);
    expect(toggle.getAttribute('aria-label')).toBe('Expand sidebar');

    toggle.click();
    expect(state.sidebarCollapsed).toBe(false);
    expect(toggle.getAttribute('aria-label')).toBe('Collapse sidebar');
  });
});
