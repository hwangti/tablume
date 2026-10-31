import * as App from '../../wailsjs/go/main/App'
import type { Column, ConnConfig, ExecResponse, SchemaList, Table, Cell, EditInfo, RowChange } from './types'

// Typed view over the generated Wails bindings (they use classes; plain
// objects work the same at runtime).
interface Api {
  ListConnections(): Promise<ConnConfig[]>
  SaveConnection(c: ConnConfig): Promise<ConnConfig>
  DeleteConnection(id: string): Promise<void>
  TestConnection(c: ConnConfig): Promise<{ version: string; sshHostKey: string }>
  NeedsPassword(id: string): Promise<boolean>
  Connect(id: string, password: string): Promise<string>
  Disconnect(id: string): Promise<void>
  ListSchemas(id: string): Promise<SchemaList>
  ListTables(id: string, schema: string): Promise<Table[]>
  ListColumns(id: string, schema: string, t: Table): Promise<Column[]>
  CompletionColumns(id: string, schema: string): Promise<Record<string, string[]>>
  SelectSQL(id: string, schema: string, t: Table, where: string, orderBy: string, limit: number): Promise<string>
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
