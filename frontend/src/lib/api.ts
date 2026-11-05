import * as App from '../../bindings/tablume/internal/app/app'
import { t } from './i18n.svelte'
import { isWeb } from './platform'
import { downloadText, uploadText } from './webfiles.svelte'
import type { Routine, Column, ConnConfig, ExecResponse, SchemaList, SchemaFilter, Table, Cell, EditInfo, RowChange, Index, NewIndex } from './types'

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

export interface AlterRequest {
  connId: string
  schema: string
  table: Table
  changes: { old: Column; new: Column }[]
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
  ApplyAlter(req: AlterRequest): Promise<AlterResult>
  ListIndexes(id: string, schema: string, t: Table): Promise<Index[] | null>
  PreviewIndexes(req: IndexRequest): Promise<string[] | null>
  ApplyIndexes(req: IndexRequest): Promise<AlterResult>
  RenameTable(id: string, schema: string, t: Table, name: string): Promise<string>
  CountRows(id: string, schema: string, t: Table, where: string): Promise<number>
  FetchPage(tabId: string, connId: string, sql: string, offset: number, n: number): Promise<FetchedPage>
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
  FetchMore(tabId: string, cursorId: string, n: number): Promise<FetchedPage>
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

// A page of rows from FetchPage / FetchMore, with its time (see StmtResult).
export interface FetchedPage {
  rows: Cell[][] | null
  hasMore: boolean
  elapsedMs: number
  fetchMs: number
  sentSql: string // the page's query; '' when rows came from the open cursor
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
}

const native = App as unknown as Api

// In the browser build, saving downloads the file and opening uploads one.
export const api: Api = {
  ...native,
  SaveTextFile: (name, content) => (isWeb ? Promise.resolve(downloadText(name, content)) : native.SaveTextFile(name, content)),
  OpenSQLFile: () => (isWeb ? uploadText('.sql,.txt,text/plain') : native.OpenSQLFile()),
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
