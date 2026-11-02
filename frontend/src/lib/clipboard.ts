export const isMac = navigator.platform.toUpperCase().includes('MAC')
export const modKey = isMac ? '⌘' : 'Ctrl+'

export async function copyText(text: string) {
  await navigator.clipboard.writeText(text)
}
