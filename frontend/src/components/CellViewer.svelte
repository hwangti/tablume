<script lang="ts">
  import { t, num } from '../lib/i18n.svelte'
  import type { Cell } from '../lib/types'
  import { copyText } from '../lib/clipboard'
  import Modal from './Modal.svelte'
  import TextEditor from './TextEditor.svelte'
  import { untrack } from 'svelte'

  let {
    column,
    value,
    editable = false,
    nullable = true,
    onsave,
    onclose,
  }: {
    column: string
    value: Cell
    editable?: boolean
    nullable?: boolean // offer "NULL로 설정" (not for a comment)
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

<Modal title={column + (editable ? ' — ' + t('편집') : '')} {onclose} width={720} dismissible={!editable} onsubmit={editable ? save : undefined}>
  {#if isNull && !editable}
    <div class="null">NULL</div>
  {:else if editable}
    <TextEditor value={text} onchange={(v) => ((text = v), (isNull = false))} placeholder={isNull ? 'NULL' : ''} onsubmit={save} />
  {:else}
    <TextEditor value={shown} readonly />
  {/if}
  {#snippet footer()}
    <span class="dim">{isNull ? 'NULL' : t('{n}자', { n: num(text.length) })}</span>
    <span class="spacer"></span>
    {#if json && !editable}
      <label><input type="checkbox" bind:checked={pretty} /> {t('JSON 정렬')}</label>
    {/if}
    {#if editable}
      {#if json && json !== text}<button class="btn" onclick={() => (text = json!)}>{t('JSON 정렬')}</button>{/if}
      {#if nullable}<button class="btn" onclick={() => ((isNull = true), (text = ''))}>{t('NULL로 설정')}</button>{/if}
      <button class="btn" onclick={onclose}>{t('취소')}</button>
      <button class="btn primary" onclick={save} title="Ctrl+Enter / Cmd+Enter">{t('적용')}</button>
    {:else}
      <button class="btn" onclick={() => copyText(shown)} disabled={isNull}>{t('복사')}</button>
      <button class="btn primary" onclick={onclose}>{t('닫기')}</button>
    {/if}
  {/snippet}
</Modal>

<style>
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
