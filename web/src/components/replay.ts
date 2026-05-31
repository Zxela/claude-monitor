// web/src/components/replay.ts
import type { Event } from '../types';
import { state, update } from '../state';
import { renderFeedEntry } from './render-message';
import { escapeHtml, sessionDisplayName } from '../utils';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let panel: HTMLElement | null = null;
let feedEl: HTMLElement | null = null;
let playBtn: HTMLElement | null = null;
let scrubber: HTMLInputElement | null = null;
let speedSelect: HTMLSelectElement | null = null;
let progressEl: HTMLElement | null = null;
// Self-scheduling setTimeout handle (not setInterval) so each step can use its
// own computed delay and re-read the speed dropdown. stopPlayback() nulls it.
let playTimer: ReturnType<typeof setTimeout> | null = null;
let totalEvents = 0;
// currentIndex is a COUNT of rendered events (0..totalEvents inclusive).
let currentIndex = 0;
let manifestEvents: Event[] = [];
// True when the backend reports more events than we loaded (hasMore), so the
// progress label can show "N / M+" instead of a false "N / M" completeness.
let manifestTruncated = false;

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
  if (panel) {
    panel.remove();
    panel = null;
  }
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
  speedSelect = panel.querySelector('.replay-speed')!;
  progressEl = panel.querySelector('.replay-progress')!;

  panel.querySelector('.replay-close-btn')!.addEventListener('click', close);
  panel.querySelector('.replay-restart-btn')!.addEventListener('click', restart);
  playBtn.addEventListener('click', togglePlay);
  scrubber.addEventListener('input', onScrub);
  // Changing speed mid-play must take effect immediately: re-arm the loop.
  speedSelect.addEventListener('change', () => {
    if (state.replayPlaying) {
      stopPlayback();
      startPlayback();
    }
  });
}

async function loadManifest(sessionId: string): Promise<void> {
  try {
    // Request the full window explicitly (server hard-caps at 10000) instead of
    // the silent default of 1000 so the scrubber covers as many events as the
    // backend will serve.
    const res = await fetch(`/api/sessions/${sessionId}/replay?limit=10000`);
    const data = await res.json();
    manifestEvents = (data.events ?? []).map((e: unknown) => e as Event);
    totalEvents = manifestEvents.length;
    // The manifest reports the true count (total) and a hasMore flag; if the
    // backend truncated, surface it so the progress label does not claim a
    // false "N / N" completeness.
    manifestTruncated =
      data.hasMore === true ||
      (typeof data.total === 'number' && data.total > manifestEvents.length);
    // currentIndex is a COUNT (0..totalEvents inclusive), so the slider's max
    // is totalEvents: dragging fully right means "all N events shown".
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

/**
 * Pure: clamp a requested seek count `n` to the renderable range.
 * `n` is a COUNT (exclusive upper bound): renderUpToCount(len, len) === len so
 * seeking fully right includes the LAST event (indices 0..len-1). Negative or
 * over-large values clamp into [0, len]. Exported for unit testing the
 * off-by-one / inclusive-of-last-event property without a DOM.
 */
export function renderUpToCount(n: number, len: number): number {
  if (!Number.isFinite(n)) return 0;
  return Math.max(0, Math.min(Math.trunc(n), Math.max(0, len)));
}

/**
 * Re-render the feed to show exactly the first `n` events.
 * `n` is an EXCLUSIVE upper bound (a count): renderUpTo(totalEvents) appends
 * indices 0..totalEvents-1, so seeking fully right includes the LAST event.
 */
function renderUpTo(n: number): void {
  if (!feedEl) return;
  const count = renderUpToCount(n, manifestEvents.length);
  feedEl.innerHTML = '';
  if (count === 0) {
    feedEl.innerHTML = '<div class="replay-empty">PRESS PLAY TO BEGIN</div>';
    return;
  }
  for (let i = 0; i < count; i++) {
    feedEl.appendChild(renderFeedEntry(manifestEvents[i]));
  }
  feedEl.scrollTop = feedEl.scrollHeight;
}

function onScrub(): void {
  if (!scrubber) return;
  stopPlayback();
  currentIndex = parseInt(scrubber.value, 10);
  // Re-render the feed up to the scrubbed position so dragging is visible.
  renderUpTo(currentIndex);
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  updateProgress();
}

// Pacing bounds (ms) for timestamp-based replay, so a long idle gap between
// real events does not freeze the replay and rapid bursts stay watchable.
export const MIN_STEP_DELAY = 30;
export const MAX_STEP_DELAY = 2000;

/**
 * Pure: normalise a raw speed selection (string from the dropdown, or any
 * number) to a positive multiplier; non-positive / non-finite falls back to 1.
 * Exported for unit testing.
 */
export function clampSpeed(raw: string | number | null | undefined): number {
  const v = typeof raw === 'number' ? raw : parseFloat(raw ?? '1');
  return Number.isFinite(v) && v > 0 ? v : 1;
}

function currentSpeed(): number {
  return clampSpeed(speedSelect?.value);
}

/**
 * Pure: delay (ms) before rendering the event at `index`, paced by the real
 * inter-event timestamp gap divided by `speed` and clamped to
 * [MIN_STEP_DELAY, MAX_STEP_DELAY]. For the first event (index<=0), an index at
 * or past the end (index>=len), or a non-finite / non-positive gap, falls back
 * to max(50, 200/speed). Exported (with timestamps passed in) so the clamp,
 * divide-by-speed, and fallback branches are testable without module state.
 */
export function stepDelayFor(
  prevTs: number,
  curTs: number,
  speed: number,
  index: number,
  len: number,
): number {
  const fallback = Math.max(50, 200 / speed);
  if (index <= 0 || index >= len) return fallback;
  const gap = curTs - prevTs;
  if (!Number.isFinite(gap) || gap <= 0) return fallback;
  return Math.min(MAX_STEP_DELAY, Math.max(MIN_STEP_DELAY, gap / speed));
}

/** Delay before rendering event at `index`, paced by real timestamps / speed. */
function stepDelay(index: number): number {
  const speed = currentSpeed();
  const len = manifestEvents.length;
  const prev = index > 0 && index <= len ? new Date(manifestEvents[index - 1].timestamp).getTime() : NaN;
  const cur = index >= 0 && index < len ? new Date(manifestEvents[index].timestamp).getTime() : NaN;
  return stepDelayFor(prev, cur, speed, index, len);
}

function startPlayback(): void {
  if (!state.replaySessionId || manifestEvents.length === 0) return;

  // Clear placeholder when starting from the beginning.
  if (feedEl && currentIndex === 0) {
    feedEl.innerHTML = '';
  }

  // Self-scheduling loop: each tick re-reads speed (via stepDelay) and uses
  // the real inter-event timestamp gap, so changing the dropdown mid-play and
  // idle gaps are both handled correctly.
  const tick = (): void => {
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
    if (currentIndex < totalEvents) {
      playTimer = setTimeout(tick, stepDelay(currentIndex));
    } else {
      stopPlayback();
      update({ replayPlaying: false });
      if (playBtn) playBtn.textContent = '▶ PLAY';
    }
  };

  playTimer = setTimeout(tick, stepDelay(currentIndex));
}

function stopPlayback(): void {
  if (playTimer) {
    clearTimeout(playTimer);
    playTimer = null;
  }
}

function updateProgress(): void {
  if (progressEl) {
    // Append '+' when the backend reported more events than we loaded so the
    // scrubber does not claim to cover the whole session.
    progressEl.textContent = `${currentIndex} / ${totalEvents}${manifestTruncated ? '+' : ''}`;
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
