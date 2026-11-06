<script lang="ts">
  import { formatBytes } from '../lib/format'
  import { isWeb } from '../lib/platform'
  import { i18n, LANGS, locale, num, t as tr } from '../lib/i18n.svelte'
  import { fitMenu, closeMenus, onCloseMenus } from '../lib/fit'
  import Icon from './Icon.svelte'
  import ZoomBox from './ZoomBox.svelte'
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
    openErd,
    ERD_DRIVERS,
    openCreateTable,
    openTableData,
    activeTab,
    pinTab,
    openDDL,
    renameTable,
    dropTable,
    truncateTable,
    createSchema,
    dropSchema,
    isSystemSchema,
    SCHEMA_DRIVERS,
    saveWorkspaceSoon,
    schemaShown,
    ensureStats,
    moveConnection,
    toast,
    setZoomLevel,
    ZOOM_MIN,
    ZOOM_MAX,
    ZOOM_PRESETS,
    setLanguage,
    type SchemaNode,
    type TableNode,
    type Tab,
  } from '../lib/state.svelte'
  import { api } from '../lib/api'
  import { setTheme, type ThemeMode } from '../lib/theme'
  import { copyText, isMac } from '../lib/clipboard'
  import { DRIVERS, driverLabel, type ConnConfig, type Column, type Routine, type Table } from '../lib/types'
  import SchemaPicker from './SchemaPicker.svelte'

  let filter = $state('')
  let viewMenu = $state(false)
  let version = $state('') // shown at the foot of the view menu
  api.Version().then((v) => (version = v), () => {})

  // 1.2만 / 1.2K
  const compact = $derived(new Intl.NumberFormat(locale(), { notation: 'compact', maximumFractionDigits: 1 }))

  function fmtRows(n: number | undefined): string {
    if (n === undefined || n < 0) return ''
    return n < 10000 ? num(n) : compact.format(n)
  }



  const fmtSize = formatBytes

  async function logout() {
    await fetch('/auth/logout', { method: 'POST' }).catch(() => {})
    location.replace('/login')
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
  type MenuItem = { label: string; run: () => void; danger?: boolean; icon?: string; sep?: boolean; disabled?: boolean; kbd?: string }
  let menu = $state<null | { x: number; y: number; items: MenuItem[] }>(null)
  let picker = $state<null | { connId: string; x: number; y: number }>(null)
  $effect(() => onCloseMenus(() => ((menu = null), (picker = null), (viewMenu = false))))

  const blank = (): ConnConfig => ({
    id: '',
    name: '',
    driver: app.drivers[0] ?? 'mysql',
    host: 'localhost',
    port: DRIVERS.find((d) => d.id === (app.drivers[0] ?? 'mysql'))?.port ?? 3306,
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

  // Schemas picked for the tree (see SchemaPicker).
  function visibleSchemas(id: string): SchemaNode[] {
    const st = app.conn[id]
    if (!st) return []
    return st.schemas.filter((s) => schemaShown(id, s.name))
  }

  // The filter searches the table lists already loaded; schemas never
  // opened aren't fetched for it (that can be slow on a big server).
  let filtering = $derived(filter.trim() !== '')

  function shownSchemas(id: string): SchemaNode[] {
    const list = visibleSchemas(id)
    if (!filtering) return list
    return list.filter((s) => (s.tables ?? []).some((t) => matches(t.t.name)))
  }

  function unloadedSchemas(id: string): number {
    return filtering ? visibleSchemas(id).filter((s) => s.tables === null).length : 0
  }

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
      if (connDrag?.active) continue // dragging a connection: connections only
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
              out.push({ kind: 'info', key: `rn|${c.id}|${s.name}`, depth: 3, text: tr('프로시저·함수가 없습니다'), cls: 'searching' })
          }
        }
      }
      if (filtering && shownSchemas(c.id).length === 0)
        out.push({ kind: 'info', key: 'f|' + c.id, depth: 1, text: tr('일치하는 테이블이 없습니다'), cls: 'searching' })
      if (filtering && unloadedSchemas(c.id) > 0)
        out.push({
          kind: 'info',
          key: 'fu|' + c.id,
          depth: 1,
          text: tr('펼친 적 없는 스키마 {n}개는 찾지 않습니다', { n: unloadedSchemas(c.id) }),
          cls: 'searching',
        })
      if (!filtering && visibleSchemas(c.id).length === 0 && st.schemas.length > 0)
        out.push({
          kind: 'info',
          key: 'h|' + c.id,
          depth: 1,
          text: tr('표시할 스키마를 고르세요'),
          cls: 'more',
          run: () => openPicker(c.id, null),
        })
    }
    return out
  })

  // rows split per connection (each starts with its connection row)
  const groups = $derived.by(() => {
    const out: Row[][] = []
    for (const r of rows) {
      if (r.kind === 'conn' || !out.length) out.push([r])
      else out[out.length - 1].push(r)
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

  // ---------------------------------------------------------------- reorder
  // Dragging a connection row shows only the connection rows; dropping puts
  // it before the row under the pointer.
  let connDrag = $state<null | { id: string; y: number; active: boolean; to: number }>(null)
  let dragJustEnded = false

  function connDown(e: PointerEvent, c: ConnConfig) {
    if (e.button !== 0 || (e.target as HTMLElement).closest('button')) return
    connDrag = { id: c.id, y: e.clientY, active: false, to: -1 }
  }

  function dragMove(e: PointerEvent) {
    if (!connDrag) return
    if (!connDrag.active) {
      if (Math.abs(e.clientY - connDrag.y) < 5) return
      connDrag.active = true
      tip = null
    }
    const els = [...treeEl.querySelectorAll('.node.conn')] as HTMLElement[]
    let to = els.length
    for (let i = 0; i < els.length; i++) {
      const r = els[i].getBoundingClientRect()
      if (e.clientY < r.top + r.height / 2) {
        to = i
        break
      }
    }
    connDrag.to = to
  }

  function dragEnd() {
    const d = connDrag
    connDrag = null
    if (!d?.active) return
    dragJustEnded = true
    setTimeout(() => (dragJustEnded = false))
    const from = app.connections.findIndex((c) => c.id === d.id)
    if (d.to < 0 || from < 0) return
    moveConnection(d.id, d.to > from ? d.to - 1 : d.to)
  }

  function rowClick(r: Row) {
    if (dragJustEnded) return
    cursor = r.key
    treeEl.focus({ preventScroll: true })
    // connections and folders open with the chevron or a double-click
    if (r.kind === 'table') openTable(r.c.id, r.s.name, r.t)
    else if (r.kind === 'routine') openRoutine(r.c.id, r.s.name, r.r)
    else if (r.kind === 'info') r.run?.()
  }

  // Table: click opens a preview tab (replaced by the next click),
  // double-click keeps it open. Connection / schema: double-click expands.
  function rowDblClick(r: Row) {
    if (dragJustEnded) return
    if (r.kind === 'table') pinTable(r.c.id, r.s.name, r.t)
    else if (r.kind === 'conn' || r.kind === 'schema' || r.kind === 'rgroup') toggle(r)
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
    if (!schemaShown(tab.connId, s.name)) {
      toast(tr('{name} 스키마는 트리에 표시하지 않도록 골라 두었습니다', { name: s.name }))
      return
    }
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
      case 'F2':
        if (r?.kind === 'table') renameTable(r.c.id, r.s.name, r.t.t)
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

  const routineLabel = (type: string) => ({ PROCEDURE: tr('프로시저'), FUNCTION: tr('함수'), PACKAGE: tr('패키지') })[type] ?? type

  function tipFor(r: Row): { title: string; lines: string[] } | null {
    if (r.kind === 'routine') {
      const lines = [routineLabel(r.r.type)]
      if (r.r.args) lines.push(`(${r.r.args})`)
      lines.push(tr('클릭: 소스 열기'))
      return { title: (r.r.schema && r.c.driver === 'mssql' ? r.r.schema + '.' : '') + r.r.name, lines }
    }
    if (r.kind === 'table') {
      const lines: string[] = []
      if (r.t.t.comment) lines.push(r.t.t.comment)
      const meta = [
        r.t.t.type !== 'TABLE' ? r.t.t.type : '',
        fmtRows(r.t.t.rows) && tr('행 {n} (추정)', { n: fmtRows(r.t.t.rows) }),
        fmtSize(r.t.t.size) && tr('크기 {n}', { n: fmtSize(r.t.t.size) }),
      ].filter(Boolean)
      if (meta.length) lines.push(meta.join(' · '))
      lines.push(tr('클릭: 미리보기 · 더블클릭: 탭 고정'))
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

  function rowEnter(e: PointerEvent, r: Row) {
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
      closeMenus()
      menu = {
        x: e.clientX,
        y: e.clientY,
        items: [
          { label: tr('소스 열기'), icon: 'code', run: () => openRoutine(rt.c.id, rt.s.name, rt.r) },
          { label: tr('이름 복사'), icon: 'copy', run: () => copyText(rt.r.name) },
        ],
      }
    }
  }

  function openPicker(connId: string, e: MouseEvent | null) {
    const el = treeEl.querySelector(`[data-key="${CSS.escape('c|' + connId)}"]`) as HTMLElement | null
    const r = el?.getBoundingClientRect()
    closeMenus()
    picker = { connId, x: e ? e.clientX : (r?.left ?? 0) + 40, y: (r?.bottom ?? e?.clientY ?? 0) + 2 }
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
    const i = app.connections.findIndex((x) => x.id === c.id)
    const last = app.connections.length - 1
    closeMenus()
    menu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        // open · the connection · what the tree shows · the saved connection · its place
        { label: tr('새 콘솔'), icon: 'console', run: () => openConsole(c.id) },
        ...(on && SCHEMA_DRIVERS.includes(c.driver) ? [{ label: tr('스키마 만들기…'), icon: 'plus', run: () => createSchema(c.id) }] : []),
        ...(on
          ? [
              { label: tr('새로고침'), icon: 'refresh', run: () => refreshSchemas(c.id), sep: true },
              { label: tr('연결 해제'), icon: 'unplug', run: () => disconnect(c.id) },
              ...(c.driver !== 'sqlite' ? [{ label: tr('표시할 스키마…'), icon: 'schemas', run: () => openPicker(c.id, e), sep: true }] : []),
            ]
          : [{ label: tr('연결'), icon: 'plug', run: () => connect(c.id), sep: true }]),
        { label: tr('편집…'), icon: 'edit', run: () => (app.editing = { ...c }), sep: true },
        { label: tr('복제'), icon: 'copy', run: () => (app.editing = { ...c, id: '', name: c.name + tr(' 복사본') }) },
        { label: tr('맨 위로'), icon: 'toTop', run: () => moveConnection(c.id, 0), sep: true, disabled: i === 0 },
        { label: tr('위로'), icon: 'arrowUp', run: () => moveConnection(c.id, i - 1), disabled: i === 0 },
        { label: tr('아래로'), icon: 'arrowDown', run: () => moveConnection(c.id, i + 1), disabled: i === last },
        { label: tr('맨 아래로'), icon: 'toBottom', run: () => moveConnection(c.id, last), disabled: i === last },
      ],
    }
  }

  async function insertText(text: string) {
    const tab = activeTab()
    if (tab) {
      tab.sql = tab.sql ? tab.sql.replace(/\s*$/, '\n\n') + text : text
    }
  }

  const isView = (t: Table) => t.type.toUpperCase().includes('VIEW')

  function tableMenu(e: MouseEvent, c: ConnConfig, s: SchemaNode, t: TableNode) {
    e.preventDefault()
    closeMenus()
    menu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        // open · SQL · copy · change (the ones that can't be undone last)
        { label: tr('데이터 보기'), icon: 'table', run: () => openTable(c.id, s.name, t), kbd: 'Enter' },
        { label: tr('새 탭에서 데이터 보기'), icon: 'plus', run: () => pinTable(c.id, s.name, t) },
        { label: tr('DDL 보기'), icon: 'ddl', run: () => openDDL(c.id, s.name, t.t) },
        {
          label: tr('SELECT 문을 콘솔에 넣기'),
          icon: 'console',
          sep: true,
          run: async () => {
            const sql = await api.SelectSQL(c.id, s.name, t.t, '', '', 100, 0)
            if (activeTab()?.connId === c.id) insertText(sql + ';')
            else openConsole(c.id, { schema: s.name, sql: sql + ';' })
          },
        },
        { label: tr('이름 복사'), icon: 'copy', run: () => copyText(t.t.name), sep: true },
        { label: tr('전체 이름 복사'), icon: 'copy', run: async () => copyText(await api.QualifiedName(c.id, s.name, t.t)) },
        { label: tr('이름 바꾸기'), icon: 'edit', run: () => renameTable(c.id, s.name, t.t), kbd: 'F2', sep: true },
        ...(isView(t.t) ? [] : [{ label: tr('테이블 비우기…'), icon: 'eraser', run: () => truncateTable(c.id, s.name, t.t), danger: true }]),
        { label: tr(isView(t.t) ? '뷰 삭제…' : '테이블 삭제…'), icon: 'trash', run: () => dropTable(c.id, s.name, t.t), danger: true },
      ],
    }
  }

  function schemaMenu(e: MouseEvent, c: ConnConfig, s: SchemaNode) {
    e.preventDefault()
    closeMenus()
    menu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        { label: tr('이 스키마로 새 콘솔'), icon: 'console', run: () => openConsole(c.id, { schema: s.name }) },
        ...(ERD_DRIVERS.includes(c.driver) ? [{ label: tr('ERD 보기'), icon: 'erd', run: () => openErd(c.id, s.name) }] : []),
        { label: tr('테이블 만들기'), icon: 'plus', run: () => openCreateTable(c.id, s.name), sep: true },
        ...(SCHEMA_DRIVERS.includes(c.driver) ? [{ label: tr('스키마 만들기…'), icon: 'plus', run: () => createSchema(c.id) }] : []),
        { label: tr('새로고침'), icon: 'refresh', run: () => loadTables(c.id, s), sep: true },
        ...(c.driver !== 'sqlite' ? [{ label: tr('표시할 스키마…'), icon: 'schemas', run: () => openPicker(c.id, e) }] : []),
        { label: tr('이름 복사'), icon: 'copy', run: () => copyText(s.name), sep: true },
        ...(SCHEMA_DRIVERS.includes(c.driver) && !isSystemSchema(c, s.name)
          ? [{ label: tr('스키마 삭제…'), icon: 'trash', run: () => dropSchema(c.id, s.name), sep: true, danger: true }]
          : []),
      ],
    }
  }

  function tableLabel(t: TableNode, driver: string) {
    return driver === 'mssql' && t.t.schema && t.t.schema !== 'dbo' ? `${t.t.schema}.${t.t.name}` : t.t.name
  }

  // Zoom from the view options: typed percent (applied on Enter / blur), ±5%
  // or a preset.
  function zoomStep(d: number) {
    setZoomLevel((Math.round(app.zoom * 100 / 5) * 5 + d) / 100)
  }
