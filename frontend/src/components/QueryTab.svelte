<script lang="ts">
  import type { EditorView } from '@codemirror/view'
  import SqlEditor, { setExecMark } from './SqlEditor.svelte'
  import ResultGrid from './ResultGrid.svelte'
  import Icon from './Icon.svelte'
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
    type Tab,
  } from '../lib/state.svelte'
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
    if (!view) return
    const s = view.state.selection.main
    const doc = view.state.doc.toString()
    const res = s.empty ? await runSQL(tab, doc, s.head) : await runSQL(tab, doc.slice(s.from, s.to), -1, s.from)
    mark(res)
  }

  async function runAll() {
    if (!view) return
    const s = view.state.selection.main
    const doc = view.state.doc.toString()
    const res = s.empty ? await runSQL(tab, doc, -1) : await runSQL(tab, doc.slice(s.from, s.to), -1, s.from)
    mark(res)
  }

  function onSqlChange(v: string) {
    if (v !== tab.sql) tab.preview = false
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
    <div class="editor" style:height="{split}%">
      <SqlEditor value={tab.sql} driver={conn?.driver} schema={completion} onchange={onSqlChange} onrun={run} onrunall={runAll} bind:view />
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="divider" onmousedown={startDrag}></div>
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
        {#if current}
          <span class="meta">
            {current.rows!.length.toLocaleString()}{current.hasMore ? '+' : ''}행 · {current.elapsedMs}ms
          </span>
          <button class="icon-btn" title="CSV로 내보내기" onclick={() => exportResult('csv')}><Icon name="download" size={14} /></button>
          <button class="btn mini" onclick={() => exportResult('json')}>JSON</button>
        {/if}
      </div>
      <div class="rbody">
        {#if current}
          <ResultGrid
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
