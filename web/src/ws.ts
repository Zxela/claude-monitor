import type { WsEvent } from './types';
import { state, update, updateSession } from './state';

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

type MessageHandler = (event: WsEvent) => void;
const handlers: MessageHandler[] = [];

export function onMessage(handler: MessageHandler): void {
  handlers.push(handler);
}

export function connect(): void {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${protocol}//${location.host}/ws`;

  ws = new WebSocket(url);

  ws.onopen = () => {
    update({ connected: true });
  };

  ws.onclose = () => {
    update({ connected: false });
    ws = null;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 2000);
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
    update({ eventCount: state.eventCount + 1 });

    if (event.session) {
      updateSession(event.session);
    }

    for (const handler of handlers) {
      handler(event);
    }
  };
}
