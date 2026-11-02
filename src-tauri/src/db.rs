//! MySQL/MariaDB access: one pool per connection, one session per console
//! tab. A port of the Go app's internal/dbx (manager.go, edit.go, alter.go,
//! ddl.go, routine.go), MySQL dialect only, using mysql_async.
//!
//! Each session is a task that owns its connection. Commands reach it over a
//! channel, which lets a result stay open between "load more" calls: the
//! task keeps reading the same result while it waits for the next command.

use crate::sql;
use crate::store::new_id;
use crate::types::*;
use mysql_async::consts::{ColumnFlags, ColumnType};
use mysql_async::prelude::*;
use mysql_async::{Conn, Opts, OptsBuilder, Pool, PoolConstraints, PoolOpts, QueryResult, TextProtocol, TxOpts, Value};
use std::collections::{BTreeMap, HashMap, HashSet};
use std::future::Future;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use tokio::sync::{mpsc, oneshot};
use tokio::time::timeout;

type Res<T> = Result<T, String>;

const NOT_CONNECTED: &str = "연결되어 있지 않습니다";
const CANCELLED: &str = "사용자에 의해 취소됨";
const NO_SQL: &str = "실행할 SQL이 없습니다";
const META_TIMEOUT: Duration = Duration::from_secs(60);
/// Results that can't be paged keep a streaming cursor; it is closed after
/// this long without "load more" so it doesn't hold locks while nobody looks.
const CURSOR_IDLE: Duration = Duration::from_secs(30);
const MAX_CELL: usize = 1 << 20;

fn err_text(e: mysql_async::Error) -> String {
    match e {
        mysql_async::Error::Server(s) => format!("Error {} ({}): {}", s.code, s.state, s.message),
        e => e.to_string(),
    }
}

async fn within<T, E: std::fmt::Display>(d: Duration, f: impl Future<Output = Result<T, E>>) -> Res<T> {
    match timeout(d, f).await {
        Ok(r) => r.map_err(|e| e.to_string()),
        Err(_) => Err("시간 초과".into()),
    }
}

pub fn quote(ident: &str) -> String {
    format!("`{}`", ident.replace('`', "``"))
}

fn qualified(schema: &str, t: &Table) -> String {
    format!("{}.{}", quote(schema), quote(&t.name))
}

// ---------------------------------------------------------------- values

fn vopt(v: &Value) -> Option<String> {
    Some(match v {
        Value::NULL => return None,
        Value::Bytes(b) => String::from_utf8_lossy(b).into_owned(),
        Value::Int(i) => i.to_string(),
        Value::UInt(u) => u.to_string(),
        Value::Float(f) => f.to_string(),
        Value::Double(f) => f.to_string(),
        Value::Date(y, m, d, h, mi, s, us) => {
            let mut out = format!("{y:04}-{m:02}-{d:02} {h:02}:{mi:02}:{s:02}");
            if *us > 0 {
                out += &format!(".{us:06}");
            }
            out
        }
        Value::Time(neg, d, h, m, s, us) => {
            let hours = *d * 24 + *h as u32;
            format!("{}{hours:02}:{m:02}:{s:02}{}", if *neg { "-" } else { "" }, if *us > 0 { format!(".{us:06}") } else { String::new() })
        }
    })
}

fn vstr(v: &Value) -> String {
    vopt(v).unwrap_or_default()
}

/// A number from the catalog, -1 when unknown.
fn vi64(v: &Value) -> i64 {
    match v {
        Value::Int(i) => *i,
        Value::UInt(u) => *u as i64,
        Value::NULL => -1,
        v => vstr(v).trim().parse::<f64>().map(|f| f as i64).unwrap_or(-1),
    }
}

fn truthy(v: &Value) -> bool {
    match v {
        Value::Int(i) => *i != 0,
        Value::UInt(u) => *u != 0,
        Value::NULL => false,
        v => matches!(vstr(v).trim().to_uppercase().as_str(), "YES" | "Y" | "1" | "TRUE" | "T"),
    }
}

/// How a result column's values are shown.
#[derive(Clone)]
struct ColMeta {
    kind: String,
    binary: bool,
    float: bool,
    bit: bool,
}

const BINARY_CHARSET: u16 = 63;

fn col_meta(cols: &[mysql_async::Column]) -> Vec<ColMeta> {
    cols.iter()
        .map(|c| {
            use ColumnType::*;
            let ty = c.column_type();
            let flags = c.flags();
            let bin = c.character_set() == BINARY_CHARSET;
            let unsigned = flags.contains(ColumnFlags::UNSIGNED_FLAG);
            let int = |n: &str| if unsigned { format!("UNSIGNED {n}") } else { n.to_string() };
            let text = |b: &str, t: &str| if bin { b.to_string() } else { t.to_string() };
            let kind = match ty {
                MYSQL_TYPE_DECIMAL | MYSQL_TYPE_NEWDECIMAL => int("DECIMAL"),
                MYSQL_TYPE_TINY => int("TINYINT"),
                MYSQL_TYPE_SHORT => int("SMALLINT"),
                MYSQL_TYPE_LONG => int("INT"),
                MYSQL_TYPE_INT24 => int("MEDIUMINT"),
                MYSQL_TYPE_LONGLONG => int("BIGINT"),
                MYSQL_TYPE_FLOAT => int("FLOAT"),
                MYSQL_TYPE_DOUBLE => int("DOUBLE"),
                MYSQL_TYPE_NULL => "NULL".into(),
                MYSQL_TYPE_TIMESTAMP | MYSQL_TYPE_TIMESTAMP2 => "TIMESTAMP".into(),
                MYSQL_TYPE_DATE | MYSQL_TYPE_NEWDATE => "DATE".into(),
                MYSQL_TYPE_TIME | MYSQL_TYPE_TIME2 => "TIME".into(),
                MYSQL_TYPE_DATETIME | MYSQL_TYPE_DATETIME2 => "DATETIME".into(),
                MYSQL_TYPE_YEAR => "YEAR".into(),
                MYSQL_TYPE_BIT => "BIT".into(),
                MYSQL_TYPE_JSON => "JSON".into(),
                MYSQL_TYPE_ENUM => "ENUM".into(),
                MYSQL_TYPE_SET => "SET".into(),
                MYSQL_TYPE_GEOMETRY => "GEOMETRY".into(),
                MYSQL_TYPE_TINY_BLOB => text("TINYBLOB", "TINYTEXT"),
                MYSQL_TYPE_MEDIUM_BLOB => text("MEDIUMBLOB", "MEDIUMTEXT"),
                MYSQL_TYPE_LONG_BLOB => text("LONGBLOB", "LONGTEXT"),
                MYSQL_TYPE_BLOB => text("BLOB", "TEXT"),
                MYSQL_TYPE_VARCHAR | MYSQL_TYPE_VAR_STRING if flags.contains(ColumnFlags::ENUM_FLAG) => "ENUM".into(),
                MYSQL_TYPE_VARCHAR | MYSQL_TYPE_VAR_STRING if flags.contains(ColumnFlags::SET_FLAG) => "SET".into(),
                MYSQL_TYPE_VARCHAR | MYSQL_TYPE_VAR_STRING => text("VARBINARY", "VARCHAR"),
                MYSQL_TYPE_STRING if flags.contains(ColumnFlags::ENUM_FLAG) => "ENUM".into(),
                MYSQL_TYPE_STRING if flags.contains(ColumnFlags::SET_FLAG) => "SET".into(),
                MYSQL_TYPE_STRING => text("BINARY", "CHAR"),
                _ => "UNKNOWN".into(),
            };
            let binary = bin
                && matches!(
                    ty,
                    MYSQL_TYPE_VARCHAR
                        | MYSQL_TYPE_VAR_STRING
                        | MYSQL_TYPE_STRING
                        | MYSQL_TYPE_TINY_BLOB
                        | MYSQL_TYPE_MEDIUM_BLOB
                        | MYSQL_TYPE_LONG_BLOB
                        | MYSQL_TYPE_BLOB
                        | MYSQL_TYPE_GEOMETRY
                );
            ColMeta { kind, binary: binary || ty == MYSQL_TYPE_GEOMETRY, float: matches!(ty, MYSQL_TYPE_FLOAT | MYSQL_TYPE_DOUBLE), bit: ty == MYSQL_TYPE_BIT }
        })
        .collect()
}

