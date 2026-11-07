<script lang="ts">
  import { untrack } from 'svelte'
  import Icon from './Icon.svelte'
  import ZoomBox from './ZoomBox.svelte'
  import { t, num } from '../lib/i18n.svelte'
  import { api, errText } from '../lib/api'
  import { app, askText, connect, globMatch, openDDL, openTableData, saveWorkspaceSoon, type Tab } from '../lib/state.svelte'
  import { copyText } from '../lib/clipboard'
  import { closeMenus, fitMenu, onCloseMenus } from '../lib/fit'
  import { layout, boxView, stack, edgePath, routedPath, DEFAULT_OPTIONS, DEFAULT_AUDIT, K, type Layout, type ViewOptions, type BoxView, type Node, type Measure } from '../lib/erd'
  import type { ForeignKey, SchemaGraph, Table } from '../lib/types'

  let { tab }: { tab: Tab } = $props()

  // Marker ids are per tab: every open tab keeps its SVG in the page.
  let uid = $derived('erd' + tab.id)

  let viewEl: HTMLDivElement
  let L = $state.raw<Layout | null>(null)
  let graph: SchemaGraph | null = null // as read, for a new layout when the audit columns change
  let error = $state('')
  let loading = $state(false)
  let opt = $state<ViewOptions>({ ...DEFAULT_OPTIONS })
  let connected = $derived(app.conn[tab.connId]?.status === 'connected')

  // Text is measured on a canvas: measuring SVG text forces a layout each time.
  const ctx = document.createElement('canvas').getContext('2d')!
  let family = 'sans-serif'
  const widths = new Map<string, number>()
  const measure: Measure = (s, px, bold) => {
    const k = `${bold ? 'b' : ''}${px}|${s}`
    let w = widths.get(k)
    if (w === undefined) {
      ctx.font = `${bold ? '700 ' : ''}${px}px ${family}`
      widths.set(k, (w = ctx.measureText(s).width))
    }
    return w
  }
  // Key marks take the wider one's room, so every name starts at one place.
  let markW = $state(12)
  // Bold widens text; tracking takes back the average gain so a highlighted
  // name still fits where it was cut.
  let hiTrack = $state(-0.3)

  async function load() {
    loading = true
    error = ''
    try {
      const g = await api.SchemaGraph(tab.connId, tab.schema)
      family = getComputedStyle(viewEl).fontFamily || family
      widths.clear()
      markW = Math.max(measure('● ', 11), measure('◆ ', 11))
      const probe = 'abcdefghijklmnopqrstuvwxyz_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
      hiTrack = -(measure(probe, 11, true) - measure(probe, 11)) / probe.length
      hot = null
      hov = null
      fitted = false
      manual = {}
      viewCache.clear()
      graph = g
      L = layout(g, measure, auditOf)
    } catch (e) {
      error = errText(e)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    if (connected && !L && !error) untrack(load)
  })

  // Audit columns: name patterns, one list for every ERD (app.erdAudit).
  const auditOf = (col: string) => app.erdAudit.some((p) => globMatch(p, col))
  $effect(() => {
    JSON.stringify(app.erdAudit)
    untrack(() => {
      if (graph) (viewCache = new Map()), (L = layout(graph, measure, auditOf))
    })
  })
  async function editAudit() {
    menu = null
    const v = await askText(t('감사 컬럼 이름 (쉼표로 구분, * 와일드카드, 비우면 기본값)'), app.erdAudit.join(', '), t('저장'))
    if (v === null) return
    const ps = v.split(/[,\n]/).map((s) => s.trim()).filter(Boolean)
    app.erdAudit = ps.length ? ps : [...DEFAULT_AUDIT]
    saveWorkspaceSoon()
  }

  // Boxes moved or widened by hand: the offset from the layout and the
  // width. "Auto arrange" empties it.
  let manual = $state<Record<string, { dx: number; dy: number; w?: number }>>({})

  // A widened box is drawn again; the others keep their view until an option changes.
  let viewCache = new Map<string, { w: number; v: BoxView }>()
  let cacheOpt = ''
  let views = $derived.by(() => {
    const key = JSON.stringify(opt)
    if (key !== cacheOpt) (viewCache = new Map()), (cacheOpt = key)
    const m = new Map<string, BoxView>()
    if (L)
      for (const b of L.nodes.values()) {
        const w = manual[b.name]?.w ?? b.w
        let c = viewCache.get(b.name)
        if (!c || c.w !== w) viewCache.set(b.name, (c = { w, v: boxView({ ...b, w }, opt, measure, markW, false) }))
        m.set(b.name, c.v)
      }
    return m
  })
  // A column's row from the top of its box; a hidden one: the middle.
  const rowAt = (n: string, col: string) => {
    const v = views.get(n)!
    const r = v.rows.find((r) => r.line.name === col)
    return r ? r.y - 4 : v.h / 2
  }
  let pos = $derived(L ? stack(L, new Map([...views].map(([n, v]) => [n, v.h])), opt, rowAt) : null)
  let nodes = $derived(L ? [...L.nodes.values()] : [])

  // Where each shown box is now: the layout plus what was done by hand.
  let geo = $derived.by(() => {
    const m = new Map<string, { x: number; y: number; w: number; h: number }>()
    if (!L || !pos) return m
    for (const b of L.nodes.values()) {
      const y = pos.y.get(b.name)
      if (y === undefined) continue
      const o = manual[b.name]
      m.set(b.name, { x: b.x + (o?.dx ?? 0), y: y + (o?.dy ?? 0), w: o?.w ?? b.w, h: views.get(b.name)!.h })
    }
    return m
  })
  // The picture's extent, boxes moved out of it included.
  let bounds = $derived.by(() => {
    if (!pos) return null
    let [x0, y0, x1, y1] = [K.PAD, K.PAD, pos.w - K.PAD, pos.h - K.PAD]
    for (const g of geo.values()) {
      x0 = Math.min(x0, g.x)
      y0 = Math.min(y0, g.y)
      x1 = Math.max(x1, g.x + g.w)
      y1 = Math.max(y1, g.y + g.h)
    }
    return { x: x0 - K.PAD, y: y0 - K.PAD, w: x1 - x0 + 2 * K.PAD, h: y1 - y0 + 2 * K.PAD }
  })

  let edges = $derived.by(() => {
    if (!L) return []
    const at = (n: string) => {
      const g = geo.get(n)!
      const v = views.get(n)!
      return {
        ...g,
        anchor: (col: string) => {
          const r = v.rows.find((r) => r.line.name === col)
          return r ? g.y + r.y - 4 : g.y + v.h / 2
        },
      }
    }
    // Through the waypoints while neither end was moved by hand.
    const routed = (e: Layout['edges'][number]) =>
      e.via.length && pos && !manual[e.a] && !manual[e.b] ? routedPath(at(e.a), at(e.b), e.ac, e.bc, e.via.map((id) => pos!.via.get(id)!)) : null
    return L.edges.map((e, i) => ({ ...e, i, d: routed(e) ?? edgePath(at(e.a), at(e.b), e.ac, e.bc, e.a === e.b) }))
  })

  // ------------------------------------------------------------ highlight
  // On a box's header: all its relations; on a column row: that column's;
  // on a line: that one. The rest dims.
  let hot = $state.raw<{ tables: Set<string>; edges: Set<number>; cols: Set<string> } | null>(null)
  const ck = (tb: string, c: string) => tb + '\0' + c

  function litEdges(ids: number[], also?: string) {
    const tables = new Set<string>(also ? [also] : [])
    const cols = new Set<string>()
    for (const i of ids) {
      const e = L!.edges[i]
      tables.add(e.a).add(e.b)
      cols.add(ck(e.a, e.ac)).add(ck(e.b, e.bc))
    }
    hot = { tables, edges: new Set(ids), cols }
  }
  const edgeIds = (f: (e: Layout['edges'][number]) => boolean) => L!.edges.flatMap((e, i) => (f(e) ? [i] : []))

  // The box under the pointer is drawn again on top, uncut.
  let hov = $state<string | null>(null)
  let hovView = $derived(hov && L && geo.has(hov) ? boxView({ ...L.nodes.get(hov)!, w: geo.get(hov)!.w }, opt, measure, markW, true) : null)
  let vr: DOMRect | null = null
  let cur: number | null | undefined // -1 header, else the row's index

  function aim(n: string, e: PointerEvent) {
    const v = views.get(n)
    const g = geo.get(n)
    if (!v || !g || !vr) return
    const sy = (e.clientY - vr.top - py) / z - g.y
    let hit: number | null = null
    if (sy < v.headH) hit = -1
    else hit = v.rows.findIndex((r) => sy >= r.y - 12 && sy < r.y - 12 + r.h)
    if (hit === -1 && sy >= v.headH) hit = null
    if (hit === cur) return
    cur = hit
    if (hit === -1) litEdges(edgeIds((x) => x.a === n || x.b === n), n)
    else if (hit !== null) {
      const c = v.rows[hit].line.name
      const ids = edgeIds((x) => (x.a === n && x.ac === c) || (x.b === n && x.bc === c))
      if (ids.length) litEdges(ids, n)
      else hot = null
    } else hot = null
  }
  function boxEnter(n: string, e: PointerEvent) {
    if (pan || drag) return
    hov = n
    vr = viewEl.getBoundingClientRect()
    cur = undefined
    aim(n, e)
  }
  function boxLeave() {
    hov = null
    cur = undefined
    hot = null
  }

  function edgeTitle(f: ForeignKey): string {
    const card = (f.one ? t('1:1 (자식 쪽 FK가 UNIQUE)') : '1:N') + ' · ' + (f.required ? t('부모 필수 (FK NOT NULL)') : t('부모 선택 (FK NULL 허용)'))
    return [
      `${f.name}: ${f.table} (${f.columns.join(', ')}) → ${f.refTable} (${f.refColumns.join(', ')})`,
      card,
      `ON DELETE ${f.onDelete} · ON UPDATE ${f.onUpdate}`,
      ...(f.looseRef ? ['⚠ ' + t('참조 대상이 UNIQUE가 아닙니다: 사실상 N:M')] : []),
    ].join('\n')
  }

  // ------------------------------------------------------------ zoom, pan
  let z = $state(1)
  let px = $state(0)
  let py = $state(0)
  let fitted = false

  function fit() {
    const b = bounds
    if (!b || !viewEl.clientWidth) return
    const vw = viewEl.clientWidth
    const vh = viewEl.clientHeight
    // not past 160%: a small picture only gets blurry
    z = Math.min(vw / b.w, vh / b.h, 1.6)
    px = (vw - b.w * z) / 2 - b.x * z
    py = Math.max(16, (vh - b.h * z) / 2) - b.y * z
    fitted = true
  }
  // The pane may be hidden when the layout is done: fit once it has a size.
  $effect(() => {
    if (pos && !fitted) untrack(fit)
  })
  $effect(() => {
    const ro = new ResizeObserver(() => !fitted && fit())
    ro.observe(viewEl)
    return () => ro.disconnect()
  })

  function zoomAt(nz: number, cx: number, cy: number) {
    nz = Math.min(3, Math.max(0.08, nz))
    px = cx - (cx - px) * (nz / z)
    py = cy - (cy - py) * (nz / z)
    z = nz
  }
  const zoomCenter = (f: number) => zoomAt(z * f, viewEl.clientWidth / 2, viewEl.clientHeight / 2)

  function wheel(e: WheelEvent) {
    e.preventDefault()
    const r = viewEl.getBoundingClientRect()
    zoomAt(z * (e.deltaY < 0 ? 1.12 : 1 / 1.12), e.clientX - r.left, e.clientY - r.top)
  }
  // Dragging the background pans; a box moves it; its corner grip widens it.
  let pan = $state<{ x: number; y: number; px: number; py: number } | null>(null)
  let drag = $state<{ n: string; size: boolean; x: number; y: number; dx: number; dy: number; w: number } | null>(null)
  const MIN_W = 130
  const MAX_W = 900

  // Two fingers zoom (and move the picture with their midpoint), whatever
  // the first one had started: a box it moved goes back.
  const fingers = new Map<number, { x: number; y: number }>()
  let pinch: { d: number; z: number; mx: number; my: number; px: number; py: number } | null = null
  function startPinch() {
    if (drag) {
      const { n, dx, dy, w } = drag
      const b = L?.nodes.get(n)
      if (dx || dy || (b && w !== b.w)) manual[n] = { dx, dy, w: b && w !== b.w ? w : undefined }
      else delete manual[n]
    }
    drag = null
    pan = null
    const [a, b] = [...fingers.values()]
    const r = viewEl.getBoundingClientRect()
    pinch = { d: Math.hypot(a.x - b.x, a.y - b.y) || 1, z, mx: (a.x + b.x) / 2 - r.left, my: (a.y + b.y) / 2 - r.top, px, py }
  }
  function movePinch() {
    if (!pinch) return
    const [a, b] = [...fingers.values()]
    const r = viewEl.getBoundingClientRect()
    const nz = Math.min(3, Math.max(0.08, (pinch.z * Math.hypot(a.x - b.x, a.y - b.y)) / pinch.d))
    // the point that was under the fingers' midpoint stays under it
    const mx = (a.x + b.x) / 2 - r.left
    const my = (a.y + b.y) / 2 - r.top
    px = mx - ((pinch.mx - pinch.px) * nz) / pinch.z
    py = my - ((pinch.my - pinch.py) * nz) / pinch.z
    z = nz
  }

  function down(e: PointerEvent) {
    if (e.pointerType === 'touch') {
      fingers.set(e.pointerId, { x: e.clientX, y: e.clientY })
      if (fingers.size === 2) {
        startPinch()
        return viewEl.setPointerCapture(e.pointerId)
      }
      if (fingers.size > 2) return
    }
    // not with the right button: the box would follow the menu
    if (e.button !== 0) return
    e.preventDefault()
    closeMenus()
    const el = e.target as Element
    const n = el.closest('[data-t]')?.getAttribute('data-t')
    const b = n ? L?.nodes.get(n) : undefined
    if (n && b) {
      const o = manual[n]
      drag = { n, size: !!el.closest('.grip'), x: e.clientX, y: e.clientY, dx: o?.dx ?? 0, dy: o?.dy ?? 0, w: o?.w ?? b.w }
      hot = null
    } else {
      boxLeave()
      pan = { x: e.clientX, y: e.clientY, px, py }
    }
    viewEl.setPointerCapture(e.pointerId)
  }
  function move(e: PointerEvent) {
    if (fingers.has(e.pointerId)) fingers.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pinch) return movePinch()
    if (fingers.size > 1) return
    if (drag) {
      // divided by the zoom, so the box stays under the pointer
      const ddx = (e.clientX - drag.x) / z
      const ddy = (e.clientY - drag.y) / z
      const w = manual[drag.n]?.w
      manual[drag.n] = drag.size
        ? { dx: drag.dx, dy: drag.dy, w: Math.max(MIN_W, Math.min(MAX_W, drag.w + ddx)) }
        : { dx: drag.dx + ddx, dy: drag.dy + ddy, w }
    } else if (pan) {
      px = pan.px + e.clientX - pan.x
      py = pan.py + e.clientY - pan.y
    }
  }
  // A pointer that's cancelled sends no pointerup: without this the cursor stays grabbing.
  function up(e?: PointerEvent) {
    if (e) fingers.delete(e.pointerId)
    pinch = null // the finger left behind doesn't take over
    drag = null
    pan = null
  }

  function arrange() {
    manual = {}
    fit()
  }
  let anyManual = $derived(Object.keys(manual).length > 0)

  // ------------------------------------------------------------ context menu
  // On a box: about that table; on the background: about the picture.
  let cmenu = $state<{ x: number; y: number; n: string | null } | null>(null)
  function openCtx(e: MouseEvent) {
    e.preventDefault()
    closeMenus()
    const n = (e.target as Element).closest('[data-t]')?.getAttribute('data-t') ?? null
    cmenu = { x: e.clientX, y: e.clientY, n }
  }
  const tableOf = (n: string): Table => ({ schema: tab.schema, name: n, type: 'TABLE', comment: L?.nodes.get(n)?.comment })
  type CtxItem = { label: string; icon?: string; run: () => void; sep?: boolean; disabled?: boolean }
  let ctxItems = $derived.by<CtxItem[]>(() => {
    if (!cmenu) return []
    const n = cmenu.n
    if (n)
      return [
        { label: t('데이터 보기'), icon: 'table', run: () => openTableData(tab.connId, tab.schema, tableOf(n)) },
        { label: t('DDL 보기'), icon: 'ddl', run: () => openDDL(tab.connId, tab.schema, tableOf(n)) },
        { label: t('이름 복사'), icon: 'copy', run: () => copyText(n) },
        ...(manual[n] ? [{ label: t('이 상자만 원래대로'), icon: 'undo', run: () => delete manual[n], sep: true }] : []),
      ]
    return [
      { label: t('화면 맞춤'), run: fit },
      { label: t('자동 정렬'), icon: 'undo', run: arrange, disabled: !anyManual },
    ]
  })

  // ------------------------------------------------------------ options
  let menu = $state<{ right: number; top: number } | null>(null)
  $effect(() => onCloseMenus(() => ((menu = null), (cmenu = null))))
  function toggleMenu(e: MouseEvent) {
    e.stopPropagation()
    if (menu) return void (menu = null)
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    closeMenus()
    menu = { right: window.innerWidth - r.right, top: r.bottom + 4 }
  }
  type Opt = { key: keyof ViewOptions | ''; label: string; title: string; run?: () => void }
  let groups = $derived<{ title: string; items: Opt[] }[]>([
    ...(L?.stats.iso && L.stats.tables
      ? [{ title: t('테이블 범위'), items: [{ key: 'iso' as const, label: t('관계 없는 테이블'), title: t('FK로 이어지지 않은 테이블을 관계도 아래에 늘어놓습니다') }] }]
      : []),
    {
      title: t('컬럼 범위'),
      items: [
        { key: 'audit', label: t('감사 컬럼'), title: app.erdAudit.join(' · ') || t('지정한 감사 컬럼이 없습니다') },
        { key: '', label: t('감사 컬럼 지정…'), title: t('ERD에서 감사 컬럼으로 보고 감출 컬럼 이름입니다'), run: editAudit },
        { key: 'all', label: t('컬럼 모두 펼치기'), title: t('{n}개까지만 보이던 컬럼을 전부 보입니다', { n: K.MAX_LINES }) },
      ],
    },
    {
      title: t('부가 정보'),
      items: [
        { key: 'tableComment', label: t('테이블 설명'), title: t('테이블 주석을 머리 띠 안에 보입니다') },
        { key: 'columnComment', label: t('컬럼 설명'), title: t('컬럼 주석을 이름 아래 한 줄로 붙입니다') },
        { key: 'nullable', label: t('NULL 허용'), title: t('NULL을 허용하는 컬럼에 표시합니다') },
        { key: 'ai', label: t('자동 증가'), title: t('AUTO_INCREMENT 컬럼에 표시합니다') },
      ],
    },
  ])
  let changed = $derived((Object.keys(DEFAULT_OPTIONS) as (keyof ViewOptions)[]).some((k) => opt[k] !== DEFAULT_OPTIONS[k]))

  let loose = $derived(L ? L.edges.filter((e) => e.fk.looseRef).length : 0)
  let statText = $derived(L ? t('테이블 {t} · 관계 {e}', { t: num(L.stats.tables + L.stats.iso), e: num(L.stats.edges) }) : '')
  let statTitle = $derived.by(() => {
    if (!L) return ''
    const s = L.stats
    return [
      s.tables ? t('테이블 {t} · 관계 {e} · {l}층 · 선 교차 {c}', { t: num(s.tables), e: num(s.edges), l: s.layers, c: s.cross }) : '',
      s.iso ? t('관계 없는 테이블 {n}', { n: num(s.iso) }) : '',
      s.tables ? t('배치 전 교차 {n}개에서 줄인 결과입니다', { n: s.cross0 }) : '',
    ]
      .filter(Boolean)
      .join('\n')
  })
