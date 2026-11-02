import { Clipboard } from '@wailsio/runtime'

export const isMac = navigator.platform.toUpperCase().includes('MAC')
export const modKey = isMac ? '⌘' : 'Ctrl+'

export async function copyText(text: string) {
  try {
    await Clipboard.SetText(text)
    return
  } catch {
    /* not running inside Wails (e.g. browser dev) */
  }
  await navigator.clipboard.writeText(text)
}