fn describe(cols: &[mysql_async::Column], meta: &[ColMeta]) -> Vec<ResultColumn> {
    cols.iter().zip(meta).map(|(c, m)| ResultColumn { name: c.name_str().into_owned(), kind: m.kind.clone() }).collect()
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02X}")).collect()
}

fn clip(mut s: String) -> String {
    if s.len() > MAX_CELL {
        let mut n = MAX_CELL;
        while !s.is_char_boundary(n) {
            n -= 1;
        }
        s.truncate(n);
        s.push('…');
    }
    s
}

/// Floats without an exponent (2.611e25 -> 26110000000000000000000000)
/// unless they are extreme; MySQL sends FLOAT/DOUBLE as "2.611e25".
fn plain_float(s: String) -> String {
    if !s.contains(['e', 'E']) {
        return s;
    }
    match s.parse::<f64>() {
        Ok(f) if f.is_finite() && (f == 0.0 || (f.abs() < 1e64 && f.abs() >= 1e-30)) => f.to_string(),
        _ => s,
    }
}

/// A text-protocol value as the grid shows it.
fn cell(v: Value, m: &ColMeta) -> Cell {
    let b = match v {
        Value::NULL => return None,
        Value::Bytes(b) => b,
        v => return vopt(&v),
    };
    if m.bit {
        return Some(b.iter().fold(0u64, |acc, x| (acc << 8) | *x as u64).to_string());
    }
    if !m.binary {
        if let Ok(s) = String::from_utf8(b.clone()) {
            return Some(if m.float { plain_float(s) } else { clip(s) });
        }
    }
    if b.len() > MAX_CELL / 2 {
        return Some(format!("0x{}… ({} bytes)", hex(&b[..256]), b.len()));
    }
    Some(format!("0x{}", hex(&b)))
}

async fn read_rows(qr: &mut QueryResult<'_, 'static, TextProtocol>, meta: &[ColMeta], limit: usize) -> Result<(Vec<Vec<Cell>>, bool), mysql_async::Error> {
    let mut out = Vec::with_capacity(limit.min(1000));
    while out.len() < limit {
        match qr.next().await? {
            Some(row) => out.push(row.unwrap().into_iter().zip(meta).map(|(v, m)| cell(v, m)).collect()),
            None => return Ok((out, false)),
        }
    }
    Ok((out, true))
}

/// Metadata query: every row as values.
async fn rows(conn: &mut Conn, q: &str, params: Vec<Value>) -> Res<Vec<Vec<Value>>> {
    let r: Vec<mysql_async::Row> = if params.is_empty() { conn.query(q).await } else { conn.exec(q, params).await }.map_err(err_text)?;
    Ok(r.into_iter().map(|r| r.unwrap()).collect())
}

fn s(v: &str) -> Value {
    Value::Bytes(v.as_bytes().to_vec())
}

// ---------------------------------------------------------------- pools

pub struct DbPool {
    pool: Pool,
}

impl DbPool {
    async fn conn(&self) -> Res<Conn> {
        match timeout(Duration::from_secs(15), self.pool.get_conn()).await {
            Ok(r) => r.map_err(err_text),
            Err(_) => Err("연결 시간 초과".into()),
        }
    }

    /// Stops the statement running on the server connection `thread`.
    async fn kill(&self, thread: u32, connection: bool) {
        if thread == 0 {
            return;
        }
        if let Ok(mut c) = self.conn().await {
            let q = format!("KILL {}{thread}", if connection { "" } else { "QUERY " });
            let _ = timeout(Duration::from_secs(5), c.query_drop(q)).await;
        }
    }
}

fn parse_params(s: &str) -> Vec<(String, String)> {
    s.split(['&', '\n', ';'])
        .filter_map(|p| {
            let (k, v) = p.trim().split_once('=').unwrap_or((p.trim(), ""));
            (!k.is_empty()).then(|| (k.trim().to_string(), v.trim().to_string()))
        })
        .collect()
}

fn check_supported(c: &ConnConfig) -> Res<()> {
    if c.driver != "mysql" && c.driver != "mariadb" {
        return Err("이 실험판은 MariaDB/MySQL만 지원합니다".into());
    }
    if c.ssh {
        return Err("이 실험판은 SSH 터널을 지원하지 않습니다".into());
    }
    Ok(())
}

fn build_opts(c: &ConnConfig) -> Res<Opts> {
    let params = parse_params(&c.params);
    let base = if params.is_empty() {
        OptsBuilder::default()
    } else {
        // Options mysql_async knows by URL name (require_ssl, compression, …).
        let q: Vec<String> = params.iter().map(|(k, v)| format!("{k}={v}")).collect();
        let o = Opts::from_url(&format!("mysql://localhost/?{}", q.join("&"))).map_err(|e| format!("추가 옵션 오류: {e}"))?;
        OptsBuilder::from_opts(o)
    };
    let port = if c.port > 0 { c.port as u16 } else { 3306 };
    let b = base
        .ip_or_hostname(c.host.clone())
        .tcp_port(port)
        .user(Some(c.user.clone()))
        .pass(Some(c.password.clone()))
        .db_name((!c.database.is_empty()).then(|| c.database.clone()))
        .prefer_socket(false)
        .client_found_rows(true) // report matched rows, like most clients
        .pool_opts(
            PoolOpts::default()
                .with_constraints(PoolConstraints::new(0, 50).unwrap())
                .with_inactive_connection_ttl(Duration::from_secs(600)),
        );
    Ok(b.into())
}

async fn open(c: &ConnConfig) -> Res<(DbPool, String)> {
    check_supported(c)?;
    let p = DbPool { pool: Pool::new(build_opts(c)?) };
    let ver = {
        let mut conn = p.conn().await?;
        let r = rows(&mut conn, "SELECT VERSION()", vec![]).await?;
        r.first().and_then(|r| r.first()).map(vstr).unwrap_or_default()
    };
    Ok((p, ver))
}

/// Opens a throwaway connection and returns the server version.
pub async fn test(c: &ConnConfig) -> Res<String> {
    let (p, ver) = open(c).await?;
    let _ = p.pool.disconnect().await;
    Ok(ver)
}

// ---------------------------------------------------------------- sessions

type Reply<T> = oneshot::Sender<Res<T>>;

enum Cmd {
    Execute(ExecRequest, Reply<ExecResponse>),
    FetchMore(String, usize, Reply<FetchResponse>),
    FetchPage(String, usize, usize, Reply<FetchResponse>),
    Count(String, Reply<i64>),
    Close,
}

struct Shared {
    pool: Arc<DbPool>,
    /// Server thread id of the session's connection (0: none), for KILL.
    thread: AtomicU32,
    cancelled: AtomicBool,
}

impl Shared {
    fn cancelled(&self) -> bool {
        self.cancelled.load(Ordering::SeqCst)
    }
}

struct Session {
    conn_id: String,
    tx: mpsc::UnboundedSender<Cmd>,
    sh: Arc<Shared>,
}

impl Session {
    fn cancel(&self) {
        self.sh.cancelled.store(true, Ordering::SeqCst);
        let sh = self.sh.clone();
        tauri::async_runtime::spawn(async move { sh.pool.kill(sh.thread.load(Ordering::SeqCst), false).await });
    }

    async fn ask<T>(&self, make: impl FnOnce(Reply<T>) -> Cmd) -> Res<T> {
        let (tx, rx) = oneshot::channel();
        self.tx.send(make(tx)).map_err(|_| "세션이 닫혔습니다".to_string())?;
        rx.await.map_err(|_| "세션이 닫혔습니다".to_string())?
    }
}

