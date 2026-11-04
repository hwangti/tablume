import * as App from '../../bindings/tablume/internal/app/app'

// Which build this page runs in: the desktop app (a native window) or the
// browser build (served by the Tablume server, -tags server). Set before
// the app mounts (main.ts).
export let isWeb = false

export async function initPlatform() {
  try {
    isWeb = (await App.Platform()) === 'web'
  } catch {
    /* no backend (vite dev in a plain browser) */
  }
}
