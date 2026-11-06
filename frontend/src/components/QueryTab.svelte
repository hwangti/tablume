<script lang="ts">
  import { num, t } from '../lib/i18n.svelte'
  import type { EditorView } from '@codemirror/view'
  import { flushSync } from 'svelte'
  import SqlEditor, { setExecMark } from './SqlEditor.svelte'
  import ResultGrid from './ResultGrid.svelte'
  import Icon from './Icon.svelte'
  import Dropdown from './Dropdown.svelte'
  import { closeMenus, onCloseMenus } from '../lib/fit'
  import SqlPreview from './SqlPreview.svelte'
  import FilterInput from './FilterInput.svelte'
  import StructureView from './StructureView.svelte'
  import IndexView from './IndexView.svelte'
  import FkView from './FkView.svelte'
  import { api, errText } from '../lib/api'
  import { modKey, isMac, copyText } from '../lib/clipboard'
  import {
    app,
    syncTreeRows,
    connById,
    runSQL,
    endTx,
    setManualCommit,
    fetchMore,
    cancel,
    loadCompletion,
    saveWorkspaceSoon,
    toast,
    confirmBox,
    reloadTable,
    tableCreated,
    connect,
    countResult,
    fetchPrev,
    jumpTo,
    fetchedCount,
    PAGE_SIZES,
    openConsole,
    type Tab,
    type Message,
    followForeignKey,
    logStatements,
    treeChanged,
  } from '../lib/state.svelte'
  import { loadEditInfo, pendingCount, tabPending, revert, previewChanges, applyChanges } from '../lib/editing.svelte'
  import type { Cell, LogEntry, StmtResult } from '../lib/types'

  let { tab }: { tab: Tab } = $props()

  let view = $state<EditorView>()
  let editor = $state<ReturnType<typeof SqlEditor>>()
  let split = $state(45) // editor height in %
  let body: HTMLDivElement
  let tick = $state(0)

  let conn = $derived(connById(tab.connId))
  let st = $derived(app.conn[tab.connId])
  let connected = $derived(st?.status === 'connected')
  let schemaOptions = $derived.by(() => {
    const list = st?.schemas.map((s) => s.name) ?? []
    return tab.schema && !list.includes(tab.schema) ? [tab.schema, ...list] : list
  })
  let completion = $derived(app.completion[tab.connId + '|' + tab.schema] ?? {})
  // Table data tabs (opened from the tree) show only the data, its DDL and a log.
  let isTable = $derived(!!tab.tableRef)

  // ---- the log pane: this tab's lines, or the connection's saved run log
  let logQuery = $state('')
  let connLog = $state<LogEntry[] | null>(null)
  let logTimer: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    if (tab.logScope !== 'conn') return
    const id = tab.connId
    const q = logQuery
    void app.logVersion // a new entry was saved
    clearTimeout(logTimer)
    logTimer = setTimeout(async () => {
      try {
        connLog = (await api.ReadLog(id, q, 500)) ?? []
      } catch (e) {
        toast(errText(e), 'error')
      }
    }, 250)
  })
  let tabLog = $derived.by(() => {
    const q = logQuery.trim().toLowerCase()
    const all = tab.messages.slice().reverse()
    return q ? all.filter((m) => `${m.full ?? m.sql ?? ''}\n${m.text}`.toLowerCase().includes(q)) : all
  })
  const pad2 = (n: number) => String(n).padStart(2, '0')
  function logTime(ms: number) {
    const d = new Date(ms)
    const time = `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
    return d.toDateString() === new Date().toDateString() ? time : `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${time}`
  }
  // a long script in one line of the log: its start (the button puts it all)
  const clip = (sql: string) => {
    const lines = sql.split('\n')
    const head = lines.slice(0, 8).join('\n')
    return head.length > 800 ? head.slice(0, 800) + '…' : lines.length > 8 ? head + '\n…' : head
  }
  /** Puts a logged statement in this console's editor (a table tab: a new console). */
  function useSql(sql: string) {
    sql = sql.trim()
    if (!/;\s*$/.test(sql)) sql += ';' // the next statement typed after it stays apart
    if (isTable) {
      openConsole(tab.connId, { schema: tab.schema, sql })
      return
    }
    tab.sql = tab.sql.trim() ? tab.sql.replace(/\s*$/, '\n\n') + sql : sql
    if (tab.layout === 'results') tab.layout = 'split'
    saveWorkspaceSoon()
  }
  async function clearConnLog() {
    if (!(await confirmBox(t('이 연결에 저장된 실행 로그를 모두 지울까요?')))) return
    try {
      await api.ClearLog(tab.connId)
      connLog = []
    } catch (e) {
      toast(errText(e), 'error')
    }
  }
  const msgSql = (m: Message) => m.full ?? m.sql ?? ''
  let tableView = $state<'data' | 'ddl' | 'log'>('data')
  let current = $derived<StmtResult | undefined>(
    isTable ? tab.results[0] : tab.activeResult >= 0 ? tab.results[tab.activeResult] : undefined,
  )
  // A table tab restored from the last session loads when it is shown and
  // its connection is already up (otherwise it offers "connect and load").
  let autoLoaded = false
  $effect(() => {
    if (autoLoaded || !isTable || tab.creating) return
    // loaded or being loaded some other way ("connect and load"): not again
    if (current || tab.running || tab.opening) return void (autoLoaded = true)
    if (app.activeTab !== tab.id || !connected) return
    autoLoaded = true
    reloadTable(tab)
  })

  // Positioned against the window: the result bar clips overflow.
  let exportMenu = $state<{ right: number; top: number } | null>(null)
  $effect(() => onCloseMenus(() => ((exportMenu = null), (sizeMenu = null))))

  function toggleExport(e: MouseEvent) {
    e.stopPropagation()
    if (exportMenu) {
      exportMenu = null
      return
    }
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    closeMenus()
    exportMenu = { right: window.innerWidth - r.right, top: r.bottom + 4 }
  }

  // DDL for table tabs, loaded when first shown and again if the tab is
  // reused for another table.
  let ddl = $state<string | null>(null)
  let ddlError = $state('')
  let ddlFor = ''
  let ddlVersion = $state(0) // bumped when the columns were altered
  // a key icon clicked in the columns: the index / foreign key rows to show
  // (cleared after the flash: a structure view made again later, after the
  // data or log, must not scroll to it again)
  let keyHl = $state<{ kind: 'fk' | 'index'; column: string; n: number } | null>(null)
  let keyHlTimer = 0
  let keyHlN = 0 // only grows: each click is a new request
  function showKey(kind: 'fk' | 'index', column: string) {
    keyHl = { kind, column, n: ++keyHlN }
    clearTimeout(keyHlTimer)
    keyHlTimer = window.setTimeout(() => (keyHl = null), 2000)
  }
  $effect(() => {
    const ref = tab.tableRef
    if (!ref || tableView !== 'ddl' || !connected) return // (restored tab: after "연결하고 불러오기")
    const key = `${tab.connId}|${ref.schema}|${ref.table.schema}.${ref.table.name}|${ddlVersion}`
    if (key === ddlFor) return
    ddlFor = key
    ddl = null
    ddlError = ''
    api
      .TableDDL(tab.connId, ref.schema, $state.snapshot(ref.table))
      .then((d) => key === ddlFor && (ddl = d))
      .catch((e) => key === ddlFor && (ddlError = errText(e)))
  })

  function openAsConsole() {
    openConsole(tab.connId, { schema: tab.tableRef?.schema ?? tab.schema, sql: tab.sql + ';' })
  }

  $effect(() => {
    if (connected && tab.schema) loadCompletion(tab.connId, tab.schema)
  })

  // Find out early whether the shown result is editable (for the lock icon).
  $effect(() => {
    if (current && !current.edit?.info && connected) loadEditInfo(tab, current)
  })

  let grid = $state<ResultGrid>()
  let pending = $derived(pendingCount(current))
  let editInfo = $derived(current?.edit?.info ?? null)

  async function guardPending(): Promise<boolean> {
    const n = tabPending(tab)
    return n === 0 || confirmBox(t('적용하지 않은 변경 {n}건이 있습니다. 버리고 실행할까요?', { n }))
  }

  // Grid edits go straight in (one transaction); the statements and the
  // outcome are in the tab's log.
  let applying = $state(false)
  async function openApply() {
    if (!current || !pending || applying) return
    applying = true
    let sql: string[] = []
    try {
      sql = (await previewChanges(tab, current)) ?? []
      const { count, inserted } = await applyChanges(tab, current)
      logStatements(tab, sql, t('{n}건 적용', { n: count }))
      toast(tab.manual ? t('{n}건 적용됨 · 커밋해야 반영됩니다', { n: count }) : t('{n}건 적용됨', { n: count }))
      if (inserted) await rerun() // show generated values (auto ids, defaults)
    } catch (e) {
      if (sql.length) logStatements(tab, sql, '', errText(e) + '\n' + t('(모두 취소됨)'))
      toast(errText(e), 'error')
    } finally {
      applying = false
    }
  }

  // structure tab: columns / indexes were changed
  function structureChanged() {
    ddlVersion++
    const ref = tab.tableRef
    if (ref) treeChanged(tab.connId, [ref.schema], ref.table.name)
  }

  // ---- table data filter bar (WHERE / ORDER BY run on the database)
  let whereDraft = $state('')
  let orderDraft = $state('')
  $effect(() => {
    whereDraft = tab.tableRef?.where ?? ''
    orderDraft = tab.tableRef?.orderBy ?? ''
  })

  function applyFilter() {
    const ref = tab.tableRef
    if (!ref) return
    if (ref.orderBy !== orderDraft) ref.sort = undefined
    ref.where = whereDraft
    ref.orderBy = orderDraft
    reloadTable(tab)
  }

  function revertFilter() {
    whereDraft = tab.tableRef?.where ?? ''
    orderDraft = tab.tableRef?.orderBy ?? ''
  }

  const WHERE_KW = ['AND', 'OR', 'NOT', 'IN', 'LIKE', 'IS NULL', 'IS NOT NULL', 'BETWEEN', 'EXISTS', 'NULL', 'TRUE', 'FALSE']
  const ORDER_KW = ['ASC', 'DESC', 'NULLS FIRST', 'NULLS LAST']
  let filterCols = $derived((current?.columns ?? []).map((c) => c.name))

  function clearFilter(which: 'where' | 'order') {
    if (which === 'where') whereDraft = ''
    else orderDraft = ''
    applyFilter()
  }

  // Header click on a table data tab: ORDER BY on the server, cycling
  // ascending -> descending -> off.
  async function serverSortBy(c: number) {
    const ref = tab.tableRef
    if (!ref || !current?.columns) return
    const col = current.columns[c].name
    const dir = ref.sort?.col !== col ? 'ASC' : ref.sort.dir === 'ASC' ? 'DESC' : null
    try {
      ref.sort = dir ? { col, dir } : undefined
      ref.orderBy = dir ? `${await api.QuoteIdent(tab.connId, col)} ${dir}` : ''
    } catch (e) {
      toast(errText(e), 'error')
      return
    }
    reloadTable(tab)
  }

  // ---- pager: the loaded rows are a window [start, start + loaded) of the
  // result; scrolling grows it, the buttons move it.
  let start = $derived(current?.start ?? 0)
  let loaded = $derived(current ? fetchedCount(current) : 0)
  let total = $derived(current?.total ?? null)
  let counting = $state(false)
  // Everything fits in what's loaded: just the row count, no paging.
  let whole = $derived(!!current && start === 0 && !current.hasMore)

  // exact row count of an unfiltered table → the tree's row count
  $effect(() => {
    const ref = tab.tableRef
    if (!isTable || !ref || ref.where.trim() || !current || current.error || tab.running) return
    const n = whole ? loaded : total
    if (n !== null && n >= 0) syncTreeRows(tab.connId, ref.schema, ref.table, n)
  })
  let sizeMenu = $state<{ right: number; top: number } | null>(null)

  async function count() {
    if (!current?.paged) return
    counting = true
    await countResult(tab, current)
    counting = false
  }

  async function loadMore() {
    if (!current) return
    const dropped = await fetchMore(tab, current)
    if (dropped) {
      flushSync()
      grid?.shiftRows(-dropped)
    }
  }

  async function loadPrev() {
    if (!current) return
    const added = await fetchPrev(tab, current)
    if (added) {
      flushSync()
      // at the first row the "load previous" bar above the rows goes away
      grid?.shiftRows(added, (current.start ?? 0) === 0 ? -1 : 0)
    }
  }

  // The buttons load a fresh page in place of what is shown (scrolling is
  // what accumulates rows). Results that can't be paged (an open cursor)
  // only scroll / load on.
  async function goFirst() {
    const r = current
    if (!r) return
    if (r.paged && !(await jumpTo(tab, r, 0))) return
    flushSync()
    grid?.scrollToRow(0)
  }

  async function goLast() {
    const r = current
    if (!r) return
    if (r.paged) {
      let t = r.total ?? null
      if (t === null) {
        counting = true
        t = await countResult(tab, r)
        counting = false
      }
      if (t === null || !(await jumpTo(tab, r, Math.max(0, t - app.pageSize)))) return
    } else {
      while (r.hasMore && !tab.loadingMore) await fetchMore(tab, r)
    }
    flushSync()
    grid?.scrollToRow(fetchedCount(r) - 1)
  }

  // ◀ ▶: the page right before / after the loaded rows (1–1,000 loaded,
  // page size 500: ▶ shows 1,001–1,500).
  async function goStep(dir: 1 | -1) {
    const r = current
    if (!r || !grid) return
    const target = dir > 0 ? start + fetchedCount(r) : Math.max(0, start - app.pageSize)
    if (r.paged) {
      if (dir < 0 && start === 0) {
        grid.scrollToRow(0)
        return
      }
      const end = r.total ?? (r.hasMore ? Infinity : start + fetchedCount(r))
      if (dir > 0 && target >= end) return
      if (!(await jumpTo(tab, r, target))) return
      flushSync()
      grid.scrollToRow(0)
      return
    }
    while (dir > 0 && target >= fetchedCount(r) && r.hasMore && !tab.loadingMore) await fetchMore(tab, r)
    flushSync()
    grid.scrollToRow(Math.min(fetchedCount(r) - 1, target))
  }

  function toggleSizeMenu(e: MouseEvent) {
    e.stopPropagation()
    if (sizeMenu) {
      sizeMenu = null
      return
    }
    const b = (e.currentTarget as HTMLElement).getBoundingClientRect()
    closeMenus()
    sizeMenu = { right: window.innerWidth - b.right, top: b.bottom + 4 }
  }

  // WHERE | ORDER BY split, shared by all table tabs (app.orderShare)
  let filterBar = $state<HTMLDivElement>()
  let splitting = $state(false)
  function startSplit(e: MouseEvent) {
    if (e.button !== 0 || !filterBar) return
    e.preventDefault()
    const r = filterBar.getBoundingClientRect()
    splitting = true
    const move = (ev: MouseEvent) => {
      const share = (r.right - ev.clientX) / r.width
      app.orderShare = Math.round(Math.max(0.12, Math.min(0.75, share)) * 1000) / 1000
    }
    const up = () => {
      splitting = false
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      document.body.style.cursor = ''
      saveWorkspaceSoon()
    }
    document.body.style.cursor = 'col-resize'
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  function setPageSize(n: number) {
    app.pageSize = n
    sizeMenu = null
    saveWorkspaceSoon()
  }

  let serverSort = $derived.by(() => {
    const s = tab.tableRef?.sort
    if (!s || !current?.columns) return null
    const col = current.columns.findIndex((c) => c.name === s.col)
    return col < 0 ? null : { col, dir: s.dir === 'ASC' ? (1 as const) : (-1 as const) }
  })

  async function rerun() {
    if (!current || !(await guardPending())) return
    await runSQL(tab, current.sql, -1)
  }

  function resultsKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
      e.preventDefault()
      openApply()
    }
  }

  let busy = $derived(tab.running || !!tab.opening)

  $effect(() => {
    if (!busy) return
    const h = setInterval(() => tick++, 100)
    return () => clearInterval(h)
  })

  function mark(results: StmtResult[] | null) {
    if (!view || !results || !results.length) return
    const len = view.state.doc.length
    const from = Math.min(results[0].from, len)
    const to = Math.min(results[results.length - 1].to, len)
    view.dispatch({ effects: setExecMark.of({ from, to }) })
  }

  async function run() {
    if (!view || !(await guardPending())) return
    const s = view.state.selection.main
    const doc = view.state.doc.toString()
    const res = s.empty ? await runSQL(tab, doc, s.head) : await runSQL(tab, doc.slice(s.from, s.to), -1, s.from)
    mark(res)
  }

  async function runAll() {
    if (!view || !(await guardPending())) return
    const s = view.state.selection.main
    const doc = view.state.doc.toString()
    const res = s.empty ? await runSQL(tab, doc, -1) : await runSQL(tab, doc.slice(s.from, s.to), -1, s.from)
    mark(res)
  }

  function toggleLayout() {
    tab.layout = tab.layout === 'results' ? 'split' : 'results'
    saveWorkspaceSoon()
    if (tab.layout === 'split') requestAnimationFrame(() => view?.focus())
  }

  function onSqlChange(v: string) {
    if (v !== tab.sql) {
      tab.preview = false
      tab.tableRef = undefined // hand-written SQL: the filter bar no longer applies
    }
    tab.sql = v
    saveWorkspaceSoon()
  }

  function changeConn(id: string) {
    tab.connId = id
    tab.schema = app.conn[id]?.current ?? ''
    tab.results = []
    saveWorkspaceSoon()
  }

  function startDrag(e: MouseEvent) {
    e.preventDefault()
    const rect = body.getBoundingClientRect()
    const move = (ev: MouseEvent) => {
      split = Math.max(10, Math.min(90, ((ev.clientY - rect.top) / rect.height) * 100))
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  async function openFile() {
    try {
      const f = await api.OpenSQLFile()
      if (f) {
        tab.sql = f.content
        tab.sqlBase = f.content
        tab.title = f.name
        saveWorkspaceSoon()
      }
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  async function saveFile() {
    const name = tab.title.endsWith('.sql') ? tab.title : tab.title.replace(/[\\/:*?"<>|]/g, '_') + '.sql'
    try {
      const sql = tab.sql
      const p = await api.SaveTextFile(name, sql)
      if (p) {
        tab.sqlBase = sql
        saveWorkspaceSoon()
        toast(t('저장됨') + ': ' + p)
      }
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  function csvCell(v: Cell, sep: string): string {
    if (v === null) return ''
    return v.includes(sep) || /["\n\r]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v
  }

  const numericType = /^(UNSIGNED )?(TINYINT|SMALLINT|MEDIUMINT|INT|INTEGER|BIGINT|INT[248]|NUMBER|NUMERIC|DECIMAL|FLOAT[48]?|DOUBLE|REAL|MONEY|SMALLMONEY|BINARY_FLOAT|BINARY_DOUBLE)\b/

  async function insertStatements(r: StmtResult): Promise<string> {
    const cols = r.columns!
    const info = r.edit?.info
    const target =
      info && info.table.name
        ? await api.QualifiedName(tab.connId, info.schema, $state.snapshot(info.table))
        : r.source?.join('.') || 'table_name'
    const names = await Promise.all(cols.map((c) => api.QuoteIdent(tab.connId, c.name)))
    const num = cols.map((c) => numericType.test(c.type))
    const lit = (v: Cell, i: number) => (v === null ? 'NULL' : num[i] ? v : "'" + v.replace(/'/g, "''") + "'")
    const head = `INSERT INTO ${target} (${names.join(', ')}) VALUES `
    return r.rows!.map((row) => head + '(' + row.map(lit).join(', ') + ');').join('\n') + '\n'
  }

  async function exportResult(kind: 'csv' | 'tsv' | 'json' | 'sql') {
    exportMenu = null
    const r = current
    if (!r || !r.columns) return
    const cols = r.columns.map((c) => c.name)
    let text: string
    try {
      if (kind === 'csv' || kind === 'tsv') {
        const sep = kind === 'csv' ? ',' : '\t'
        const line = (vals: Cell[]) => vals.map((v) => csvCell(v, sep)).join(sep)
        text = (kind === 'csv' ? '\ufeff' : '') + [line(cols), ...r.rows!.map(line)].join('\r\n')
      } else if (kind === 'json') {
        text = JSON.stringify(
          r.rows!.map((row) => Object.fromEntries(cols.map((c, i) => [c, row[i]]))),
          null,
          2,
        )
      } else {
        text = await insertStatements(r)
      }
      const base = (r.edit?.info?.table.name || r.source?.[r.source.length - 1] || 'result').replace(/[\\/:*?"<>|]/g, '_')
      const p = await api.SaveTextFile(`${base}.${kind}`, text)
      if (p) toast(t(r.hasMore ? '{n}행 내보냄 (불러온 행만)' : '{n}행 내보냄', { n: num(r.rows!.length) }) + ': ' + p)
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  function resultLabel(r: StmtResult, i: number) {
    const m = r.sql.match(/\bfrom\s+([^\s,;()]+)/i)
    return m ? m[1].replace(/["`[\]]/g, '') : t('결과 {n}', { n: i + 1 })
  }

  let elapsed = $derived.by(() => {
    void tick
    return busy ? ((Date.now() - tab.startedAt) / 1000).toFixed(1) + 's' : ''
  })
