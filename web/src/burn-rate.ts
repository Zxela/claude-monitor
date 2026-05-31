import { state } from './state';

export interface BurnRateSample {
  timestamp: number;
  costRate: number;
  tokenRate: number;
  totalCost: number;
}

const CAPACITY = 120;
const INTERVAL_MS = 5000;

let samples: BurnRateSample[] = [];
let timer: ReturnType<typeof setInterval> | null = null;
let prevTokenTotal = 0;
let prevTimestamp = 0;

function getActiveSessionTotals(): { costRate: number; tokenTotal: number; totalCost: number } {
  let costRate = 0;
  let tokenTotal = 0;
  let totalCost = 0;
  for (const sess of state.sessions.values()) {
    totalCost += sess.totalCost;
    if (sess.isActive) {
      costRate += sess.costRate;
    }
    tokenTotal +=
      sess.inputTokens + sess.cacheReadTokens + sess.cacheCreationTokens + sess.outputTokens;
  }
  return { costRate, tokenTotal, totalCost };
}

function sample(): void {
  const now = Date.now();
  const totals = getActiveSessionTotals();
  const { tokenTotal, totalCost } = totals;
  // Trace the same server-recomputed cost rate the panel header and topbar use,
  // so the sparkline never tells a different story. Fall back to the client sum
  // only when the server stats are not yet available.
  const apiRate = state.stats?.costRate;
  const costRate = apiRate != null && apiRate > 0 ? apiRate : totals.costRate;

  let tokenRate = 0;
  if (prevTimestamp > 0 && now > prevTimestamp) {
    const deltaTokens = tokenTotal - prevTokenTotal;
    const deltaMinutes = (now - prevTimestamp) / 60000;
    if (deltaMinutes > 0 && deltaTokens >= 0) {
      tokenRate = deltaTokens / deltaMinutes;
    }
  }

  prevTokenTotal = tokenTotal;
  prevTimestamp = now;

  samples.push({ timestamp: now, costRate, tokenRate, totalCost });
  if (samples.length > CAPACITY) {
    samples = samples.slice(samples.length - CAPACITY);
  }
}

export function startSampling(): void {
  if (timer) return;
  sample();
  timer = setInterval(sample, INTERVAL_MS);
}

export function getSamples(): BurnRateSample[] {
  return samples;
}

export function getCurrentRate(): number {
  // Single source of truth: prefer the server's freshly-recomputed cost rate
  // (the same value the topbar $/MIN renders) so the burn-rate panel header,
  // PROJECTED TODAY, and depletion never diverge from the topbar. Fall back to
  // the local sampler only when stats are not yet available.
  const apiRate = state.stats?.costRate;
  if (apiRate != null && apiRate > 0) return apiRate;
  if (samples.length === 0) return 0;
  return samples[samples.length - 1].costRate;
}

export function getTokenRate(): number {
  if (samples.length === 0) return 0;
  return samples[samples.length - 1].tokenRate;
}

export function getProjectedDailyCost(currentTotalCost: number): number {
  const rate = getCurrentRate();
  if (rate <= 0) return currentTotalCost;
  const now = new Date();
  const endOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
  const remainingMinutes = (endOfDay.getTime() - now.getTime()) / 60000;
  return currentTotalCost + rate * remainingMinutes;
}

export function getDepletionMinutes(budget: number, spent: number): number | null {
  const rate = getCurrentRate();
  if (rate <= 0 || budget <= spent) return null;
  return (budget - spent) / rate;
}
