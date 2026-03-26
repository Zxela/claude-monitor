import type { GroupedSessions, ProjectEntry, SearchResult, Session, HistoryRow, ParsedMessage } from './types';

const BASE = '';

export class ApiError extends Error {
  constructor(public status: number, public statusText: string) {
    super(`API error: ${status} ${statusText}`);
    this.name = 'ApiError';
  }
}

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new ApiError(res.status, res.statusText);
  }
  return res.json();
}

export async function fetchSessions(): Promise<Session[]> {
  return request<Session[]>(`${BASE}/api/sessions`);
}

export async function fetchGroupedSessions(): Promise<GroupedSessions> {
  return request<GroupedSessions>(`${BASE}/api/sessions/grouped`);
}

export async function fetchProjects(): Promise<ProjectEntry[]> {
  return request<ProjectEntry[]>(`${BASE}/api/projects`);
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

export async function fetchVersion(): Promise<string> {
  const data = await request<{ version: string }>(`${BASE}/api/version`);
  return data.version;
}
