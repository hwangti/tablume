<script lang="ts">
  // CSV into a table: columns are matched by name (or by position without a
  // header line), and every row goes in in one transaction.
  import { onMount } from 'svelte'
  import { num, t } from '../lib/i18n.svelte'
  import { api, errText, openDataFile } from '../lib/api'
  import { app, toast, reloadTable } from '../lib/state.svelte'
  import { decodeText, guessSeparator, parseCSV } from '../lib/csv'
  import type { Column, RowChange, Table } from '../lib/types'
  import Modal from './Modal.svelte'

  let { connId, schema, table }: { connId: string; schema: string; table: Table } = $props()

  let cols = $state<Column[]>([])
  let file = $state('')
  let text = ''
  let sep = $state(',')
  let rows = $state.raw<string[][]>([])
  let header = $state(true)
  let emptyNull = $state(true)
  let map = $state<number[]>([]) // per table column: the CSV column, -1 = none
  let busy = $state(false)
  let error = $state('')

  let names = $derived(header ? (rows[0] ?? []) : (rows[0] ?? []).map((_, i) => t('{n}번째 열', { n: i + 1 })))
  let data = $derived(header ? rows.slice(1) : rows)
  let used = $derived(cols.map((c, i) => ({ c, k: map[i] })).filter((x) => x.k >= 0))

  const close = () => !busy && (app.dataDialog = null)

  onMount(async () => {
    try {
      cols = await api.ListColumns(connId, schema, $state.snapshot(table))
    } catch (e) {
      error = errText(e)
    }
  })

  function automap() {
    const lower = names.map((n) => n.trim().toLowerCase())
    map = cols.map((c, i) => (header ? lower.indexOf(c.name.toLowerCase()) : i < names.length ? i : -1))
  }

  async function pick() {
    error = ''
    try {
      const f = await openDataFile()
      if (!f) return
      text = decodeText(f.bytes)
      file = f.name
      sep = guessSeparator(text)
      parse()
    } catch (e) {
      error = errText(e)
    }
  }

  function parse() {
    rows = parseCSV(text, sep)
    automap()
  }

  const value = (r: string[], k: number) => {
    const v = r[k]
    return v === undefined || (emptyNull && v === '') ? null : v
  }

  async function run() {
    if (busy || !used.length || !data.length) return
    busy = true
    error = ''
    try {
      const changes: RowChange[] = data.map((r) => ({ kind: 'insert', key: {}, values: Object.fromEntries(used.map(({ c, k }) => [c.name, value(r, k)])) }))
      // (props are gone once the dialog closes)
      const [c, sc, name] = [connId, schema, table.name]
      const n = await api.ApplyChanges({ connId, schema, table: $state.snapshot(table), changes })
      busy = false
      close()
      toast(t('“{name}”에 {n}행 가져옴', { name, n: num(n) }))
      for (const tab of app.tabs) if (tab.connId === c && tab.tableRef?.schema === sc && tab.tableRef.table.name === name) reloadTable(tab)
    } catch (e) {
      error = errText(e)
    } finally {
      busy = false
    }
  }
</script>

<Modal title={t('“{name}”에 CSV 가져오기', { name: table.name })} onclose={close} width={640} dismissible={false} onsubmit={run}>
  <div class="form">
    <div class="row">
      <button class="btn" onclick={pick} disabled={busy}>{t('파일 고르기…')}</button>
      <span class="file">{file || t('CSV, TSV (UTF-8 또는 CP949)')}</span>
    </div>
    {#if rows.length}
      <div class="row">
        <label class="chk">{t('구분자')}
          <select bind:value={sep} onchange={parse}>
            <option value=",">, {t('쉼표')}</option>
            <option value={'\t'}>{t('탭')}</option>
            <option value=";">; {t('세미콜론')}</option>
          </select>
        </label>
        <label class="chk"><input type="checkbox" bind:checked={header} onchange={automap} /> {t('첫 줄은 컬럼 이름')}</label>
        <label class="chk"><input type="checkbox" bind:checked={emptyNull} /> {t('빈 칸은 NULL')}</label>
      </div>
      <div class="map">
        <div class="mh">{t('테이블 컬럼')}</div>
        <div class="mh">{t('CSV 열')}</div>
        {#each cols as c, i (c.name)}
          <div class="mc"><span class="mono">{c.name}</span> <span class="dim">{c.type.toLowerCase()}{c.extra ? ' · ' + c.extra : ''}</span></div>
          <select bind:value={map[i]}>
            <option value={-1}>— {t('넣지 않음')} —</option>
            {#each names as n, k (k)}<option value={k}>{n}</option>{/each}
          </select>
        {/each}
      </div>
      {#if used.length}
        <div class="preview">
          <table>
            <thead><tr>{#each used as { c } (c.name)}<th>{c.name}</th>{/each}</tr></thead>
            <tbody>
              {#each data.slice(0, 5) as r, ri (ri)}
                <tr>{#each used as { c, k } (c.name)}{@const v = value(r, k)}<td class:null={v === null}>{v ?? 'NULL'}</td>{/each}</tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      <p class="note">{t('{n}행을 트랜잭션 하나로 넣습니다. 하나라도 실패하면 모두 취소됩니다.', { n: num(data.length) })}</p>
    {/if}
    {#if error}<pre class="ferr">{error}</pre>{/if}
  </div>
  {#snippet footer()}
    <span style="flex:1"></span>
    <button class="btn" onclick={close} disabled={busy}>{t('취소')}</button>
    <button class="btn primary" onclick={run} disabled={busy || !used.length || !data.length}>
      {busy ? t('가져오는 중…') : t('{n}행 가져오기', { n: num(data.length) })}
    </button>
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
    flex-wrap: wrap;
  }
  .file {
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chk {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: 10px;
  }
  .map {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 4px 12px;
    align-items: center;
    max-height: 240px;
    overflow: auto;
  }
  .mh {
    color: var(--text-dim);
    font-size: 11.5px;
  }
  .mc {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono {
    font-family: var(--font-mono);
  }
  .dim {
    color: var(--text-faint);
    font-size: 11.5px;
  }
  .preview {
    max-height: 160px;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  table {
    border-collapse: collapse;
    font-family: var(--font-mono);
    font-size: 12px;
  }
  th,
  td {
    padding: 3px 8px;
    border-bottom: 1px solid var(--grid-line);
    text-align: left;
    white-space: nowrap;
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  th {
    color: var(--text-dim);
    font-weight: 500;
    background: var(--bg-panel);
    position: sticky;
    top: 0;
  }
  td.null {
    color: var(--null);
  }
  .note {
    margin: 0;
    color: var(--text-faint);
    font-size: 12px;
  }
  .ferr {
    margin: 0;
    color: var(--danger);
    font-size: 12px;
    white-space: pre-wrap;
    max-height: 120px;
    overflow: auto;
  }
</style>
