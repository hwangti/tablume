<script lang="ts">
  // The table's columns as a table (the "컬럼" side of the structure tab). Type,
  // NULL, default and comment can be edited: the changes become ALTER
  // statements that are shown before they run.
  import { t } from '../lib/i18n.svelte'
  import { api, errText } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { toast, openConsole } from '../lib/state.svelte'
  import type { Column, Table } from '../lib/types'
  import Icon from './Icon.svelte'
  import SqlPreview from './SqlPreview.svelte'
  import Modal from './Modal.svelte'

  let {
    connId,
    schema,
    table,
    driver = '',
    version = 0,
    onchanged,
    onlog,
  }: { connId: string; schema: string; table: Table; driver?: string; version?: number; onchanged?: () => void; onlog?: (sql: string[], outcome: string, error?: string) => void } = $props()

  let cols = $state<Column[] | null>(null)
  let error = $state('')
  let loadedFor = ''

  function load() {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    loadedFor = key
    cols = null
    error = ''
    draft = {}
    api
      .ListColumns(connId, schema, $state.snapshot(table))
      .then((c) => key === loadedFor && (cols = c ?? []))
      .catch((e) => key === loadedFor && (error = errText(e)))
  }

  // version: bumped after index changes (unique flags) — reloads unless
  // column edits are waiting, which a reload would throw away
  let loadedVersion = 0
  $effect(() => {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    const v = version
    if (key !== loadedFor) load()
    else if (v !== loadedVersion && !Object.keys(draft).length) load()
    loadedVersion = v
  })

  let sel = $state(-1)

  // ---- editing
  type Field = 'type' | 'nullable' | 'default' | 'comment'
  type Draft = { type: string; nullable: boolean; default: string; comment: string }
  let draft = $state<Record<string, Draft>>({})
  let editing = $state<{ name: string; field: Exclude<Field, 'nullable'>; value: string } | null>(null)

  let readOnlyReason = $derived(
    driver === 'sqlite'
      ? t('SQLite는 컬럼을 바꾸려면 테이블을 새로 만들어야 해서 여기서는 편집할 수 없습니다')
      : table.type.toUpperCase().includes('VIEW')
        ? t('뷰는 편집할 수 없습니다')
        : '',
  )

  const def = (c: Column) => (c.default && c.default.toUpperCase() !== 'NULL' ? c.default : '')

  function current(c: Column): Draft {
    return draft[c.name] ?? { type: c.type, nullable: c.nullable, default: def(c), comment: c.comment ?? '' }
  }

  function changed(c: Column, f: Field): boolean {
    const d = draft[c.name]
    if (!d) return false
    if (f === 'nullable') return d.nullable !== c.nullable
    if (f === 'default') return d.default.trim() !== def(c)
    if (f === 'type') return d.type.trim().toLowerCase() !== c.type.toLowerCase()
    return d.comment !== (c.comment ?? '')
  }

  function setField(c: Column, f: Field, v: string | boolean) {
    const d = { ...current(c), [f]: v }
    draft[c.name] = d
    if (!(['type', 'nullable', 'default', 'comment'] as Field[]).some((x) => changed(c, x))) delete draft[c.name]
  }

  let pending = $derived(cols ? cols.filter((c) => draft[c.name]).length : 0)

  function startEdit(c: Column, field: Exclude<Field, 'nullable'>) {
    if (readOnlyReason) return
    editing = { name: c.name, field, value: current(c)[field] }
  }

  function commitEdit() {
    const e = editing
    if (!e || !cols) return
    editing = null
    const c = cols.find((x) => x.name === e.name)
    if (c) setField(c, e.field, e.value)
  }

  function inputKey(e: KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Enter') {
      e.preventDefault()
      commitEdit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      editing = null
    }
  }

  function focus(node: HTMLInputElement) {
    node.focus()
    node.select()
  }

  function changes() {
    return (cols ?? [])
      .filter((c) => draft[c.name])
      .map((c) => {
        const d = draft[c.name]
        return {
          old: $state.snapshot(c),
          new: { ...$state.snapshot(c), type: d.type.trim(), nullable: d.nullable, default: d.default.trim(), comment: d.comment },
        }
      })
  }

  // ---- apply: preview the statements, then run them
  let apply = $state<{ sql: string[]; busy: boolean; error: string; note: string } | null>(null)
  const transactional = $derived(driver === 'postgres' || driver === 'mssql')

  async function openApply() {
    try {
      const sql = await api.PreviewAlter({ connId, schema, table: $state.snapshot(table), changes: changes() })
      apply = { sql: sql ?? [], busy: false, error: '', note: '' }
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  async function doApply() {
    const a = apply
    if (!a) return
    a.busy = true
    try {
      const r = await api.ApplyAlter({ connId, schema, table: $state.snapshot(table), changes: changes() })
      if (r.error) {
        onlog?.(a.sql.slice(0, r.rolledBack ? a.sql.length : r.applied + 1), '', r.error + '\n' + (r.rolledBack ? t('(모두 취소됨)') : t('({a}/{n}개 적용됨)', { a: r.applied, n: r.total })))
        a.busy = false
        a.error = r.error
        a.note = r.rolledBack
          ? t('하나라도 실패하면 전부 취소됩니다. 아무것도 바뀌지 않았습니다.')
          : t('{n}개 중 {a}개는 이미 적용됐습니다 (이 DB는 DDL을 되돌릴 수 없습니다).', { n: r.total, a: r.applied })
        if (!r.rolledBack && r.applied > 0) {
          draft = {}
          onchanged ? onchanged() : load()
        }
        return
      }
      apply = null
      onlog?.(a.sql, t('컬럼 변경 {n}건 적용', { n: r.applied }))
      toast(t('컬럼 변경 {n}건 적용됨', { n: r.applied }))
      draft = {}
      onchanged ? onchanged() : load()
    } catch (e) {
      a.busy = false
      a.error = errText(e)
    }
  }

  function openSql(sql: string[]) {
    apply = null
    openConsole(connId, { schema, sql: sql.join(';\n') + ';\n', title: `${t('컬럼 변경')}: ${table.name}` })
  }

  function copyRow(c: Column) {
    copyText([c.name, c.type, c.nullable ? 'NULL' : 'NOT NULL', c.default ?? '', c.comment ?? ''].join('\t'))
    toast(t('행 복사됨'))
  }
</script>

{#if error}
  <div class="msg"><pre>{error}</pre></div>
{:else if cols === null}
  <div class="placeholder"><span class="spin"></span> {t('컬럼 불러오는 중…')}</div>
{:else}
  <div class="wrap">
    <div class="bar">
      <span class="title">{t('컬럼')} <span class="count">{cols.length}</span></span>
      {#if table.comment}<span class="tcomment">{table.comment}</span>{/if}
      <span class="grow"></span>
      {#if readOnlyReason}
        <span class="ro" title={readOnlyReason}><Icon name="lock" size={12} /> {t('읽기 전용')}</span>
      {:else if pending > 0}
        <span class="pending">{t('컬럼 {n}개 변경', { n: pending })}</span>
        <button class="btn mini primary" onclick={openApply}><Icon name="check" size={13} /> {t('적용')}</button>
        <button class="btn mini" onclick={() => (draft = {})}><Icon name="undo" size={13} /> {t('되돌리기')}</button>
      {:else}
        <span class="hint">{t('타입·NULL·기본값·코멘트를 더블클릭해서 바꿀 수 있습니다')}</span>
      {/if}
    </div>
    <table>
      <thead>
        <tr>
          <th class="n">#</th>
          <th class="k">{t('키')}</th>
          <th>{t('이름')}</th>
          <th>{t('타입')}</th>
          <th class="c">NULL</th>
          <th>{t('기본값')}</th>
          <th>{t('기타')}</th>
          <th>{t('참조 (FK)')}</th>
          <th>{t('코멘트')}</th>
        </tr>
      </thead>
      <tbody>
        {#each cols as c, i (c.name)}
          {@const cur = current(c)}
          <tr class:sel={sel === i} onclick={() => (sel = i)}>
            <td class="n">{i + 1}</td>
            <td class="k">
              {#if c.pk}<span title={t('기본 키 (PK)')}><Icon name="key" size={12} color="var(--key-pk)" /></span>{/if}
              {#if c.fk}<span title={t('외래 키 (FK)')}><Icon name="link" size={12} color="var(--key-fk)" /></span>{/if}
              {#if c.unique}<span title={t('유니크 (UK)')}><Icon name="unique" size={12} color="var(--key-uk)" /></span>{/if}
            </td>
            <td class="name mono" ondblclick={() => copyRow(c)} title={t('더블클릭: 행 복사')}>{c.name}</td>
            {#snippet cell(field: 'type' | 'default' | 'comment', cls: string, shown: string, empty = '')}
              <td class="{cls} edit" class:edited={changed(c, field)} ondblclick={() => startEdit(c, field)}>
                {#if editing?.name === c.name && editing.field === field}
                  <input class="cell-input mono" bind:value={editing.value} use:focus onkeydown={inputKey} onblur={commitEdit} spellcheck="false" />
                {:else if shown}{shown}{:else}<span class="dim">{empty}</span>{/if}
              </td>
            {/snippet}
            {@render cell('type', 'type mono', cur.type.toLowerCase())}
            <td class="c" class:edited={changed(c, 'nullable')}>
              <label class="nullbox" title={readOnlyReason || t('NULL 허용')}>
                <input type="checkbox" checked={cur.nullable} disabled={!!readOnlyReason} onchange={(e) => setField(c, 'nullable', (e.currentTarget as HTMLInputElement).checked)} />
                {#if cur.nullable}<span class="dim">NULL</span>{:else}<b>NOT NULL</b>{/if}
              </label>
            </td>
            {@render cell('default', 'mono def', cur.default, c.default?.toUpperCase() === 'NULL' ? 'NULL' : '')}
            <td class="dim">{c.extra ?? ''}</td>
            <td class="mono fk">{c.fk ?? ''}</td>
            {@render cell('comment', 'comment', cur.comment)}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if apply}
  <Modal title={t('컬럼 변경 적용 — {n}개 문장', { n: apply.sql.length })} onclose={() => (apply = null)} width={760} dismissible={false} onsubmit={() => apply && !apply.busy && !apply.error && doApply()}>
    <p class="note">
      {#if transactional}
        {t('아래 문장을 하나의 트랜잭션으로 실행합니다. 하나라도 실패하면 모두 취소됩니다.')}
      {:else}
        {t('아래 문장을 차례로 실행합니다. 이 DB는 DDL을 되돌릴 수 없어서, 중간에 실패하면 그 앞까지는 적용된 채로 남습니다.')}
      {/if}
    </p>
    <SqlPreview sql={apply.sql} driver={driver} />
    {#if apply.error}
      <div class="err">{apply.error}{#if apply.note}<div class="err-note">{apply.note}</div>{/if}</div>
    {/if}
    {#snippet footer()}
      <button class="btn" onclick={() => apply && copyText(apply.sql.join(';\n') + ';')}><Icon name="copy" size={13} /> {t('SQL 복사')}</button>
      <button class="btn" title={t('실행하지 않고 새 SQL 콘솔에 옮깁니다')} onclick={() => apply && openSql(apply.sql)}><Icon name="console" size={13} /> {t('콘솔에서 열기')}</button>
      <span style="flex:1"></span>
      <button class="btn" onclick={() => (apply = null)}>{t('닫기')}</button>
      <button class="btn primary" onclick={doApply} disabled={apply?.busy || !!apply?.error}>{apply?.busy ? t('적용 중…') : t('적용')}</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .wrap {
    font-size: 12.5px;
  }
  .title {
    font-weight: 600;
    margin-right: 6px;
  }
  .count {
    color: var(--text-faint);
    font-weight: normal;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 32px;
    padding: 4px 12px;
  }
  .tcomment {
    color: var(--text-dim);
  }
  .grow {
    flex: 1;
  }
  .hint {
    color: var(--text-faint);
    font-size: 11.5px;
  }
  .pending {
    color: var(--warn);
    font-weight: 600;
    margin-right: 4px;
  }
  .ro {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--text-faint);
    font-size: 11.5px;
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
    /* top line as a shadow: a collapsed border wouldn't stay with the
       sticky header */
    box-shadow: inset 0 1px 0 var(--border);
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
    background: var(--grid-cur-row);
  }
  td.edit {
    cursor: text;
  }
  td.edited {
    background: color-mix(in srgb, var(--warn) 22%, transparent);
  }
  td:has(.cell-input) {
    padding: 0;
  }
  .cell-input {
    width: 100%;
    min-width: 120px;
    height: 24px;
    padding: 0 8px;
    border: 2px solid var(--accent);
    border-radius: 0;
    font-size: 12.5px;
  }
  .nullbox {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    cursor: pointer;
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
  .note {
    margin: 0 0 8px;
    color: var(--text-dim);
  }
  .err {
    margin-top: 10px;
    padding: 8px 10px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
    white-space: pre-wrap;
    user-select: text;
    -webkit-user-select: text;
  }
  .err-note {
    margin-top: 6px;
    color: var(--text-dim);
  }
</style>
