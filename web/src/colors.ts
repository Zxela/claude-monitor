// Shared color constants for canvas rendering — mirrors CSS custom properties.
// Canvas APIs can't read CSS vars, so we maintain this parallel source of truth.
export const COLORS = {
  bg: '#0d1117',
  bgCard: '#161b22',
  bgDeep: '#0f0f1a',
  text: '#c9d1d9',
  textDim: '#8b949e',
  green: '#3fb950',
  yellow: '#d29922',
  red: '#f85149',
  cyan: '#58a6ff',
  purple: '#bc8cff',
  orange: '#f0883e',
  statusThinking: '#d29922',
  statusTool: '#58a6ff',
  statusActive: '#3fb950',
  statusIdle: '#44445a',
  edge: 'rgba(100,100,140,0.3)',
  // Feed entry type colors
  user: '#5588ff',
  assistant: '#33dd99',
  toolUse: '#ddcc44',
  toolResult: '#44cccc',
  hook: '#aa77dd',
  error: '#dd4455',
  system: '#444',
};
