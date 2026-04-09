// web/src/components/onboarding.ts
const STORAGE_KEY = 'claude-monitor-onboarded';

export function init(): void {
  if (localStorage.getItem(STORAGE_KEY)) return;

  const tip = document.createElement('div');
  tip.className = 'onboarding-tip';
  tip.innerHTML = `
    <div class="onboarding-content">
      <strong>Welcome to Claude Monitor!</strong>
      <p>Press <kbd>?</kbd> for keyboard shortcuts</p>
      <p>Click a session to view its feed</p>
      <p>Use <kbd>1</kbd> <kbd>2</kbd> <kbd>3</kbd> to filter sessions</p>
      <button class="onboarding-dismiss">Got it</button>
    </div>
  `;

  tip.querySelector('.onboarding-dismiss')!.addEventListener('click', () => {
    localStorage.setItem(STORAGE_KEY, '1');
    tip.remove();
  });

  // Auto-dismiss after 15 seconds
  setTimeout(() => {
    if (tip.parentNode) {
      localStorage.setItem(STORAGE_KEY, '1');
      tip.remove();
    }
  }, 15000);

  document.body.appendChild(tip);
}
