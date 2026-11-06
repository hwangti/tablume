<script lang="ts">
  // A copy button over an editor's top-right corner, shown while the
  // pointer is over the editor or it has focus. Copies the selection, or
  // the whole text when nothing is selected. The parent is positioned.
  import type { EditorView } from '@codemirror/view'
  import { t } from '../lib/i18n.svelte'
  import { copyText } from '../lib/clipboard'
  import Icon from './Icon.svelte'

  let { view }: { view: () => EditorView | undefined } = $props()
  let done = $state(false)
  let timer: ReturnType<typeof setTimeout> | undefined

  async function copy() {
    const v = view()
    if (!v) return
    const s = v.state
    const picked = s.selection.ranges.filter((r) => !r.empty).map((r) => s.sliceDoc(r.from, r.to))
    await copyText(picked.length ? picked.join('\n') : s.doc.toString())
    done = true
    clearTimeout(timer)
    timer = setTimeout(() => (done = false), 1200)
  }
</script>

<!-- mousedown is prevented: the editor keeps its focus and selection -->
<button
  class="copy-over"
  class:done
  title={t('복사 — 선택 영역, 없으면 전체')}
  onmousedown={(e) => e.preventDefault()}
  onclick={copy}
>
  <Icon name={done ? 'check' : 'copy'} size={14} />
</button>

<style>
  .copy-over {
    position: absolute;
    /* the same both ways; 10px is the scrollbar's width, so it stays clear */
    top: 10px;
    right: 10px;
    z-index: 5;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--border);
    border-radius: 5px;
    background: var(--bg-elev);
    color: var(--text-dim);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.12s;
  }
  :global(*:hover) > .copy-over,
  :global(*:focus-within) > .copy-over,
  .copy-over.done {
    opacity: 1;
  }
  .copy-over:hover {
    color: var(--text);
    background: var(--bg-hover);
  }
  .copy-over.done {
    color: var(--ok);
  }
</style>
