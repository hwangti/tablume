<script lang="ts">
  import { fitMenu, closeMenus, onCloseMenus } from './lib/fit'
  import { modKey } from './lib/clipboard'
  import { isWeb } from './lib/platform'
  import { t as tr } from './lib/i18n.svelte'
  import { filePick } from './lib/webfiles.svelte'
  import FileBrowser from './components/FileBrowser.svelte'
  import { onMount } from 'svelte'
  import { installTooltips } from './lib/tooltip'
  import Sidebar from './components/Sidebar.svelte'
  import QueryTab from './components/QueryTab.svelte'
  import ErdView from './components/ErdView.svelte'
  import ConnectionDialog from './components/ConnectionDialog.svelte'
  import ExportDialog from './components/ExportDialog.svelte'
  import ImportDialog from './components/ImportDialog.svelte'
  import Modal from './components/Modal.svelte'
  import SqlPreview from './components/SqlPreview.svelte'
  import Icon from './components/Icon.svelte'
  import TabStrip from './components/TabStrip.svelte'
  import { layout, groups, zoneAt, zoneRect, resize, equalize, type Bar, type Rect, type Side } from './lib/panes'
  import logo from './assets/logo.svg'
  import {
    app,
    loadConnections,
    loadWorkspace,
    openConsole,
    closeTab,
    closeTabs,
    duplicateTab,
    connById,
    activeTab,
    saveWorkspaceSoon,
    pinTab,
    zoomBy,
    tabWindowId,
    initTabWindow,
    initMainWindowEvents,
    detachTab,
    backToMain,
    closeTabWindow,
    toggleSidebar,
    initWebSessions,
    checkUpdate,
    dismissToast,
    groupTabs,
    tabShown,
    joinGroup,
    moveToGroup,
    splitTo,
    unsplitAll,
    type Tab,
  } from './lib/state.svelte'
  import { Window } from '@wailsio/runtime'
  import { driverLabel } from './lib/types'

  let sideW = $state(270)
  let password = $state('')
  let ready = $state(false)

  onMount(async () => {
    installTooltips()
    // passive: false so the page itself doesn't scroll while zooming
    window.addEventListener('wheel', zoomWheel, { passive: false, capture: true })
    if (tabWindowId) {
      await initTabWindow()
    } else {
      await loadConnections()
      await loadWorkspace()
      initMainWindowEvents()
      if (isWeb) initWebSessions()
      // after the start-up work, so it never slows the first screen
      if (app.updateCheck) setTimeout(() => checkUpdate(false), 4000)
    }
    ready = true
  })

  function startDrag(e: MouseEvent) {
    e.preventDefault()
    const move = (ev: MouseEvent) => (sideW = Math.max(180, Math.min(600, ev.clientX)))
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  function keydown(e: KeyboardEvent) {
    const mod = e.metaKey || e.ctrlKey
    if (!mod || app.editing || app.prompt || tabWindowId) return
    const k = e.key.toLowerCase()
    if (k === 'b' && !e.shiftKey && !e.altKey) {
      e.preventDefault()
      sideToggle()
    } else if (k === 't') {
      e.preventDefault()
      const connId = activeTab()?.connId ?? app.connections[0]?.id
      if (connId) openConsole(connId)
    } else if (k === 'w' && app.activeTab) {
      e.preventDefault()
      closeTab(app.activeTab)
    } else if (/^[1-9]$/.test(k) && !e.shiftKey) {
      const t = stripTabs()[Number(k) - 1]
      if (t) {
        e.preventDefault()
        app.activeTab = t.id
      }
    }
  }

  // A phone: the tree is a drawer over the content, folded away by default.
  // Not app.sidebarHidden: the workspace is shared with the desktop's browser.
  const narrowMq = matchMedia('(max-width: 700px)')
  let narrow = $state(narrowMq.matches)
  narrowMq.addEventListener('change', (e) => ((narrow = e.matches), (drawer = false)))
  let drawer = $state(false)
  // Opening a table or a console from it is what it was opened for: a tap
  // on a table (the one already shown too) or a tab opened from a menu. A
  // moment later, so a double tap still reaches the tree (it pins the tab).
  let closing: ReturnType<typeof setTimeout> | undefined
  const closeDrawerSoon = () => {
    clearTimeout(closing)
    closing = setTimeout(() => (drawer = false), 300)
  }
  const shownKey = () => {
    const t = app.tabs.find((x) => x.id === app.activeTab)
    return `${app.activeTab}|${app.tabs.length}|${t?.schema}|${t?.title}`
  }
  let shown = shownKey()
  $effect(() => {
    const k = shownKey()
    if (k !== shown && narrow && drawer) closeDrawerSoon()
    shown = k
  })
  function drawerClick(e: MouseEvent) {
    const el = e.target as Element
    if (narrow && el.closest('.node.table, .node.routine') && !el.closest('.chev')) closeDrawerSoon()
  }

  // The sidebar button and Ctrl/⌘+B; in 넓게 보기 they bring everything back.
  function sideToggle() {
    if (narrow) {
      drawer = app.wide || !drawer
      app.wide = false
      return
    }
    if (app.wide) {
      app.wide = false
      if (app.sidebarHidden) toggleSidebar()
    } else toggleSidebar()
  }

  // Window zoom: Ctrl/Cmd + = / - / 0 (numpad too) and Ctrl/Cmd + wheel,
  // anywhere in the window (caught before the editor sees the key). The
  // browser build leaves zoom to the browser.
  function zoomKey(e: KeyboardEvent) {
    if (isWeb || !(e.ctrlKey || e.metaKey) || e.altKey) return
    const c = e.code
    const step =
      e.key === '0' || c === 'Numpad0'
        ? 0
        : e.key === '-' || e.key === '_' || c === 'NumpadSubtract'
          ? -1
          : e.key === '=' || e.key === '+' || c === 'NumpadAdd'
            ? 1
            : null
    if (step === null) return
    e.preventDefault()
    e.stopPropagation()
    zoomBy(step)
  }

  // A trackpad sends a stream of small deltas; one step per 120 ms at most.
  let lastWheelZoom = 0
  function zoomWheel(e: WheelEvent) {
    if (isWeb || !(e.ctrlKey || e.metaKey) || Math.abs(e.deltaY) < 1) return
    e.preventDefault()
    if (e.timeStamp - lastWheelZoom < 120) return
    lastWheelZoom = e.timeStamp
    zoomBy(e.deltaY < 0 ? 1 : -1)
  }

  // Next / previous tab: Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PageDown / PageUp,
  // and on macOS also ⌘⇧] / ⌘⇧[. Caught before the editor or the grid
  // (where Tab moves between cells) see the key.
  function switchKey(e: KeyboardEvent) {
    const tabs = stripTabs()
    if (app.editing || app.prompt || tabs.length < 2) return
    let dir = 0
    if (e.ctrlKey && e.key === 'Tab') dir = e.shiftKey ? -1 : 1
    else if (e.ctrlKey && !e.shiftKey && (e.key === 'PageDown' || e.key === 'PageUp')) dir = e.key === 'PageDown' ? 1 : -1
    else if (e.metaKey && e.shiftKey && (e.code === 'BracketRight' || e.code === 'BracketLeft')) dir = e.code === 'BracketRight' ? 1 : -1
    if (!dir) return
    e.preventDefault()
    e.stopPropagation()
    const i = tabs.findIndex((t) => t.id === app.activeTab)
    const n = tabs.length
    app.activeTab = tabs[(i + dir + n) % n].id
  }

  function submitPassword() {
    app.prompt?.resolve(app.prompt.value !== undefined ? promptText : password)
    password = ''
  }

  // text prompts start from their value
  let promptText = $state('')
  $effect(() => {
    if (app.prompt?.value !== undefined) promptText = app.prompt.value
  })

  // after the value has been filled in
  function selectAll(node: HTMLInputElement) {
    requestAnimationFrame(() => {
      node.focus()
      node.select()
    })
  }

  // ---------------------------------------------------------------- tab groups
  // The tab area: each group's strip and its shown tab where the split tree
  // puts them (see panes.ts). Every tab's pane stays mounted where it is;
  // only its place and visibility change, so nothing reloads on a split.
  // 넓게 보기 shows the active tab alone; a phone, one strip of all tabs.

  const FULL: Rect = { x: 0, y: 0, w: 1, h: 1 }
  const STRIP = 34 // px

  const flat = $derived(narrow)
  const view = $derived.by(() => {
    if (app.wide || flat) return { rects: new Map<string, Rect>(), bars: [] as Bar[] }
    return layout(app.panes)
  })
  const multi = $derived(view.rects.size > 1)

  /** The tabs ⌘1–9 and Ctrl+Tab go through: the focused group's. */
  const stripTabs = () => (flat ? app.tabs.filter((t) => !t.detached) : groupTabs(app.focus))

  const paneShown = (t: Tab) => (app.wide || flat ? t.id === app.activeTab : tabShown(t))
  const paneRect = (t: Tab) => (app.wide || flat ? FULL : (view.rects.get(t.group ?? '') ?? FULL))

  const pct = (v: number) => `${+(v * 100).toFixed(4)}%`
  function place(r: Rect, top = 0, height?: number) {
    return `left:${pct(r.x)};width:${pct(r.w)};top:calc(${pct(r.y)} + ${top}px);height:${height !== undefined ? height + 'px' : `calc(${pct(r.h)} - ${top}px)`}`
  }

  // A click or the keyboard in a pane makes its tab the active one.
  const focusTab = (t: Tab) => app.activeTab !== t.id && (app.activeTab = t.id)

  let contentEl = $state<HTMLDivElement>() as HTMLDivElement

  // The line between two groups: dragged to resize, double-clicked to share evenly.
  function barDown(e: PointerEvent, b: Bar) {
    if (e.button !== 0) return
    e.preventDefault()
    const box = contentEl.getBoundingClientRect()
    const row = b.dir === 'row'
    resizing = row ? 'col-resize' : 'row-resize'
    const move = (ev: PointerEvent) => {
      const f = row ? ((ev.clientX - box.left) / box.width - b.rect.x) / b.rect.w : ((ev.clientY - box.top) / box.height - b.rect.y) / b.rect.h
      const min = row ? 160 / (b.rect.w * box.width) : (STRIP + 80) / (b.rect.h * box.height)
      app.panes = resize(app.panes, b.path, b.i, f, min)
    }
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      resizing = ''
      saveWorkspaceSoon()
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }
  let resizing = $state('') // the cursor while a line is dragged

  // Dragging a tab: along a strip it moves (into that strip's group, if
  // another); over a group's pane it shows where it would go, and dropping
  // there splits that group (an edge) or moves the tab into it (the middle).
  // A finger drags after holding the tab (the menu that opened then closes);
  // before that, a swipe scrolls the strip.
  const TAB_HOLD_MS = 450
  let tabHeld = false
  let drop = $state<{ gid: string; zone: Side | 'center'; rect: Rect } | null>(null)

  function tabDown(e: PointerEvent, id: string) {
    if (e.button === 1) {
      e.preventDefault()
      closeTab(id)
      return
    }
    if (e.button !== 0 || (e.target as HTMLElement).closest('button, input')) return
    const x0 = e.clientX
    const y0 = e.clientY
    const touch = e.pointerType !== 'mouse'
    // held: from here a move drags (the first touchmove too)
    const hold = touch ? setTimeout(() => (tabHeld = true), TAB_HOLD_MS) : 0
    const move = (ev: PointerEvent) => {
      if (ev.pointerId !== e.pointerId) return
      if (!app.tabDrag) {
        if (Math.abs(ev.clientX - x0) < 5 && Math.abs(ev.clientY - y0) < 5) return
        if (touch) {
          if (!tabHeld) return up() // a swipe
          closeMenus()
        }
        app.tabDrag = id
        // (WebKit carries a selection begun on the tab into an editor it passes over)
        getSelection()?.removeAllRanges()
      }
      dragOver(id, ev.clientX, ev.clientY)
    }
    const up = () => {
      clearTimeout(hold)
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      if (app.tabDrag) {
        if (drop) drop.zone === 'center' ? moveToGroup(id, drop.gid) : splitTo(id, drop.gid, drop.zone)
        app.tabDrag = '' // (emptied groups go now)
        saveWorkspaceSoon()
      }
      drop = null
      tabHeld = false
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
  }

  function dragOver(id: string, x: number, y: number) {
    const t = app.tabs.find((v) => v.id === id)
    if (!t) return
    // along a strip: placed among its tabs, live
    for (const el of contentEl.querySelectorAll<HTMLElement>('.strip')) {
      const r = el.getBoundingClientRect()
      if (x < r.left || x > r.right || y < r.top - 6 || y > r.bottom + 6) continue
      drop = null
      const gid = el.dataset.gid ?? ''
      if (gid) joinGroup(id, gid)
      const els = [...el.querySelectorAll<HTMLElement>('.tab')].filter((v) => v.dataset.id !== id)
      const order = els.map((v) => v.dataset.id!)
      const to = els.findIndex((v) => {
        const b = v.getBoundingClientRect()
        return x < b.left + b.width / 2
      })
      order.splice(to < 0 ? order.length : to, 0, id)
      reorder(gid, order)
      return
    }
    // over a group: an edge splits it, the middle takes the tab in
    drop = null
    if (!view.rects.size) return
    const box = contentEl.getBoundingClientRect()
    const fx = (x - box.left) / box.width
    const fy = (y - box.top) / box.height
    for (const [gid, r] of view.rects) {
      if (fx < r.x || fx > r.x + r.w || fy < r.y || fy > r.y + r.h) continue
      // (a group's only tab can't split it)
      if (t.group === gid && groupTabs(gid).length < 2) return
      const zone = zoneAt(r, fx, fy)
      if (zone === 'center' && t.group === gid) return
      drop = { gid, zone, rect: zoneRect(r, zone) }
      return
    }
  }

  // A strip's tabs in this order ('' = every tab, on a phone); they keep
  // the places in app.tabs that the strip's tabs had.
  function reorder(gid: string, order: string[]) {
    const mine = (t: Tab) => !t.detached && (!gid || t.group === gid)
    if (app.tabs.filter(mine).length !== order.length) return
    const byId = new Map(app.tabs.map((t) => [t.id, t]))
    let k = 0
    const next = app.tabs.map((t) => (mine(t) ? byId.get(order[k++])! : t))
    if (next.some((t, i) => t !== app.tabs[i])) app.tabs = next
  }

  // ---------------------------------------------------------------- tab menu
  type TabMenuItem = { label: string; icon?: string; run: () => void; disabled?: boolean; sep?: boolean; danger?: boolean; kbd?: string }
  let tabMenu = $state<{ x: number; y: number; items: TabMenuItem[] } | null>(null)
  $effect(() => onCloseMenus(() => (tabMenu = null)))

  function openTabMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    const i = app.tabs.findIndex((t) => t.id === id)
    const t = app.tabs[i]
    if (!t) return
    // Bulk closes act on this strip (tabs in their own windows aren't in one).
    const ids = app.tabs.map((x) => x.id)
    const here = (x: string) => {
      const y = app.tabs.find((v) => v.id === x)
      return !!y && !y.detached && (flat || y.group === t.group)
    }
    const all = groups(app.panes).map((g) => g.id)
    const mine = groupTabs(t.group ?? '')
    const left = ids.slice(0, i).filter(here)
    const right = ids.slice(i + 1).filter(here)
    const others = ids.filter((x) => x !== id && here(x))
    const sameConn = app.tabs.filter((x) => x.connId === t.connId && !x.detached).map((x) => x.id)
    const c = connById(t.connId)
    closeMenus()
    tabMenu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        // (no shortcut shown in the browser, which keeps ⌘W / ⌘T for its own tabs)
        { label: tr('닫기'), icon: 'close', run: () => closeTab(id), kbd: isWeb ? undefined : modKey + 'W' },
        { label: tr('다른 탭 닫기'), run: () => closeTabs(others), disabled: !others.length },
        { label: tr('왼쪽 탭 닫기'), run: () => closeTabs(left), disabled: !left.length },
        { label: tr('오른쪽 탭 닫기'), run: () => closeTabs(right), disabled: !right.length },
        { label: tr('같은 연결 탭 닫기') + (c ? ` (${c.name})` : ''), icon: 'db', run: () => closeTabs(sameConn) },
        { label: tr('모든 탭 닫기'), run: () => closeTabs(ids.filter(here)), danger: true },
        app.wide && app.activeTab === id
          ? { label: tr('넓게 보기 종료'), icon: 'unwide', run: () => (app.wide = false), sep: true }
          : { label: tr('넓게 보기'), icon: 'wide', run: () => ((app.activeTab = id), (app.wide = true)), sep: true },
        ...(flat
          ? []
          : [
              { label: tr('오른쪽으로 분할'), icon: 'splitRight', run: () => splitTo(id, t.group!, 'right'), disabled: mine.length < 2, sep: true },
              { label: tr('아래로 분할'), icon: 'splitDown', run: () => splitTo(id, t.group!, 'bottom'), disabled: mine.length < 2 },
              ...(all.length > 1
                ? [
                    { label: tr('다른 그룹으로 옮기기'), run: () => moveToGroup(id, all[(all.indexOf(t.group!) + 1) % all.length]) },
                    { label: tr('분할 모두 해제'), run: unsplitAll },
                  ]
                : []),
            ]),
        ...(isWeb ? [] : [{ label: tr('새 창으로 열기'), icon: 'detach', run: () => detachTab(id), sep: true }]),
        { label: tr('탭 복제'), icon: 'copy', run: () => duplicateTab(id) },
        ...(t.preview ? [{ label: tr('탭 고정'), icon: 'lock', run: () => pinTab(t) }] : []),
        { label: tr('이름 바꾸기'), icon: 'edit', run: () => (renaming = id) },
      ],
    }
  }

  let renaming = $state<string | null>(null)

  // The browser tab is titled after the active tab.
  $effect(() => {
    if (!isWeb || tabWindowId) return
    const t = activeTab()
    document.title = t ? `${t.title} — Tablume` : 'Tablume'
  })

  const sideHidden = $derived(narrow ? !drawer : app.sidebarHidden)
  const sideGone = $derived(sideHidden || app.wide)

  // A tab window is titled after its tab.
  $effect(() => {
    if (!tabWindowId) return
    const t = app.tabs[0]
    if (!t) return
    const c = connById(t.connId)
    Window.SetTitle(c ? `${t.title} — ${c.name}` : t.title).catch(() => {})
  })
