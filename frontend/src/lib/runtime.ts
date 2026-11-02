// The few native calls the pages make, on Tauri. Same shape as the Wails
// runtime calls they replaced; outside Tauri (plain browser) they reject.
import { emit, listen } from '@tauri-apps/api/event'
import { getCurrentWebview } from '@tauri-apps/api/webview'
import { getCurrentWindow } from '@tauri-apps/api/window'

export const Events = {
  On(name: string, fn: (ev: { data: unknown; sender: string }) => void) {
    listen(name, (e) => fn({ data: e.payload, sender: '' })).catch(() => {})
  },
  async Emit(name: string, data?: unknown) {
    await emit(name, data)
  },
}

export const Window = {
  async SetTitle(t: string) {
    await getCurrentWindow().setTitle(t)
  },
  async SetBackgroundColour(r: number, g: number, b: number, a: number) {
    await getCurrentWindow().setBackgroundColor([r, g, b, a])
  },
  async SetZoom(z: number) {
    await getCurrentWebview().setZoom(z)
  },
}

/** This window's label: "main", or "tab-<id>" for a tab window. */
export function windowLabel(): string {
  try {
    return getCurrentWindow().label
  } catch {
    return ''
  }
}
