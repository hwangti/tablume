import { Clipboard } from '@wailsio/runtime'
import { isWeb } from './platform'

export const isMac = navigator.platform.toUpperCase().includes('MAC')
export const modKey = isMac ? '⌘' : 'Ctrl+'

export async function copyText(text: string) {
  // (the browser build's server has no clipboard of the browser's)
  if (!isWeb) {
    try {
      await Clipboard.SetText(text)
      return
    } catch {
      /* not running inside Wails (e.g. browser dev) */
    }
  }
  await navigator.clipboard.writeText(text)
}
