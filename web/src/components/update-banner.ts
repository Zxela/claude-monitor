import { state, subscribe, update } from '../state';

export function render(container: HTMLElement): void {
  const banner = document.createElement('div');
  banner.className = 'update-banner';
  banner.style.display = 'none';
  banner.setAttribute('role', 'status');
  banner.setAttribute('aria-label', 'Update available');

  container.prepend(banner);

  subscribe((_state, changed) => {
    if (changed.has('updateVersion') || changed.has('updateDismissed')) {
      const { updateVersion, updateUrl, updateDismissed } = state;

      if (updateVersion && !updateDismissed && !sessionStorage.getItem('update-dismissed')) {
        banner.style.display = 'flex';
        banner.innerHTML = `
          <span class="update-banner-text">
            Update available: <strong>${updateVersion}</strong>
            ${updateUrl ? `<a href="${updateUrl}" target="_blank" rel="noopener">View release</a>` : ''}
          </span>
          <button class="update-banner-dismiss" aria-label="Dismiss update notification">&times;</button>
        `;
        banner.querySelector('.update-banner-dismiss')!.addEventListener('click', () => {
          sessionStorage.setItem('update-dismissed', '1');
          update({ updateDismissed: true });
        });
      } else {
        banner.style.display = 'none';
      }
    }
  });
}
