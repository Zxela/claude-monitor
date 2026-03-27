export interface Session {
  id: string;
  projectDir: string;
  projectName: string;
  sessionName?: string;
  filePath: string;
  totalCostUSD: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  cacheHitPct: number;
  messageCount: number;
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
  isSubagent?: boolean;
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

export interface SearchResult {
  sessionId: string;
  sessionName: string;
  projectName: string;
  type: string;
  role: string;
  contentText: string;
  toolName?: string;
  timestamp: string;
  messageId?: string;
  costUSD: number;
  isError: boolean;
}

export interface HistoryRow {
  id: string;
  projectName: string;
  sessionName: string;
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  messageCount: number;
  errorCount: number;
  startedAt: string;
  endedAt: string;
  durationSeconds: number;
  model: string;
  cwd: string;
  gitBranch: string;
  taskDescription: string;
  parentId?: string;
}

export interface WsEvent {
  event: 'session_new' | 'message' | 'update_available';
  session?: Session;
  message?: ParsedMessage;
  version?: string;
  url?: string;
}

export interface ParsedMessage {
  type: string;
  role: string;
  contentText: string;
  toolName?: string;
  toolDetail?: string;
  timestamp: string;
  messageId?: string;
  costUSD: number;
  isError: boolean;
  model?: string;
  hookEvent?: string;
  fullContent?: string;  // untruncated content (backend truncates contentText to 200 chars)
  toolUseId?: string;
  forToolUseId?: string;
  isAgent?: boolean;
}