</script>

<svelte:window
  onclick={() => ((menu = null), (cmenu = null))}
  onblur={() => (cmenu = null)}
  onkeydown={(e) => e.key === 'Escape' && ((menu = null), (cmenu = null))}
/>

{#snippet box(b: Node, w: number, v: BoxView)}
  <rect class="frame" width={w} height={v.h} rx="7" />
  <path class="hd" d="M1,{v.headH} V7 A6,6 0 0 1 7,1 H{w - 7} A6,6 0 0 1 {w - 1},7 V{v.headH} Z" />
  <text class="title" x="10" y="18">{b.name}</text>
  {#if v.comment}<text class="tcmt" x="10" y={K.HEAD_H + 6}>{v.comment}</text>{/if}
  {#each v.rows as r (r.line.name)}
    <g class="ln {r.line.kind}" class:aud={r.line.audit} class:hi={hot?.cols.has(ck(b.name, r.line.name))}>
      {#if r.line.kind}<text class="mk" x="10" y={r.y}>{r.line.kind === 'pk' ? '●' : '◆'}</text>{/if}
      <text class="nm" x={10 + (r.line.kind ? markW : 0)} y={r.y}>{r.name}</text>
      <text class="tail" x={w - 10} y={r.y} text-anchor="end">{#each r.tail as p, i}<tspan class={p.cls}>{(i ? ' ' : '') + p.text}</tspan>{/each}</text>
      {#if r.comment}<text class="cm" x="21" y={r.y + K.CMT_LINE}>{r.comment}</text>{/if}
    </g>
  {/each}
  {#if v.more}<text class="more" x="10" y={v.moreY}>{t('…외 {n}개', { n: v.more })}</text>{/if}
  <!-- the corner grip widens the box: a crescent on the same centre as the rounded corner -->
  <path class="grip" d="M{w},{v.h - 7} A7,7 0 0 1 {w - 7},{v.h} L{w - 7},{v.h - 3.6} A3.4,3.4 0 0 0 {w - 3.6},{v.h - 7} Z" />
{/snippet}

<div class="erd">
  <div class="bar">
    <span class="legend" title={t('● 기본 키 · ◆ 외래 키\n선 끝: ○< 여럿 · ○| 하나 · || 필수 (○는 선택)\n자식 쪽 FK가 UNIQUE면 1:1, 아니면 1:N. 왼쪽이 부모입니다.\n휠로 확대하고 끌어서 옮깁니다.')}
      ><b class="pk">●</b>PK <b class="fk">◆</b>FK <b>○&lt;</b>N <b>○|</b>1</span
    >
    {#if L}<span class="stat" title={statTitle}>{statText}</span>{/if}
    {#if loose}
      <span class="warn" title={t('InnoDB는 참조 대상이 UNIQUE가 아니어도 FK를 허용합니다.\n부모가 여럿일 수 있어 사실상 N:M입니다.')}>⚠ {t('비유일 참조 {n}', { n: loose })}</span>
    {/if}
    <span class="fill"></span>
    {#if L && L.nodes.size && !L.edges.length}
      <span class="warn" title={t('관계가 정말 없다기보다 제약을 걸지 않고\n애플리케이션 코드로만 관리하는 경우가 많습니다.')}>{t('FK 제약 없음')}</span>
    {/if}
    <div class="dropdown">
      <button class="icon-btn split" class:changed class:open={!!menu} onclick={toggleMenu} title={t('보기 옵션')}
        ><Icon name="sliders" size={14} /><Icon name="chevronDown" size={10} /></button
      >
      {#if menu}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="menu" style:right="{menu.right}px" style:top="{menu.top}px" use:fitMenu={menu} onclick={(e) => e.stopPropagation()}>
          {#each groups as g (g.title)}
            <div class="ttl">{g.title}</div>
            {#each g.items as it (it.label)}
              {#if it.key}
                {@const k = it.key}
                <button role="menuitemcheckbox" aria-checked={opt[k]} title={it.title} onclick={() => (opt[k] = !opt[k])}>
                  <span class="check">{opt[k] ? '✓' : ''}</span>{it.label}
                </button>
              {:else}
                <button title={it.title} onclick={it.run}><span class="check"></span>{it.label}</button>
              {/if}
            {/each}
          {/each}
        </div>
      {/if}
    </div>
    <button class="icon-btn" onclick={arrange} disabled={!anyManual} title={t('자동 정렬 — 옮기거나 넓힌 상자를 되돌립니다')}><Icon name="undo" size={14} /></button>
    <span class="sep"></span>
    <button class="icon-btn" onclick={() => ((L = null), (error = ''), connected && load())} disabled={loading || !connected} title={t('스키마를 다시 읽습니다')}
      ><Icon name="refresh" size={14} /></button
    >
    <span class="sep"></span>
    <ZoomBox
      value={z}
      min={0.08}
      max={3}
      presets={[0.25, 0.5, 0.75, 1, 1.5, 2]}
      onset={(v) => zoomCenter(v / z)}
      onstep={(d) => zoomCenter(d > 0 ? 1.2 : 1 / 1.2)}
      extra={pos ? [{ label: t('화면 맞춤'), run: fit }] : []}
    />
  </div>

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="view" class:panning={!!pan} bind:this={viewEl} onwheel={wheel} onpointerdown={down} onpointermove={move} onpointerup={up} onpointercancel={up} onpointerleave={() => !drag && boxLeave()} oncontextmenu={openCtx}>
    {#if !connected}
      <div class="msg">
        {t('연결되어 있지 않습니다')}
        <button class="btn" onclick={() => connect(tab.connId)}>{t('연결')}</button>
      </div>
    {:else if error}
      <div class="msg err">{error}</div>
    {:else if loading && !L}
      <div class="msg">{t('불러오는 중…')}</div>
    {:else if L && !L.nodes.size}
      <div class="msg">{t('테이블이 없습니다')}</div>
    {/if}
    {#if L && pos}
      <svg style:--hi-track="{hiTrack.toFixed(3)}px">
        <defs>
          {#each ['', '-h'] as s (s)}
            <g class={s ? 'mk-hot' : 'mk'}>
              <marker id="{uid}-many{s}" viewBox="0 0 20 14" refX={K.MARK_C} refY="7" markerWidth="20" markerHeight="14" orient="auto" markerUnits="userSpaceOnUse" overflow="visible">
                <path d="M0.8,7 L{K.MARK_C},7 M9,7 L0.8,1.6 M9,7 L0.8,12.4" /><circle cx="13.4" cy="7" r="3.1" />
              </marker>
              <marker id="{uid}-one{s}" viewBox="0 0 20 14" refX={K.MARK_C} refY="7" markerWidth="20" markerHeight="14" orient="auto" markerUnits="userSpaceOnUse" overflow="visible">
                <path d="M0.8,7 L{K.MARK_C},7 M4.5,1.6 L4.5,12.4" /><circle cx="13.4" cy="7" r="3.1" />
              </marker>
              <marker id="{uid}-req{s}" viewBox="0 0 20 14" refX={20 - K.MARK_P} refY="7" markerWidth="20" markerHeight="14" orient="auto" markerUnits="userSpaceOnUse" overflow="visible">
                <path d="M{20 - K.MARK_P},7 L20,7 M15.5,1.6 L15.5,12.4 M10,1.6 L10,12.4" />
              </marker>
              <marker id="{uid}-opt{s}" viewBox="0 0 20 14" refX={20 - K.MARK_P} refY="7" markerWidth="20" markerHeight="14" orient="auto" markerUnits="userSpaceOnUse" overflow="visible">
                <path d="M{20 - K.MARK_P},7 L20,7 M15.5,1.6 L15.5,12.4" /><circle cx="10" cy="7" r="3.1" />
              </marker>
            </g>
          {/each}
        </defs>
        <g transform="translate({px} {py}) scale({z})">
          <g class="edges">
            <!-- the lit lines last, over the rest; the rest dim with their boxes -->
            {#each hot ? [...edges.filter((e) => !hot!.edges.has(e.i)), ...edges.filter((e) => hot!.edges.has(e.i))] : edges as e (e.i)}
              {@const on = !!hot?.edges.has(e.i)}
              <path
                class="ed"
                class:loose={e.fk.looseRef}
                class:hot={on}
                class:dim={!!hot && !on}
                d={e.d}
                marker-start="url(#{uid}-{e.fk.one ? 'one' : 'many'}{on ? '-h' : ''})"
                marker-end="url(#{uid}-{e.fk.required ? 'req' : 'opt'}{on ? '-h' : ''})"
              />
            {/each}
            <!-- wide invisible copies: a 1.4px line can't be pointed at -->
            {#each edges as e (e.i)}
              <path class="edh" d={e.d} onpointerenter={() => !pan && litEdges([e.i])} onpointerleave={() => (hot = null)}><title>{edgeTitle(e.fk)}</title></path>
            {/each}
          </g>
          {#if opt.iso && L.isoBand >= 0}
            <text class="isolbl" x={K.PAD} y={pos.isoY - 12}>{t('관계(FK) 없는 테이블 {n}', { n: num(L.stats.iso) })}</text>
          {/if}
          <g class="boxes">
            {#each nodes as b (b.name)}
              {@const g = geo.get(b.name)}
              {#if g}
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <g
                  class="bx"
                  data-t={b.name}
                  class:dim={hot && !hot.tables.has(b.name)}
                  class:moved={!!manual[b.name]}
                  class:grab={drag?.n === b.name}
                  transform="translate({g.x} {g.y})"
                  onpointerenter={(e) => boxEnter(b.name, e)}
                  onpointermove={(e) => !pan && !drag && aim(b.name, e)}
                  onpointerleave={() => !drag && boxLeave()}
                >
                  {#if b.comment}<title>{b.name} — {b.comment}</title>{/if}
                  {@render box(b, g.w, views.get(b.name)!)}
                </g>
              {/if}
            {/each}
            {#if hov && hovView && geo.has(hov)}
              {@const g = geo.get(hov)!}
              <g class="bx full" class:moved={!!manual[hov]} class:grab={drag?.n === hov} transform="translate({g.x} {g.y})">{@render box(L.nodes.get(hov)!, g.w, hovView)}</g>
            {/if}
          </g>
        </g>
      </svg>
    {/if}
  </div>
  {#if cmenu}
    <div class="ctx" style:left="{cmenu.x}px" style:top="{cmenu.y}px" use:fitMenu={cmenu}>
      {#if cmenu.n}<div class="ctx-ttl">{cmenu.n}</div>{/if}
      {#each ctxItems as it (it.label)}
        {#if it.sep}<hr />{/if}
        <button disabled={it.disabled} onclick={() => ((cmenu = null), it.run())}>
          <span class="mi">{#if it.icon}<Icon name={it.icon} size={14} />{/if}</span>{it.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .erd {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    --erd-line: color-mix(in srgb, var(--text-faint) 70%, transparent);
    --erd-box: var(--bg-elev);
    --erd-head: color-mix(in srgb, var(--accent) 14%, var(--bg-elev));
    --erd-frame: color-mix(in srgb, var(--text-faint) 45%, var(--bg-elev));
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 4px;
    white-space: nowrap;
    /* sideways on a phone, as the other toolbars */
    overflow-x: auto;
    scrollbar-width: none;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
    font-size: 12px;
    color: var(--text-dim);
  }
  .bar::-webkit-scrollbar {
    display: none;
  }
  .bar > * {
    flex-shrink: 0;
  }
  .bar b {
    color: var(--text);
  }
  .bar b.pk {
    color: var(--key-pk);
  }
  .bar b.fk {
    color: var(--key-fk);
  }
  .bar .fill {
    flex: 1;
  }
  .legend b {
    margin-right: 2px;
  }
  .legend,
  .bar .warn {
    margin-right: 8px;
  }
  .stat {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text-faint);
  }
  .bar .dim {
    color: var(--text-faint);
  }
  .bar .warn {
    color: var(--warn);
  }
  .sep {
    width: 1px;
    height: 18px;
    margin: 0 2px;
    background: var(--border);
  }
  .icon-btn.split {
    width: auto;
    gap: 1px;
    padding: 0 3px 0 5px;
  }
  .icon-btn.open {
    background: var(--bg-hover);
    color: var(--text);
  }
  /* options off their defaults */
  .icon-btn.changed {
    color: var(--accent);
  }
  .dropdown .menu {
    position: fixed;
    z-index: 60;
    display: flex;
    flex-direction: column;
    min-width: 180px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .menu .ttl {
    padding: 6px 8px 3px 22px;
    font-size: 11px;
    color: var(--text-faint);
  }
  .menu .ttl:not(:first-child) {
    margin-top: 3px;
    border-top: 1px solid var(--border);
  }
  .menu button {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    height: 26px;
    color: var(--text);
    cursor: pointer;
    border: 0;
    background: none;
    border-radius: 4px;
    padding: 0 8px;
  }
  .menu button:hover {
    background: var(--bg-select);
    color: var(--text);
  }
  .menu .check {
    width: 14px;
    color: var(--accent);
  }

  .view {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    background: var(--bg);
    cursor: grab;
    touch-action: none;
    user-select: none;
  }
  .view.panning {
    cursor: grabbing;
  }
  .msg {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--text-dim);
    cursor: default;
  }
  .msg.err {
    color: var(--danger);
    padding: 20px;
    white-space: pre-wrap;
  }
  svg {
    display: block;
    width: 100%;
    height: 100%;
    font-family: var(--font-ui);
  }
  /* Markers don't take the line's stroke: each colour is a set of its own. */
  .mk :global(marker),
  .mk-hot :global(marker) {
    fill: none;
    stroke: var(--erd-line);
    stroke-width: 1.4;
    stroke-linecap: round;
  }
  .mk-hot :global(marker) {
    stroke: var(--accent);
    stroke-width: 2.2;
  }
  defs circle {
    fill: var(--bg);
  }
  .ed {
    fill: none;
    stroke: var(--erd-line);
    stroke-width: 1.4;
    pointer-events: none;
  }
  .ed.loose {
    stroke: var(--warn);
    stroke-dasharray: 6 4;
  }
  .ed.hot {
    stroke: var(--accent);
    stroke-width: 2.2;
  }
  /* as the boxes: the line and its ends */
  .ed.dim {
    opacity: 0.16;
  }
  .edh {
    fill: none;
    stroke: transparent;
    stroke-width: 14;
    pointer-events: stroke;
    cursor: pointer;
  }
  .isolbl {
    font-size: 12px;
    font-weight: 700;
    fill: var(--text-faint);
  }
  .bx {
    cursor: move;
  }
  .grip {
    fill: transparent;
    stroke: transparent;
    stroke-width: 8; /* a wider target than the crescent */
    cursor: ew-resize; /* only the width changes */
  }
  .bx.full .grip {
    fill: var(--text-faint);
  }
  /* moved or widened by hand: what "auto arrange" puts back */
  .bx.moved .frame {
    stroke: color-mix(in srgb, var(--accent) 55%, var(--erd-frame));
  }
  .bx.grab .frame {
    stroke: var(--accent);
    stroke-width: 2;
  }
  .ctx {
    position: fixed;
    z-index: 100;
    display: flex;
    flex-direction: column;
    min-width: 190px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .ctx-ttl {
    padding: 4px 10px 5px;
    color: var(--text-faint);
    font-size: 11px;
    font-weight: 600;
  }
  .ctx button {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px 5px 8px;
    border: none;
    border-radius: 4px;
    background: none;
    color: var(--text);
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .ctx button:hover:not(:disabled) {
    background: var(--bg-select);
  }
  .ctx button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .ctx hr {
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
  .bx.dim {
    opacity: 0.16;
  }
  .bx.full {
    pointer-events: none;
  }
  /* The uncut copy runs past the box over its neighbours: a halo keeps it readable. */
  .bx.full text {
    paint-order: stroke;
    stroke: var(--erd-box);
    stroke-width: 3px;
    stroke-linejoin: round;
  }
  .frame {
    fill: var(--erd-box);
    stroke: var(--erd-frame);
  }
  .hd {
    fill: var(--erd-head);
  }
  .title {
    font-size: 12px;
    font-weight: 700;
    fill: var(--text);
  }
  .tcmt {
    font-size: 10.5px;
    fill: var(--text-dim);
  }
  .ln text {
    font-size: 11px;
  }
  .nm {
    fill: var(--text);
  }
  .pk .mk,
  .pk .nm {
    fill: var(--key-pk);
  }
  .fk .mk,
  .fk .nm {
    fill: var(--key-fk);
  }
  /* audit columns stay a step fainter, but darker than the types */
  .aud .nm {
    fill: var(--text-dim);
  }
  .ln .tail {
    font-size: 10px;
  }
  .tail .ty {
    fill: var(--text-faint);
  }
  .tail .ai {
    fill: var(--warn);
  }
  .tail .nul {
    fill: var(--danger);
  }
  .ln .cm {
    font-size: 9.5px;
    fill: var(--text-faint);
  }
  .ln.hi .nm {
    fill: var(--accent);
    font-weight: 700;
    letter-spacing: var(--hi-track, -0.3px);
  }
  .ln.hi .ty {
    fill: var(--accent);
  }
  .more {
    font-size: 10.5px;
    fill: var(--text-faint);
  }
</style>
