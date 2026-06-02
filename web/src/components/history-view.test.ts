import { describe, it, expect } from 'vitest';
import { groupRows } from './history-view';
import type { Session } from '../types';

// Minimal Session factory — only the fields groupRows/sortData touch.
// sortData (module default) sorts by lastActive DESC, so distinct lastActive
// values make ordering deterministic.
function makeSession(over: Partial<Session> & { id: string }): Session {
  return {
    totalCost: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    messageCount: 0,
    eventCount: 0,
    lastActive: '2024-01-01T00:00:00Z',
    isActive: false,
    startedAt: '2024-01-01T00:00:00Z',
    status: 'idle',
    costRate: 0,
    errorCount: 0,
    taskDescription: '',
    ...over,
  };
}

describe('groupRows', () => {
  it('groups a plain parent with two children into one group', () => {
    const parent = makeSession({ id: 'p', lastActive: '2024-01-01T03:00:00Z' });
    const c1 = makeSession({ id: 'c1', parentId: 'p', lastActive: '2024-01-01T02:00:00Z' });
    const c2 = makeSession({ id: 'c2', parentId: 'p', lastActive: '2024-01-01T01:00:00Z' });

    const groups = groupRows([parent, c1, c2]);
    expect(groups).toHaveLength(1);
    expect(groups[0].parent.id).toBe('p');
    expect(groups[0].children).toHaveLength(2);
    expect(groups[0].children.map((c) => c.id).sort()).toEqual(['c1', 'c2']);
  });

  it('promotes an orphan child (parent not in set) to top-level', () => {
    const orphan = makeSession({
      id: 'orphan',
      parentId: 'missing-parent',
      lastActive: '2024-01-01T05:00:00Z',
    });
    const top = makeSession({ id: 'top', lastActive: '2024-01-01T04:00:00Z' });

    const groups = groupRows([orphan, top]);
    const parentIds = groups.map((g) => g.parent.id).sort();
    expect(parentIds).toEqual(['orphan', 'top']);
    // Orphan is rendered top-level with no children of its own.
    const orphanGroup = groups.find((g) => g.parent.id === 'orphan')!;
    expect(orphanGroup.children).toHaveLength(0);
  });

  it('flattens a nested subagent under the top-level ancestor', () => {
    // grandparent (top-level) -> child -> grandchild
    const grandparent = makeSession({ id: 'gp', lastActive: '2024-01-01T09:00:00Z' });
    const child = makeSession({ id: 'c', parentId: 'gp', lastActive: '2024-01-01T08:00:00Z' });
    const grandchild = makeSession({
      id: 'gc',
      parentId: 'c',
      lastActive: '2024-01-01T07:00:00Z',
    });

    const groups = groupRows([grandparent, child, grandchild]);
    // Only the grandparent is top-level.
    expect(groups).toHaveLength(1);
    expect(groups[0].parent.id).toBe('gp');
    // Both the direct child and the (flattened) grandchild land under gp.
    const childIds = groups[0].children.map((c) => c.id).sort();
    expect(childIds).toEqual(['c', 'gc']);
  });

  it('flattens a 4+-level chain entirely under the top-level ancestor', () => {
    // gp -> c -> gc -> ggc: the ancestor walk must climb past every intermediate
    // level, not just one, so all descendants collapse under the single root.
    const gp = makeSession({ id: 'gp', lastActive: '2024-01-01T09:00:00Z' });
    const c = makeSession({ id: 'c', parentId: 'gp', lastActive: '2024-01-01T08:00:00Z' });
    const gc = makeSession({ id: 'gc', parentId: 'c', lastActive: '2024-01-01T07:00:00Z' });
    const ggc = makeSession({ id: 'ggc', parentId: 'gc', lastActive: '2024-01-01T06:00:00Z' });

    const groups = groupRows([gp, c, gc, ggc]);
    expect(groups).toHaveLength(1);
    expect(groups[0].parent.id).toBe('gp');
    expect(groups[0].children.map((x) => x.id).sort()).toEqual(['c', 'gc', 'ggc']);
  });

  it('terminates on a parentId cycle (depth guard)', () => {
    // a <-> b form a parentId cycle. The ancestor walk is bounded by the depth
    // guard, so groupRows must return rather than loop forever. Neither node has a
    // root parent (each claims an in-set parent), so there are no top-level rows.
    const a = makeSession({ id: 'a', parentId: 'b', lastActive: '2024-01-01T02:00:00Z' });
    const b = makeSession({ id: 'b', parentId: 'a', lastActive: '2024-01-01T01:00:00Z' });

    const groups = groupRows([a, b]);
    expect(groups).toEqual([]);
  });
});
