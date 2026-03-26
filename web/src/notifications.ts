// web/src/notifications.ts

interface NotifSettings {
  budget: boolean;
  error: boolean;
}

let settings: NotifSettings = { budget: true, error: true };

export function loadSettings(): void {
  try {
    const saved = localStorage.getItem('notif-settings');
    if (saved) settings = JSON.parse(saved);
  } catch { /* ignore */ }
}

export function saveSettings(s: NotifSettings): void {
  settings = s;
  localStorage.setItem('notif-settings', JSON.stringify(s));
}

export function getSettings(): NotifSettings {
  return { ...settings };
}

export function notify(type: 'budget' | 'error', title: string, body: string): void {
  if (type === 'budget' && !settings.budget) return;
  if (type === 'error' && !settings.error) return;
  if (Notification.permission === 'default') {
    Notification.requestPermission();
    return;
  }
  if (Notification.permission !== 'granted') return;
  new Notification(title, { body });
}
