// web/src/components/replay.ts
import type { ParsedMessage } from '../types';
import { state, update } from '../state';
import { renderFeedEntry } from './render-message';
import { escapeHtml } from '../utils';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let feedEl: HTMLElement | null = null;
let playBtn: HTMLElement | null = null;
let scrubber: HTMLInputElement | null = null;
let progressEl: HTMLElement | null = null;
let es: EventSource | null = null;
let totalEvents = 0;
let currentIndex = 0;
let manifestEvents: ParsedMessage[] = [];

export function render(mount: HTMLElement): void {
  container = mount;
}

export function open(sessionId: string): void {
  update({ replaySessionId: sessionId, replayPlaying: false });
  renderReplayPanel(sessionId);
  loadManifest(sessionId);
}

export function close(): void {
  stopStream();
  update({ replaySessionId: null, replayPlaying: false });
}

function renderReplayPanel(sessionId: string): void {
  if (!container) return;

  const session = state.sessions.get(sessionId);
  const name = session?.sessionName || session?.projectName || sessionId;

  container.innerHTML = '';
  const panel = document.createElement('div');
  panel.className = 'replay-panel';
  panel.innerHTML = `
    <div class="replay-header">
      <span class="session-label">${escapeHtml(name)}</span>
      <button class="replay-close-btn" title="Close replay">✕</button>
    </div>
    <div class="replay-feed">
      <div class="replay-empty">PRESS PLAY TO BEGIN</div>
    </div>
    <div class="replay-controls">
      <button class="replay-restart-btn" title="Restart">⏮</button>
      <button class="replay-play-btn">▶ PLAY</button>
      <select class="replay-speed">
        <option value="0.5">0.5x</option>
        <option value="1" selected>1x</option>
        <option value="2">2x</option>
        <option value="4">4x</option>
      </select>
      <input type="range" class="replay-scrubber" min="0" max="0" value="0" />
      <span class="replay-progress">0 / 0</span>
    </div>
  `;

  container.appendChild(panel);

  feedEl = panel.querySelector('.replay-feed')!;
  playBtn = panel.querySelector('.replay-play-btn')!;
  scrubber = panel.querySelector('.replay-scrubber')!;
  progressEl = panel.querySelector('.replay-progress')!;

  panel.querySelector('.replay-close-btn')!.addEventListener('click', close);
  panel.querySelector('.replay-restart-btn')!.addEventListener('click', restart);
  playBtn.addEventListener('click', togglePlay);
  scrubber.addEventListener('input', onScrub);
}

async function loadManifest(sessionId: string): Promise<void> {
  try {
    const res = await fetch(`/api/sessions/${sessionId}/replay`);
    const data = await res.json();
    manifestEvents = (data.events ?? []).map((e: unknown) => e as ParsedMessage);
    totalEvents = manifestEvents.length;
    if (scrubber) scrubber.max = String(totalEvents);
    updateProgress();
  } catch (err) {
    console.error('Failed to load replay manifest:', err);
  }
}

export function togglePlay(): void {
  if (state.replayPlaying) {
    stopStream();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  } else {
    startStream();
    update({ replayPlaying: true });
    if (playBtn) playBtn.textContent = '⏸ PAUSE';
  }
}

export function restart(): void {
  stopStream();
  currentIndex = 0;
  if (feedEl) feedEl.innerHTML = '<div class="replay-empty">PRESS PLAY TO BEGIN</div>';
  if (scrubber) scrubber.value = '0';
  updateProgress();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
}

function onScrub(): void {
  if (!scrubber) return;
  stopStream();
  currentIndex = parseInt(scrubber.value, 10);
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  updateProgress();
}

function startStream(): void {
  if (!state.replaySessionId) return;
  const speed = (container?.querySelector('.replay-speed') as HTMLSelectElement)?.value ?? '1';
  const url = `/api/sessions/${state.replaySessionId}/replay/stream?from=${currentIndex}&speed=${speed}`;

  // Clear placeholder
  if (feedEl && currentIndex === 0) {
    feedEl.innerHTML = '';
  }

  es = new EventSource(url);

  es.addEventListener('message', (e) => {
    const data = JSON.parse(e.data);
    if (data.message) {
      const entry = renderFeedEntry(data.message as ParsedMessage);
      feedEl?.appendChild(entry);
      feedEl!.scrollTop = feedEl!.scrollHeight;
    }
    currentIndex = (data.index ?? currentIndex) + 1;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  });

  es.addEventListener('done', () => {
    stopStream();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  });

  es.onerror = () => {
    stopStream();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  };
}

function stopStream(): void {
  if (es) {
    es.close();
    es = null;
  }
}

function updateProgress(): void {
  if (progressEl) {
    progressEl.textContent = `${currentIndex} / ${totalEvents}`;
  }
}

export function stepForward(): void {
  if (!state.replaySessionId) return;
  stopStream();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  if (currentIndex < totalEvents) {
    const evt = manifestEvents[currentIndex];
    if (evt && feedEl) {
      const empty = feedEl.querySelector('.replay-empty');
      if (empty) empty.remove();
      feedEl.appendChild(renderFeedEntry(evt));
      feedEl.scrollTop = feedEl.scrollHeight;
    }
    currentIndex++;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  }
}

export function stepBackward(): void {
  if (!state.replaySessionId) return;
  stopStream();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  if (currentIndex > 0 && feedEl) {
    if (feedEl.lastElementChild) feedEl.removeChild(feedEl.lastElementChild);
    currentIndex--;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  }
}

