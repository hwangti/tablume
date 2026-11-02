import { api, errText } from './api'
import { getTheme, setTheme } from './theme'
import { tabPending } from './editing.svelte'
import type { Cell, Column, ConnConfig, Routine, SchemaFilter, StmtResult, Table } from './types'

export interface TableNode {
  t: Table
  expanded: boolean
  loading: boolean
  columns: Column[] | null
  error?: string
}

export interface SchemaNode {
  name: string
  expanded: boolean
  loading: boolean
  tables: TableNode[] | null
  error?: string
  // procedures / functions, loaded when their group is opened
  routines?: Routine[] | null
  routinesOpen?: boolean
  routinesLoading?: boolean
  // row counts / sizes, loaded separately (slow on big catalogs) and only
  // while the tree shows them
  stats?: 'loading' | 'done'
}

export interface ConnState {
  status: 'idle' | 'connecting' | 'connected' | 'error'
  version?: string
  error?: string
  expanded: boolean
  schemas: SchemaNode[]
  current: string
}

export interface Message {
  time: string
  kind: 'info' | 'error'
  text: string
}

export interface Tab {
  id: string
  title: string
  connId: string
  schema: string
  sql: string
  results: StmtResult[]
  activeResult: number // index into results, or -1 for the messages pane
  running: boolean
  loadingPrev?: boolean
  // Table tab being prepared (building the SELECT) before it runs.
  opening?: boolean
  openSeq?: number
  loadingMore: boolean
  messages: Message[]
  startedAt: number
  // A preview tab shows table data and is reused by the next table click,
  // until the user edits its SQL or pins it (double-click).
  preview?: boolean
  // 'results' hides the editor so the grid gets the whole tab.
  layout?: 'split' | 'results'
  // Set for table data tabs: the SELECT is rebuilt from these when the
  // filter bar or a column header changes. Dropped if the SQL is edited.
  tableRef?: TableRef
}

export interface TableRef {
  schema: string
  table: Table
  where: string
  orderBy: string
  sort?: { col: string; dir: 'ASC' | 'DESC' } // set by header clicks
}

// Results (console and table tabs alike) load one page per query: the
// backend adds LIMIT/OFFSET and the statement finishes, so no cursor stays
// open holding locks. Scrolling appends the next page (or prepends the
// previous one); the loaded rows form a window of at most WINDOW_MAX rows.
export const PAGE_SIZES = [100, 500, 1000, 5000, 10000]

function windowMax(): number {
  return Math.max(10000, app.pageSize * 2)
}

function tableSelect(tab: Tab): Promise<string> {
  const ref = tab.tableRef!
  return api.SelectSQL(tab.connId, ref.schema, $state.snapshot(ref.table), ref.where, ref.orderBy, 0, 0)
}

export interface Toast {
  id: number
  kind: 'info' | 'error'
  text: string
}

export const app = $state({
  connections: [] as ConnConfig[],
  conn: {} as Record<string, ConnState>,
  tabs: [] as Tab[],
  activeTab: '',
  pageSize: 500, // rows per query, console and table tabs
  editing: null as ConnConfig | null,
  prompt: null as null | { title: string; resolve: (v: string | null) => void },
  confirm: null as null | { text: string; resolve: (v: boolean) => void },
  toasts: [] as Toast[],
  completion: {} as Record<string, Record<string, string[]>>,
  // What the schema tree shows next to each table / column.
  treeView: { comment: true, tableCount: true, rows: false, size: false },
  tableOpen: { pkDesc: false },
  gridZoom: 1,
  gridColNumbers: false, // column numbers in result headers
  ddlMode: 'table' as 'sql' | 'table', // DDL tab: columns table first
  theme: getTheme(),
})

let toastSeq = 0
export function toast(text: string, kind: 'info' | 'error' = 'info') {
  const id = ++toastSeq
  app.toasts.push({ id, kind, text })
  setTimeout(() => {
    app.toasts = app.toasts.filter((t) => t.id !== id)
  }, kind === 'error' ? 8000 : 3000)
}

export function newId(): string {
  return Math.random().toString(36).slice(2, 10) + Date.now().toString(36).slice(-4)
}

function now(): string {
  const d = new Date()
  return [d.getHours(), d.getMinutes(), d.getSeconds()].map((n) => String(n).padStart(2, '0')).join(':')
}

