// web/src/components/help-overlay.ts
import '../styles/views.css';

let overlay: HTMLElement | null = null;

export function toggle(): void {
  if (overlay) {
    overlay.remove();
    overlay = null;
    return;
  }

  overlay = document.createElement('div');
  overlay.className = 'help-overlay';
  overlay.setAttribute('role', 'dialog');
  overlay.setAttribute('aria-modal', 'true');
  overlay.setAttribute('aria-label', 'Keyboard shortcuts');
  overlay.innerHTML = `
    <div class="help-content">
      <h3>Keyboard Shortcuts</h3>
      <div class="help-row"><span>Focus search</span><kbd>/</kbd></div>
      <div class="help-row"><span>Clear / deselect</span><kbd>Esc</kbd></div>
      <div class="help-row"><span>Navigate sessions</span><kbd>↑↓</kbd></div>
      <div class="help-row"><span>Select session</span><kbd>Enter</kbd></div>
      <div class="help-row"><span>Active filter</span><kbd>1</kbd></div>
      <div class="help-row"><span>Recent filter</span><kbd>2</kbd></div>
      <div class="help-row"><span>All filter</span><kbd>3</kbd></div>
      <div class="help-row"><span>Graph view</span><kbd>g</kbd></div>
      <div class="help-row"><span>History view</span><kbd>h</kbd></div>
      <div class="help-row"><span>Analytics view</span><kbd>a</kbd></div>
      <div class="help-row"><span>Help</span><kbd>?</kbd></div>
    </div>
  `;

  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) { overlay!.remove(); overlay = null; }
  });
  overlay.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { overlay!.remove(); overlay = null; }
  });

  document.body.appendChild(overlay);
}
