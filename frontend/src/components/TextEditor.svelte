<script lang="ts">
  // Plain text editor for a cell value (CellViewer). Text that looks like
  // JSON gets JSON highlighting and bracket matching.
  import { onMount, untrack } from 'svelte'
  import { EditorState, Compartment } from '@codemirror/state'
  import { EditorView, keymap, lineNumbers, drawSelection, highlightActiveLine, highlightSpecialChars, placeholder as cmPlaceholder } from '@codemirror/view'
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
  import { syntaxHighlighting, bracketMatching, foldGutter, codeFolding, foldKeymap } from '@codemirror/language'
  import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete'
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search'
  import { json } from '@codemirror/lang-json'
  import { highlight, theme } from '../lib/cm'

  let {
    value,
    readonly = false,
    placeholder = '',
    onchange,
    onsubmit,
  }: { value: string; readonly?: boolean; placeholder?: string; onchange?: (v: string) => void; onsubmit?: () => void } = $props()

  let host: HTMLDivElement
  let view: EditorView | undefined
  const lang = new Compartment()
  const empty = new Compartment()

  /** Starts like a JSON object or array (it may not parse while being typed). */
  function looksJson(s: string): boolean {
    return /^\s*[[{]/.test(s)
  }

  const langFor = (s: string) => (looksJson(s) ? [json(), codeFolding(), foldGutter()] : [])
  let isJson = untrack(() => looksJson(value))

  const local = EditorView.theme({
    '&': { height: '100%', backgroundColor: 'var(--bg-input)', fontSize: '12.5px' },
    '.cm-gutters': { backgroundColor: 'var(--bg-input)' },
    '.cm-foldGutter span': { color: 'var(--text-faint)' },
    '.cm-placeholder': { color: 'var(--null)', fontStyle: 'italic' },
  })

  const placeholderExt = (p: string) => (p ? cmPlaceholder(p) : [])

  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          keymap.of([{ key: 'Mod-Enter', run: () => (onsubmit?.(), !!onsubmit) }]),
          lineNumbers(),
          highlightSpecialChars(),
          history(),
          drawSelection(),
          highlightActiveLine(),
          bracketMatching(),
          closeBrackets(),
          highlightSelectionMatches(),
          syntaxHighlighting(highlight),
          keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap, indentWithTab]),
          lang.of(langFor(value)),
          empty.of(placeholderExt(placeholder)),
          theme,
          local,
          EditorView.lineWrapping,
          EditorState.readOnly.of(readonly),
          EditorView.updateListener.of((u) => {
            if (!u.docChanged) return
            const v = u.state.doc.toString()
            // switch highlighting when the text starts / stops looking like JSON
            if (looksJson(v) !== isJson) {
              isJson = !isJson
              u.view.dispatch({ effects: lang.reconfigure(langFor(v)) })
            }
            onchange?.(v)
          }),
        ],
      }),
    })
    if (!readonly) view.focus()
    return () => view?.destroy()
  })

  // Value replaced from outside (JSON 정렬, NULL로 설정).
  $effect(() => {
    const v = value
    if (view && v !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } })
    }
  })

  $effect(() => {
    view?.dispatch({ effects: empty.reconfigure(placeholderExt(placeholder)) })
  })
</script>

<div class="te" bind:this={host}></div>

<style>
  .te {
    height: 420px;
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: hidden;
    resize: vertical;
    user-select: text;
    -webkit-user-select: text;
  }
</style>
