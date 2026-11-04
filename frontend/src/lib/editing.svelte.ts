// Pending edits on a result set: kept client-side until "적용", then sent to
// the backend as one transaction.
import { t } from './i18n.svelte'
import { api, errText } from './api'
import { toast, type Tab } from './state.svelte'
import type { Cell, EditState, RowChange, StmtResult } from './types'

export function editState(r: StmtResult): EditState {
  if (!r.edit) r.edit = { info: null, loading: false, edits: {}, deleted: {}, added: {} }
  return r.edit
}

/** Loads (once) whether the result can be edited. */
export async function loadEditInfo(tab: Tab, r: StmtResult) {
  const st = editState(r)
  if (st.info || st.loading) return st.info
  if (!r.source || !r.source.length) {
    st.info = {
      editable: false,
      reason: t('단일 테이블 SELECT 결과만 편집할 수 있습니다 (JOIN, GROUP BY, UNION 등은 불가)'),
      schema: '',
      table: { schema: '', name: '', type: '' },
      columns: [],
      key: [],
    }
    return st.info
  }
  st.loading = true
  try {
    st.info = await api.EditInfo(
      tab.connId,
      tab.schema,
      r.source,
      (r.columns ?? []).map((c) => c.name),
    )
  } catch (e) {
    toast(errText(e), 'error')
  } finally {
    st.loading = false
  }
  return st.info
}

export function cellValue(r: StmtResult, row: number, col: number): Cell {
  const e = r.edit?.edits[row]
  if (e && col in e) return e[col]
  return r.rows![row][col]
}

export function isEdited(r: StmtResult, row: number, col: number): boolean {
  const e = r.edit?.edits[row]
  return !!e && col in e
}

export function setCell(r: StmtResult, row: number, col: number, value: Cell) {
  const st = editState(r)
  const orig = r.rows![row][col]
  const e = (st.edits[row] ??= {})
  if (!st.added[row] && value === orig) {
    delete e[col]
    if (Object.keys(e).length === 0) delete st.edits[row]
  } else {
    e[col] = value
  }
}

/** Undoes the edits of single cells (a new row's cell goes back to unset). */
export function revertCells(r: StmtResult, cells: [number, number][]) {
  const st = r.edit
  if (!st) return
  for (const [row, col] of cells) {
    const e = st.edits[row]
    if (!e) continue
    delete e[col]
    if (!st.added[row] && Object.keys(e).length === 0) delete st.edits[row]
  }
}

export function addRow(r: StmtResult): number {
  const st = editState(r)
  r.rows!.push((r.columns ?? []).map(() => null))
  const i = r.rows!.length - 1
  st.added[i] = true
  st.edits[i] = {}
  return i
}

export function toggleDelete(r: StmtResult, rows: number[]) {
  const st = editState(r)
  const all = rows.every((i) => st.deleted[i])
  for (const i of rows) {
    if (all) delete st.deleted[i]
    else st.deleted[i] = true
  }
}

export function revert(r: StmtResult, rows?: number[]) {
  const st = r.edit
  if (!st) return
  const target = rows ?? [...new Set([...Object.keys(st.edits), ...Object.keys(st.deleted), ...Object.keys(st.added)].map(Number))]
  // Highest index first, so trailing added rows can simply be popped.
  for (const i of [...target].sort((a, b) => b - a)) {
    delete st.edits[i]
    if (!st.added[i]) {
      delete st.deleted[i]
    } else if (i === r.rows!.length - 1) {
      r.rows!.pop()
      delete st.added[i]
      delete st.deleted[i]
    } else {
      // An added row in the middle (more rows were fetched after it): keep
      // the slot but mark it dropped. added+deleted rows are never sent.
      st.deleted[i] = true
    }
  }
}

export function pendingCount(r: StmtResult | undefined): number {
  const st = r?.edit
  if (!st) return 0
  let n = 0
  for (const k of Object.keys(st.edits)) {
    const i = Number(k)
    if (st.deleted[i]) continue
    if (st.added[i] || Object.keys(st.edits[i]).length) n++
  }
  for (const k of Object.keys(st.deleted)) if (!st.added[Number(k)]) n++
  return n
}

export function tabPending(tab: Tab): number {
  return tab.results.reduce((n, r) => n + pendingCount(r), 0)
}

export function buildChanges(r: StmtResult): RowChange[] {
  const st = r.edit!
  const info = st.info!
  const keyOf = (row: number) => {
    const key: Record<string, Cell> = {}
    for (const k of info.key) {
      const c = info.columns.indexOf(k)
      key[k] = r.rows![row][c]
    }
    return key
  }
  const valuesOf = (row: number) => {
    const values: Record<string, Cell> = {}
    for (const [c, v] of Object.entries(st.edits[row] ?? {})) {
      const name = info.columns[Number(c)]
      if (name) values[name] = v
    }
    return values
  }
  const out: RowChange[] = []
  const rows = [...new Set([...Object.keys(st.edits), ...Object.keys(st.deleted)].map(Number))].sort((a, b) => a - b)
  // deletes first, then updates, then inserts (avoids key clashes)
  for (const i of rows) if (st.deleted[i] && !st.added[i]) out.push({ kind: 'delete', key: keyOf(i), values: {} })
  for (const i of rows)
    if (!st.deleted[i] && !st.added[i] && Object.keys(st.edits[i] ?? {}).length)
      out.push({ kind: 'update', key: keyOf(i), values: valuesOf(i) })
  for (const i of rows) if (st.added[i] && !st.deleted[i]) out.push({ kind: 'insert', key: {}, values: valuesOf(i) })
  return out
}

/** After a successful apply, fold the edits into the visible rows. */
export function commitLocally(r: StmtResult) {
  const st = r.edit!
  for (const [k, e] of Object.entries(st.edits)) {
    const row = r.rows![Number(k)]
    for (const [c, v] of Object.entries(e)) row[Number(c)] = v
  }
  const del = Object.keys(st.deleted)
    .map(Number)
    .sort((a, b) => b - a)
  for (const i of del) r.rows!.splice(i, 1)
  st.edits = {}
  st.deleted = {}
  st.added = {}
}

export async function previewChanges(tab: Tab, r: StmtResult): Promise<string[]> {
  const info = r.edit!.info!
  return api.PreviewChanges({ connId: tab.connId, schema: info.schema, table: info.table, changes: buildChanges(r) })
}

export async function applyChanges(tab: Tab, r: StmtResult): Promise<{ count: number; inserted: boolean }> {
  const info = r.edit!.info!
  const changes = buildChanges(r)
  const count = await api.ApplyChanges({ connId: tab.connId, schema: info.schema, table: info.table, changes })
  commitLocally(r)
  return { count, inserted: changes.some((c) => c.kind === 'insert') }
}
