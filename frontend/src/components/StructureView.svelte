<script lang="ts">
  // The table's columns as a table (the "표" side of the DDL view).
  import { api, errText } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { toast } from '../lib/state.svelte'
  import type { Column, Table } from '../lib/types'
  import Icon from './Icon.svelte'

  let { connId, schema, table }: { connId: string; schema: string; table: Table } = $props()

  let cols = $state<Column[] | null>(null)
  let error = $state('')
  let loadedFor = ''

  $effect(() => {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    if (key === loadedFor) return
    loadedFor = key
    cols = null
    error = ''
    api
      .ListColumns(connId, schema, $state.snapshot(table))
      .then((c) => key === loadedFor && (cols = c ?? []))
      .catch((e) => key === loadedFor && (error = errText(e)))
  })

  let sel = $state(-1)

  function copyRow(c: Column) {
    copyText([c.name, c.type, c.nullable ? 'NULL' : 'NOT NULL', c.default ?? '', c.comment ?? ''].join('\t'))
    toast('행 복사됨')
  }
</script>

{#if error}
  <div class="msg"><pre>{error}</pre></div>
{:else if cols === null}
  <div class="placeholder"><span class="spin"></span> 컬럼 불러오는 중…</div>
{:else}
  <div class="wrap">
    {#if table.comment}<div class="tcomment">{table.comment}</div>{/if}
    <table>
      <thead>
        <tr>
          <th class="n">#</th>
          <th class="k">키</th>
          <th>이름</th>
          <th>타입</th>
          <th class="c">NULL</th>
          <th>기본값</th>
          <th>기타</th>
          <th>참조 (FK)</th>
          <th>코멘트</th>
        </tr>
      </thead>
      <tbody>
        {#each cols as c, i (c.name)}
          <tr class:sel={sel === i} onclick={() => (sel = i)} ondblclick={() => copyRow(c)}>
            <td class="n">{i + 1}</td>
            <td class="k">
              {#if c.pk}<span title="기본 키 (PK)"><Icon name="key" size={12} color="var(--key-pk)" /></span>{/if}
              {#if c.fk}<span title="외래 키 (FK)"><Icon name="link" size={12} color="var(--key-fk)" /></span>{/if}
              {#if c.unique}<span title="유니크 (UK)"><Icon name="unique" size={12} color="var(--key-uk)" /></span>{/if}
            </td>
            <td class="name mono">{c.name}</td>
            <td class="type mono">{c.type.toLowerCase()}</td>
            <td class="c">{#if c.nullable}<span class="dim">NULL</span>{:else}<b>NOT NULL</b>{/if}</td>
            <td class="mono def">{#if c.default && c.default !== 'NULL'}{c.default}{:else if c.default === 'NULL'}<span class="dim">NULL</span>{/if}</td>
            <td class="dim">{c.extra ?? ''}</td>
            <td class="mono fk">{c.fk ?? ''}</td>
            <td class="comment">{c.comment ?? ''}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .wrap {
    height: 100%;
    overflow: auto;
    font-size: 12.5px;
  }
  .tcomment {
    padding: 8px 12px 6px;
    color: var(--text-dim);
  }
  table {
    border-collapse: collapse;
    min-width: 100%;
  }
  th {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 0 10px;
    height: 28px;
    text-align: left;
    font-weight: 600;
    white-space: nowrap;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    border-right: 1px solid var(--grid-line);
  }
  td {
    padding: 0 10px;
    height: 24px;
    white-space: nowrap;
    border-bottom: 1px solid var(--grid-line);
    border-right: 1px solid var(--grid-line);
  }
  tbody tr:nth-child(even) {
    background: var(--grid-alt);
  }
  tbody tr.sel {
    background: var(--bg-select);
  }
  .n {
    width: 1%;
    text-align: right;
    color: var(--text-faint);
    font-size: 11px;
  }
  .k {
    width: 1%;
  }
  td.k span {
    display: inline-flex;
    vertical-align: middle;
  }
  .c {
    width: 1%;
  }
  .name {
    font-weight: 600;
  }
  .type {
    color: var(--syn-type);
  }
  .def {
    color: var(--syn-string);
  }
  .fk {
    color: var(--key-fk);
  }
  .comment {
    white-space: normal;
    min-width: 200px;
    color: var(--text-dim);
  }
  .dim {
    color: var(--text-faint);
  }
  b {
    font-weight: 600;
  }
  .msg {
    padding: 12px;
    color: var(--danger);
  }
  .placeholder {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 16px;
    color: var(--text-dim);
  }
</style>
