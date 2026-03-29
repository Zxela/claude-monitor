// web/src/components/replay.ts
import type { ParsedMessage } from '../types';
import { state, update } from '../state';
import { renderFeedEntry } from './render-message';
import { escapeHtml, sessionDisplayName } from '../utils';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let panel: HTMLElement | null = null;
let feedEl: HTMLElement | null = null;
let playBtn: HTMLElement | null = null;
let scrubber: HTMLInputElement | null = null;
let progressEl: HTMLElement | null = null;
let playTimer: ReturnType<typeof setInterval> | null = null;
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

function close(): void {
  stopPlayback();
  if (panel) { panel.remove(); panel = null; }
  update({ replaySessionId: null, replayPlaying: false });
}

function renderReplayPanel(sessionId: string): void {
  if (!container) return;

  const session = state.sessions.get(sessionId);
  const name = session ? sessionDisplayName(session) : sessionId;

  if (panel) panel.remove();
  panel = document.createElement('div');
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
    stopPlayback();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  } else {
    startPlayback();
    update({ replayPlaying: true });
    if (playBtn) playBtn.textContent = '⏸ PAUSE';
  }
}

export function restart(): void {
  stopPlayback();
  currentIndex = 0;
  if (feedEl) feedEl.innerHTML = '<div class="replay-empty">PRESS PLAY TO BEGIN</div>';
  if (scrubber) scrubber.value = '0';
  updateProgress();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
}

function onScrub(): void {
  if (!scrubber) return;
  stopPlayback();
  currentIndex = parseInt(scrubber.value, 10);
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  updateProgress();
}

function startPlayback(): void {
  if (!state.replaySessionId || manifestEvents.length === 0) return;
  const speed = parseFloat((container?.querySelector('.replay-speed') as HTMLSelectElement)?.value ?? '1');

  // Clear placeholder
  if (feedEl && currentIndex === 0) {
    feedEl.innerHTML = '';
  }

  // Calculate delay between events based on actual timestamps and speed
  const baseDelay = Math.max(50, 200 / speed);

  playTimer = setInterval(() => {
    if (currentIndex >= totalEvents) {
      stopPlayback();
      update({ replayPlaying: false });
      if (playBtn) playBtn.textContent = '▶ PLAY';
      return;
    }
    const evt = manifestEvents[currentIndex];
    if (evt && feedEl) {
      feedEl.appendChild(renderFeedEntry(evt));
      feedEl.scrollTop = feedEl.scrollHeight;
    }
    currentIndex++;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  }, baseDelay);
}

function stopPlayback(): void {
  if (playTimer) {
    clearInterval(playTimer);
    playTimer = null;
  }
}

function updateProgress(): void {
  if (progressEl) {
    progressEl.textContent = `${currentIndex} / ${totalEvents}`;
  }
}

export function stepForward(): void {
  if (!state.replaySessionId) return;
  stopPlayback();
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
  stopPlayback();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  if (currentIndex > 0 && feedEl) {
    if (feedEl.lastElementChild) feedEl.removeChild(feedEl.lastElementChild);
    currentIndex--;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  }
}

