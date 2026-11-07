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
  import CopyOverlay from './CopyOverlay.svelte'
  import { t } from '../lib/i18n.svelte'
  import { onMount } from 'svelte'
  import { EditorState, Compartment, Prec } from '@codemirror/state'
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, highlightSpecialChars, rectangularSelection, crosshairCursor, dropCursor } from '@codemirror/view'
  import { defaultKeymap, history, historyKeymap, indentWithTab, toggleComment } from '@codemirror/commands'
  import { sql } from '@codemirror/lang-sql'
  import { syntaxHighlighting, bracketMatching, indentOnInput } from '@codemirror/language'
  import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap, completionStatus, acceptCompletion } from '@codemirror/autocomplete'
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
  import { highlight, theme, sqlDialect } from '../lib/cm'
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

  function langExt() {
    return sql({
      dialect: sqlDialect(driver),
      schema,
      upperCaseKeywords: false,
    })
  }

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
      toast(t('SQL을 정렬할 수 없습니다') + ': ' + errText(e).split('\n')[0], 'error')
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

<div class="editor" bind:this={host}><CopyOverlay view={() => view} /></div>

<style>
  .editor {
    position: relative;
    height: 100%;
    overflow: hidden;
    user-select: text;
    -webkit-user-select: text;
  }
</style>
