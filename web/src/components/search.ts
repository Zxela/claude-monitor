import type { Event } from '../types';
import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import { fetchSearch } from '../api';
import { escapeHtml } from '../utils';
import '../styles/feed.css';

let dropdown: HTMLElement | null = null;
let debounceTimer: ReturnType<typeof setTimeout> | null = null;

export function render(searchBoxEl: HTMLElement): void {
  dropdown = document.createElement('div');
  dropdown.className = 'search-dropdown search-dropdown-hidden';
  dropdown.setAttribute('role', 'listbox');
  dropdown.setAttribute('aria-label', 'Search results');
  searchBoxEl.appendChild(dropdown);

  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('searchQuery')) {
    if (state.searchQuery.length === 0) {
      hideDropdown();
      return;
    }
    showDropdown();
    update({ searchLoading: true });

    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(async () => {
      try {
        const results = await fetchSearch(state.searchQuery);
        update({ searchResults: results, searchLoading: false, searchError: false });
      } catch (err) {
        console.error('Search failed:', err);
        update({ searchResults: [], searchLoading: false, searchError: true });
      }
    }, 300);
  }

  if (changed.has('searchResults') || changed.has('searchLoading')) {
    renderResults();
  }

  if (changed.has('searchOpen')) {
    if (!state.searchOpen) hideDropdown();
  }
}

function showDropdown(): void {
  dropdown?.classList.remove('search-dropdown-hidden');
}

function hideDropdown(): void {
  dropdown?.classList.add('search-dropdown-hidden');
}

function renderResults(): void {
  if (!dropdown) return;

  if (state.searchLoading) {
    dropdown.innerHTML = '<div class="search-status">Searching...</div>';
    return;
  }

  if (state.searchError) {
    dropdown.innerHTML = `<div class="search-status" style="color:var(--red)">Search failed — check server connection</div>`;
    return;
  }

  if (state.searchResults.length === 0 && state.searchQuery.length > 0) {
    dropdown.innerHTML = `<div class="search-status">No results for "${escapeHtml(state.searchQuery)}"</div>`;
    return;
  }

  const groups = new Map<string, { name: string; project: string; results: Event[] }>();
  for (const r of state.searchResults as unknown as Event[]) {
    let group = groups.get(r.sessionId);
    if (!group) {
      const sess = state.sessions.get(r.sessionId);
      const name = sess?.sessionName || r.sessionId.slice(0, 8);
      const project = sess?.cwd || '';
      group = { name, project, results: [] };
      groups.set(r.sessionId, group);
    }
    group.results.push(r);
  }

  dropdown.innerHTML = '';
  for (const [sessionId, group] of groups) {
    const visibleResults = group.results.slice(0, 3);
    const remaining = group.results.length - visibleResults.length;

    for (const result of visibleResults) {
      const el = document.createElement('div');
      el.className = 'search-result';
      el.setAttribute('role', 'option');
      el.innerHTML = `
        <div class="search-result-header">
          <span class="search-result-session">${escapeHtml(group.name)}</span>
          <span class="search-result-project">${escapeHtml(group.project)}</span>
          <span class="search-type-badge ${badgeType(result)}">${badgeType(result)}</span>
          <span class="search-result-time">${formatTime(result.timestamp)}</span>
        </div>
        <div class="search-result-body">${highlightMatch(searchPreview(result, state.searchQuery), state.searchQuery)}</div>
      `;
      el.addEventListener('click', () => {
        update({
          selectedSessionId: sessionId,
          searchOpen: false,
          searchQuery: '',
          searchHighlightEventId: result.id ?? null,
        });
        const searchInput = document.querySelector<HTMLInputElement>('[data-search]');
        if (searchInput) searchInput.value = '';
      });
      dropdown.appendChild(el);
    }

    if (remaining > 0) {
      const more = document.createElement('div');
      more.className = 'search-group-more';
      more.textContent = `${remaining} more matches in this session`;
      dropdown.appendChild(more);
    }
  }
}

/** Build a useful preview string for search results.
 *  Prefer whichever field actually contains the search match. */
function searchPreview(r: Event, query?: string): string {
  const cp = r.contentPreview || '';
  const name = r.toolName || '';
  const detail = r.toolDetail || '';

  // If we have a query, prefer the field that contains the match
  if (query) {
    const q = query.toLowerCase();
    if (detail && detail.toLowerCase().includes(q)) return name ? `${name}: ${detail}` : detail;
    if (cp && cp.toLowerCase().includes(q)) return cp;
    // Try individual words
    const words = q.split(/\s+/).filter((w) => w.length > 1);
    for (const w of words) {
      if (detail && detail.toLowerCase().includes(w)) return name ? `${name}: ${detail}` : detail;
    }
  }

  // Fallback: if contentPreview is just a bracketed tag, prefer richer fields
  if (cp.match(/^\[(?:tool|hook|agent):/)) {
    if (name && detail) return `${name}: ${detail}`;
    if (detail) return detail;
  }
  return cp;
}

function badgeType(r: Event): string {
  if (r.isError) return 'error';
  if (r.toolName) return 'tool';
  return r.role || 'assistant';
}

function formatTime(ts: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  const now = new Date();
  const isToday = d.toDateString() === now.toDateString();
  const time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  if (isToday) return time;
  const date = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  return `${date} ${time}`;
}

function highlightMatch(text: string, query: string): string {
  if (!text || !query) return escapeHtml(text || '');
  const maxLen = 80;
  const lowerText = text.toLowerCase();

  // Find the best match position — try full query first, then individual words
  let matchIdx = lowerText.indexOf(query.toLowerCase());
  if (matchIdx < 0) {
    const words = query.split(/\s+/).filter((w) => w.length > 1);
    for (const word of words) {
      matchIdx = lowerText.indexOf(word.toLowerCase());
      if (matchIdx >= 0) break;
    }
  }

  let truncated = text;
  if (text.length > maxLen) {
    if (matchIdx >= 0) {
      const padding = Math.floor((maxLen - query.length) / 2);
      const start = Math.max(0, matchIdx - padding);
      const end = Math.min(text.length, start + maxLen);
      truncated =
        (start > 0 ? '...' : '') + text.substring(start, end) + (end < text.length ? '...' : '');
    } else {
      truncated = text.substring(0, maxLen) + '...';
    }
  }

  const escaped = escapeHtml(truncated);
  // Highlight all query words individually
  const words = query.split(/\s+/).filter((w) => w.length > 1);
  const pattern = words.length > 0 ? words.map(escapeRegex).join('|') : escapeRegex(query);
  const re = new RegExp(`(${pattern})`, 'gi');
  return escaped.replace(re, '<mark>$1</mark>');
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
