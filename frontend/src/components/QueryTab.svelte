<script lang="ts">
  import type { EditorView } from '@codemirror/view'
  import SqlEditor, { setExecMark } from './SqlEditor.svelte'
  import ResultGrid from './ResultGrid.svelte'
  import Icon from './Icon.svelte'
  import Modal from './Modal.svelte'
  import { api, errText } from '../lib/api'
  import { modKey } from '../lib/clipboard'
  import {
    app,
    connById,
    runSQL,
    fetchMore,
    cancel,
    loadCompletion,
    saveWorkspaceSoon,
    toast,
    confirmBox,
    reloadTable,
    type Tab,
  } from '../lib/state.svelte'
  import { loadEditInfo, pendingCount, tabPending, revert, previewChanges, applyChanges } from '../lib/editing.svelte'
  import type { Cell, StmtResult } from '../lib/types'

  let { tab }: { tab: Tab } = $props()

  let view = $state<EditorView>()
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
  let current = $derived<StmtResult | undefined>(tab.activeResult >= 0 ? tab.results[tab.activeResult] : undefined)

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
  let applyView = $state<{ sql: string[]; busy: boolean; error: string } | null>(null)

  async function guardPending(): Promise<boolean> {
    const n = tabPending(tab)
    return n === 0 || confirmBox(`적용하지 않은 변경 ${n}건이 있습니다. 버리고 실행할까요?`)
  }

  async function openApply() {
    if (!current || !pending) return
    try {
      applyView = { sql: await previewChanges(tab, current), busy: false, error: '' }
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  async function doApply() {
    const view = applyView
    if (!current || !view) return
    view.busy = true
    try {
      const { count, inserted } = await applyChanges(tab, current)
      applyView = null
      toast(`${count}건 적용됨`)
      if (inserted) await rerun() // show generated values (auto ids, defaults)
    } catch (e) {
      view.busy = false
      view.error = errText(e)
    }
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

  function filterKey(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      applyFilter()
    } else if (e.key === 'Escape') {
      whereDraft = tab.tableRef?.where ?? ''
      orderDraft = tab.tableRef?.orderBy ?? ''
    }
  }

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

  $effect(() => {
    if (!tab.running) return
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
      const p = await api.SaveTextFile(name, tab.sql)
      if (p) toast('저장됨: ' + p)
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  function csvCell(v: Cell): string {
    if (v === null) return ''
    return /[",\n\r]/.test(v) ? '"' + v.replace(/"/g, '""') + '"' : v
  }

  async function exportResult(kind: 'csv' | 'json') {
    const r = current
    if (!r || !r.columns) return
    const cols = r.columns.map((c) => c.name)
    let text: string
    if (kind === 'csv') {
      text = '﻿' + [cols.map(csvCell).join(','), ...r.rows!.map((row) => row.map(csvCell).join(','))].join('\r\n')
    } else {
      text = JSON.stringify(
        r.rows!.map((row) => Object.fromEntries(cols.map((c, i) => [c, row[i]]))),
        null,
        2,
      )
    }
    try {
      const p = await api.SaveTextFile(`result.${kind}`, text)
      if (p) toast(`${r.rows!.length}행 내보냄${r.hasMore ? ' (불러온 행만)' : ''}: ${p}`)
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  function resultLabel(r: StmtResult, i: number) {
    const m = r.sql.match(/\bfrom\s+([^\s,;()]+)/i)
    return m ? m[1].replace(/["`[\]]/g, '') : `결과 ${i + 1}`
  }

  let elapsed = $derived.by(() => {
    void tick
    return tab.running ? ((Date.now() - tab.startedAt) / 1000).toFixed(1) + 's' : ''
  })
</script>

<div class="tab">
  <div class="toolbar">
    <button class="icon-btn run" title="실행 ({modKey}Enter) — 커서 위치 문장 또는 선택 영역" onclick={run} disabled={tab.running}>
      <Icon name="play" />
    </button>
    <button class="icon-btn run" title="전체 실행 ({modKey}Shift+Enter)" onclick={runAll} disabled={tab.running}>
      <Icon name="playAll" />
    </button>
    <button class="icon-btn stop" title="취소" onclick={() => cancel(tab)} disabled={!tab.running}>
      <Icon name="stop" />
    </button>
    <span class="sep"></span>
    <select value={tab.connId} onchange={(e) => changeConn(e.currentTarget.value)} title="연결">
      {#each app.connections as c (c.id)}
        <option value={c.id}>{c.name}</option>
      {/each}
    </select>
    <select
      bind:value={tab.schema}
      onchange={saveWorkspaceSoon}
      disabled={!connected || conn?.driver === 'sqlite'}
      title="기본 스키마 / 데이터베이스"
    >
      {#if !schemaOptions.length}<option value="">(스키마)</option>{/if}
      {#each schemaOptions as s (s)}
        <option value={s}>{s}</option>
      {/each}
    </select>
    <span class="sep"></span>
    <button class="icon-btn" title="SQL 파일 열기" onclick={openFile}><Icon name="open" /></button>
    <button class="icon-btn" title="SQL 파일로 저장" onclick={saveFile}><Icon name="save" /></button>
    <span class="grow"></span>
    {#if tab.running}
      <span class="running"><span class="spin"></span> 실행 중 {elapsed}</span>
    {/if}
    <label class="fetch" title="한 번에 가져올 행 수">
      가져오기
      <select bind:value={app.fetchSize} onchange={saveWorkspaceSoon}>
        {#each [100, 500, 1000, 5000, 10000] as n (n)}<option value={n}>{n}</option>{/each}
      </select>
    </label>
  </div>

  <div class="body" bind:this={body}>
    <div class="editor" class:collapsed={tab.layout === 'results'} style:height="{split}%">
      <SqlEditor value={tab.sql} driver={conn?.driver} schema={completion} onchange={onSqlChange} onrun={run} onrunall={runAll} bind:view />
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="divider"
      class:collapsed={tab.layout === 'results'}
      onmousedown={startDrag}
      ondblclick={toggleLayout}
      title="드래그: 크기 조절 · 더블클릭: 결과만 보기"
    ></div>
    <div class="results">
      <div class="rtabs">
        {#each tab.results as r, i (i)}
          <button class="rtab" class:on={tab.activeResult === i} onclick={() => (tab.activeResult = i)}>
            <Icon name="table" size={13} />
            {resultLabel(r, i)}
          </button>
        {/each}
        <button class="rtab" class:on={tab.activeResult === -1} onclick={() => (tab.activeResult = -1)}>
          메시지
          {#if tab.messages.some((m) => m.kind === 'error')}<span class="dot"></span>{/if}
        </button>
        <span class="grow"></span>
        <button
          class="icon-btn"
          title={tab.layout === 'results' ? 'SQL 에디터 보이기' : '결과만 크게 보기'}
          onclick={toggleLayout}
        >
          <Icon name={tab.layout === 'results' ? 'restore' : 'maximize'} size={14} />
        </button>
        {#if current}
          {#if pending > 0}
            <span class="pending">변경 {pending}건</span>
            <button class="btn mini primary" onclick={openApply} title="변경 적용 ({modKey}S)"><Icon name="check" size={13} /> 적용</button>
            <button class="btn mini" onclick={() => current && revert(current)} title="모든 변경 되돌리기"><Icon name="undo" size={13} /> 되돌리기</button>
            <span class="sep"></span>
          {/if}
          {#if editInfo?.editable}
            <button class="icon-btn" title="행 추가" onclick={() => grid?.newRow()}><Icon name="plus" size={14} /></button>
            <button class="icon-btn" title="선택 행 삭제 (Delete)" onclick={() => grid?.deleteRows()}><Icon name="minus" size={14} /></button>
          {:else if editInfo}
            <span class="ro" title={'읽기 전용: ' + editInfo.reason}><Icon name="lock" size={13} /> 읽기 전용</span>
          {/if}
          <button class="icon-btn" title="다시 실행" onclick={rerun} disabled={tab.running}><Icon name="refresh" size={14} /></button>
          <span class="meta">
            {current.rows!.length.toLocaleString()}{current.hasMore ? '+' : ''}행 · {current.elapsedMs}ms
          </span>
          <button class="icon-btn" title="CSV로 내보내기" onclick={() => exportResult('csv')}><Icon name="download" size={14} /></button>
          <button class="btn mini" onclick={() => exportResult('json')}>JSON</button>
        {/if}
      </div>
      {#if tab.tableRef}
        <div class="filterbar">
          <span class="kw">WHERE</span>
          <div class="fwrap">
            <input
              class="mono"
              bind:value={whereDraft}
              onkeydown={filterKey}
              placeholder="예: status = 'ACTIVE' AND created_at >= '2025-01-01'"
              spellcheck="false"
            />
            {#if tab.tableRef.where}<button class="fclear" title="WHERE 지우기" onclick={() => clearFilter('where')}><Icon name="close" size={11} /></button>{/if}
          </div>
          <span class="kw">ORDER BY</span>
          <div class="fwrap order">
            <input
              class="mono"
              bind:value={orderDraft}
              onkeydown={filterKey}
              placeholder="예: id DESC  (헤더 클릭으로도 정렬)"
              spellcheck="false"
            />
            {#if tab.tableRef.orderBy}<button class="fclear" title="정렬 지우기" onclick={() => clearFilter('order')}><Icon name="close" size={11} /></button>{/if}
          </div>
          <button class="btn mini" onclick={applyFilter} disabled={tab.running} title="Enter">조회</button>
        </div>
      {/if}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="rbody" onkeydown={resultsKey}>
        {#if current}
          <ResultGrid
            bind:this={grid}
            result={current}
            ensureEdit={() => loadEditInfo(tab, current!)}
            serverSort={tab.tableRef ? serverSort : undefined}
            onsort={tab.tableRef ? serverSortBy : undefined}
            columns={current.columns!}
            rows={current.rows!}
            hasMore={current.hasMore}
            loadingMore={tab.loadingMore}
            onloadmore={() => fetchMore(tab, current!)}
          />
        {:else}
          <div class="messages">
            {#if tab.messages.length === 0}
              <div class="placeholder">
                <p>{modKey}Enter — 커서가 있는 문장 실행 (선택했으면 선택 영역)</p>
                <p>{modKey}Shift+Enter — 전체 실행</p>
              </div>
            {/if}
            {#each tab.messages.slice().reverse() as m, i (tab.messages.length - i)}
              <div class="msg" class:error={m.kind === 'error'}>
                <span class="time">{m.time}</span>
                <pre>{m.text}</pre>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>
</div>

{#if applyView}
  <Modal title="변경 적용 — {applyView.sql.length}건" onclose={() => (applyView = null)} width={760} dismissible={false}>
    <p class="dim" style="margin:0 0 8px">아래 SQL을 하나의 트랜잭션으로 실행합니다. 하나라도 실패하면 모두 취소됩니다.</p>
    <pre class="apply-list mono">{applyView.sql.join(';\n') + ';'}</pre>
    {#if applyView.error}<div class="apply-error">{applyView.error}</div>{/if}
    {#snippet footer()}
      <span style="flex:1"></span>
      <button class="btn" onclick={() => (applyView = null)}>취소</button>
      <button class="btn primary" onclick={doApply} disabled={applyView?.busy}>{applyView?.busy ? '적용 중…' : '적용'}</button>
    {/snippet}
  </Modal>
{/if}

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
  .toolbar select {
    max-width: 200px;
    padding: 2px 6px;
    height: 26px;
  }
  .run {
    color: var(--ok);
  }
  .stop {
    color: var(--danger);
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
  .fetch {
    display: flex;
    align-items: center;
    gap: 6px;
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
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0 6px;
    height: 30px;
    flex-shrink: 0;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    overflow-y: hidden;
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
  .filterbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 8px;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
  }
  .kw {
    color: var(--syn-keyword);
    font-family: var(--font-mono);
    font-size: 11.5px;
    font-weight: 600;
    white-space: nowrap;
  }
  .fwrap {
    position: relative;
    flex: 3;
    min-width: 0;
  }
  .fwrap.order {
    flex: 1.4;
  }
  .fwrap input {
    width: 100%;
    height: 24px;
    padding: 2px 22px 2px 7px;
    font-size: 12px;
  }
  .fclear {
    position: absolute;
    right: 4px;
    top: 5px;
    display: inline-flex;
    padding: 2px;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-faint);
    cursor: pointer;
  }
  .fclear:hover {
    color: var(--text);
    background: var(--bg-hover);
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
  .apply-list {
    max-height: 360px;
    overflow: auto;
    margin: 0;
    padding: 8px 10px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    font-size: 12px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
    user-select: text;
    -webkit-user-select: text;
  }
  .apply-error {
    margin-top: 10px;
    color: var(--danger);
    white-space: pre-wrap;
    user-select: text;
    -webkit-user-select: text;
  }
  .meta {
    color: var(--text-dim);
    margin-right: 4px;
    white-space: nowrap;
  }
  .mini {
    height: 22px;
    padding: 0 6px;
    font-size: 11px;
  }
  .rbody {
    flex: 1;
    min-height: 0;
  }
  .messages {
    height: 100%;
    overflow: auto;
    padding: 6px 0;
    user-select: text;
    -webkit-user-select: text;
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
