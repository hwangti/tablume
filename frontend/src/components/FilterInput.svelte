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
  let hl: HTMLDivElement

  // ---- syntax highlighting: a colored copy of the text sits behind the
  // (transparent-text) input and follows its horizontal scroll.
  const KW = new Set(
    ('AND OR NOT IN LIKE ILIKE IS NULL BETWEEN EXISTS TRUE FALSE ASC DESC NULLS FIRST LAST SELECT FROM WHERE ' +
      'CASE WHEN THEN ELSE END AS ESCAPE COLLATE DISTINCT ANY ALL SOME REGEXP RLIKE INTERVAL DATE TIMESTAMP ' +
      'GROUP BY HAVING LIMIT OFFSET JOIN ON UNION')
      .split(' '),
  )
  type Tok = { text: string; cls: string }
  const TOKEN_RE =
    /('(?:[^']|'')*'?)|("(?:[^"]|"")*"?|`[^`]*`?|\[[^\]]*\]?)|(--.*$)|(\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)|([\p{L}_][\p{L}\p{N}_$#]*)|(\s+)|([^])/gu

  function tokenize(src: string): Tok[] {
    const out: Tok[] = []
    const colSet = new Set(columns.map((c) => c.toLowerCase()))
    for (const m of src.matchAll(TOKEN_RE)) {
      const [text, str, quoted, comment, num, word] = m
      let cls = ''
      if (str) cls = 'str'
      else if (quoted) cls = 'col'
      else if (comment) cls = 'cmt'
      else if (num) cls = 'num'
      else if (word) {
        const after = src.slice((m.index ?? 0) + text.length)
        if (KW.has(word.toUpperCase())) cls = 'kw'
        else if (/^\s*\(/.test(after)) cls = 'fn'
        else if (colSet.has(word.toLowerCase())) cls = 'col'
      }
      out.push({ text, cls })
    }
    return out
  }
  let tokens = $derived(tokenize(value))

  function syncScroll() {
    if (hl && input) hl.scrollLeft = input.scrollLeft
  }
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
    requestAnimationFrame(() => (input.setSelectionRange(pos, pos), syncScroll()))
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
  <div class="hl mono" bind:this={hl} aria-hidden="true">{#each tokens as t, i (i)}<span class={t.cls}>{t.text}</span>{/each}</div>
  <input
    class="mono"
    bind:this={input}
    bind:value
    oninput={() => (suggest(), syncScroll())}
    onkeydown={keydown}
    onkeyup={syncScroll}
    onscroll={syncScroll}
    onselect={syncScroll}
    onmouseup={syncScroll}
    onmousemove={(e) => e.buttons && syncScroll()}
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
    font-family: var(--font-mono);
  }
  input {
    position: relative;
    width: 100%;
    height: 24px;
    padding: 3px 27px 3px 1ch;
    border: none;
    border-radius: 0;
    font-size: 12px;
    line-height: 18px;
    background: transparent;
    color: transparent;
    caret-color: var(--text);
  }
  input::selection {
    color: transparent;
    background: color-mix(in srgb, var(--accent) 35%, transparent);
  }
  .hl {
    position: absolute;
    inset: 0;
    padding: 3px 27px 3px 1ch;
    font-size: 12px;
    line-height: 18px;
    white-space: pre;
    overflow: hidden;
    color: var(--syn-ident);
    pointer-events: none;
  }
  .hl .kw {
    color: var(--syn-keyword);
  }
  .hl .str {
    color: var(--syn-string);
  }
  .hl .num {
    color: var(--syn-number);
  }
  .hl .cmt {
    color: var(--syn-comment);
  }
  .hl .fn {
    color: var(--syn-fn);
  }
  .hl .col {
    color: var(--syn-type);
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
