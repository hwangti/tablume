// Keeps a pop-up menu inside the window. A menu opened at the pointer that
// would run off the bottom opens upward instead (or is pushed up when it
// doesn't fit either way); one that would run off the right moves left.
// Pass the menu's position so a menu re-opened elsewhere is fitted again.
export function fitMenu(node: HTMLElement, _pos?: unknown) {
  const fit = () => {
    node.style.translate = ''
    const r = node.getBoundingClientRect()
    const m = 4
    let dx = 0
    let dy = 0
    if (r.right > innerWidth - m) dx = Math.max(m - r.left, innerWidth - m - r.right)
    if (r.bottom > innerHeight - m) {
      dy = r.top - r.height >= m ? -r.height : Math.max(m - r.top, innerHeight - m - r.bottom)
    }
    if (dx || dy) node.style.translate = `${dx}px ${dy}px`
  }
  fit()
  return { update: () => requestAnimationFrame(fit) }
}
