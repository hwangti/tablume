<script lang="ts">
  // [−][100 %][+] [100% ▾]: a typed value with steps either side, then a
  // split button that resets to 100% or picks a preset. Used for the window
  // zoom (view options) and the ERD.
  import Icon from './Icon.svelte'
  import { t } from '../lib/i18n.svelte'
  import { fitMenu, onCloseMenus } from '../lib/fit'

  let {
    value,
    min,
    max,
    presets,
    onset,
    onstep,
    title = '',
    stepTitles = [t('축소'), t('확대')],
    extra = [],
  }: {
    value: number // 1 = 100%
    min: number
    max: number
    presets: number[]
    onset: (v: number) => void
    onstep: (dir: -1 | 1) => void
    title?: string
    stepTitles?: [string, string]
    extra?: { label: string; run: () => void }[] // listed above the presets
  } = $props()

  let root: HTMLDivElement
  let splitEl: HTMLSpanElement
  let listEl = $state<HTMLDivElement>()
  // The list goes on <body> at the window position under the button: the
  // ERD's bar scrolls sideways on a phone and would cut it off.
  let pos = $state<{ top: number; right: number } | null>(null)
  const open = $derived(!!pos)
  function toggle() {
    if (pos) return void (pos = null)
    const r = splitEl.getBoundingClientRect()
    pos = { top: r.bottom + 3, right: innerWidth - r.right }
  }
  const close = () => (pos = null)
  $effect(() => onCloseMenus(close))
  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy: () => node.remove() }
  }
  // one decimal at most: a fitted zoom like 23.68% must not change by
  // being shown and read back
  const shown = (v: number) => String(Math.round(v * 1000) / 10)

  function typed(e: Event) {
    const el = e.currentTarget as HTMLInputElement
    const v = parseFloat(el.value)
    if (Number.isFinite(v) && v > 0) onset(Math.min(max, Math.max(min, v / 100)))
    el.value = shown(value) // the clamped value, or back to it
  }
</script>

<svelte:window onpointerdown={(e) => open && !root.contains(e.target as Node) && !listEl?.contains(e.target as Node) && close()} />

<div class="zoombox" bind:this={root}>
  <span class="grp">
    <button title={stepTitles[0]} disabled={value <= min} onclick={() => onstep(-1)}><Icon name="minus" size={12} /></button>
    <label class="mid" {title}>
      <input
        size="1"
        inputmode="decimal"
        value={shown(value)}
        onfocus={(e) => e.currentTarget.select()}
        onchange={typed}
        onkeydown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur()
          if (e.key === 'Escape') ((e.currentTarget.value = shown(value)), e.currentTarget.blur())
          if (e.key === 'ArrowDown' && e.altKey && !open) toggle()
        }}
      /><span class="pct">%</span>
    </label>
    <button title={stepTitles[1]} disabled={value >= max} onclick={() => onstep(1)}><Icon name="plus" size={12} /></button>
  </span>
  <span class="grp split" bind:this={splitEl}>
    <button class="reset" onclick={() => onset(1)}>100%</button>
    <button class="drop" class:on={open} title={t('배율 고르기')} onclick={toggle}><Icon name="chevronDown" size={12} /></button>
    {#if pos}
      <!-- (its clicks stay here: a menu it sits in closes on a click on the window) -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <div class="list" role="listbox" tabindex="-1" data-menu bind:this={listEl} use:portal use:fitMenu={pos} style:top="{pos.top}px" style:right="{pos.right}px" onclick={(e) => e.stopPropagation()}>
        {#each extra as x (x.label)}
          <button class="extra" onclick={() => (x.run(), close())}>{x.label}</button>
        {/each}
        {#if extra.length}<div class="line"></div>{/if}
        {#each presets as p (p)}
          <button role="option" aria-selected={shown(p) === shown(value)} class:on={shown(p) === shown(value)} onclick={() => (onset(p), close())}
            >{shown(p)}%</button
          >
        {/each}
      </div>
    {/if}
  </span>
</div>

<style>
  .zoombox {
    display: flex;
    gap: 6px;
  }
  .grp {
    position: relative;
    display: flex;
    height: 24px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-elev);
  }
  .grp:first-child {
    flex: 1;
  }
  .grp > button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    flex-shrink: 0;
    padding: 0;
    border: none;
    background: none;
    color: var(--text-dim);
    cursor: pointer;
  }
  .grp > button:first-child {
    border-radius: 3px 0 0 3px;
  }
  .grp > button:last-child {
    border-radius: 0 3px 3px 0;
  }
  .grp > button:hover:not(:disabled),
  .grp > button.on {
    background: var(--bg-hover);
    color: var(--text);
  }
  .grp > button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .mid {
    display: flex;
    align-items: center;
    flex: 1;
    min-width: 0;
    padding-right: 7px;
    border-left: 1px solid var(--border);
    border-right: 1px solid var(--border);
    background: var(--bg-input);
    cursor: text;
  }
  .mid:focus-within {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .mid input {
    flex: 1;
    min-width: 0;
    width: 44px;
    height: 100%;
    padding: 0 2px 0 0;
    border: none;
    border-radius: 0;
    background: none;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .mid input:focus {
    outline: none;
  }
  .pct {
    color: var(--text-faint);
  }
  .split .reset {
    width: auto;
    padding: 0 8px;
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .split .drop {
    width: 22px;
    border-left: 1px solid var(--border);
  }
  .list {
    position: fixed;
    z-index: 300;
    display: flex;
    flex-direction: column;
    min-width: 72px;
    font-size: 12px;
    padding: 3px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 4px;
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.3);
  }
  .list button {
    padding: 3px 8px;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text);
    font-variant-numeric: tabular-nums;
    text-align: right;
    cursor: pointer;
  }
  .list .extra {
    text-align: left;
    white-space: nowrap;
  }
  .list .line {
    margin: 3px 0;
    border-top: 1px solid var(--border);
  }
  .list button:hover {
    background: var(--bg-hover);
  }
  .list button.on {
    background: var(--bg-select);
  }
</style>
