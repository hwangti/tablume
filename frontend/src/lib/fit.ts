// Keeps a pop-up menu inside the window. A menu opened at the pointer that
// would run off the bottom opens upward instead (or is pushed up when it
// doesn't fit either way); one that would run off the right moves left (and
// off the left, right).
// Pass the menu's position so a menu re-opened elsewhere is fitted again.
export function fitMenu(node: HTMLElement, _pos?: unknown) {
  const fit = () => {
    node.style.translate = ''
    const r = node.getBoundingClientRect()
    const m = 4
    let dx = 0
    let dy = 0
    if (r.right > innerWidth - m) dx = Math.max(m - r.left, innerWidth - m - r.right)
    else if (r.left < m) dx = m - r.left // (a menu anchored on its right side)
    if (r.bottom > innerHeight - m) {
      dy = r.top - r.height >= m ? -r.height : Math.max(m - r.top, innerHeight - m - r.bottom)
    }
    if (dx || dy) node.style.translate = `${dx}px ${dy}px`
  }
  fit()
  return { update: () => requestAnimationFrame(fit) }
}

// Only one pop-up menu is open at a time: whoever opens one calls
// closeMenus() first, and every component with menus closes its own.
// (Menu toggles stop the click from reaching the window handlers that
// close menus, and right-clicks never reach them.)
const CLOSE = 'tablume:closemenus'

export function closeMenus() {
  window.dispatchEvent(new Event(CLOSE))
}

/** Use in an $effect: runs close whenever another menu opens. */
export function onCloseMenus(close: () => void): () => void {
  window.addEventListener(CLOSE, close)
  return () => window.removeEventListener(CLOSE, close)
}
