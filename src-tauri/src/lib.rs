//! Tablume on Tauri: the commands the frontend calls (same names and
//! arguments as the Go app's bindings, see frontend/src/lib/api.ts) and the
//! tab windows.

mod db;
mod sql;
mod store;
mod types;

use std::collections::{BTreeMap, HashMap, HashSet};
use std::path::PathBuf;
use std::sync::Mutex;
use tauri::{AppHandle, Emitter, Manager as _, State, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_dialog::{DialogExt, FilePath};
use types::*;

type Res<T> = Result<T, String>;

struct App {
    store: Mutex<Option<store::Store>>,
    db: db::Manager,
    tabs: Mutex<TabWindows>,
}

/// Tabs moved into windows of their own (see the frontend's state.svelte.ts).
#[derive(Default)]
struct TabWindows {
    state: HashMap<String, String>, // the tab as the window should open it (JSON)
    closing: HashSet<String>,       // allowed to close without "put back"
    quitting: bool,
}

impl App {
    fn with_store<T>(&self, f: impl FnOnce(&store::Store) -> Res<T>) -> Res<T> {
        match self.store.lock().unwrap().as_ref() {
            Some(s) => f(s),
            None => Err("설정 파일을 열 수 없습니다".into()),
        }
    }
}

// ---------------------------------------------------------------- connections

#[tauri::command]
fn list_connections(app: State<App>) -> Vec<ConnConfig> {
    app.with_store(|s| Ok(s.list())).unwrap_or_default()
}

#[tauri::command]
fn save_connection(app: State<App>, c: ConnConfig) -> Res<ConnConfig> {
    app.with_store(|s| s.save_conn(c))
}

#[tauri::command]
fn delete_connection(app: State<App>, id: String) -> Res<()> {
    app.db.disconnect(&id);
    app.with_store(|s| s.delete(&id))
}

#[tauri::command]
fn set_schema_filter(app: State<App>, id: String, f: Option<SchemaFilter>) -> Res<()> {
    app.with_store(|s| s.set_schema_filter(&id, f))
}

#[tauri::command]
fn reorder_connections(app: State<App>, ids: Vec<String>) -> Res<()> {
    app.with_store(|s| s.reorder(&ids))
}

/// Tries the settings from the form. An empty password on an existing
/// connection falls back to the stored one.
#[tauri::command]
async fn test_connection(app: State<'_, App>, mut c: ConnConfig) -> Res<TestResult> {
    if !c.id.is_empty() {
        if let Ok(Some(saved)) = app.with_store(|s| Ok(s.get(&c.id))) {
            if c.password.is_empty() {
                c.password = saved.password;
            }
        }
    }
    let version = db::test(&c).await?;
    Ok(TestResult { version, ssh_host_key: String::new() })
}

#[tauri::command]
fn needs_password(app: State<App>, id: String) -> bool {
    matches!(app.with_store(|s| Ok(s.get(&id))), Ok(Some(c)) if !c.save_password)
}

#[tauri::command]
async fn connect(app: State<'_, App>, id: String, password: String) -> Res<String> {
    let mut c = app.with_store(|s| s.get(&id).ok_or_else(|| "연결 정보를 찾을 수 없습니다".to_string()))?;
    if !password.is_empty() {
        c.password = password;
    }
    app.db.connect(&c).await
}

#[tauri::command]
fn disconnect(app: State<App>, id: String) {
    app.db.disconnect(&id)
}

#[tauri::command]
fn is_connected(app: State<App>, id: String) -> bool {
    app.db.connected(&id)
}

// ---------------------------------------------------------------- metadata

#[tauri::command]
async fn list_schemas(app: State<'_, App>, id: String) -> Res<SchemaList> {
    app.db.schemas(&id).await
}

#[tauri::command]
async fn list_tables(app: State<'_, App>, id: String, schema: String) -> Res<Vec<Table>> {
    app.db.tables(&id, &schema).await
}

#[tauri::command]
async fn table_stats(app: State<'_, App>, id: String, schema: String) -> Res<Vec<Table>> {
    app.db.table_stats(&id, &schema).await
}

#[tauri::command]
async fn list_columns(app: State<'_, App>, id: String, schema: String, t: Table) -> Res<Vec<Column>> {
    app.db.columns(&id, &schema, &t).await
}

#[tauri::command]
async fn completion_columns(app: State<'_, App>, id: String, schema: String) -> Res<BTreeMap<String, Vec<String>>> {
    app.db.all_columns(&id, &schema).await
}

#[tauri::command]
#[allow(clippy::too_many_arguments)]
fn select_sql(app: State<App>, id: String, schema: String, t: Table, cond: String, order_by: String, limit: i64, offset: i64) -> Res<String> {
    app.db.select_sql(&id, &schema, &t, &cond, &order_by, limit, offset)
}

#[tauri::command]
async fn list_routines(app: State<'_, App>, id: String, schema: String) -> Res<Vec<Routine>> {
    app.db.routines(&id, &schema).await
}

#[tauri::command]
async fn routine_source(app: State<'_, App>, id: String, schema: String, r: Routine) -> Res<String> {
    app.db.routine_source(&id, &schema, &r).await
}

#[tauri::command]
async fn preview_alter(app: State<'_, App>, req: AlterRequest) -> Res<Vec<String>> {
    app.db.preview_alter(&req).await
}

#[tauri::command]
async fn apply_alter(app: State<'_, App>, req: AlterRequest) -> Res<AlterResult> {
    app.db.apply_alter(&req).await
}

#[tauri::command]
async fn count_rows(app: State<'_, App>, id: String, schema: String, t: Table, cond: String) -> Res<i64> {
    app.db.count_rows(&id, &schema, &t, &cond).await
}

#[tauri::command]
fn quote_ident(app: State<App>, id: String, name: String) -> Res<String> {
    app.db.quote_ident(&id, &name)
}

#[tauri::command]
fn qualified_name(app: State<App>, id: String, schema: String, t: Table) -> Res<String> {
    app.db.qualified_name(&id, &schema, &t)
}

#[tauri::command]
async fn table_ddl(app: State<'_, App>, id: String, schema: String, t: Table) -> Res<String> {
    app.db.ddl(&id, &schema, &t).await
}

// ---------------------------------------------------------------- editing

#[tauri::command]
async fn edit_info(app: State<'_, App>, id: String, schema: String, source: Vec<String>, columns: Vec<String>) -> Res<EditInfo> {
    app.db.edit_info(&id, &schema, &source, &columns).await
}

#[tauri::command]
async fn preview_changes(app: State<'_, App>, req: ApplyRequest) -> Res<Vec<String>> {
    app.db.preview_changes(&req).await
}

#[tauri::command]
async fn apply_changes(app: State<'_, App>, req: ApplyRequest) -> Res<usize> {
    app.db.apply_changes(&req).await
}

// ---------------------------------------------------------------- queries

#[tauri::command]
async fn execute(app: State<'_, App>, req: ExecRequest) -> Res<ExecResponse> {
    app.db.execute(req).await
}

#[tauri::command]
async fn fetch_more(app: State<'_, App>, tab_id: String, cursor_id: String, n: usize) -> Res<FetchResponse> {
    app.db.fetch_more(&tab_id, &cursor_id, n).await
}

#[tauri::command]
async fn fetch_page(app: State<'_, App>, tab_id: String, conn_id: String, sql: String, offset: usize, n: usize) -> Res<FetchResponse> {
    app.db.fetch_page(&tab_id, &conn_id, &sql, offset, n).await
}

#[tauri::command]
async fn count_result(app: State<'_, App>, tab_id: String, conn_id: String, sql: String) -> Res<i64> {
    app.db.count_result(&tab_id, &conn_id, &sql).await
}

#[tauri::command]
fn cancel(app: State<App>, tab_id: String) {
    app.db.cancel(&tab_id)
}

#[tauri::command]
fn close_session(app: State<App>, tab_id: String) {
    app.db.close_session(&tab_id)
}

// ---------------------------------------------------------------- files

fn path_of(p: Option<FilePath>) -> Option<PathBuf> {
    p.and_then(|p| p.into_path().ok())
}

async fn pick(d: tauri_plugin_dialog::FileDialogBuilder<tauri::Wry>) -> Option<PathBuf> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    d.pick_file(move |p| {
        let _ = tx.send(path_of(p));
    });
    rx.await.ok().flatten()
}

