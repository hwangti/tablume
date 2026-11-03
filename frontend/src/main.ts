import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { initTheme } from './lib/theme'
import { initPlatform } from './lib/platform'

initTheme()

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
