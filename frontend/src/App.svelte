<script lang="ts">
  import { onMount } from 'svelte'
  import { installTooltips } from './lib/tooltip'
  import Sidebar from './components/Sidebar.svelte'
  import QueryTab from './components/QueryTab.svelte'
  import ConnectionDialog from './components/ConnectionDialog.svelte'
  import Modal from './components/Modal.svelte'
  import Icon from './components/Icon.svelte'
  import logo from './assets/logo.svg'
  import { app, loadConnections, loadWorkspace, openConsole, closeTab, closeTabs, duplicateTab, connById, activeTab, saveWorkspaceSoon, pinTab } from './lib/state.svelte'
  import { driverLabel } from './lib/types'

  let sideW = $state(270)
  let password = $state('')
  let ready = $state(false)

  onMount(async () => {
    installTooltips()
    await loadConnections()
    await loadWorkspace()
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
    if (!mod || app.editing || app.prompt) return
    const k = e.key.toLowerCase()
    if (k === 't') {
      e.preventDefault()
      const connId = activeTab()?.connId ?? app.connections[0]?.id
      if (connId) openConsole(connId)
    } else if (k === 'w' && app.activeTab) {
      e.preventDefault()
      closeTab(app.activeTab)
    } else if (/^[1-9]$/.test(k) && !e.shiftKey) {
      const t = app.tabs[Number(k) - 1]
      if (t) {
        e.preventDefault()
        app.activeTab = t.id
      }
    }
  }

  // Next / previous tab: Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PageDown / PageUp,
  // and on macOS also ⌘⇧] / ⌘⇧[. Caught before the editor or the grid
  // (where Tab moves between cells) see the key.
  function switchKey(e: KeyboardEvent) {
    if (app.editing || app.prompt || app.tabs.length < 2) return
    let dir = 0
    if (e.ctrlKey && e.key === 'Tab') dir = e.shiftKey ? -1 : 1
    else if (e.ctrlKey && !e.shiftKey && (e.key === 'PageDown' || e.key === 'PageUp')) dir = e.key === 'PageDown' ? 1 : -1
    else if (e.metaKey && e.shiftKey && (e.code === 'BracketRight' || e.code === 'BracketLeft')) dir = e.code === 'BracketRight' ? 1 : -1
    if (!dir) return
    e.preventDefault()
    e.stopPropagation()
    const i = app.tabs.findIndex((t) => t.id === app.activeTab)
    const n = app.tabs.length
    app.activeTab = app.tabs[(i + dir + n) % n].id
  }

  function submitPassword() {
    app.prompt?.resolve(password)
    password = ''
  }

  // Tabs reorder by dragging; the array is reordered live while moving.
  let dragging = $state<string | null>(null)
  let tabsEl: HTMLDivElement

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
  type TabMenuItem = { label: string; icon?: string; run: () => void; disabled?: boolean; sep?: boolean; danger?: boolean }
  let tabMenu = $state<{ x: number; y: number; items: TabMenuItem[] } | null>(null)
  let tabMenuEl = $state<HTMLDivElement>()
  // keep the menu inside the window
  $effect(() => {
    if (!tabMenu || !tabMenuEl) return
    const r = tabMenuEl.getBoundingClientRect()
    if (r.right > innerWidth - 4) tabMenuEl.style.left = Math.max(4, innerWidth - r.width - 4) + 'px'
    if (r.bottom > innerHeight - 4) tabMenuEl.style.top = Math.max(4, innerHeight - r.height - 4) + 'px'
  })

  function openTabMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    const i = app.tabs.findIndex((t) => t.id === id)
    const t = app.tabs[i]
    if (!t) return
    const ids = app.tabs.map((x) => x.id)
    const left = ids.slice(0, i)
    const right = ids.slice(i + 1)
    const others = ids.filter((x) => x !== id)
    const sameConn = app.tabs.filter((x) => x.connId === t.connId).map((x) => x.id)
    const c = connById(t.connId)
    tabMenu = {
      x: e.clientX,
      y: e.clientY,
      items: [
        { label: '닫기', icon: 'close', run: () => closeTab(id) },
        { label: '다른 탭 닫기', run: () => closeTabs(others), disabled: !others.length },
        { label: '왼쪽 탭 닫기', run: () => closeTabs(left), disabled: !left.length },
        { label: '오른쪽 탭 닫기', run: () => closeTabs(right), disabled: !right.length },
        { label: `같은 연결 탭 닫기${c ? ` (${c.name})` : ''}`, icon: 'db', run: () => closeTabs(sameConn) },
        { label: '모든 탭 닫기', run: () => closeTabs(ids), danger: true },
        { label: '탭 복제', icon: 'copy', run: () => duplicateTab(id), sep: true },
        ...(t.preview ? [{ label: '탭 고정', icon: 'lock', run: () => pinTab(t) }] : []),
        { label: '이름 바꾸기', icon: 'edit', run: () => (renaming = id) },
      ],
    }
  }

  function tabMouseDown(e: MouseEvent, id: string) {
    if (e.button === 1) {
      e.preventDefault()
      closeTab(id)
      return
    }
    if (e.button !== 0 || (e.target as HTMLElement).closest('button, input')) return
    const x0 = e.clientX
    const move = (ev: MouseEvent) => {
      if (!dragging && Math.abs(ev.clientX - x0) < 5) return
      dragging = id
      const els = [...tabsEl.querySelectorAll<HTMLElement>('.tab')]
      const from = app.tabs.findIndex((t) => t.id === id)
      let to = els.findIndex((el) => {
        const r = el.getBoundingClientRect()
        return ev.clientX < r.left + r.width / 2
      })
      if (to < 0) to = els.length - 1
      else if (to > from) to -= 1
      if (to !== from && to >= 0) {
        const [t] = app.tabs.splice(from, 1)
        app.tabs.splice(to, 0, t)
      }
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      if (dragging) saveWorkspaceSoon()
      dragging = null
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  let renaming = $state<string | null>(null)
</script>

<svelte:window onkeydown={keydown} onkeydowncapture={switchKey} onclick={() => (tabMenu = null)} onblur={() => (tabMenu = null)} />

<div class="layout" style:grid-template-columns="{sideW}px 1px 1fr">
  <Sidebar />
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="vdivider" onmousedown={startDrag}></div>

  <main>
    <div class="tabs-wrap" class:more-left={moreLeft} class:more-right={moreRight}>
    <div class="tabs" bind:this={tabsEl} onwheel={tabsWheel} onscroll={tabsEdges}>
      {#each app.tabs as t (t.id)}
        {@const c = connById(t.connId)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="tab"
          data-id={t.id}
          class:on={t.id === app.activeTab}
          class:dragging={dragging === t.id}
          style:--conn-color={c?.color || 'transparent'}
          onclick={() => (app.activeTab = t.id)}
          onmousedown={(e) => tabMouseDown(e, t.id)}
          ondblclick={() => (t.preview ? pinTab(t) : (renaming = t.id))}
          oncontextmenu={(e) => openTabMenu(e, t.id)}
          title="{c?.name ?? ''} · {c ? driverLabel(c.driver) : ''}"
        >
          {#if t.running}<span class="spin"></span>{:else}<Icon name={t.tableRef ? 'table' : 'console'} size={13} />{/if}
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
            <span class="name" class:preview={t.preview} title={t.preview ? '미리보기 탭 — 다른 테이블을 클릭하면 바뀝니다. 더블클릭하면 고정됩니다.' : ''}>{t.title}</span>
          {/if}
          <button class="x" onclick={(e) => (e.stopPropagation(), closeTab(t.id))} aria-label="닫기"><Icon name="close" size={12} /></button>
        </div>
      {/each}
      {#if app.connections.length}
        <button
          class="icon-btn newtab"
          title="새 콘솔 (Ctrl/⌘+T)"
          onclick={() => openConsole(activeTab()?.connId ?? app.connections[0].id)}
        >
          <Icon name="plus" size={14} />
        </button>
      {/if}
    </div>
    </div>

    <div class="content">
      {#each app.tabs as t (t.id)}
        <div class="pane" class:hidden={t.id !== app.activeTab}>
          <QueryTab tab={t} />
        </div>
      {/each}
      {#if ready && app.tabs.length === 0}
        <div class="welcome">
          <img src={logo} width="64" height="64" alt="" />
          <h2>Tablume</h2>
          {#if app.connections.length === 0}
            <p>왼쪽의 <b>+</b> 버튼으로 첫 연결을 추가하세요.</p>
          {:else}
            <p>연결을 클릭해 접속하고, 테이블을 클릭하면 데이터를 볼 수 있습니다.</p>
            <p class="dim">콘솔 아이콘이나 Ctrl/⌘+T로 새 SQL 콘솔을 엽니다.</p>
          {/if}
        </div>
      {/if}
    </div>
  </main>
  {#if tabMenu}
    <div class="tab-menu" bind:this={tabMenuEl} style:left="{tabMenu.x}px" style:top="{tabMenu.y}px">
      {#each tabMenu.items as it (it.label)}
        {#if it.sep}<hr />{/if}
        <button class:danger={it.danger} disabled={it.disabled} onclick={it.run}>
          <span class="mi">{#if it.icon}<Icon name={it.icon} size={14} />{/if}</span>{it.label}
        </button>
      {/each}
    </div>
  {/if}
</div>

{#if app.editing}
  <ConnectionDialog initial={app.editing} />
{/if}

{#if app.prompt}
  <Modal title={app.prompt.title} onclose={() => app.prompt?.resolve(null)} width={380} dismissible={false}>
    <form onsubmit={(e) => (e.preventDefault(), submitPassword())}>
      <!-- svelte-ignore a11y_autofocus -->
      <input type="password" bind:value={password} autofocus style="width:100%" placeholder="비밀번호" />
    </form>
    {#snippet footer()}
      <span style="flex:1"></span>
      <button class="btn" onclick={() => app.prompt?.resolve(null)}>취소</button>
      <button class="btn primary" onclick={submitPassword}>연결</button>
    {/snippet}
  </Modal>
{/if}

{#if app.confirm}
  <Modal title="확인" onclose={() => app.confirm?.resolve(false)} width={380}>
    <p style="margin:0">{app.confirm.text}</p>
    {#snippet footer()}
      <span style="flex:1"></span>
      <button class="btn" onclick={() => app.confirm?.resolve(false)}>취소</button>
      <button class="btn primary" onclick={() => app.confirm?.resolve(true)}>확인</button>
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
    bottom: 1px;
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
    height: 34px;
    flex-shrink: 0;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
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
    background: var(--bg);
    color: var(--text);
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
    padding: 2px;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-faint);
    cursor: pointer;
    visibility: hidden;
  }
  .tab:hover .x,
  .tab.on .x {
    visibility: visible;
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
