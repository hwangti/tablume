<script lang="ts">
  // Export whole tables: one table as CSV / JSON / SQL, a schema as SQL.
  import { num, t } from '../lib/i18n.svelte'
  import { api, errText } from '../lib/api'
  import { app, toast } from '../lib/state.svelte'
  import type { ExportRequest, Table } from '../lib/types'
  import Modal from './Modal.svelte'
  import { untrack } from 'svelte'

  let { connId, schema, tables }: { connId: string; schema: string; tables: Table[] } = $props()

  let one = $derived(tables.length === 1)
  let viewOnly = $derived(one && tables[0].type.toUpperCase().includes('VIEW'))
  // (the dialog is made anew for each export)
  let format = $state<ExportRequest['format']>(untrack(() => (tables.length === 1 ? 'csv' : 'sql')))
  let ddl = $state(untrack(() => tables.length !== 1))
  let data = $state(true)
  let busy = $state(false)

  let title = $derived(one ? t('“{name}” 내보내기', { name: tables[0].name }) : t('스키마 “{name}” 내보내기', { name: schema }))
  let ok = $derived(format !== 'sql' || ddl || (data && !viewOnly))

  const close = () => (app.dataDialog = null)

  async function run() {
    if (!ok || busy) return
    busy = true
    try {
      const req: ExportRequest = { connId, schema, tables: $state.snapshot(tables), format, ddl: format === 'sql' && ddl, data: format !== 'sql' || data }
      const r = await api.ExportData(req)
      if (r.rows >= 0 && !r.path) return // cancelled
      close()
      if (r.rows >= 0) toast(t('{n}행 내보냄', { n: num(r.rows) }) + ': ' + r.path)
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      busy = false
    }
  }
</script>

<Modal {title} onclose={close} width={420} onsubmit={run}>
  <div class="form">
    {#if one}
      <div class="row">
        <span class="lbl">{t('형식')}</span>
        <label class="chk"><input type="radio" bind:group={format} value="csv" /> CSV</label>
        <label class="chk"><input type="radio" bind:group={format} value="json" /> JSON</label>
        <label class="chk"><input type="radio" bind:group={format} value="sql" /> SQL</label>
      </div>
    {/if}
    {#if format === 'sql'}
      <div class="row">
        <span class="lbl">{t('내용')}</span>
        <label class="chk"><input type="checkbox" bind:checked={ddl} /> {t('구조 (CREATE)')}</label>
        <label class="chk"><input type="checkbox" bind:checked={data} disabled={viewOnly} /> {t('데이터 (INSERT)')}</label>
      </div>
    {/if}
    <p class="note">
      {#if format === 'csv'}{t('첫 줄은 컬럼 이름입니다. NULL은 빈 칸, 바이너리는 0x… 16진수로 씁니다. Excel에서 바로 열 수 있게 UTF-8 BOM을 붙입니다.')}
      {:else if format === 'json'}{t('행마다 객체 하나인 배열입니다. 숫자는 숫자로, 바이너리는 0x… 문자열로 씁니다.')}
      {:else}{t('이 DB에서 그대로 실행할 수 있는 SQL입니다. 참조되는 테이블이 먼저 나옵니다.')}{/if}
      {t('테이블 전체를 서버에서 읽어 파일로 바로 씁니다.')}
    </p>
  </div>
  {#snippet footer()}
    <span style="flex:1"></span>
    <button class="btn" onclick={close}>{t('취소')}</button>
    <button class="btn primary" onclick={run} disabled={!ok || busy}>{busy ? t('내보내는 중…') : t('내보내기')}</button>
  {/snippet}
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .lbl {
    width: 56px;
    flex-shrink: 0;
    color: var(--text-dim);
  }
  .chk {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: 10px;
  }
  .note {
    margin: 0;
    color: var(--text-faint);
    font-size: 12px;
    line-height: 1.5;
  }
</style>