</script>

<svelte:window onclick={() => ((exportMenu = null), (sizeMenu = null))} />

{#snippet logList(hint: boolean)}
  <div class="messages">
    {#if tab.logScope === 'conn'}
      {#if connLog?.length === 0}
        <div class="placeholder"><p>{logQuery.trim() ? t('찾는 로그가 없습니다') : t('저장된 실행 로그가 없습니다')}</p></div>
      {/if}
      {#each connLog ?? [] as m, i (i)}
        <div class="msg" class:error={m.kind === 'error'} class:auto={m.auto}>
          <span class="time">{logTime(m.time)}</span>
          <div class="mbody">
            <SqlPreview code={clip(m.sql)} driver={conn?.driver} bare />
            <pre>{m.text}{#if m.tab}<span class="from"> · {m.tab}</span>{/if}</pre>
          </div>
          <button class="icon-btn use" title={t('콘솔에 넣기')} onclick={() => useSql(m.sql)}><Icon name="console" size={13} /></button>
        </div>
      {/each}
    {:else}
      {#if tab.messages.length === 0 && hint}
        <div class="placeholder">
          <p>{modKey}Enter — {t('커서가 있는 문장 실행 (선택했으면 선택 영역)')}</p>
          <p>{modKey}Shift+Enter — {t('전체 실행')}</p>
        </div>
      {:else if tabLog.length === 0 && logQuery.trim()}
        <div class="placeholder"><p>{t('찾는 로그가 없습니다')}</p></div>
      {/if}
      {#each tabLog as m, i (tabLog.length - i)}
        <div class="msg" class:error={m.kind === 'error'} class:auto={m.auto}>
          <span class="time">{m.time}</span>
          <div class="mbody">
            {#if m.sql}<SqlPreview code={m.sql} driver={conn?.driver} bare />{/if}
            <pre>{m.text}</pre>
          </div>
          {#if msgSql(m)}
            <button class="icon-btn use" title={t('콘솔에 넣기')} onclick={() => useSql(msgSql(m))}><Icon name="console" size={13} /></button>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
{/snippet}

<!-- Manual commit: the mode (a toggle), and Commit / Rollback. Those are
     always there (off in autocommit), so switching moves nothing. -->
{#snippet txControls()}
  <button
    class="txbtn"
    class:on={tab.manual}
    onclick={() => setManualCommit(tab, !tab.manual)}
    title={tab.manual ? t('수동 커밋') + ' — ' + t('커밋할 때까지 변경을 트랜잭션에 둡니다') : t('자동 커밋') + ' — ' + t('문장마다 바로 반영합니다')}
    >Tx: {tab.manual ? t('수동') : t('자동')}</button
  >
  <button class="txbtn commit" onclick={() => endTx(tab, true)} disabled={!tab.inTx || tab.running} title={tab.manual ? t('변경을 반영합니다') : t('수동 커밋에서 씁니다')}>
    <Icon name="check" size={14} />{t('커밋')}
  </button>
  <button class="txbtn" onclick={() => endTx(tab, false)} disabled={!tab.inTx || tab.running} title={tab.manual ? t('변경을 버립니다') : t('수동 커밋에서 씁니다')}>
    <Icon name="undo" size={14} />{t('롤백')}
  </button>
{/snippet}

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="tab">
  {#if !isTable}
    <div class="toolbar">
      <button class="icon-btn run" title="{t('실행')} ({modKey}Enter) — {t('커서 위치 문장 또는 선택 영역')}" onclick={run} disabled={tab.running}>
        <Icon name="play" />
      </button>
      <button class="icon-btn run" title="{t('전체 실행')} ({modKey}Shift+Enter)" onclick={runAll} disabled={tab.running}>
        <Icon name="playAll" />
      </button>
      <button class="icon-btn stop" title={t('취소')} onclick={() => cancel(tab)} disabled={!tab.running}>
        <Icon name="stop" />
      </button>
      {#if !conn?.readOnly}
        <span class="sep"></span>
        {@render txControls()}
      {/if}
      <span class="sep"></span>
      <button class="icon-btn" title="{t('SQL 정렬')} ({isMac ? '⌘⌥L' : 'Ctrl+Alt+L'}) — {t('선택 영역 또는 전체')}" onclick={() => editor?.format()}
        ><Icon name="format" /></button
      >
      <span class="sep"></span>
      <button class="icon-btn" title={t('SQL 파일 열기')} onclick={openFile}><Icon name="open" /></button>
      <button class="icon-btn" title={t('SQL 파일로 저장')} onclick={saveFile}><Icon name="save" /></button>
      <span class="grow"></span>
      {#if tab.running}
        <span class="running"><span class="spin"></span> {t('실행 중')} {elapsed}</span>
      {:else if tab.inTx}
        <span class="txwait">{t('커밋 대기 중')}</span>
      {/if}
      <span class="sep"></span>
      <Dropdown
        value={tab.connId}
        options={app.connections.map((c) => ({ value: c.id, label: c.name }))}
        onchange={changeConn}
        disabled={tab.inTx}
        title={tab.inTx ? t('커밋하거나 롤백한 뒤 연결을 바꿀 수 있습니다') : t('연결#선택')}
      />
      <Dropdown
        value={tab.schema}
        options={schemaOptions.map((s) => ({ value: s, label: s }))}
        onchange={(v) => ((tab.schema = v), saveWorkspaceSoon())}
        placeholder="({t('스키마')})"
        disabled={!connected || conn?.driver === 'sqlite'}
        title={t('기본 스키마 / 데이터베이스')}
      />
    </div>
  {/if}

  <div class="body" bind:this={body}>
    {#if !isTable}
      <div class="editor" class:collapsed={tab.layout === 'results'} style:height="{split}%">
        <SqlEditor value={tab.sql} driver={conn?.driver} schema={completion} onchange={onSqlChange} onrun={run} onrunall={runAll} bind:view bind:this={editor} />
      </div>
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="divider"
        class:collapsed={tab.layout === 'results'}
        onmousedown={startDrag}
        ondblclick={toggleLayout}
        title={t('드래그: 크기 조절 · 더블클릭: 결과만 보기')}
      ></div>
    {/if}
    <div class="results">
      <div class="rtabs">
        {#if tab.creating}
          <span class="rtab on"><Icon name="table" size={13} /> {t('새 테이블')}</span>
        {:else if isTable}
          <button class="rtab" class:on={tableView === 'data'} onclick={() => (tableView = 'data')}>
            <Icon name="table" size={13} /> {t('데이터')}
          </button>
          <button class="rtab" class:on={tableView === 'ddl'} onclick={() => (tableView = 'ddl')}>
            <Icon name="ddl" size={13} /> {t('구조')}
          </button>
          <button class="rtab" class:on={tableView === 'log'} onclick={() => (tableView = 'log')}>
            {t('로그')}
            {#if tab.messages.some((m) => m.kind === 'error')}<span class="dot"></span>{/if}
          </button>
          {#if busy}<span class="running small"><span class="spin"></span> {elapsed}</span>{/if}
        {:else}
          {#each tab.results as r, i (i)}
            <button class="rtab" class:on={tab.activeResult === i} onclick={() => (tab.activeResult = i)}>
              <Icon name="table" size={13} />
              {resultLabel(r, i)}
            </button>
          {/each}
          <button class="rtab" class:on={tab.activeResult === -1} onclick={() => (tab.activeResult = -1)}>
            {t('로그')}
            {#if tab.messages.some((m) => m.kind === 'error')}<span class="dot"></span>{/if}
          </button>
        {/if}
        <span class="grow"></span>
        <!-- buttons in groups by role, separated by a line (.grp + .grp) -->
        {#if !tab.creating && (isTable ? tableView === 'log' : !current)}
          <div class="grp">
            <input class="logq" type="search" placeholder={t('로그 검색')} bind:value={logQuery} />
            <div class="seg" role="group">
              <button class:on={tab.logScope !== 'conn'} onclick={() => (tab.logScope = 'tab')}>{t('이 탭')}</button>
              <button class:on={tab.logScope === 'conn'} onclick={() => (tab.logScope = 'conn')} title={t('이 연결에서 실행한 것 전부 (저장됨)')}>{t('이 연결 전체')}</button>
            </div>
            {#if tab.logScope === 'conn'}
              <button class="icon-btn" title={t('저장된 실행 로그 지우기')} onclick={clearConnLog}><Icon name="trash" size={14} /></button>
            {/if}
          </div>
        {/if}
        {#if !isTable}
          <div class="grp">
            <button
              class="icon-btn"
              title={tab.layout === 'results' ? t('SQL 에디터 보이기') : t('결과만 크게 보기')}
              onclick={toggleLayout}
            >
              <Icon name={tab.layout === 'results' ? 'restore' : 'maximize'} size={14} />
            </button>
          </div>
        {/if}
        {#if isTable && tableView === 'data' && !current && !busy}
          <!-- restored tab, not loaded yet -->
          <div class="grp">
            <button class="icon-btn" title={connected ? t('불러오기') : t('연결하고 불러오기')} onclick={() => reloadTable(tab)}>
              <Icon name="refresh" size={14} />
            </button>
            <button class="icon-btn" title={t('이 SELECT를 SQL 콘솔에서 열기')} onclick={openAsConsole} disabled={!tab.sql}><Icon name="console" size={14} /></button>
          </div>
        {/if}
        {#if current && (!isTable || tableView === 'data')}
          {#if pending > 0}
            <div class="grp">
              <span class="pending">{t('변경 {n}건', { n: pending })}</span>
              <button class="btn mini primary" onclick={openApply} title="{t('변경 적용')} ({modKey}Enter · {modKey}S)"><Icon name="check" size={13} /> {t('적용')}</button>
              <button class="btn mini" onclick={() => current && revert(current)} title={t('모든 변경 되돌리기')}><Icon name="undo" size={13} /> {t('되돌리기')}</button>
            </div>
          {/if}
          {#if isTable && !conn?.readOnly}
            <div class="grp">{@render txControls()}</div>
          {/if}
          {#if editInfo}
            <div class="grp">
              {#if !editInfo.editable}
                <span class="ro" title={t('읽기 전용') + ': ' + editInfo.reason}><Icon name="lock" size={13} /> {t('읽기 전용')}</span>
              {/if}
              {#if editInfo.editable || editInfo.insertable}
                <button class="icon-btn" title={t('행 추가')} onclick={() => grid?.newRow()}><Icon name="plus" size={14} /></button>
                <button class="icon-btn" title={(editInfo.editable ? t('선택 행 삭제') : t('추가한 행 취소')) + ' (Delete)'} onclick={() => grid?.deleteRows()}><Icon name="minus" size={14} /></button>
              {/if}
            </div>
          {/if}
          <div class="grp">
            <button class="icon-btn" title={t('다시 조회')} onclick={isTable ? () => reloadTable(tab) : rerun} disabled={tab.running}>
              <Icon name="refresh" size={14} />
            </button>
            <div class="dropdown">
              <button class="icon-btn split" class:active={!!exportMenu} onclick={toggleExport} title={t('결과 내보내기')}>
                <Icon name="download" size={14} /><Icon name="chevronDown" size={10} />
              </button>
              {#if exportMenu}
                <div class="menu" style:right="{exportMenu.right}px" style:top="{exportMenu.top}px">
                  <button onclick={() => exportResult('csv')}>CSV <span class="dim">.csv</span></button>
                  <button onclick={() => exportResult('tsv')}>TSV <span class="dim">.tsv</span></button>
                  <button onclick={() => exportResult('json')}>JSON <span class="dim">.json</span></button>
                  <button onclick={() => exportResult('sql')}>{t('INSERT 문')} <span class="dim">.sql</span></button>
                  <div class="hint">{t(current.hasMore ? '불러온 {n}행 (전체 아님)' : '불러온 {n}행', { n: num(current.rows!.length) })}</div>
                  {#if tab.tableRef}
                    <hr />
                    <button onclick={() => ((exportMenu = null), (app.dataDialog = { kind: 'export', connId: tab.connId, schema: tab.tableRef!.schema, tables: [tab.tableRef!.table] }))}
                      >{t('테이블 전체 내보내기…')}</button
                    >
                  {/if}
                </div>
              {/if}
            </div>
            {#if isTable}
              <button class="icon-btn" title={t('이 SELECT를 SQL 콘솔에서 열기')} onclick={openAsConsole}><Icon name="console" size={14} /></button>
            {/if}
          </div>
        {/if}
        {#if isTable && tableView !== 'data' && !tab.creating}
          <div class="grp">
            {#if tableView === 'ddl'}
              <div class="seg" role="group">
                <button class:on={app.ddlMode === 'table'} onclick={() => ((app.ddlMode = 'table'), saveWorkspaceSoon())}>{t('컬럼·인덱스')}</button>
                <button class:on={app.ddlMode === 'sql'} onclick={() => ((app.ddlMode = 'sql'), saveWorkspaceSoon())}>DDL</button>
              </div>
              {#if app.ddlMode === 'sql' && ddl}
                <button class="btn mini" onclick={() => ddl && copyText(ddl)}><Icon name="copy" size={13} /> {t('복사')}</button>
              {/if}
            {/if}
            <button class="icon-btn" title={t('이 SELECT를 SQL 콘솔에서 열기')} onclick={openAsConsole}><Icon name="console" size={14} /></button>
          </div>
        {/if}
        {#if current && (!isTable || tableView === 'data') && current.columns?.length}
          <div class="pager dropdown grp">
            <button class="icon-btn sm" title={t('처음으로')} disabled={busy || whole} onclick={goFirst}><Icon name="first" size={13} /></button>
            <button class="icon-btn sm" title={t('이전 {n}행', { n: num(app.pageSize) })} disabled={busy || whole} onclick={() => goStep(-1)}><Icon name="prev" size={13} /></button>
            <button class="range" class:open={!!sizeMenu} title={t('불러온 행 · 눌러서 페이지 크기 변경')} onclick={toggleSizeMenu}>
              {#if whole}{num(loaded)}{:else}{num(start + 1)}–{num(start + loaded)}{/if}
              <Icon name="chevronDown" size={10} />
            </button>
            {#if whole}
              <!-- all rows shown: the count is the range -->
            {:else if total !== null}
              <button class="total" title={current.paged ? t('다시 세기 (COUNT)') : t('전체 행 수')} onclick={count} disabled={counting || !current.paged}>{num(total)}</button>
            {:else}
              <button class="total unknown" title={current.paged ? t('전체 행 수 세기 (COUNT)') : t('이 쿼리는 셀 수 없습니다')} onclick={count} disabled={counting || !current.paged}>
                {counting ? t('세는 중…') : num(start + loaded + 1) + '+'}
              </button>
            {/if}
            <button class="icon-btn sm" title={t('다음 {n}행', { n: num(app.pageSize) })} disabled={busy || whole} onclick={() => goStep(1)}><Icon name="next" size={13} /></button>
            <button class="icon-btn sm" title={t('마지막으로')} disabled={busy || whole || (!current.hasMore && total !== null && start + loaded >= total)} onclick={goLast}><Icon name="last" size={13} /></button>
            {#if sizeMenu}
              <div class="menu size-menu" style:right="{sizeMenu.right}px" style:top="{sizeMenu.top}px">
                {#each PAGE_SIZES as n (n)}
                  <button class:on={app.pageSize === n} onclick={() => setPageSize(n)}>{num(n)}</button>
                {/each}
              </div>
            {/if}
            <span class="meta">{current.elapsedMs}ms</span>
          </div>
        {/if}
      </div>
      {#if isTable && tableView === 'data' && tab.tableRef && !tab.creating}
        <!-- DataGrip-style: part of the grid's surface, no boxed inputs -->
        <div class="filterbar" bind:this={filterBar}>
          <div class="fpart">
            <span class="ficon where"><Icon name="filter" size={14} /></span>
            <span class="kw" class:on={!!tab.tableRef.where}>WHERE</span>
            <div class="fwrap">
              <FilterInput bind:value={whereDraft} columns={filterCols} keywords={WHERE_KW} onsubmit={applyFilter} onrevert={revertFilter} />
              {#if tab.tableRef.where}<button class="x-btn fclear" title={t('WHERE 지우기')} onclick={() => clearFilter('where')}><Icon name="close" size={14} /></button>{/if}
            </div>
          </div>
          <div class="fpart order" style:flex="0 0 {app.orderShare * 100}%">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="fsplit" class:on={splitting} title={t('끌어서 폭 조정 · 더블클릭하면 기본값')} onmousedown={startSplit} ondblclick={() => ((app.orderShare = 0.32), saveWorkspaceSoon())}></div>
            <span class="ficon order"><Icon name="sortLines" size={14} /></span>
            <span class="kw" class:on={!!tab.tableRef.orderBy}>ORDER BY</span>
            <div class="fwrap">
              <FilterInput bind:value={orderDraft} columns={filterCols} keywords={ORDER_KW} onsubmit={applyFilter} onrevert={revertFilter} />
              {#if tab.tableRef.orderBy}<button class="x-btn fclear" title={t('정렬 지우기')} onclick={() => clearFilter('order')}><Icon name="close" size={14} /></button>{/if}
            </div>
          </div>
        </div>
      {/if}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="rbody" onkeydown={resultsKey}>
        {#if isTable && tableView === 'data' && busy}
          <!-- over the old rows (reload) or the empty tab (first load) -->
          <div class="loading" class:blank={!current}>
            <div class="loading-pill"><span class="spin"></span> {t('데이터 불러오는 중…')} {elapsed}</div>
          </div>
        {/if}
        {#if tab.creating && tab.tableRef}
          <div class="structure">
            <StructureView
              create
              connId={tab.connId}
              schema={tab.tableRef.schema}
              table={tab.tableRef.table}
              driver={conn?.driver}
              onlog={(sql, outcome, err) => logStatements(tab, sql, outcome, err)}
              oncreated={(made) => ((tableView = 'ddl'), tableCreated(tab, made))}
            />
          </div>
        {:else if isTable && tableView === 'ddl' && !connected}
          <!-- a tab restored from the last session: as for the data -->
          <div class="reload-blank">
            <button class="btn primary" disabled={st?.status === 'connecting'} onclick={() => connect(tab.connId)}>
              <Icon name="plug" size={14} />
              {st?.status === 'connecting' ? t('연결 중…') : t('연결하고 불러오기')}
            </button>
            {#if conn}<span class="dim">{conn.name}</span>{/if}
          </div>
        {:else if isTable && tableView === 'ddl' && app.ddlMode === 'table' && tab.tableRef}
          <div class="structure">
            <StructureView
              connId={tab.connId}
              schema={tab.tableRef.schema}
              table={tab.tableRef.table}
              driver={conn?.driver}
              version={ddlVersion}
              onchanged={structureChanged}
              onlog={(sql, outcome, err) => logStatements(tab, sql, outcome, err)}
              onkey={showKey}
            />
            <IndexView
              highlight={keyHl?.kind === 'index' ? keyHl : null}
              connId={tab.connId}
              schema={tab.tableRef.schema}
              table={tab.tableRef.table}
              driver={conn?.driver}
              onchanged={structureChanged}
              onlog={(sql, outcome, err) => logStatements(tab, sql, outcome, err)}
            />
            {#if conn?.driver !== 'sqlite'}
              <FkView
                highlight={keyHl?.kind === 'fk' ? keyHl : null}
                connId={tab.connId}
                schema={tab.tableRef.schema}
                table={tab.tableRef.table}
                driver={conn?.driver}
                version={ddlVersion}
                onchanged={structureChanged}
                onlog={(sql, outcome, err) => logStatements(tab, sql, outcome, err)}
              />
            {/if}
          </div>
        {:else if isTable && tableView === 'ddl'}
          {#if ddlError}
            <div class="msg error"><pre>{ddlError}</pre></div>
          {:else if ddl === null}
            <div class="placeholder"><span class="spin"></span> {t('DDL 불러오는 중…')}</div>
          {:else}
            <SqlEditor value={ddl} driver={conn?.driver} readonly onchange={() => {}} onrun={() => {}} onrunall={() => {}} />
          {/if}
        {:else if isTable && tableView === 'log'}
          {@render logList(false)}
        {:else if current}
          <ResultGrid
            bind:this={grid}
            result={current}
            ensureEdit={() => loadEditInfo(tab, current!)}
            serverSort={isTable ? serverSort : undefined}
            onsort={isTable ? serverSortBy : undefined}
            columns={current.columns!}
            rows={current.rows!}
            hasMore={current.hasMore}
            loadingMore={tab.loadingMore}
            onloadmore={loadMore}
            canLoadPrev={!!current.paged && start > 0}
            loadingPrev={!!tab.loadingPrev}
            onloadprev={loadPrev}
            rowBase={start}
            layoutKey={tab.tableRef ? `${tab.connId}|${tab.tableRef.schema}|${tab.tableRef.table.schema}.${tab.tableRef.table.name}` : ''}
            onfollow={(c, v, num) => current && followForeignKey(tab, current, c, v, num)}
            onapply={openApply}
          />
        {:else if isTable && tableView === 'data' && !busy}
          <div class="reload-blank">
            <button class="btn primary" onclick={() => reloadTable(tab)}>
              <Icon name={connected ? 'refresh' : 'plug'} size={14} />
              {connected ? t('데이터 불러오기') : t('연결하고 불러오기')}
            </button>
            {#if !connected && conn}<span class="dim">{conn.name}</span>{/if}
          </div>
        {:else}
          {@render logList(!isTable)}
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  .tab {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
  }
  .toolbar :global(.dd) {
    max-width: 200px;
  }
  .run {
    color: var(--ok);
  }
  /* like .icon-btn, with text */
  .txbtn {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    height: 26px;
    padding: 0 6px;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-dim);
    font-size: 12px;
    cursor: pointer;
  }
  .txbtn:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--text);
  }
  .txbtn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .txbtn.on {
    color: var(--warn);
  }
  .txbtn.commit:not(:disabled) {
    color: var(--ok);
  }
  .txwait {
    color: var(--warn);
    font-size: 12px;
  }
  .stop {
    color: var(--danger);
  }
  .grp {
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .grp + .grp {
    margin-left: 6px;
    padding-left: 6px;
    border-left: 1px solid var(--border);
  }
  .sep {
    width: 1px;
    height: 18px;
    margin: 0 4px;
    background: var(--border);
  }
  .grow {
    flex: 1;
  }
  .running {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: 10px;
    color: var(--text-dim);
  }
  .body {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .editor {
    min-height: 0;
  }
  .editor.collapsed,
  .divider.collapsed {
    display: none;
  }
  .divider {
    height: 5px;
    flex-shrink: 0;
    cursor: row-resize;
    border-top: 1px solid var(--border);
    background: var(--bg-panel);
  }
  .divider:hover {
    background: var(--accent);
  }
  .results {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .rtabs {
    font-size: 13px;
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0 6px;
    height: 30px;
    flex-shrink: 0;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    /* scrolls sideways when narrow (a phone), without a bar that would
       take height from the 30px row */
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
  }
  .rtabs::-webkit-scrollbar {
    display: none;
  }
  .rtab {
    display: flex;
    align-items: center;
    gap: 5px;
    height: 100%;
    padding: 0 10px;
    border: none;
    border-bottom: 2px solid transparent;
    background: none;
    color: var(--text-dim);
    cursor: pointer;
    white-space: nowrap;
  }
  .rtab.on {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--danger);
  }
  /* columns and indexes, one scroll */
  .structure {
    height: 100%;
    overflow: auto;
    padding-bottom: 16px;
  }
  .filterbar {
    display: flex;
    align-items: stretch;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    background: var(--bg);
  }
  .fpart {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 3;
    min-width: 0;
    padding: 2px 4px 2px 10px;
  }
  .fpart.order {
    position: relative;
    border-left: 1px solid var(--border);
  }
  /* drag handle on the WHERE | ORDER BY border */
  .fsplit {
    position: absolute;
    left: -4px;
    top: 0;
    bottom: 0;
    width: 7px;
    cursor: col-resize;
    z-index: 2;
  }
  .fsplit:hover,
  .fsplit.on {
    background: linear-gradient(to right, transparent 3px, var(--accent) 3px, var(--accent) 4px, transparent 4px);
  }
  .ficon {
    display: inline-flex;
    flex-shrink: 0;
  }
  .ficon.where {
    color: var(--accent);
  }
  .ficon.order {
    color: var(--ok);
  }
  .kw {
    color: var(--text-faint);
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
    /* the input's own left padding is one space wide: no gap on top */
    margin-right: -6px;
  }
  .kw.on {
    color: var(--syn-keyword);
  }
  .fwrap {
    position: relative;
    flex: 1;
    min-width: 0;
  }
  .fclear {
    position: absolute;
    right: 4px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 1;
  }
  .dropdown {
    position: relative;
  }
  .pager {
    display: flex;
    align-items: center;
    gap: 1px;
  }
  .rtabs .icon-btn {
    width: 26px;
    height: 26px;
  }
  .rtabs .icon-btn.sm,
  .icon-btn.sm {
    width: 22px;
    height: 22px;
  }
  .range {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    height: 22px;
    padding: 0 6px;
    border: none;
    border-radius: 4px;
    background: none;
    font-size: 12px;
    color: var(--text);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    cursor: pointer;
  }
  .range:hover,
  .range.open {
    background: var(--bg-hover);
  }
  .size-menu {
    min-width: 150px;
  }
  .size-menu button.on {
    color: var(--accent);
    font-weight: 600;
  }
  .total {
    padding: 0 3px;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-dim);
    font: inherit;
    cursor: pointer;
  }
  .total:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--text);
  }
  .total.unknown {
    color: var(--accent);
    font-weight: 600;
  }

  .seg {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: 4px;
    overflow: hidden;
  }
  .seg button {
    height: 22px;
    padding: 0 10px;
    border: none;
    background: none;
    color: var(--text-dim);
    font-size: 12px;
    cursor: pointer;
  }
  .seg button + button {
    border-left: 1px solid var(--border);
  }
  .seg button.on {
    background: var(--bg-select);
    color: var(--text);
  }
  .icon-btn.split {
    width: auto;
    gap: 1px;
    padding: 0 3px 0 5px;
  }
  .icon-btn.split.active {
    background: var(--bg-hover);
    color: var(--text);
  }
  .dropdown .menu {
    position: fixed;
    z-index: 60;
    display: flex;
    flex-direction: column;
    min-width: 170px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .dropdown .menu button {
    display: flex;
    justify-content: space-between;
    padding: 5px 10px;
    border: none;
    border-radius: 4px;
    background: none;
    text-align: left;
    cursor: pointer;
  }
  .dropdown .menu button:hover {
    background: var(--bg-select);
  }
  .dropdown .hint {
    padding: 5px 10px 3px;
    border-top: 1px solid var(--border);
    margin-top: 3px;
    color: var(--text-faint);
    font-size: 11px;
  }
  .running.small {
    margin: 0 0 0 8px;
    font-size: 12px;
  }
  .pending {
    color: var(--warn);
    font-weight: 600;
    margin-right: 2px;
    white-space: nowrap;
  }
  .ro {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--text-faint);
    font-size: 12px;
    margin-right: 4px;
    white-space: nowrap;
    cursor: help;
  }
  .meta {
    color: var(--text-dim);
    margin: 0 4px 0 8px;
    white-space: nowrap;
  }
  .mini {
    height: 22px;
    padding: 0 6px;
    font-size: 11px;
  }
  .rbody {
    position: relative;
    flex: 1;
    min-height: 0;
  }
  .loading {
    position: absolute;
    inset: 0;
    z-index: 5;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 60px;
    background: color-mix(in srgb, var(--bg) 45%, transparent);
    pointer-events: none;
  }
  .loading.blank {
    background: var(--bg);
  }
  .loading-pill {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 14px;
    border: 1px solid var(--border);
    border-radius: 16px;
    background: var(--bg-elev);
    color: var(--text-dim);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  }
  .messages {
    height: 100%;
    overflow: auto;
    padding: 6px 0;
    user-select: text;
    -webkit-user-select: text;
  }
  .messages .placeholder {
    font-size: 13px;
  }
  .msg {
    display: flex;
    gap: 12px;
    padding: 4px 12px;
    border-bottom: 1px solid var(--grid-line);
  }
  .msg .time {
    color: var(--text-faint);
    font-family: var(--font-mono);
    font-size: 11.5px;
    padding-top: 1px;
  }
  .mbody {
    min-width: 0;
  }
  /* queries the app made (a table tab's SELECT, pages read on): dimmed, so
     what was typed stands out */
  .msg.auto:not(.error) .mbody {
    opacity: 0.5;
  }
  .msg .use {
    margin-left: auto;
    flex-shrink: 0;
    opacity: 0.45;
  }
  .msg:hover .use,
  .msg .use:focus-visible {
    opacity: 1;
  }
  .msg .from {
    color: var(--text-faint);
  }
  .logq {
    width: 160px;
    height: 22px;
    padding: 0 6px;
    font-size: 12px;
  }
  .msg pre {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .msg.error pre {
    color: var(--danger);
  }
  .reload-blank {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    height: 100%;
  }
  .reload-blank .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .placeholder {
    padding: 30px;
    color: var(--text-faint);
    text-align: center;
    line-height: 1.4;
  }
  .spin {
    width: 10px;
    height: 10px;
    border: 2px solid var(--text-faint);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
