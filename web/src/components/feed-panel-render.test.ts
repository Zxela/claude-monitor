import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { Event, GroupedSessions, Session, WsEvent } from '../types';

const mocks = vi.hoisted(() => ({
  fetchSessionEvents: vi.fn(),
  fetchPinnedEvents: vi.fn(),
  notify: vi.fn(),
  openReplay: vi.fn(),
  wsHandlers: [] as ((e: WsEvent) => void)[],
}));
vi.mock('../api', () => ({
  fetchSessionEvents: mocks.fetchSessionEvents,
  fetchPinnedEvents: mocks.fetchPinnedEvents,
}));
vi.mock('../ws', () => ({
  onMessage: (h: (e: WsEvent) => void) => mocks.wsHandlers.push(h),
}));
vi.mock('../notifications', () => ({ notify: mocks.notify }));
vi.mock('./replay', () => ({ open: mocks.openReplay }));

const tick = () => new Promise((r) => setTimeout(r, 0));

let nextId = 1;
function ev(over: Partial<Event>): Event {
  return {
    id: nextId++,
    sessionId: 's1',
    type: 'message',
    role: 'user',
    contentPreview: 'hello',
    costUSD: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    isError: false,
    timestamp: '2026-01-01T00:00:00Z',
    ...over,
  };
}
const at = (sec: number) =>
  new Date(Date.UTC(2026, 0, 1, 0, 0, sec)).toISOString();

function sess(over: Partial<Session> & { id: string }): Session {
  return { sessionName: over.id, isActive: false, ...over } as Session;
}

async function setup(sessions: Session[] = [sess({ id: 's1' })]) {
  vi.resetModules();
  mocks.wsHandlers.length = 0;
  document.body.innerHTML = '';
  const st = await import('../state');
  for (const s of sessions) st.state.sessions.set(s.id, s);
  const { render } = await import('./feed-panel');
  const mount = document.createElement('div');
  document.body.appendChild(mount);
  render(mount);
  const push = (data: Event, session: Session = sessions[0]) => {
    for (const h of mocks.wsHandlers) h({ event: 'event', data, session });
  };
  return { ...st, mount, push };
}

async function selectWith(events: Event[], pinned: Event[] = []) {
  mocks.fetchSessionEvents.mockResolvedValue(events);
  mocks.fetchPinnedEvents.mockResolvedValue(pinned);
  const ctx = await setup();
  ctx.update({ selectedSessionId: 's1' });
  await tick();
  return ctx;
}

const entries = (root: ParentNode) => [
  ...root.querySelectorAll<HTMLElement>('.feed-entry'),
];
const visibleTypes = (root: ParentNode) =>
  entries(root)
    .filter((e) => e.style.display !== 'none')
    .map((e) => e.dataset.type);
const filterBtn = (root: ParentNode, type: string) =>
  root.querySelector<HTMLButtonElement>(
    `.feed-filter-btn[data-type="${type}"]`,
  )!;
const header = (root: ParentNode) =>
  root.querySelector('.feed-header')!.textContent!.replace(/\s+/g, ' ');

beforeEach(() => {
  nextId = 1;
  mocks.fetchSessionEvents.mockReset().mockResolvedValue([]);
  mocks.fetchPinnedEvents.mockReset().mockResolvedValue([]);
  mocks.notify.mockReset();
  mocks.openReplay.mockReset();
  Element.prototype.scrollIntoView = vi.fn();
});

