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
      <div class="help-row"><span>Table view</span><kbd>t</kbd></div>
      <div class="help-row"><span>Help</span><kbd>?</kbd></div>
      <h3>Replay Controls</h3>
      <div class="help-row"><span>Play / pause</span><kbd>Space</kbd></div>
      <div class="help-row"><span>Restart</span><kbd>R</kbd></div>
      <div class="help-row"><span>Step backward / forward</span><kbd>←→</kbd></div>
    </div>
  `;

  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) { overlay!.remove(); overlay = null; }
  });

  document.body.appendChild(overlay);
}