export function connById(id: string): ConnConfig | undefined {
  return app.connections.find((c) => c.id === id)
}

function connState(id: string): ConnState {
  if (!app.conn[id]) {
    app.conn[id] = { status: 'idle', expanded: false, schemas: [], current: '' }
  }
  return app.conn[id]
}

// ---------------------------------------------------------------- connections

export async function loadConnections() {
  app.connections = (await api.ListConnections()) ?? []
  for (const c of app.connections) connState(c.id)
}

export function askPassword(title: string): Promise<string | null> {
  return new Promise((resolve) => {
    app.prompt = {
      title,
      resolve: (v) => {
        app.prompt = null
        resolve(v)
      },
    }
  })
}

// window.confirm isn't available in every webview (macOS WKWebView), so
// confirmations use our own modal.
export function confirmBox(text: string): Promise<boolean> {
  return new Promise((resolve) => {
    app.confirm = {
      text,
      resolve: (v) => {
        app.confirm = null
        resolve(v)
      },
    }
  })
}

export async function connect(id: string): Promise<boolean> {
  const st = connState(id)
  if (st.status === 'connected') return true
  if (st.status === 'connecting') return false
  const c = connById(id)
  if (!c) return false
  let pw = ''
  if (await api.NeedsPassword(id)) {
    const v = await askPassword(`${c.name} 비밀번호`)
    if (v === null) return false
    pw = v
  }
  st.status = 'connecting'
  st.error = undefined
  try {
    st.version = await api.Connect(id, pw)
    st.status = 'connected'
    st.expanded = true
    await refreshSchemas(id)
    return true
  } catch (e) {
    st.status = 'error'
    st.error = errText(e)
    toast(`${c.name}: ${st.error}`, 'error')
    return false
  }
}

export async function disconnect(id: string) {
  await api.Disconnect(id)
  const st = connState(id)
  st.status = 'idle'
  st.schemas = []
  st.expanded = false
  for (const k of Object.keys(app.completion)) if (k.startsWith(id + '|')) delete app.completion[k]
}

export async function refreshSchemas(id: string) {
  const st = connState(id)
  try {
    const list = await api.ListSchemas(id)
    const old = new Map(st.schemas.map((s) => [s.name, s]))
    st.current = list.current
    st.schemas = (list.schemas ?? []).map(
      (name) => old.get(name) ?? { name, expanded: false, loading: false, tables: null },
    )
    for (const s of st.schemas) if (s.expanded && schemaShown(id, s.name)) await loadTables(id, s)
    const cur = st.schemas.find((s) => s.name === st.current)
    if (cur && !cur.expanded && schemaShown(id, cur.name) && st.schemas.every((s) => !s.expanded || !schemaShown(id, s.name))) {
      cur.expanded = true
      await loadTables(id, cur)
    }
  } catch (e) {
    toast(errText(e), 'error')
  }
}

export async function loadTables(id: string, s: SchemaNode) {
  s.loading = true
  s.error = undefined
  try {
    const old = new Map((s.tables ?? []).map((t) => [t.t.schema + '.' + t.t.name, t]))
    const tables = (await api.ListTables(id, s.name)) ?? []
    s.tables = tables.map(
      (t) => old.get(t.schema + '.' + t.name) ?? { t, expanded: false, loading: false, columns: null },
    )
    s.stats = undefined
  } catch (e) {
    s.error = errText(e)
  } finally {
    s.loading = false
  }
  if (s.tables && (app.treeView.rows || app.treeView.size)) await loadStats(id, s)
}

// Row counts and sizes for a schema's tables, merged into the loaded list.
export async function loadStats(id: string, s: SchemaNode) {
  if (!s.tables || s.stats) return
  s.stats = 'loading'
  try {
    const stats = (await api.TableStats(id, s.name)) ?? []
    const by = new Map(stats.map((x) => [x.schema + '.' + x.name, x]))
    for (const t of s.tables) {
      const x = by.get(t.t.schema + '.' + t.t.name)
      if (x) {
        t.t.rows = x.rows
        t.t.size = x.size
      }
    }
    s.stats = 'done'
  } catch (e) {
    s.stats = undefined
    toast(`${s.name}: 행 수 · 크기를 가져오지 못했습니다 — ${errText(e)}`, 'error')
  }
}

