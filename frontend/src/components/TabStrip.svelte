<script lang="ts">
  // One tab group's strip (or, on a phone, the strip of all tabs). Dragging
  // is App's: a tab can go to another strip or split a group.
  import type { Snippet } from 'svelte'
  import { t as tr } from '../lib/i18n.svelte'
  import { isWeb } from '../lib/platform'
  import { app, connById, pinTab, saveWorkspaceSoon, type Tab } from '../lib/state.svelte'
  import { driverLabel } from '../lib/types'
  import Icon from './Icon.svelte'

  let {
    gid,
    tabs,
    active,
    focused,
    renaming = $bindable(),
    held,
    ondown,
    onmenu,
    onclose,
    onnew,
    onwide,
    lead,
  }: {
    gid: string // '' = all tabs (a phone)
    tabs: Tab[]
    active: string
    focused: boolean // the group with the active tab (when there are several)
    renaming: string | null
    held: () => boolean // a finger is dragging a tab: the strip doesn't scroll
    ondown: (e: PointerEvent, id: string) => void
    onmenu: (e: MouseEvent, id: string) => void
    onclose: (id: string) => void
    onnew?: () => void
    onwide?: () => void // 넓게 보기 for this group's tab
    lead?: Snippet
  } = $props()

  let tabsEl = $state<HTMLDivElement>() as HTMLDivElement

  // The active tab is always scrolled into view (a new tab opens at the end).
  $effect(() => {
    const id = active
    void tabs.length
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
    void tabs.length
    requestAnimationFrame(tabsEdges)
    const ro = new ResizeObserver(tabsEdges)
    if (tabsEl) ro.observe(tabsEl)
    return () => ro.disconnect()
  })

  // A drag after the hold mustn't scroll the strip (or the page) instead:
  // only a touchmove that isn't passive can stop that, which Svelte's
  // ontouchmove is.
  function holdScroll(node: HTMLElement) {
    const stop = (e: TouchEvent) => held() && e.cancelable && e.preventDefault()
    node.addEventListener('touchmove', stop, { passive: false })
    return { destroy: () => node.removeEventListener('touchmove', stop) }
  }
</script>

<div class="strip" class:blur={!focused} data-gid={gid}>
  {@render lead?.()}
  <div class="tabs-wrap" class:more-left={moreLeft} class:more-right={moreRight}>
    <div class="tabs" class:edge={!lead} bind:this={tabsEl} use:holdScroll onwheel={tabsWheel} onscroll={tabsEdges}>
      {#each tabs as t (t.id)}
        {@const c = connById(t.connId)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="tab"
          data-id={t.id}
          class:on={t.id === active}
          class:dragging={app.tabDrag === t.id}
          style:--conn-color={c?.color || 'transparent'}
          onclick={() => (app.activeTab = t.id)}
          onpointerdown={(e) => ondown(e, t.id)}
          ondragstart={(e) => e.preventDefault()}
          ondblclick={() => (t.preview ? pinTab(t) : (renaming = t.id))}
          oncontextmenu={(e) => onmenu(e, t.id)}
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
          <button class="x" onclick={(e) => (e.stopPropagation(), onclose(t.id))} aria-label={tr('닫기')}><Icon name="close" size={14} /></button>
        </div>
      {/each}
      {#if onnew}
        <button class="icon-btn newtab" title={isWeb ? tr('새 콘솔') : tr('새 콘솔') + ' (Ctrl/⌘+T)'} onclick={onnew}>
          <Icon name="plus" size={14} />
        </button>
      {/if}
    </div>
  </div>
  {#if onwide}
    <button class="icon-btn strip-btn" title={tr('넓게 보기 — 이 탭만 창 가득 보입니다')} onclick={onwide}>
      <Icon name="wide" />
    </button>
  {/if}
</div>

<style>
  .strip {
    display: flex;
    align-items: stretch;
    height: 34px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }
  .strip :global(.strip-btn),
  .strip-btn {
    align-self: center;
    flex-shrink: 0;
    margin: 0 4px;
  }
  .tabs-wrap {
    position: relative;
    flex: 1;
    min-width: 0;
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
  /* no button before it: the line between groups is the left edge */
  .tabs.edge {
    border-left: none;
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
  /* another group's shown tab: marked, but not as the one being worked in */
  .strip.blur .tab.on {
    background: var(--bg);
    box-shadow: inset 0 -2px 0 var(--text-faint), inset 0 3px 0 var(--conn-color);
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
