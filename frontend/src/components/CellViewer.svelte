<script lang="ts">
  import type { Cell } from '../lib/types'
  import { copyText } from '../lib/clipboard'
  import Modal from './Modal.svelte'

  let { column, value, onclose }: { column: string; value: Cell; onclose: () => void } = $props()

  let pretty = $state(true)
  let json = $derived.by(() => {
    if (value === null) return null
    const s = value.trim()
    if (!(s.startsWith('{') || s.startsWith('['))) return null
    try {
      return JSON.stringify(JSON.parse(s), null, 2)
    } catch {
      return null
    }
  })
  let shown = $derived(value === null ? '' : json && pretty ? json : value)
</script>

<Modal title={column} {onclose} width={720}>
  {#if value === null}
    <div class="null">NULL</div>
  {:else}
    <textarea class="mono" readonly value={shown}></textarea>
  {/if}
  {#snippet footer()}
    <span class="dim">{value === null ? '' : `${value.length.toLocaleString()}자`}</span>
    <span class="spacer"></span>
    {#if json}
      <label><input type="checkbox" bind:checked={pretty} /> JSON 정렬</label>
    {/if}
    <button class="btn" onclick={() => copyText(shown)} disabled={value === null}>복사</button>
    <button class="btn primary" onclick={onclose}>닫기</button>
  {/snippet}
</Modal>

<style>
  textarea {
    width: 100%;
    height: 420px;
    resize: vertical;
    font-size: 12.5px;
    line-height: 1.5;
    white-space: pre;
  }
  .null {
    padding: 40px;
    text-align: center;
    color: var(--null);
    font-style: italic;
  }
  .spacer {
    flex: 1;
  }
  label {
    display: flex;
    align-items: center;
    gap: 4px;
  }
</style>
