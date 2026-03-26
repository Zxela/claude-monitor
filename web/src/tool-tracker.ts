// web/src/tool-tracker.ts
// Tracks the last tool used per session to avoid circular imports between
// feed-panel.ts and session-card.ts.

const lastToolBySession = new Map<string, string>();

export function setLastTool(sessionId: string, toolInfo: string): void {
  lastToolBySession.set(sessionId, toolInfo);
}

export function getLastTool(sessionId: string): string | undefined {
  return lastToolBySession.get(sessionId);
}
