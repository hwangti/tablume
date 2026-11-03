import { format, type SqlLanguage } from 'sql-formatter'
import type { EditorView } from '@codemirror/view'

const languages: Record<string, SqlLanguage> = {
  mysql: 'mysql',
  mariadb: 'mariadb',
  postgres: 'postgresql',
  mssql: 'transactsql',
  oracle: 'plsql',
  sqlite: 'sqlite',
}

// Lays out SQL over several lines (keywords, columns, joins, conditions),
// keeping the keyword case as typed.
export function formatSQL(text: string, driver?: string): string {
  return format(text, {
    language: (driver && languages[driver]) || 'sql',
    keywordCase: 'preserve',
    tabWidth: 4,
    linesBetweenQueries: 1,
  })
}

// Reformats the selection, or the whole editor when nothing is selected
// (DataGrip: Reformat Code). Throws when the SQL can't be parsed.
export function formatEditor(view: EditorView, driver?: string): boolean {
  const sel = view.state.selection.main
  const from = sel.empty ? 0 : sel.from
  const to = sel.empty ? view.state.doc.length : sel.to
  const src = view.state.sliceDoc(from, to)
  if (!src.trim()) return false
  const lead = /^\s*/.exec(src)![0]
  const trail = /\s*$/.exec(src)![0]
  const out = lead + formatSQL(src.trim(), driver) + (sel.empty ? (trail.includes('\n') ? '\n' : '') : trail)
  if (out === src) return true
  view.dispatch({
    changes: { from, to, insert: out },
    selection: sel.empty ? { anchor: Math.min(sel.head, from + out.length) } : { anchor: from, head: from + out.length },
    scrollIntoView: true,
    userEvent: 'format',
  })
  return true
}

// 1536 → "1.5 KB"; "" for unknown sizes
export function formatBytes(b: number | undefined): string {
  if (b === undefined || b < 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = b
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return (v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)) + ' ' + units[i]
}