/// The session task's own state.
struct Actor {
    sh: Arc<Shared>,
    conn: Option<Conn>,
    schema: String,
    /// Id of a cursor closed for being idle.
    expired: String,
}

fn use_schema(s: &str) -> String {
    format!("USE {}", quote(s))
}

async fn ensure_conn(sh: &Shared, conn: &mut Option<Conn>, schema: &str) -> Res<()> {
    if let Some(c) = conn.as_mut() {
        if timeout(Duration::from_secs(5), c.ping()).await.is_ok_and(|r| r.is_ok()) {
            return Ok(());
        }
        *conn = None;
    }
    let mut c = sh.pool.conn().await?;
    sh.thread.store(c.id(), Ordering::SeqCst);
    if !schema.is_empty() {
        let _ = c.query_drop(use_schema(schema)).await;
    }
    *conn = Some(c);
    Ok(())
}

/// Finishes a result nobody will read further. Draining it can take long
/// on a big result, so after a moment the query is stopped on the server;
/// false means the connection should not be used again.
async fn close_result(qr: QueryResult<'_, 'static, TextProtocol>, sh: &Shared) -> bool {
    let fut = qr.drop_result();
    tokio::pin!(fut);
    if let Ok(r) = timeout(Duration::from_secs(2), &mut fut).await {
        return r.is_ok();
    }
    sh.pool.kill(sh.thread.load(Ordering::SeqCst), false).await;
    if let Ok(r) = timeout(Duration::from_secs(5), &mut fut).await {
        return r.is_ok();
    }
    sh.pool.kill(sh.thread.load(Ordering::SeqCst), true).await;
    false
}

enum Out {
    Rows(Vec<ResultColumn>, Vec<Vec<Cell>>),
    Affected(i64),
}

/// Runs q and reads at most `limit` rows, closing the result.
async fn run_all(sh: &Shared, conn: &mut Conn, q: &str, limit: usize) -> Result<Out, (String, bool)> {
    let mut qr = conn.query_iter(q).await.map_err(|e| (err_text(e), true))?;
    if qr.columns_ref().is_empty() {
        let n = qr.affected_rows() as i64;
        qr.drop_result().await.map_err(|e| (err_text(e), true))?;
        return Ok(Out::Affected(n));
    }
    let cols = qr.columns_ref().to_vec();
    let meta = col_meta(&cols);
    let (data, more) = read_rows(&mut qr, &meta, limit).await.map_err(|e| (err_text(e), true))?;
    let ok = if more { close_result(qr, sh).await } else { qr.drop_result().await.is_ok() };
    if !ok {
        return Err(("결과를 닫지 못해 이 탭의 연결을 새로 엽니다".into(), false));
    }
    Ok(Out::Rows(describe(&cols, &meta), data))
}

fn is_conn_error(e: &mysql_async::Error) -> bool {
    matches!(e, mysql_async::Error::Io(_) | mysql_async::Error::Driver(_))
}

impl Actor {
    async fn run(mut self, mut rx: mpsc::UnboundedReceiver<Cmd>) {
        let mut pending: Option<Cmd> = None;
        loop {
            let cmd = match pending.take() {
                Some(c) => c,
                None => match rx.recv().await {
                    Some(c) => c,
                    None => break,
                },
            };
            match cmd {
                Cmd::Close => break,
                Cmd::Execute(req, reply) => pending = self.execute(req, reply, &mut rx).await,
                Cmd::FetchMore(id, _, reply) => {
                    let msg = if self.expired == id {
                        format!("{}초 동안 더 불러오지 않아 결과 커서를 닫았습니다 (테이블 잠금을 오래 잡지 않도록). 더 보려면 쿼리를 다시 실행하세요", CURSOR_IDLE.as_secs())
                    } else {
                        "결과 커서가 닫혔습니다. 쿼리를 다시 실행하세요".into()
                    };
                    let _ = reply.send(Err(msg));
                }
                Cmd::FetchPage(stmt, offset, n, reply) => {
                    let r = self.fetch_page(&stmt, offset, n).await;
                    let _ = reply.send(r);
                }
                Cmd::Count(stmt, reply) => {
                    let r = self.count(&stmt).await;
                    let _ = reply.send(r);
                }
            }
        }
    }

    fn fail(&self, e: String) -> String {
        if self.sh.cancelled() {
            CANCELLED.into()
        } else {
            e
        }
    }

    async fn fetch_page(&mut self, stmt: &str, offset: usize, n: usize) -> Res<FetchResponse> {
        self.sh.cancelled.store(false, Ordering::SeqCst);
        let n = if n == 0 { 500 } else { n };
        let q = sql::paged_sql(stmt, n + 1, offset).ok_or("이 쿼리는 페이지 단위로 읽을 수 없습니다")?;
        ensure_conn(&self.sh, &mut self.conn, &self.schema).await?;
        let conn = self.conn.as_mut().unwrap();
        match run_all(&self.sh, conn, &q, n + 1).await {
            Ok(Out::Rows(_, mut rows)) => {
                let has_more = rows.len() > n;
                rows.truncate(n);
                Ok(FetchResponse { rows, has_more })
            }
            Ok(Out::Affected(_)) => Ok(FetchResponse::default()),
            Err((e, keep)) => {
                if !keep {
                    self.conn = None;
                }
                Err(self.fail(e))
            }
        }
    }

    async fn count(&mut self, stmt: &str) -> Res<i64> {
        self.sh.cancelled.store(false, Ordering::SeqCst);
        ensure_conn(&self.sh, &mut self.conn, &self.schema).await?;
        let conn = self.conn.as_mut().unwrap();
        match rows(conn, &sql::count_sql(stmt), vec![]).await {
            Ok(r) => Ok(r.first().and_then(|r| r.first()).map(vi64).unwrap_or(0)),
            Err(e) => Err(self.fail(e)),
        }
    }

