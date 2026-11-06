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
  readOnly?: boolean
  ssh?: boolean
  sshHost?: string
  sshPort?: number
  sshUser?: string
  sshAuth?: 'password' | 'key'
  sshPassword?: string // password, or key passphrase
  sshKeyFile?: string
  sshHostKey?: string // pinned SHA256 fingerprint
  schemas?: SchemaFilter // schemas shown in the tree; unset = all but system ones
}

export interface SchemaFilter {
  all: boolean // every non-system schema
  current: boolean // the session's default schema
  names: string[]
  patterns: string[] // * and ?, case-insensitive
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
  unique?: boolean
  fk?: string // referenced "table.column"
  default?: string
  extra?: string // auto increment / identity / generated
  collation?: string // MySQL/MariaDB, in column changes
  onUpdate?: string
  invisible?: boolean
}

// ERD data: a schema's tables and the foreign keys between them.
export interface GraphTable {
  name: string
  view: boolean
  comment: string
  columns: Column[]
}

export interface ForeignKey {
  name: string
  table: string
  columns: string[]
  refSchema: string // '' = the same schema
  refTable: string
  refColumns: string[]
  onUpdate: string
  onDelete: string
  one: boolean // child FK columns unique: 1:1
  required: boolean // FK columns NOT NULL: the parent must exist
  looseRef: boolean // referenced columns not unique: in effect N:M
}

export interface SchemaGraph {
  tables: GraphTable[]
  fks: ForeignKey[]
}

export interface TableFK {
  name: string
  columns: string[]
  refTable: Table
  refSchema: string // "" = the same schema
  refColumns: string[]
  onUpdate: string // "" where the database has no ON UPDATE (Oracle)
  onDelete: string
}

export interface Index {
  name: string
  schema: string // the index's own schema when it differs (Oracle)
  columns: string[] // key columns as shown: "a", "b DESC", "c(10)", expressions
  include: string[] | null // non-key columns (SQL Server, PostgreSQL)
  unique: boolean
  primary: boolean
  constraint: string // PRIMARY KEY / UNIQUE constraint it backs, '' = plain index
  type: string
  where: string // partial / filtered index
}

export interface NewIndex {
  name: string
  columns: { name: string; desc: boolean }[]
  unique: boolean
}

export interface Routine {
  schema: string
  name: string
  type: string // PROCEDURE, FUNCTION, PACKAGE
  args: string // PostgreSQL overload arguments
  id: string
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
  fetchMs: number // the part of elapsedMs spent reading rows
  sentSql?: string // the statement as sent when it differs (paged: LIMIT/OFFSET added)
  // Measured here: the app's own share of the wait (the round trip between
  // the window and the Go side, the session's bookkeeping) and the time
  // until the grid was painted.
  appMs?: number
  renderMs?: number
  error: string
  notice: string
  source: string[] | null
  // More rows come from re-running the query one page at a time
  // (FetchPage) instead of an open cursor.
  paged?: boolean
  // client-side only
  edit?: EditState
  start?: number // absolute number of the first loaded row (paged results)
  total?: number | null // row count, once known
}

export interface EditInfo {
  editable: boolean
  insertable?: boolean // new rows allowed even when not editable (no PK)
  reason: string
  schema: string
  table: Table
  columns: string[] // table column per result column, '' = read-only
  key: string[]
  keys?: string[] // per result column: "PK", "UK", "FK" (comma separated)
  refs?: string[] // per result column: FK target "table.column"
  comments?: string[] // per result column
  fills?: string[] // per result column: what a new row gets when left out: 'generated' | 'default' | ''
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
  confirm?: string[] | null
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

// An entry of a connection's saved run log.
export interface LogEntry {
  time: number // Unix ms
  tab: string
  kind: 'info' | 'error'
  text: string
  sql: string
  auto?: boolean // made by the app (a table tab's SELECT, a page read on)
}
