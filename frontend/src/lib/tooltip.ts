// Our own tooltips for every `title` attribute: the webview's native ones
// appear only after the OS delay (about a second on macOS) and can't be
// styled. Titles are moved to data-tip as soon as they are set, so the
// native tooltip never shows, and a small panel follows the pointer.

const SHOW_MS = 300 // first tooltip
const NEXT_MS = 60 // moving to the next element while one is showing

let tipEl: HTMLDivElement | null = null
let current: HTMLElement | null = null
let timer: ReturnType<typeof setTimeout> | undefined
let lastHidden = 0
let x = 0
let y = 0

function adopt(el: Element) {
  const t = el.getAttribute('title')
  if (t === null) return
  el.setAttribute('data-tip', t)
  el.removeAttribute('title')
}

function hide() {
  clearTimeout(timer)
  if (tipEl?.style.display === 'block') lastHidden = Date.now()
  if (tipEl) tipEl.style.display = 'none'
  current = null
}

function show() {
  if (!current || !current.isConnected) return
  const text = current.getAttribute('data-tip')
  if (!text) return
  tipEl ??= Object.assign(document.body.appendChild(document.createElement('div')), { className: 'app-tip' })
  tipEl.textContent = text
  tipEl.style.display = 'block'
  const w = tipEl.offsetWidth
  const h = tipEl.offsetHeight
  let left = x + 10
  let top = y + 18
  if (left + w > window.innerWidth - 6) left = Math.max(6, window.innerWidth - w - 6)
  if (top + h > window.innerHeight - 6) top = Math.max(6, y - h - 8)
  tipEl.style.left = left + 'px'
  tipEl.style.top = top + 'px'
}

export function installTooltips() {
  document.querySelectorAll('[title]').forEach(adopt)
  new MutationObserver((list) => {
    for (const m of list) {
      if (m.type === 'attributes') adopt(m.target as Element)
      else
        m.addedNodes.forEach((n) => {
          if (n instanceof Element) {
            adopt(n)
            n.querySelectorAll('[title]').forEach(adopt)
          }
        })
    }
  }).observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['title'] })

  document.addEventListener(
    'mouseover',
    (e) => {
      const el = (e.target as Element).closest?.('[data-tip]') as HTMLElement | null
      if (el === current) return
      const wasShowing = tipEl?.style.display === 'block' || Date.now() - lastHidden < 300
      hide()
      if (!el || !el.getAttribute('data-tip')) return
      current = el
      timer = setTimeout(show, wasShowing ? NEXT_MS : SHOW_MS)
    },
    true,
  )
  document.addEventListener(
    'mousemove',
    (e) => {
      x = e.clientX
      y = e.clientY
    },
    { capture: true, passive: true },
  )
  document.addEventListener('mouseleave', hide)
  for (const ev of ['mousedown', 'wheel', 'keydown', 'blur'])
    window.addEventListener(ev, () => {
      hide()
      lastHidden = 0
    }, { capture: true, passive: true })
}
