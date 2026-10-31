<script lang="ts">
  import type { Cell } from '../lib/types'
  import { copyText } from '../lib/clipboard'
  import Modal from './Modal.svelte'
  import { untrack } from 'svelte'

  let {
    column,
    value,
    editable = false,
    onsave,
    onclose,
  }: {
    column: string
    value: Cell
    editable?: boolean
    onsave?: (v: Cell) => void
    onclose: () => void
  } = $props()

  // Editing works on a copy; nothing changes until "적용".
  let text = $state(untrack(() => value ?? ''))
  let isNull = $state(untrack(() => value === null))
  let pretty = $state(!untrack(() => editable))

  function prettyJson(s: string): string | null {
    const t = s.trim()
    if (!(t.startsWith('{') || t.startsWith('['))) return null
    try {
      return JSON.stringify(JSON.parse(t), null, 2)
    } catch {
      return null
    }
  }

  let json = $derived(prettyJson(text))
  let shown = $derived(!editable && pretty && json ? json : text)

  function save() {
    onsave?.(isNull ? null : text)
    onclose()
  }
</script>

<Modal title={column + (editable ? ' — 편집' : '')} {onclose} width={720} dismissible={!editable}>
  {#if isNull && !editable}
    <div class="null">NULL</div>
  {:else if editable}
    <textarea class="mono" bind:value={text} oninput={() => (isNull = false)} placeholder={isNull ? 'NULL' : ''}></textarea>
  {:else}
    <textarea class="mono" readonly value={shown}></textarea>
  {/if}
  {#snippet footer()}
    <span class="dim">{isNull ? 'NULL' : `${text.length.toLocaleString()}자`}</span>
    <span class="spacer"></span>
    {#if json && !editable}
      <label><input type="checkbox" bind:checked={pretty} /> JSON 정렬</label>
    {/if}
    {#if editable}
      {#if json && json !== text}<button class="btn" onclick={() => (text = json!)}>JSON 정렬</button>{/if}
      <button class="btn" onclick={() => ((isNull = true), (text = ''))}>NULL로 설정</button>
      <button class="btn" onclick={onclose}>취소</button>
      <button class="btn primary" onclick={save}>적용</button>
    {:else}
      <button class="btn" onclick={() => copyText(shown)} disabled={isNull}>복사</button>
      <button class="btn primary" onclick={onclose}>닫기</button>
    {/if}
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