// After turning on rows or size in the tree: fetch them for the schemas
// already loaded, one at a time.
export async function ensureStats() {
  if (!app.treeView.rows && !app.treeView.size) return
  for (const c of app.connections) {
    const st = app.conn[c.id]
    if (st?.status !== 'connected') continue
    for (const s of st.schemas) if (s.tables && !s.stats && schemaShown(c.id, s.name)) await loadStats(c.id, s)
  }
}

// ---------------------------------------------------------------- schema filter

// Schemas hidden unless picked by name (the server's own catalogs).
export const SYSTEM_SCHEMAS = new Set([
  'information_schema',
  'pg_catalog',
  'pg_toast',
  'mysql',
  'performance_schema',
  'sys',
  'tempdb',
  'model',
  'msdb',
])

export function isSystemSchema(c: ConnConfig | undefined, name: string): boolean {
  return c?.driver !== 'sqlite' && SYSTEM_SCHEMAS.has(name)
}

export const DEFAULT_FILTER: SchemaFilter = { all: true, current: true, names: [], patterns: [] }

export function schemaFilter(c: ConnConfig | undefined): SchemaFilter {
  return c?.schemas ?? DEFAULT_FILTER
}

export function globMatch(pattern: string, name: string): boolean {
  const re = pattern
    .trim()
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*/g, '.*')
    .replace(/\?/g, '.')
  return new RegExp('^' + re + '$', 'i').test(name)
}

// Whether the tree shows this schema, by the connection's filter.
export function schemaShown(connId: string, name: string): boolean {
  const c = connById(connId)
  if (c?.driver === 'sqlite') return true
  const f = schemaFilter(c)
  if (f.names.includes(name)) return true
  if (f.current && name === app.conn[connId]?.current) return true
  if (f.patterns.some((p) => p.trim() && globMatch(p, name))) return true
  return f.all && !isSystemSchema(c, name)
}

export async function setSchemaFilter(connId: string, f: SchemaFilter) {
  const c = connById(connId)
  if (!c) return
  const same = f.all && f.current && !f.names.length && !f.patterns.length
  c.schemas = same ? undefined : $state.snapshot(f)
  try {
    await api.SetSchemaFilter(connId, c.schemas ?? null)
  } catch (e) {
    toast(errText(e), 'error')
  }
  // a newly shown schema that was open before loads its tables
  const st = app.conn[connId]
  for (const s of st?.schemas ?? []) if (s.expanded && s.tables === null && schemaShown(connId, s.name)) loadTables(connId, s)
}

// ---------------------------------------------------------------- order

export async function moveConnection(id: string, to: number) {
  const from = app.connections.findIndex((c) => c.id === id)
  if (from < 0) return
  to = Math.max(0, Math.min(app.connections.length - 1, to))
  if (from === to) return
  const list = [...app.connections]
  const [c] = list.splice(from, 1)
  list.splice(to, 0, c)
  app.connections = list
  try {
    await api.ReorderConnections(list.map((x) => x.id))
  } catch (e) {
    toast(errText(e), 'error')
  }
}

export async function toggleSchema(id: string, s: SchemaNode) {
  s.expanded = !s.expanded
  if (s.expanded && s.tables === null) await loadTables(id, s)
}

export async function toggleTable(id: string, schema: string, t: TableNode) {
  t.expanded = !t.expanded
  if (t.expanded && t.columns === null) {
    t.loading = true
    try {
      t.columns = (await api.ListColumns(id, schema, t.t)) ?? []
    } catch (e) {
      t.error = errText(e)
    } finally {
      t.loading = false
    }
  }
}

export async function loadCompletion(connId: string, schema: string) {
  const key = connId + '|' + schema
  if (!schema || app.completion[key]) return
  app.completion[key] = {}
  try {
    app.completion[key] = (await api.CompletionColumns(connId, schema)) ?? {}
  } catch {
    delete app.completion[key]
  }
}

// ---------------------------------------------------------------- tabs

export function activeTab(): Tab | undefined {
  return app.tabs.find((t) => t.id === app.activeTab)
}

