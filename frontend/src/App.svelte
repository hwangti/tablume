<script lang="ts">
  import { onMount } from 'svelte'
  import Sidebar from './components/Sidebar.svelte'
  import QueryTab from './components/QueryTab.svelte'
  import ConnectionDialog from './components/ConnectionDialog.svelte'
  import Modal from './components/Modal.svelte'
  import Icon from './components/Icon.svelte'
  import logo from './assets/logo.svg'
  import { app, loadConnections, loadWorkspace, openConsole, closeTab, connById, activeTab, saveWorkspaceSoon, pinTab } from './lib/state.svelte'
  import { driverLabel } from './lib/types'

  let sideW = $state(270)
  let password = $state('')
  let ready = $state(false)

  onMount(async () => {
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

  function submitPassword() {
    app.prompt?.resolve(password)
    password = ''
  }

  function tabMouseDown(e: MouseEvent, id: string) {
    if (e.button === 1) {
      e.preventDefault()
      closeTab(id)
    }
  }

  let renaming = $state<string | null>(null)
</script>

<svelte:window onkeydown={keydown} />

<div class="layout" style:grid-template-columns="{sideW}px 1px 1fr">
  <Sidebar />
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="vdivider" onmousedown={startDrag}></div>

  <main>
    <div class="tabs">
      {#each app.tabs as t (t.id)}
        {@const c = connById(t.connId)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="tab"
          class:on={t.id === app.activeTab}
          style:--conn-color={c?.color || 'transparent'}
          onclick={() => (app.activeTab = t.id)}
          onmousedown={(e) => tabMouseDown(e, t.id)}
          ondblclick={() => (t.preview ? pinTab(t) : (renaming = t.id))}
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
  .tabs {
    display: flex;
    align-items: stretch;
    height: 34px;
    flex-shrink: 0;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    overflow-y: hidden;
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