</script>

<svelte:window
  onclick={() => ((menu = null), (viewMenu = false))}
  onpointermove={dragMove}
  onpointerup={dragEnd}
  onkeydown={(e) => e.key === 'Escape' && connDrag && (connDrag = null)}
/>

{#snippet treeRow(r: Row)}
  {#if r.kind === 'info'}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="node info {r.cls}" style:padding-left="{8 + r.depth * 16 + 18}px" title={r.text} onclick={() => r.run?.()}>
      <span class="label">{r.text}</span>
    </div>
  {:else}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="node {r.kind}"
      class:cursor={cursor === r.key}
      class:active={r.key === activeKey}
      class:dragging={r.kind === 'conn' && connDrag?.active && connDrag.id === r.c.id}
      class:drop-before={r.kind === 'conn' && connDrag?.active && connDrag.to === app.connections.indexOf(r.c)}
      class:drop-after={r.kind === 'conn' &&
        connDrag?.active &&
        connDrag.to === app.connections.length &&
        app.connections.indexOf(r.c) === app.connections.length - 1}
      onpointerdown={(e) => r.kind === 'conn' && connDown(e, r.c)}
      data-key={r.key}
      style:padding-left="{8 + r.depth * 16}px"
      style:--conn-color={r.kind === 'conn' ? r.c.color || 'transparent' : null}
      onclick={() => rowClick(r)}
      ondblclick={() => rowDblClick(r)}
      oncontextmenu={(e) => rowMenu(e, r)}
      onpointerenter={(e) => e.pointerType === 'mouse' && rowEnter(e, r)}
      onpointerleave={rowLeave}
      title={r.kind === 'conn' ? `${driverLabel(r.c.driver)}${app.conn[r.c.id]?.version ? ' · ' + app.conn[r.c.id]?.version : ''}` : undefined}
    >
      {#if r.kind === 'col' || r.kind === 'routine'}
        <span class="chev-slot"></span>
      {:else}
        <button
          class="chev-slot chev"
          class:open={isOpen(r)}
          tabindex="-1"
          aria-label={tr('펼치기')}
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
        {#if st?.status === 'connected' && r.c.driver !== 'sqlite' && st.schemas.length}
          <button
            class="pick-btn"
            class:on={picker?.connId === r.c.id}
            title={tr('표시할 스키마 고르기')}
            onclick={(e) => (e.stopPropagation(), picker?.connId === r.c.id ? (picker = null) : openPicker(r.c.id, null))}
            ondblclick={(e) => e.stopPropagation()}
            >{visibleSchemas(r.c.id).length}/{st.schemas.length}</button
          >
        {/if}
        <span class="drv">{driverLabel(r.c.driver)}</span>
        <button class="icon-btn small act" title={tr('새 콘솔')} onclick={(e) => (e.stopPropagation(), openConsole(r.c.id))}>
          <Icon name="console" size={16} />
        </button>
      {:else if r.kind === 'schema'}
        <Icon name="schema" size={15} color="var(--warn)" />
        <span class="label" class:current={r.s.name === app.conn[r.c.id]?.current}>{r.s.name}</span>
        {#if r.s.loading || r.s.stats === 'loading'}<span class="spin" title={r.s.loading ? '' : tr('행 수 · 크기 가져오는 중')}></span>{/if}
        {#if r.s.tables && app.treeView.tableCount}<span class="count">{num(r.s.tables.length)}</span>{/if}
      {:else if r.kind === 'table'}
        <Icon name={r.t.t.type.includes('VIEW') ? 'view' : 'table'} size={15} color="var(--syn-fn)" />
        <span class="label">{tableLabel(r.t, r.c.driver)}</span>
        {#if app.treeView.comment && r.t.t.comment}<span class="cmt">{r.t.t.comment}</span>{/if}
        {#if r.t.loading}<span class="spin"></span>{/if}
        {#if tableMeta(r.t)}<span class="tmeta">{tableMeta(r.t)}</span>{/if}
      {:else if r.kind === 'rgroup'}
        <Icon name="fn" size={15} color="var(--syn-type)" />
        <span class="label">{tr('프로시저 · 함수')}</span>
        {#if r.s.routinesLoading}<span class="spin"></span>{/if}
        {#if r.s.routines && app.treeView.tableCount}<span class="count">{num(r.s.routines.length)}</span>{/if}
      {:else if r.kind === 'routine'}
        <span class="rbadge" class:f={r.r.type === 'FUNCTION'} class:k={r.r.type === 'PACKAGE'} title={routineLabel(r.r.type)}
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
{/snippet}

<aside>
  <div class="top">
    <div class="filter">
      <Icon name="filter" size={14} />
      <input bind:value={filter} placeholder={tr('테이블 필터')} spellcheck="false" />
      {#if filter}<button class="x-btn" title={tr('필터 지우기')} onclick={() => (filter = '')}><Icon name="close" size={14} /></button>{/if}
    </div>
    <button class="icon-btn" title={tr('열린 테이블을 트리에서 찾기')} onclick={locate} disabled={!activeKey}><Icon name="locate" /></button>
    <div class="viewopt">
      <button
        class="icon-btn"
        class:active={viewMenu}
        title={tr('보기 옵션')}
        onclick={(e) => (e.stopPropagation(), viewMenu || closeMenus(), (viewMenu = !viewMenu))}
      >
        <Icon name="sliders" />
      </button>
      {#if viewMenu}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="viewmenu" use:fitMenu={app.sidebarHidden} onclick={(e) => e.stopPropagation()}>
          <div class="vm-title">{tr('언어')}{i18n.lang === 'ko' ? ' · Language' : ''}</div>
          <div class="theme-seg" role="group">
            {#each LANGS as l (l.id)}
              <button class:on={i18n.lang === l.id} onclick={() => setLanguage(l.id)}>{l.label}</button>
            {/each}
          </div>
          <div class="vm-title sep">{tr('테마')}</div>
          <div class="theme-seg" role="group">
            {#each [['auto', tr('자동')], ['dark', tr('다크')], ['light', tr('라이트')]] as [m, label] (m)}
              <button
                class:on={app.theme === m}
                title={m === 'auto' ? tr('OS 설정을 따릅니다') : ''}
                onclick={() => {
                  app.theme = m as ThemeMode
                  setTheme(app.theme)
                  saveWorkspaceSoon()
                }}>{label}</button
              >
            {/each}
          </div>
          <!-- [−][150 %][+] [100% ▾]: typed value, ±5% or a preset — not a
               slider: the whole window (the slider too) rescales on every
               change, so a drag can't be followed. (The browser build
               leaves zoom to the browser.) -->
          {#if !isWeb}
          <div class="vm-title sep">{tr('화면 배율 (%)')}</div>
          <div class="zoom-row">
            <ZoomBox
              value={app.zoom}
              min={ZOOM_MIN}
              max={ZOOM_MAX}
              presets={ZOOM_PRESETS}
              onset={(v) => setZoomLevel(Math.round(v * 100) / 100)}
              onstep={(d) => zoomStep(d * 5)}
              stepTitles={[tr('5% 축소'), tr('5% 확대')]}
              title="{ZOOM_MIN * 100}–{ZOOM_MAX * 100}%, {tr('Enter로 적용')} · {isMac ? '⌘+ / ⌘- / ⌘0, ⌘ + ' + tr('휠') : 'Ctrl + = / - / 0, Ctrl + ' + tr('휠')}"
            />
          </div>
          {/if}
          <div class="vm-title sep">{tr('트리에 표시')}</div>
          <label><input type="checkbox" bind:checked={app.treeView.tableCount} onchange={saveWorkspaceSoon} /> {tr('스키마별 테이블 수')}</label>
          <label><input type="checkbox" bind:checked={app.treeView.comment} onchange={saveWorkspaceSoon} /> {tr('테이블 · 컬럼 코멘트')}</label>
          <label
            ><input type="checkbox" bind:checked={app.treeView.rows} onchange={() => (saveWorkspaceSoon(), ensureStats())} /> {tr('테이블 행 수')}</label
          >
          <label
            ><input type="checkbox" bind:checked={app.treeView.size} onchange={() => (saveWorkspaceSoon(), ensureStats())} /> {tr('테이블 크기')}</label
          >

          <div class="vm-title sep">{tr('결과 표')}</div>
          <label><input type="checkbox" bind:checked={app.gridColNumbers} onchange={saveWorkspaceSoon} /> {tr('컬럼 번호 표시')}</label>
          <div class="vm-title sep">{tr('테이블 열 때')}</div>
          <label><input type="checkbox" bind:checked={app.tableOpen.pkDesc} onchange={saveWorkspaceSoon} /> {tr('기본 정렬: PK 내림차순')}</label>
          {#if isWeb}
            <div class="vm-title sep">{tr('계정')}</div>
            <a class="vm-item" href="/passkeys"><Icon name="key" size={14} /> {tr('패스키 관리')}</a>
            <button class="vm-item" onclick={logout}><Icon name="logout" size={14} /> {tr('로그아웃')}</button>
          {/if}
          {#if version}<div class="vm-version">Tablume {version}</div>{/if}
        </div>
      {/if}
    </div>
    <button class="icon-btn" title={tr('새 연결')} onclick={() => (app.editing = blank())}><Icon name="plus" /></button>
  </div>
  <div class="tree-edge"></div>

  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="tree" bind:this={treeEl} tabindex="0" role="tree" onkeydown={treeKey} onscroll={() => (tip = null)}>
    {#if app.connections.length === 0}
      <div class="hint">
        <p>{tr('저장된 연결이 없습니다.')}</p>
        <button class="btn primary" onclick={() => (app.editing = blank())}><Icon name="plus" size={14} /> {tr('새 연결')}</button>
      </div>
    {/if}

    <!-- one box per connection: its row sticks only while its own rows
         scroll by, then the next connection pushes it up -->
    {#each groups as g (g[0].key)}
      <div class="cgroup">
        {#each g as r (r.key)}{@render treeRow(r)}{/each}
      </div>
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
  <div class="menu" style:left="{menu.x}px" style:top="{menu.y}px" use:fitMenu={menu}>
    {#each menu.items as it (it.label)}
      {#if it.sep}<hr />{/if}
      <button class:danger={it.danger} onclick={it.run} disabled={it.disabled}>
        <span class="mi">{#if it.icon}<Icon name={it.icon} size={14} />{/if}</span>{it.label}
        {#if it.kbd}<kbd>{it.kbd}</kbd>{/if}
      </button>
    {/each}
  </div>
{/if}

{#if picker}
  <SchemaPicker connId={picker.connId} x={picker.x} y={picker.y} onclose={() => (picker = null)} />
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
    /* as tall as the tab bar beside it */
    padding: 4px 8px;
  }
  .filter {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: 4px;
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
  /* When the tree's top edge falls between device pixels (zoom 125% on a
     1x display), WebKit rounds the scrolled rows and the stuck connection
     row apart and one pixel row of the rows under it shows. A cover inside
     the tree can't reach that row, so this band outside it lies over the
     edge, 1px on each side, above the sticky rows. */
  .tree-edge {
    position: relative;
    z-index: 3;
    flex-shrink: 0;
    height: 2px;
    margin: -1px 0;
    background: var(--bg-panel);
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
  .zoom-row {
    margin: 2px 8px 4px;
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
  .vm-version {
    margin-top: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 11px;
    user-select: text;
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
  /* Connection rows stay at the top while their own rows scroll under
     them (sticky inside .cgroup), and the next connection pushes them up. */
  .node.conn {
    position: sticky;
    top: 0;
    z-index: 2;
    height: 28px;
    box-shadow: inset 3px 0 0 var(--conn-color);
    background-color: var(--bg-panel);
  }
  .node.conn:hover {
    background: linear-gradient(var(--bg-hover), var(--bg-hover)) var(--bg-panel);
  }
  .node.conn.cursor {
    background: linear-gradient(var(--bg-hover), var(--bg-hover)) var(--bg-panel);
  }
  .tree:focus .node.conn.cursor {
    background: linear-gradient(var(--bg-select), var(--bg-select)) var(--bg-panel);
  }
  /* rows revealed by keyboard / locate aren't hidden under the sticky row */
  .node:not(.conn) {
    scroll-margin-top: 30px;
  }
  .node.conn.dragging {
    opacity: 0.5;
  }
  .node.conn.drop-before {
    box-shadow:
      inset 3px 0 0 var(--conn-color),
      inset 0 2px 0 var(--accent);
  }
  .node.conn.drop-after {
    box-shadow:
      inset 3px 0 0 var(--conn-color),
      inset 0 -2px 0 var(--accent);
  }
  .pick-btn {
    flex-shrink: 0;
    height: 18px;
    padding: 0 5px;
    border: 1px solid transparent;
    border-radius: 3px;
    background: none;
    color: var(--text-faint);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    cursor: pointer;
  }
  .pick-btn:hover,
  .pick-btn.on {
    border-color: var(--border);
    background: var(--bg-input);
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
    /* fixed: inputs (the zoom field) would otherwise set it */
    width: 240px;
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
  .vm-item {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 -6px;
    padding: 3px 6px;
    border: none;
    border-radius: 4px;
    background: none;
    color: var(--text);
    font: inherit;
    text-align: left;
    text-decoration: none;
    cursor: pointer;
  }
  .vm-item:hover {
    background: var(--bg-hover);
  }
  .vm-item :global(svg) {
    color: var(--text-dim);
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
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px 5px 8px;
    border: none;
    border-radius: 4px;
    background: none;
    text-align: left;
    cursor: pointer;
  }
  .menu button:hover:not(:disabled) {
    background: var(--bg-select);
  }
  .menu kbd {
    margin-left: auto;
    padding-left: 16px;
    color: var(--text-dim);
    font-family: var(--font-ui);
    font-size: 11px;
  }
  .menu button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .menu hr {
    margin: 4px 2px;
    border: none;
    border-top: 1px solid var(--border);
  }
  .mi {
    display: inline-flex;
    width: 14px;
    flex-shrink: 0;
    color: var(--text-dim);
  }
  .menu button.danger {
    color: var(--danger);
  }
</style>
