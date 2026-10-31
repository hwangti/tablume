<script lang="ts">
  import Icon from './Icon.svelte'
  import {
    app,
    connect,
    disconnect,
    refreshSchemas,
    toggleSchema,
    toggleTable,
    loadTables,
    openConsole,
    openTableData,
    activeTab,
    pinTab,
    type SchemaNode,
    type TableNode,
    type Tab,
  } from '../lib/state.svelte'
  import { api } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { driverLabel, type ConnConfig } from '../lib/types'

  let filter = $state('')

  // A double-click also fires two clicks. Share the in-flight open between
  // them, and pin only once it has finished, so no duplicate tab appears.
  let opening: { key: string; done: Promise<Tab> } | null = null

  function openTable(connId: string, schema: string, t: TableNode) {
    const key = `${connId}|${schema}|${t.t.schema}.${t.t.name}`
    if (opening?.key === key) return opening.done
    const cur = { key, done: openTableData(connId, schema, t.t) }
    opening = cur
    cur.done.finally(() => {
      if (opening === cur) opening = null
    })
    return cur.done
  }

  async function pinTable(connId: string, schema: string, t: TableNode) {
    pinTab(await openTable(connId, schema, t))
  }
  let menu = $state<null | { x: number; y: number; items: { label: string; run: () => void; danger?: boolean }[] }>(null)

  const blank = (): ConnConfig => ({
    id: '',
    name: '',
    driver: 'mysql',
    host: 'localhost',
    port: 3306,
    user: '',
    password: '',
    savePassword: true,
    database: '',
    oracleSid: false,
    params: '',
    color: '',
  })

  function matches(name: string) {
    return !filter || name.toLowerCase().includes(filter.toLowerCase())
  }

  function visibleSchemas(id: string): SchemaNode[] {
    const st = app.conn[id]
    if (!st) return []
    const c = app.connections.find((x) => x.id === id)
    const sys = new Set(['information_schema', 'pg_catalog', 'mysql', 'performance_schema', 'sys', 'tempdb', 'model', 'msdb'])
    if (st.showAllSchemas || c?.driver === 'sqlite') return st.schemas
    return st.schemas.filter((s) => !sys.has(s.name) || s.name === st.current || s.expanded)
  }

  function hiddenCount(id: string) {
    return (app.conn[id]?.schemas.length ?? 0) - visibleSchemas(id).length
  }

  async function toggleConn(c: ConnConfig) {
    const st = app.conn[c.id]
    if (st.status !== 'connected') await connect(c.id)
    else st.expanded = !st.expanded
  }

  function connMenu(e: MouseEvent, c: ConnConfig) {
    e.preventDefault()
    const st = app.conn[c.id]
    const on = st?.status === 'connected'
    menu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        { label: '새 콘솔', run: () => openConsole(c.id) },
        on ? { label: '새로고침', run: () => refreshSchemas(c.id) } : { label: '연결', run: () => connect(c.id) },
        ...(on ? [{ label: '연결 해제', run: () => disconnect(c.id) }] : []),
        { label: '편집…', run: () => (app.editing = { ...c }) },
        { label: '복제', run: () => (app.editing = { ...c, id: '', name: c.name + ' 복사본' }) },
      ],
    }
  }

  async function insertText(text: string) {
    const tab = activeTab()
    if (tab) {
      tab.sql = tab.sql ? tab.sql.replace(/\s*$/, '\n\n') + text : text
    }
  }

  function tableMenu(e: MouseEvent, c: ConnConfig, s: SchemaNode, t: TableNode) {
    e.preventDefault()
    menu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        { label: '새 탭에서 데이터 보기', run: () => pinTable(c.id, s.name, t) },
        {
          label: 'SELECT 문을 콘솔에 넣기',
          run: async () => {
            const sql = await api.SelectSQL(c.id, s.name, t.t, 100)
            if (activeTab()?.connId === c.id) insertText(sql + ';')
            else openConsole(c.id, { schema: s.name, sql: sql + ';' })
          },
        },
        { label: '이름 복사', run: () => copyText(t.t.name) },
        { label: '전체 이름 복사', run: async () => copyText(await api.QualifiedName(c.id, s.name, t.t)) },
      ],
    }
  }

  function schemaMenu(e: MouseEvent, c: ConnConfig, s: SchemaNode) {
    e.preventDefault()
    menu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        { label: '이 스키마로 새 콘솔', run: () => openConsole(c.id, { schema: s.name }) },
        { label: '새로고침', run: () => loadTables(c.id, s) },
        { label: '이름 복사', run: () => copyText(s.name) },
      ],
    }
  }

  function tableLabel(t: TableNode, driver: string) {
    return driver === 'mssql' && t.t.schema && t.t.schema !== 'dbo' ? `${t.t.schema}.${t.t.name}` : t.t.name
  }
