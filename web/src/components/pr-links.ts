// web/src/components/pr-links.ts
import type { PrLink } from '../types';

function isHttpUrl(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === 'http:' || protocol === 'https:';
  } catch {
    return false;
  }
}

/** Builds PR/MR links with DOM APIs so no field is parsed as HTML. */
export function renderPrLinks(links: PrLink[] | undefined): HTMLElement | null {
  const safe = (links ?? []).filter((l) => isHttpUrl(l.url));
  if (safe.length === 0) return null;
  const wrap = document.createElement('span');
  wrap.className = 'pr-links';
  wrap.style.cssText =
    'margin-left:8px;display:inline-flex;gap:6px;font-size:10px';
  for (const l of safe) {
    const a = document.createElement('a');
    a.href = l.url;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = `PR #${l.number}`;
    a.title = l.repository ? `${l.repository} #${l.number}` : l.url;
    a.style.color = 'var(--cyan)';
    wrap.appendChild(a);
  }
  return wrap;
}
