<script lang="ts">
  import type { Cell, EditInfo, ResultColumn, StmtResult } from '../lib/types'
  import { copyText, modKey } from '../lib/clipboard'
  import { toast } from '../lib/state.svelte'
  import { cellValue, isEdited, setCell, addRow, toggleDelete, revert } from '../lib/editing.svelte'
  import CellViewer from './CellViewer.svelte'

  let {
    result,
    columns,
    rows,
    hasMore,
    loadingMore,
    onloadmore,
    ensureEdit,
  }: {
    result: StmtResult
    columns: ResultColumn[]
    rows: Cell[][]
    hasMore: boolean
    loadingMore: boolean
    onloadmore: () => void
    // Resolves whether this result can be edited (loads it on first use).
    ensureEdit: () => Promise<EditInfo | null>
  } = $props()

  let editing = $state<{ ri: number; vi: number; c: number; value: string } | null>(null)
  let info = $derived(result.edit?.info ?? null)
  let st = $derived(result.edit)

  function val(ri: number, c: number): Cell {
    return cellValue(result, ri, c)
  }

  // Starts editing a cell; falls back to the read-only viewer when the
  // result or column can't be edited.
  async function startEdit(vi: number, c: number, initial?: string) {
    const ri = order[vi]
    const inf = await ensureEdit()
    if (!inf?.editable) {
      if (initial === undefined) openViewer(vi, c)
      else if (inf) toast('읽기 전용: ' + inf.reason)
      return
    }
    if (!inf.columns[c]) {
      if (initial === undefined) openViewer(vi, c)
      else toast('이 컬럼은 편집할 수 없습니다 (바이너리 또는 계산 컬럼)')
      return
    }
    if (st?.deleted[ri]) return
    const v = val(ri, c)
    if (initial === undefined && v !== null && (v.includes('\n') || v.length > 200)) {
      openViewer(vi, c) // long text: edit in the larger editor
      return
    }
    editing = { ri, vi, c, value: initial ?? v ?? '' }
  }

  function commitEdit(move?: [number, number]) {
    if (!editing) return
    const { ri, vi, c, value } = editing
    editing = null
    setCell(result, ri, c, value)
    scroller.focus({ preventScroll: true })
    if (move) {
      const r = Math.max(0, Math.min(order.length - 1, vi + move[0]))
      const cc = Math.max(0, Math.min(columns.length - 1, c + move[1]))
      anchor = focus = { r, c: cc }
      scrollIntoView(r, cc)
    }
  }

  function cancelEdit() {
    editing = null
    scroller.focus({ preventScroll: true })
  }

  function editorKey(e: KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Enter') {
      e.preventDefault()
      commitEdit([1, 0])
    } else if (e.key === 'Tab') {
      e.preventDefault()
      commitEdit([0, e.shiftKey ? -1 : 1])
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancelEdit()
    }
  }

  function focusInput(node: HTMLInputElement, selectAll: boolean) {
    node.focus({ preventScroll: true })
    if (selectAll) node.select()
    else node.setSelectionRange(node.value.length, node.value.length)
  }

  function selectedRows(): number[] {
    if (!sel) return []
    const out: number[] = []
    for (let v = sel.r1; v <= sel.r2; v++) out.push(order[v])
    return out
  }

  async function setNull() {
    const inf = await ensureEdit()
    if (!inf?.editable || !sel) return
    for (let v = sel.r1; v <= sel.r2; v++)
      for (let c = sel.c1; c <= sel.c2; c++) if (inf.columns[c] && !st?.deleted[order[v]]) setCell(result, order[v], c, null)
  }

  export async function newRow() {
    const inf = await ensureEdit()
    if (!inf?.editable) {
      if (inf) toast('읽기 전용: ' + inf.reason)
      return
    }
    sort = null
    const ri = addRow(result)
    const c = inf.columns.findIndex((x) => x)
    anchor = focus = { r: ri, c: Math.max(0, c) }
    scrollIntoView(ri, Math.max(0, c))
    startEdit(ri, Math.max(0, c))
  }

  export async function deleteRows() {
    const inf = await ensureEdit()
    if (!inf?.editable) {
      if (inf) toast('읽기 전용: ' + inf.reason)
      return
    }
    toggleDelete(result, selectedRows())
  }


  const ROW_H = 24
  const HEAD_H = 28
  const OVERSCAN = 10

  let scroller: HTMLDivElement
  let scrollTop = $state(0)
  let viewH = $state(400)
  let widths = $state<number[]>([])
  let sort = $state<{ col: number; dir: 1 | -1 } | null>(null)
  let anchor = $state<{ r: number; c: number } | null>(null)
  let focus = $state<{ r: number; c: number } | null>(null)
  let menu = $state<{ x: number; y: number } | null>(null)
  let viewing = $state<{ col: string; value: Cell; ri: number; c: number } | null>(null)

  const numericRe =
    /^(UNSIGNED )?(TINYINT|SMALLINT|MEDIUMINT|INT|INTEGER|BIGINT|INT[248]|NUMBER|NUMERIC|DECIMAL|FLOAT[48]?|DOUBLE|REAL|MONEY|SMALLMONEY|BINARY_FLOAT|BINARY_DOUBLE|IBFLOAT|IBDOUBLE)\b/
  let numeric = $derived(columns.map((c) => numericRe.test(c.type)))

  // Reset per-result state when a new result set arrives.
  let lastColumns: ResultColumn[] | null = null
  $effect.pre(() => {
    if (columns !== lastColumns) {
      lastColumns = columns
      widths = columns.map((c, i) => guessWidth(c, i))
      sort = null
      anchor = focus = null
      if (scroller) scroller.scrollTop = 0
    }
  })

  // Display width in "monospace cells": CJK / Hangul count double.
  function textWidth(v: string): number {
    let w = 0
    for (let i = 0; i < v.length && w < 60; i++) {
      const code = v.charCodeAt(i)
      w += code >= 0x1100 && (code <= 0x115f || (code >= 0x2e80 && code <= 0xa4cf) || (code >= 0xac00 && code <= 0xd7a3) || (code >= 0xf900 && code <= 0xfaff) || (code >= 0xff00 && code <= 0xff60)) ? 2 : 1
    }
    return Math.min(w, 60)
  }

  function guessWidth(c: ResultColumn, i: number): number {
    const head = c.name.length * 7.6 + c.type.length * 6.2 + 34
    let len = 4
    for (let r = 0; r < Math.min(rows.length, 200); r++) {
      const v = rows[r][i]
      len = Math.max(len, v === null ? 6 : textWidth(v))
    }
    return Math.round(Math.max(56, Math.min(420, Math.max(head, len * 7.6 + 18))))
  }

  // Row order: identity, or sorted by one column (only loaded rows).
  let order = $derived.by(() => {
    const idx = rows.map((_, i) => i)
    if (!sort) return idx
    const { col, dir } = sort
    const num = numeric[col]
    idx.sort((a, b) => {
      const x = val(a, col)
      const y = val(b, col)
      if (x === y) return 0
      if (x === null) return 1
      if (y === null) return -1
      if (num) {
        const d = Number(x) - Number(y)
        if (!Number.isNaN(d)) return d * dir
      }
      return x.localeCompare(y, 'ko') * dir
    })
    return idx
  })

  let first = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN))
  let last = $derived(Math.min(order.length, Math.ceil((scrollTop + viewH) / ROW_H) + OVERSCAN))
  let visible = $derived(order.slice(first, last).map((ri, k) => ({ ri, vi: first + k })))
  let numW = $derived(Math.max(44, String(rows.length).length * 8 + 20))
  let totalW = $derived(numW + widths.reduce((a, b) => a + b, 0))

  function onScroll() {
    scrollTop = scroller.scrollTop
    if (hasMore && !loadingMore && scroller.scrollTop + scroller.clientHeight > rows.length * ROW_H - ROW_H * 20) {
      onloadmore()
    }
  }

  let sel = $derived.by(() => {
    if (!anchor || !focus) return null
    return {
      r1: Math.min(anchor.r, focus.r),
      r2: Math.max(anchor.r, focus.r),
      c1: Math.min(anchor.c, focus.c),
      c2: Math.max(anchor.c, focus.c),
    }
  })

  function isSel(vi: number, c: number) {
    return !!sel && vi >= sel.r1 && vi <= sel.r2 && c >= sel.c1 && c <= sel.c2
  }

  function cellDown(e: MouseEvent, vi: number, c: number) {
    if (e.button === 2 && isSel(vi, c)) return
    if (e.shiftKey && anchor) focus = { r: vi, c }
    else anchor = focus = { r: vi, c }
    scroller.focus({ preventScroll: true })
  }

  function cellOver(e: MouseEvent, vi: number, c: number) {
    if (e.buttons === 1 && anchor) focus = { r: vi, c }
  }

  function rowDown(e: MouseEvent, vi: number) {
    if (e.shiftKey && anchor) focus = { r: vi, c: columns.length - 1 }
    else {
      anchor = { r: vi, c: 0 }
      focus = { r: vi, c: columns.length - 1 }
    }
    scroller.focus({ preventScroll: true })
  }

  function selectAll() {
    if (!rows.length) return
    anchor = { r: 0, c: 0 }
    focus = { r: order.length - 1, c: columns.length - 1 }
  }

  function scrollIntoView(vi: number, c: number) {
    const top = vi * ROW_H
    if (top < scroller.scrollTop) scroller.scrollTop = top
    else if (top + ROW_H > scroller.scrollTop + scroller.clientHeight - HEAD_H) scroller.scrollTop = top + ROW_H - scroller.clientHeight + HEAD_H + 12
    let left = numW
    for (let i = 0; i < c; i++) left += widths[i]
    const right = left + widths[c]
    if (left - numW < scroller.scrollLeft) scroller.scrollLeft = left - numW
    else if (right > scroller.scrollLeft + scroller.clientWidth) scroller.scrollLeft = right - scroller.clientWidth + 12
  }

  function keydown(e: KeyboardEvent) {
    const mod = e.metaKey || e.ctrlKey
    if (mod && e.key.toLowerCase() === 'c') {
      e.preventDefault()
      copySelection(false)
      return
    }
    if (mod && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      selectAll()
      return
    }
    if (!focus) return
    const page = Math.max(1, Math.floor(scroller.clientHeight / ROW_H) - 2)
    const moves: Record<string, [number, number]> = {
      ArrowUp: [-1, 0],
      ArrowDown: [1, 0],
      ArrowLeft: [0, -1],
      ArrowRight: [0, 1],
      PageUp: [-page, 0],
      PageDown: [page, 0],
      Tab: [0, e.shiftKey ? -1 : 1],
    }
    if (e.key === 'Enter' && e.shiftKey) {
      e.preventDefault()
      openViewer(focus.r, focus.c)
      return
    }
    if (e.key === 'Enter' || e.key === 'F2') {
      e.preventDefault()
      startEdit(focus.r, focus.c)
      return
    }
    if ((e.key === 'Delete' || (e.key === 'Backspace' && mod)) && sel) {
      e.preventDefault()
      deleteRows()
      return
    }
    if (e.key.length === 1 && !mod && !e.altKey) {
      e.preventDefault()
      startEdit(focus.r, focus.c, e.key) // typing replaces the value
      return
    }
    let mv = moves[e.key]
    if (e.key === 'Home') mv = mod ? [-order.length, 0] : [0, -columns.length]
    if (e.key === 'End') mv = mod ? [order.length, 0] : [0, columns.length]
    if (!mv) return
    e.preventDefault()
    const r = Math.max(0, Math.min(order.length - 1, focus.r + mv[0]))
    const c = Math.max(0, Math.min(columns.length - 1, focus.c + mv[1]))
    focus = { r, c }
    if (!e.shiftKey || e.key === 'Tab') anchor = { r, c }
    scrollIntoView(r, c)
  }

  function selectedMatrix(): { cols: number[]; data: Cell[][] } | null {
    if (!sel) return null
    const cols: number[] = []
    for (let c = sel.c1; c <= sel.c2; c++) cols.push(c)
    const data: Cell[][] = []
    for (let v = sel.r1; v <= sel.r2; v++) data.push(cols.map((c) => val(order[v], c)))
    return { cols, data }
  }

  function tsv(v: Cell): string {
    if (v === null) return ''
    return /[\t\n"]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v
  }

  async function copySelection(withHeader: boolean) {
    const m = selectedMatrix()
    if (!m) return
    const lines = m.data.map((r) => r.map(tsv).join('\t'))
    if (withHeader) lines.unshift(m.cols.map((c) => columns[c].name).join('\t'))
    await copyText(lines.join('\n'))
    const n = m.data.length * m.cols.length
    if (n > 1) toast(`${m.data.length}행 × ${m.cols.length}열 복사됨`)
  }

  async function copyInList() {
    const m = selectedMatrix()
    if (!m) return
    const vals = new Set<string>()
    for (const r of m.data)
      for (let k = 0; k < r.length; k++) {
        const v = r[k]
        if (v === null) continue
        vals.add(numeric[m.cols[k]] ? v : "'" + v.replace(/'/g, "''") + "'")
      }
    await copyText('(' + [...vals].join(', ') + ')')
    toast(`IN 목록 복사됨 (${vals.size}개 값)`)
  }

  function openViewer(vi: number, c: number) {
    const ri = order[vi]
    const canEdit = !!info?.editable && !!info.columns[c] && !st?.deleted[ri]
    viewing = { col: columns[c].name, value: val(ri, c), ri, c: canEdit ? c : -1 }
  }

  function headerClick(c: number) {
    if (!sort || sort.col !== c) sort = { col: c, dir: 1 }
    else if (sort.dir === 1) sort = { col: c, dir: -1 }
    else sort = null
    anchor = focus = null
  }

  function startResize(e: MouseEvent, c: number) {
    e.preventDefault()
    e.stopPropagation()
    const x0 = e.clientX
    const w0 = widths[c]
    const move = (ev: MouseEvent) => (widths[c] = Math.max(40, w0 + ev.clientX - x0))
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  function display(v: string): string {
    const s = v.length > 300 ? v.slice(0, 300) + '…' : v
    return s.replace(/\r?\n/g, '↵')
  }

  function contextMenu(e: MouseEvent) {
    e.preventDefault()
    if (!sel) return
    menu = { x: e.clientX, y: e.clientY }
  }
</script>

<svelte:window onclick={() => (menu = null)} />

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class="grid"
  bind:this={scroller}
  bind:clientHeight={viewH}
  onscroll={onScroll}
  onkeydown={keydown}
  oncontextmenu={contextMenu}
  tabindex="0"
  role="grid"
>
  <div class="inner" style:height="{HEAD_H + order.length * ROW_H + (hasMore ? ROW_H : 0)}px" style:width="{totalW}px">
    <div class="head" style:height="{HEAD_H}px" style:width="{totalW}px">
      <div class="num corner" style:width="{numW}px"></div>
      {#each columns as col, c (c)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div class="th" style:width="{widths[c]}px" title="{col.name} {col.type}" onclick={() => headerClick(c)} role="columnheader" tabindex="-1">
          <span class="name">{col.name}</span>
          {#if sort?.col === c}<span class="sort">{sort.dir === 1 ? '▲' : '▼'}</span>{/if}
          <span class="type">{col.type.toLowerCase()}</span>
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span class="resize" onmousedown={(e) => startResize(e, c)}></span>
        </div>
      {/each}
    </div>

    {#each visible as { ri, vi } (ri)}
      <div class="tr" class:alt={vi % 2 === 1} style:top="{HEAD_H + vi * ROW_H}px" style:width="{totalW}px">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="num"
          class:added={st?.added[ri]}
          class:deleted={st?.deleted[ri]}
          style:width="{numW}px"
          onmousedown={(e) => rowDown(e, vi)}
        >
          {st?.added[ri] ? '+' : vi + 1}
        </div>
        {#each columns as _, c (c)}
          {@const v = val(ri, c)}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="td"
            class:sel={isSel(vi, c)}
            class:focus={focus?.r === vi && focus?.c === c}
            class:right={numeric[c]}
            class:edited={isEdited(result, ri, c)}
            class:added={st?.added[ri]}
            class:deleted={st?.deleted[ri]}
            class:editing={editing?.ri === ri && editing?.c === c}
            style:width="{widths[c]}px"
            onmousedown={(e) => cellDown(e, vi, c)}
            onmouseover={(e) => cellOver(e, vi, c)}
            ondblclick={() => startEdit(vi, c)}
            onfocus={() => {}}
          >
            {#if editing?.ri === ri && editing?.c === c}
              <input
                class="cell-input"
                bind:value={editing.value}
                use:focusInput={editing.value.length !== 1}
                onkeydown={editorKey}
                onblur={() => commitEdit()}
                onmousedown={(e) => e.stopPropagation()}
                spellcheck="false"
              />
            {:else if v === null}<span class="null">&lt;null&gt;</span>{:else}{display(v)}{/if}
          </div>
        {/each}
      </div>
    {/each}

    {#if hasMore}
      <div class="more" style:top="{HEAD_H + order.length * ROW_H}px">
        {#if loadingMore}더 불러오는 중…{:else}<button class="link" onclick={onloadmore}>더 불러오기</button>{/if}
      </div>
    {/if}
  </div>

  {#if rows.length === 0}
    <div class="empty">결과 행이 없습니다</div>
  {/if}
</div>

{#if menu}
  <div class="menu" style:left="{menu.x}px" style:top="{menu.y}px">
    <button onclick={() => copySelection(false)}>복사 <kbd>{modKey}C</kbd></button>
    <button onclick={() => copySelection(true)}>헤더 포함 복사</button>
    <button onclick={copyInList}>IN (…) 목록으로 복사</button>
    {#if focus}<button onclick={() => focus && openViewer(focus.r, focus.c)}>값 보기 <kbd>Shift+Enter</kbd></button>{/if}
    <button onclick={selectAll}>전체 선택</button>
    {#if info?.editable}
      <hr />
      {#if focus}<button onclick={() => focus && startEdit(focus.r, focus.c)}>편집 <kbd>Enter</kbd></button>{/if}
      <button onclick={setNull}>NULL로 설정</button>
      <button onclick={newRow}>행 추가</button>
      <button onclick={deleteRows}>행 삭제 / 복원 <kbd>Delete</kbd></button>
      <button onclick={() => revert(result, selectedRows())}>선택 행 변경 되돌리기</button>
    {/if}
  </div>
{/if}

{#if viewing}
  <CellViewer
    column={viewing.col}
    value={viewing.value}
    editable={viewing.c >= 0}
    onsave={(v) => viewing && setCell(result, viewing.ri, viewing.c, v)}
    onclose={() => (viewing = null)}
  />
{/if}

<style>
  .grid {
    position: relative;
    height: 100%;
    overflow: auto;
    outline: none;
    font-family: var(--font-mono);
    font-variant-ligatures: none;
    font-size: 12.5px;
    background: var(--bg);
  }

  .inner {
    position: relative;
    min-width: 100%;
  }

  .head {
    position: sticky;
    top: 0;
    z-index: 2;
    display: flex;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }

  .th {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 8px;
    border-right: 1px solid var(--grid-line);
    cursor: pointer;
    overflow: hidden;
    white-space: nowrap;
    font-family: var(--font-ui);
    flex-shrink: 0;
  }

  .th:hover {
    background: var(--bg-hover);
  }

  .th .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .th .type {
    color: var(--text-faint);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .th .sort {
    font-size: 9px;
    color: var(--accent);
  }

  .resize {
    position: absolute;
    right: 0;
    top: 0;
    width: 6px;
    height: 100%;
    cursor: col-resize;
  }

  .tr {
    position: absolute;
    left: 0;
    display: flex;
    height: 24px;
  }

  .tr.alt {
    background: var(--grid-alt);
  }

  .num {
    position: sticky;
    left: 0;
    z-index: 1;
    flex-shrink: 0;
    padding: 0 8px;
    text-align: right;
    line-height: 24px;
    color: var(--text-faint);
    background: var(--bg-panel);
    border-right: 1px solid var(--border);
    font-size: 11px;
    cursor: default;
  }

  .corner {
    z-index: 3;
    line-height: 28px;
  }

  .td {
    flex-shrink: 0;
    padding: 0 8px;
    line-height: 24px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    border-right: 1px solid var(--grid-line);
    border-bottom: 1px solid var(--grid-line);
    cursor: default;
  }

  .td.right {
    text-align: right;
  }

  .td.sel {
    background: var(--bg-select);
  }

  .td.focus {
    box-shadow: inset 0 0 0 1px var(--accent);
  }

  .null {
    color: var(--null);
    font-style: italic;
  }

  .td.edited {
    background: color-mix(in srgb, var(--warn) 22%, transparent);
  }

  .td.added,
  .num.added {
    background: color-mix(in srgb, var(--ok) 14%, transparent);
  }

  .num.added {
    color: var(--ok);
    font-weight: 600;
  }

  .td.deleted,
  .num.deleted {
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    text-decoration: line-through;
    color: var(--text-dim);
  }

  .td.editing {
    padding: 0;
    overflow: visible;
  }

  .cell-input {
    width: 100%;
    height: 100%;
    padding: 0 7px;
    border: 2px solid var(--accent);
    border-radius: 0;
    font: inherit;
    background: var(--bg-input);
  }

  .more {
    position: absolute;
    left: 0;
    right: 0;
    height: 24px;
    padding-left: 60px;
    line-height: 24px;
    color: var(--text-dim);
    font-family: var(--font-ui);
  }

  .link {
    border: none;
    background: none;
    color: var(--accent);
    cursor: pointer;
    padding: 0;
  }

  .empty {
    position: absolute;
    top: 60px;
    width: 100%;
    text-align: center;
    color: var(--text-dim);
    font-family: var(--font-ui);
  }

  .menu {
    position: fixed;
    z-index: 100;
    display: flex;
    flex-direction: column;
    min-width: 200px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }

  .menu button {
    display: flex;
    justify-content: space-between;
    padding: 5px 10px;
    border: none;
    border-radius: 4px;
    background: none;
    text-align: left;
    cursor: pointer;
  }

  .menu button:hover {
    background: var(--bg-select);
  }

  .menu hr {
    width: 100%;
    margin: 4px 0;
    border: none;
    border-top: 1px solid var(--border);
  }

  kbd {
    color: var(--text-dim);
    font-family: var(--font-ui);
    font-size: 11px;
  }
</style>