</script>

<svelte:window onclick={() => (menu = null)} />

<aside>
  <div class="top">
    <span class="title">연결</span>
    <button class="icon-btn" title="새 연결" onclick={() => (app.editing = blank())}><Icon name="plus" /></button>
  </div>
  <div class="filter">
    <Icon name="filter" size={14} />
    <input bind:value={filter} placeholder="테이블 필터" spellcheck="false" />
    {#if filter}<button class="icon-btn small" onclick={() => (filter = '')}><Icon name="close" size={12} /></button>{/if}
  </div>

  <div class="tree">
    {#if app.connections.length === 0}
      <div class="hint">
        <p>저장된 연결이 없습니다.</p>
        <button class="btn primary" onclick={() => (app.editing = blank())}><Icon name="plus" size={14} /> 새 연결</button>
      </div>
    {/if}

    {#each app.connections as c (c.id)}
      {@const st = app.conn[c.id]}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div
        class="node conn"
        style:--conn-color={c.color || 'transparent'}
        onclick={() => toggleConn(c)}
        ondblclick={() => st?.status === 'connected' && openConsole(c.id)}
        oncontextmenu={(e) => connMenu(e, c)}
        title="{driverLabel(c.driver)}{st?.version ? ' · ' + st.version : ''}"
      >
        <span class="chev" class:open={st?.expanded && st?.status === 'connected'}><Icon name="chevron" size={12} /></span>
        <span class="dbicon" class:on={st?.status === 'connected'} class:err={st?.status === 'error'}><Icon name="db" size={15} /></span>
        <span class="label">{c.name}</span>
        {#if st?.status === 'connecting'}<span class="spin"></span>{/if}
        <span class="drv">{driverLabel(c.driver)}</span>
        <button class="icon-btn small act" title="새 콘솔" onclick={(e) => (e.stopPropagation(), openConsole(c.id))}>
          <Icon name="console" size={14} />
        </button>
      </div>

      {#if st?.status === 'connected' && st.expanded}
        {#each visibleSchemas(c.id) as s (s.name)}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <div class="node" style:padding-left="22px" onclick={() => toggleSchema(c.id, s)} oncontextmenu={(e) => schemaMenu(e, c, s)}>
            <span class="chev" class:open={s.expanded}><Icon name="chevron" size={12} /></span>
            <Icon name="schema" size={15} color="var(--warn)" />
            <span class="label" class:current={s.name === st.current}>{s.name}</span>
            {#if s.loading}<span class="spin"></span>{/if}
            {#if s.tables}<span class="count">{s.tables.length}</span>{/if}
          </div>
          {#if s.expanded}
            {#if s.error}<div class="err-line" style:padding-left="44px">{s.error}</div>{/if}
            {#each (s.tables ?? []).filter((t) => matches(t.t.name)) as t (t.t.schema + '.' + t.t.name)}
              <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
              <div
                class="node"
                style:padding-left="38px"
                onclick={() => openTable(c.id, s.name, t)}
                ondblclick={() => pinTable(c.id, s.name, t)}
                oncontextmenu={(e) => tableMenu(e, c, s, t)}
                title="클릭: 데이터 보기 · 더블클릭: 탭 고정 · 화살표: 컬럼 보기"
              >
                <button
                  class="chev btn-chev"
                  class:open={t.expanded}
                  onclick={(e) => (e.stopPropagation(), toggleTable(c.id, s.name, t))}
                  ondblclick={(e) => e.stopPropagation()}
                  aria-label="컬럼 펼치기"
                >
                  <Icon name="chevron" size={12} />
                </button>
                <Icon name={t.t.type.includes('VIEW') ? 'view' : 'table'} size={15} color="var(--syn-fn)" />
                <span class="label">{tableLabel(t, c.driver)}</span>
                {#if t.loading}<span class="spin"></span>{/if}
              </div>
              {#if t.expanded && t.columns}
                {#each t.columns as col (col.name)}
                  <div class="node col" style:padding-left="70px" title="{col.name} {col.type}{col.nullable ? '' : ' NOT NULL'}">
                    <Icon name={col.pk ? 'key' : 'column'} size={13} color={col.pk ? 'var(--warn)' : 'var(--text-faint)'} />
                    <span class="label">{col.name}</span>
                    <span class="ctype">{col.type.toLowerCase()}{col.nullable ? '' : ' !'}</span>
                  </div>
                {/each}
              {/if}
              {#if t.expanded && t.error}<div class="err-line" style:padding-left="70px">{t.error}</div>{/if}
            {/each}
          {/if}
        {/each}
        {#if hiddenCount(c.id) > 0}
          <button class="more" onclick={() => (st.showAllSchemas = true)}>시스템 스키마 {hiddenCount(c.id)}개 더 보기</button>
        {/if}
      {/if}
      {#if st?.status === 'error' && st.error}
        <div class="err-line" style:padding-left="22px" title={st.error}>{st.error}</div>
      {/if}
    {/each}
  </div>
</aside>

{#if menu}
  <div class="menu" style:left="{menu.x}px" style:top="{menu.y}px">
    {#each menu.items as it (it.label)}
      <button class:danger={it.danger} onclick={it.run}>{it.label}</button>
    {/each}
  </div>
{/if}

<style>
  aside {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    background: var(--bg-panel);
  }
  .top {
    display: flex;
    align-items: center;
    padding: 8px 8px 4px 12px;
  }
  .title {
    flex: 1;
    font-weight: 600;
    color: var(--text-dim);
    font-size: 12px;
  }
  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 4px 8px 6px;
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text-faint);
  }
  .filter input {
    flex: 1;
    min-width: 0;
    border: none;
    padding: 4px 0;
    background: transparent;
  }
  .tree {
    flex: 1;
    overflow: auto;
    padding-bottom: 20px;
  }
  .node {
    display: flex;
    align-items: center;
    gap: 5px;
    height: 24px;
    padding: 0 8px 0 6px;
    white-space: nowrap;
    cursor: default;
  }
  .node:hover {
    background: var(--bg-hover);
  }
  .conn {
    height: 28px;
    box-shadow: inset 3px 0 0 var(--conn-color);
  }
  .chev {
    display: inline-flex;
    width: 12px;
    color: var(--text-faint);
    transition: transform 0.1s;
  }
  .chev.open {
    transform: rotate(90deg);
  }
  .btn-chev {
    justify-content: center;
    width: 16px;
    height: 18px;
    margin: 0 -2px;
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    cursor: pointer;
  }
  .btn-chev:hover {
    background: var(--bg-hover);
    color: var(--text);
  }
  .dbicon {
    display: inline-flex;
    color: var(--text-faint);
  }
  .dbicon.on {
    color: var(--ok);
  }
  .dbicon.err {
    color: var(--danger);
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .conn .label {
    font-weight: 500;
  }
  .label.current {
    font-weight: 600;
  }
  .drv,
  .count,
  .ctype {
    margin-left: auto;
    padding-left: 6px;
    color: var(--text-faint);
    font-size: 11px;
  }
  .ctype {
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .col {
    gap: 6px;
  }
  .act {
    display: none;
  }
  .conn:hover .act {
    display: inline-flex;
  }
  .conn:hover .drv {
    display: none;
  }
  .act {
    margin-left: auto;
  }
  .small {
    width: 20px;
    height: 20px;
  }
  .err-line {
    padding: 2px 8px;
    color: var(--danger);
    font-size: 11.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .more {
    display: block;
    margin: 2px 0 4px 44px;
    padding: 0;
    border: none;
    background: none;
    color: var(--text-dim);
    font-size: 11.5px;
    cursor: pointer;
  }
  .more:hover {
    color: var(--accent);
  }
  .hint {
    padding: 30px 16px;
    text-align: center;
    color: var(--text-dim);
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
  .menu {
    position: fixed;
    z-index: 100;
    display: flex;
    flex-direction: column;
    min-width: 180px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .menu button {
    padding: 5px 10px;
    border: none;
    border-radius: 4px;
    background: none;
    text-align: left;
    cursor: pointer;
  }
  .menu button:hover {
    background: var(--bg-select);
  }
  .menu button.danger {
    color: var(--danger);
  }
</style>
