<script lang="ts" generics="V extends string | number">
  // A select in the app's own style: a button like an input, and a pop-up
  // list (the page-size menu's look). Long lists get a search box. The list
  // is put on <body> so a dialog's transform and overflow don't move or cut
  // it.
  import { t } from '../lib/i18n.svelte'
  import { closeMenus, fitMenu, onCloseMenus } from '../lib/fit'
  import Icon from './Icon.svelte'

  let {
    value,
    options,
    onchange,
    placeholder = '',
    mono = false,
    width = '',
    disabled = false,
    title = '',
  }: {
    value: V
    options: { value: V; label: string; hint?: string }[]
    onchange: (v: V) => void
    placeholder?: string
    mono?: boolean
    width?: string
    disabled?: boolean
    title?: string
  } = $props()

  let btn: HTMLButtonElement
  let pos = $state<{ x: number; y: number; w: number } | null>(null)
  let search = $state('')
  let hi = $state(0)
  let listEl = $state<HTMLDivElement>()
  const searchable = $derived(options.length > 12)

  let shown = $derived.by(() => {
    const q = search.trim().toLowerCase()
    return q ? options.filter((o) => o.label.toLowerCase().includes(q)) : options
  })
  let current = $derived(options.find((o) => o.value === value))

  $effect(() => onCloseMenus(() => (pos = null)))

  function open() {
    if (pos) return void (pos = null)
    closeMenus()
    const r = btn.getBoundingClientRect()
    search = ''
    hi = Math.max(0, options.findIndex((o) => o.value === value))
    pos = { x: r.left, y: r.bottom + 3, w: r.width }
    requestAnimationFrame(() => listEl?.querySelector('.on')?.scrollIntoView({ block: 'nearest' }))
  }

  function pick(o: { value: V }) {
    pos = null
    btn.focus()
    if (o.value !== value) onchange(o.value)
  }

  function key(e: KeyboardEvent) {
    if (!pos) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        open()
      }
      return
    }
    e.stopPropagation() // not the dialog's Escape / Enter
    if (e.key === 'Escape') {
      e.preventDefault()
      pos = null
      btn.focus()
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      hi = Math.min(shown.length - 1, Math.max(0, hi + (e.key === 'ArrowDown' ? 1 : -1)))
      requestAnimationFrame(() => listEl?.querySelector('.hi')?.scrollIntoView({ block: 'nearest' }))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (shown[hi]) pick(shown[hi])
    }
  }

  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy: () => node.remove() }
  }

  // (after the list has moved to <body>)
  function focusSearch(node: HTMLInputElement) {
    requestAnimationFrame(() => node.focus())
  }
</script>

<svelte:window onpointerdown={(e) => pos && !btn.contains(e.target as Node) && !listEl?.contains(e.target as Node) && (pos = null)} />

<button bind:this={btn} type="button" class="dd" class:open={!!pos} style:width {disabled} {title} onclick={open} onkeydown={key}>
  <span class="val" class:ph={!current} class:mono={mono && !!current}>{current?.label ?? placeholder}</span>
  <Icon name="chevronDown" size={12} />
</button>

{#if pos}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="dd-list" bind:this={listEl} use:portal use:fitMenu={pos} style:left="{pos.x}px" style:top="{pos.y}px" style:min-width="{pos.w}px" onkeydown={key}>
    {#if searchable}
      <input class="dd-search" placeholder={t('찾기')} bind:value={search} use:focusSearch oninput={() => (hi = 0)} spellcheck="false" />
    {/if}
    <div class="dd-items" role="listbox">
      {#each shown as o, i (o.value)}
        <button type="button" role="option" aria-selected={o.value === value} class:on={o.value === value} class:hi={i === hi} class:mono onclick={() => pick(o)} onpointermove={() => (hi = i)}>
          <span>{o.label}</span>
          {#if o.hint}<span class="hint">{o.hint}</span>{/if}
        </button>
      {:else}
        <div class="none">{t('없음')}</div>
      {/each}
    </div>
  </div>
{/if}

<style>
  .dd {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 26px;
    min-width: 0;
    padding: 0 7px 0 8px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }
  .dd:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .dd:focus-visible,
  .dd.open {
    border-color: var(--accent);
    outline: none;
  }
  .dd :global(svg) {
    flex-shrink: 0;
    color: var(--text-dim);
  }
  .val {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ph {
    color: var(--text-faint);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .dd-list {
    position: fixed;
    z-index: 300;
    display: flex;
    flex-direction: column;
    max-width: 420px;
    max-height: 300px;
    padding: 4px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
    font-size: 13px;
  }
  .dd-search {
    height: 26px;
    margin-bottom: 4px;
    flex-shrink: 0;
  }
  .dd-items {
    display: flex;
    flex-direction: column;
    overflow-y: auto;
  }
  .dd-items button {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 5px 10px;
    border: none;
    border-radius: 4px;
    background: none;
    color: var(--text);
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .dd-items button.hi {
    background: var(--bg-select);
  }
  .dd-items button.on {
    color: var(--accent);
    font-weight: 600;
  }
  .hint {
    margin-left: auto;
    color: var(--text-faint);
    font-weight: normal;
    font-size: 11.5px;
  }
  .none {
    padding: 5px 10px;
    color: var(--text-faint);
  }
</style>