export function openConsole(connId: string, opts: { schema?: string; sql?: string; title?: string } = {}): Tab {
  const c = connById(connId)
  const st = app.conn[connId]
  const n = app.tabs.filter((t) => t.connId === connId).length
  const tab: Tab = {
    id: newId(),
    title: opts.title ?? `${c?.name ?? 'console'}${n ? ` (${n + 1})` : ''}`,
    connId,
    schema: opts.schema ?? st?.current ?? '',
    sql: opts.sql ?? '',
    results: [],
    activeResult: 0,
    running: false,
    loadingMore: false,
    messages: [],
    startedAt: 0,
  }
  app.tabs.push(tab)
  app.activeTab = tab.id
  saveWorkspaceSoon()
  return app.tabs[app.tabs.length - 1]
}

export async function closeTab(id: string) {
  const i = app.tabs.findIndex((t) => t.id === id)
  if (i < 0) return
  const n = tabPending(app.tabs[i])
  if (n && !(await confirmBox(`적용하지 않은 변경 ${n}건이 있습니다. 버리고 탭을 닫을까요?`))) return
  api.CloseSession(id)
  app.tabs.splice(i, 1)
  if (app.activeTab === id) app.activeTab = app.tabs[Math.min(i, app.tabs.length - 1)]?.id ?? ''
  saveWorkspaceSoon()
}

function log(tab: Tab, kind: Message['kind'], text: string) {
  tab.messages.push({ time: now(), kind, text })
  if (tab.messages.length > 500) tab.messages.splice(0, tab.messages.length - 500)
}

function short(sql: string): string {
  const one = sql.replace(/\s+/g, ' ').trim()
  return one.length > 120 ? one.slice(0, 120) + '…' : one
}

/**
 * Runs SQL in the tab. cursor >= 0 runs the statement under the caret;
 * cursor < 0 runs every statement in `sql`. `offset` is added to returned
 * positions (used when running a selection).
 */
export async function runSQL(tab: Tab, sql: string, cursor: number, offset = 0): Promise<StmtResult[] | null> {
  if (tab.running) return null
  if (!(await connect(tab.connId))) return null
  tab.running = true
  tab.startedAt = Date.now()
  try {
    const res = await api.Execute({
      tabId: tab.id,
      connId: tab.connId,
      schema: tab.schema,
      sql,
      cursor,
      fetch: app.pageSize,
    })
    const results = (res.results ?? []).map((r) => ({
      ...r,
      from: r.from + offset,
      to: r.to + offset,
      rows: r.rows ?? [],
      columns: r.columns ?? [],
      start: 0,
      total: r.hasMore ? null : (r.rows ?? []).length,
    }))
    if (res.currentSchema) tab.schema = res.currentSchema
    for (const r of results) {
      if (r.error) log(tab, 'error', `${short(r.sql)}\n${r.error}`)
      else if (r.columns && r.columns.length)
        log(tab, 'info', `${short(r.sql)}\n${r.rows!.length}${r.hasMore ? '+' : ''}행 조회 (${r.elapsedMs}ms)`)
      else
        log(
          tab,
          'info',
          `${short(r.sql)}\n${r.rowsAffected >= 0 ? `${r.rowsAffected}행 영향` : '완료'} (${r.elapsedMs}ms)`,
        )
    }
    const grids = results.filter((r) => r.columns && r.columns.length)
    const failed = results.some((r) => r.error)
    tab.results = grids
    tab.activeResult = failed || grids.length === 0 ? -1 : grids.length - 1
    return results
  } catch (e) {
    log(tab, 'error', errText(e))
    tab.activeResult = -1
    return null
  } finally {
    tab.running = false
  }
}

/** Loaded rows that came from the database (rows added in the grid excluded). */
export function fetchedCount(r: StmtResult): number {
  return (r.rows?.length ?? 0) - Object.keys(r.edit?.added ?? {}).length
}

/**
 * Appends the next page. Returns how many rows were dropped from the top
 * to keep the window small (the grid shifts its scroll by that much).
 */
