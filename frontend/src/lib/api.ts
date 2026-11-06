import * as App from '../../bindings/tablume/internal/app/app'
import { t } from './i18n.svelte'
import { isWeb } from './platform'
import { downloadText, uploadText, uploadBytes, downloadURL } from './webfiles.svelte'
import type { Routine, Column, ConnConfig, ExecResponse, SchemaList, SchemaFilter, Table, Cell, EditInfo, RowChange, Index, NewIndex, TableFK, LogEntry, ExportRequest } from './types'

export interface AlterResult {
  applied: number
  total: number
  error: string
  rolledBack: boolean
}

export interface IndexRequest {
  connId: string
  schema: string
  table: Table
  drop: Index[]
  create: NewIndex[]
}

export interface FKRequest {
  connId: string
  schema: string
  table: Table
  drop: TableFK[]
  create: TableFK[]
}

export interface CreateRequest {
  connId: string
  schema: string
  table: Table
  columns: Column[]
  engine: string
}

export interface AlterRequest {
  connId: string
  schema: string
  table: Table
  changes: { old: Column; new: Column }[]
  add: Column[]
  order?: string[]
  drop?: string[]
  tableComment?: string
  rename?: string
  engine?: string
}

export interface TableOptions {
  engine: string
  engines: string[] | null
  columns: Record<string, { collation: string; onUpdate: string; invisible: boolean }> | null
  collations: string[] | null
}

// Typed view over the generated Wails bindings, using our own types
// (the generated ones mark every slice and pointer as nullable).
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
  PreviewCreateTable(req: CreateRequest): Promise<string[] | null>
  CreateTable(req: CreateRequest): Promise<{ table: Table; result: AlterResult }>
  TableOptions(id: string, schema: string, t: Table): Promise<TableOptions>
  ApplyAlter(req: AlterRequest): Promise<AlterResult>
  ListIndexes(id: string, schema: string, t: Table): Promise<Index[] | null>
  PreviewIndexes(req: IndexRequest): Promise<string[] | null>
  ApplyIndexes(req: IndexRequest): Promise<AlterResult>
  ListForeignKeys(id: string, schema: string, t: Table): Promise<TableFK[] | null>
  PreviewForeignKeys(req: FKRequest): Promise<string[] | null>
  ApplyForeignKeys(req: FKRequest): Promise<AlterResult>
  RenameTable(id: string, schema: string, t: Table, name: string): Promise<string>
  AppendLog(id: string, entries: LogEntry[]): Promise<void>
  ReadLog(id: string, q: string, limit: number): Promise<LogEntry[] | null>
  ClearLog(id: string): Promise<void>
  ObjectListSQL(id: string, schema: string, kind: 'tables' | 'routines'): Promise<string>
  CreateSchema(id: string, name: string): Promise<string>
  DropSchema(id: string, name: string): Promise<string>
  DropTable(id: string, schema: string, t: Table): Promise<string>
  TruncateTable(id: string, schema: string, t: Table): Promise<string>
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
    confirmed?: boolean
    manual?: boolean
  }): Promise<ExecResponse>
  FetchMore(tabId: string, cursorId: string, n: number): Promise<{ rows: Cell[][] | null; hasMore: boolean }>
  Cancel(tabId: string): Promise<void>
  CloseSession(tabId: string): Promise<void>
  EndTx(tabId: string, commit: boolean): Promise<void>
  // path '' = cancelled; rows -1 in the browser build (a download started)
  ExportData(req: ExportRequest): Promise<{ path: string; rows: number }>
  TxOpen(tabId: string): Promise<boolean>
  PickSQLiteFile(): Promise<string>
  PickKeyFile(): Promise<string>
  SaveTextFile(name: string, content: string): Promise<string>
  OpenSQLFile(): Promise<{ name: string; content: string } | null>
  LoadWorkspace(): Promise<string>
  SaveWorkspace(data: string): Promise<void>
  SetDarkTitleBar(dark: boolean): Promise<void>
  IsConnected(id: string): Promise<boolean>
  SupportedDrivers(): Promise<string[]>
  OpenTabWindow(tabId: string, title: string, state: string): Promise<void>
  TabWindowState(tabId: string): Promise<string>
  CloseTabWindow(tabId: string): Promise<void>
  FocusTabWindow(tabId: string): Promise<boolean>
  BrowseFiles(dir: string): Promise<FileList>
  TouchSessions(tabIds: string[]): Promise<void>
  SetLanguage(lang: string): Promise<void>
  Version(): Promise<string>
}

export interface FileList {
  root: string
  dir: string
  entries: { name: string; dir: boolean; size: number; modified: string }[]
}

export interface ApplyRequest {
  connId: string
  schema: string
  table: Table
  changes: RowChange[]
  tabId?: string
  manual?: boolean
}

const native = App as unknown as Api

// In the browser build, saving downloads the file and opening uploads one.
export const api: Api = {
  ...native,
  SaveTextFile: (name, content) => (isWeb ? Promise.resolve(downloadText(name, content)) : native.SaveTextFile(name, content)),
  OpenSQLFile: () => (isWeb ? uploadText('.sql,.txt,text/plain') : native.OpenSQLFile()),
  ExportData: (req) =>
    isWeb
      ? Promise.resolve((downloadURL('/x/export?req=' + encodeURIComponent(JSON.stringify(req))), { path: '', rows: -1 }))
      : native.ExportData(req),
}

/** Picks a data file (CSV) to import, as bytes. */
export async function openDataFile(): Promise<{ name: string; bytes: Uint8Array } | null> {
  if (isWeb) return uploadBytes('.csv,.tsv,.txt,text/csv,text/plain')
  const f = (await (App as unknown as { OpenDataFile(): Promise<{ name: string; data: string } | null> }).OpenDataFile()) ?? null
  return f && { name: f.name, bytes: Uint8Array.from(atob(f.data ?? ''), (c) => c.charCodeAt(0)) }
}

// The browser build's server answers this once the sign-in has expired.
const LOGIN_REQUIRED = 'tablume: login required'
let reloading = false

export function loginExpired(e: unknown): boolean {
  if (!isWeb || !String(e instanceof Error ? e.message : e).includes(LOGIN_REQUIRED)) return false
  if (!reloading) {
    reloading = true
    location.reload() // the server sends the page to the sign-in page
  }
  return true
}

export function errText(e: unknown): string {
  if (loginExpired(e)) return t('로그인이 만료되었습니다. 다시 로그인하세요.')
  if (e instanceof Error) return e.message
  return String(e)
}