    /// Runs the statement under the caret, or the whole script when
    /// cursor < 0, stopping at the first error. Returns a command that
    /// arrived while a cursor was open, to be handled next.
    async fn execute(&mut self, req: ExecRequest, reply: Reply<ExecResponse>, rx: &mut mpsc::UnboundedReceiver<Cmd>) -> Option<Cmd> {
        let fetch = if req.fetch > 0 { req.fetch as usize } else { 500 };
        let all = sql::split(&req.sql);
        let stmts = if req.cursor >= 0 {
            match sql::pick_at(&req.sql, &all, sql::byte_offset(&req.sql, req.cursor as usize)) {
                Some(s) => vec![s],
                None => vec![],
            }
        } else {
            all
        };
        if stmts.is_empty() {
            let _ = reply.send(Err(NO_SQL.into()));
            return None;
        }
        self.sh.cancelled.store(false, Ordering::SeqCst);
        if let Err(e) = ensure_conn(&self.sh, &mut self.conn, &self.schema).await {
            let _ = reply.send(Err(e));
            return None;
        }
        let Actor { sh, conn: conn_slot, schema, expired } = self;
        if !req.schema.is_empty() && req.schema != *schema {
            if let Err(e) = conn_slot.as_mut().unwrap().query_drop(use_schema(&req.schema)).await {
                let _ = reply.send(Err(format!("스키마 변경 실패: {}", err_text(e))));
                return None;
            }
            *schema = req.schema.clone();
        }

        let mut resp = ExecResponse::default();
        let n = stmts.len();
        for (i, st) in stmts.iter().enumerate() {
            let last = i == n - 1;
            let mut r = StmtResult {
                sql: st.sql.clone(),
                from: sql::utf16_len(&req.sql[..st.start]),
                to: sql::utf16_len(&req.sql[..st.end]),
                rows_affected: -1,
                ..Default::default()
            };
            let start = Instant::now();
            let Some(conn) = conn_slot.as_mut() else { break };
            let mut done = false;
            let mut drop_conn = false;

            // A plain query gets a LIMIT and runs to completion; "load more"
            // reads the next page (FetchPage).
            if let Some(q) = sql::paged_sql(&st.sql, fetch + 1, 0) {
                match run_all(sh, conn, &q, fetch + 1).await {
                    Ok(Out::Rows(cols, mut data)) => {
                        r.has_more = data.len() > fetch;
                        data.truncate(fetch);
                        r.columns = Some(cols);
                        r.rows = Some(data);
                        r.paged = true;
                        r.source = sql::detect_source(&st.sql);
                        done = true;
                    }
                    Ok(Out::Affected(a)) => {
                        r.rows_affected = a;
                        done = true;
                    }
                    Err((e, keep)) => {
                        // If the paged form fails (a construct LIMIT doesn't
                        // fit), the statement runs as written below and
                        // reports its own error.
                        if sh.cancelled() || !keep {
                            r.error = if sh.cancelled() { CANCELLED.into() } else { e };
                            drop_conn = !keep;
                            done = true;
                        }
                    }
                }
            }

            if !done {
                match conn.query_iter(st.sql.as_str()).await {
                    Err(e) => r.error = err_text(e),
                    Ok(mut qr) => {
                        if qr.columns_ref().is_empty() {
                            r.rows_affected = qr.affected_rows() as i64;
                            if let Err(e) = qr.drop_result().await {
                                r.error = err_text(e);
                            }
                        } else {
                            let cols = qr.columns_ref().to_vec();
                            let meta = col_meta(&cols);
                            match read_rows(&mut qr, &meta, fetch).await {
                                Ok((data, more)) => {
                                    r.columns = Some(describe(&cols, &meta));
                                    r.rows = Some(data);
                                    r.has_more = more;
                                    r.source = sql::detect_source(&st.sql);
                                    if more && last {
                                        // Keep the result open so "load more" can continue it.
                                        r.cursor_id = new_id();
                                        r.elapsed_ms = start.elapsed().as_millis() as i64;
                                        let id = r.cursor_id.clone();
                                        resp.results.push(r);
                                        resp.current_schema = schema.clone();
                                        let _ = reply.send(Ok(resp));
                                        let (next, broken) = cursor_loop(qr, meta, &id, rx, sh, expired).await;
                                        if broken {
                                            *conn_slot = None;
                                        }
                                        return next;
                                    }
                                    let ok = if more { close_result(qr, sh).await } else { qr.drop_result().await.is_ok() };
                                    drop_conn = !ok;
                                }
                                Err(e) => {
                                    drop_conn = is_conn_error(&e);
                                    r.error = err_text(e);
                                }
                            }
                        }
                    }
                }
            }
            if drop_conn {
                *conn_slot = None;
            }
            r.elapsed_ms = start.elapsed().as_millis() as i64;
            if !r.error.is_empty() && sh.cancelled() {
                r.error = CANCELLED.into();
            }
            let failed = !r.error.is_empty();
            resp.results.push(r);
            if failed {
                break;
            }
        }

        if let Some(c) = conn_slot.as_mut() {
            if let Ok(Ok(Some(Some(cur)))) = timeout(Duration::from_secs(5), c.query_first::<Option<String>, _>("SELECT DATABASE()")).await {
                *schema = cur;
            }
        }
        resp.current_schema = schema.clone();
        let _ = reply.send(Ok(resp));
        None
    }
}

/// Serves "load more" from an open result until it ends, goes idle, or
/// another command arrives (returned to be handled next). The bool tells
/// that the connection is no longer usable.
async fn cursor_loop(
    mut qr: QueryResult<'_, 'static, TextProtocol>,
    meta: Vec<ColMeta>,
    id: &str,
    rx: &mut mpsc::UnboundedReceiver<Cmd>,
    sh: &Shared,
    expired: &mut String,
) -> (Option<Cmd>, bool) {
    let idle = tokio::time::sleep(CURSOR_IDLE);
    tokio::pin!(idle);
    let mut next = None;
    loop {
        tokio::select! {
            _ = &mut idle => {
                *expired = id.to_string();
                break;
            }
            cmd = rx.recv() => match cmd {
                Some(Cmd::FetchMore(cid, n, reply)) if cid == id => {
                    idle.as_mut().reset(tokio::time::Instant::now() + CURSOR_IDLE);
                    let n = if n == 0 { 500 } else { n };
                    match read_rows(&mut qr, &meta, n).await {
                        Ok((rows, more)) => {
                            let _ = reply.send(Ok(FetchResponse { rows, has_more: more }));
                            if !more {
                                return (None, qr.drop_result().await.is_err());
                            }
                        }
                        Err(e) => {
                            let msg = if is_conn_error(&e) {
                                // MySQL/MariaDB drop a result nobody reads for
                                // net_write_timeout (60s by default).
                                "DB 서버가 연결을 끊어 나머지 결과를 가져올 수 없습니다. 결과를 오래 읽지 않으면 서버가 연결을 끊습니다(MySQL/MariaDB의 net_write_timeout). 쿼리를 다시 실행하세요".to_string()
                            } else {
                                err_text(e)
                            };
                            let _ = reply.send(Err(msg));
                            return (None, true);
                        }
                    }
                }
                Some(other) => {
                    next = Some(other);
                    break;
                }
                None => {
                    next = Some(Cmd::Close);
                    break;
                }
            }
        }
    }
    (next, !close_result(qr, sh).await)
}

// ---------------------------------------------------------------- manager

#[derive(Default)]
pub struct Manager {
    pools: Mutex<HashMap<String, Arc<DbPool>>>,
    sessions: Mutex<HashMap<String, Arc<Session>>>,
}

fn sort_tables(ts: &mut [Table]) {
    ts.sort_by(|a, b| a.name.to_lowercase().cmp(&b.name.to_lowercase()).then_with(|| a.name.cmp(&b.name)));
}

fn select_sql(q: &str, where_: &str, order_by: &str, n: i64, offset: i64) -> String {
    let mut out = format!("SELECT * FROM {q}");
    if !where_.trim().is_empty() {
        out += &format!(" WHERE {}", where_.trim());
    }
    if !order_by.trim().is_empty() {
        out += &format!(" ORDER BY {}", order_by.trim());
    }
    match (n > 0, offset > 0) {
        (true, true) => out + &format!(" LIMIT {n} OFFSET {offset}"),
        (true, false) => out + &format!(" LIMIT {n}"),
        // No OFFSET without LIMIT in MySQL.
        (false, true) => out + &format!(" LIMIT {} OFFSET {offset}", 1i64 << 62),
        _ => out,
    }
}

fn is_binary_type(t: &str) -> bool {
    let t = t.to_uppercase();
    t.contains("BLOB") || t.contains("BINARY") || t == "GEOMETRY"
}

/// Binary columns can't be edited as text.
fn editable_type(t: &str) -> bool {
    let base = t.split('(').next().unwrap_or("").trim();
    !is_binary_type(base)
}

fn arg(v: &serde_json::Value) -> Value {
    match v {
        serde_json::Value::Null => Value::NULL,
        serde_json::Value::String(x) => s(x),
        other => s(&other.to_string()),
    }
}

/// A statement with literal values, for display only.
fn inline_args(q: &str, args: &[Value]) -> String {
    let mut out = String::new();
    let mut k = 0;
    for ch in q.chars() {
        if ch == '?' && k < args.len() {
            out += &match vopt(&args[k]) {
                None => "NULL".into(),
                Some(v) => format!("'{}'", v.replace('\'', "''")),
            };
            k += 1;
        } else {
            out.push(ch);
        }
    }
    out
}

struct Change {
    sql: String,
    args: Vec<Value>,
    one: bool, // must affect exactly one row
}

