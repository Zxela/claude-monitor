import type { Session, GroupedSessions, Event, Stats, StorageInfo, RepoEntry } from './types';

const BASE = '';

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

export async function fetchGroupedSessions(): Promise<GroupedSessions> {
  return request<GroupedSessions>(`${BASE}/api/sessions?group=activity`);
}

export async function fetchSessions(limit = 50, offset = 0): Promise<Session[]> {
  return request<Session[]>(`${BASE}/api/sessions?limit=${limit}&offset=${offset}`);
}

export async function fetchSearch(query: string, limit = 50): Promise<Event[]> {
  return request<Event[]>(`${BASE}/api/search?q=${encodeURIComponent(query)}&limit=${limit}`);
}

export async function fetchFullSearch(query: string, limit = 50): Promise<Event[]> {
  return request<Event[]>(`${BASE}/api/search/full?q=${encodeURIComponent(query)}&limit=${limit}`);
}

export async function fetchSessionEvents(sessionId: string, last?: number): Promise<Event[]> {
  const params = last ? `?last=${last}` : '';
  return request<Event[]>(`${BASE}/api/sessions/${sessionId}/events${params}`);
}

export async function fetchPinnedEvents(sessionId: string): Promise<Event[]> {
  return request<Event[]>(`${BASE}/api/sessions/${sessionId}/events?pinned=true`);
}

export async function fetchRepos(): Promise<RepoEntry[]> {
  return request<RepoEntry[]>(`${BASE}/api/repos`);
}

export type StatsWindow = 'all' | 'today' | 'week' | 'month';

export async function fetchStats(window: StatsWindow = 'today'): Promise<Stats> {
  return request<Stats>(`${BASE}/api/stats?window=${window}`);
}

export async function fetchVersion(): Promise<string> {
  const data = await request<{ version: string }>(`${BASE}/api/version`);
  return data.version;
}

export async function fetchStorage(): Promise<StorageInfo> {
  return request<StorageInfo>(`${BASE}/api/storage`);
}

export async function fetchSettings(): Promise<Record<string, string>> {
  return request<Record<string, string>>(`${BASE}/api/settings`);
}

export async function clearRepoCache(): Promise<void> {
  await fetch(`${BASE}/api/cache/repos`, { method: 'DELETE' });
}