</script>

<svelte:window onkeydown={keydown} onkeydowncapture={(e) => (zoomKey(e), switchKey(e))} onclick={() => (tabMenu = null)} onblur={() => (tabMenu = null)} />

{#if tabWindowId}
  {@const t = app.tabs[0]}
  {@const c = t ? connById(t.connId) : undefined}
  <div class="tw">
    <div class="tw-bar" style:--conn-color={c?.color || 'transparent'}>
      {#if t}
        <Icon name={t.erd ? 'erd' : t.tableRef ? 'table' : 'console'} size={14} />
        <span class="tw-title">{t.title}</span>
        {#if c}<span class="tw-conn">{c.name}</span>{/if}
      {/if}
      <span class="tw-fill"></span>
      <button class="btn mini" title={tr('이 탭을 메인 창의 탭으로 되돌립니다 (창 닫기 버튼과 같음)')} onclick={backToMain} disabled={!t}>
        <Icon name="attach" size={14} /> {tr('메인 창으로')}
      </button>
      <button class="btn mini" title={tr('탭을 닫습니다')} onclick={closeTabWindow} disabled={!t}><Icon name="close" size={14} /> {tr('닫기')}</button>
    </div>
    <div class="content">
      {#if t}
        <div class="pane">{#if t.erd}<ErdView tab={t} />{:else}<QueryTab tab={t} />{/if}</div>
      {:else if ready}
        <div class="welcome"><p>{tr('이 탭은 더 이상 없습니다.')}</p></div>
      {/if}
    </div>
  </div>
{:else}
<div class="layout" style:grid-template-columns={sideGone || narrow ? '1fr' : `${sideW}px 1px 1fr`}>
  <!-- hidden, not removed: the tree keeps what is expanded, the filter, … -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="side" class:gone={sideGone} class:drawer={narrow} onclick={drawerClick}><Sidebar /></div>
  {#if narrow}
    {#if !sideGone}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div class="scrim" onclick={() => (drawer = false)}></div>
    {/if}
  {:else}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="vdivider" class:gone={sideGone} onmousedown={startDrag}></div>
  {/if}

  <main>
    {#if app.wide}
      {@const t = activeTab()}
      {@const c = t ? connById(t.connId) : undefined}
      <div class="tw-bar wide" style:--conn-color={c?.color || 'transparent'}>
        {#if t}
          <Icon name={t.erd ? 'erd' : t.tableRef ? 'table' : 'console'} size={14} />
          <span class="tw-title">{t.title}</span>
          {#if c}<span class="tw-conn">{c.name}</span>{/if}
        {/if}
        <span class="tw-fill"></span>
        <button class="icon-btn strip-btn" title="{tr('넓게 보기 종료 — 연결 목록과 탭 줄을 다시 보입니다')} ({modKey}B)" onclick={() => (app.wide = false)}>
          <Icon name="unwide" />
        </button>
      </div>
    {/if}

    <div class="content" class:resizing={!!resizing} class:tab-drag={!!app.tabDrag} style:cursor={resizing || null} bind:this={contentEl}>
      {#snippet sideBtn()}
        <button class="icon-btn strip-btn" title="{tr(sideHidden ? '연결 목록 보이기' : '연결 목록 숨기기')} ({modKey}B)" onclick={sideToggle}>
          <Icon name={sideHidden ? 'sidebarOff' : 'sidebar'} />
        </button>
      {/snippet}
      {#snippet strip(gid: string, tabs: Tab[], active: string, focused: boolean, first: boolean)}
        <TabStrip
          {gid}
          {tabs}
          {active}
          {focused}
          bind:renaming
          held={() => tabHeld}
          ondown={tabDown}
          onmenu={openTabMenu}
          onclose={closeTab}
          onnew={app.connections.length
            ? () => {
                if (gid) app.focus = gid
                openConsole(activeTab()?.connId ?? app.connections[0].id)
              }
            : undefined}
          lead={first ? sideBtn : undefined}
          onwide={active ? () => ((app.activeTab = active), (app.wide = true)) : undefined}
        />
      {/snippet}

      {#if flat && !app.wide}
        <div class="at" style={place(FULL, 0, STRIP)}>
          {@render strip('', app.tabs.filter((t) => !t.detached), app.activeTab, true, true)}
        </div>
      {:else if !app.wide}
        {#each groups(app.panes) as g (g.id)}
          {@const r = view.rects.get(g.id) ?? FULL}
          <div class="at" style={place(r, 0, STRIP)}>
            {@render strip(g.id, groupTabs(g.id), g.active, !multi || g.id === app.focus, r.x === 0 && r.y === 0)}
          </div>
        {/each}
      {/if}

      {#each app.tabs as t (t.id)}
        {#if !t.detached}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="pane"
            class:hidden={!paneShown(t)}
            style={place(paneRect(t), app.wide ? 0 : STRIP)}
            onpointerdowncapture={() => focusTab(t)}
            onfocusin={() => focusTab(t)}
          >
            {#if t.erd}<ErdView tab={t} />{:else}<QueryTab tab={t} />{/if}
          </div>
        {/if}
      {/each}

      {#each view.bars as b (b.path.join('.') + ':' + b.i)}
        {@const row = b.dir === 'row'}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="bar"
          class:row
          style={row
            ? `left:${pct(b.at)};top:${pct(b.rect.y)};height:${pct(b.rect.h)}`
            : `top:${pct(b.at)};left:${pct(b.rect.x)};width:${pct(b.rect.w)}`}
          title={tr('드래그해 크기를 바꿉니다. 더블클릭하면 똑같이 나눕니다.')}
          onpointerdown={(e) => barDown(e, b)}
          ondblclick={() => ((app.panes = equalize(app.panes, b.path)), saveWorkspaceSoon())}
        ></div>
      {/each}

      {#if drop}
        <div class="drop" style={place(drop.rect)}></div>
      {/if}

      {#if ready && !app.tabs.some((t) => !t.detached)}
        <div class="welcome" style={place(FULL, app.wide ? 0 : STRIP)}>
          <img src={logo} width="64" height="64" alt="" />
          <h2>Tablume</h2>
          {#if app.connections.length === 0}
            <p>{@html tr('왼쪽의 <b>+</b> 버튼으로 첫 연결을 추가하세요.')}</p>
          {:else}
            <p>{tr('연결을 클릭해 접속하고, 테이블을 클릭하면 데이터를 볼 수 있습니다.')}</p>
            <p class="dim">{isWeb ? tr('콘솔 아이콘으로 새 SQL 콘솔을 엽니다.') : tr('콘솔 아이콘이나 Ctrl/⌘+T로 새 SQL 콘솔을 엽니다.')}</p>
          {/if}
        </div>
      {/if}
    </div>
  </main>
  {#if tabMenu}
    <div class="tab-menu" style:left="{tabMenu.x}px" style:top="{tabMenu.y}px" use:fitMenu={tabMenu}>
      {#each tabMenu.items as it (it.label)}
        {#if it.sep}<hr />{/if}
        <button class:danger={it.danger} disabled={it.disabled} onclick={it.run}>
          <span class="mi">{#if it.icon}<Icon name={it.icon} size={14} />{/if}</span>{it.label}
          {#if it.kbd}<kbd>{it.kbd}</kbd>{/if}
        </button>
      {/each}
    </div>
  {/if}
</div>
{/if}

{#if app.editing}
  <ConnectionDialog initial={app.editing} />
{/if}

{#if app.dataDialog}
  {@const d = app.dataDialog}
  {#key d}
    {#if d.kind === 'export'}<ExportDialog connId={d.connId} schema={d.schema} tables={d.tables} />
    {:else}<ImportDialog connId={d.connId} schema={d.schema} table={d.tables[0]} />{/if}
  {/key}
{/if}

{#if filePick.req}
  {#key filePick.req}<FileBrowser req={filePick.req} />{/key}
{/if}

{#if app.prompt}
  <Modal title={app.prompt.title} onclose={() => app.prompt?.resolve(null)} width={380} dismissible={false}>
    <form onsubmit={(e) => (e.preventDefault(), submitPassword())}>
      {#if app.prompt.value !== undefined}
        <input type="text" class="mono" bind:value={promptText} use:selectAll style="width:100%" spellcheck="false" />
      {:else}
        <!-- focused by hand: autofocus only acts when nothing else has the
             focus, and a double-click in the tree leaves it on the tree -->
        <input type="password" bind:value={password} use:selectAll style="width:100%" placeholder={tr('비밀번호')} />
      {/if}
    </form>
    {#snippet footer()}
      <span style="flex:1"></span>
      <button class="btn" onclick={() => app.prompt?.resolve(null)}>{tr('취소')}</button>
      <button class="btn primary" onclick={submitPassword}>{app.prompt?.ok ?? tr('연결')}</button>
    {/snippet}
  </Modal>
{/if}

{#if app.confirm}
  <Modal title={tr('확인')} onclose={() => app.confirm?.resolve(false)} width={app.confirm.sql ? 560 : 380}>
    <p style="margin:0">{app.confirm.text}</p>
    {#if app.confirm.sql}<div class="confirm-sql"><SqlPreview code={app.confirm.sql.join("\n\n")} driver={app.confirm.driver} /></div>{/if}
    {#snippet footer()}
      <span style="flex:1"></span>
      <button class="btn" onclick={() => app.confirm?.resolve(false)}>{tr('취소')}</button>
      <button class="btn primary" onclick={() => app.confirm?.resolve(true)}>{tr('확인')}</button>
    {/snippet}
  </Modal>
{/if}

<div class="toasts">
  {#each app.toasts as t (t.id)}
    <div class="toast" class:error={t.kind === 'error'} class:act={t.action}>
      <span>{t.text}</span>
      {#if t.action}
        <button class="btn primary" onclick={() => (t.action!.run(), dismissToast(t.id))}>{t.action.label}</button>
        <button class="x-btn" title={tr('닫기')} onclick={() => dismissToast(t.id)}><Icon name="close" size={14} /></button>
      {/if}
    </div>
  {/each}
</div>

<style>
  .layout {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    height: 100%;
    overflow: hidden;
  }
  /* a tab in its own window: one bar, then the tab */
  .tw {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }
  .tw-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 34px;
    flex-shrink: 0;
    padding: 0 8px 0 12px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    box-shadow: inset 0 3px 0 var(--conn-color);
  }
  .tw-title {
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tw-conn {
    color: var(--text-dim);
    font-size: 12px;
    white-space: nowrap;
  }
  .tw-fill {
    flex: 1;
  }
  /* the end button where the wide-view button was (the strip has no padding) */
  .tw-bar.wide {
    padding-right: 0;
  }
  .tw-bar .btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .side {
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  /* a phone: over the content from the left, the rest dimmed */
  .side.drawer {
    position: fixed;
    inset: 0 auto 0 0;
    width: min(86vw, 360px);
    z-index: 40;
    background: var(--bg-panel);
    box-shadow: 0 0 24px rgba(0, 0, 0, 0.28);
  }
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 39;
    background: rgba(0, 0, 0, 0.28);
  }
  .gone {
    display: none;
  }
  .strip-btn {
    align-self: center;
    flex-shrink: 0;
    margin: 0 4px;
  }
  .vdivider {
    background: var(--border);
    cursor: col-resize;
    position: relative;
  }
  .vdivider::after {
    content: '';
    position: absolute;
    inset: 0 -3px;
  }
  main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .tab-menu {
    position: fixed;
    z-index: 100;
    display: flex;
    flex-direction: column;
    min-width: 210px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .tab-menu button {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px 5px 8px;
    border: none;
    border-radius: 4px;
    background: none;
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .tab-menu kbd {
    margin-left: auto;
    padding-left: 16px;
    color: var(--text-dim);
    font-family: var(--font-ui);
    font-size: 11px;
  }
  .tab-menu button:hover:not(:disabled) {
    background: var(--bg-select);
  }
  .tab-menu button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .tab-menu button.danger {
    color: var(--danger);
  }
  .tab-menu hr {
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
  .content {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }
  /* a group's strip and pane, placed by the split tree */
  .at,
  .pane,
  .welcome {
    position: absolute;
  }
  .pane.hidden {
    visibility: hidden;
    pointer-events: none;
  }
  /* while a line or a tab is dragged the panes under it don't take the
     pointer, and nothing gets selected on the way */
  .content.resizing .pane,
  .content.tab-drag .pane {
    pointer-events: none;
  }
  .content.tab-drag {
    user-select: none;
    -webkit-user-select: none;
  }
  .bar {
    position: absolute;
    z-index: 5;
    height: 1px;
    background: var(--border);
    cursor: row-resize;
  }
  .bar.row {
    width: 1px;
    height: auto;
    cursor: col-resize;
  }
  .bar::after {
    content: '';
    position: absolute;
    inset: -3px 0;
  }
  .bar.row::after {
    inset: 0 -3px;
  }
  .bar:hover,
  .content.resizing .bar {
    background: var(--accent);
  }
  /* where a dragged tab would go */
  .drop {
    position: absolute;
    z-index: 6;
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    border: 2px solid var(--accent);
    pointer-events: none;
  }
  .welcome {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-dim);
    text-align: center;
  }
  .welcome h2 {
    margin: 12px 0 4px;
    color: var(--text);
    font-weight: 600;
  }
  .welcome p {
    margin: 4px;
  }
  .confirm-sql {
    margin-top: 10px;
  }
  .toasts {
    position: fixed;
    right: 16px;
    bottom: 16px;
    z-index: 200;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 480px;
  }
  .toast {
    padding: 9px 14px;
    border-radius: 6px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.3);
    word-break: break-word;
    user-select: text;
    -webkit-user-select: text;
  }
  .toast.act {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 8px 7px 14px;
  }
  .toast.act span {
    flex: 1;
  }
  .toast.error {
    border-color: var(--danger);
    color: var(--danger);
  }
</style>
