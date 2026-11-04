<script lang="ts">
  import { untrack } from 'svelte'
  import Icon from './Icon.svelte'
  import ZoomBox from './ZoomBox.svelte'
  import { t, num } from '../lib/i18n.svelte'
  import { api, errText } from '../lib/api'
  import { app, connect, type Tab } from '../lib/state.svelte'
  import { closeMenus, fitMenu, onCloseMenus } from '../lib/fit'
  import { layout, boxView, stack, edgePath, DEFAULT_OPTIONS, K, type Layout, type ViewOptions, type BoxView, type Node, type Measure } from '../lib/erd'
  import type { ForeignKey } from '../lib/types'

  let { tab }: { tab: Tab } = $props()

  // Marker ids are per tab: every open tab keeps its SVG in the page.
  let uid = $derived('erd' + tab.id)

  let viewEl: HTMLDivElement
  let L = $state.raw<Layout | null>(null)
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
      L = layout(g, measure)
    } catch (e) {
      error = errText(e)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    if (connected && !L && !error) untrack(load)
  })

  let views = $derived.by(() => {
    const m = new Map<string, BoxView>()
    if (L) for (const b of L.nodes.values()) m.set(b.name, boxView(b, opt, measure, markW, false))
    return m
  })
  let pos = $derived(L ? stack(L, new Map([...views].map(([n, v]) => [n, v.h])), opt) : null)
  let nodes = $derived(L ? [...L.nodes.values()] : [])

  const anchor = (b: Node, v: BoxView, y0: number) => (col: string) => {
    const r = v.rows.find((r) => r.line.name === col)
    return r ? y0 + r.y - 4 : y0 + v.h / 2
  }
  let edges = $derived.by(() => {
    if (!L || !pos) return []
    const p = pos
    const geo = (n: string) => {
      const b = L!.nodes.get(n)!
      const v = views.get(n)!
      const y = p.y.get(n)!
      return { x: b.x, y, w: b.w, h: v.h, anchor: anchor(b, v, y) }
    }
    return L.edges.map((e, i) => ({ ...e, i, d: edgePath(geo(e.a), geo(e.b), e.ac, e.bc, e.a === e.b) }))
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
  let hovView = $derived(hov && L ? boxView(L.nodes.get(hov)!, opt, measure, markW, true) : null)
  let vr: DOMRect | null = null
  let cur: number | null | undefined // -1 header, else the row's index

  function aim(n: string, e: PointerEvent) {
    const v = views.get(n)
    if (!v || !vr || !pos) return
    const sy = (e.clientY - vr.top - py) / z - pos.y.get(n)!
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
    if (pan) return
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
    if (!pos || !viewEl.clientWidth) return
    const vw = viewEl.clientWidth
    const vh = viewEl.clientHeight
    // not past 160%: a small picture only gets blurry
    z = Math.min(vw / pos.w, vh / pos.h, 1.6)
    px = (vw - pos.w * z) / 2
    py = Math.max(16, (vh - pos.h * z) / 2)
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
  let pan = $state<{ x: number; y: number; px: number; py: number } | null>(null)
  function down(e: PointerEvent) {
    if (e.button !== 0) return
    e.preventDefault()
    closeMenus()
    boxLeave()
    pan = { x: e.clientX, y: e.clientY, px, py }
    viewEl.setPointerCapture(e.pointerId)
  }
  function move(e: PointerEvent) {
    if (!pan) return
    px = pan.px + e.clientX - pan.x
    py = pan.py + e.clientY - pan.y
  }

  // ------------------------------------------------------------ options
  let menu = $state<{ left: number; top: number } | null>(null)
  $effect(() => onCloseMenus(() => (menu = null)))
  function toggleMenu(e: MouseEvent) {
    e.stopPropagation()
    if (menu) return void (menu = null)
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    closeMenus()
    menu = { left: r.left, top: r.bottom + 4 }
  }
  type Opt = { key: keyof ViewOptions; label: string; title: string }
  let groups = $derived<{ title: string; items: Opt[] }[]>([
    ...(L?.stats.iso && L.stats.tables
      ? [{ title: t('테이블 범위'), items: [{ key: 'iso' as const, label: t('관계 없는 테이블'), title: t('FK로 이어지지 않은 테이블을 관계도 아래에 늘어놓습니다') }] }]
      : []),
    {
      title: t('컬럼 범위'),
      items: [
        { key: 'audit', label: t('감사 컬럼'), title: 'created_* · updated_* · deleted_* · version' },
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

<svelte:window onclick={() => (menu = null)} />

{#snippet box(b: Node, v: BoxView)}
  <rect class="frame" width={b.w} height={v.h} rx="7" />
  <path class="hd" d="M1,{v.headH} V7 A6,6 0 0 1 7,1 H{b.w - 7} A6,6 0 0 1 {b.w - 1},7 V{v.headH} Z" />
  <text class="title" x="10" y="18">{b.name}</text>
  {#if v.comment}<text class="tcmt" x="10" y={K.HEAD_H + 6}>{v.comment}</text>{/if}
  {#each v.rows as r (r.line.name)}
    <g class="ln {r.line.kind}" class:aud={r.line.audit} class:hi={hot?.cols.has(ck(b.name, r.line.name))}>
      {#if r.line.kind}<text class="mk" x="10" y={r.y}>{r.line.kind === 'pk' ? '●' : '◆'}</text>{/if}
      <text class="nm" x={10 + (r.line.kind ? markW : 0)} y={r.y}>{r.name}</text>
      <text class="tail" x={b.w - 10} y={r.y} text-anchor="end">{#each r.tail as p, i}<tspan class={p.cls}>{(i ? ' ' : '') + p.text}</tspan>{/each}</text>
      {#if r.comment}<text class="cm" x="21" y={r.y + K.CMT_LINE}>{r.comment}</text>{/if}
    </g>
  {/each}
  {#if v.more}<text class="more" x="10" y={v.moreY}>{t('…외 {n}개', { n: v.more })}</text>{/if}
{/snippet}

<div class="erd">
  <div class="bar">
    <span class="legend" title={t('● 기본 키 · ◆ 외래 키\n선 끝: ○< 여럿 · ○| 하나 · || 필수 (○는 선택)\n자식 쪽 FK가 UNIQUE면 1:1, 아니면 1:N. 위쪽이 부모입니다.\n휠로 확대하고 끌어서 옮깁니다.')}
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
    <ZoomBox value={z} min={0.08} max={3} presets={[0.25, 0.5, 0.75, 1, 1.5, 2]} onset={(v) => zoomCenter(v / z)} onstep={(d) => zoomCenter(d > 0 ? 1.2 : 1 / 1.2)} />
    <div class="dropdown">
      <button class:changed class:open={!!menu} onclick={toggleMenu}>{t('보기 옵션')} <Icon name="chevronDown" size={10} /></button>
      {#if menu}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="menu" style:left="{menu.left}px" style:top="{menu.top}px" use:fitMenu={menu} onclick={(e) => e.stopPropagation()}>
          {#each groups as g (g.title)}
            <div class="ttl">{g.title}</div>
            {#each g.items as it (it.key)}
              <button role="menuitemcheckbox" aria-checked={opt[it.key]} title={it.title} onclick={() => (opt[it.key] = !opt[it.key])}>
                <span class="check">{opt[it.key] ? '✓' : ''}</span>{it.label}
              </button>
            {/each}
          {/each}
        </div>
      {/if}
    </div>
    <span class="grp">
      <button onclick={fit} disabled={!pos}>{t('화면 맞춤')}</button>
      <button onclick={() => ((L = null), (error = ''), connected && load())} disabled={loading || !connected} title={t('스키마를 다시 읽습니다')}
        ><Icon name="refresh" size={13} /></button
      >
    </span>
  </div>

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="view" class:panning={!!pan} bind:this={viewEl} onwheel={wheel} onpointerdown={down} onpointermove={move} onpointerup={() => (pan = null)} onpointercancel={() => (pan = null)} onpointerleave={boxLeave}>
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
            {#each edges as e (e.i)}
              {@const on = !!hot?.edges.has(e.i)}
              <path
                class="ed"
                class:loose={e.fk.looseRef}
                class:hot={on}
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
              {#if pos.y.has(b.name)}
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <g
                  class="bx"
                  class:dim={hot && !hot.tables.has(b.name)}
                  transform="translate({b.x} {pos.y.get(b.name)})"
                  onpointerenter={(e) => boxEnter(b.name, e)}
                  onpointermove={(e) => !pan && aim(b.name, e)}
                  onpointerleave={boxLeave}
                >
                  {#if b.comment}<title>{b.name} — {b.comment}</title>{/if}
                  {@render box(b, views.get(b.name)!)}
                </g>
              {/if}
            {/each}
            {#if hov && hovView && pos.y.has(hov)}
              {@const b = L.nodes.get(hov)!}
              <g class="bx full" transform="translate({b.x} {pos.y.get(hov)})">{@render box(b, hovView)}</g>
            {/if}
          </g>
        </g>
      </svg>
    {/if}
  </div>
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
    gap: 12px;
    white-space: nowrap;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
    font-size: 12px;
    color: var(--text-dim);
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
  .bar button {
    height: 24px;
    padding: 0 9px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-elev);
    color: var(--text);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .bar button:hover:not(:disabled),
  .bar button.open {
    background: var(--bg-hover);
  }
  .bar button:disabled {
    opacity: 0.45;
    cursor: default;
  }
  /* options off their defaults */
  .bar button.changed {
    color: var(--accent);
  }
  .grp {
    display: inline-flex;
  }
  .grp > button {
    border-radius: 0;
  }
  .grp > button + button {
    border-left: 0;
  }
  .grp > button:first-child {
    border-radius: 4px 0 0 4px;
  }
  .grp > button:last-child {
    border-radius: 0 4px 4px 0;
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
    padding: 6px 8px 3px 26px;
    font-size: 11px;
    color: var(--text-faint);
  }
  .menu .ttl:not(:first-child) {
    margin-top: 3px;
    border-top: 1px solid var(--border);
  }
  .menu button {
    justify-content: flex-start;
    height: 26px;
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
    cursor: default;
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
