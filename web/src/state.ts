import type { Session, GroupedSessions, SearchResult, ProjectEntry } from './types';

export interface AppState {
  sessions: Map<string, Session>;
  grouped: GroupedSessions | null;
  projects: ProjectEntry[];
  selectedSessionId: string | null;
  view: 'list' | 'graph' | 'history' | 'table';
  projectFilter: string | null;
  searchQuery: string;
  searchResults: SearchResult[];
  searchLoading: boolean;
  searchOpen: boolean;
  connected: boolean;
  eventCount: number;
  version: string;

  // Feed
  feedTypeFilters: Record<string, boolean>;

  // Replay
  replaySessionId: string | null;
  replayPlaying: boolean;

  // Budget
  budgetThreshold: number | null;
  budgetDismissed: boolean;

  // Render
  renderVersion: number;  // bump to force re-renders
}

type Listener = (state: AppState, changedKeys: Set<string>) => void;

const listeners: Listener[] = [];

export const state: AppState = {
  sessions: new Map(),
  grouped: null,
  projects: [],
  selectedSessionId: null,
  view: 'list',
  projectFilter: null,
  searchQuery: '',
  searchResults: [],
  searchLoading: false,
  searchOpen: false,
  connected: false,
  eventCount: 0,
  version: '',
  feedTypeFilters: { user: true, assistant: true, tool: true, result: true, agent: true, hook: true, error: true },
  replaySessionId: null,
  replayPlaying: false,
  budgetThreshold: null,
  budgetDismissed: false,
  renderVersion: 0,
};

export function subscribe(listener: Listener): () => void {
  listeners.push(listener);
  return () => {
    const idx = listeners.indexOf(listener);
    if (idx >= 0) listeners.splice(idx, 1);
  };
}

export function update(changes: Partial<AppState>): void {
  const changedKeys = new Set<string>();
  for (const [key, value] of Object.entries(changes)) {
    if ((state as unknown as Record<string, unknown>)[key] !== value) {
      (state as unknown as Record<string, unknown>)[key] = value;
      changedKeys.add(key);
    }
  }
  if (changedKeys.size > 0) {
    for (const listener of listeners) {
      listener(state, changedKeys);
    }
  }
}

export function updateSession(session: Session): void {
  state.sessions.set(session.id, session);
  for (const listener of listeners) {
    listener(state, new Set(['sessions']));
  }
}
