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
        banner.innerHTML = '';

        const textSpan = document.createElement('span');
        textSpan.className = 'update-banner-text';

        const label = document.createTextNode('Update available: ');
        textSpan.appendChild(label);

        const strong = document.createElement('strong');
        strong.textContent = updateVersion;
        textSpan.appendChild(strong);

        if (updateUrl && updateUrl.startsWith('https://github.com/')) {
          const link = document.createElement('a');
          link.href = updateUrl;
          link.target = '_blank';
          link.rel = 'noopener';
          link.textContent = 'View release';
          textSpan.appendChild(document.createTextNode(' '));
          textSpan.appendChild(link);
        }

        banner.appendChild(textSpan);

        const dismissBtn = document.createElement('button');
        dismissBtn.className = 'update-banner-dismiss';
        dismissBtn.setAttribute('aria-label', 'Dismiss update notification');
        dismissBtn.textContent = '×';
        dismissBtn.addEventListener('click', () => {
          sessionStorage.setItem('update-dismissed', '1');
          update({ updateDismissed: true });
        });
        banner.appendChild(dismissBtn);
      } else {
        banner.style.display = 'none';
      }
    }
  });
}
