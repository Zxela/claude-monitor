import type { Session, GroupedSessions, Event, Stats } from './types';
import type { StatsWindow } from './api';

export interface AppState {
  sessions: Map<string, Session>;
  grouped: GroupedSessions | null;
  selectedSessionId: string | null;
  view: 'list' | 'graph' | 'history' | 'analytics';
  repoFilter: string | null;
  searchQuery: string;
  searchResults: Event[];
  searchLoading: boolean;
  searchError: boolean;
  searchOpen: boolean;
  searchHighlightEventId: number | null;
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

  focusedSessionId: string | null;

  // Update
  updateVersion: string | null;
  updateUrl: string | null;
  updateDismissed: boolean;

  // History grouping
  historyShowSubagents: boolean;

  // Stats
  stats: Stats | null;
  statsWindow: StatsWindow;

  // Sidebar
  sidebarCollapsed: boolean;
}

type Listener = (state: AppState, changedKeys: Set<string>) => void;

const listeners: Listener[] = [];

export const state: AppState = {
  sessions: new Map(),
  grouped: null,
  selectedSessionId: null,
  view: 'list',
  repoFilter: null,
  searchQuery: '',
  searchResults: [],
  searchLoading: false,
  searchError: false,
  searchOpen: false,
  searchHighlightEventId: null,
  connected: false,
  eventCount: 0,
  version: '',
  feedTypeFilters: { user: true, assistant: true, tool_use: true, tool_result: true, agent: true, hook: true, error: true, command: false },
  replaySessionId: null,
  replayPlaying: false,
  budgetThreshold: null,
  budgetDismissed: false,
  renderVersion: 0,
  focusedSessionId: null,
  updateVersion: null,
  updateUrl: null,
  updateDismissed: false,
  historyShowSubagents: false,
  stats: null,
  statsWindow: (localStorage.getItem('claude-monitor-stats-window') as StatsWindow) || 'today',
  sidebarCollapsed: false,
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
    for (const listener of [...listeners]) {
      listener(state, changedKeys);
    }
  }
}

export function updateSession(session: Session): void {
  state.sessions.set(session.id, session);
  for (const listener of [...listeners]) {
    listener(state, new Set(['sessions']));
  }
}
