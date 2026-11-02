//! Data passed to and from the frontend (same JSON shapes as the Go app).

use serde::{Deserialize, Serialize};

fn is_empty(s: &str) -> bool {
    s.is_empty()
}

/// One saved connection. Passwords only travel in memory; the store keeps
/// them in the OS keychain.
#[derive(Serialize, Deserialize, Clone, Default, Debug)]
#[serde(rename_all = "camelCase", default)]
pub struct ConnConfig {
    pub id: String,
    pub name: String,
    pub driver: String,
    pub host: String,
    pub port: i64,
    pub user: String,
    #[serde(skip_serializing_if = "is_empty")]
    pub password: String,
    pub save_password: bool,
    pub database: String,
    pub oracle_sid: bool,
    pub params: String,
    pub color: String,
    pub ssh: bool,
    pub ssh_host: String,
    pub ssh_port: i64,
    pub ssh_user: String,
    pub ssh_auth: String,
    #[serde(skip_serializing_if = "is_empty")]
    pub ssh_password: String,
    pub ssh_key_file: String,
    pub ssh_host_key: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub schemas: Option<SchemaFilter>,
}

#[derive(Serialize, Deserialize, Clone, Default, Debug)]
#[serde(default)]
pub struct SchemaFilter {
    pub all: bool,
    pub current: bool,
    pub names: Vec<String>,
    pub patterns: Vec<String>,
}

#[derive(Serialize, Deserialize, Clone, Default, Debug)]
#[serde(default)]
pub struct Table {
    pub schema: String,
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub comment: String,
    pub rows: i64,
    pub size: i64,
}

#[derive(Serialize, Deserialize, Clone, Default, Debug)]
#[serde(default)]
pub struct Column {
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub nullable: bool,
    pub pk: bool,
    pub comment: String,
    pub unique: bool,
    pub fk: String,
    pub default: String,
    pub extra: String,
}

#[derive(Serialize, Default)]
pub struct SchemaList {
    pub schemas: Vec<String>,
    pub current: String,
}

#[derive(Serialize, Clone)]
pub struct ResultColumn {
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
}

/// A grid value: text, or None for NULL.
pub type Cell = Option<String>;

#[derive(Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct StmtResult {
    pub sql: String,
    pub from: usize,
    pub to: usize,
    pub columns: Option<Vec<ResultColumn>>,
    pub rows: Option<Vec<Vec<Cell>>>,
    pub has_more: bool,
    pub cursor_id: String,
    pub paged: bool,
    pub rows_affected: i64,
    pub elapsed_ms: i64,
    pub error: String,
    pub notice: String,
    pub source: Option<Vec<String>>,
}

#[derive(Deserialize, Clone, Debug)]
#[serde(rename_all = "camelCase")]
pub struct ExecRequest {
    pub tab_id: String,
    pub conn_id: String,
    #[serde(default)]
    pub schema: String,
    pub sql: String,
    /// Caret position (UTF-16); -1 runs every statement.
    pub cursor: i64,
    #[serde(default)]
    pub fetch: i64,
}

#[derive(Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct ExecResponse {
    pub results: Vec<StmtResult>,
    pub current_schema: String,
}

#[derive(Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct FetchResponse {
    pub rows: Vec<Vec<Cell>>,
    pub has_more: bool,
}

#[derive(Serialize, Deserialize, Clone, Default, Debug)]
#[serde(default)]
pub struct Routine {
    pub schema: String,
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub args: String,
    pub id: String,
}

#[derive(Serialize, Default)]
pub struct EditInfo {
    pub editable: bool,
    pub insertable: bool,
    pub reason: String,
    pub schema: String,
    pub table: Table,
    pub columns: Vec<String>,
    pub key: Vec<String>,
    pub keys: Vec<String>,
    pub refs: Vec<String>,
    pub comments: Vec<String>,
}

#[derive(Deserialize, Debug)]
pub struct RowChange {
    pub kind: String,
    #[serde(default)]
    pub key: std::collections::BTreeMap<String, serde_json::Value>,
    #[serde(default)]
    pub values: std::collections::BTreeMap<String, serde_json::Value>,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct ApplyRequest {
    pub conn_id: String,
    pub schema: String,
    pub table: Table,
    pub changes: Vec<RowChange>,
}

#[derive(Deserialize, Debug)]
pub struct ColumnAlter {
    pub old: Column,
    pub new: Column,
}

#[derive(Deserialize, Debug)]
#[serde(rename_all = "camelCase")]
pub struct AlterRequest {
    pub conn_id: String,
    pub schema: String,
    pub table: Table,
    pub changes: Vec<ColumnAlter>,
}

#[derive(Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct AlterResult {
    pub applied: usize,
    pub total: usize,
    pub error: String,
    pub rolled_back: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TestResult {
    pub version: String,
    pub ssh_host_key: String,
}

#[derive(Serialize)]
pub struct OpenedFile {
    pub name: String,
    pub content: String,
}
