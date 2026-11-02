<script lang="ts" module>
  import { StateEffect, StateField } from '@codemirror/state'
  import { Decoration, type DecorationSet } from '@codemirror/view'

  // Marks the statement that was just executed until the text changes.
  export const setExecMark = StateEffect.define<{ from: number; to: number } | null>()
  const execMark = StateField.define<DecorationSet>({
    create: () => Decoration.none,
    update(deco, tr) {
      deco = tr.docChanged ? Decoration.none : deco
      for (const e of tr.effects) {
        if (e.is(setExecMark)) {
          deco =
            e.value && e.value.to > e.value.from
              ? Decoration.set([Decoration.mark({ class: 'cm-exec' }).range(e.value.from, e.value.to)])
              : Decoration.none
        }
      }
      return deco
    },
    provide: (f) => EditorView.decorations.from(f),
  })
</script>

<script lang="ts">
  import { onMount } from 'svelte'
  import { EditorState, Compartment, Prec } from '@codemirror/state'
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, highlightSpecialChars, rectangularSelection, crosshairCursor, dropCursor } from '@codemirror/view'
  import { defaultKeymap, history, historyKeymap, indentWithTab, toggleComment } from '@codemirror/commands'
  import { sql, MySQL, MariaSQL, PostgreSQL, MSSQL, PLSQL, SQLite, StandardSQL, type SQLDialect } from '@codemirror/lang-sql'
  import { syntaxHighlighting, HighlightStyle, bracketMatching, indentOnInput } from '@codemirror/language'
  import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap, completionStatus, acceptCompletion } from '@codemirror/autocomplete'
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
  import { tags as t } from '@lezer/highlight'
  import type { Driver } from '../lib/types'
  import { formatEditor } from '../lib/format'
  import { toast, app } from '../lib/state.svelte'
  import { errText } from '../lib/api'

  let {
    value,
    driver,
    schema = {},
    onchange,
    onrun,
    onrunall,
    readonly = false,
    view = $bindable(),
  }: {
    value: string
    driver: Driver | undefined
    schema?: Record<string, string[]>
    onchange: (v: string) => void
    onrun: () => void
    onrunall: () => void
    readonly?: boolean
    view?: EditorView
  } = $props()

  let host: HTMLDivElement
  const lang = new Compartment()

  const dialects: Record<string, SQLDialect> = {
    mysql: MySQL,
    mariadb: MariaSQL,
    postgres: PostgreSQL,
    mssql: MSSQL,
    oracle: PLSQL,
    sqlite: SQLite,
  }

  function langExt() {
    return sql({
      dialect: (driver && dialects[driver]) || StandardSQL,
      schema,
      upperCaseKeywords: false,
    })
  }

  const highlight = HighlightStyle.define([
    { tag: [t.keyword, t.operatorKeyword, t.modifier, t.bool, t.null], color: 'var(--syn-keyword)' },
    { tag: [t.string, t.special(t.string)], color: 'var(--syn-string)' },
    { tag: [t.number], color: 'var(--syn-number)' },
    { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--syn-comment)', fontStyle: 'italic' },
    { tag: [t.typeName, t.standard(t.name)], color: 'var(--syn-type)' },
    { tag: [t.function(t.variableName), t.function(t.name)], color: 'var(--syn-fn)' },
    { tag: [t.operator, t.punctuation, t.bracket], color: 'var(--syn-op)' },
    { tag: [t.special(t.name), t.quote], color: 'var(--syn-fn)' },
  ])

  const theme = EditorView.theme({
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

  // Tab accepts the completion and replaces the whole word under the caret
  // (Enter keeps the rest of the word), like JetBrains IDEs.
  function acceptReplacing(v: EditorView): boolean {
    if (completionStatus(v.state) !== 'active') return false
    if (!acceptCompletion(v)) return false
    const pos = v.state.selection.main.head
    const line = v.state.doc.lineAt(pos)
    const rest = /^[\p{L}\p{N}_$#]*/u.exec(line.text.slice(pos - line.from))![0]
    if (rest) v.dispatch({ changes: { from: pos, to: pos + rest.length } })
    return true
  }

  function reformat(v: EditorView) {
    try {
      formatEditor(v, driver)
    } catch (e) {
      toast('SQL을 정렬할 수 없습니다: ' + errText(e).split('\n')[0], 'error')
    }
  }

  // Toolbar button: same as Ctrl+Alt+L / Cmd+Option+L
  export function format() {
    if (view && !readonly) {
      reformat(view)
      view.focus()
    }
  }

  onMount(() => {
    const runKeys = Prec.highest(
      keymap.of([
        { key: 'Mod-Enter', run: () => (onrun(), true) },
        { key: 'Shift-Mod-Enter', run: () => (onrunall(), true) },
        { key: 'Mod-/', run: toggleComment },
        { key: 'Mod-Alt-l', run: (v) => (readonly || reformat(v), true) },
        { key: 'Tab', run: acceptReplacing },
      ]),
    )
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          runKeys,
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightSpecialChars(),
          history(),
          drawSelection(),
          dropCursor(),
          EditorState.allowMultipleSelections.of(true),
          indentOnInput(),
          bracketMatching(),
          closeBrackets(),
          autocompletion({ activateOnTyping: true }),
          rectangularSelection(),
          crosshairCursor(),
          highlightActiveLine(),
          highlightSelectionMatches(),
          syntaxHighlighting(highlight),
          keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...completionKeymap, indentWithTab]),
          lang.of(langExt()),
          execMark,
          theme,
          EditorState.readOnly.of(readonly),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) onchange(u.state.doc.toString())
          }),
        ],
      }),
    })
    return () => view?.destroy()
  })

  // Reconfigure dialect / completion schema when they change.
  $effect(() => {
    void driver
    void schema
    view?.dispatch({ effects: lang.reconfigure(langExt()) })
  })

  // External value changes (e.g. "open file") replace the document.
  $effect(() => {
    if (view && value !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
    }
  })
</script>

<div class="editor" bind:this={host}></div>

<style>
  .editor {
    height: 100%;
    overflow: hidden;
    user-select: text;
    -webkit-user-select: text;
  }
</style>
