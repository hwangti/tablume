import { invoke } from '@tauri-apps/api/core'
import type { Routine, Column, ConnConfig, ExecResponse, SchemaList, SchemaFilter, Table, Cell, EditInfo, RowChange } from './types'

export interface AlterRequest {
  connId: string
  schema: string
  table: Table
  changes: { old: Column; new: Column }[]
}

// The backend commands (src-tauri/src/lib.rs), with the Go app's names.
interface Api {
  ListConnections(): Promise<ConnConfig[]>
  SaveConnection(c: ConnConfig): Promise<ConnConfig>
  DeleteConnection(id: string): Promise<void>
  SetSchemaFilter(id: string, f: SchemaFilter | null): Promise<void>
  ReorderConnections(ids: string[]): Promise<void>
  TestConnection(c: ConnConfig): Promise<{ version: string; sshHostKey: string }>
  NeedsPassword(id: string): Promise<boolean>
  Connect(id: string, password: string): Promise<string>
  Disconnect(id: string): Promise<void>
  ListSchemas(id: string): Promise<SchemaList>
  ListTables(id: string, schema: string): Promise<Table[]>
  TableStats(id: string, schema: string): Promise<Table[]>
  ListColumns(id: string, schema: string, t: Table): Promise<Column[]>
  CompletionColumns(id: string, schema: string): Promise<Record<string, string[]>>
  SelectSQL(id: string, schema: string, t: Table, where: string, orderBy: string, limit: number, offset: number): Promise<string>
  ListRoutines(id: string, schema: string): Promise<Routine[]>
  RoutineSource(id: string, schema: string, r: Routine): Promise<string>
  PreviewAlter(req: AlterRequest): Promise<string[]>
  ApplyAlter(req: AlterRequest): Promise<{ applied: number; total: number; error: string; rolledBack: boolean }>
  CountRows(id: string, schema: string, t: Table, where: string): Promise<number>
  FetchPage(tabId: string, connId: string, sql: string, offset: number, n: number): Promise<{ rows: Cell[][] | null; hasMore: boolean }>
  CountResult(tabId: string, connId: string, sql: string): Promise<number>
  QuoteIdent(id: string, name: string): Promise<string>
  QualifiedName(id: string, schema: string, t: Table): Promise<string>
  TableDDL(id: string, schema: string, t: Table): Promise<string>
  EditInfo(id: string, schema: string, source: string[], columns: string[]): Promise<EditInfo>
  PreviewChanges(req: ApplyRequest): Promise<string[]>
  ApplyChanges(req: ApplyRequest): Promise<number>
  Execute(req: {
    tabId: string
    connId: string
    schema: string
    sql: string
    cursor: number
    fetch: number
  }): Promise<ExecResponse>
  FetchMore(tabId: string, cursorId: string, n: number): Promise<{ rows: Cell[][] | null; hasMore: boolean }>
  Cancel(tabId: string): Promise<void>
  CloseSession(tabId: string): Promise<void>
  PickSQLiteFile(): Promise<string>
  PickKeyFile(): Promise<string>
  SaveTextFile(name: string, content: string): Promise<string>
  OpenSQLFile(): Promise<{ name: string; content: string } | null>
  LoadWorkspace(): Promise<string>
  SaveWorkspace(data: string): Promise<void>
  SetDarkTitleBar(dark: boolean): Promise<void>
  IsConnected(id: string): Promise<boolean>
  OpenTabWindow(tabId: string, title: string, state: string): Promise<void>
  TabWindowState(tabId: string): Promise<string>
  CloseTabWindow(tabId: string): Promise<void>
  FocusTabWindow(tabId: string): Promise<boolean>
}

export interface ApplyRequest {
  connId: string
  schema: string
  table: Table
  changes: RowChange[]
}

const call = <T>(cmd: string, args?: Record<string, unknown>) => invoke<T>(cmd, args)

export const api: Api = {
  ListConnections: () => call('list_connections'),
  SaveConnection: (c) => call('save_connection', { c }),
  DeleteConnection: (id) => call('delete_connection', { id }),
  SetSchemaFilter: (id, f) => call('set_schema_filter', { id, f }),
  ReorderConnections: (ids) => call('reorder_connections', { ids }),
  TestConnection: (c) => call('test_connection', { c }),
  NeedsPassword: (id) => call('needs_password', { id }),
  Connect: (id, password) => call('connect', { id, password }),
  Disconnect: (id) => call('disconnect', { id }),
  IsConnected: (id) => call('is_connected', { id }),
  ListSchemas: (id) => call('list_schemas', { id }),
  ListTables: (id, schema) => call('list_tables', { id, schema }),
  TableStats: (id, schema) => call('table_stats', { id, schema }),
  ListColumns: (id, schema, t) => call('list_columns', { id, schema, t }),
  CompletionColumns: (id, schema) => call('completion_columns', { id, schema }),
  SelectSQL: (id, schema, t, cond, orderBy, limit, offset) => call('select_sql', { id, schema, t, cond, orderBy, limit, offset }),
  ListRoutines: (id, schema) => call('list_routines', { id, schema }),
  RoutineSource: (id, schema, r) => call('routine_source', { id, schema, r }),
  PreviewAlter: (req) => call('preview_alter', { req }),
  ApplyAlter: (req) => call('apply_alter', { req }),
  CountRows: (id, schema, t, cond) => call('count_rows', { id, schema, t, cond }),
  FetchPage: (tabId, connId, sql, offset, n) => call('fetch_page', { tabId, connId, sql, offset, n }),
  CountResult: (tabId, connId, sql) => call('count_result', { tabId, connId, sql }),
  QuoteIdent: (id, name) => call('quote_ident', { id, name }),
  QualifiedName: (id, schema, t) => call('qualified_name', { id, schema, t }),
  TableDDL: (id, schema, t) => call('table_ddl', { id, schema, t }),
  EditInfo: (id, schema, source, columns) => call('edit_info', { id, schema, source, columns }),
  PreviewChanges: (req) => call('preview_changes', { req }),
  ApplyChanges: (req) => call('apply_changes', { req }),
  Execute: (req) => call('execute', { req }),
  FetchMore: (tabId, cursorId, n) => call('fetch_more', { tabId, cursorId, n }),
  Cancel: (tabId) => call('cancel', { tabId }),
  CloseSession: (tabId) => call('close_session', { tabId }),
  PickSQLiteFile: () => call('pick_sqlite_file'),
  PickKeyFile: () => call('pick_key_file'),
  SaveTextFile: (name, content) => call('save_text_file', { name, content }),
  OpenSQLFile: () => call('open_sql_file'),
  LoadWorkspace: () => call('load_workspace'),
  SaveWorkspace: (data) => call('save_workspace', { data }),
  SetDarkTitleBar: (dark) => call('set_dark_title_bar', { dark }),
  OpenTabWindow: (tabId, title, state) => call('open_tab_window', { tabId, title, state }),
  TabWindowState: (tabId) => call('tab_window_state', { tabId }),
  CloseTabWindow: (tabId) => call('close_tab_window', { tabId }),
  FocusTabWindow: (tabId) => call('focus_tab_window', { tabId }),
}

export function errText(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}
