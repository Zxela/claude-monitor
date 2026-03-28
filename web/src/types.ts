export interface Session {
  id: string;
  repoId?: string;
  sessionName?: string;
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  messageCount: number;
  eventCount: number;
  lastActive: string;
  isActive: boolean;
  startedAt: string;
  status: 'idle' | 'thinking' | 'tool_use' | 'waiting';
  parentId?: string;
  children?: string[];
  cwd?: string;
  gitBranch?: string;
  model?: string;
  costRate: number;
  errorCount: number;
  taskDescription: string;
}

export interface GroupedSessions {
  active: Session[];
  lastHour: Session[];
  today: Session[];
  yesterday: Session[];
  thisWeek: Session[];
  older: Session[];
}

export interface Event {
  id: number;
  sessionId: string;
  uuid?: string;
  messageId?: string;
  type: string;
  role: string;
  contentPreview: string;
  fullContent?: string;
  toolName?: string;
  toolDetail?: string;
  costUSD: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  model?: string;
  isError: boolean;
  stopReason?: string;
  hookEvent?: string;
  hookName?: string;
  toolUseId?: string;
  forToolUseId?: string;
  isAgent?: boolean;
  timestamp: string;
}

export interface Stats {
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  sessionCount: number;
  activeSessions: number;
  cacheHitPct: number;
  costRate: number;
  costByModel: Record<string, number>;
  costByRepo: Record<string, number>;
}

export interface WsEvent {
  event: 'session_new' | 'session_update' | 'event' | 'update_available';
  session?: Session;
  data?: Event;
  version?: string;
  url?: string;
}

// Legacy alias — frontend components still reference ParsedMessage
// TODO: migrate components to use Event directly
export type ParsedMessage = Event;

export interface SearchResult {
  sessionId: string;
  sessionName: string;
  repoName: string;
  type: string;
  role: string;
  contentPreview: string;
  toolName?: string;
  timestamp: string;
  messageId?: string;
  costUSD: number;
  isError: boolean;
}

export interface StorageInfo {
  totalSizeBytes: number;
  hotContentBytes: number;
  warmContentBytes: number;
  eventCount: number;
}

export interface RepoEntry {
  id: string;
  name: string;
  url?: string;
  totalCost: number;
}
