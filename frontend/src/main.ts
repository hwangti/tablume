import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { initTheme } from './lib/theme'
import { initPlatform } from './lib/platform'
import { installLongPress } from './lib/longpress'
import { closeMenus } from './lib/fit'

initTheme()
installLongPress()

// Escape closes an open pop-up menu, and does nothing else then (a dialog
// under it stays, an edit isn't cancelled). Menus carry data-menu.
addEventListener(
  'keydown',
  (e) => {
    if (e.key !== 'Escape' || !document.querySelector('[data-menu]')) return
    e.preventDefault()
    e.stopPropagation()
    closeMenus()
  },
  true,
)

// iOS zooms into a text field with type under 16px when it gets focus, and
// stays zoomed. maximum-scale=1 stops that; iOS still lets a pinch zoom
// (Android wouldn't, so only there). iPadOS calls itself a Mac.
if (/iP(hone|ad|od)/.test(navigator.userAgent) || (/Mac/.test(navigator.userAgent) && navigator.maxTouchPoints > 1))
  document.querySelector('meta[name=viewport]')?.setAttribute('content', 'width=device-width, initial-scale=1, maximum-scale=1')

// Focus rings only while moving around with the keyboard. When the native
// window first gets focus, WebKit can put focus on the first button (the
// view options), which then showed a ring as if it were selected.
addEventListener('keydown', (e) => e.key === 'Tab' && document.documentElement.classList.add('kbd-nav'), true)
addEventListener('mousedown', () => document.documentElement.classList.remove('kbd-nav'), true)
// ...and don't leave such a button focused (Enter / Space would press it).
const dropButtonFocus = () => {
  const el = document.activeElement
  if (el instanceof HTMLButtonElement && !document.documentElement.classList.contains('kbd-nav')) el.blur()
}
// (the window itself getting focus, not elements inside it)
addEventListener('focus', (e) => e.target === window && setTimeout(dropButtonFocus))
addEventListener('load', () => setTimeout(dropButtonFocus, 300))

// The build (desktop / browser) decides parts of the UI, so it is known first.
initPlatform().finally(() => mount(App, { target: document.getElementById('app')! }))
