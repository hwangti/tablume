# Tablume

**English** | [한국어](README.ko.md)

A lightweight cross-platform multi-database SQL client written in Go.

Built as a lighter alternative to DataGrip. It runs on macOS and Windows, and there is also a [web version](#web-version) you run on a server and use from a browser.

![Tablume](docs/screenshot-oracle.png)

- **Databases**: MySQL, MariaDB, Oracle, SQL Server, PostgreSQL, SQLite
- **Size**: a single executable, about 35 MB. No bundled JVM or Chromium: it uses the web view that comes with the OS (WebView2 on Windows, WKWebView on macOS).
- **No Oracle Instant Client needed.** It uses a pure Go driver ([go-ora](https://github.com/sijms/go-ora)).

## Features

- Connections: a color per connection, connection test, passwords kept in the OS keychain (macOS Keychain, Windows Credential Manager)
- SSH tunnels: password or key file (passphrase-protected keys included). The host key of an SSH server is saved on the first connection, and a server whose key has changed is refused. A dropped tunnel reconnects on the next query.
- Schema tree: schema/database → tables/views → columns (PK, type, NOT NULL)
  - Schemas to show: click `3/98` (shown/total) next to a connection to choose which schemas the tree shows. All schemas (system schemas excluded), the default schema (login user / default database), individual checks and patterns like `HR_*` can be combined; saved per connection.
  - Connection order: drag a connection, or right-click → Move to Top / Up / Down / to Bottom.
  - The name of the connection you are in stays pinned at the top of the tree while scrolling.
- Procedures and functions: expand "Procedures · Functions" under a schema for the list (packages included on Oracle; not for SQLite). Clicking one opens its source (the CREATE statement) in a console preview tab, wrapped so it runs as is: `DELIMITER` on MySQL, `GO` on SQL Server, `/` on Oracle.
  - Table filter
  - Double-click to view data
  - Context menu: generate SELECT, copy names, …
- SQL console (any number of tabs, each with its own connection and session)
  - `Ctrl/⌘+Enter`: run the statement at the cursor, or the selection if there is one
  - `Ctrl/⌘+Shift+Enter`: run the whole script in order, stopping at an error
  - Cancel a running query. If the server doesn't respond to the cancel, the tab's connection is reopened after 3 seconds.
  - Syntax highlighting per database, table/column completion
  - Statements are split by each database's rules:
    - Oracle: a PL/SQL block runs up to the `/` line.
    - SQL Server: batches are split at `GO`.
    - MySQL: `DELIMITER` is supported, and `BEGIN … END` blocks run as written.
    - PostgreSQL: `$$` quoting is recognized.
  - On Oracle, PL/SQL created with compilation errors shows the errors (like SQL\*Plus's "created with compilation errors").
  - Tabs and their SQL come back when the app restarts. `.sql` files can be opened and saved.
  - Closing a console with SQL you typed (not as opened or last saved to a file) asks first.
- Result grid
  - Virtual scrolling: tens of thousands of rows stay light.
  - Fetches the first 500 rows (configurable) and more as you scroll.
  - Cell and range selection; copy as TSV, with headers, or as an `IN (…)` list
  - Column sorting and resizing
  - Value viewer/editor: values that look like JSON (starting with `{` or `[`) are highlighted, with folding and bracket matching. A Format JSON button; `Ctrl`/`⌘`+`Enter` applies.
  - Follow foreign keys: `⌘`+click (Mac) or `Alt`/`Ctrl`+click (Windows) on an FK cell opens the referenced table filtered by `WHERE column = value`. Holding the key shows FK cells as links; the cell menu's "Open Referenced Row" does the same. The original tab is kept.
  - Export to CSV/JSON
- Editing in the result grid (DataGrip style)
  - A result can be edited when it comes from a single-table SELECT and includes the whole primary key. Otherwise it is marked "Read-only" with the reason.
  - Tables without a primary key still take new rows; only changing or deleting existing rows is blocked.
  - Cell edits, Set to NULL, added and deleted rows are collected on screen and run in one transaction with "Apply" (`⌘`/`Ctrl`/`Alt`+`Enter`, `⌘`/`Ctrl`+`S`). The statements and their results are logged in the tab's log as highlighted SQL (so are column and index changes in the Structure tab, and table renames).
  - "Revert Selected Cells" in the cell menu (`⌘`/`Ctrl`+`Z`) undoes only the edited cells. Cells of a new row go back to `<generated>`/`<default>`.
  - While editing, `Tab`/`Shift+Tab` opens the next cell for editing too (from the last column, the next row).
  - In a new row, columns left empty show `<generated>` (AUTO_INCREMENT, IDENTITY, sequences, computed columns) or `<default>` (a default value) and are left out of the INSERT. Opening a cell and leaving it without typing keeps NULL as NULL; typing and then clearing it gives an empty string (`''`; Oracle stores that as NULL).
  - If a change affects a different number of rows than expected (one), everything is rolled back.
- Structure tab: a table tab's "Structure" switches between `Columns · Indexes | DDL`. Columns · Indexes is one page with the column table followed by the index table. Right-click a table → "View DDL" opens its CREATE statement in a new console tab.
  - Columns: key, type, NULL, default, FK and comment in a table.
  - Double-click type, default or comment, or click the NULL checkbox, then "Apply": the `ALTER` statements are shown before they run. PostgreSQL and SQL Server run them in one transaction, so a failure rolls everything back; MySQL/MariaDB and Oracle can't roll back DDL, so statements before a failure stay applied (the preview says so). On MySQL, `MODIFY` rewrites the whole column definition, keeping its character set, collation, AUTO_INCREMENT and ON UPDATE. SQLite tables and views can't be edited.
  - Indexes: name, columns (order, DESC), unique, kind, condition (partial indexes) and INCLUDE. "Add Index" builds one from columns picked in order (named `idx_table_column` by default); the pencil button or a double-click changes one by dropping and creating it again; the trash button drops it. "Apply" shows the SQL first. "Open in Console" in the preview puts that SQL in a new console tab without running it (column changes too). The primary key can't be dropped or changed, and an index belonging to a UNIQUE constraint is dropped with `DROP CONSTRAINT` (not on SQLite). Indexes with expressions, prefix lengths, conditions or INCLUDE can only be dropped.
  - Where the DDL comes from:
  - MySQL: `SHOW CREATE TABLE`
  - Oracle: `DBMS_METADATA`
  - PostgreSQL, SQL Server: assembled from the system catalogs (constraints and indexes included)
  - SQLite: `sqlite_master`
- Tree view options: table and column comments, row counts and sizes can be shown in the tree. Row counts and sizes are estimates from the database's statistics, not `COUNT(*)`, fetched separately from the table list and only while the option is on (they can be slow with many tables). Oracle sizes are blocks × block size from the statistics (table data only). When a table tab counts all rows without WHERE, or loads every row, the tree's count is updated to that number.
- Running DDL such as `CREATE`/`ALTER`/`DROP`/`RENAME` in a console reloads the table list of the schemas involved and the columns of expanded tables. `CREATE DATABASE`/`SCHEMA` reloads the schema list too.
- Rename a table: right-click it in the tree → "Rename", or press `F2`. Tabs showing the table follow the new name.
- Tree
  - Clicking a table opens it in a preview tab (the next click reuses it); double-clicking keeps the tab. Enter opens it too. A table that is already open switches to its tab.
  - Connections and schemas expand with the ▸ button or a double-click (a click only selects).
  - ↑↓ to move, → to expand, ← to collapse.
  - Open tables are highlighted in the tree. The ⌖ button above the tree expands to and scrolls to the open table.
  - Hovering a table or column shows its full comment, row count and size.
  - "Default order: PK descending" in the view options opens tables sorted by primary key, descending.
- Tabs can be reordered by dragging.
- Table data tabs: double-clicking a table in the tree opens a tab with results only, no editor. Switch with `Data | Structure | Log` at the top; the console icon opens the same SELECT in an SQL console.
  - Consoles and table tabs both fetch results page by page. A plain SELECT gets `LIMIT/OFFSET` (`OFFSET … FETCH` on SQL Server and Oracle 12c+) and each page runs on its own, so no cursor stays open holding table locks. Queries that already have `LIMIT`/`TOP`/`FOR UPDATE` and the like, and Oracle 11g, read on from a cursor, which is closed after 30 seconds without reading.
  - Scrolling down appends the next page; rows above are added with "Load previous rows" at the top. To save memory at most 10,000 rows (twice the page size if that is larger) are held at once.
  - Result bar: ⏮ first · ◀ ▶ one page at a time (buttons load that page instead of appending) · `1–1,000 ▾` (click to pick a page size from 100 to 10,000) · `1,001+` (click to count all rows with `COUNT(*)`) · ⏭ last.
- Result columns
  - Drag a header to reorder columns; hide or show them from the header menu or the button in the grid's top-left corner. Table tabs keep the order and hidden columns when reloaded.
  - "Show column numbers" in the view options numbers the headers.
  - Selecting a cell highlights cells with the same value in that column in yellow. While editing a text cell, the `…` button at the right of the input continues in a large editor.
- Grid display
  - Numbers, dates and booleans are colored. FLOAT/DOUBLE values are shown as they are, without exponent notation (`2.6e+25`).
  - Columns in headers and the tree get PK (🔑), UNIQUE (☆) and FK (🔗) icons; an FK's target is shown in a tooltip.
- Export: CSV, TSV, JSON, INSERT statements (.sql)
- Typing in the table filter shows only schemas with matching tables, expanded. It searches schemas whose tables were already loaded; schemas never expanded aren't queried.
- Table data filter (DataGrip style)
  - Type a condition in the `WHERE` / `ORDER BY` fields above a table data tab and press Enter to query the database again. Both fields complete column names and keywords (`Ctrl+Space` opens the list).
  - No row limit: scrolling keeps loading to the end.
  - Clicking a column header sorts in the database with `ORDER BY`: ascending → descending → off.
  - In a plain query result, clicking a header sorts the loaded rows on screen.
- Results only: the maximize button in the result tab row, or a double-click on the divider, hides the editor and shows only the grid. Table data tabs open this way.
- Dialogs can be moved by dragging their title bar; double-clicking it centers them again.
- Language: English / 한국어 in the view options. The first time, the OS (browser) language is used. Error messages from the database server stay in the server's language.
- Theme: Auto (follows the OS) / Dark / Light in the view options.
- Zoom: "Zoom (%)" `[−][100 ▾][+]` in the view options zooms the whole window by ±5%, a typed value, or a ▾ preset (100·110·125·150·175·200). `Ctrl/⌘`+`+`/`-`/`0` and `Ctrl/⌘`+wheel step through the presets (desktop app).
- Hide the connection list: the button at the left of the tab strip or `Ctrl/⌘+B` hides it for a while (expanded nodes and the filter are kept).
- Wide view: the button at the right of the tab strip, or right-click a tab → "Wide View", shows only the current tab across the window (no connection list or tab strip). "Exit Wide View" in the top bar or `Ctrl/⌘+B` goes back.
- The desktop app reopens at the window size it was closed at (maximized if it was).

## Shortcuts

| Key | Action |
|---|---|
| `Ctrl/⌘ + Enter` | Run the current statement (or the selection) |
| `Ctrl/⌘ + Shift + Enter` | Run all |
| `Ctrl/⌘ + T` / `Ctrl/⌘ + W` | New console / close tab |
| `Ctrl/⌘ + B` | Hide / show the connection list (in wide view: back to the normal view) |
| `Ctrl/⌘ + 1…9` | Go to tab (right-click a tab: close · close other/left/right/same-connection/all tabs · duplicate · rename) |
| `Ctrl + Tab` / `Ctrl + Shift + Tab` | Next / previous tab (also `Ctrl + PageDown/PageUp`, and `⌘ + Shift + ]/[` on macOS) |
| `Ctrl/⌘ + /` | Toggle comment |
| `Ctrl + Alt + L` / `⌘ + ⌥ + L` | Format SQL (selection or everything; one-line SQL becomes several lines) |
| `Ctrl/⌘ + F` | Find/replace in the editor |
| `Enter` / `F2` / double-click in the grid | Edit the cell (view the value if it can't be edited) |
| Typing in the grid | Start editing, replacing the cell's value |
| `Shift + Enter` in the grid | View the cell value |
| `F2` while editing a cell | Continue in the large editor |
| `Delete` in the grid | Delete / restore the selected rows (pending until applied) |
| `Ctrl/⌘ + S` in the grid | Apply changes |
| Double-click / middle-click a tab | Rename / close |

## Notes per database

- **Oracle**: connect with a "Service name" (e.g. `ORCL`, `FREEPDB1`, `XEPDB1`), or check "Use SID". 11g is supported; table previews use `ROWNUM`. On 12c and later the schema list hides Oracle's internal accounts (SYS, XDB, …).
- **SQL Server**: the tree's first level is databases. For a named instance, write the host as `host\SQLEXPRESS`. `TrustServerCertificate=true` is the default.
- **MySQL/MariaDB**: affected row counts are rows matched, not rows actually changed (`clientFoundRows`).
- **MySQL/MariaDB TLS**: `tls=true` in the extra options has the OS verify the server certificate. If that fails because the certificate was issued by your company or the server itself (macOS rejects such certificates more strictly, as "not standards compliant"), name that CA with `tls-ca=/path/ca.pem`; Go then verifies against it directly. To encrypt without verifying, use `tls=skip-verify` (DataGrip's default SSL behavior). Through an SSH tunnel, the certificate is still checked against the database host name.
- **Extra options** take driver options as `key=value&key2=value2`, e.g. `tls=true`, `sslmode=require`, `encrypt=true`.
- Transactions: autocommit by default. Each tab keeps one connection, so `BEGIN`/`START TRANSACTION` … `COMMIT`/`ROLLBACK` typed by hand run in the same session.

## Web version

The same app, run on a server and used from a browser (like dbgate's web version). Sign-in is with passkeys: Touch ID / Face ID on Mac and iPhone, Windows Hello (PIN, fingerprint, face) on Windows.

- There is one account. Every device signed in sees the same connection list and workspace (open tabs, SQL).
- The server connects to the databases, so only databases the server can reach are available.

### Running

Docker:

```bash
docker build -t tablume .
```

```bash
docker run -d --name tablume --restart unless-stopped -p 127.0.0.1:8080:8080 -v tablume:/data -e TABLUME_URL=https://db.example.com tablume
```

Or run the executable directly. This builds `build/bin/Tablume-server`:

```bash
wails3 task build:server
```

```bash
./build/bin/Tablume-server -url https://db.example.com
```

| Option | Environment variable | Default | Description |
|---|---|---|---|
| `-url` | `TABLUME_URL` | (optional on localhost) | The address typed in the browser. Passkeys are bound to its domain. Must be HTTPS (except localhost). |
| `-listen` | `TABLUME_LISTEN` | `127.0.0.1:8080` (Docker: `:8080`) | Address the server listens on |
| `-data` | `TABLUME_DATA` | the OS config folder (Docker: `/data`) | Connection list, workspace, passkeys, encryption key |
| `-files` | `TABLUME_FILES` | `<data>/files` | The only folder SQLite files and SSH keys are read from |
| `-reset-passkeys` | | | Remove every passkey, sign every device out and print a new setup link |

The executable is statically linked and needs nothing else installed. With systemd, a unit like this works:

```ini
[Unit]
Description=Tablume
After=network-online.target
Wants=network-online.target

[Service]
User=tablume
Environment=TABLUME_URL=https://db.example.com
Environment=TABLUME_LISTEN=127.0.0.1:8080
Environment=TABLUME_DATA=/var/lib/tablume
ExecStart=/usr/local/bin/tablume-server
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

### First sign-in

1. With no passkeys yet, the server prints a setup address in its log (`docker logs tablume`, `journalctl -u tablume`). It looks like `https://…/login?token=…` and changes when the server restarts.
2. Open it and click "Register a Passkey on This Device".
3. Other devices:
   - Apple devices share passkeys through iCloud Keychain and can sign in right away.
   - On a Windows PC, the sign-in page can use your phone's passkey by scanning a QR code.
   - To create a passkey (Windows Hello) on the PC itself, open View options → Account → "Manage Passkeys" → "Create Setup Link" on a signed-in device and open the one-time link (15 minutes) on the PC.
4. "Manage Passkeys" renames and deletes passkeys. Devices signed in with a deleted passkey are signed out. The last one can't be deleted.
5. A sign-in lasts 30 days from its last use.

### Reverse proxy

HTTPS is handled by a reverse proxy in front (nginx, Caddy, …). Two things must be set:

- The events WebSocket (`/wails/events`) has to pass through.
- Long queries answer late, so allow a generous read timeout (nginx's default is 60 seconds).

Caddy needs nothing extra:

```
db.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

nginx:

```nginx
map $http_upgrade $connection_upgrade { default upgrade; '' close; }

server {
    server_name db.example.com;
    # listen 443 ssl; ssl_certificate …

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
        client_max_body_size 50m;
    }
}
```

### Differences from the desktop app

- **Files**
  - SQLite files and SSH keys are picked only inside the server's files folder (`-files`). "Browse…" opens a browser of that folder. Other paths are refused when connecting.
  - For SQLite, a file name that doesn't exist creates a new database.
  - SQL can't reach other files either; this is enforced in the SQLite engine. `ATTACH` (except `:memory:`), `VACUUM INTO` and `PRAGMA temp_store_directory`/`data_store_directory` are refused, and extension loading (`load_extension`) is off. Only SQLite options that open no other file are accepted (`_pragma`, `_txlock`, `_time_format`, `_timezone`, `mode`, `cache`, `immutable`, `nolock`, `psow`).
  - The SSH key file can't be left empty: as in the desktop app that would mean `~/.ssh`, which here is the server's keys.
  - Saving `.sql` files and exporting results are downloads; opening `.sql` files is an upload.
- **Screen**
  - "Wide View" takes the place of opening a tab in a new window.
  - Zoom is the browser's zoom (`Ctrl/⌘ + +/-`).
  - Shortcuts the browser takes first (`Ctrl/⌘+T`, `Ctrl/⌘+W`, `Ctrl/⌘+1…9`, `Ctrl+Tab`, …) aren't shown in menus and hints.
- **Passwords**
  - Saved database and SSH passwords are stored AES-GCM encrypted in `connections.json` in the data folder instead of an OS keychain. The key is `secret.key` in the same folder.
  - So whoever has the whole data folder can decrypt them. Take care with backups of it.
- **Database sessions**
  - Each browser tab (device) gets its own database sessions. A phone and a PC open at once don't share transactions.
  - Closing a page closes its database sessions right away (open transactions are rolled back). If no close signal arrives, the server cleans them up after 30 minutes without contact.
  - The workspace (open tabs, SQL) is the one last saved, from whichever device.
- **Sign-in expiry**
  - When the sign-in expires, the next action returns to the sign-in page.

### Security design

- The app itself runs on a random port on `127.0.0.1`. The only port open to the outside is the front door that checks sign-in.
- The app refuses requests without the secret header the front door adds.
- Requests from other sites (POSTs and WebSockets with another Origin) are refused even with the cookie.
- A signed-in user can connect (or test a connection) to any host and port the server can reach, and open SSH tunnels, because the server makes the database connections. Error messages also reveal whether ports on the internal network are open, so limiting the server's (container's) outbound firewall to the databases it needs is recommended.
- Other programs on the same server can, however, listen in on the app's events WebSocket (connection status notifications and the like), because Wails offers no way to check that path. This doesn't apply in a Docker container (other than with `--network host`).

## Development

Requirements: Go 1.26+, Node 20+, the [Wails v3](https://v3.wails.io) CLI

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
```

```bash
wails3 dev
```

```bash
wails3 build
```

```bash
wails3 package
```

- `wails3 dev`: development with hot reload. Go changes rebuild; Vite applies frontend changes immediately.
- `wails3 build`: builds for the current OS into `build/bin/`. The build steps are in `Taskfile.yml`.
- `wails3 package`: a release build. On macOS it also makes `build/bin/Tablume.app`.
- The Go functions the frontend calls (`frontend/bindings/`) are generated from `app.go` during the build. To generate them by hand, run `wails3 task bindings`.
- Building on Linux needs `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`. Wails v3 defaults to GTK4, so the Taskfile adds the `gtk3` tag.
- A Windows `.exe` can be built on Linux or macOS too: `wails3 build GOOS=windows`.
- Builds can include only the databases you need: list the ones to leave out in `EXTRA_TAGS` (`no_mysql`, `no_postgres`, `no_mssql`, `no_oracle`, `no_sqlite`). With only MySQL/MariaDB, for example, the Windows `.exe` shrinks from about 38 MB to about 14 MB. Left-out databases don't appear when creating a connection, and saved connections to them explain why when connecting. GitHub Actions always makes full builds.

```bash
wails3 package EXTRA_TAGS=no_postgres,no_mssql,no_oracle,no_sqlite
```

- `wails3 task build:server` builds the [web version](#web-version). To try it locally, run it and register a passkey at the `http://localhost:8080/login?token=…` address in the log. Playwright tests use Chromium's virtual authenticator (CDP `WebAuthn.addVirtualAuthenticator`).
- The macOS menu bar (Tablume / Edit / Window) is built in `menu.go` in the app's language and rebuilt when it changes. Items macOS adds itself (Services, Start Dictation, …) follow the OS language (`build/darwin/Info.plist` declares English and Korean).
- The macOS app can only be built on macOS. If that's not at hand, use GitHub Actions below.

### Release builds with GitHub Actions

`.github/workflows/build.yml` runs when a `v*` tag is pushed, or by hand from the Actions tab. It builds a macOS (Apple Silicon) `.app` zip and a Windows `.exe` and uploads them as artifacts.

The macOS build isn't signed, so the first launch needs right-click → Open in Finder, or:

```bash
xattr -dr com.apple.quarantine Tablume.app
```

### Tests

```bash
go test ./internal/...
```

```bash
CGO_ENABLED=0 go test -tags server .
```

These are unit tests (statement splitting, SQLite, translations, the web front door, file restrictions). Integration tests against the five databases run like this. First start test databases with Docker:

```bash
docker run -d --name t-maria -e MARIADB_ROOT_PASSWORD=pw -e MARIADB_DATABASE=shop -p 3306:3306 mariadb:11
```

```bash
docker run -d --name t-pg -e POSTGRES_PASSWORD=pw -p 5432:5432 postgres:17-alpine
```

```bash
docker run -d --name t-ora -e ORACLE_PASSWORD=pw -e APP_USER=app -e APP_USER_PASSWORD=pw -p 1521:1521 gvenzl/oracle-free:slim-faststart
```

```bash
docker run -d --name t-mssql -e ACCEPT_EULA=Y -e 'MSSQL_SA_PASSWORD=Passw0rd!' -p 1433:1433 mcr.microsoft.com/mssql/server:2022-latest
```

When they are all up, run the integration tests:

```bash
TABLUME_IT=1 go test ./internal/dbx/ -v
```

## Layout

```
main.go, app.go            Wails entry point and the API exposed to the frontend (App service)
platform_desktop.go        Desktop-only setup
platform_web.go            Web version (-tags server): options, front door, session cleanup
files.go                   Web version: server file browsing and path restrictions
windowstate.go             Main window size, kept between runs
menu.go                    macOS menu bar
Taskfile.yml               Build steps (wails3 build / package / dev)
internal/dbx/              Database layer
  dialect.go               Common database interface and registration
  dialect_<db>.go          Per-database connection strings, metadata, DDL, column and index changes (leave out with the no_<db> build tag)
  manager.go               Connection pools, per-tab sessions, run/cancel/cursors
  split.go                 Splitting SQL scripts into statements (per-database rules)
internal/store/            Connection list (JSON) and keychain (encrypted in a file without one)
internal/web/              Web version front door: passkey sign-in (WebAuthn), sessions, proxy to the app
internal/i18n/             Backend message translations (en.go: English table)
frontend/                  Svelte 5 + CodeMirror 6 UI
  src/lib/i18n*.ts         Interface translations (i18n-en.ts: English table)
```

Translations: the Korean text is the key. The UI writes `t('새 콘솔')` and Go `i18n.T("…")` / `i18n.Errorf("…", …)`, and the English tables (`frontend/src/lib/i18n-en.ts`, `internal/i18n/en.go`) map the same text to English. `npm run check` (frontend) and `go test ./internal/i18n` (backend) list missing entries.

Settings are stored in:

- macOS: `~/Library/Application Support/Tablume/`
- Windows: `%AppData%\Tablume\`
- Web version: the folder given with `-data`

## Ideas for later

- A manual-commit mode toggle
- Query history
- Indexes/constraints in the tree
- Following multi-column foreign keys (today a single cell's value is looked up)

## License

[MIT](LICENSE)