describe('feed panel, all sessions', () => {
  it('starts with the all-sessions header, a prompt and the default filters', async () => {
    const { mount } = await setup();

    expect(header(mount)).toContain('LIVE FEED');
    expect(header(mount)).toContain('ALL SESSIONS');
    expect(mount.querySelector('.feed-empty')?.textContent).toBe(
      'Select a session to view its event feed',
    );
    expect(filterBtn(mount, 'command').getAttribute('aria-pressed')).toBe(
      'false',
    );
    expect(filterBtn(mount, 'user').getAttribute('aria-pressed')).toBe('true');
  });

  it('backfills from active sessions once, oldest first, deduped and labelled', async () => {
    mocks.fetchSessionEvents.mockImplementation(async (id: string) =>
      id === 'a'
        ? [
            ev({ contentPreview: 'a2', timestamp: at(2) }),
            ev({ contentPreview: 'dup', timestamp: at(5) }),
          ]
        : [
            ev({ contentPreview: 'b1', timestamp: at(1) }),
            ev({ contentPreview: 'dup', timestamp: at(5) }),
          ],
    );
    const { mount, update } = await setup([
      sess({ id: 'a', isActive: true }),
      sess({ id: 'b', isActive: true }),
      sess({ id: 'idle' }),
    ]);

    update({ grouped: {} as GroupedSessions });
    await tick();
    update({ grouped: {} as GroupedSessions });
    await tick();

    expect(mocks.fetchSessionEvents.mock.calls.map((c) => c[0]).sort()).toEqual(
      ['a', 'b'],
    );
    expect(mocks.fetchSessionEvents).toHaveBeenCalledWith('a', 20);
    const text = entries(mount).map((e) => e.textContent);
    expect(text).toHaveLength(3);
    expect(text[0]).toContain('b1');
    expect(text[1]).toContain('a2');
    expect(text[2]).toContain('dup');
    expect(mount.querySelector('.fe-sid')?.textContent).toBe('b');
  });

  it('appends live events from any session with a session label', async () => {
    const other = sess({ id: 'other' });
    const { mount, push } = await setup([sess({ id: 's1' }), other]);

    push(ev({ contentPreview: 'one' }));
    push(ev({ contentPreview: 'two' }), other);

    expect(entries(mount)).toHaveLength(2);
    expect(
      [...mount.querySelectorAll('.fe-sid')].map((s) => s.textContent),
    ).toEqual(['s1', 'other']);
    expect(mount.querySelector('.feed-empty')).toBeNull();
  });

  it('ignores live events while another view owns the mount', async () => {
    const { mount, push, update } = await setup();
    update({ view: 'graph' });

    push(ev({}));

    expect(entries(mount)).toHaveLength(0);
  });
});

describe('feed panel, one session', () => {
  it('loads recent and pinned events in time order without duplicates', async () => {
    const { mount } = await selectWith(
      [
        ev({ contentPreview: 'second', timestamp: at(2) }),
        ev({ contentPreview: 'third', timestamp: at(3) }),
      ],
      [
        ev({ contentPreview: 'first', timestamp: at(1), isError: true }),
        ev({ contentPreview: 'third', timestamp: at(3) }),
      ],
    );

    expect(mocks.fetchSessionEvents).toHaveBeenCalledWith('s1', 200);
    expect(mocks.fetchPinnedEvents).toHaveBeenCalledWith('s1');
    const text = entries(mount).map((e) => e.textContent);
    expect(text).toHaveLength(3);
    expect(text[0]).toContain('first');
    expect(text[1]).toContain('second');
    expect(text[2]).toContain('third');
    expect(mount.querySelector('.fe-sid')).toBeNull();
  });

  it('labels the header live or history from the session state', async () => {
    const { mount, update, state } = await setup([
      sess({ id: 'live', sessionName: 'Live One', isActive: true }),
      sess({ id: 'old', sessionName: 'Old One' }),
    ]);

    update({ selectedSessionId: 'live' });
    expect(header(mount)).toContain('LIVE FEED');
    expect(header(mount)).toContain('Live One');

    update({ selectedSessionId: 'old' });
    expect(header(mount)).toContain('SESSION HISTORY');
    expect(header(mount)).toContain('Old One');
    expect(state.selectedSessionId).toBe('old');
  });

  it('refreshes the header when the selected session gains a title or PR link', async () => {
    const { mount, update, updateSession } = await setup([
      sess({ id: 's1', sessionName: '', cwd: '/w/repo', isActive: true }),
    ]);
    update({ selectedSessionId: 's1' });
    const timeline = mount.querySelector('.timeline-btn');

    updateSession(
      sess({ id: 's1', sessionName: '', cwd: '/w/repo', isActive: true }),
    );
    expect(mount.querySelector('.timeline-btn')).toBe(timeline);

    updateSession(
      sess({
        id: 's1',
        sessionName: '',
        aiTitle: 'Fix login',
        isActive: true,
        prLinks: [{ number: 7, url: 'https://example/7', repository: 'g/r' }],
      }),
    );
    expect(header(mount)).toContain('Fix login');
    const link = mount.querySelector<HTMLAnchorElement>(
      '.feed-header .pr-links a',
    );
    expect(link?.href).toBe('https://example/7');
  });

  it('says so when the session has no events or the load fails', async () => {
    const empty = await selectWith([]);
    expect(empty.mount.querySelector('.feed-empty')?.textContent).toBe(
      'No events in this session yet',
    );

    mocks.fetchSessionEvents.mockRejectedValue(new Error('500'));
    const failed = await setup();
    failed.update({ selectedSessionId: 's1' });
    await tick();
    expect(failed.mount.querySelector('.feed-empty')?.textContent).toBe(
      'Failed to load messages',
    );
  });

  it('only appends live events for the selected session', async () => {
    const { mount, push } = await selectWith([ev({ contentPreview: 'x' })]);

    push(ev({ contentPreview: 'mine' }));
    push(ev({ contentPreview: 'theirs' }), sess({ id: 's2' }));

    const text = entries(mount).map((e) => e.textContent);
    expect(text).toHaveLength(2);
    expect(text[1]).toContain('mine');
  });

  it('raises a notification for a live error event', async () => {
    const { push } = await selectWith([]);

    push(ev({ isError: true, contentPreview: 'boom' }));

    expect(mocks.notify).toHaveBeenCalledWith(
      'error',
      'Agent Error',
      's1: boom',
    );
  });

  it('drops thinking-only assistant messages and empty system messages', async () => {
    const { mount, push } = await selectWith([]);

    push(ev({ role: 'assistant', contentPreview: '[thinking...]' }));
    push(ev({ role: '', contentPreview: '' }));
    push(ev({ role: 'assistant', contentPreview: 'answer' }));

    expect(entries(mount).map((e) => e.dataset.type)).toEqual(['assistant']);
  });

  it('highlights a search hit once and clears the request', async () => {
    mocks.fetchSessionEvents.mockResolvedValue([
      ev({ id: 41, contentPreview: 'a' }),
      ev({ id: 42, contentPreview: 'b', timestamp: at(1) }),
    ]);
    const { mount, update, state } = await setup();
    update({ searchHighlightEventId: 42 });
    update({ selectedSessionId: 's1' });
    await tick();

    const hit = mount.querySelector('[data-event-id="42"]')!;
    expect(hit.classList.contains('search-highlight')).toBe(true);
    expect(state.searchHighlightEventId).toBeNull();
  });

  it('wires the header buttons to back, timeline and replay', async () => {
    const { mount, state } = await selectWith([]);
    const btn = (cls: string) => mount.querySelector<HTMLElement>(cls)!;

    btn('.replay-btn').click();
    expect(mocks.openReplay).toHaveBeenCalledWith('s1');

    btn('.back-to-feed').click();
    expect(state.selectedSessionId).toBeNull();
    expect(header(mount)).toContain('ALL SESSIONS');
  });

  it('switches to the timeline view from the header', async () => {
    const { mount, state } = await selectWith([]);
    mount.querySelector<HTMLElement>('.timeline-btn')!.click();
    expect(state.view).toBe('timeline');
  });
});

