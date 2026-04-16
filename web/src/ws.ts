import type { WsEvent } from './types';
import { state, update, updateSession } from './state';
import { fetchGroupedSessions } from './api';

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectDelay = 2000;
const MAX_RECONNECT_DELAY = 30000;

type MessageHandler = (event: WsEvent) => void;
const handlers: MessageHandler[] = [];

export function onMessage(handler: MessageHandler): void {
  handlers.push(handler);
}

// Dropped-events warning banner — shown when the server reports dropped events.
let droppedBanner: HTMLElement | null = null;
let droppedBannerDismissedAt = 0; // total drop count at time of last dismiss

function showDroppedEventsBanner(delta: number, total: number): void {
  // Don't re-show if the user already dismissed at this total count.
  if (total <= droppedBannerDismissedAt) return;

  // Create the banner element the first time.
  if (!droppedBanner) {
    droppedBanner = document.createElement('div');
    droppedBanner.setAttribute('role', 'alert');
    droppedBanner.style.cssText = [
      'display:none',
      'align-items:center',
      'justify-content:space-between',
      'gap:8px',
      'padding:8px 14px',
      'font-size:12px',
      'font-family:inherit',
      'background:rgba(255,160,0,0.15)',
      'color:#ffb300',
      'border:1px solid rgba(255,160,0,0.4)',
      'border-radius:4px',
      'margin:6px 8px 0',
      'box-sizing:border-box',
    ].join(';');

    const msgSpan = document.createElement('span');
    msgSpan.className = 'dropped-events-msg';
    droppedBanner.appendChild(msgSpan);

    const dismissBtn = document.createElement('button');
    dismissBtn.textContent = '×';
    dismissBtn.setAttribute('aria-label', 'Dismiss dropped-events warning');
    dismissBtn.style.cssText = [
      'background:none',
      'border:none',
      'color:inherit',
      'cursor:pointer',
      'font-size:16px',
      'line-height:1',
      'padding:0 2px',
      'flex-shrink:0',
    ].join(';');
    droppedBanner.appendChild(dismissBtn);

    // Insert at the top of #app, before the update banner if present.
    const app = document.getElementById('app');
    if (app) app.prepend(droppedBanner);
  }

  // Update message text and show.
  const msgSpan = droppedBanner.querySelector<HTMLElement>('.dropped-events-msg');
  if (msgSpan) {
    msgSpan.textContent =
      `\u26a0 ${delta} event(s) were dropped \u2014 session data may be incomplete. ` +
      `Reduce active sessions or restart to recover.`;
  }
  // Update dismiss closure to capture latest total.
  const dismissBtn = droppedBanner.querySelector<HTMLButtonElement>('button');
  if (dismissBtn) {
    const capturedTotal = total;
    dismissBtn.onclick = () => {
      droppedBannerDismissedAt = capturedTotal;
      if (droppedBanner) droppedBanner.style.display = 'none';
    };
  }
  droppedBanner.style.display = 'flex';
}

export function connect(): void {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${protocol}//${location.host}/ws`;

  ws = new WebSocket(url);

  ws.onopen = () => {
    update({ connected: true });
    reconnectDelay = 2000; // Reset backoff on successful connection

    // Re-fetch sessions to reconcile state missed during disconnect
    fetchGroupedSessions()
      .then((grouped) => {
        const allSessions = [
          ...grouped.active,
          ...grouped.lastHour,
          ...grouped.today,
          ...grouped.yesterday,
          ...grouped.thisWeek,
          ...grouped.older,
        ];
        for (const sess of allSessions) {
          updateSession(sess);
        }
        update({ grouped });
      })
      .catch(() => {
        /* ignore — WS is connected, sessions will arrive via events */
      });
  };

  ws.onclose = () => {
    update({ connected: false });
    ws = null;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
  };

  ws.onerror = () => {
    ws?.close();
  };

  ws.onmessage = (e) => {
    let event: WsEvent;
    try {
      event = JSON.parse(e.data);
    } catch {
      console.warn('ws: failed to parse message', e.data);
      return;
    }
    // Handle update_available event (no session data).
    if (event.event === 'update_available' && event.version) {
      update({ updateVersion: event.version, updateUrl: event.url ?? null });
      return;
    }

    // Handle dropped_events notification from the server.
    if (event.type === 'dropped_events' && typeof event.delta === 'number' && typeof event.count === 'number') {
      showDroppedEventsBanner(event.delta, event.count);
      return;
    }

    update({ eventCount: state.eventCount + 1 });

    if (event.session) {
      updateSession(event.session);
    }

    for (const handler of handlers) {
      handler(event);
    }
  };
}