fn path_string(p: Option<PathBuf>) -> String {
    p.map(|p| p.to_string_lossy().into_owned()).unwrap_or_default()
}

#[tauri::command]
async fn pick_sqlite_file(window: WebviewWindow) -> Res<String> {
    let d = window.dialog().file().set_parent(&window).set_title("SQLite 데이터베이스 파일");
    Ok(path_string(pick(d).await))
}

/// An SSH private key, starting in ~/.ssh.
#[tauri::command]
async fn pick_key_file(window: WebviewWindow) -> Res<String> {
    let mut d = window.dialog().file().set_parent(&window).set_title("SSH 개인 키 파일");
    if let Some(ssh) = dirs::home_dir().map(|h| h.join(".ssh")).filter(|p| p.is_dir()) {
        d = d.set_directory(ssh);
    }
    Ok(path_string(pick(d).await))
}

/// Asks for a path and writes content; "" if cancelled.
#[tauri::command]
async fn save_text_file(window: WebviewWindow, name: String, content: String) -> Res<String> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    window.dialog().file().set_parent(&window).set_file_name(&name).save_file(move |p| {
        let _ = tx.send(path_of(p));
    });
    let Some(path) = rx.await.ok().flatten() else { return Ok(String::new()) };
    std::fs::write(&path, content).map_err(|e| e.to_string())?;
    Ok(path.to_string_lossy().into_owned())
}

