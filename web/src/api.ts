import type { GroupedSessions, ProjectEntry, SearchResult, Session, HistoryRow } from './types';

const BASE = '';

export async function fetchSessions(): Promise<Session[]> {
  const res = await fetch(`${BASE}/api/sessions`);
  return res.json();
}

export async function fetchGroupedSessions(): Promise<GroupedSessions> {
  const res = await fetch(`${BASE}/api/sessions/grouped`);
  return res.json();
}

export async function fetchProjects(): Promise<ProjectEntry[]> {
  const res = await fetch(`${BASE}/api/projects`);
  return res.json();
}

export async function fetchSearch(query: string, limit = 50): Promise<SearchResult[]> {
  const res = await fetch(`${BASE}/api/search?q=${encodeURIComponent(query)}&limit=${limit}`);
  return res.json();
}

export async function fetchHistory(limit = 50, offset = 0): Promise<HistoryRow[]> {
  const res = await fetch(`${BASE}/api/history?limit=${limit}&offset=${offset}`);
  return res.json();
}

export async function fetchRecentMessages(sessionId: string): Promise<unknown[]> {
  const res = await fetch(`${BASE}/api/sessions/${sessionId}/recent`);
  return res.json();
}

export async function fetchVersion(): Promise<string> {
  const res = await fetch(`${BASE}/api/version`);
  const data = await res.json();
  return data.version;
}
