// CSV for the import dialog (RFC 4180: quoted fields may hold the
// separator, quotes doubled, and line breaks).

/** Decodes a file: UTF-8 (a BOM is dropped), else CP949, as Korean Excel saves. */
export function decodeText(bytes: Uint8Array): string {
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    return new TextDecoder('euc-kr').decode(bytes)
  }
}

/** The separator the first line uses most: tab, semicolon or comma. */
export function guessSeparator(text: string): string {
  const line = text.slice(0, text.search(/\r?\n|$/))
  const count = (c: string) => line.split(c).length - 1
  return ['\t', ';', ','].reduce((a, c) => (count(c) > count(a) ? c : a), ',')
}

export function parseCSV(text: string, sep: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let f = ''
  let quoted = false
  let i = 0
  const endRow = () => {
    row.push(f)
    f = ''
    // a blank line (or the last line break) is not a row
    if (row.length > 1 || row[0] !== '') rows.push(row)
    row = []
  }
  while (i < text.length) {
    const c = text[i]
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          f += '"'
          i++
        } else quoted = false
      } else f += c
    } else if (c === '"' && f === '') quoted = true
    else if (c === sep) {
      row.push(f)
      f = ''
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++
      endRow()
    } else f += c
    i++
  }
  if (f !== '' || row.length) endRow()
  return rows
}