describe('feed panel filters', () => {
  const mixed = () => [
    ev({ role: 'user', contentPreview: 'u', timestamp: at(1) }),
    ev({ role: 'assistant', contentPreview: 'a', timestamp: at(2) }),
    ev({
      role: 'assistant',
      toolName: 'Bash',
      toolUseId: 't1',
      contentPreview: '[tool: Bash] ls',
      timestamp: at(3),
    }),
    ev({
      role: 'user',
      forToolUseId: 't1',
      contentPreview: 'out',
      timestamp: at(4),
    }),
  ];

  it('hides and shows a type from its button', async () => {
    const { mount, state } = await selectWith(mixed());
    expect(visibleTypes(mount)).toEqual([
      'user',
      'assistant',
      'tool_use',
      'tool_use',
    ]);

    filterBtn(mount, 'user').click();
    expect(state.feedTypeFilters.user).toBe(false);
    expect(filterBtn(mount, 'user').getAttribute('aria-pressed')).toBe('false');
    expect(visibleTypes(mount)).not.toContain('user');

    filterBtn(mount, 'user').click();
    expect(visibleTypes(mount)).toContain('user');
  });

  it('turns tool results off along with tool calls', async () => {
    const { mount, state } = await selectWith(mixed());

    filterBtn(mount, 'tool_use').click();

    expect(state.feedTypeFilters.tool_use).toBe(false);
    expect(state.feedTypeFilters.tool_result).toBe(false);
    expect(visibleTypes(mount)).toEqual(['user', 'assistant']);
  });

  it('TOOL_RESULT hides results of tool calls in the feed', async () => {
    const { mount } = await selectWith(mixed());

    filterBtn(mount, 'tool_result').click();

    const result = mount.querySelector<HTMLElement>('.type-tool_result')!;
    expect(result.style.display).toBe('none');
  });

  it('solos a type on shift-click', async () => {
    const { mount, state } = await selectWith(mixed());

    filterBtn(mount, 'assistant').dispatchEvent(
      new MouseEvent('click', { shiftKey: true }),
    );

    expect(
      Object.entries(state.feedTypeFilters)
        .filter(([, on]) => on)
        .map(([t]) => t),
    ).toEqual(['assistant']);
    expect(visibleTypes(mount)).toEqual(['assistant']);
  });

  it('ALL turns everything on, then off with an empty-match notice', async () => {
    const { mount, state } = await selectWith(mixed());
    const all = filterBtn(mount, 'all');

    all.click();
    expect(Object.values(state.feedTypeFilters).every(Boolean)).toBe(true);
    expect(filterBtn(mount, 'command').getAttribute('aria-pressed')).toBe(
      'true',
    );

    all.click();
    expect(Object.values(state.feedTypeFilters).some(Boolean)).toBe(false);
    expect(visibleTypes(mount)).toEqual([]);
    expect(mount.querySelector('.feed-filter-empty')?.textContent).toBe(
      'No messages match the current filters',
    );

    all.click();
    expect(visibleTypes(mount)).toHaveLength(4);
    expect(mount.querySelector('.feed-filter-empty')).toBeNull();
  });

  it('hides new live entries whose type is filtered out', async () => {
    const { mount, push } = await selectWith([]);
    filterBtn(mount, 'user').click();

    push(ev({ role: 'user', contentPreview: 'hidden' }));
    push(ev({ role: 'assistant', contentPreview: 'shown' }));

    expect(visibleTypes(mount)).toEqual(['assistant']);
  });

  it('the err badge re-filters an already-selected session', async () => {
    const { mount, update } = await selectWith(mixed());
    const { errorsOnlyFilters } = await import('./session-card');

    update({ selectedSessionId: 's1', feedTypeFilters: errorsOnlyFilters() });

    expect(visibleTypes(mount)).toEqual([]);
  });

  it('errors-only view hides skill entries', async () => {
    mocks.fetchSessionEvents.mockResolvedValue([
      ev({ role: 'assistant', toolName: 'Skill', contentPreview: 'sk' }),
      ev({ isError: true, contentPreview: 'boom', timestamp: at(1) }),
    ]);
    const { mount, update } = await setup();
    const { errorsOnlyFilters } = await import('./session-card');

    update({ selectedSessionId: 's1', feedTypeFilters: errorsOnlyFilters() });
    await tick();

    expect(visibleTypes(mount)).toEqual(['error']);
  });
});