#[tauri::command]
async fn open_sql_file(window: WebviewWindow) -> Res<Option<OpenedFile>> {
    let d = window.dialog().file().set_parent(&window).add_filter("SQL (*.sql)", &["sql"]).add_filter("모든 파일", &["*"]);
    let Some(path) = pick(d).await else { return Ok(None) };
    let content = std::fs::read_to_string(&path).map_err(|e| e.to_string())?;
    let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
    Ok(Some(OpenedFile { name, content }))
}

// Workspace (open tabs and their SQL) lives next to the connection list.
fn workspace_path() -> PathBuf {
    store::dir().join("workspace.json")
}

#[tauri::command]
fn load_workspace() -> String {
    std::fs::read_to_string(workspace_path()).unwrap_or_default()
}

#[tauri::command]
fn save_workspace(data: String) -> Res<()> {
    std::fs::write(workspace_path(), data).map_err(|e| e.to_string())
}

/// The native title bar follows the app theme (Windows).
#[tauri::command]
fn set_dark_title_bar(window: WebviewWindow, dark: bool) {
    let _ = window.set_theme(Some(if dark { tauri::Theme::Dark } else { tauri::Theme::Light }));
}

// ---------------------------------------------------------------- tab windows
// Window and tab talk through events (emitted by the pages):
//   tab:sync / tab:restore / tab:close   tab window → main
//   tab-window:close                     here → tab window (close button)

fn tab_label(tab_id: &str) -> String {
    format!("tab-{tab_id}")
}

#[tauri::command]
fn open_tab_window(handle: AppHandle, app: State<App>, tab_id: String, title: String, state: String) -> Res<()> {
    let label = tab_label(&tab_id);
    if let Some(w) = handle.get_webview_window(&label) {
        let _ = w.show();
        let _ = w.set_focus();
        return Ok(());
    }
    app.tabs.lock().unwrap().state.insert(tab_id.clone(), state);
    let w = WebviewWindowBuilder::new(&handle, &label, WebviewUrl::App("index.html".into()))
        .title(&title)
        .inner_size(1100.0, 760.0)
        .min_inner_size(520.0, 360.0)
        .background_color(tauri::window::Color(30, 31, 34, 255))
        .build()
        .map_err(|e| e.to_string())?;
    let h = handle.clone();
    w.on_window_event(move |e| match e {
        // The close button puts the tab back (the page sends its latest
        // state first), unless the tab itself asked to close.
        WindowEvent::CloseRequested { api, .. } => {
            let app = h.state::<App>();
            let tabs = app.tabs.lock().unwrap();
            if !tabs.quitting && !tabs.closing.contains(&tab_id) {
                api.prevent_close();
                let _ = h.emit("tab-window:close", tab_id.clone());
            }
        }
        WindowEvent::Destroyed => {
            let app = h.state::<App>();
            let mut tabs = app.tabs.lock().unwrap();
            tabs.state.remove(&tab_id);
            tabs.closing.remove(&tab_id);
        }
        _ => {}
    });
    Ok(())
}

