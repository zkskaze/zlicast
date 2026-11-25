export function focusDialog(node, close) {
  const prior = document.activeElement;
  const interactive = () =>
    Array.from(
      node.querySelectorAll(
        'button:not([disabled]),input:not([disabled]),select:not([disabled]),summary,a[href]'
      )
    ).filter((el) => el.getClientRects().length);
  (node.querySelector('input:not([disabled])') || interactive()[0])?.focus();
  function key(e) {
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
      return;
    }
    if (e.key !== 'Tab') return;
    const elements = interactive(),
      first = elements[0],
      last = elements.at(-1);
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last?.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first?.focus();
    }
  }
  node.addEventListener('keydown', key);
  return {
    destroy() {
      node.removeEventListener('keydown', key);
      prior?.focus();
    }
  };
}
