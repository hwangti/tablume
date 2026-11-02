// Look of the CodeMirror editors (SQL editor, cell editor): colors come
// from the theme's CSS variables.
import { HighlightStyle } from '@codemirror/language'
import { EditorView } from '@codemirror/view'
import { tags as t } from '@lezer/highlight'
import { MySQL, MariaSQL, PostgreSQL, MSSQL, PLSQL, SQLite, StandardSQL, type SQLDialect } from '@codemirror/lang-sql'

const dialects: Record<string, SQLDialect> = {
  mysql: MySQL,
  mariadb: MariaSQL,
  postgres: PostgreSQL,
  mssql: MSSQL,
  oracle: PLSQL,
  sqlite: SQLite,
}

export const sqlDialect = (driver: string | undefined): SQLDialect => (driver && dialects[driver]) || StandardSQL

export const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.operatorKeyword, t.modifier, t.bool, t.null], color: 'var(--syn-keyword)' },
  { tag: [t.string, t.special(t.string)], color: 'var(--syn-string)' },
  { tag: [t.number], color: 'var(--syn-number)' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--syn-comment)', fontStyle: 'italic' },
  { tag: [t.typeName, t.standard(t.name)], color: 'var(--syn-type)' },
  { tag: [t.function(t.variableName), t.function(t.name)], color: 'var(--syn-fn)' },
  { tag: [t.operator, t.punctuation, t.bracket], color: 'var(--syn-op)' },
  { tag: [t.special(t.name), t.quote], color: 'var(--syn-fn)' },
  { tag: t.propertyName, color: 'var(--syn-type)' }, // JSON keys
])

export const theme = EditorView.theme({
  '&': { height: '100%', fontSize: '13px', backgroundColor: 'var(--bg)', color: 'var(--syn-ident)' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.55' },
  '.cm-content': { caretColor: 'var(--text)', padding: '6px 0' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--text)', borderLeftWidth: '2px' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'var(--editor-sel) !important' },
  '.cm-activeLine': { backgroundColor: 'var(--editor-line)' },
  '.cm-gutters': { backgroundColor: 'var(--bg)', color: 'var(--text-faint)', border: 'none', paddingRight: '6px' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--editor-line)', color: 'var(--text-dim)' },
  '.cm-exec': { backgroundColor: 'var(--exec-mark)' },
  '.cm-selectionMatch': { backgroundColor: 'color-mix(in srgb, var(--accent) 22%, transparent)' },
  '&.cm-focused .cm-matchingBracket': { backgroundColor: 'color-mix(in srgb, var(--accent) 30%, transparent)', outline: 'none' },
  '.cm-tooltip': { backgroundColor: 'var(--bg-elev)', border: '1px solid var(--border)', borderRadius: '6px', overflow: 'hidden' },
  '.cm-tooltip.cm-tooltip-autocomplete > ul': { fontFamily: 'var(--font-mono)', fontSize: '12.5px' },
  '.cm-tooltip-autocomplete > ul > li': { padding: '2px 8px !important' },
  '.cm-tooltip-autocomplete > ul > li[aria-selected]': { backgroundColor: 'var(--bg-select)', color: 'var(--text)' },
  '.cm-completionDetail': { color: 'var(--text-dim)', fontStyle: 'normal', marginLeft: '8px' },
  '.cm-panels': { backgroundColor: 'var(--bg-panel)', color: 'var(--text)', borderColor: 'var(--border)' },
  '.cm-panel.cm-search input, .cm-panel.cm-search button': { fontSize: '12px' },
  '.cm-textfield': { backgroundColor: 'var(--bg-input)', border: '1px solid var(--border)', borderRadius: '4px' },
  '.cm-button': { backgroundImage: 'none', backgroundColor: 'var(--bg-elev)', border: '1px solid var(--border)', borderRadius: '4px', color: 'var(--text)' },
  '.cm-searchMatch': { backgroundColor: 'color-mix(in srgb, var(--warn) 30%, transparent)' },
})
