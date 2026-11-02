// Theme: follow the OS (auto) or force dark / light. The choice is kept in
// localStorage so it applies before the first paint, and in the workspace.
import { Window } from './runtime'
import { api } from './api'

export type ThemeMode = 'auto' | 'dark' | 'light'

const KEY = 'tablume.theme'
const media = window.matchMedia('(prefers-color-scheme: light)')
let mode: ThemeMode = readStored()

function readStored(): ThemeMode {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'dark' || v === 'light' || v === 'auto') return v
  } catch {
    /* storage unavailable */
  }
  return 'auto'
}

export function effectiveTheme(m: ThemeMode = mode): 'dark' | 'light' {
  return m === 'auto' ? (media.matches ? 'light' : 'dark') : m
}

function apply() {
  const t = effectiveTheme()
  document.documentElement.dataset.theme = t
  // Native parts: window background (seen while resizing) and, on Windows,
  // the title bar.
  const ignore = () => {} // not running inside a native window
  if (t === 'light') Window.SetBackgroundColour(255, 255, 255, 255).catch(ignore)
  else Window.SetBackgroundColour(30, 31, 34, 255).catch(ignore)
  if (navigator.userAgent.includes('Windows')) api.SetDarkTitleBar(t === 'dark').catch(ignore)
}

export function getTheme(): ThemeMode {
  return mode
}

export function setTheme(m: ThemeMode) {
  mode = m
  try {
    localStorage.setItem(KEY, m)
  } catch {
    /* ignore */
  }
  apply()
}

export function initTheme() {
  media.addEventListener('change', () => mode === 'auto' && apply())
  apply()
}