export async function fetchMore(tab: Tab, r: StmtResult): Promise<number> {
  if (!r.hasMore || tab.loadingMore || tab.running) return 0
  if (!r.paged && !r.cursorId) return 0
  tab.loadingMore = true
  try {
    const res = r.paged
      ? await api.FetchPage(tab.id, tab.connId, r.sql, (r.start ?? 0) + fetchedCount(r), app.pageSize)
      : await api.FetchMore(tab.id, r.cursorId, app.pageSize)
    const rows = res.rows ?? []
    // new rows go before rows added in the grid (those stay at the bottom)
    insertFetched(r, fetchedCount(r), rows)
    r.hasMore = res.hasMore
    if (!r.hasMore) r.total = (r.start ?? 0) + fetchedCount(r)
    return r.paged ? trimFront(r) : 0
  } catch (e) {
    r.hasMore = false
    toast(errText(e), 'error')
    return 0
  } finally {
    tab.loadingMore = false
  }
}

/** Prepends the previous page. Returns the number of rows added on top. */
export async function fetchPrev(tab: Tab, r: StmtResult): Promise<number> {
  const start = r.start ?? 0
  if (!r.paged || start <= 0 || tab.loadingMore || tab.running) return 0
  tab.loadingPrev = true
  try {
    const n = Math.min(app.pageSize, start)
    const res = await api.FetchPage(tab.id, tab.connId, r.sql, start - n, n)
    const rows = res.rows ?? []
    insertFetched(r, 0, rows)
    r.start = start - rows.length
    trimBack(r)
    return rows.length
  } catch (e) {
    toast(errText(e), 'error')
    return 0
  } finally {
    tab.loadingPrev = false
  }
}

/** Replaces the loaded rows with the page at `offset` (pending edits are dropped). */
export async function jumpTo(tab: Tab, r: StmtResult, offset: number): Promise<boolean> {
  if (!r.paged || tab.loadingMore || tab.running) return false
  if (tabPending(tab) && !(await confirmBox(`적용하지 않은 변경 ${tabPending(tab)}건이 있습니다. 버리고 이동할까요?`))) return false
  tab.loadingMore = true
  try {
    const res = await api.FetchPage(tab.id, tab.connId, r.sql, offset, app.pageSize)
    if (r.edit) {
      r.edit.edits = {}
      r.edit.deleted = {}
      r.edit.added = {}
    }
    r.rows = res.rows ?? []
    r.start = offset
    r.hasMore = res.hasMore
    if (!r.hasMore) r.total = offset + r.rows.length
    return true
  } catch (e) {
    toast(errText(e), 'error')
    return false
  } finally {
    tab.loadingMore = false
  }
}

export async function countResult(tab: Tab, r: StmtResult): Promise<number | null> {
  try {
    r.total = await api.CountResult(tab.id, tab.connId, r.sql)
  } catch (e) {
    toast(errText(e), 'error')
  }
  return r.total ?? null
}

// A table tab counted its rows exactly (COUNT, or every row fetched with no
// WHERE): show that in the tree instead of the statistics estimate.
export function syncTreeRows(connId: string, schema: string, t: Table, n: number) {
  const s = app.conn[connId]?.schemas.find((x) => x.name === schema)
  const node = s?.tables?.find((x) => x.t.schema === t.schema && x.t.name === t.name)
  if (node && node.t.rows !== n) node.t.rows = n
}

// Inserts fetched rows at index `at`, moving the row-indexed edit state of
// the rows behind them.
function insertFetched(r: StmtResult, at: number, rows: Cell[][]) {
  if (!rows.length) return
  shiftEdits(r, at, rows.length)
  r.rows!.splice(at, 0, ...rows)
}

// Moves edit state of rows >= from by k (k < 0 removes rows from-|k| … from-1).
function shiftEdits(r: StmtResult, from: number, k: number) {
  const st = r.edit
  if (!st) return
  const move = <T>(m: Record<number, T>) => {
    const out: Record<number, T> = {}
    for (const [key, v] of Object.entries(m)) {
      const i = Number(key)
      out[i >= from ? i + k : i] = v
    }
    return out
  }
  st.edits = move(st.edits)
  st.deleted = move(st.deleted)
  st.added = move(st.added)
}

function hasEdits(r: StmtResult, from: number, to: number): boolean {
  const st = r.edit
  if (!st) return false
  for (const m of [st.edits, st.deleted, st.added])
    for (const key of Object.keys(m)) {
      const i = Number(key)
      if (i >= from && i < to) return true
    }
  return false
}

