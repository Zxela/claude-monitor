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
  version?: string;
  entrypoint?: string;
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
  thinkingContent?: string;
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
  // Tool result metadata
  durationMs?: number;
  success?: boolean;
  stderr?: string;
  interrupted?: boolean;
  truncated?: boolean;
  // Agent result metadata
  agentDurationMs?: number;
  agentTokens?: number;
  agentToolUseCount?: number;
  agentType?: string;
  // System message metadata
  subtype?: string;
  turnMessageCount?: number;
  hookCount?: number;
  hookInfos?: string;
  level?: string;
  // Session-level metadata
  isMeta?: boolean;
  version?: string;
  entrypoint?: string;
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

export interface TrendBucket {
  date: string;
  cost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  sessionCount: number;
  cacheHitPct: number;
  avgSessionCost: number;
  medianSessionCost: number;
  p95SessionCost: number;
  avgSessionTokens: number;
  outputInputRatio: number;
}

export interface RepoTrend {
  repoId: string;
  repoName: string;
  cost: number;
  tokens: number;
  sessions: number;
}

export interface ModelTrend {
  model: string;
  cost: number;
  tokens: number;
  sessions: number;
}

export interface TrendSummary {
  totalCost: number;
  effectiveTokens: number;
  cacheHitPct: number;
  sessionCount: number;
}

export interface TrendResult {
  window: string;
  buckets: TrendBucket[];
  byRepo: RepoTrend[];
  byModel: ModelTrend[];
  summary: TrendSummary;
}

export interface WsEvent {
  event: 'session_new' | 'session_update' | 'event' | 'update_available';
  // dropped_events uses 'type' instead of 'event' as the discriminator
  type?: 'dropped_events';
  session?: Session;
  data?: Event;
  version?: string;
  url?: string;
  count?: number;
  delta?: number;
}

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
