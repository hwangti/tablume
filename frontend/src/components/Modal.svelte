<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from './Icon.svelte'

  let {
    title,
    onclose,
    width = 520,
    dismissible = true,
    children,
    footer,
  }: {
    title: string
    onclose: () => void
    width?: number
    // false for dialogs with user input: a stray click outside must not discard it
    dismissible?: boolean
    children: Snippet
    footer?: Snippet
  } = $props()

  // Dragging by the header moves the dialog; it stays reachable on screen.
  let dx = $state(0)
  let dy = $state(0)
  let box: HTMLDivElement

  function dragStart(e: MouseEvent) {
    if (e.button !== 0 || (e.target as HTMLElement).closest('button')) return
    e.preventDefault()
    const x0 = e.clientX - dx
    const y0 = e.clientY - dy
    const r = box.getBoundingClientRect()
    const baseLeft = r.left - dx
    const baseTop = r.top - dy
    const move = (ev: MouseEvent) => {
      dx = Math.min(window.innerWidth - baseLeft - 80, Math.max(-baseLeft - r.width + 80, ev.clientX - x0))
      dy = Math.min(window.innerHeight - baseTop - 40, Math.max(-baseTop, ev.clientY - y0))
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onclose()
    }
  }
</script>

<svelte:window onkeydown={keydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onmousedown={(e) => dismissible && e.target === e.currentTarget && onclose()}>
  <div class="modal" bind:this={box} style:width="{width}px" style:transform="translate({dx}px, {dy}px)" role="dialog" aria-label={title}>
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <header onmousedown={dragStart} ondblclick={() => ((dx = 0), (dy = 0))} title="드래그해서 이동 · 더블클릭하면 가운데로">
      <span class="title">{title}</span>
      <button class="x-btn" onclick={onclose} onmousedown={(e) => e.stopPropagation()} aria-label="닫기"><Icon name="close" size={14} /></button>
    </header>
    <div class="body">{@render children()}</div>
    {#if footer}
      <footer>{@render footer()}</footer>
    {/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
  }
  .modal {
    max-width: calc(100vw - 40px);
    max-height: calc(100vh - 40px);
    display: flex;
    flex-direction: column;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 12px 48px rgba(0, 0, 0, 0.45);
  }
  header {
    display: flex;
    align-items: center;
    cursor: move;
    padding: 10px 10px 10px 16px;
    border-bottom: 1px solid var(--border);
  }
  .title {
    flex: 1;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .body {
    padding: 16px;
    overflow: auto;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 16px;
    border-top: 1px solid var(--border);
  }
</style>