// Keeps the window small: drops rows from the top (not ones with edits).
function trimFront(r: StmtResult): number {
  const extra = fetchedCount(r) - windowMax()
  if (extra <= 0) return 0
  const k = Math.ceil(extra / app.pageSize) * app.pageSize
  if (hasEdits(r, 0, k)) return 0
  r.rows!.splice(0, k)
  shiftEdits(r, k, -k)
  r.start = (r.start ?? 0) + k
  return k
}

function trimBack(r: StmtResult) {
  const n = fetchedCount(r)
  const extra = n - windowMax()
  if (extra <= 0) return
  const k = Math.ceil(extra / app.pageSize) * app.pageSize
  if (hasEdits(r, n - k, r.rows!.length)) return
  r.rows!.splice(n - k, k)
  shiftEdits(r, n, -k)
  r.hasMore = true
}

export function cancel(tab: Tab) {
  api.Cancel(tab.id)
}

const sameTable = (a: Table, b: Table) => a.schema === b.schema && a.name === b.name

export async function openTableData(connId: string, schema: string, t: Table): Promise<Tab> {
  // The table is already open in a tab (pinned or preview): go there.
  const open = app.tabs.find((x) => x.connId === connId && x.tableRef?.schema === schema && sameTable(x.tableRef.table, t))
  // A preview tab with unsaved edits is never replaced.
  let tab = open ?? app.tabs.find((x) => x.preview && !x.running && tabPending(x) === 0)
  if (tab && tab.connId === connId && tab.tableRef?.schema === schema && sameTable(tab.tableRef.table, t)) {
    app.activeTab = tab.id // same table clicked again: keep its filter
    // (a tab restored from the last session hasn't run yet)
    if (!tab.results.length && !tab.opening) await reloadTable(tab)
    return tab
  }
  const ref: TableRef = { schema, table: t, where: '', orderBy: '' }
  // Show the tab right away (with a loading state); building the SELECT
  // may need a round trip (PK lookup) before the query even starts.
  if (tab) {
    tab.connId = connId
    tab.schema = schema
    tab.sql = ''
    tab.title = t.name
    tab.results = []
    tab.activeResult = 0
    tab.tableRef = ref
    app.activeTab = tab.id
  } else {
    tab = openConsole(connId, { schema, sql: '', title: t.name })
    tab.preview = true
    tab.layout = 'results' // table data: the grid gets the whole tab
    tab.tableRef = ref
  }
  const seq = (tab.openSeq = (tab.openSeq ?? 0) + 1)
  const cur = tab.tableRef! // the reactive copy of ref
  tab.opening = true
  tab.startedAt = Date.now()
  let sql: string
  try {
    if (!(await connect(connId))) return tab
    if (app.tableOpen.pkDesc) await applyPkDesc(connId, cur)
    sql = await tableSelect(tab)
  } catch (e) {
    toast(errText(e), 'error')
    return tab
  } finally {
    if (tab.openSeq === seq) tab.opening = false
  }
  if (tab.openSeq !== seq) return tab // the tab was reused for another table meanwhile
  tab.sql = sql
  saveWorkspaceSoon()
  await runSQL(tab, sql, -1)
  return tab
}

export async function toggleRoutines(id: string, s: SchemaNode) {
  s.routinesOpen = !s.routinesOpen
  if (!s.routinesOpen || s.routines) return
  s.routinesLoading = true
  try {
    s.routines = (await api.ListRoutines(id, s.name)) ?? []
  } catch (e) {
    s.routines = []
    toast(errText(e), 'error')
  } finally {
    s.routinesLoading = false
  }
}

