import type {
  Session,
  GroupedSessions,
  Event,
  Stats,
  StorageInfo,
  RepoEntry,
  TrendResult,
} from './types';

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

export async function fetchGroupedSessions(): Promise<GroupedSessions> {
  return request<GroupedSessions>(`/api/sessions?group=activity`);
}

export async function fetchSessions(limit = 50, offset = 0): Promise<Session[]> {
  return request<Session[]>(`/api/sessions?limit=${limit}&offset=${offset}`);
}

export async function fetchSearch(query: string, limit = 50): Promise<Event[]> {
  return request<Event[]>(`/api/search?q=${encodeURIComponent(query)}&limit=${limit}`);
}

export async function fetchFullSearch(query: string, limit = 50): Promise<Event[]> {
  return request<Event[]>(`/api/search/full?q=${encodeURIComponent(query)}&limit=${limit}`);
}

export interface CombinedSearchResult {
  results: Event[];
  meta: { searchedFull: boolean };
}

export async function fetchSearchCombined(
  query: string,
  limit = 50,
): Promise<CombinedSearchResult> {
  return request<CombinedSearchResult>(
    `/api/search/combined?q=${encodeURIComponent(query)}&limit=${limit}`,
  );
}

export async function fetchSessionEvents(sessionId: string, last?: number): Promise<Event[]> {
  const params = last ? `?last=${last}` : '';
  return request<Event[]>(`/api/sessions/${sessionId}/events${params}`);
}

export async function fetchPinnedEvents(sessionId: string): Promise<Event[]> {
  return request<Event[]>(`/api/sessions/${sessionId}/events?pinned=true`);
}

export async function fetchRepos(): Promise<RepoEntry[]> {
  return request<RepoEntry[]>(`/api/repos`);
}

export type StatsWindow = 'all' | 'today' | 'week' | 'month';

export async function fetchStats(window: StatsWindow = 'today'): Promise<Stats> {
  return request<Stats>(`/api/stats?window=${window}`);
}

export type TrendWindow = '24h' | '7d' | '30d';

export async function fetchTrends(window: TrendWindow = '7d', repo?: string): Promise<TrendResult> {
  const params = new URLSearchParams({ window });
  if (repo) params.set('repo', repo);
  return request<TrendResult>(`/api/stats/trends?${params}`);
}

export async function fetchVersion(): Promise<string> {
  const data = await request<{ version: string }>(`/api/version`);
  return data.version;
}

export async function fetchStorage(): Promise<StorageInfo> {
  return request<StorageInfo>(`/api/storage`);
}

export async function fetchSettings(): Promise<Record<string, string>> {
  return request<Record<string, string>>(`/api/settings`);
}

export async function clearRepoCache(): Promise<void> {
  await fetch(`/api/cache/repos`, { method: 'DELETE' });
}
