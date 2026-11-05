// Long press on a touch screen opens the same menus as a right click.
//
// iOS sends no contextmenu event at all, so the tree, tab, grid and ERD menus
// couldn't be opened there. One listener for the whole page: a finger held
// still for a moment sends a contextmenu event to what's under it, which the
// existing oncontextmenu handlers take as they are (with the mousedown and
// mouseup of button 2 around it, as a right click sends). Where the browser sends
// its own (Android Chrome), that one is used and none is made up.
//
// Text fields keep their own long press (caret, selection, paste).

const HOLD_MS = 500
const SLOP = 10 // px the finger may drift before it counts as a scroll

let timer = 0
let start: { x: number; y: number; id: number } | null = null
let made = false // the event being sent is ours
let swallowUntil = 0 // the click that ends a long press isn't a tap
let firedUntil = 0 // ours went out: the browser's late one is the same press

const editable = (t: EventTarget | null) =>
  t instanceof Element && !!t.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"]')

function cancel() {
  clearTimeout(timer)
  start = null
}

export function installLongPress() {
  addEventListener(
    'pointerdown',
    (e) => {
      cancel()
      if (e.pointerType !== 'touch' || !e.isPrimary || editable(e.target)) return
      start = { x: e.clientX, y: e.clientY, id: e.pointerId }
      timer = window.setTimeout(() => {
        const s = start
        start = null
        if (!s) return
        const el = document.elementFromPoint(s.x, s.y)
        if (!el || editable(el)) return
        swallowUntil = firedUntil = Date.now() + 800
        navigator.vibrate?.(8)
        // As a right click goes: the press first (a grid cell gets selected
        // by it), then the menu, then the release.
        const at = { bubbles: true, cancelable: true, composed: true, view: window, clientX: s.x, clientY: s.y, screenX: s.x, screenY: s.y, button: 2 }
        made = true
        try {
          el.dispatchEvent(new MouseEvent('mousedown', { ...at, buttons: 2 }))
          el.dispatchEvent(new MouseEvent('contextmenu', { ...at, buttons: 2 }))
          el.dispatchEvent(new MouseEvent('mouseup', { ...at, buttons: 0 }))
        } finally {
          made = false
        }
      }, HOLD_MS)
    },
    { capture: true, passive: true },
  )
  addEventListener(
    'pointermove',
    (e) => {
      if (start && e.pointerId === start.id && Math.hypot(e.clientX - start.x, e.clientY - start.y) > SLOP) cancel()
    },
    { capture: true, passive: true },
  )
  for (const t of ['pointerup', 'pointercancel'] as const) addEventListener(t, cancel, { capture: true, passive: true })
  // The browser's own (Android): use it and send none; after ours, it's a
  // second one for the same press.
  addEventListener(
    'contextmenu',
    (e) => {
      if (made) return
      if (start) cancel()
      else if (Date.now() < firedUntil) {
        e.preventDefault()
        e.stopPropagation()
      }
    },
    { capture: true },
  )
  // The mouse events a browser sends when the finger lifts land on the menu
  // that just opened under it: none of them is a tap on it.
  for (const t of ['mousedown', 'mouseup', 'click'] as const)
    addEventListener(
      t,
      (e) => {
        if (made || Date.now() >= swallowUntil) return
        if (t === 'click') swallowUntil = 0
        e.preventDefault()
        e.stopPropagation()
      },
      { capture: true },
    )
}
