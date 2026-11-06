import { api, errText, loginExpired } from './api'
import { Events, Window } from '@wailsio/runtime'
import { isWeb } from './platform'
import { i18n, isLang, t as tr, type Lang } from './i18n.svelte'
import { getTheme, setTheme } from './theme'
import { tabPending } from './editing.svelte'
import { DRIVERS } from './types'
import type { Cell, Column, ConnConfig, Driver, Routine, SchemaFilter, StmtResult, Table } from './types'

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
  sql?: string // the statement(s), shown with syntax colors above text
}

export interface Tab {
  id: string
  title: string
  connId: string
  schema: string
  sql: string
  // The SQL as opened (a file, a routine's source, …) or last saved to a
  // file: a console whose SQL differs asks before it is closed.
  sqlBase?: string
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
  // Shown in a window of its own (main window only; not saved, so after a
  // restart the tab is back in the main window).
  detached?: boolean
  // A table being designed (the structure view only; nothing in the
  // database yet, not saved with the workspace).
  creating?: boolean
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
  // password prompt, or a text prompt when value is set
  prompt: null as null | { title: string; value?: string; ok?: string; resolve: (v: string | null) => void },
  confirm: null as null | { text: string; resolve: (v: boolean) => void },
  toasts: [] as Toast[],
  completion: {} as Record<string, Record<string, string[]>>,
  // What the schema tree shows next to each table / column.
  treeView: { comment: true, tableCount: true, rows: false, size: false },
  tableOpen: { pkDesc: false },
  zoom: 1, // whole window (desktop app; the browser zooms the web page itself)
  sidebarHidden: false, // the connection tree folded away (Ctrl/⌘+B)
  wide: false, // 넓게 보기: only the active tab, no tree or tab strip (not saved)
  orderShare: 0.32, // ORDER BY part of a table tab's filter bar (WHERE gets the rest)
  gridColNumbers: false, // column numbers in result headers
  ddlMode: 'table' as 'sql' | 'table', // structure tab: columns and indexes, or the DDL text
  theme: getTheme(),
  // Databases this build includes (a build can leave some out).
  drivers: DRIVERS.map((d) => d.id) as Driver[],
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
  const drivers = await api.SupportedDrivers().catch(() => null)
  if (drivers?.length) app.drivers = DRIVERS.filter((d) => drivers.includes(d.id)).map((d) => d.id)
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

/** Asks for a line of text (null = cancelled). */
export function askText(title: string, value: string, ok = tr('확인')): Promise<string | null> {
  return new Promise((resolve) => {
    app.prompt = {
      title,
      value,
      ok,
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
  // Another window may have connected already; reconnecting would drop
  // its sessions.
  if (await api.IsConnected(id).catch(() => false)) {
    st.status = 'connected'
    st.expanded = true
    await refreshSchemas(id)
    return true
  }
  let pw = ''
  if (await api.NeedsPassword(id)) {
    const v = await askPassword(tr('{name} 비밀번호', { name: c.name }))
    if (v === null) return false
    pw = v
  }
  st.status = 'connecting'
  st.error = undefined
  try {
    st.version = await api.Connect(id, pw)
    st.status = 'connected'
    st.expanded = true
    Events.Emit('conn:changed', { id, connected: true }).catch(() => {})
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
  Events.Emit('conn:changed', { id, connected: false }).catch(() => {})
  markDisconnected(id)
}

function markDisconnected(id: string) {
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
    toast(`${s.name}: ${tr('행 수 · 크기를 가져오지 못했습니다')} — ${errText(e)}`, 'error')
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

// ---------------------------------------------------------------- language
// The backend has its own copy (its error messages, the macOS menu, the
// browser build's sign-in pages).
export function setLanguage(l: Lang, save = true) {
  i18n.lang = l
  document.documentElement.lang = l
  api.SetLanguage(l).catch(() => {})
  if (save) saveWorkspaceSoon()
}

// ---------------------------------------------------------------- zoom
// One zoom for the whole window, done by the webview itself (like browser
// zoom), so every part scales the same. The native webviews stop at 100%.
export const ZOOM_MIN = 1
export const ZOOM_MAX = 2
export const ZOOM_PRESETS = [1, 1.1, 1.25, 1.5, 1.75, 2]

function applyZoom() {
  if (isWeb) return
  const z = app.zoom
  Window.SetZoom(z).catch(() => {
    // No native window (server build in a browser): CSS zoom instead.
    document.documentElement.style.zoom = z === 1 ? '' : String(z)
  })
}

export function setZoomLevel(v: number): boolean {
  const next = Math.round(Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, v)) * 100) / 100
  if (!isFinite(next) || next === app.zoom) return false
  app.zoom = next
  applyZoom()
  saveWorkspaceSoon()
  return true
}

// step: +1 / -1 to the next preset, 0 back to 100%
export function zoomBy(step: number): boolean {
  if (step === 0) return setZoomLevel(1)
  const z = app.zoom
  const next =
    step > 0 ? (ZOOM_PRESETS.find((s) => s > z + 0.001) ?? ZOOM_MAX) : ([...ZOOM_PRESETS].reverse().find((s) => s < z - 0.001) ?? ZOOM_MIN)
  return setZoomLevel(next)
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
    sqlBase: opts.sql ?? '',
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

// A console with SQL typed in it (not as opened or saved): closing the tab
// loses it.
export function sqlEdited(t: Tab): boolean {
  return !t.tableRef && t.sql.trim() !== '' && t.sql !== (t.sqlBase ?? '')
}

// Asks before closing a tab with unapplied grid edits or edited SQL.
async function okToClose(t: Tab): Promise<boolean> {
  const n = tabPending(t)
  if (n) return confirmBox(tr('적용하지 않은 변경 {n}건이 있습니다. 버리고 탭을 닫을까요?', { n }))
  if (sqlEdited(t)) return confirmBox(tr('“{name}” 콘솔에 편집한 SQL이 있습니다. 닫으면 사라집니다. 닫을까요?', { name: t.title }))
  return true
}

export async function closeTab(id: string) {
  const i = app.tabs.findIndex((t) => t.id === id)
  if (i < 0) return
  if (!(await okToClose(app.tabs[i]))) return
  if (app.tabs[i].detached) api.CloseTabWindow(id)
  api.CloseSession(id)
  app.tabs.splice(i, 1)
  if (app.activeTab === id) app.activeTab = app.tabs[Math.min(i, app.tabs.length - 1)]?.id ?? ''
  saveWorkspaceSoon()
}

// Closes several tabs, asking once about unsaved edits in any of them.
export async function closeTabs(ids: string[]) {
  const set = new Set(ids)
  const closing = app.tabs.filter((t) => set.has(t.id))
  if (!closing.length) return
  const n = closing.reduce((a, t) => a + tabPending(t), 0)
  const edited = closing.filter(sqlEdited)
  if (n && !(await confirmBox(tr('닫을 탭에 적용하지 않은 변경 {n}건이 있습니다. 버리고 닫을까요?', { n })))) return
  if (!n && edited.length === 1 && !(await okToClose(edited[0]))) return
  if (!n && edited.length > 1 && !(await confirmBox(tr('닫을 탭 중 콘솔 {n}개에 편집한 SQL이 있습니다. 닫으면 사라집니다. 닫을까요?', { n: edited.length }))))
    return
  const activeIdx = app.tabs.findIndex((t) => t.id === app.activeTab)
  for (const t of closing) api.CloseSession(t.id)
  const remaining = app.tabs.filter((t) => !set.has(t.id))
  if (set.has(app.activeTab)) {
    // the nearest surviving tab to the right, else to the left
    const right = app.tabs.slice(activeIdx).find((t) => !set.has(t.id))
    const left = [...app.tabs.slice(0, activeIdx)].reverse().find((t) => !set.has(t.id))
    app.activeTab = (right ?? left)?.id ?? ''
  }
  app.tabs = remaining
  saveWorkspaceSoon()
}

// Opens a copy of the tab next to it: same SQL (or table, filter and
// sort), its own session and results.
export async function duplicateTab(id: string) {
  const src = app.tabs.find((t) => t.id === id)
  if (!src) return
  const tab = openConsole(src.connId, { schema: src.schema, sql: src.sql, title: src.title + tr(' 복사본') })
  tab.layout = src.layout
  if (src.tableRef) tab.tableRef = $state.snapshot(src.tableRef)
  // place it right after the original
  const from = app.tabs.findIndex((t) => t.id === tab.id)
  const [moved] = app.tabs.splice(from, 1)
  app.tabs.splice(app.tabs.findIndex((t) => t.id === id) + 1, 0, moved)
  saveWorkspaceSoon()
  const copy = app.tabs.find((t) => t.id === tab.id)!
  if (copy.tableRef) await reloadTable(copy)
}

function log(tab: Tab, kind: Message['kind'], text: string, sql?: string) {
  tab.messages.push({ time: now(), kind, text, sql })
  if (tab.messages.length > 500) tab.messages.splice(0, tab.messages.length - 500)
}

/** Adds statements run outside the editor (grid edits, structure changes) to the tab's log. */
export function logStatements(tab: Tab, sql: string[], outcome: string, error = '') {
  const text = sql.map((x) => x.trim().replace(/;$/, '') + ';').join('\n')
  log(tab, error ? 'error' : 'info', error || outcome, text)
}

// ---- keeping the tree in step with DDL

const DDL_RE = /^(?:create|alter|drop|rename|truncate|comment\s+on)\b/i
const SCHEMA_DDL_RE = /^(?:create|drop|alter)\s+(?:database|schema|user)\b/i

function stripLeadingComments(sql: string): string {
  return sql.replace(/^(?:\s+|--[^\n]*(?:\n|$)|\/\*[\s\S]*?\*\/)*/, '')
}

/**
 * After DDL ran: reloads the table lists of the schemas it touched (the
 * given ones, and any whose name appears in the SQL) and the columns of
 * loaded tables named in it. In a tab window the main window does it.
 */
export async function treeChanged(connId: string, schemas: string[], sql: string, schemaList = false) {
  if (tabWindowId) {
    Events.Emit('tree:changed', { connId, schemas, sql, schemaList }).catch(() => {})
    return
  }
  const st = app.conn[connId]
  if (!st || st.status !== 'connected') return
  if (schemaList) await refreshSchemas(connId)
  const words = new Set((sql.toLowerCase().match(/[\p{L}\p{N}_$#]+/gu) ?? []) as string[])
  const named = new Set(schemas.filter(Boolean).map((x) => x.toLowerCase()))
  for (const s of st.schemas) {
    if (s.tables === null) continue
    const key = s.name.toLowerCase()
    if (!named.has(key) && !words.has(key)) continue
    delete app.completion[connId + '|' + s.name]
    await loadTables(connId, s)
    for (const t of s.tables ?? []) {
      if (t.columns === null || !words.has(t.t.name.toLowerCase())) continue
      if (!t.expanded) {
        t.columns = null // loaded again when opened
        continue
      }
      try {
        t.columns = (await api.ListColumns(connId, s.name, t.t)) ?? []
      } catch {
        t.columns = null
      }
    }
  }
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
    const schemaBefore = tab.schema
    if (res.currentSchema) tab.schema = res.currentSchema
    const ddl = results.filter((r) => !r.error && DDL_RE.test(stripLeadingComments(r.sql))).map((r) => stripLeadingComments(r.sql))
    if (ddl.length) treeChanged(tab.connId, [schemaBefore, tab.schema], ddl.join('\n'), ddl.some((x) => SCHEMA_DDL_RE.test(x)))
    for (const r of results) {
      if (r.error) log(tab, 'error', r.error, short(r.sql))
      else if (r.columns && r.columns.length)
        log(tab, 'info', tr('{n}행 조회', { n: r.rows!.length + (r.hasMore ? '+' : '') }) + ` (${r.elapsedMs}ms)`, short(r.sql))
      else log(tab, 'info', (r.rowsAffected >= 0 ? tr('{n}행 영향', { n: r.rowsAffected }) : tr('완료')) + ` (${r.elapsedMs}ms)`, short(r.sql))
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
  if (tabPending(tab) && !(await confirmBox(tr('적용하지 않은 변경 {n}건이 있습니다. 버리고 이동할까요?', { n: tabPending(tab) })))) return false
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

/**
 * Opens a table's data in a tab. With where (following a foreign key), a
 * tab already showing the table gets that filter.
 */
/** A new tab to design a table in (the structure view), made with CREATE TABLE. */
export async function openCreateTable(connId: string, schema: string) {
  if (!(await connect(connId))) return
  const mssql = connById(connId)?.driver === 'mssql'
  const tab = openConsole(connId, { schema, sql: '', title: tr('새 테이블') })
  tab.layout = 'results'
  tab.tableRef = { schema, table: { name: '', type: 'TABLE', schema: mssql ? 'dbo' : '', comment: '', rows: -1 }, where: '', orderBy: '' }
  tab.creating = true
}

/** The designed table was made: the tab becomes that table's tab. */
export function tableCreated(tab: Tab, t: Table) {
  if (!tab.tableRef) return
  tab.creating = false
  tab.tableRef.table = t
  tab.title = t.name
  saveWorkspaceSoon()
  treeChanged(tab.connId, [tab.tableRef.schema], t.name)
}

export async function openTableData(connId: string, schema: string, t: Table, where?: string): Promise<Tab> {
  // The table is already open in a tab (pinned or preview): go there.
  const open = app.tabs.find(
    (x) => x.connId === connId && x.tableRef?.schema === schema && sameTable(x.tableRef.table, t) && !(where !== undefined && x.detached),
  )
  if (open && where !== undefined) {
    app.activeTab = open.id
    if (open.tableRef!.where !== where || !open.results.length) {
      const prev = open.tableRef!.where
      open.tableRef!.where = where
      if (!(await reloadTable(open))) open.tableRef!.where = prev // kept its unsaved edits
    }
    return open
  }
  if (open?.detached) {
    api.FocusTabWindow(open.id)
    return open
  }
  // A preview tab with unsaved edits is never replaced.
  let tab = open ?? app.tabs.find((x) => x.preview && !x.detached && !x.running && tabPending(x) === 0)
  if (tab && tab.connId === connId && tab.tableRef?.schema === schema && sameTable(tab.tableRef.table, t)) {
    app.activeTab = tab.id // same table clicked again: keep its filter
    // (a tab restored from the last session hasn't run yet)
    if (!tab.results.length && !tab.opening) await reloadTable(tab)
    return tab
  }
  const ref: TableRef = { schema, table: t, where: where ?? '', orderBy: '' }
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
    tab.sqlBase = src
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

/**
 * Cmd/Alt+click on a foreign key cell: opens the referenced table filtered
 * to the row the value points at. The tab it came from is pinned so the
 * new one doesn't replace it.
 */
export async function followForeignKey(tab: Tab, r: StmtResult, col: number, value: string, numeric: boolean) {
  const info = r.edit?.info
  const ref = info?.refs?.[col]
  if (!info || !ref) return
  const conn = app.connections.find((c) => c.id === tab.connId)
  // "table.column", or "schema.table.column" when it's in another schema
  const parts = ref.split('.')
  const column = parts.pop()!
  const name = parts.pop()!
  const other = parts.join('.')
  const mssql = conn?.driver === 'mssql' // tree node = database, schema inside it
  const treeSchema = mssql || !other ? info.schema : other
  const tableSchema = mssql ? other || info.table.schema : ''
  const known = app.conn[tab.connId]?.schemas
    .find((s) => s.name === treeSchema)
    ?.tables?.find((n) => n.t.name === name && (!mssql || n.t.schema === tableSchema))?.t
  const table: Table = known ? $state.snapshot(known) : { schema: tableSchema, name, type: 'TABLE', comment: '', rows: -1, size: -1 }
  let lit: string
  if (numeric && /^-?\d+(\.\d+)?$/.test(value)) lit = value
  else {
    let v = value.replace(/'/g, "''")
    if (conn?.driver === 'mysql' || conn?.driver === 'mariadb') v = v.replace(/\\/g, '\\\\')
    lit = `'${v}'`
  }
  let where: string
  try {
    where = `${await api.QuoteIdent(tab.connId, column)} = ${lit}`
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  pinTab(tab)
  await openTableData(tab.connId, treeSchema, table, where)
}

/**
 * Renames a table or view (asks for the name). Tabs showing it follow the
 * new name; the tree is reloaded.
 */
export async function renameTable(connId: string, schema: string, t: Table) {
  const view = t.type.toUpperCase().includes('VIEW')
  const name = (await askText(`${tr(view ? '뷰 이름 바꾸기' : '테이블 이름 바꾸기')} — ${t.name}`, t.name, tr('바꾸기')))?.trim()
  if (!name || name === t.name) return
  let st: string
  try {
    st = await api.RenameTable(connId, schema, $state.snapshot(t), name)
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  toast(`${t.name} → ${name}`)
  for (const tab of retargetTable(connId, schema, t, { name })) logStatements(tab, [st], tr(view ? '뷰 이름 바꿈' : '테이블 이름 바꿈'))
  await treeChanged(connId, [schema], st)
}

/**
 * A schema's tables or routines as a grid (a catalog query, shown with the
 * editor hidden; the same list opens the tab it already has).
 */
export async function openObjectList(connId: string, schema: string, kind: 'tables' | 'routines') {
  const title = `${tr(kind === 'tables' ? '테이블 목록' : '루틴 목록')}: ${schema}`
  const old = app.tabs.find((x) => x.connId === connId && x.title === title && !x.tableRef)
  if (old) {
    if (old.detached) api.FocusTabWindow(old.id)
    else app.activeTab = old.id
    // restored from the last session: results aren't kept
    if (!old.results.length && !old.running) await runSQL(old, old.sql, -1)
    return
  }
  if (!(await connect(connId))) return
  let sql: string
  try {
    sql = await api.ObjectListSQL(connId, schema, kind)
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  const tab = openConsole(connId, { schema, sql, title })
  tab.layout = 'results'
  await runSQL(tab, sql, -1)
}

export const SCHEMA_DRIVERS = ['mysql', 'mariadb', 'postgres', 'mssql']

/** Makes a schema (asks for the name) and opens it in the tree. */
export async function createSchema(connId: string) {
  const name = (await askText(tr('스키마 만들기'), '', tr('만들기')))?.trim()
  if (!name) return
  let st: string
  try {
    st = await api.CreateSchema(connId, name)
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  toast(tr('{name} 만듦', { name }))
  await treeChanged(connId, [], st, true)
  if (tabWindowId) return
  // a filtered tree would hide it: show it by name
  if (!schemaShown(connId, name)) {
    const f = $state.snapshot(schemaFilter(connById(connId)))
    await setSchemaFilter(connId, { ...f, names: [...f.names, name] })
  }
  const s = app.conn[connId]?.schemas.find((x) => x.name === name)
  if (s && !s.expanded) {
    s.expanded = true
    await loadTables(connId, s)
  }
}

/** Drops a schema and everything in it, after asking; tabs of its tables close. */
export async function dropSchema(connId: string, name: string) {
  if (!(await confirmBox(tr('스키마 “{name}”을(를) 삭제할까요? 안에 든 테이블과 데이터가 모두 사라지고 되돌릴 수 없습니다.', { name })))) return
  let st: string
  try {
    st = await api.DropSchema(connId, name)
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  toast(tr('{name} 삭제함', { name }))
  for (const tab of app.tabs.filter((x) => x.connId === connId && x.tableRef?.schema === name)) {
    if (tab.detached) api.CloseTabWindow(tab.id)
    api.CloseSession(tab.id)
    app.tabs.splice(app.tabs.indexOf(tab), 1)
    if (app.activeTab === tab.id) app.activeTab = app.tabs[app.tabs.length - 1]?.id ?? ''
  }
  saveWorkspaceSoon()
  await treeChanged(connId, [], st, true)
}

// Tabs showing a table (its data, structure, DDL).
const tabsOf = (connId: string, schema: string, t: Table) =>
  app.tabs.filter((x) => x.connId === connId && x.tableRef?.schema === schema && sameTable(x.tableRef.table, t))

/** Drops a table or view, after asking; its tabs close. */
export async function dropTable(connId: string, schema: string, t: Table) {
  const view = t.type.toUpperCase().includes('VIEW')
  if (!(await confirmBox(tr(view ? '뷰 “{name}”을(를) 삭제할까요? 되돌릴 수 없습니다.' : '테이블 “{name}”을(를) 삭제할까요? 데이터도 함께 사라지고 되돌릴 수 없습니다.', { name: t.name })))) return
  let st: string
  try {
    st = await api.DropTable(connId, schema, $state.snapshot(t))
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  toast(tr('{name} 삭제함', { name: t.name }))
  for (const tab of tabsOf(connId, schema, t)) {
    // (no asking about unapplied edits: there is nothing to apply them to)
    if (tab.detached) api.CloseTabWindow(tab.id)
    api.CloseSession(tab.id)
    app.tabs.splice(app.tabs.indexOf(tab), 1)
    if (app.activeTab === tab.id) app.activeTab = app.tabs[app.tabs.length - 1]?.id ?? ''
  }
  saveWorkspaceSoon()
  await treeChanged(connId, [schema], st)
}

/** Deletes every row of a table, after asking; its tabs read it again. */
export async function truncateTable(connId: string, schema: string, t: Table) {
  if (!(await confirmBox(tr('테이블 “{name}”의 모든 행을 지울까요? 되돌릴 수 없습니다.', { name: t.name })))) return
  let st: string
  try {
    st = await api.TruncateTable(connId, schema, $state.snapshot(t))
  } catch (e) {
    toast(errText(e), 'error')
    return
  }
  toast(tr('{name} 비움', { name: t.name }))
  for (const tab of tabsOf(connId, schema, t)) {
    logStatements(tab, [st], tr('테이블 비움'))
    if (tab.results.length) reloadTable(tab)
  }
}

/**
 * A table was renamed or its comment changed: the tabs showing it follow
 * (renamed: their data is read again, its SELECT has the old name).
 * Returns those tabs.
 */
export function retargetTable(connId: string, schema: string, t: Table, patch: Partial<Table>): Tab[] {
  const hit: Tab[] = []
  for (const tab of app.tabs) {
    const ref = tab.tableRef
    if (tab.connId !== connId || !ref || ref.schema !== schema || !sameTable(ref.table, t)) continue
    ref.table = { ...$state.snapshot(ref.table), ...patch }
    if (patch.name) {
      if (tab.title === t.name) tab.title = patch.name
      if (!tabPending(tab) && tab.results.length) reloadTable(tab)
    }
    hit.push(tab)
  }
  saveWorkspaceSoon()
  return hit
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
export async function reloadTable(tab: Tab): Promise<boolean> {
  const ref = tab.tableRef
  if (!ref) return false
  if (tabPending(tab) && !(await confirmBox(tr('적용하지 않은 변경 {n}건이 있습니다. 버리고 다시 조회할까요?', { n: tabPending(tab) })))) return false
  // a tab restored from the last session: connect first
  if (!(await connect(tab.connId))) return false
  try {
    tab.sql = await tableSelect(tab)
  } catch (e) {
    toast(errText(e), 'error')
    return false
  }
  saveWorkspaceSoon()
  await runSQL(tab, tab.sql, -1)
  return true
}

export function pinTab(tab: Tab | undefined) {
  if (tab?.preview) {
    tab.preview = false
    saveWorkspaceSoon()
  }
}

// ---------------------------------------------------------------- workspace

// The saved form of a tab (workspace file, and what a tab window gets).
export type SavedTab = Pick<Tab, 'id' | 'title' | 'connId' | 'schema' | 'sql' | 'sqlBase' | 'preview' | 'layout' | 'tableRef'>

function savedTab(t: Tab): SavedTab {
  return $state.snapshot({
    id: t.id,
    title: t.title,
    connId: t.connId,
    schema: t.schema,
    sql: t.sql,
    sqlBase: t.sqlBase,
    preview: t.preview,
    layout: t.layout,
    tableRef: t.tableRef,
  })
}

function tabFromSaved(t: SavedTab): Tab {
  // (saved before sqlBase: what was there counts as kept)
  return { ...t, sqlBase: t.sqlBase ?? t.sql, results: [], activeResult: 0, running: false, loadingMore: false, messages: [], startedAt: 0 }
}

let saveTimer: ReturnType<typeof setTimeout> | undefined
export function saveWorkspaceSoon() {
  clearTimeout(saveTimer)
  // A tab window doesn't own the workspace: it reports its tab to the main
  // window, which saves it.
  if (tabWindowId) {
    saveTimer = setTimeout(syncTabWindow, 400)
    return
  }
  saveTimer = setTimeout(() => {
    const data = {
      activeTab: app.activeTab,
      pageSize: app.pageSize,
      treeView: app.treeView,
      tableOpen: app.tableOpen,
      zoom: app.zoom,
      orderShare: app.orderShare,
      gridColNumbers: app.gridColNumbers,
      ddlView: app.ddlMode,
      theme: app.theme,
      lang: i18n.lang,
      sidebarHidden: app.sidebarHidden,
      tabs: app.tabs.filter((t) => !t.creating).map(savedTab),
    }
    api.SaveWorkspace(JSON.stringify(data)).catch(() => {})
  }, 600)
}

// tabs: false in a tab window, which only takes the settings.
export async function loadWorkspace(tabs = true) {
  try {
    const raw = await api.LoadWorkspace()
    if (!raw) return setLanguage(i18n.lang, false) // (the detected one)
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
    else if (data.ddlView === 'index') app.ddlMode = 'table' // was a view of its own
    if (typeof data.gridColNumbers === 'boolean') app.gridColNumbers = data.gridColNumbers
    setLanguage(isLang(data.lang) ? data.lang : i18n.lang, false)
    if (typeof data.sidebarHidden === 'boolean') app.sidebarHidden = data.sidebarHidden
    if (typeof data.orderShare === 'number' && data.orderShare >= 0.1 && data.orderShare <= 0.8) app.orderShare = data.orderShare
    // (the old per-tab gridZoom is ignored)
    if (!isWeb && typeof data.zoom === 'number' && data.zoom > ZOOM_MIN && data.zoom <= ZOOM_MAX) {
      app.zoom = data.zoom
      applyZoom()
    }
    if (!tabs) return
    let active = data.activeTab
    for (const t of data.tabs ?? []) {
      if (!connById(t.connId)) continue
      // In the browser build every page gets tab ids (and so DB sessions) of
      // its own: two devices open at once mustn't share a session.
      if (isWeb) {
        const id = newId()
        if (t.id === active) active = id
        t.id = id
      }
      app.tabs.push(tabFromSaved(t))
    }
    app.activeTab = app.tabs.find((t) => t.id === active)?.id ?? app.tabs[0]?.id ?? ''
  } catch {
    /* ignore a broken workspace file */
  }
}

// ---------------------------------------------------------------- tab windows
// A tab can move into a window of its own. The main window keeps the tab
// (marked detached: out of the tab strip, no pane) and saves it; the tab window reports
// changes back. Results aren't carried over: table tabs reload, consoles
// start empty.

// Set when this page is a tab window (index.html?tab=<id>).
export const tabWindowId = new URLSearchParams(location.search).get('tab') ?? ''

type TabEvent = { id: string; tab?: SavedTab }

function onEvent<T>(name: string, fn: (data: T, sender: string) => void) {
  try {
    Events.On(name, (ev) => fn(ev.data as T, ev.sender ?? ''))
  } catch {
    /* no runtime (plain browser) */
  }
}

function applySaved(t: Tab, s: SavedTab) {
  t.title = s.title
  t.connId = s.connId
  t.schema = s.schema
  t.sql = s.sql
  t.sqlBase = s.sqlBase
  t.preview = s.preview
  t.layout = s.layout
  t.tableRef = s.tableRef
}

// Main window: move a tab into its own window.
export async function detachTab(id: string) {
  const i = app.tabs.findIndex((t) => t.id === id)
  const t = app.tabs[i]
  if (!t || t.detached) return
  const n = tabPending(t)
  if (n && !(await confirmBox(tr('적용하지 않은 변경 {n}건이 있습니다. 버리고 새 창으로 열까요?', { n })))) return
  if (app.activeTab === id) {
    const rest = app.tabs.filter((x) => !x.detached && x.id !== id)
    const right = app.tabs.slice(i + 1).find((x) => !x.detached)
    app.activeTab = (right ?? rest[rest.length - 1])?.id ?? ''
  }
  t.detached = true
  t.preview = false // never reused for another table while it's away
  await api.OpenTabWindow(id, t.title, JSON.stringify(savedTab(t)))
  saveWorkspaceSoon()
}

function attach(id: string, s?: SavedTab) {
  const t = app.tabs.find((x) => x.id === id)
  if (!t) return
  if (s) applySaved(t, s)
  t.detached = false
  t.results = []
  t.activeResult = 0
  app.activeTab = id
  saveWorkspaceSoon()
  if (t.tableRef && app.conn[t.connId]?.status === 'connected') reloadTable(t)
}

export function initMainWindowEvents() {
  onEvent<TabEvent>('tab:sync', ({ id, tab }) => {
    const t = app.tabs.find((x) => x.id === id)
    if (t && tab) {
      applySaved(t, tab)
      saveWorkspaceSoon()
    }
  })
  onEvent<TabEvent>('tab:restore', ({ id, tab }) => attach(id, tab))
  onEvent<{ connId: string; schemas: string[]; sql: string; schemaList: boolean }>('tree:changed', (d) =>
    treeChanged(d.connId, d.schemas, d.sql, d.schemaList),
  )
  onEvent<TabEvent>('tab:close', ({ id }) => {
    const i = app.tabs.findIndex((x) => x.id === id)
    if (i < 0) return
    app.tabs.splice(i, 1)
    saveWorkspaceSoon()
  })
  initConnEvents()
}

// Keep each window's idea of what is connected in step.
function initConnEvents() {
  onEvent<{ id: string; connected: boolean }>('conn:changed', async ({ id, connected }) => {
    const st = connState(id)
    if (connected && st.status !== 'connected') {
      st.status = 'connected'
      await refreshSchemas(id)
    } else if (!connected && st.status === 'connected') {
      markDisconnected(id)
    }
  })
}

// ---- in the tab window

function syncTabWindow() {
  const t = app.tabs[0]
  if (t) Events.Emit('tab:sync', { id: t.id, tab: savedTab(t) }).catch(() => {})
}

export async function initTabWindow() {
  await loadConnections()
  await loadWorkspace(false)
  const raw = await api.TabWindowState(tabWindowId)
  if (!raw) return
  const t = tabFromSaved(JSON.parse(raw) as SavedTab)
  app.tabs = [t]
  app.activeTab = t.id
  initConnEvents()
  onEvent<string>('tab-window:close', (id) => id === tabWindowId && backToMain())
  const tab = app.tabs[0]
  if (await api.IsConnected(tab.connId).catch(() => false)) {
    await connect(tab.connId)
    if (tab.tableRef) reloadTable(tab)
  }
}

// Puts the tab back into the main window and closes this one.
export async function backToMain() {
  const t = app.tabs[0]
  if (!t) return
  const n = tabPending(t)
  if (n && !(await confirmBox(tr('적용하지 않은 변경 {n}건이 있습니다. 버리고 메인 창으로 옮길까요?', { n })))) return
  clearTimeout(saveTimer)
  await Events.Emit('tab:restore', { id: t.id, tab: savedTab(t) }).catch(() => {})
  api.CloseTabWindow(t.id)
}

// Closes the tab (and this window).
export async function closeTabWindow() {
  const t = app.tabs[0]
  if (!t) return
  if (!(await okToClose(t))) return
  clearTimeout(saveTimer)
  api.CloseSession(t.id)
  await Events.Emit('tab:close', { id: t.id }).catch(() => {})
  api.CloseTabWindow(t.id)
}

// ---------------------------------------------------------------- browser build

export function toggleSidebar() {
  app.sidebarHidden = !app.sidebarHidden
  saveWorkspaceSoon()
}

// A page's DB sessions stay open while it is: it touches them every few
// minutes, and the server closes ones nobody touched for half an hour.
// Closing the page releases them right away.
export function initWebSessions() {
  const ids = () => app.tabs.map((t) => t.id)
  setInterval(() => {
    api.TouchSessions(ids()).catch((e) => loginExpired(e))
  }, 5 * 60_000)
  addEventListener('pagehide', () => {
    navigator.sendBeacon('/web/release', JSON.stringify(ids()))
  })
}