#[tauri::command]
fn tab_window_state(app: State<App>, tab_id: String) -> String {
    app.tabs.lock().unwrap().state.get(&tab_id).cloned().unwrap_or_default()
}

/// Closes the tab's window without asking it to put the tab back.
#[tauri::command]
fn close_tab_window(handle: AppHandle, app: State<App>, tab_id: String) {
    if let Some(w) = handle.get_webview_window(&tab_label(&tab_id)) {
        app.tabs.lock().unwrap().closing.insert(tab_id);
        let _ = w.destroy();
    }
}

/// Brings the tab's window to the front; false if it has none.
#[tauri::command]
fn focus_tab_window(handle: AppHandle, tab_id: String) -> bool {
    let Some(w) = handle.get_webview_window(&tab_label(&tab_id)) else { return false };
    let _ = w.unminimize();
    let _ = w.show();
    let _ = w.set_focus();
    true
}

#[cfg(target_os = "macos")]
fn mac_menu(app: &AppHandle) -> tauri::Result<tauri::menu::Menu<tauri::Wry>> {
    use tauri::menu::{Menu, PredefinedMenuItem as P, Submenu};
    let sep = || P::separator(app);
    let about = tauri::menu::AboutMetadata::default();
    Menu::with_items(
        app,
        &[
            &Submenu::with_items(
                app,
                "Tablume",
                true,
                &[
                    &P::about(app, Some("Tablume에 관하여"), Some(about))?,
                    &sep()?,
                    &P::services(app, Some("서비스"))?,
                    &sep()?,
                    &P::hide(app, Some("Tablume 가리기"))?,
                    &P::hide_others(app, Some("기타 가리기"))?,
                    &P::show_all(app, Some("모두 보기"))?,
                    &sep()?,
                    &P::quit(app, Some("Tablume 종료"))?,
                ],
            )?,
            &Submenu::with_items(
                app,
                "편집",
                true,
                &[
                    &P::undo(app, Some("실행 취소"))?,
                    &P::redo(app, Some("실행 복귀"))?,
                    &sep()?,
                    &P::cut(app, Some("오려두기"))?,
                    &P::copy(app, Some("복사하기"))?,
                    &P::paste(app, Some("붙여넣기"))?,
                    &P::select_all(app, Some("전체 선택"))?,
                ],
            )?,
            &Submenu::with_items(app, "윈도우", true, &[&P::minimize(app, Some("최소화"))?, &P::maximize(app, Some("확대/축소"))?])?,
        ],
    )
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let store = store::Store::open().map_err(|e| eprintln!("설정 파일 열기 실패: {e}")).ok();
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .manage(App { store: Mutex::new(store), db: db::Manager::default(), tabs: Mutex::new(TabWindows::default()) })
        .setup(|app| {
            #[cfg(target_os = "macos")]
            app.set_menu(mac_menu(app.handle())?)?;
            let _ = app;
            Ok(())
        })
        .on_window_event(|w, e| {
            // Closing the main window quits the app, tab windows and all.
            if w.label() == "main" && matches!(e, WindowEvent::CloseRequested { .. }) {
                let h = w.app_handle();
                h.state::<App>().tabs.lock().unwrap().quitting = true;
                h.state::<App>().db.disconnect_all();
                h.exit(0);
            }
        })
        .invoke_handler(tauri::generate_handler![
            list_connections,
            save_connection,
            delete_connection,
            set_schema_filter,
            reorder_connections,
            test_connection,
            needs_password,
            connect,
            disconnect,
            is_connected,
            list_schemas,
            list_tables,
            table_stats,
            list_columns,
            completion_columns,
            select_sql,
            list_routines,
            routine_source,
            preview_alter,
            apply_alter,
            count_rows,
            quote_ident,
            qualified_name,
            table_ddl,
            edit_info,
            preview_changes,
            apply_changes,
            execute,
            fetch_more,
            fetch_page,
            count_result,
            cancel,
            close_session,
            pick_sqlite_file,
            pick_key_file,
            save_text_file,
            open_sql_file,
            load_workspace,
            save_workspace,
            set_dark_title_bar,
            open_tab_window,
            tab_window_state,
            close_tab_window,
            focus_tab_window,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
