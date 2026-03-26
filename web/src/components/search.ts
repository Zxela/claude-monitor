import type { SearchResult } from '../types';
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
        update({ searchResults: results, searchLoading: false });
      } catch {
        update({ searchResults: [], searchLoading: false });
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

  if (state.searchResults.length === 0 && state.searchQuery.length > 0) {
    dropdown.innerHTML = `<div class="search-status">No results for "${escapeHtml(state.searchQuery)}"</div>`;
    return;
  }

  const groups = new Map<string, { name: string; project: string; results: SearchResult[] }>();
  for (const r of state.searchResults) {
    let group = groups.get(r.sessionId);
    if (!group) {
      group = { name: r.sessionName, project: r.projectName, results: [] };
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
      el.innerHTML = `
        <div class="search-result-header">
          <span class="search-result-session">${escapeHtml(group.name)}</span>
          <span class="search-result-project">${escapeHtml(group.project)}</span>
          <span class="search-type-badge ${badgeType(result)}">${badgeType(result)}</span>
          <span class="search-result-time">${formatTime(result.timestamp)}</span>
        </div>
        <div class="search-result-body">${highlightMatch(result.contentText, state.searchQuery)}</div>
      `;
      el.addEventListener('click', () => {
        update({ selectedSessionId: sessionId, searchOpen: false, searchQuery: '' });
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

function badgeType(r: SearchResult): string {
  if (r.isError) return 'error';
  if (r.toolName) return 'tool';
  return r.role || 'assistant';
}

function formatTime(ts: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

function highlightMatch(text: string, query: string): string {
  if (!text || !query) return escapeHtml(text || '');
  const truncated = text.length > 200 ? text.substring(0, 200) + '...' : text;
  const escaped = escapeHtml(truncated);
  const re = new RegExp(`(${escapeRegex(query)})`, 'gi');
  return escaped.replace(re, '<mark>$1</mark>');
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
