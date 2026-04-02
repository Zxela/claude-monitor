import { Chart, LineController, BarController, DoughnutController, LineElement, BarElement, ArcElement, PointElement, LinearScale, CategoryScale, Tooltip, Legend, Filler } from 'chart.js';

// Register only the components we use (tree-shaking)
Chart.register(
  LineController, BarController, DoughnutController,
  LineElement, BarElement, ArcElement, PointElement,
  LinearScale, CategoryScale,
  Tooltip, Legend, Filler
);

export const COLORS = {
  green: '#4ae68a',
  blue: '#6ab4ff',
  orange: '#ffa64a',
  purple: '#c49aff',
  red: '#ff6b6b',
  yellow: '#ffd166',
  gray: '#666',
  gridLine: 'rgba(255,255,255,0.06)',
  text: '#aaa',
};

export const DARK_THEME = {
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 300 },
  plugins: {
    legend: {
      labels: { color: COLORS.text, font: { family: 'monospace', size: 11 } },
    },
    tooltip: {
      backgroundColor: '#1e1e3a',
      borderColor: '#3a3a5a',
      borderWidth: 1,
      titleFont: { family: 'monospace', size: 12 },
      bodyFont: { family: 'monospace', size: 11 },
      titleColor: '#e0e0e0',
      bodyColor: '#ccc',
    },
  },
  scales: {
    x: {
      ticks: { color: COLORS.text, font: { family: 'monospace', size: 10 } },
      grid: { color: COLORS.gridLine },
    },
    y: {
      ticks: { color: COLORS.text, font: { family: 'monospace', size: 10 } },
      grid: { color: COLORS.gridLine },
    },
  },
} as const;

export { Chart };
