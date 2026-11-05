<script lang="ts">
  // One column in detail (the structure view's ✎): name, type, NULL,
  // default, auto increment and a comment over several lines. Saving puts
  // the values into the view's pending changes; nothing runs here.
  import { t } from '../lib/i18n.svelte'
  import { untrack } from 'svelte'
  import Modal from './Modal.svelte'
  import Dropdown from './Dropdown.svelte'

  export type ColumnForm = { name: string; type: string; nullable: boolean; default: string; comment: string; auto: boolean; collation: string; onUpdate: string; invisible: boolean }

  let {
    title,
    value,
    autoReason = '',
    info = [],
    collations = null,
    onsave,
    onclose,
  }: {
    title: string
    value: ColumnForm
    autoReason?: string // why auto increment can't change here ('' = it can)
    info?: [string, string][] // read-only facts: keys, reference, extra
    collations?: string[] | null // MySQL/MariaDB: the server's (shows collation and ON UPDATE)
    onsave: (v: ColumnForm) => void
    onclose: () => void
  } = $props()

  let f = $state(untrack(() => ({ ...value })))
  // a collation only means something for text
  const textType = $derived(/char|text|enum|set/i.test(f.type))
  let err = $derived(!f.name.trim() ? t('이름을 넣으세요') : !f.type.trim() ? t('타입을 넣으세요') : '')

  // numbering itself: NOT NULL and no default, in every database
  function setAuto(on: boolean) {
    f.auto = on
    if (on) {
      f.nullable = false
      f.default = ''
    }
  }

  function save() {
    if (err) return
    onsave({ ...f, name: f.name.trim(), type: f.type.trim(), default: f.default.trim(), onUpdate: f.onUpdate.trim() })
    onclose()
  }

  const stop = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && (e.target as HTMLElement).tagName === 'INPUT') (e.preventDefault(), save())
  }
</script>

<Modal {title} {onclose} width={560} dismissible={false} onsubmit={save}>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="form" onkeydown={stop}>
    <label class="row"><span class="lbl">{t('이름')}</span><input class="mono" bind:value={f.name} spellcheck="false" /></label>
    <label class="row"><span class="lbl">{t('타입')}</span><input class="mono" bind:value={f.type} spellcheck="false" placeholder="varchar(50)" /></label>
    <div class="row">
      <span class="lbl"></span>
      <label class="chk"><input type="checkbox" bind:checked={f.nullable} disabled={f.auto} /> {t('NULL 허용')}</label>
      <label class="chk" title={autoReason}>
        <input type="checkbox" checked={f.auto} disabled={!!autoReason} onchange={(e) => setAuto(e.currentTarget.checked)} />
        {t('자동 증가')}
      </label>
      {#if collations}
        <label class="chk" title={t('SELECT *에서 빠지는 컬럼 (MySQL 8.0.23+, MariaDB 10.3+)')}><input type="checkbox" bind:checked={f.invisible} /> {t('숨김 (INVISIBLE)')}</label>
      {/if}
    </div>
    {#if autoReason}<div class="row"><span class="lbl"></span><span class="note">{autoReason}</span></div>{/if}
    <label class="row"><span class="lbl">{t('기본값')}</span><input class="mono" bind:value={f.default} disabled={f.auto} spellcheck="false" placeholder={t("SQL 식 (예: 0, 'a', CURRENT_TIMESTAMP)")} /></label>
    {#if collations}
      <label class="row"><span class="lbl">ON UPDATE</span><input class="mono" bind:value={f.onUpdate} spellcheck="false" placeholder="CURRENT_TIMESTAMP" /></label>
      {#if textType || f.collation}
        <div class="row">
          <span class="lbl">{t('콜레이션')}</span>
          <Dropdown value={f.collation} options={collations.map((c) => ({ value: c, label: c }))} onchange={(v) => (f.collation = v)} placeholder={t('기본값')} mono width="100%" />
        </div>
      {/if}
    {/if}
    <label class="row top"><span class="lbl">{t('코멘트')}</span><textarea rows="5" bind:value={f.comment}></textarea></label>
    {#each info as [k, v] (k)}
      <div class="row"><span class="lbl">{k}</span><span class="mono dim">{v}</span></div>
    {/each}
  </div>
  {#snippet footer()}
    <span class="ferr">{err}</span>
    <span style="flex:1"></span>
    <button class="btn" onclick={onclose}>{t('취소')}</button>
    <button class="btn primary" onclick={save} disabled={!!err}>{t('확인')}</button>
  {/snippet}
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .row.top {
    align-items: flex-start;
  }
  .lbl {
    width: 76px;
    flex-shrink: 0;
    color: var(--text-dim);
  }
  .row.top .lbl {
    padding-top: 5px;
  }
  .row input:not([type='checkbox']),
  textarea {
    flex: 1;
    min-width: 0;
  }
  .row input:not([type='checkbox']) {
    height: 26px;
  }
  textarea {
    resize: vertical;
    line-height: 1.5;
  }
  .chk {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: 14px;
  }
  .note {
    color: var(--text-faint);
    font-size: 11.5px;
  }
  .dim {
    color: var(--text-dim);
  }
  .ferr {
    color: var(--danger);
    font-size: 12px;
  }
</style>
