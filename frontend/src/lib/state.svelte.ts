import { api, errText } from './api'
import type { Column, ConnConfig, StmtResult, Table } from './types'

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
}

export interface ConnState {
  status: 'idle' | 'connecting' | 'connected' | 'error'
  version?: string
  error?: string
  expanded: boolean
  schemas: SchemaNode[]
  current: string
  showAllSchemas: boolean
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
  loadingMore: boolean
  messages: Message[]
  startedAt: number
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
  fetchSize: 500,
  editing: null as ConnConfig | null,
  prompt: null as null | { title: string; resolve: (v: string | null) => void },
  confirm: null as null | { text: string; resolve: (v: boolean) => void },
  toasts: [] as Toast[],
  completion: {} as Record<string, Record<string, string[]>>,
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
    app.conn[id] = { status: 'idle', expanded: false, schemas: [], current: '', showAllSchemas: false }
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
    for (const s of st.schemas) if (s.expanded) await loadTables(id, s)
    const cur = st.schemas.find((s) => s.name === st.current)
    if (cur && !cur.expanded && st.schemas.every((s) => !s.expanded)) {
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
  } catch (e) {
    s.error = errText(e)
  } finally {
    s.loading = false
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

export function closeTab(id: string) {
  const i = app.tabs.findIndex((t) => t.id === id)
  if (i < 0) return
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
      fetch: app.fetchSize,
    })
    const results = (res.results ?? []).map((r) => ({
      ...r,
      from: r.from + offset,
      to: r.to + offset,
      rows: r.rows ?? [],
      columns: r.columns ?? [],
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

export async function fetchMore(tab: Tab, r: StmtResult) {
  if (!r.hasMore || !r.cursorId || tab.loadingMore) return
  tab.loadingMore = true
  try {
    const res = await api.FetchMore(tab.id, r.cursorId, app.fetchSize)
    r.rows!.push(...(res.rows ?? []))
    r.hasMore = res.hasMore
  } catch (e) {
    r.hasMore = false
    toast(errText(e), 'error')
  } finally {
    tab.loadingMore = false
  }
}

export function cancel(tab: Tab) {
  api.Cancel(tab.id)
}

export async function openTableData(connId: string, schema: string, t: Table) {
  const sql = await api.SelectSQL(connId, schema, t, 1000)
  const tab = openConsole(connId, { schema, sql, title: t.name })
  await runSQL(tab, sql, -1)
}

// ---------------------------------------------------------------- workspace

let saveTimer: ReturnType<typeof setTimeout> | undefined
export function saveWorkspaceSoon() {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    const data = {
      activeTab: app.activeTab,
      fetchSize: app.fetchSize,
      tabs: app.tabs.map((t) => ({ id: t.id, title: t.title, connId: t.connId, schema: t.schema, sql: t.sql })),
    }
    api.SaveWorkspace(JSON.stringify(data)).catch(() => {})
  }, 600)
}

export async function loadWorkspace() {
  try {
    const raw = await api.LoadWorkspace()
    if (!raw) return
    const data = JSON.parse(raw)
    if (data.fetchSize) app.fetchSize = data.fetchSize
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
