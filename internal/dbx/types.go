package dbx

// ConnConfig describes one saved connection. Password is only carried in
// memory / over the bridge; the store keeps it in the OS keychain.
type ConnConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Driver       string `json:"driver"` // mysql, mariadb, postgres, mssql, oracle, sqlite
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Password     string `json:"password,omitempty"`
	SavePassword bool   `json:"savePassword"`
	// Database is the default database (mysql/pg/mssql), the service name or
	// SID (oracle), or the file path (sqlite).
	Database  string `json:"database"`
	OracleSID bool   `json:"oracleSid"`
	// Params are extra driver options as "key=value" pairs separated by '&'.
	Params string `json:"params"`
	Color  string `json:"color"`
	// ReadOnly runs only statements that read; edits and DDL are refused,
	// and where the database has it the session is read-only too.
	ReadOnly bool `json:"readOnly"`

	// SSH tunnel: Host/Port above are then as seen from the SSH server.
	SSH     bool   `json:"ssh"`
	SSHHost string `json:"sshHost"`
	SSHPort int    `json:"sshPort"`
	SSHUser string `json:"sshUser"`
	SSHAuth string `json:"sshAuth"` // password | key
	// SSHPassword is the password, or the key passphrase with key auth.
	SSHPassword string `json:"sshPassword,omitempty"`
	SSHKeyFile  string `json:"sshKeyFile"`
	// SSHHostKey pins the server's key (SHA256 fingerprint), set on first use.
	SSHHostKey string `json:"sshHostKey"`

	// Schemas picks the schemas shown in the tree; nil shows all but the
	// system ones.
	Schemas *SchemaFilter `json:"schemas,omitempty"`
}

// SchemaFilter is the set of schemas (databases on MySQL and SQL Server)
// shown under a connection.
type SchemaFilter struct {
	All      bool     `json:"all"`      // every non-system schema
	Current  bool     `json:"current"`  // the session's default schema
	Names    []string `json:"names"`    // picked one by one
	Patterns []string `json:"patterns"` // globs with * and ?, case-insensitive
}

type Table struct {
	// Schema is the schema inside the tree node. Only used by mssql, where
	// the tree node is a database and tables live in dbo, sales, ...
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"` // TABLE, VIEW, ...
	// From the catalog statistics, not COUNT(*): approximate, -1 if unknown.
	Comment string `json:"comment"`
	Rows    int64  `json:"rows"`
	Size    int64  `json:"size"` // bytes, -1 if unknown
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	PK       bool   `json:"pk"`
	Comment  string `json:"comment"`
	Unique   bool   `json:"unique"` // in a unique constraint/index (not the PK)
	FK       string `json:"fk"`     // referenced "table.column" ("schema.table.column" in another schema)
	Default  string `json:"default"`
	// Extra: auto increment / identity / generated, for the structure view.
	Extra string `json:"extra"`
	// MySQL/MariaDB, in column changes only (see TableOptions.Columns):
	// the collation and the ON UPDATE expression. Old and New differ where
	// they change; equal (or both "") keeps what the column has.
	Collation string `json:"collation,omitempty"`
	OnUpdate  string `json:"onUpdate,omitempty"`
	Invisible bool   `json:"invisible,omitempty"` // left out of SELECT *
}

type SchemaList struct {
	Schemas []string `json:"schemas"`
	Current string   `json:"current"`
	// Comments of the schemas that have one (MariaDB, PostgreSQL, SQL Server).
	Comments map[string]string `json:"comments"`
}

type ResultColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type StmtResult struct {
	SQL      string         `json:"sql"`
	From     int            `json:"from"` // UTF-16 offsets into the submitted text
	To       int            `json:"to"`
	Columns  []ResultColumn `json:"columns"`
	Rows     [][]any        `json:"rows"`
	HasMore  bool           `json:"hasMore"`
	CursorID string         `json:"cursorId"`
	// Paged: more rows come from FetchPage (the query re-run with an
	// offset) rather than from an open cursor.
	Paged        bool   `json:"paged"`
	RowsAffected int64  `json:"rowsAffected"` // -1 when unknown
	ElapsedMs    int64  `json:"elapsedMs"`
	Error        string `json:"error"`
	Notice       string `json:"notice"`
	// Source is the table a plain single-table SELECT reads from, as written.
	Source []string `json:"source"`
}

type ExecRequest struct {
	TabID  string `json:"tabId"`
	ConnID string `json:"connId"`
	Schema string `json:"schema"`
	SQL    string `json:"sql"`
	// Cursor is the caret position (UTF-16). -1 runs every statement in SQL.
	Cursor int `json:"cursor"`
	Fetch  int `json:"fetch"`
	// Confirmed runs UPDATE/DELETE without WHERE; otherwise they come
	// back in ExecResponse.Confirm and nothing runs.
	Confirmed bool `json:"confirmed"`
	// Manual: changes wait for EndTx instead of committing (txn.go).
	Manual bool `json:"manual"`
}

type ExecResponse struct {
	Results       []StmtResult `json:"results"`
	CurrentSchema string       `json:"currentSchema"`
	Confirm       []string     `json:"confirm"`
	InTx          bool         `json:"inTx"` // manual commit: changes wait for Commit or Rollback
}

type FetchResponse struct {
	Rows    [][]any `json:"rows"`
	HasMore bool    `json:"hasMore"`
}
