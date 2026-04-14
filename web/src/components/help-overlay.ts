// web/src/components/help-overlay.ts
import '../styles/views.css';

let overlay: HTMLElement | null = null;
let previousFocus: HTMLElement | null = null;

function close(): void {
  if (!overlay) return;
  overlay.remove();
  overlay = null;
  previousFocus?.focus();
  previousFocus = null;
}

export function toggle(): void {
  if (overlay) {
    close();
    return;
  }

  previousFocus = document.activeElement as HTMLElement | null;

  overlay = document.createElement('div');
  overlay.className = 'help-overlay';
  overlay.setAttribute('role', 'dialog');
  overlay.setAttribute('aria-modal', 'true');
  overlay.setAttribute('aria-label', 'Keyboard shortcuts');
  overlay.innerHTML = `
    <div class="help-content">
      <div class="help-header">
        <h3>Keyboard Shortcuts</h3>
        <button class="help-close-btn" aria-label="Close" title="Close">&#x2715;</button>
      </div>
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
      <div class="help-row"><span>Table view</span><kbd>t</kbd></div>
      <div class="help-row"><span>Expand subagents</span><kbd>→</kbd></div>
      <div class="help-row"><span>Collapse subagents</span><kbd>←</kbd></div>
      <div class="help-row"><span>Replay: play / pause</span><kbd>Space</kbd></div>
      <div class="help-row"><span>Replay: restart</span><kbd>R</kbd></div>
      <div class="help-row"><span>Help</span><kbd>?</kbd></div>
    </div>
  `;

  const closeBtn = overlay.querySelector('.help-close-btn') as HTMLElement;
  closeBtn.addEventListener('click', close);

  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) close();
  });

  overlay.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      close();
      return;
    }

    // Focus trap: keep Tab within the overlay
    if (e.key === 'Tab') {
      const focusable = overlay!.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  });

  document.body.appendChild(overlay);
  closeBtn.focus();
}
