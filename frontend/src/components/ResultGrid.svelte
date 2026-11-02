<script module lang="ts">
  // View state per result (scroll, column widths, sort), so switching to
  // another result tab or the log and back keeps it.
  type Pos = { r: number; c: number } | null
  type View = { columns: unknown; top: number; left: number; widths: number[]; sort: { col: number; dir: 1 | -1 } | null; anchor: Pos; focus: Pos }
  const views = new WeakMap<object, View>()
  // Column order and hidden columns, by column name: per table for table
  // tabs (survives reloads), per result otherwise.
  type Layout = { order: string[]; hidden: string[] }
  const layouts = new Map<string, Layout>()
  const resultLayouts = new WeakMap<object, Layout>()
</script>

<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import type { Cell, EditInfo, ResultColumn, StmtResult } from '../lib/types'
  import { copyText, modKey } from '../lib/clipboard'
  import { app, toast, saveWorkspaceSoon } from '../lib/state.svelte'
  import { cellValue, isEdited, setCell, addRow, toggleDelete, revert } from '../lib/editing.svelte'
  import CellViewer from './CellViewer.svelte'
  import Icon from './Icon.svelte'

  let {
    result,
    columns,
    rows,
    hasMore,
    loadingMore,
    onloadmore,
    canLoadPrev = false,
    loadingPrev = false,
    onloadprev,
    ensureEdit,
    serverSort = null,
    onsort,
    rowBase = 0,
    layoutKey = '',
  }: {
    result: StmtResult
    columns: ResultColumn[]
    rows: Cell[][]
    hasMore: boolean
    loadingMore: boolean
    onloadmore: () => void
    // Paged results whose window doesn't start at row 1 load the previous
    // page when scrolled to the top.
    canLoadPrev?: boolean
    loadingPrev?: boolean
    onloadprev?: () => void
    // Resolves whether this result can be edited (loads it on first use).
    ensureEdit: () => Promise<EditInfo | null>
    // When onsort is given (table data tabs), header clicks sort on the
    // server and serverSort is the indicator; otherwise only the loaded
    // rows are sorted locally.
    serverSort?: { col: number; dir: 1 | -1 } | null
    onsort?: (col: number) => void
    rowBase?: number // number of the first row (paged table data)
    layoutKey?: string // keeps column order / hidden columns across reloads (table tabs)
  } = $props()

  let editing = $state<{ ri: number; vi: number; c: number; value: string } | null>(null)
  let info = $derived(result.edit?.info ?? null)
  let st = $derived(result.edit)

  // Existing rows need a key to be changed; rows added here only need the
  // table (insertable), so they stay editable even without a primary key.
  function rowEditable(inf: EditInfo | null | undefined, ri: number): boolean {
    return !!inf && (inf.editable || (!!inf.insertable && !!result.edit?.added[ri]))
  }

  function val(ri: number, c: number): Cell {
    return cellValue(result, ri, c)
  }

  // Starts editing a cell; falls back to the read-only viewer when the
  // result or column can't be edited.
  async function startEdit(vi: number, p: number, initial?: string) {
    const ri = order[vi]
    const c = vc[p]
    const inf = await ensureEdit()
    if (!inf || !rowEditable(inf, ri)) {
      if (initial === undefined) openViewer(vi, p)
      else if (inf) toast('읽기 전용: ' + inf.reason)
      return
    }
    if (!inf.columns[c]) {
      if (initial === undefined) openViewer(vi, p)
      else toast('이 컬럼은 편집할 수 없습니다 (바이너리 또는 계산 컬럼)')
      return
    }
    if (st?.deleted[ri]) return
    const v = val(ri, c)
    if (initial === undefined && v !== null && (v.includes('\n') || v.length > 200)) {
      openViewer(vi, p) // long text: edit in the larger editor
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
      const cc = Math.max(0, Math.min(vc.length - 1, vc.indexOf(c) + move[1]))
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
    } else if (e.key === 'F2') {
      e.preventDefault()
      editInViewer() // F2 again: continue in the large editor
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
    if (!inf || !sel) return
    for (let v = sel.r1; v <= sel.r2; v++)
      for (const c of vc.slice(sel.c1, sel.c2 + 1))
        if (inf.columns[c] && rowEditable(inf, order[v]) && !st?.deleted[order[v]]) setCell(result, order[v], c, null)
  }

  export async function newRow() {
    const inf = await ensureEdit()
    if (!inf?.editable && !inf?.insertable) {
      if (inf) toast('읽기 전용: ' + inf.reason)
      return
    }
    const ri = addRow(result)
    const vi = order.indexOf(ri)
    const c = Math.max(0, vc.findIndex((i) => inf.columns[i]))
    anchor = focus = { r: vi, c }
    scrollIntoView(vi, c)
    startEdit(vi, c)
  }

  export async function deleteRows() {
    const inf = await ensureEdit()
    const rs = selectedRows()
    if (!inf || !rs.every((ri) => rowEditable(inf, ri))) {
      if (inf) toast('읽기 전용: ' + inf.reason)
      return
    }
    toggleDelete(result, rs)
  }


  // Zoom (Ctrl + wheel / Ctrl +, -, 0) scales rows, fonts and widths;
  // widths[] stay in 100% units so resizing survives zoom changes.
  let z = $derived(app.gridZoom)
  let ROW_H = $derived(Math.round(24 * z))
  let HEAD_H = $derived(Math.round(28 * z))
  // The "load previous rows" bar takes a row of its own above the data.
  let TOP_PAD = $derived(canLoadPrev || loadingPrev ? ROW_H : 0)
  let BODY_T = $derived(HEAD_H + TOP_PAD)
  const OVERSCAN = 10

  let scroller: HTMLDivElement
  let scrollTop = $state(0)
  let viewH = $state(400)
  let viewW = $state(800)
  let scrollLeft = $state(0)
  let widths = $state<number[]>([])
  let sort = $state<{ col: number; dir: 1 | -1 } | null>(null)
  let anchor = $state<{ r: number; c: number } | null>(null)
  let focus = $state<{ r: number; c: number } | null>(null)
  let menu = $state<{ x: number; y: number } | null>(null)
  let viewing = $state<{ col: string; value: Cell; ri: number; c: number } | null>(null)

  const numericRe =
    /^(UNSIGNED )?(TINYINT|SMALLINT|MEDIUMINT|INT|INTEGER|BIGINT|INT[248]|NUMBER|NUMERIC|DECIMAL|FLOAT[48]?|DOUBLE|REAL|MONEY|SMALLMONEY|BINARY_FLOAT|BINARY_DOUBLE|IBFLOAT|IBDOUBLE)\b/
  let numeric = $derived(columns.map((c) => numericRe.test(c.type)))
  const dateRe = /^(DATE|TIME|DATETIME|TIMESTAMP|SMALLDATETIME|YEAR|INTERVAL)/
  const boolRe = /^(BOOL|BIT$)/
  // Value colour per column: numbers, dates and booleans stand out from text.
  let kind = $derived(
    columns.map((c, i) => (numeric[i] ? 'num' : dateRe.test(c.type) ? 'date' : boolRe.test(c.type) ? 'bool' : '')),
  )

  // ---- column layout: vc[p] is the result column shown at position p
  let orderNames = $state<string[]>([])
  let hiddenNames = $state<string[]>([])
  let vc = $derived.by(() => {
    const idx = new Map(orderNames.map((n, i) => [n, i]))
    const all = columns.map((_, i) => i)
    all.sort((a, b) => (idx.get(columns[a].name) ?? 1e6 + a) - (idx.get(columns[b].name) ?? 1e6 + b))
    const hide = new Set(hiddenNames)
    const shown = all.filter((i) => !hide.has(columns[i].name))
    return shown.length ? shown : all.slice(0, 1)
  })
  let hiddenCount = $derived(columns.length - vc.length)

  function loadLayout() {
    const l = layoutKey ? layouts.get(layoutKey) : resultLayouts.get(result)
    orderNames = l?.order ?? []
    hiddenNames = l?.hidden ?? []
  }
  function storeLayout() {
    const l = { order: $state.snapshot(orderNames), hidden: $state.snapshot(hiddenNames) }
    if (layoutKey) layouts.set(layoutKey, l)
    else resultLayouts.set(result, l)
  }

  export function setHidden(names: string[]) {
    hiddenNames = names
    anchor = focus = null
    storeLayout()
  }
  export function columnList() {
    return columns.map((c, i) => ({ i, name: c.name, type: c.type, hidden: hiddenNames.includes(c.name) }))
  }

  function moveColumn(from: number, to: number) {
    const names = vc.map((i) => columns[i].name)
    const [m] = names.splice(from, 1)
    names.splice(to > from ? to - 1 : to, 0, m)
    // hidden columns keep their place in the full order
    const full = (orderNames.length ? orderNames : columns.map((c) => c.name)).filter((n) => !names.includes(n))
    orderNames = [...names, ...full]
    anchor = focus = null
    storeLayout()
  }

  function keyIcons(c: number): { icon: string; color: string; label: string }[] {
    const k = info?.keys?.[c]
    if (!k) return []
    const out = []
    if (k.includes('PK')) out.push({ icon: 'key', color: 'var(--key-pk)', label: '기본 키 (PK)' })
    if (k.includes('FK')) out.push({ icon: 'link', color: 'var(--key-fk)', label: `외래 키 (FK) → ${info?.refs?.[c] ?? ''}` })
    if (k.includes('UK')) out.push({ icon: 'unique', color: 'var(--key-uk)', label: '유니크 (UK)' })
    return out
  }

  // Scroll position to put back once the grid is laid out; until then the
  // saved view must not be overwritten with the initial 0.
  let restoreTo: { top: number; left: number } | null = null

  // Reset per-result state when a new result set arrives.
  let lastColumns: ResultColumn[] | null = null
  $effect.pre(() => {
    if (columns !== lastColumns) {
      lastColumns = columns
      loadLayout()
      const v = views.get(result)
      if (v && v.columns === columns && v.widths.length === columns.length) {
        widths = v.widths
        sort = v.sort
        anchor = v.anchor
        focus = v.focus
        restoreTo = { top: v.top, left: v.left }
      } else {
        widths = columns.map((c, i) => guessWidth(c, i))
        sort = null
        anchor = focus = null
        if (scroller) scroller.scrollTop = 0
      }
    }
  })

  function saveView() {
    if (!scroller || restoreTo) return
    views.set(result, { columns, top: scroller.scrollTop, left: scroller.scrollLeft, widths: $state.snapshot(widths), sort: $state.snapshot(sort), anchor: $state.snapshot(anchor), focus: $state.snapshot(focus) })
  }
  $effect(() => {
    void widths.join()
    void sort
    void anchor
    void focus
    untrack(saveView)
  })

  onMount(() => {
    if (!restoreTo) return
    requestAnimationFrame(() => {
      const r = restoreTo
      restoreTo = null
      if (!r || !scroller) return
      scroller.scrollTop = r.top
      scroller.scrollLeft = r.left
      scrollTop = scroller.scrollTop
      saveView()
    })
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
    // name + type + padding, plus room for a key icon and the column number
    const head = c.name.length * 7.6 + c.type.length * 6.2 + 34 + 18 + (app.gridColNumbers ? 14 : 0)
    let len = 4
    for (let r = 0; r < Math.min(rows.length, 200); r++) {
      const v = rows[r][i]
      len = Math.max(len, v === null ? 6 : textWidth(v))
    }
    return Math.round(Math.max(56, Math.min(420, Math.max(head, len * 7.6 + 18))))
  }

  // Row order: identity, or sorted by one column (only loaded rows).
  // New rows always stay at the bottom, below everything fetched so far.
  let order = $derived.by(() => {
    const added = st?.added ?? {}
    const idx: number[] = []
    const tail: number[] = []
    for (let i = 0; i < rows.length; i++) (added[i] ? tail : idx).push(i)
    if (!sort || onsort) return idx.concat(tail)
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
    return idx.concat(tail)
  })

  let shownSort = $derived(onsort ? (serverSort ?? null) : sort)

  let first = $derived(Math.max(0, Math.floor((scrollTop - TOP_PAD) / ROW_H) - OVERSCAN))
  let last = $derived(Math.min(order.length, Math.ceil((scrollTop - TOP_PAD + viewH) / ROW_H) + OVERSCAN))
  let visible = $derived(order.slice(first, last).map((ri, k) => ({ ri, vi: first + k })))
  let numW = $derived(Math.round(Math.max(44, String(rowBase + rows.length).length * 8 + 20) * z))
  let cw = $derived(widths.map((w) => Math.round(w * z)))
  let totalW = $derived(numW + vc.reduce((a, i) => a + (cw[i] ?? 0), 0))

  // Zoom (app.gridZoom) is set for the whole tab by QueryTab (Ctrl+wheel,
  // Ctrl+-/+/0, the % button); keep the first visible row in place.
  let lastZoom = app.gridZoom
  $effect(() => {
    const zz = z
    if (zz === lastZoom || !scroller) return
    const row = scroller.scrollTop / Math.round(24 * lastZoom)
    lastZoom = zz
    requestAnimationFrame(() => scroller && (scroller.scrollTop = row * Math.round(24 * zz)))
  })

  function onScroll() {
    scrollTop = scroller.scrollTop
    scrollLeft = scroller.scrollLeft
    // While new rows are pending at the bottom, don't auto-load more rows
    // above them (they would jump away); the explicit button still works.
    const hasAdded = !!st && Object.keys(st.added).length > 0
    if (hasMore && !loadingMore && !hasAdded && scroller.scrollTop + scroller.clientHeight > TOP_PAD + rows.length * ROW_H - ROW_H * 20) {
      onloadmore()
    }
    // (the previous page is only loaded by its button: loading it while
    // scrolling down from the top of a page was confusing)
    saveView()
  }

  /** Rows were inserted (k > 0) or removed (k < 0) above the view: keep it still. */
  // `extra` moves only the scroll (e.g. -1 when the top bar went away).
  export function shiftRows(k: number, extra = 0) {
    if (!k && !extra) return
    if (anchor) anchor = { r: Math.max(0, anchor.r + k), c: anchor.c }
    if (focus) focus = { r: Math.max(0, focus.r + k), c: focus.c }
    if (editing) editing = { ...editing, ri: editing.ri + k, vi: editing.vi + k }
    scroller.scrollTop = Math.max(0, scroller.scrollTop + (k + extra) * ROW_H)
    scrollTop = scroller.scrollTop
  }

  /** Index of the first row in view. */
  export function topRow(): number {
    return Math.max(0, Math.floor((scroller.scrollTop - TOP_PAD) / ROW_H))
  }

  export function scrollToRow(vi: number) {
    scroller.scrollTop = vi <= 0 ? 0 : TOP_PAD + vi * ROW_H
    scrollTop = scroller.scrollTop
    const c = focus?.c ?? 0
    if (order.length) anchor = focus = { r: Math.min(order.length - 1, Math.max(0, vi)), c }
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

  // One cell selected: other cells of that column with the same value are
  // highlighted (as in HeidiSQL).
  let same = $derived.by(() => {
    if (!sel || sel.r1 !== sel.r2 || sel.c1 !== sel.c2 || sel.r1 >= order.length) return null
    const c = vc[sel.c1]
    const v = val(order[sel.r1], c)
    return v === null || v === '' ? null : { c, v }
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
    if (e.shiftKey && anchor) focus = { r: vi, c: vc.length - 1 }
    else {
      anchor = { r: vi, c: 0 }
      focus = { r: vi, c: vc.length - 1 }
    }
    scroller.focus({ preventScroll: true })
  }

  function selectAll() {
    if (!rows.length) return
    anchor = { r: 0, c: 0 }
    focus = { r: order.length - 1, c: vc.length - 1 }
  }

  function scrollIntoView(vi: number, c: number) {
    const top = TOP_PAD + vi * ROW_H
    if (top < scroller.scrollTop) scroller.scrollTop = top
    else if (top + ROW_H > scroller.scrollTop + scroller.clientHeight - HEAD_H) scroller.scrollTop = top + ROW_H - scroller.clientHeight + HEAD_H + 12
    let left = numW
    for (let i = 0; i < c; i++) left += cw[vc[i]]
    const right = left + cw[vc[c]]
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
    if (e.key === 'Home') mv = mod ? [-order.length, 0] : [0, -vc.length]
    if (e.key === 'End') mv = mod ? [order.length, 0] : [0, vc.length]
    if (!mv) return
    e.preventDefault()
    const r = Math.max(0, Math.min(order.length - 1, focus.r + mv[0]))
    const c = Math.max(0, Math.min(vc.length - 1, focus.c + mv[1]))
    focus = { r, c }
    if (!e.shiftKey || e.key === 'Tab') anchor = { r, c }
    scrollIntoView(r, c)
  }

  function selectedMatrix(): { cols: number[]; data: Cell[][] } | null {
    if (!sel) return null
    const cols: number[] = []
    for (let c = sel.c1; c <= sel.c2; c++) cols.push(vc[c])
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

  // "…" while editing inline: continue in the large editor with the draft.
  function editInViewer() {
    if (!editing) return
    const { ri, c, value } = editing
    editing = null
    viewing = { col: columns[c].name, value, ri, c }
  }

  function openViewer(vi: number, p: number) {
    const ri = order[vi]
    const c = vc[p]
    const canEdit = rowEditable(info, ri) && !!info?.columns[c] && !st?.deleted[ri]
    viewing = { col: columns[c].name, value: val(ri, c), ri, c: canEdit ? c : -1 }
  }

  function headerClick(c: number) {
    if (dragged) {
      dragged = false // the click that ends a drag doesn't sort
      return
    }
    if (onsort) {
      onsort(c)
      return
    }
    if (!sort || sort.col !== c) sort = { col: c, dir: 1 }
    else if (sort.dir === 1) sort = { col: c, dir: -1 }
    else sort = null
    anchor = focus = null
  }

  // ---- header drag to reorder columns
  let headEl: HTMLDivElement
  let drag = $state<{ from: number; to: number } | null>(null)
  let dragged = false

  function edgeX(p: number): number {
    let x = numW
    for (let i = 0; i < p; i++) x += cw[vc[i]]
    return x
  }

  function headDown(e: MouseEvent, p: number) {
    if (e.button !== 0) return
    const x0 = e.clientX
    dragged = false
    const move = (ev: MouseEvent) => {
      if (!dragged && Math.abs(ev.clientX - x0) < 6) return
      dragged = true
      const x = ev.clientX - headEl.getBoundingClientRect().left
      let to = vc.length
      for (let i = 0; i < vc.length; i++) {
        if (x < edgeX(i) + cw[vc[i]] / 2) {
          to = i
          break
        }
      }
      drag = { from: p, to }
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      if (drag && drag.to !== drag.from && drag.to !== drag.from + 1) moveColumn(drag.from, drag.to)
      drag = null
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  // ---- header menu (right click)
  let headMenu = $state<{ x: number; y: number; c: number } | null>(null)
  let colMenu = $state<{ x: number; y: number } | null>(null)

  function startResize(e: MouseEvent, c: number) {
    e.preventDefault()
    e.stopPropagation()
    const x0 = e.clientX
    const w0 = widths[c]
    const move = (ev: MouseEvent) => (widths[c] = Math.max(40, w0 + (ev.clientX - x0) / z))
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

<svelte:window onclick={() => ((menu = null), (headMenu = null), (colMenu = null))} />

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class="grid"
  bind:this={scroller}
  bind:clientHeight={viewH}
  bind:clientWidth={viewW}
  onscroll={onScroll}
  style:--gz={z}
  style:--row-h="{ROW_H}px"
  style:--head-h="{HEAD_H}px"
  onkeydown={keydown}
  oncontextmenu={contextMenu}
  tabindex="0"
  role="grid"
>
  <div class="inner" style:height="{BODY_T + order.length * ROW_H + (hasMore ? ROW_H : 0)}px" style:width="{totalW}px">
    <div class="head" bind:this={headEl} style:height="{HEAD_H}px" style:width="{totalW}px">
      <!-- corner: column chooser -->
      <button
        class="num corner"
        class:has-hidden={hiddenCount > 0}
        style:width="{numW}px"
        title={hiddenCount > 0 ? `컬럼 ${hiddenCount}개 숨김 · 눌러서 보이기/숨기기` : '컬럼 보이기/숨기기'}
        onclick={(e) => (e.stopPropagation(), (colMenu = colMenu ? null : { x: e.clientX, y: e.clientY }))}
      >
        <Icon name="columns" size={13} />{#if hiddenCount > 0}<span class="hc">{hiddenCount}</span>{/if}
      </button>
      {#each vc as c, p (c)}
        {@const col = columns[c]}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="th"
          class:dragging={drag?.from === p}
          style:width="{cw[c]}px"
          title={`${col.name} ${col.type.toLowerCase()}${info?.comments?.[c] ? '\n' + info.comments[c] : ''}\n\n${onsort ? '클릭: DB에서 정렬 (ORDER BY)' : hasMore ? '클릭: 불러온 행만 화면에서 정렬' : '클릭: 정렬'} · 드래그: 순서 변경`}
          onmousedown={(e) => headDown(e, p)}
          onclick={() => headerClick(c)}
          oncontextmenu={(e) => (e.preventDefault(), e.stopPropagation(), (headMenu = { x: e.clientX, y: e.clientY, c }))}
          role="columnheader" tabindex="-1">
          {#if app.gridColNumbers}<span class="cno">{c + 1}</span>{/if}
          {#each keyIcons(c) as k (k.icon)}
            <span class="kicon" title={k.label}><Icon name={k.icon} size={12} color={k.color} /></span>
          {/each}
          <span class="name">{col.name}</span>
          {#if shownSort?.col === c}<span class="sort">{shownSort.dir === 1 ? '▲' : '▼'}</span>{/if}
          <span class="type">{col.type.toLowerCase()}</span>
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span class="resize" onmousedown={(e) => startResize(e, c)}></span>
        </div>
      {/each}
    </div>
    {#if drag && drag.to !== drag.from && drag.to !== drag.from + 1}
      <div class="drop-line" style:left="{edgeX(drag.to) - 1}px"></div>
    {/if}

    {#if canLoadPrev || loadingPrev}
      <div class="more top" style:top="{HEAD_H}px" style:left="{scrollLeft}px" style:width="{viewW}px">
        {#if loadingPrev}이전 행 불러오는 중…{:else}<button class="link" onclick={() => onloadprev?.()}>이전 행 불러오기</button>{/if}
      </div>
    {/if}

    {#each visible as { ri, vi } (ri)}
      <div class="tr" class:alt={vi % 2 === 1} class:cur={!!sel && vi >= sel.r1 && vi <= sel.r2} style:top="{BODY_T + vi * ROW_H}px" style:width="{totalW}px">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="num"
          class:added={st?.added[ri]}
          class:deleted={st?.deleted[ri]}
          style:width="{numW}px"
          onmousedown={(e) => rowDown(e, vi)}
        >
          {st?.added[ri] ? '+' : rowBase + vi + 1}
        </div>
        {#each vc as c, p (c)}
          {@const v = val(ri, c)}
          {@const isFocus = focus?.r === vi && focus?.c === p}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="td"
            class:sel={isSel(vi, p)}
            class:focus={isFocus}
            class:same={!!same && same.c === c && v === same.v && !isFocus}
            class:right={numeric[c]}
            class:v-num={kind[c] === 'num'}
            class:v-date={kind[c] === 'date'}
            class:v-bool={kind[c] === 'bool'}
            class:edited={isEdited(result, ri, c)}
            class:added={st?.added[ri]}
            class:deleted={st?.deleted[ri]}
            class:editing={editing?.ri === ri && editing?.c === c}
            style:width="{cw[c]}px"
            onmousedown={(e) => cellDown(e, vi, p)}
            onmouseover={(e) => cellOver(e, vi, p)}
            ondblclick={() => startEdit(vi, p)}
            onfocus={() => {}}
          >
            {#if editing?.ri === ri && editing?.c === c}
              <input
                class="cell-input"
                class:with-dots={!kind[c]}
                bind:value={editing.value}
                use:focusInput={editing.value.length !== 1}
                onkeydown={editorKey}
                onblur={() => commitEdit()}
                onmousedown={(e) => e.stopPropagation()}
                spellcheck="false"
              />
              <!-- text columns only (numbers, dates, booleans are short); mousedown
                   is prevented so the input keeps focus (no blur = no commit) -->
              {#if !kind[c]}<button
                class="dots"
                title="큰 창에서 편집 (F2)"
                onmousedown={(e) => (e.preventDefault(), e.stopPropagation())}
                ondblclick={(e) => e.stopPropagation()}
                onclick={(e) => (e.stopPropagation(), editInViewer())}>…</button
              >{/if}
            {:else if v === null}<span class="null">&lt;null&gt;</span>{:else}{display(v)}{/if}
          </div>
        {/each}
      </div>
    {/each}

    {#if hasMore}
      <div class="more" style:top="{BODY_T + order.length * ROW_H}px" style:left="{scrollLeft}px" style:width="{viewW}px">
        {#if loadingMore}더 불러오는 중…{:else}<button class="link" onclick={onloadmore}>더 불러오기</button>{/if}
      </div>
    {/if}
  </div>

  {#if rows.length === 0}
    <div class="empty">결과 행이 없습니다</div>
  {/if}
</div>

{#snippet mi(name: string)}<span class="mi"><Icon {name} size={14} /></span>{/snippet}

{#if menu}
  <div class="menu" style:left="{menu.x}px" style:top="{menu.y}px">
    <button onclick={() => copySelection(false)}>{@render mi('copy')}복사 <kbd>{modKey}C</kbd></button>
    <button onclick={() => copySelection(true)}>{@render mi('copy')}헤더 포함 복사</button>
    <button onclick={copyInList}>{@render mi('list')}IN (…) 목록으로 복사</button>
    {#if focus}<button onclick={() => focus && openViewer(focus.r, focus.c)}>{@render mi('view')}값 보기 <kbd>Shift+Enter</kbd></button>{/if}
    <button onclick={selectAll}>{@render mi('selectAll')}전체 선택</button>
    {#if info?.editable || info?.insertable}
      <hr />
      {#if focus}<button onclick={() => focus && startEdit(focus.r, focus.c)}>{@render mi('edit')}편집 <kbd>Enter</kbd></button>{/if}
      <button onclick={setNull}>{@render mi('nullValue')}NULL로 설정</button>
      <button onclick={newRow}>{@render mi('rowAdd')}행 추가</button>
      <button class="danger" onclick={deleteRows}>{@render mi('trash')}행 삭제 / 복원 <kbd>Delete</kbd></button>
      <button onclick={() => revert(result, selectedRows())}>{@render mi('undo')}선택 행 변경 되돌리기</button>
    {/if}
  </div>
{/if}

{#if colMenu}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="menu colmenu" style:left="{colMenu.x}px" style:top="{colMenu.y}px" onclick={(e) => e.stopPropagation()}>
    <div class="cm-head">
      <span>컬럼 {vc.length}/{columns.length}</span>
      <button class="link" onclick={() => setHidden([])}>모두 보이기</button>
    </div>
    <div class="cm-list">
      {#each columns as col, i (i)}
        <label>
          <input
            type="checkbox"
            checked={!hiddenNames.includes(col.name)}
            onchange={(e) =>
              setHidden((e.currentTarget as HTMLInputElement).checked ? hiddenNames.filter((n) => n !== col.name) : [...hiddenNames, col.name])}
          />
          {#if app.gridColNumbers}<span class="cno">{i + 1}</span>{/if}
          <span class="cm-name">{col.name}</span>
          <span class="cm-type">{col.type.toLowerCase()}</span>
        </label>
      {/each}
    </div>
    {#if orderNames.length}
      <button class="link cm-reset" onclick={() => ((orderNames = []), storeLayout())}>컬럼 순서 원래대로</button>
    {/if}
  </div>
{/if}

{#if headMenu}
  {@const hc = headMenu.c}
  <div class="menu" style:left="{headMenu.x}px" style:top="{headMenu.y}px">
    <button onclick={() => setHidden([...hiddenNames, columns[hc].name])} disabled={vc.length <= 1}>{@render mi('eyeOff')}이 컬럼 숨기기</button>
    {#if hiddenCount > 0}<button onclick={() => setHidden([])}>{@render mi('view')}숨긴 컬럼 모두 보이기 ({hiddenCount})</button>{/if}
    {#if orderNames.length}<button onclick={() => ((orderNames = []), storeLayout())}>{@render mi('undo')}컬럼 순서 원래대로</button>{/if}
    <button onclick={() => copyText(columns[hc].name)}>{@render mi('copy')}컬럼 이름 복사</button>
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
    font-size: calc(12.5px * var(--gz));
    background: var(--bg);
  }

  .inner {
    position: relative;
    min-width: 100%;
  }

  .head {
    position: sticky;
    top: 0;
    z-index: 3;
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
    font-size: calc(11px * var(--gz));
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .th .sort {
    font-size: calc(9px * var(--gz));
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
    height: var(--row-h);
  }

  .tr.alt {
    background: var(--grid-alt);
  }

  /* rows of the selected cells: easy to follow across the columns */
  .tr.cur {
    background: var(--grid-cur-row);
  }

  .tr.cur .num {
    color: var(--accent);
    font-weight: 600;
    background: color-mix(in srgb, var(--accent) 14%, var(--bg-panel));
  }

  .num {
    position: sticky;
    left: 0;
    z-index: 1;
    flex-shrink: 0;
    padding: 0 8px;
    text-align: right;
    line-height: var(--row-h);
    color: var(--text-faint);
    background: var(--bg-panel);
    border-right: 1px solid var(--border);
    font-size: calc(11px * var(--gz));
    cursor: default;
  }

  .corner {
    z-index: 3;
    line-height: var(--head-h);
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 2px;
    height: 100%;
    border: none;
    border-right: 1px solid var(--border);
    cursor: pointer;
  }
  .corner:hover {
    color: var(--text);
    background: var(--bg-hover);
  }
  .corner.has-hidden {
    color: var(--accent);
  }
  .hc {
    font-size: 10px;
    font-weight: 700;
  }
  .cno {
    flex-shrink: 0;
    color: var(--text-faint);
    font-size: calc(10.5px * var(--gz, 1));
    font-weight: 400;
    font-variant-numeric: tabular-nums;
  }
  .th.dragging {
    opacity: 0.5;
  }
  .drop-line {
    position: absolute;
    top: 0;
    bottom: 0;
    z-index: 4;
    width: 2px;
    background: var(--accent);
    pointer-events: none;
  }
  .td {
    position: relative;
  }
  .cell-input.with-dots {
    padding-right: 26px; /* room for the "…" button */
  }
  .td.same {
    background: var(--cell-same);
  }
  .dots {
    position: absolute;
    top: 50%;
    right: 2px;
    transform: translateY(-50%);
    height: calc(var(--row-h) - 6px);
    padding: 0 5px;
    border: 1px solid var(--border);
    border-radius: 3px;
    background: var(--bg-elev);
    color: var(--text-dim);
    font-family: var(--font-ui);
    font-size: 11px;
    line-height: 1;
    cursor: pointer;
  }
  .dots:hover {
    color: var(--text);
    border-color: var(--accent);
  }
  .colmenu {
    min-width: 260px;
    max-width: 360px;
    padding: 6px;
  }
  .cm-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 2px 6px 6px;
    color: var(--text-dim);
    font-size: 12px;
  }
  .cm-list {
    max-height: 340px;
    overflow: auto;
  }
  .cm-list label {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 3px 6px;
    border-radius: 4px;
    cursor: pointer;
  }
  .cm-list label:hover {
    background: var(--bg-hover);
  }
  .cm-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cm-type {
    color: var(--text-faint);
    font-size: 11px;
  }
  .cm-reset {
    margin: 6px 6px 2px;
    align-self: flex-start;
  }

  .td {
    flex-shrink: 0;
    padding: 0 8px;
    line-height: var(--row-h);
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

  .td.v-num {
    color: var(--cell-num);
  }

  .td.v-date {
    color: var(--cell-date);
  }

  .td.v-bool {
    color: var(--cell-bool);
  }

  .kicon {
    display: inline-flex;
    flex-shrink: 0;
    margin-right: -3px;
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

  /* "load more / previous" rows: across the visible width, centred, above
     the sticky row-number column */
  .more {
    position: absolute;
    z-index: 2;
    height: var(--row-h);
    text-align: center;
    line-height: var(--row-h);
    color: var(--text-dim);
    font-family: var(--font-ui);
  }

  .more.top {
    background: var(--bg-panel);
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
    align-items: center;
    gap: 8px;
    padding: 5px 10px 5px 8px;
    border: none;
    border-radius: 4px;
    background: none;
    text-align: left;
    cursor: pointer;
  }

  .menu button:hover:not(:disabled) {
    background: var(--bg-select);
  }
  .menu button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .menu button.danger {
    color: var(--danger);
  }
  .menu kbd {
    margin-left: auto;
    padding-left: 16px;
  }
  .mi {
    display: inline-flex;
    width: 14px;
    flex-shrink: 0;
    color: var(--text-dim);
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
