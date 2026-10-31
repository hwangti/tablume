<script lang="ts">
  // Single-line SQL fragment input (WHERE / ORDER BY) with completion of
  // column names and a few keywords.
  let {
    value = $bindable(''),
    columns,
    keywords,
    onsubmit,
    onrevert,
  }: {
    value: string
    columns: string[]
    keywords: string[]
    onsubmit: () => void
    onrevert: () => void
  } = $props()

  let input: HTMLInputElement
  let items = $state<{ text: string; kind: 'col' | 'kw' }[]>([])
  let sel = $state(0)
  let tokenStart = 0

  function token(): { start: number; word: string } {
    const caret = input.selectionStart ?? value.length
    const m = /[\p{L}\p{N}_$#]*$/u.exec(value.slice(0, caret))!
    return { start: caret - m[0].length, word: m[0] }
  }

  function suggest(force = false) {
    const { start, word } = token()
    tokenStart = start
    if (!word && !force) {
      items = []
      return
    }
    const w = word.toLowerCase()
    const cols = columns.filter((c) => c.toLowerCase().startsWith(w) && c.toLowerCase() !== w)
    const kws = keywords.filter((k) => k.toLowerCase().startsWith(w) && k.toLowerCase() !== w)
    items = [...cols.map((text) => ({ text, kind: 'col' as const })), ...kws.map((text) => ({ text, kind: 'kw' as const }))].slice(0, 12)
    sel = 0
  }

  // Replaces the whole word under the caret, including characters after it.
  function accept(i: number) {
    const it = items[i]
    if (!it) return
    const caret = input.selectionStart ?? value.length
    const rest = /^[\p{L}\p{N}_$#]*/u.exec(value.slice(caret))![0]
    const insert = it.kind === 'kw' ? it.text + ' ' : it.text
    value = value.slice(0, tokenStart) + insert + value.slice(caret + rest.length)
    const pos = tokenStart + insert.length
    items = []
    requestAnimationFrame(() => input.setSelectionRange(pos, pos))
  }

  function keydown(e: KeyboardEvent) {
    const open = items.length > 0
    if (open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault()
      sel = (sel + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length
    } else if (open && (e.key === 'Enter' || e.key === 'Tab')) {
      e.preventDefault()
      accept(sel)
    } else if (open && e.key === 'Escape') {
      e.preventDefault()
      items = []
    } else if (e.key === ' ' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      suggest(true)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      onsubmit()
    } else if (e.key === 'Escape') {
      onrevert()
    }
  }
</script>

<div class="fi">
  <input
    class="mono"
    bind:this={input}
    bind:value
    oninput={() => suggest()}
    onkeydown={keydown}
    onblur={() => setTimeout(() => (items = []), 120)}
    spellcheck="false"
    autocomplete="off"
  />
  {#if items.length}
    <div class="list">
      {#each items as it, i (it.kind + it.text)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="item" class:on={i === sel} onmousedown={(e) => (e.preventDefault(), accept(i))}>
          <span class="k {it.kind}">{it.kind === 'col' ? 'col' : 'kw'}</span>
          {it.text}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .fi {
    position: relative;
    width: 100%;
  }
  input {
    width: 100%;
    height: 24px;
    padding: 2px 26px 2px 7px;
    font-size: 12px;
  }
  .list {
    position: absolute;
    top: 26px;
    left: 0;
    z-index: 70;
    min-width: 220px;
    max-height: 260px;
    overflow: auto;
    padding: 3px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
    font-family: var(--font-mono);
    font-size: 12.5px;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 8px;
    border-radius: 4px;
    cursor: pointer;
    white-space: nowrap;
  }
  .item.on {
    background: var(--bg-select);
  }
  .k {
    width: 22px;
    font-size: 10px;
    color: var(--text-faint);
  }
  .k.col {
    color: var(--syn-fn);
  }
  .k.kw {
    color: var(--syn-keyword);
  }
</style>
