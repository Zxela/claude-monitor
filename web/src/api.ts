import type { GroupedSessions, SearchResult, HistoryRow, ParsedMessage, Stats } from './types';

const BASE = '';

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

export async function fetchGroupedSessions(): Promise<GroupedSessions> {
  return request<GroupedSessions>(`${BASE}/api/sessions/grouped`);
}

export async function fetchSearch(query: string, limit = 50): Promise<SearchResult[]> {
  return request<SearchResult[]>(`${BASE}/api/search?q=${encodeURIComponent(query)}&limit=${limit}`);
}

export async function fetchHistory(limit = 50, offset = 0): Promise<HistoryRow[]> {
  return request<HistoryRow[]>(`${BASE}/api/history?limit=${limit}&offset=${offset}`);
}

export async function fetchRecentMessages(sessionId: string): Promise<ParsedMessage[]> {
  return request<ParsedMessage[]>(`${BASE}/api/sessions/${sessionId}/recent`);
}

export type StatsWindow = 'all' | 'today' | 'week' | 'month';

export async function fetchStats(window: StatsWindow = 'today'): Promise<Stats> {
  return request<Stats>(`${BASE}/api/stats?window=${window}`);
}

export async function fetchVersion(): Promise<string> {
  const data = await request<{ version: string }>(`${BASE}/api/version`);
  return data.version;
}
