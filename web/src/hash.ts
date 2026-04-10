// web/src/hash.ts
import { state, subscribe, update } from './state';

let debounceTimer: ReturnType<typeof setTimeout> | null = null;

export function init(): void {
  // Restore state from hash on load
  restoreFromHash();

  // Listen for browser back/forward
  window.addEventListener('hashchange', restoreFromHash);

  // Write hash on state changes
  subscribe((_state, changed) => {
    if (changed.has('selectedSessionId') || changed.has('view')) {
      if (debounceTimer) clearTimeout(debounceTimer);
      debounceTimer = setTimeout(writeHash, 200);
    }
  });
}

function writeHash(): void {
  const parts: string[] = [];
  if (state.selectedSessionId) parts.push(`session=${state.selectedSessionId}`);
  if (state.view !== 'list') parts.push(`view=${state.view}`);
  const hash = parts.length > 0 ? '#' + parts.join('&') : '';
  if (location.hash !== hash) {
    history.replaceState(null, '', hash || location.pathname);
  }
}

function restoreFromHash(): void {
  const hash = location.hash.slice(1);
  if (!hash) return;
  const params = new URLSearchParams(hash);
  const changes: Record<string, unknown> = {};
  const session = params.get('session');
  if (session) changes.selectedSessionId = session;
  const view = params.get('view');
  if (view && ['list', 'graph', 'history', 'analytics', 'timeline'].includes(view)) {
    changes.view = view;
  }
  if (Object.keys(changes).length > 0) {
    update(changes as Partial<typeof state>);
  }
}
