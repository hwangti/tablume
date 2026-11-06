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
      const t = app.tabs.filter((x) => !x.detached)[Number(k) - 1]
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
    const tabs = app.tabs.filter((t) => !t.detached)
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

  // Tabs reorder by dragging; the array is reordered live while moving.
  let dragging = $state<string | null>(null)
  let tabsEl = $state<HTMLDivElement>() as HTMLDivElement

  // The active tab is always scrolled into view (a new tab opens at the end).
  $effect(() => {
    const id = app.activeTab
    void app.tabs.length
    requestAnimationFrame(() => {
      const el = tabsEl?.querySelector<HTMLElement>(`.tab[data-id="${CSS.escape(id)}"]`)
      el?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
    })
  })

  // No scrollbar on the tab strip (it took height): the wheel scrolls it,
  // and a shadow on either side shows there are more tabs that way.
  function tabsWheel(e: WheelEvent) {
    if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
      tabsEl.scrollLeft += e.deltaY
      e.preventDefault()
    }
  }

  let moreLeft = $state(false)
  let moreRight = $state(false)
  function tabsEdges() {
    if (!tabsEl) return
    moreLeft = tabsEl.scrollLeft > 1
    moreRight = tabsEl.scrollLeft + tabsEl.clientWidth < tabsEl.scrollWidth - 1
  }
  $effect(() => {
    void app.tabs.length
    requestAnimationFrame(tabsEdges)
    const ro = new ResizeObserver(tabsEdges)
    if (tabsEl) ro.observe(tabsEl)
    return () => ro.disconnect()
  })

  // ---------------------------------------------------------------- tab menu
  type TabMenuItem = { label: string; icon?: string; run: () => void; disabled?: boolean; sep?: boolean; danger?: boolean; kbd?: string }
  let tabMenu = $state<{ x: number; y: number; items: TabMenuItem[] } | null>(null)
  $effect(() => onCloseMenus(() => (tabMenu = null)))

  function openTabMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    const i = app.tabs.findIndex((t) => t.id === id)
    const t = app.tabs[i]
    if (!t) return
    // Bulk closes leave tabs in their own windows alone.
    const ids = app.tabs.map((x) => x.id)
    const here = (x: string) => !app.tabs.find((y) => y.id === x)?.detached
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
        ...(isWeb ? [] : [{ label: tr('새 창으로 열기'), icon: 'detach', run: () => detachTab(id) }]),
        { label: tr('탭 복제'), icon: 'copy', run: () => duplicateTab(id) },
        ...(t.preview ? [{ label: tr('탭 고정'), icon: 'lock', run: () => pinTab(t) }] : []),
        { label: tr('이름 바꾸기'), icon: 'edit', run: () => (renaming = id) },
      ],
    }
  }

  // A finger drags a tab after holding it (the menu that opened then
  // closes); before that, a swipe scrolls the strip.
  const TAB_HOLD_MS = 450
  let tabHeld = false // a finger is dragging: the strip doesn't scroll

  function tabDown(e: PointerEvent, id: string) {
    if (e.button === 1) {
      e.preventDefault()
      closeTab(id)
      return
    }
    if (e.button !== 0 || (e.target as HTMLElement).closest('button, input')) return
    const x0 = e.clientX
    const touch = e.pointerType !== 'mouse'
    // held: from here a move drags (the first touchmove too)
    const hold = touch ? setTimeout(() => (tabHeld = true), TAB_HOLD_MS) : 0
    const move = (ev: PointerEvent) => {
      if (ev.pointerId !== e.pointerId) return
      if (!dragging) {
        if (Math.abs(ev.clientX - x0) < 5) return
        if (touch) {
          if (!tabHeld) return up() // a swipe
          closeMenus()
        }
      }
      dragging = id
      // Positions among the shown tabs; tabs in their own windows aren't
      // in the strip and keep their place in app.tabs.
      const els = [...tabsEl.querySelectorAll<HTMLElement>('.tab')]
      const shown = els.map((el) => el.dataset.id!)
      const from = shown.indexOf(id)
      let to = els.findIndex((el) => {
        const r = el.getBoundingClientRect()
        return ev.clientX < r.left + r.width / 2
      })
      if (to < 0) to = els.length - 1
      else if (to > from) to -= 1
      if (from >= 0 && to !== from && to >= 0) {
        shown.splice(from, 1)
        shown.splice(to, 0, id)
        const byId = new Map(app.tabs.map((t) => [t.id, t]))
        let k = 0
        app.tabs = app.tabs.map((t) => (t.detached ? t : byId.get(shown[k++])!))
      }
    }
    const up = () => {
      clearTimeout(hold)
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      if (dragging) saveWorkspaceSoon()
      dragging = null
      tabHeld = false
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
  }

  // A drag after the hold mustn't scroll the strip (or the page) instead:
  // only a touchmove that isn't passive can stop that, which Svelte's
  // ontouchmove is.
  function holdScroll(node: HTMLElement) {
    const stop = (e: TouchEvent) => tabHeld && e.cancelable && e.preventDefault()
    node.addEventListener('touchmove', stop, { passive: false })
    return { destroy: () => node.removeEventListener('touchmove', stop) }
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
    {:else}
    <div class="strip">
    <button
      class="icon-btn strip-btn"
      title="{tr(sideHidden ? '연결 목록 보이기' : '연결 목록 숨기기')} ({modKey}B)"
      onclick={sideToggle}
    >
      <Icon name={sideHidden ? 'sidebarOff' : 'sidebar'} />
    </button>
    <div class="tabs-wrap" class:more-left={moreLeft} class:more-right={moreRight}>
    <div class="tabs" bind:this={tabsEl} use:holdScroll onwheel={tabsWheel} onscroll={tabsEdges}>
      {#each app.tabs.filter((x) => !x.detached) as t (t.id)}
        {@const c = connById(t.connId)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="tab"
          data-id={t.id}
          class:on={t.id === app.activeTab}
          class:dragging={dragging === t.id}
          style:--conn-color={c?.color || 'transparent'}
          onclick={() => (app.activeTab = t.id)}
          onpointerdown={(e) => tabDown(e, t.id)}
          ondblclick={() => (t.preview ? pinTab(t) : (renaming = t.id))}
          oncontextmenu={(e) => openTabMenu(e, t.id)}
          title="{c?.name ?? ''} · {c ? driverLabel(c.driver) : ''}"
        >
          {#if t.running}<span class="spin"></span>{:else}<Icon name={t.erd ? 'erd' : t.tableRef ? 'table' : 'console'} size={13} />{/if}
          {#if renaming === t.id}
            <!-- svelte-ignore a11y_autofocus -->
            <input
              class="rename"
              autofocus
              bind:value={t.title}
              onblur={() => ((renaming = null), saveWorkspaceSoon())}
              onkeydown={(e) => (e.key === 'Enter' || e.key === 'Escape') && e.currentTarget.blur()}
            />
          {:else}
            <span class="name" class:preview={t.preview} title={t.preview ? tr('미리보기 탭 — 다른 테이블을 클릭하면 바뀝니다. 더블클릭하면 고정됩니다.') : ''}>{t.title}</span>
          {/if}
          {#if c?.readOnly}<span class="ro" title={tr('읽기 전용 연결입니다')}><Icon name="lock" size={11} /></span>{/if}
          {#if t.inTx}<span class="tx" title={tr('커밋하지 않은 변경이 있습니다')}></span>{/if}
          <button class="x" onclick={(e) => (e.stopPropagation(), closeTab(t.id))} aria-label={tr('닫기')}><Icon name="close" size={14} /></button>
        </div>
      {/each}
      {#if app.connections.length}
        <button
          class="icon-btn newtab"
          title={isWeb ? tr('새 콘솔') : tr('새 콘솔') + ' (Ctrl/⌘+T)'}
          onclick={() => openConsole(activeTab()?.connId ?? app.connections[0].id)}
        >
          <Icon name="plus" size={14} />
        </button>
      {/if}
    </div>
    </div>
    {#if activeTab()}
      <button class="icon-btn strip-btn" title={tr('넓게 보기 — 이 탭만 창 가득 보입니다')} onclick={() => (app.wide = true)}>
        <Icon name="wide" />
      </button>
    {/if}
    </div>
    {/if}

    <div class="content">
      {#each app.tabs as t (t.id)}
        {#if !t.detached}
          <div class="pane" class:hidden={t.id !== app.activeTab}>
            {#if t.erd}<ErdView tab={t} />{:else}<QueryTab tab={t} />{/if}
          </div>
        {/if}
      {/each}
      {#if ready && !app.tabs.some((t) => !t.detached)}
        <div class="welcome">
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
        <!-- svelte-ignore a11y_autofocus -->
        <input type="password" bind:value={password} autofocus style="width:100%" placeholder={tr('비밀번호')} />
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
    <div class="toast" class:error={t.kind === 'error'}>{t.text}</div>
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
  .strip {
    display: flex;
    align-items: stretch;
    height: 34px;
    flex-shrink: 0;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }
  .strip-btn {
    align-self: center;
    flex-shrink: 0;
    margin: 0 4px;
  }
  .strip .tabs-wrap {
    flex: 1;
    min-width: 0;
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
  .tabs-wrap {
    position: relative;
    flex-shrink: 0;
  }
  .tabs-wrap::before,
  .tabs-wrap::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    z-index: 2;
    width: 18px;
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .tabs-wrap::before {
    left: 0;
    background: linear-gradient(to right, rgba(0, 0, 0, 0.3), transparent);
  }
  .tabs-wrap::after {
    right: 0;
    background: linear-gradient(to left, rgba(0, 0, 0, 0.3), transparent);
  }
  .tabs-wrap.more-left::before,
  .tabs-wrap.more-right::after {
    opacity: 1;
  }
  .tabs {
    display: flex;
    align-items: stretch;
    height: 33px;
    flex-shrink: 0;
    border-left: 1px solid var(--border);
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar {
    display: none;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    max-width: 220px;
    padding: 0 6px 0 12px;
    border-right: 1px solid var(--border);
    color: var(--text-dim);
    cursor: default;
    white-space: nowrap;
    box-shadow: inset 0 -2px 0 transparent, inset 0 3px 0 var(--conn-color);
  }
  .tab.on {
    background: color-mix(in srgb, var(--accent) 14%, var(--bg));
    color: var(--text);
    font-weight: 500;
    box-shadow: inset 0 -2px 0 var(--accent), inset 0 3px 0 var(--conn-color);
  }
  .tab.dragging {
    opacity: 0.6;
    background: var(--bg-hover);
  }
  .tab .name {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tab .name.preview {
    font-style: italic;
  }
  .rename {
    width: 120px;
    padding: 1px 4px;
  }
  .x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-faint);
    cursor: pointer;
    visibility: hidden;
  }
  .tab.on .x {
    visibility: visible;
  }
  /* not on touch: a tap that reveals something is taken as a hover, not a tap */
  @media (hover: hover) {
    .tab:hover .x {
      visibility: visible;
    }
  }
  .x:hover {
    background: var(--bg-hover);
    color: var(--text);
  }
  .newtab {
    align-self: center;
    margin-left: 4px;
  }
  .content {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }
  .pane {
    position: absolute;
    inset: 0;
  }
  .pane.hidden {
    visibility: hidden;
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
  .tab .tx {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--warn);
    flex-shrink: 0;
  }
  .tab .ro {
    display: inline-flex;
    color: var(--text-faint);
    flex-shrink: 0;
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
  .toast.error {
    border-color: var(--danger);
    color: var(--danger);
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