describe('feed panel tool grouping', () => {
  it('links a tool call, its hooks and its result, and highlights them together on hover', async () => {
    const { mount } = await selectWith([
      ev({
        role: 'assistant',
        toolName: 'Read',
        toolUseId: 'tu',
        contentPreview: 'x',
        timestamp: at(1),
      }),
      ev({
        role: 'system',
        hookEvent: 'PostToolUse',
        hookName: 'h',
        timestamp: at(2),
      }),
      ev({
        role: 'user',
        forToolUseId: 'tu',
        contentPreview: 'r',
        timestamp: at(3),
      }),
      ev({ role: 'user', contentPreview: 'next', timestamp: at(4) }),
    ]);
    const [call, hook, result, next] = entries(mount);

    expect(call.dataset.groupId).toBe('tu');
    expect(hook.dataset.groupId).toBe('tu');
    expect(hook.classList.contains('tool-group-child')).toBe(true);
    expect(result.dataset.groupId).toBe('tu');
    expect(next.dataset.groupId).toBeUndefined();

    hook.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    expect(
      entries(mount).map((e) => e.classList.contains('group-highlight')),
    ).toEqual([true, true, true, false]);

    mount
      .querySelector('.feed-content')!
      .dispatchEvent(new MouseEvent('mouseleave'));
    expect(mount.querySelectorAll('.group-highlight')).toHaveLength(0);
  });
});

describe('feed panel scroll lock', () => {
  it('keeps its resume-scroll button in the all-sessions feed', async () => {
    const { mount } = await setup();
    expect(mount.querySelector('.scroll-lock-btn')).not.toBeNull();
  });

  it('keeps its resume-scroll button after loading a session', async () => {
    const { mount } = await selectWith([ev({})]);
    expect(mount.querySelector('.scroll-lock-btn')).not.toBeNull();
  });

  it('keeps its resume-scroll button once the all-sessions feed hits its cap', async () => {
    const { mount, push } = await setup();
    for (let i = 0; i < 501; i++) push(ev({ contentPreview: `m${i}` }));
    expect(entries(mount)).toHaveLength(500);
    expect(mount.querySelector('.scroll-lock-btn')).not.toBeNull();
  });
});