/** Opens a routine's source in the preview tab (a console, so it can be edited and run). */
export async function openRoutine(connId: string, schema: string, r: Routine) {
  let src: string
  try {
    src = await api.RoutineSource(connId, schema, r)
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  const title = r.name
  let tab = app.tabs.find((x) => x.preview && !x.running && tabPending(x) === 0)
  if (tab) {
    tab.connId = connId
    tab.schema = schema
    tab.sql = src
    tab.title = title
    tab.results = []
    tab.tableRef = undefined
    tab.layout = 'split'
    app.activeTab = tab.id
    saveWorkspaceSoon()
  } else {
    tab = openConsole(connId, { schema, sql: src, title })
    tab.preview = true
  }
}

export async function openDDL(connId: string, schema: string, t: Table) {
  try {
    const ddl = await api.TableDDL(connId, schema, t)
    openConsole(connId, { schema, sql: ddl, title: `DDL: ${t.name}` })
  } catch (e) {
    toast(errText(e), 'error')
  }
}

/** Default sort for newly opened tables: primary key, descending. */
async function applyPkDesc(connId: string, ref: TableRef) {
  try {
    const pk = ((await api.ListColumns(connId, ref.schema, ref.table)) ?? []).filter((c) => c.pk)
    if (!pk.length) return
    const quoted = await Promise.all(pk.map((c) => api.QuoteIdent(connId, c.name)))
    ref.orderBy = quoted.map((q) => q + ' DESC').join(', ')
    if (pk.length === 1) ref.sort = { col: pk[0].name, dir: 'DESC' }
  } catch {
    /* no sort then */
  }
}

/** Re-queries a table data tab from its filter / sort. */
/** Re-runs a table tab's query (after a filter or sort change, or refresh). */
export async function reloadTable(tab: Tab) {
  const ref = tab.tableRef
  if (!ref) return
  if (tabPending(tab) && !(await confirmBox(`적용하지 않은 변경 ${tabPending(tab)}건이 있습니다. 버리고 다시 조회할까요?`))) return
  // a tab restored from the last session: connect first
  if (!(await connect(tab.connId))) return
  try {
    tab.sql = await tableSelect(tab)
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  saveWorkspaceSoon()
  await runSQL(tab, tab.sql, -1)
}

export function pinTab(tab: Tab | undefined) {
  if (tab?.preview) {
    tab.preview = false
    saveWorkspaceSoon()
  }
}

// ---------------------------------------------------------------- workspace

let saveTimer: ReturnType<typeof setTimeout> | undefined
export function saveWorkspaceSoon() {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    const data = {
      activeTab: app.activeTab,
      pageSize: app.pageSize,
      treeView: app.treeView,
      tableOpen: app.tableOpen,
      gridZoom: app.gridZoom,
      gridColNumbers: app.gridColNumbers,
      ddlView: app.ddlMode,
      theme: app.theme,
      tabs: app.tabs.map((t) => ({
        id: t.id,
        title: t.title,
        connId: t.connId,
        schema: t.schema,
        sql: t.sql,
        preview: t.preview,
        layout: t.layout,
        tableRef: t.tableRef,
      })),
    }
    api.SaveWorkspace(JSON.stringify(data)).catch(() => {})
  }, 600)
}

export async function loadWorkspace() {
  try {
    const raw = await api.LoadWorkspace()
    if (!raw) return
    const data = JSON.parse(raw)
    // pageSize replaced the console's fetchSize
    const ps = data.pageSize ?? data.fetchSize
    if (typeof ps === 'number' && PAGE_SIZES.includes(ps)) app.pageSize = ps
    if (data.treeView) app.treeView = { ...app.treeView, ...data.treeView }
    if (data.tableOpen) app.tableOpen = { ...app.tableOpen, ...data.tableOpen }
    if ((data.theme === 'auto' || data.theme === 'dark' || data.theme === 'light') && data.theme !== app.theme) {
      app.theme = data.theme
      setTheme(data.theme)
    }
    // (saved as ddlView since the table became the default; the old ddlMode key is ignored)
    if (data.ddlView === 'table' || data.ddlView === 'sql') app.ddlMode = data.ddlView
    if (typeof data.gridColNumbers === 'boolean') app.gridColNumbers = data.gridColNumbers
    if (typeof data.gridZoom === 'number' && data.gridZoom >= 0.5 && data.gridZoom <= 3) app.gridZoom = data.gridZoom
    for (const t of data.tabs ?? []) {
      if (!connById(t.connId)) continue
      app.tabs.push({
        ...t,
        results: [],
        activeResult: 0,
        running: false,
        loadingMore: false,
        messages: [],
        startedAt: 0,
      })
    }
    app.activeTab = app.tabs.find((t) => t.id === data.activeTab)?.id ?? app.tabs[0]?.id ?? ''
  } catch {
    /* ignore a broken workspace file */
  }
}