fn build_change(target: &str, types: &HashMap<String, String>, c: &RowChange) -> Res<Change> {
    let known = |col: &str| if types.contains_key(col) { Ok(()) } else { Err(format!("알 수 없는 컬럼: {col}")) };
    let mut args = vec![];
    let mut where_ = || -> Res<String> {
        if c.key.is_empty() {
            return Err("행을 식별할 키 값이 없습니다".into());
        }
        let mut conds = vec![];
        for (col, v) in &c.key {
            known(col)?;
            if v.is_null() {
                conds.push(format!("{} IS NULL", quote(col)));
            } else {
                args.push(arg(v));
                conds.push(format!("{} = ?", quote(col)));
            }
        }
        Ok(conds.join(" AND "))
    };
    match c.kind.as_str() {
        "update" => {
            if c.values.is_empty() {
                return Err("변경할 값이 없습니다".into());
            }
            let mut sets = vec![];
            let mut vals = vec![];
            for (col, v) in &c.values {
                known(col)?;
                sets.push(format!("{} = ?", quote(col)));
                vals.push(arg(v));
            }
            let w = where_()?;
            vals.extend(args);
            Ok(Change { sql: format!("UPDATE {target} SET {} WHERE {w}", sets.join(", ")), args: vals, one: true })
        }
        "delete" => {
            let w = where_()?;
            Ok(Change { sql: format!("DELETE FROM {target} WHERE {w}"), args, one: true })
        }
        "insert" => {
            let (mut cols, mut vals) = (vec![], vec![]);
            for (col, v) in &c.values {
                known(col)?;
                cols.push(quote(col));
                vals.push(arg(v));
            }
            if cols.is_empty() {
                return Ok(Change { sql: format!("INSERT INTO {target} () VALUES ()"), args: vec![], one: false });
            }
            let ph = vec!["?"; cols.len()].join(", ");
            Ok(Change { sql: format!("INSERT INTO {target} ({}) VALUES ({ph})", cols.join(", ")), args: vals, one: false })
        }
        k => Err(format!("알 수 없는 변경 종류: {k}")),
    }
}

fn default_of(s: &str) -> &str {
    let s = s.trim();
    if s.eq_ignore_ascii_case("NULL") {
        ""
    } else {
        s
    }
}

fn sql_quote(s: &str) -> String {
    format!("'{}'", s.replace('\\', "\\\\").replace('\'', "''"))
}

