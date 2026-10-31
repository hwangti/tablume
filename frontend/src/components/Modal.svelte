<script lang="ts">
  import type { Snippet } from 'svelte'

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
  <div class="modal" style:width="{width}px" role="dialog" aria-label={title}>
    <header>
      <span class="title">{title}</span>
      <button class="icon-btn" onclick={onclose} aria-label="닫기">✕</button>
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
