const _escDiv = document.createElement('div');

export function escapeHtml(s: string): string {
  _escDiv.textContent = s;
  return _escDiv.innerHTML;
}

export function escapeAttr(s: string): string {
  return s.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function formatDurationSecs(secs: number): string {
  if (secs < 60) return `${Math.floor(secs)}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60);
  return `${h}h${m}m`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

const ACTIVE_THRESHOLD_MS = 45_000; // 45s — slightly longer than backend's 30s to prevent flashing

export function isSessionActive(lastActive: string): boolean {
  if (!lastActive) return false;
  return (Date.now() - new Date(lastActive).getTime()) < ACTIVE_THRESHOLD_MS;
}

export function timeAgo(ts: string): string {
  if (!ts) return '';
  const secs = Math.floor((Date.now() - new Date(ts).getTime()) / 1000);
  if (secs < 0) return 'now';
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
  return `${Math.floor(secs / 86400)}d ago`;
}