const STRING_TYPES: &[&str] = &["CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "ENUM", "SET"];

impl Manager {
    fn pool(&self, id: &str) -> Res<Arc<DbPool>> {
        self.pools.lock().unwrap().get(id).cloned().ok_or_else(|| NOT_CONNECTED.to_string())
    }

    async fn meta_conn(&self, id: &str) -> Res<Conn> {
        self.pool(id)?.conn().await
    }

    pub async fn connect(&self, c: &ConnConfig) -> Res<String> {
        self.disconnect(&c.id);
        let (p, ver) = open(c).await?;
        self.pools.lock().unwrap().insert(c.id.clone(), Arc::new(p));
        Ok(ver)
    }

    pub fn disconnect(&self, id: &str) {
        let p = self.pools.lock().unwrap().remove(id);
        let ss: Vec<_> = {
            let mut sessions = self.sessions.lock().unwrap();
            let keys: Vec<_> = sessions.iter().filter(|(_, s)| s.conn_id == id).map(|(k, _)| k.clone()).collect();
            keys.into_iter().filter_map(|k| sessions.remove(&k)).collect()
        };
        for s in ss {
            s.cancel();
            let _ = s.tx.send(Cmd::Close);
        }
        if let Some(p) = p {
            tauri::async_runtime::spawn(async move {
                let pool = p.pool.clone();
                drop(p);
                let _ = timeout(Duration::from_secs(10), pool.disconnect()).await;
            });
        }
    }

    pub fn connected(&self, id: &str) -> bool {
        self.pools.lock().unwrap().contains_key(id)
    }

    pub fn disconnect_all(&self) {
        let ids: Vec<_> = self.pools.lock().unwrap().keys().cloned().collect();
        for id in ids {
            self.disconnect(&id);
        }
    }

    // ------------------------------------------------------------ metadata

    pub async fn schemas(&self, id: &str) -> Res<SchemaList> {
        let mut c = self.meta_conn(id).await?;
        let fut = async {
            let list = rows(&mut c, "SHOW DATABASES", vec![]).await?;
            let cur = rows(&mut c, "SELECT DATABASE()", vec![]).await?;
            Ok::<_, String>(SchemaList {
                schemas: list.iter().map(|r| vstr(&r[0])).collect(),
                current: cur.first().map(|r| vstr(&r[0])).unwrap_or_default(),
            })
        };
        within(META_TIMEOUT, fut).await
    }

    pub async fn tables(&self, id: &str, schema: &str) -> Res<Vec<Table>> {
        let mut c = self.meta_conn(id).await?;
        let q = "SELECT TABLE_NAME, CASE TABLE_TYPE WHEN 'BASE TABLE' THEN 'TABLE' ELSE TABLE_TYPE END,
            CASE WHEN TABLE_TYPE = 'VIEW' THEN '' ELSE TABLE_COMMENT END
            FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME";
        let r = within(META_TIMEOUT, rows(&mut c, q, vec![s(schema)])).await?;
        let mut out: Vec<Table> = r
            .iter()
            .map(|r| Table { name: vstr(&r[0]), kind: vstr(&r[1]), comment: vstr(&r[2]), rows: -1, size: -1, ..Default::default() })
            .collect();
        sort_tables(&mut out);
        let mut seen = HashSet::new();
        out.retain(|t| seen.insert(t.name.clone()));
        Ok(out)
    }

    /// Row estimates (from statistics, not COUNT(*)) and sizes.
    pub async fn table_stats(&self, id: &str, schema: &str) -> Res<Vec<Table>> {
        let mut c = self.meta_conn(id).await?;
        let q = "SELECT TABLE_NAME, TABLE_ROWS, DATA_LENGTH + INDEX_LENGTH
            FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE <> 'VIEW'";
        let r = within(META_TIMEOUT, rows(&mut c, q, vec![s(schema)])).await?;
        Ok(r.iter().map(|r| Table { name: vstr(&r[0]), rows: vi64(&r[1]), size: vi64(&r[2]), ..Default::default() }).collect())
    }

    async fn columns_on(c: &mut Conn, schema: &str, table: &str) -> Res<Vec<Column>> {
        let q = "SELECT c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE, c.COLUMN_KEY = 'PRI', c.COLUMN_COMMENT,
            EXISTS (SELECT 1 FROM information_schema.STATISTICS s WHERE s.TABLE_SCHEMA = c.TABLE_SCHEMA AND s.TABLE_NAME = c.TABLE_NAME
                AND s.COLUMN_NAME = c.COLUMN_NAME AND s.NON_UNIQUE = 0 AND s.INDEX_NAME <> 'PRIMARY'),
            (SELECT CONCAT(k.REFERENCED_TABLE_NAME, '.', k.REFERENCED_COLUMN_NAME) FROM information_schema.KEY_COLUMN_USAGE k
                WHERE k.TABLE_SCHEMA = c.TABLE_SCHEMA AND k.TABLE_NAME = c.TABLE_NAME AND k.COLUMN_NAME = c.COLUMN_NAME
                AND k.REFERENCED_TABLE_NAME IS NOT NULL LIMIT 1),
            CASE WHEN c.COLUMN_DEFAULT IS NULL THEN NULL
                WHEN @@version LIKE '%MariaDB%' OR c.EXTRA LIKE '%DEFAULT_GENERATED%' THEN c.COLUMN_DEFAULT
                ELSE QUOTE(c.COLUMN_DEFAULT) END,
            TRIM(REPLACE(c.EXTRA, 'DEFAULT_GENERATED', ''))
            FROM information_schema.COLUMNS c WHERE c.TABLE_SCHEMA = ? AND c.TABLE_NAME = ? ORDER BY c.ORDINAL_POSITION";
        let r = within(META_TIMEOUT, rows(c, q, vec![s(schema), s(table)])).await?;
        Ok(r.iter()
            .map(|r| Column {
                name: vstr(&r[0]),
                kind: vstr(&r[1]),
                nullable: truthy(&r[2]),
                pk: truthy(&r[3]),
                comment: vstr(&r[4]),
                unique: truthy(&r[5]),
                fk: vstr(&r[6]),
                default: vstr(&r[7]).trim().to_string(),
                extra: vstr(&r[8]).trim().to_string(),
            })
            .collect())
    }

    pub async fn columns(&self, id: &str, schema: &str, t: &Table) -> Res<Vec<Column>> {
        let mut c = self.meta_conn(id).await?;
        Self::columns_on(&mut c, schema, &t.name).await
    }

    /// Table name -> column names, for autocompletion.
    pub async fn all_columns(&self, id: &str, schema: &str) -> Res<BTreeMap<String, Vec<String>>> {
        let mut c = self.meta_conn(id).await?;
        let q = "SELECT TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION";
        let r = within(META_TIMEOUT, rows(&mut c, q, vec![s(schema)])).await?;
        let mut out: BTreeMap<String, Vec<String>> = BTreeMap::new();
        for r in r {
            out.entry(vstr(&r[0])).or_default().push(vstr(&r[1]));
        }
        Ok(out)
    }

    pub fn select_sql(&self, id: &str, schema: &str, t: &Table, where_: &str, order_by: &str, limit: i64, offset: i64) -> Res<String> {
        self.pool(id)?;
        Ok(select_sql(&qualified(schema, t), where_, order_by, limit, offset))
    }

    pub async fn count_rows(&self, id: &str, schema: &str, t: &Table, where_: &str) -> Res<i64> {
        let mut c = self.meta_conn(id).await?;
        let mut q = format!("SELECT COUNT(*) FROM {}", qualified(schema, t));
        if !where_.trim().is_empty() {
            q += &format!(" WHERE {}", where_.trim());
        }
        let r = within(META_TIMEOUT, rows(&mut c, &q, vec![])).await?;
        Ok(r.first().map(|r| vi64(&r[0])).unwrap_or(0))
    }

    pub fn qualified_name(&self, id: &str, schema: &str, t: &Table) -> Res<String> {
        self.pool(id)?;
        Ok(qualified(schema, t))
    }

    pub fn quote_ident(&self, id: &str, name: &str) -> Res<String> {
        self.pool(id)?;
        Ok(quote(name))
    }

    pub async fn ddl(&self, id: &str, schema: &str, t: &Table) -> Res<String> {
        let mut c = self.meta_conn(id).await?;
        let r = within(META_TIMEOUT, rows(&mut c, &format!("SHOW CREATE TABLE {}", qualified(schema, t)), vec![])).await?;
        let ddl = r.first().filter(|r| r.len() >= 2).map(|r| vstr(&r[1])).unwrap_or_default();
        if ddl.trim().is_empty() {
            return Err(format!("{}의 DDL을 찾을 수 없습니다", t.name));
        }
        Ok(format!("{};\n", ddl.trim()))
    }

    pub async fn routines(&self, id: &str, schema: &str) -> Res<Vec<Routine>> {
        let mut c = self.meta_conn(id).await?;
        let q = "SELECT ROUTINE_SCHEMA, ROUTINE_NAME, ROUTINE_TYPE FROM information_schema.ROUTINES
            WHERE ROUTINE_SCHEMA = ? ORDER BY ROUTINE_TYPE DESC, ROUTINE_NAME";
        let r = within(META_TIMEOUT, rows(&mut c, q, vec![s(schema)])).await?;
        Ok(r.iter().map(|r| Routine { schema: vstr(&r[0]), name: vstr(&r[1]), kind: vstr(&r[2]), ..Default::default() }).collect())
    }

    pub async fn routine_source(&self, id: &str, schema: &str, r: &Routine) -> Res<String> {
        let mut c = self.meta_conn(id).await?;
        let kind = if r.kind == "FUNCTION" { "FUNCTION" } else { "PROCEDURE" };
        let q = format!("SHOW CREATE {kind} {}.{}", quote(schema), quote(&r.name));
        let rs = within(META_TIMEOUT, rows(&mut c, &q, vec![])).await?;
        // columns: name, sql_mode, Create Procedure/Function, …
        let src = rs.first().filter(|r| r.len() >= 3).map(|r| vstr(&r[2])).unwrap_or_default();
        if src.trim().is_empty() {
            return Err(format!("{}의 소스를 볼 수 없습니다 (권한이 없을 수 있습니다)", r.name));
        }
        // The body contains ';' — wrapped so the console runs it as one statement.
        Ok(format!("DELIMITER $$\n{}$$\nDELIMITER ;\n", src.trim()))
    }

    // ------------------------------------------------------------ editing

    /// Whether a result can be edited in place: it must come from a single
    /// table whose primary key columns are all in the result.
    pub async fn edit_info(&self, id: &str, current: &str, source: &[String], result_cols: &[String]) -> Res<EditInfo> {
        if source.is_empty() {
            return Ok(EditInfo { reason: "단일 테이블 SELECT 결과만 편집할 수 있습니다 (JOIN, GROUP BY, UNION 등은 불가)".into(), ..Default::default() });
        }
        let (schema, name) = match source.len() {
            1 => (current.to_string(), source[0].clone()),
            2 => (source[0].clone(), source[1].clone()),
            _ => (source[1].clone(), source[2].clone()),
        };
        let mut c = self.meta_conn(id).await?;
        let cols = Self::columns_on(&mut c, &schema, &name).await?;
        if cols.is_empty() {
            return Ok(EditInfo { reason: format!("테이블 {}을(를) 찾을 수 없습니다", source.join(".")), ..Default::default() });
        }
        let n = result_cols.len();
        let mut info = EditInfo {
            schema,
            table: Table { name, ..Default::default() },
            columns: vec![String::new(); n],
            keys: vec![String::new(); n],
            refs: vec![String::new(); n],
            comments: vec![String::new(); n],
            insertable: true,
            ..Default::default()
        };
        let by_name: HashMap<String, &Column> = cols.iter().map(|c| (c.name.to_uppercase(), c)).collect();
        info.key = cols.iter().filter(|c| c.pk).map(|c| c.name.clone()).collect();
        let mut seen = HashSet::new();
        for (i, rc) in result_cols.iter().enumerate() {
            let Some(c) = by_name.get(&rc.to_uppercase()) else { continue };
            if !seen.insert(c.name.clone()) {
                continue;
            }
            let mut kinds = vec![];
            if c.pk {
                kinds.push("PK");
            }
            if c.unique {
                kinds.push("UK");
            }
            if !c.fk.is_empty() {
                kinds.push("FK");
                info.refs[i] = c.fk.clone();
            }
            info.keys[i] = kinds.join(",");
            info.comments[i] = c.comment.clone();
            if editable_type(&c.kind) {
                info.columns[i] = c.name.clone();
            }
        }
        if info.key.is_empty() {
            info.reason = "기본 키(PK)가 없어 기존 행은 수정·삭제할 수 없습니다 (행 추가는 가능)".into();
            return Ok(info);
        }
        if let Some(k) = info.key.iter().find(|k| !seen.contains(*k)) {
            info.reason = format!("결과에 기본 키 컬럼 {k}이(가) 없어 기존 행은 수정·삭제할 수 없습니다 (행 추가는 가능)");
            return Ok(info);
        }
        info.editable = true;
        Ok(info)
    }

    async fn changes(&self, req: &ApplyRequest) -> Res<(Conn, Vec<Change>)> {
        let mut c = self.meta_conn(&req.conn_id).await?;
        let cols = Self::columns_on(&mut c, &req.schema, &req.table.name).await?;
        let types: HashMap<String, String> = cols.into_iter().map(|c| (c.name, c.kind)).collect();
        let target = qualified(&req.schema, &req.table);
        let list = req.changes.iter().map(|ch| build_change(&target, &types, ch)).collect::<Res<Vec<_>>>()?;
        Ok((c, list))
    }

    /// The SQL ApplyChanges would run, with values inlined, for the
    /// confirmation view.
    pub async fn preview_changes(&self, req: &ApplyRequest) -> Res<Vec<String>> {
        let (_, list) = self.changes(req).await?;
        Ok(list.iter().map(|c| inline_args(&c.sql, &c.args)).collect())
    }

    /// Runs all changes in one transaction. Updates and deletes must each
    /// hit exactly one row, otherwise everything is rolled back.
    pub async fn apply_changes(&self, req: &ApplyRequest) -> Res<usize> {
        let (mut c, list) = self.changes(req).await?;
        let mut tx = c.start_transaction(TxOpts::default()).await.map_err(err_text)?;
        for (i, ch) in list.iter().enumerate() {
            let r = if ch.args.is_empty() { tx.query_drop(&ch.sql).await } else { tx.exec_drop(&ch.sql, ch.args.clone()).await };
            if let Err(e) = r {
                let _ = tx.rollback().await;
                return Err(format!("{}번째 변경 실패 (모두 취소됨): {}\n{}", i + 1, err_text(e), inline_args(&ch.sql, &ch.args)));
            }
            let n = tx.affected_rows();
            if ch.one && n != 1 {
                let _ = tx.rollback().await;
                return Err(format!(
                    "{}번째 변경이 {n}개 행에 적용되어 모두 취소했습니다. 다른 곳에서 바뀌었을 수 있으니 새로고침하세요.\n{}",
                    i + 1,
                    inline_args(&ch.sql, &ch.args)
                ));
            }
        }
        tx.commit().await.map_err(err_text)?;
        Ok(list.len())
    }

    // ------------------------------------------------------------ alter

    /// MODIFY restates the whole column, so everything not being changed
    /// (character set, collation, AUTO_INCREMENT, ON UPDATE …) is carried over.
    async fn alter_plan(&self, req: &AlterRequest) -> Res<(Conn, Vec<String>)> {
        if req.table.kind.to_uppercase().contains("VIEW") {
            return Err("뷰의 컬럼은 바꿀 수 없습니다".into());
        }
        let mut c = self.meta_conn(&req.conn_id).await?;
        let target = qualified(&req.schema, &req.table);
        let mut out = vec![];
        for a in &req.changes {
            let (o, n) = (&a.old, &a.new);
            if n.kind.trim().is_empty() {
                return Err(format!("{}: 타입이 비어 있습니다", o.name));
            }
            let changed = !o.kind.trim().eq_ignore_ascii_case(n.kind.trim())
                || o.nullable != n.nullable
                || default_of(&o.default) != default_of(&n.default)
                || o.comment != n.comment;
            if !changed {
                continue;
            }
            let q = "SELECT CHARACTER_SET_NAME, COLLATION_NAME, EXTRA, GENERATION_EXPRESSION
                FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?";
            let r = within(META_TIMEOUT, rows(&mut c, q, vec![s(&req.schema), s(&req.table.name), s(&o.name)])).await?;
            let r = r.first().ok_or_else(|| format!("{}: 컬럼을 찾을 수 없습니다", o.name))?;
            let (cs, coll, extra, generated) = (vstr(&r[0]), vstr(&r[1]), vstr(&r[2]), vstr(&r[3]));
            if !generated.trim().is_empty() {
                return Err(format!("{}: 계산(generated) 컬럼은 여기서 바꿀 수 없습니다", o.name));
            }
            let mut def = format!("{} {}", quote(&n.name), n.kind.trim());
            let base = n.kind.trim().to_uppercase();
            let base = base.split('(').next().unwrap_or("").trim().to_string();
            if STRING_TYPES.contains(&base.as_str()) && !cs.is_empty() {
                def += &format!(" CHARACTER SET {cs} COLLATE {coll}");
            }
            def += if n.nullable { " NULL" } else { " NOT NULL" };
            let d = default_of(&n.default);
            if !d.is_empty() {
                def += &format!(" DEFAULT {d}");
            }
            let ex = extra.replace("DEFAULT_GENERATED", "").trim().to_lowercase();
            if ex.contains("auto_increment") {
                def += " AUTO_INCREMENT";
            }
            if let Some(i) = ex.find("on update ") {
                def += &format!(" ON UPDATE {}", ex[i + "on update ".len()..].trim());
            }
            if !n.comment.is_empty() {
                def += &format!(" COMMENT {}", sql_quote(&n.comment));
            }
            out.push(format!("ALTER TABLE {target} MODIFY COLUMN {def}"));
        }
        Ok((c, out))
    }

    pub async fn preview_alter(&self, req: &AlterRequest) -> Res<Vec<String>> {
        Ok(self.alter_plan(req).await?.1)
    }

    /// Runs the statements one by one (MySQL DDL isn't transactional),
    /// stopping at the first error.
    pub async fn apply_alter(&self, req: &AlterRequest) -> Res<AlterResult> {
        let (mut c, stmts) = self.alter_plan(req).await?;
        let mut res = AlterResult { total: stmts.len(), ..Default::default() };
        let _ = c.query_drop(use_schema(&req.schema)).await;
        for st in stmts {
            if let Err(e) = c.query_drop(st).await {
                res.error = err_text(e);
                return Ok(res);
            }
            res.applied += 1;
        }
        Ok(res)
    }

    // ------------------------------------------------------------ sessions

    fn session(&self, tab_id: &str, conn_id: &str) -> Res<Arc<Session>> {
        let pool = self.pool(conn_id)?;
        let mut sessions = self.sessions.lock().unwrap();
        if let Some(s) = sessions.get(tab_id) {
            if s.conn_id == conn_id {
                return Ok(s.clone());
            }
            s.cancel();
            let _ = s.tx.send(Cmd::Close);
        }
        let (tx, rx) = mpsc::unbounded_channel();
        let sh = Arc::new(Shared { pool, thread: AtomicU32::new(0), cancelled: AtomicBool::new(false) });
        let actor = Actor { sh: sh.clone(), conn: None, schema: String::new(), expired: String::new() };
        tauri::async_runtime::spawn(actor.run(rx));
        let s = Arc::new(Session { conn_id: conn_id.to_string(), tx, sh });
        sessions.insert(tab_id.to_string(), s.clone());
        Ok(s)
    }

    pub async fn execute(&self, req: ExecRequest) -> Res<ExecResponse> {
        let s = self.session(&req.tab_id, &req.conn_id)?;
        s.ask(|r| Cmd::Execute(req, r)).await
    }

    pub async fn fetch_more(&self, tab_id: &str, cursor_id: &str, n: usize) -> Res<FetchResponse> {
        let s = self.sessions.lock().unwrap().get(tab_id).cloned().ok_or("세션이 없습니다")?;
        s.ask(|r| Cmd::FetchMore(cursor_id.to_string(), n, r)).await
    }

    pub async fn fetch_page(&self, tab_id: &str, conn_id: &str, stmt: &str, offset: usize, n: usize) -> Res<FetchResponse> {
        let s = self.session(tab_id, conn_id)?;
        s.ask(|r| Cmd::FetchPage(stmt.to_string(), offset, n, r)).await
    }

    pub async fn count_result(&self, tab_id: &str, conn_id: &str, stmt: &str) -> Res<i64> {
        let s = self.session(tab_id, conn_id)?;
        s.ask(|r| Cmd::Count(stmt.to_string(), r)).await
    }

    pub fn cancel(&self, tab_id: &str) {
        if let Some(s) = self.sessions.lock().unwrap().get(tab_id) {
            s.cancel();
        }
    }

    pub fn close_session(&self, tab_id: &str) {
        if let Some(s) = self.sessions.lock().unwrap().remove(tab_id) {
            s.cancel();
            let _ = s.tx.send(Cmd::Close);
        }
    }
}

/// Run with TABLUME_IT=1 against a local MariaDB (root/pw, database shop).
#[cfg(test)]
mod it {
    use super::*;

    fn cfg() -> ConnConfig {
        ConnConfig { id: "m".into(), driver: "mariadb".into(), host: "127.0.0.1".into(), user: "root".into(), password: "pw".into(), database: "shop".into(), ..Default::default() }
    }

    fn exec(sql: &str, cursor: i64) -> ExecRequest {
        ExecRequest { tab_id: "t1".into(), conn_id: "m".into(), schema: "shop".into(), sql: sql.into(), cursor, fetch: 100 }
    }

    #[test]
    fn integration() {
        if std::env::var("TABLUME_IT").is_err() {
            return;
        }
        tauri::async_runtime::block_on(async {
            let m = Manager::default();
            println!("version {}", m.connect(&cfg()).await.unwrap());
            let setup = "DROP TABLE IF EXISTS it; CREATE TABLE it (id INT PRIMARY KEY, name VARCHAR(20) NOT NULL, v DOUBLE, b VARBINARY(4), bits BIT(8));
                INSERT INTO it VALUES (1,'한글',2.611e25,x'00ff',b'101'),(2,'b',NULL,NULL,NULL);
                DROP PROCEDURE IF EXISTS p1;
                CREATE PROCEDURE p1() BEGIN SELECT 1 AS a; SELECT 2 AS b; END;
                DROP TABLE IF EXISTS big;
                CREATE TABLE big (id INT PRIMARY KEY, v VARCHAR(20)) SELECT seq AS id, CONCAT('row ', seq) AS v FROM seq_1_to_5000;";
            let r = m.execute(exec(setup, -1)).await.unwrap();
            assert!(r.results.iter().all(|x| x.error.is_empty()), "{:?}", r.results.iter().map(|x| &x.error).collect::<Vec<_>>());
            assert_eq!(r.results.len(), 7);
            assert_eq!(r.results[2].rows_affected, 2);

            // paged query, values
            let r = m.execute(exec("select * from it order by id", 0)).await.unwrap();
            let x = &r.results[0];
            assert!(x.paged && !x.has_more, "{}", x.error);
            let row = &x.rows.as_ref().unwrap()[0];
            assert_eq!(row, &vec![Some("1".into()), Some("한글".into()), Some("26110000000000000000000000".into()), Some("0x00FF".into()), Some("5".into())]);
            let kinds: Vec<_> = x.columns.as_ref().unwrap().iter().map(|c| c.kind.clone()).collect();
            assert_eq!(kinds, ["INT", "VARCHAR", "DOUBLE", "VARBINARY", "BIT"]);
            assert_eq!(x.source, Some(vec!["it".to_string()]));
            assert_eq!(r.current_schema, "shop");

            // paging over 5000 rows
            let r = m.execute(exec("select * from big", 0)).await.unwrap();
            assert!(r.results[0].has_more && r.results[0].rows.as_ref().unwrap().len() == 100);
            let p = m.fetch_page("t1", "m", "select * from big", 4950, 100).await.unwrap();
            assert_eq!((p.rows.len(), p.has_more), (50, false));
            assert_eq!(m.count_result("t1", "m", "select * from big order by id").await.unwrap(), 5000);

            // streaming cursor (already limited, so not pageable)
            let r = m.execute(exec("select * from big limit 250", 0)).await.unwrap();
            let x = &r.results[0];
            assert!(!x.paged && x.has_more && !x.cursor_id.is_empty());
            let f = m.fetch_more("t1", &x.cursor_id, 100).await.unwrap();
            assert_eq!((f.rows.len(), f.has_more), (100, true));
            // another command closes the cursor
            let r2 = m.execute(exec("select 1", 0)).await.unwrap();
            assert_eq!(r2.results[0].rows.as_ref().unwrap()[0][0].as_deref(), Some("1"));
            assert!(m.fetch_more("t1", &x.cursor_id, 100).await.is_err());

            // caret picks the statement; CALL returns the first result set
            let sql = "select 1 a;\nselect 2 b;";
            let r = m.execute(exec(sql, 15)).await.unwrap();
            assert_eq!(r.results[0].sql, "select 2 b");
            let r = m.execute(exec("call p1()", 0)).await.unwrap();
            assert_eq!(r.results[0].rows.as_ref().unwrap()[0][0].as_deref(), Some("1"), "{}", r.results[0].error);
            let r = m.execute(exec("select 3", 0)).await.unwrap();
            assert_eq!(r.results[0].rows.as_ref().unwrap()[0][0].as_deref(), Some("3"), "{}", r.results[0].error);

            // errors stop the script
            let r = m.execute(exec("select 1; select * from nope; select 2", -1)).await.unwrap();
            assert_eq!(r.results.len(), 2);
            assert!(r.results[1].error.contains("1146"), "{}", r.results[1].error);

            // cancel
            let t = std::time::Instant::now();
            let mm = &m;
            let (r, _) = tokio::join!(mm.execute(exec("select sleep(20)", 0)), async {
                tokio::time::sleep(Duration::from_millis(500)).await;
                mm.cancel("t1");
            });
            let r = r.unwrap();
            assert!(t.elapsed() < Duration::from_secs(5), "cancel took {:?}", t.elapsed());
            println!("after cancel: {:?} rows={:?} err={}", t.elapsed(), r.results[0].rows, r.results[0].error);
            let r = m.execute(exec("select 4", 0)).await.unwrap();
            assert_eq!(r.results[0].rows.as_ref().unwrap()[0][0].as_deref(), Some("4"));

            // USE changes the session schema
            let r = m.execute(exec("use mysql", 0)).await.unwrap();
            assert_eq!(r.current_schema, "mysql");

            // metadata
            let tables = m.tables("m", "shop").await.unwrap();
            assert!(tables.iter().any(|t| t.name == "it"));
            let t = Table { name: "it".into(), kind: "TABLE".into(), ..Default::default() };
            let cols = m.columns("m", "shop", &t).await.unwrap();
            assert!(cols[0].pk && !cols[1].nullable);
            assert!(m.ddl("m", "shop", &t).await.unwrap().starts_with("CREATE TABLE `it`"));
            let rs = m.routines("m", "shop").await.unwrap();
            assert!(m.routine_source("m", "shop", &rs[0]).await.unwrap().starts_with("DELIMITER $$"));

            // editing
            let info = m.edit_info("m", "shop", &["it".into()], &["id".into(), "name".into(), "b".into()]).await.unwrap();
            assert!(info.editable, "{}", info.reason);
            assert_eq!(info.columns, ["id", "name", ""]);
            let ch = |kind: &str, key: serde_json::Value, values: serde_json::Value| RowChange {
                kind: kind.into(),
                key: serde_json::from_value(key).unwrap(),
                values: serde_json::from_value(values).unwrap(),
            };
            let req = ApplyRequest {
                conn_id: "m".into(),
                schema: "shop".into(),
                table: t.clone(),
                changes: vec![
                    ch("update", serde_json::json!({"id": "1"}), serde_json::json!({"name": "it's", "v": null})),
                    ch("insert", serde_json::json!({}), serde_json::json!({"id": "3", "name": "c"})),
                    ch("delete", serde_json::json!({"id": "2"}), serde_json::json!({})),
                ],
            };
            let pv = m.preview_changes(&req).await.unwrap();
            assert_eq!(pv[0], "UPDATE `shop`.`it` SET `name` = 'it''s', `v` = NULL WHERE `id` = '1'");
            assert_eq!(m.apply_changes(&req).await.unwrap(), 3);
            let bad = ApplyRequest { changes: vec![ch("delete", serde_json::json!({"id": "99"}), serde_json::json!({}))], ..req };
            assert!(m.apply_changes(&bad).await.unwrap_err().contains("0개 행"));

            // alter
            let mut new = cols[1].clone();
            new.kind = "varchar(40)".into();
            new.comment = "이름".into();
            let areq = AlterRequest { conn_id: "m".into(), schema: "shop".into(), table: t.clone(), changes: vec![ColumnAlter { old: cols[1].clone(), new }] };
            let st = m.preview_alter(&areq).await.unwrap();
            assert_eq!(st, ["ALTER TABLE `shop`.`it` MODIFY COLUMN `name` varchar(40) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL COMMENT '이름'"]);
            let res = m.apply_alter(&areq).await.unwrap();
            assert_eq!((res.applied, res.error.as_str()), (1, ""));
            m.disconnect("m");
        });
    }
}
