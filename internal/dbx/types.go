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
}

type Table struct {
	// Schema is the schema inside the tree node. Only used by mssql, where
	// the tree node is a database and tables live in dbo, sales, ...
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"` // TABLE, VIEW, ...
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	PK       bool   `json:"pk"`
}

type SchemaList struct {
	Schemas []string `json:"schemas"`
	Current string   `json:"current"`
}

type ResultColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type StmtResult struct {
	SQL          string         `json:"sql"`
	From         int            `json:"from"` // UTF-16 offsets into the submitted text
	To           int            `json:"to"`
	Columns      []ResultColumn `json:"columns"`
	Rows         [][]any        `json:"rows"`
	HasMore      bool           `json:"hasMore"`
	CursorID     string         `json:"cursorId"`
	RowsAffected int64          `json:"rowsAffected"` // -1 when unknown
	ElapsedMs    int64          `json:"elapsedMs"`
	Error        string         `json:"error"`
	Notice       string         `json:"notice"`
}

type ExecRequest struct {
	TabID  string `json:"tabId"`
	ConnID string `json:"connId"`
	Schema string `json:"schema"`
	SQL    string `json:"sql"`
	// Cursor is the caret position (UTF-16). -1 runs every statement in SQL.
	Cursor int `json:"cursor"`
	Fetch  int `json:"fetch"`
}

type ExecResponse struct {
	Results       []StmtResult `json:"results"`
	CurrentSchema string       `json:"currentSchema"`
}

type FetchResponse struct {
	Rows    [][]any `json:"rows"`
	HasMore bool    `json:"hasMore"`
}
