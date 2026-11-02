import * as App from '../../bindings/tablume/app'
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
  SupportedDrivers(): Promise<string[]>
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

export const api = App as unknown as Api

export function errText(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}
