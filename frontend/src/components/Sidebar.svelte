<script lang="ts">
  import Icon from './Icon.svelte'
  import {
    app,
    connect,
    disconnect,
    refreshSchemas,
    toggleSchema,
    toggleRoutines,
    openRoutine,
    toggleTable,
    loadTables,
    openConsole,
    openTableData,
    activeTab,
    pinTab,
    openDDL,
    saveWorkspaceSoon,
    type SchemaNode,
    type TableNode,
    type Tab,
  } from '../lib/state.svelte'
  import { api } from '../lib/api'
  import { setTheme, type ThemeMode } from '../lib/theme'
  import { copyText } from '../lib/clipboard'
  import { driverLabel, type ConnConfig, type Column, type Routine } from '../lib/types'

  let filter = $state('')
  let viewMenu = $state(false)

  const compact = new Intl.NumberFormat('ko-KR', { notation: 'compact', maximumFractionDigits: 1 })

  function fmtRows(n: number | undefined): string {
    if (n === undefined || n < 0) return ''
    return n < 10000 ? n.toLocaleString('ko-KR') : compact.format(n)
  }



  function fmtSize(b: number | undefined): string {
    if (b === undefined || b < 0) return ''
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let i = 0
    let v = b
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024
      i++
    }
    return (v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)) + ' ' + units[i]
  }

  function tableMeta(t: TableNode): string {
    const parts: string[] = []
    if (app.treeView.rows) {
      const r = fmtRows(t.t.rows)
      if (r) parts.push(r)
    }
    if (app.treeView.size) {
      const sz = fmtSize(t.t.size)
      if (sz) parts.push(sz)
    }
    return parts.join(' · ')
  }

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

  // While filtering, every schema's table list is loaded in the background
  // and only schemas with a matching table are shown, already expanded.
  let filtering = $derived(filter.trim() !== '')

  function shownSchemas(id: string): SchemaNode[] {
    const list = visibleSchemas(id)
    if (!filtering) return list
    return list.filter((s) => (s.tables ?? []).some((t) => matches(t.t.name)))
  }

  function pendingSchemas(id: string): number {
    return filtering ? visibleSchemas(id).filter((s) => s.tables === null || s.loading).length : 0
  }

  let loadingAll = false
  $effect(() => {
    if (!filtering || loadingAll) return
    const todo: [string, SchemaNode][] = []
    for (const c of app.connections) {
      if (app.conn[c.id]?.status !== 'connected') continue
      for (const s of visibleSchemas(c.id)) if (s.tables === null && !s.loading) todo.push([c.id, s])
    }
    if (!todo.length) return
    loadingAll = true
    ;(async () => {
      // A few at a time, so a server with many schemas isn't flooded.
      for (let i = 0; i < todo.length; i += 4) {
        await Promise.all(todo.slice(i, i + 4).map(([id, s]) => loadTables(id, s)))
      }
      loadingAll = false
    })()
  })

  function keyIcon(col: { pk: boolean; unique?: boolean; fk?: string }): { name: string; color: string } {
    if (col.pk) return { name: 'key', color: 'var(--key-pk)' }
    if (col.fk) return { name: 'link', color: 'var(--key-fk)' }
    if (col.unique) return { name: 'unique', color: 'var(--key-uk)' }
    return { name: 'column', color: 'var(--text-faint)' }
  }

  function columnTitle(col: { name: string; type: string; nullable: boolean; pk: boolean; unique?: boolean; fk?: string; comment?: string }) {
    const tags = [col.pk && 'PK', col.unique && 'UNIQUE', col.fk && `FK → ${col.fk}`].filter(Boolean).join(', ')
    return `${col.name} ${col.type}${col.nullable ? '' : ' NOT NULL'}${tags ? '\n' + tags : ''}${col.comment ? '\n' + col.comment : ''}`
  }

  // ---------------------------------------------------------------- flat rows
  // The tree renders as one list so every level shares the same layout and
  // the keyboard can walk it.
  type Row =
    | { kind: 'conn'; key: string; depth: 0; c: ConnConfig }
    | { kind: 'schema'; key: string; depth: 1; c: ConnConfig; s: SchemaNode }
    | { kind: 'table'; key: string; depth: 2; c: ConnConfig; s: SchemaNode; t: TableNode }
    | { kind: 'col'; key: string; depth: 3; c: ConnConfig; t: TableNode; col: Column }
    | { kind: 'rgroup'; key: string; depth: 2; c: ConnConfig; s: SchemaNode }
    | { kind: 'routine'; key: string; depth: 3; c: ConnConfig; s: SchemaNode; r: Routine }
    | { kind: 'info'; key: string; depth: number; text: string; cls: string; run?: () => void }

  const tableKey = (connId: string, schema: string, t: { schema: string; name: string }) =>
    `t|${connId}|${schema}|${t.schema}.${t.name}`

  let rows = $derived.by(() => {
    const out: Row[] = []
    for (const c of app.connections) {
      const st = app.conn[c.id]
      out.push({ kind: 'conn', key: 'c|' + c.id, depth: 0, c })
      if (st?.status === 'error' && st.error) out.push({ kind: 'info', key: 'ce|' + c.id, depth: 1, text: st.error, cls: 'err-line' })
      if (st?.status !== 'connected' || !st.expanded) continue
      for (const s of shownSchemas(c.id)) {
        out.push({ kind: 'schema', key: `s|${c.id}|${s.name}`, depth: 1, c, s })
        if (!(s.expanded || filtering)) continue
        if (s.error) out.push({ kind: 'info', key: `se|${c.id}|${s.name}`, depth: 2, text: s.error, cls: 'err-line' })
        for (const t of (s.tables ?? []).filter((t) => matches(t.t.name))) {
          const tk = tableKey(c.id, s.name, t.t)
          out.push({ kind: 'table', key: tk, depth: 2, c, s, t })
          if (!t.expanded) continue
          if (t.error) out.push({ kind: 'info', key: 'te|' + tk, depth: 3, text: t.error, cls: 'err-line' })
          for (const col of t.columns ?? []) out.push({ kind: 'col', key: `${tk}|${col.name}`, depth: 3, c, t, col })
        }
        // procedures & functions, below the tables (not while filtering)
        if (!filtering && c.driver !== 'sqlite' && s.tables) {
          out.push({ kind: 'rgroup', key: `rg|${c.id}|${s.name}`, depth: 2, c, s })
          if (s.routinesOpen) {
            for (const r of s.routines ?? [])
              out.push({ kind: 'routine', key: `r|${c.id}|${s.name}|${r.schema}.${r.name}(${r.args})|${r.type}`, depth: 3, c, s, r })
            if (s.routines && s.routines.length === 0)
              out.push({ kind: 'info', key: `rn|${c.id}|${s.name}`, depth: 3, text: '프로시저·함수가 없습니다', cls: 'searching' })
          }
        }
      }
      if (filtering && pendingSchemas(c.id) > 0)
        out.push({ kind: 'info', key: 'f|' + c.id, depth: 1, text: `테이블 찾는 중… (스키마 ${pendingSchemas(c.id)}개 남음)`, cls: 'searching' })
      else if (filtering && shownSchemas(c.id).length === 0)
        out.push({ kind: 'info', key: 'f|' + c.id, depth: 1, text: '일치하는 테이블이 없습니다', cls: 'searching' })
      const hidden = hiddenCount(c.id)
      if (hidden > 0 && !filtering)
        out.push({ kind: 'info', key: 'h|' + c.id, depth: 1, text: `시스템 스키마 ${hidden}개 더 보기`, cls: 'more', run: () => (st.showAllSchemas = true) })
    }
    return out
  })

  // Table of the active tab, highlighted in the tree.
  let activeKey = $derived.by(() => {
    const tab = activeTab()
    const ref = tab?.tableRef
    return ref ? tableKey(tab!.connId, ref.schema, ref.table) : ''
  })

  let cursor = $state('')
  let treeEl: HTMLDivElement

  function isOpen(r: Row): boolean {
    if (r.kind === 'conn') return app.conn[r.c.id]?.status === 'connected' && !!app.conn[r.c.id]?.expanded
    if (r.kind === 'schema') return r.s.expanded || filtering
    if (r.kind === 'table') return r.t.expanded
    if (r.kind === 'rgroup') return !!r.s.routinesOpen
    return false
  }

  async function toggle(r: Row) {
    if (r.kind === 'conn') await toggleConn(r.c)
    else if (r.kind === 'schema') await toggleSchema(r.c.id, r.s)
    else if (r.kind === 'table') await toggleTable(r.c.id, r.s.name, r.t)
    else if (r.kind === 'rgroup') await toggleRoutines(r.c.id, r.s)
  }

  function rowClick(r: Row) {
    cursor = r.key
    treeEl.focus({ preventScroll: true })
    if (r.kind === 'conn' || r.kind === 'schema' || r.kind === 'rgroup') toggle(r)
    else if (r.kind === 'table') openTable(r.c.id, r.s.name, r.t)
    else if (r.kind === 'routine') openRoutine(r.c.id, r.s.name, r.r)
    else if (r.kind === 'info') r.run?.()
  }

  // Click opens a preview tab (replaced by the next click); double-click
  // keeps it open.
  function rowDblClick(r: Row) {
    if (r.kind === 'table') pinTable(r.c.id, r.s.name, r.t)
    else if (r.kind === 'conn' && app.conn[r.c.id]?.status === 'connected') openConsole(r.c.id)
  }

  function moveCursor(key: string, block: ScrollLogicalPosition = 'nearest') {
    cursor = key
    requestAnimationFrame(() => treeEl.querySelector(`[data-key="${CSS.escape(key)}"]`)?.scrollIntoView({ block }))
  }

  // Reveals the active tab's table in the tree: expands its connection and
  // schema, clears a filter that hides it, and scrolls it into the middle.
  async function locate() {
    const tab = activeTab()
    const ref = tab?.tableRef
    if (!tab || !ref) return
    const st = app.conn[tab.connId]
    if (!st || st.status !== 'connected') return
    st.expanded = true
    const s = st.schemas.find((x) => x.name === ref.schema)
    if (!s) return
    if (!s.expanded) {
      s.expanded = true
      if (s.tables === null) await loadTables(tab.connId, s)
    }
    if (!matches(ref.table.name)) filter = ''
    moveCursor(tableKey(tab.connId, ref.schema, ref.table), 'center')
    treeEl.focus({ preventScroll: true })
  }

  function treeKey(e: KeyboardEvent) {
    const nav = rows.filter((r) => r.kind !== 'info')
    if (!nav.length) return
    let i = nav.findIndex((r) => r.key === cursor)
    const r = i >= 0 ? nav[i] : null
    switch (e.key) {
      case 'ArrowDown':
        moveCursor(nav[Math.min(nav.length - 1, i + 1)].key)
        break
      case 'ArrowUp':
        moveCursor(nav[Math.max(0, i - 1)].key)
        break
      case 'Home':
        moveCursor(nav[0].key)
        break
      case 'End':
        moveCursor(nav[nav.length - 1].key)
        break
      case 'ArrowRight':
        if (!r || r.kind === 'col' || r.kind === 'routine') break
        if (!isOpen(r)) toggle(r)
        else if (nav[i + 1] && nav[i + 1].depth > r.depth) moveCursor(nav[i + 1].key)
        break
      case 'ArrowLeft':
        if (!r) break
        if (r.kind !== 'col' && r.kind !== 'routine' && isOpen(r) && !(r.kind === 'schema' && filtering)) toggle(r)
        else {
          while (i > 0 && nav[i].depth >= r.depth) i--
          if (nav[i].depth < r.depth) moveCursor(nav[i].key)
        }
        break
      case 'Enter':
        if (!r) break
        if (r.kind === 'table') openTable(r.c.id, r.s.name, r.t)
        else if (r.kind === 'routine') openRoutine(r.c.id, r.s.name, r.r)
        else if (r.kind !== 'col') toggle(r)
        break
      default:
        return
    }
    e.preventDefault()
  }

  // ---------------------------------------------------------------- tooltip
  // A floating panel next to the tree: shows the full comment, unlike the
  // native title which truncates and appears late.
  let tip = $state<null | { top: number; left: number; title: string; lines: string[] }>(null)
  let tipTimer: ReturnType<typeof setTimeout> | undefined

  const routineLabel: Record<string, string> = { PROCEDURE: '프로시저', FUNCTION: '함수', PACKAGE: '패키지' }

  function tipFor(r: Row): { title: string; lines: string[] } | null {
    if (r.kind === 'routine') {
      const lines = [routineLabel[r.r.type] ?? r.r.type]
      if (r.r.args) lines.push(`(${r.r.args})`)
      lines.push('클릭: 소스 열기')
      return { title: (r.r.schema && r.c.driver === 'mssql' ? r.r.schema + '.' : '') + r.r.name, lines }
    }
    if (r.kind === 'table') {
      const lines: string[] = []
      if (r.t.t.comment) lines.push(r.t.t.comment)
      const meta = [
        r.t.t.type !== 'TABLE' ? r.t.t.type : '',
        fmtRows(r.t.t.rows) && `행 ${fmtRows(r.t.t.rows)} (추정)`,
        fmtSize(r.t.t.size) && `크기 ${fmtSize(r.t.t.size)}`,
      ].filter(Boolean)
      if (meta.length) lines.push(meta.join(' · '))
      lines.push('클릭: 미리보기 · 더블클릭: 탭 고정')
      return { title: r.t.t.name, lines }
    }
    if (r.kind === 'col') {
      const col = r.col
      const tags = [col.pk && 'PK', col.unique && 'UNIQUE', col.fk && `FK → ${col.fk}`].filter(Boolean).join(' · ')
      const lines = [`${col.type}${col.nullable ? '' : ' NOT NULL'}`]
      if (tags) lines.push(tags)
      if (col.comment) lines.push(col.comment)
      return { title: col.name, lines }
    }
    return null
  }

  function rowEnter(e: MouseEvent, r: Row) {
    clearTimeout(tipTimer)
    const content = tipFor(r)
    if (!content) {
      tip = null
      return
    }
    const el = e.currentTarget as HTMLElement
    tipTimer = setTimeout(() => {
      const rect = el.getBoundingClientRect()
      const side = treeEl.getBoundingClientRect()
      tip = { top: Math.min(rect.top, window.innerHeight - 120), left: side.right + 6, ...content }
    }, tip ? 80 : 450)
  }

  function rowLeave() {
    clearTimeout(tipTimer)
    tipTimer = setTimeout(() => (tip = null), 60)
  }

  function rowMenu(e: MouseEvent, r: Row) {
    cursor = r.key
    tip = null
    if (r.kind === 'conn') connMenu(e, r.c)
    else if (r.kind === 'schema') schemaMenu(e, r.c, r.s)
    else if (r.kind === 'table') tableMenu(e, r.c, r.s, r.t)
    else if (r.kind === 'routine') {
      e.preventDefault()
      const rt = r
      menu = {
        x: e.clientX,
        y: e.clientY,
        items: [
          { label: '소스 열기', run: () => openRoutine(rt.c.id, rt.s.name, rt.r) },
          { label: '이름 복사', run: () => copyText(rt.r.name) },
        ],
      }
    }
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
        { label: 'DDL 보기', run: () => openDDL(c.id, s.name, t.t) },
        { label: '데이터 보기', run: () => openTable(c.id, s.name, t) },
        { label: '새 탭에서 데이터 보기', run: () => pinTable(c.id, s.name, t) },
        {
          label: 'SELECT 문을 콘솔에 넣기',
          run: async () => {
            const sql = await api.SelectSQL(c.id, s.name, t.t, '', '', 100, 0)
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

<svelte:window onclick={() => ((menu = null), (viewMenu = false))} />

<aside>
  <div class="top">
    <span class="title">연결</span>
    <button class="icon-btn" title="열린 테이블을 트리에서 찾기" onclick={locate} disabled={!activeKey}><Icon name="locate" /></button>
    <div class="viewopt">
      <button
        class="icon-btn"
        class:active={viewMenu}
        title="보기 옵션"
        onclick={(e) => (e.stopPropagation(), (viewMenu = !viewMenu))}
      >
        <Icon name="sliders" />
      </button>
      {#if viewMenu}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="viewmenu" onclick={(e) => e.stopPropagation()}>
          <div class="vm-title">테마</div>
          <div class="theme-seg" role="group">
            {#each [['auto', '자동'], ['dark', '다크'], ['light', '라이트']] as [m, label] (m)}
              <button
                class:on={app.theme === m}
                title={m === 'auto' ? 'OS 설정을 따릅니다' : ''}
                onclick={() => {
                  app.theme = m as ThemeMode
                  setTheme(app.theme)
                  saveWorkspaceSoon()
                }}>{label}</button
              >
            {/each}
          </div>
          <div class="vm-title sep">트리에 표시</div>
          <label><input type="checkbox" bind:checked={app.treeView.tableCount} onchange={saveWorkspaceSoon} /> 스키마별 테이블 수</label>
          <label><input type="checkbox" bind:checked={app.treeView.comment} onchange={saveWorkspaceSoon} /> 테이블 · 컬럼 코멘트</label>
          <label><input type="checkbox" bind:checked={app.treeView.rows} onchange={saveWorkspaceSoon} /> 테이블 행 수 (통계 기준 추정)</label>
          <label><input type="checkbox" bind:checked={app.treeView.size} onchange={saveWorkspaceSoon} /> 테이블 크기</label>
          <div class="vm-title sep">결과 표</div>
          <label><input type="checkbox" bind:checked={app.gridColNumbers} onchange={saveWorkspaceSoon} /> 컬럼 번호 표시</label>
          <div class="vm-title sep">테이블 열 때</div>
          <label><input type="checkbox" bind:checked={app.tableOpen.pkDesc} onchange={saveWorkspaceSoon} /> 기본 정렬: PK 내림차순</label>
        </div>
      {/if}
    </div>
    <button class="icon-btn" title="새 연결" onclick={() => (app.editing = blank())}><Icon name="plus" /></button>
  </div>
  <div class="filter">
    <Icon name="filter" size={14} />
    <input bind:value={filter} placeholder="테이블 필터" spellcheck="false" />
    {#if filter}<button class="x-btn" title="필터 지우기" onclick={() => (filter = '')}><Icon name="close" size={14} /></button>{/if}
  </div>

  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="tree" bind:this={treeEl} tabindex="0" role="tree" onkeydown={treeKey} onscroll={() => (tip = null)}>
    {#if app.connections.length === 0}
      <div class="hint">
        <p>저장된 연결이 없습니다.</p>
        <button class="btn primary" onclick={() => (app.editing = blank())}><Icon name="plus" size={14} /> 새 연결</button>
      </div>
    {/if}

    {#each rows as r (r.key)}
      {#if r.kind === 'info'}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="node info {r.cls}" style:padding-left="{8 + r.depth * 16 + 18}px" title={r.text} onclick={() => r.run?.()}>
          {#if r.cls === 'searching' && r.text.includes('찾는 중')}<span class="spin"></span>{/if}
          <span class="label">{r.text}</span>
        </div>
      {:else}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="node {r.kind}"
          class:cursor={cursor === r.key}
          class:active={r.key === activeKey}
          data-key={r.key}
          style:padding-left="{8 + r.depth * 16}px"
          style:--conn-color={r.kind === 'conn' ? r.c.color || 'transparent' : null}
          onclick={() => rowClick(r)}
          ondblclick={() => rowDblClick(r)}
          oncontextmenu={(e) => rowMenu(e, r)}
          onmouseenter={(e) => rowEnter(e, r)}
          onmouseleave={rowLeave}
          title={r.kind === 'conn' ? `${driverLabel(r.c.driver)}${app.conn[r.c.id]?.version ? ' · ' + app.conn[r.c.id]?.version : ''}` : undefined}
        >
          {#if r.kind === 'col' || r.kind === 'routine'}
            <span class="chev-slot"></span>
          {:else}
            <button
              class="chev-slot chev"
              class:open={isOpen(r)}
              tabindex="-1"
              aria-label="펼치기"
              onclick={(e) => (e.stopPropagation(), (cursor = r.key), toggle(r))}
              ondblclick={(e) => e.stopPropagation()}
            >
              <Icon name="chevron" size={12} />
            </button>
          {/if}

          {#if r.kind === 'conn'}
            {@const st = app.conn[r.c.id]}
            <span class="dbicon" class:on={st?.status === 'connected'} class:err={st?.status === 'error'}><Icon name="db" size={15} /></span>
            <span class="label">{r.c.name}</span>
            {#if st?.status === 'connecting'}<span class="spin"></span>{/if}
            <span class="drv">{driverLabel(r.c.driver)}</span>
            <button class="icon-btn small act" title="새 콘솔" onclick={(e) => (e.stopPropagation(), openConsole(r.c.id))}>
              <Icon name="console" size={14} />
            </button>
          {:else if r.kind === 'schema'}
            <Icon name="schema" size={15} color="var(--warn)" />
            <span class="label" class:current={r.s.name === app.conn[r.c.id]?.current}>{r.s.name}</span>
            {#if r.s.loading}<span class="spin"></span>{/if}
            {#if r.s.tables && app.treeView.tableCount}<span class="count">{r.s.tables.length.toLocaleString('ko-KR')}</span>{/if}
          {:else if r.kind === 'table'}
            <Icon name={r.t.t.type.includes('VIEW') ? 'view' : 'table'} size={15} color="var(--syn-fn)" />
            <span class="label">{tableLabel(r.t, r.c.driver)}</span>
            {#if app.treeView.comment && r.t.t.comment}<span class="cmt">{r.t.t.comment}</span>{/if}
            {#if r.t.loading}<span class="spin"></span>{/if}
            {#if tableMeta(r.t)}<span class="tmeta">{tableMeta(r.t)}</span>{/if}
          {:else if r.kind === 'rgroup'}
            <Icon name="fn" size={15} color="var(--syn-type)" />
            <span class="label">프로시저 · 함수</span>
            {#if r.s.routinesLoading}<span class="spin"></span>{/if}
            {#if r.s.routines && app.treeView.tableCount}<span class="count">{r.s.routines.length.toLocaleString('ko-KR')}</span>{/if}
          {:else if r.kind === 'routine'}
            <span class="rbadge" class:f={r.r.type === 'FUNCTION'} class:k={r.r.type === 'PACKAGE'} title={routineLabel[r.r.type] ?? r.r.type}
              >{r.r.type === 'FUNCTION' ? 'F' : r.r.type === 'PACKAGE' ? 'K' : 'P'}</span
            >
            <span class="label">{r.c.driver === 'mssql' && r.r.schema !== 'dbo' ? r.r.schema + '.' : ''}{r.r.name}</span>
            {#if r.r.args}<span class="cmt">({r.r.args})</span>{/if}
          {:else if r.kind === 'col'}
            <Icon name={keyIcon(r.col).name} size={13} color={keyIcon(r.col).color} />
            <span class="label">{r.col.name}</span>
            {#if app.treeView.comment && r.col.comment}<span class="cmt">{r.col.comment}</span>{/if}
            <span class="ctype">{r.col.type.toLowerCase()}{r.col.nullable ? '' : ' !'}</span>
          {/if}
        </div>
      {/if}
    {/each}
  </div>
</aside>

{#if tip}
  <div class="tip" style:top="{tip.top}px" style:left="{tip.left}px">
    <div class="tip-title">{tip.title}</div>
    {#each tip.lines as l, i (i)}<div class="tip-line">{l}</div>{/each}
  </div>
{/if}

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
    outline: none;
  }
  .node.cursor {
    background: var(--bg-hover);
  }
  .tree:focus .node.cursor {
    background: var(--bg-select);
  }
  .node.active {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .node.table.active .label {
    color: var(--accent);
    font-weight: 600;
  }
  .chev-slot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 16px;
    height: 18px;
    margin-right: 1px;
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-faint);
  }
  button.chev-slot {
    cursor: pointer;
  }
  button.chev-slot:hover {
    background: var(--bg-hover);
    color: var(--text);
  }
  .chev-slot :global(svg) {
    transition: transform 0.1s;
  }
  .chev-slot.open :global(svg) {
    transform: rotate(90deg);
  }
  .info {
    cursor: default;
  }
  .theme-seg {
    display: flex;
    margin: 2px 8px 4px;
    border: 1px solid var(--border);
    border-radius: 4px;
    overflow: hidden;
  }
  .theme-seg button {
    flex: 1;
    height: 24px;
    border: none;
    background: none;
    color: var(--text-dim);
    cursor: pointer;
  }
  .theme-seg button + button {
    border-left: 1px solid var(--border);
  }
  .theme-seg button.on {
    background: var(--bg-select);
    color: var(--text);
  }
  .tip {
    position: fixed;
    z-index: 120;
    max-width: 380px;
    padding: 8px 10px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
    pointer-events: none;
    line-height: 1.45;
  }
  .tip-title {
    font-weight: 600;
    margin-bottom: 2px;
    word-break: break-all;
  }
  .tip-line {
    color: var(--text-dim);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .tip-line:last-child:not(:first-of-type) {
    color: var(--text-faint);
  }
  .vm-title.sep {
    margin-top: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }
  .node {
    display: flex;
    align-items: center;
    gap: 5px;
    height: 24px;
    padding: 0 8px 0 6px;
    white-space: nowrap;
    cursor: default;
    /* rows never get wider than the tree: text shrinks instead, so the
       row count / size stay lined up at the right edge */
    min-width: 0;
    overflow: hidden;
  }
  .node:hover {
    background: var(--bg-hover);
  }
  /* Icons keep their size; text truncates instead (comments first). */
  .node > :global(svg),
  .node > .chev,
  .node > .dbicon {
    flex-shrink: 0;
  }
  .conn {
    height: 28px;
    box-shadow: inset 3px 0 0 var(--conn-color);
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
    /* shrinks only after the comment (flex-shrink 1000) is gone */
    flex: 0 1 auto;
    min-width: 3em;
  }
  /* with a comment, keep a little of it visible */
  .label:has(+ .cmt) {
    max-width: 70%;
  }
  .cmt {
    flex: 0 1000 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text-faint);
    font-size: 11.5px;
  }
  .rbadge {
    flex-shrink: 0;
    width: 15px;
    height: 15px;
    border-radius: 3px;
    background: color-mix(in srgb, var(--syn-type) 18%, transparent);
    color: var(--syn-type);
    font-size: 10px;
    font-weight: 700;
    line-height: 15px;
    text-align: center;
  }
  .rbadge.f {
    background: color-mix(in srgb, var(--syn-fn) 18%, transparent);
    color: var(--syn-fn);
  }
  .rbadge.k {
    background: color-mix(in srgb, var(--warn) 20%, transparent);
    color: var(--warn);
  }
  .tmeta {
    margin-left: auto;
    padding-left: 8px;
    flex-shrink: 0;
    color: var(--text-faint);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .viewopt {
    position: relative;
  }
  .icon-btn.active {
    background: var(--bg-hover);
    color: var(--text);
  }
  .viewmenu {
    position: absolute;
    top: 30px;
    right: -30px;
    z-index: 100;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 220px;
    padding: 10px 12px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .vm-title {
    color: var(--text-dim);
    font-size: 11.5px;
    margin-bottom: 2px;
  }
  .viewmenu label {
    display: flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
    cursor: pointer;
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
    flex-shrink: 1;
    min-width: 40px;
    max-width: 40%;
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
  .node.err-line {
    color: var(--danger);
    font-size: 11.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .node.more {
    color: var(--text-dim);
    font-size: 11.5px;
    cursor: pointer;
  }
  .node.more:hover {
    color: var(--accent);
  }
  .node.searching {
    color: var(--text-faint);
    font-size: 11.5px;
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
