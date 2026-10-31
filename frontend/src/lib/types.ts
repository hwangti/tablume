export type Driver = 'mysql' | 'mariadb' | 'postgres' | 'mssql' | 'oracle' | 'sqlite'

export interface ConnConfig {
  id: string
  name: string
  driver: Driver
  host: string
  port: number
  user: string
  password?: string
  savePassword: boolean
  database: string
  oracleSid: boolean
  params: string
  color: string
}

export interface Table {
  schema: string
  name: string
  type: string
  comment?: string
  rows?: number // estimate from statistics, -1 unknown
  size?: number // bytes, -1 unknown
}

export interface Column {
  name: string
  type: string
  nullable: boolean
  pk: boolean
  comment?: string
}

export interface SchemaList {
  schemas: string[]
  current: string
}

export interface ResultColumn {
  name: string
  type: string
}

export type Cell = string | null

export interface StmtResult {
  sql: string
  from: number
  to: number
  columns: ResultColumn[] | null
  rows: Cell[][] | null
  hasMore: boolean
  cursorId: string
  rowsAffected: number
  elapsedMs: number
  error: string
  notice: string
  source: string[] | null
  // client-side only
  edit?: EditState
}

export interface EditInfo {
  editable: boolean
  reason: string
  schema: string
  table: Table
  columns: string[] // table column per result column, '' = read-only
  key: string[]
}

export interface EditState {
  info: EditInfo | null
  loading: boolean
  edits: Record<number, Record<number, Cell>> // row -> col -> new value
  deleted: Record<number, true>
  added: Record<number, true>
}

export interface RowChange {
  kind: 'update' | 'delete' | 'insert'
  key: Record<string, Cell>
  values: Record<string, Cell>
}

export interface ExecResponse {
  results: StmtResult[]
  currentSchema: string
}

export const DRIVERS: { id: Driver; label: string; port: number }[] = [
  { id: 'mysql', label: 'MySQL', port: 3306 },
  { id: 'mariadb', label: 'MariaDB', port: 3306 },
  { id: 'oracle', label: 'Oracle', port: 1521 },
  { id: 'mssql', label: 'SQL Server', port: 1433 },
  { id: 'postgres', label: 'PostgreSQL', port: 5432 },
  { id: 'sqlite', label: 'SQLite', port: 0 },
]

export function driverLabel(d: Driver): string {
  return DRIVERS.find((x) => x.id === d)?.label ?? d
}
