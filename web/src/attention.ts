import type { Session } from './types';
import { state } from './state';

const lastSeenErrors = new Map<string, number>();

export function needsAttention(sess: Session): boolean {
  if (sess.status === 'waiting') return true;
  const lastSeen = lastSeenErrors.get(sess.id) ?? 0;
  return sess.errorCount > lastSeen;
}

export function acknowledgeAttention(): void {
  for (const sess of state.sessions.values()) {
    lastSeenErrors.set(sess.id, sess.errorCount);
  }
  // Prune entries for sessions that no longer exist
  for (const id of lastSeenErrors.keys()) {
    if (!state.sessions.has(id)) {
      lastSeenErrors.delete(id);
    }
  }
}

export function getAttentionCount(): number {
  if (state.view === 'graph') return 0; // already viewing, no badge needed
  let count = 0;
  for (const sess of state.sessions.values()) {
    if (sess.isActive && needsAttention(sess)) count++;
  }
  return count;
}
