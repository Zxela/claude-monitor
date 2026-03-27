// web/src/components/budget-popover.ts
import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import { loadSettings, saveSettings, getSettings, notify } from '../notifications';
import '../styles/views.css';

let popover: HTMLElement | null = null;
let banner: HTMLElement | null = null;
let costStatEl: HTMLElement | null = null;

export function render(gearBtn: HTMLElement, costEl: HTMLElement, bannerMount: HTMLElement): void {
  costStatEl = costEl;

  // Create banner
  banner = document.createElement('div');
  banner.className = 'budget-banner hidden';
  bannerMount.prepend(banner);

  // Load saved threshold
  const saved = localStorage.getItem('budget');
  if (saved) {
    update({ budgetThreshold: parseFloat(saved) });
  }

  loadSettings();

  gearBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    togglePopover(gearBtn);
  });
  gearBtn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      e.stopPropagation();
      togglePopover(gearBtn);
    }
  });

  document.addEventListener('click', () => {
    if (popover) { popover.remove(); popover = null; }
  });

  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('stats') || changed.has('budgetThreshold') || changed.has('budgetDismissed')) {
    checkBudget();
  }
}

function togglePopover(anchor: HTMLElement): void {
  if (popover) {
    popover.remove();
    popover = null;
    return;
  }

  popover = document.createElement('div');
  popover.className = 'budget-popover';
  popover.setAttribute('role', 'dialog');
  popover.setAttribute('aria-modal', 'false');
  popover.setAttribute('aria-label', 'Budget and notification settings');
  popover.addEventListener('click', e => e.stopPropagation());
  popover.innerHTML = `
    <input type="number" step="1" placeholder="Budget threshold (USD)" value="${state.budgetThreshold ?? ''}" />
    <div class="budget-actions">
      <button class="set-btn">Set</button>
      <button class="clear-btn">Clear</button>
    </div>
    <div style="margin-top:8px;font-size:10px;color:var(--text-dim)">
      <label style="display:block;margin:3px 0;cursor:pointer">
        <input type="checkbox" class="notif-budget" ${getSettings().budget ? 'checked' : ''} /> Budget exceeded
      </label>
      <label style="display:block;margin:3px 0;cursor:pointer">
        <input type="checkbox" class="notif-error" ${getSettings().error ? 'checked' : ''} /> Agent errored
      </label>
    </div>
  `;

  const input = popover.querySelector('input')!;
  popover.querySelector('.set-btn')!.addEventListener('click', () => {
    const val = parseFloat(input.value);
    if (!isNaN(val) && val > 0) {
      localStorage.setItem('budget', String(val));
      update({ budgetThreshold: val, budgetDismissed: false });
    }
  });

  popover.querySelector('.clear-btn')!.addEventListener('click', () => {
    localStorage.removeItem('budget');
    update({ budgetThreshold: null, budgetDismissed: false });
    if (popover) { popover.remove(); popover = null; }
  });

  anchor.parentElement!.style.position = 'relative';
  anchor.parentElement!.appendChild(popover);

  popover.querySelector('.notif-budget')?.addEventListener('change', (e) => {
    const s = getSettings();
    s.budget = (e.target as HTMLInputElement).checked;
    saveSettings(s);
  });
  popover.querySelector('.notif-error')?.addEventListener('change', (e) => {
    const s = getSettings();
    s.error = (e.target as HTMLInputElement).checked;
    saveSettings(s);
  });
}

function checkBudget(): void {
  if (!state.budgetThreshold || !costStatEl || !banner) return;

  const total = state.stats?.totalCost ?? 0;

  if (total >= state.budgetThreshold) {
    costStatEl.classList.add('over-budget');
    if (!state.budgetDismissed) {
      banner.className = 'budget-banner';
      banner.innerHTML = `Budget exceeded: $${total.toFixed(0)} / $${state.budgetThreshold}
        <button style="background:none;border:none;color:var(--red);cursor:pointer;font-family:var(--font-mono)" aria-label="Dismiss budget warning">✕</button>`;
      banner.querySelector('button')!.addEventListener('click', () => {
        update({ budgetDismissed: true });
      });
      notify('budget', 'Budget Exceeded', `Total spend $${total.toFixed(0)} exceeds $${state.budgetThreshold}`);
    } else {
      banner.className = 'budget-banner hidden';
    }
  } else {
    costStatEl.classList.remove('over-budget');
    banner.className = 'budget-banner hidden';
  }
}
