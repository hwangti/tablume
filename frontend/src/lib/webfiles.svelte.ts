// Files in the browser build: downloads and uploads stand in for the save /
// open dialogs, and server files (SQLite databases, SSH keys) are picked in
// the app's own browser of the server's files folder (FileBrowser).

export type ServerPick = {
  title: string
  start: string
  // a file name that doesn't exist yet may be typed (a new SQLite database)
  allowNew: boolean
  resolve: (path: string | null) => void
}

export const filePick = $state({ req: null as ServerPick | null })

export function pickServerFile(title: string, start = '', allowNew = false): Promise<string | null> {
  filePick.req?.resolve(null)
  return new Promise((resolve) => {
    filePick.req = {
      title,
      start,
      allowNew,
      resolve: (p) => {
        filePick.req = null
        resolve(p)
      },
    }
  })
}

export function downloadText(name: string, content: string): string {
  const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
  return name
}

export function uploadBytes(accept: string): Promise<{ name: string; bytes: Uint8Array } | null> {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = accept
    input.style.display = 'none'
    document.body.append(input)
    const done = () => input.remove()
    input.addEventListener('cancel', () => (done(), resolve(null)))
    input.addEventListener('change', async () => {
      const f = input.files?.[0]
      done()
      if (!f) return resolve(null)
      try {
        resolve({ name: f.name, bytes: new Uint8Array(await f.arrayBuffer()) })
      } catch (e) {
        reject(e)
      }
    })
    input.click()
  })
}

// A download of the server's /x/export (whole tables, streamed by the app).
export function downloadURL(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = ''
  document.body.append(a)
  a.click()
  a.remove()
}

export function uploadText(accept: string): Promise<{ name: string; content: string } | null> {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = accept
    input.style.display = 'none'
    document.body.append(input)
    const done = () => input.remove()
    input.addEventListener('cancel', () => (done(), resolve(null)))
    input.addEventListener('change', async () => {
      const f = input.files?.[0]
      done()
      if (!f) return resolve(null)
      try {
        resolve({ name: f.name, content: await f.text() })
      } catch (e) {
        reject(e)
      }
    })
    input.click()
  })
}
