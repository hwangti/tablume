<script lang="ts">
  // The table's foreign keys (the "외래 키" side of the structure tab). They
  // can be added and dropped; like column and index changes, the statements
  // are shown before they run.
  import { t } from '../lib/i18n.svelte'
  import { api, errText, type AlterResult } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { toast, openConsole } from '../lib/state.svelte'
  import type { Column, Table, TableFK } from '../lib/types'
  import Icon from './Icon.svelte'
  import SqlPreview from './SqlPreview.svelte'
  import Modal from './Modal.svelte'
  import Dropdown from './Dropdown.svelte'

  let {
    connId,
    schema,
    table,
    driver = '',
    version = 0,
    onchanged,
    onlog,
    highlight = null,
  }: {
    connId: string
    schema: string
    table: Table
    driver?: string
    version?: number
    onchanged?: () => void
    onlog?: (sql: string[], outcome: string, error?: string) => void
    highlight?: { column: string; n: number } | null
  } = $props()

  let fks = $state<TableFK[] | null>(null)
  let cols = $state<Column[]>([])
  let error = $state('')
  let loadedFor = ''

  let dropped = $state<Record<string, true>>({})
  let created = $state<TableFK[]>([])
  let pending = $derived(Object.keys(dropped).length + created.length)

  function load() {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    loadedFor = key
    fks = null
    error = ''
    dropped = {}
    created = []
    const tb = $state.snapshot(table)
    Promise.all([api.ListForeignKeys(connId, schema, tb), api.ListColumns(connId, schema, tb)])
      .then(([f, c]) => {
        if (key !== loadedFor) return
        fks = f ?? []
        cols = c ?? []
      })
      .catch((e) => key === loadedFor && (error = errText(e)))
  }

  // version: bumped after column changes (a dropped column takes its keys)
  let loadedVersion = 0
  $effect(() => {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    const v = version
    if (key !== loadedFor || (v !== loadedVersion && !pending)) load()
    loadedVersion = v
  })


  // ---- a key icon clicked in the columns: flash the rows with that column
  let flash = $state(new Set<string>())
  let flashTimer = 0
  let seen = 0 // the request last shown: once each, not again when the list reloads
  $effect(() => {
    const h = highlight
    const list = fks
    if (!h || !list || h.n === seen) return
    seen = h.n
    const col = h.column.toLowerCase()
    flash = new Set(list.filter((x) => x.columns.some((c) => c.toLowerCase() === col)).map((x) => x.name))
    clearTimeout(flashTimer)
    flashTimer = window.setTimeout(() => (flash = new Set()), 1800)
    requestAnimationFrame(() => document.querySelector(`[data-flash="${h.n}"]`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }))
  })

  let readOnlyReason = $derived(table.type.toUpperCase().includes('VIEW') ? t('뷰에는 외래 키가 없습니다') : '')
  const refName = (f: TableFK) => (f.refSchema ? f.refSchema + '.' : '') + f.refTable.name

  // ON DELETE / ON UPDATE choices; Oracle has no ON UPDATE rule
  const rules = $derived(
    driver === 'oracle'
      ? ['NO ACTION', 'CASCADE', 'SET NULL']
      : driver === 'mysql' || driver === 'mariadb'
        ? ['NO ACTION', 'RESTRICT', 'CASCADE', 'SET NULL']
        : driver === 'mssql'
          ? ['NO ACTION', 'CASCADE', 'SET NULL', 'SET DEFAULT']
          : ['NO ACTION', 'RESTRICT', 'CASCADE', 'SET NULL', 'SET DEFAULT'],
  )

  const ruleOpts = $derived(rules.map((r) => ({ value: r, label: r })))
  const colOpts = (cs: Column[]) => cs.map((c) => ({ value: c.name, label: c.name, hint: c.type.toLowerCase() }))

  // ---- add dialog
  type Form = { name: string; named: boolean; ref: number; pairs: { col: string; ref: string }[]; onDelete: string; onUpdate: string }
  let form = $state<Form | null>(null)
  let tables = $state<Table[]>([])
  let refCols = $state<Column[]>([])
  const maxName = $derived(driver === 'oracle' ? 30 : driver === 'postgres' ? 63 : driver === 'mssql' ? 128 : 64)

  async function openAdd() {
    form = { name: '', named: false, ref: -1, pairs: [], onDelete: 'NO ACTION', onUpdate: 'NO ACTION' }
    refCols = []
    try {
      tables = ((await api.ListTables(connId, schema)) ?? []).filter((x) => !x.type.toUpperCase().includes('VIEW'))
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  // picking the referenced table: pair its primary key columns with this
  // table's columns of the same name, where there are some
  async function pickRef(i: number) {
    if (!form) return
    form.ref = i
    refCols = []
    if (i < 0) return
    const c = (await api.ListColumns(connId, schema, $state.snapshot(tables[i]))) ?? []
    if (!form || form.ref !== i) return
    refCols = c
    const pk = c.filter((x) => x.pk)
    form.pairs = (pk.length ? pk : [c[0]]).filter(Boolean).map((p) => ({ col: cols.find((x) => x.name.toLowerCase() === p.name.toLowerCase())?.name ?? '', ref: p.name }))
  }

  function suggest(f: Form): string {
    const tb = f.ref >= 0 ? tables[f.ref]?.name : ''
    let n = ['fk', table.name, ...(f.pairs.some((p) => p.col) ? f.pairs.map((p) => p.col).filter(Boolean) : [tb])].join('_').replace(/[^\p{L}\p{N}_]/gu, '_')
    if (driver === 'oracle') n = n.toUpperCase()
    return n.slice(0, maxName)
  }

  let formName = $derived(form ? (form.named ? form.name : suggest(form)) : '')
  let formError = $derived.by(() => {
    if (!form) return ''
    if (form.ref < 0) return t('참조할 테이블을 고르세요')
    if (!form.pairs.length || form.pairs.some((p) => !p.col || !p.ref)) return t('컬럼과 참조할 컬럼을 짝지어 고르세요')
    if (!formName.trim()) return t('이름을 넣으세요')
    const n = formName.trim().toLowerCase()
    if (fks?.some((x) => x.name.toLowerCase() === n && !dropped[x.name]) || created.some((x) => x.name.toLowerCase() === n)) return t('같은 이름의 외래 키가 이미 있습니다')
    return ''
  })

  function saveForm() {
    const f = form
    if (!f || formError) return
    const ref = tables[f.ref]
    created.push({
      name: formName.trim(),
      columns: f.pairs.map((p) => p.col),
      refTable: $state.snapshot(ref),
      refSchema: '',
      refColumns: f.pairs.map((p) => p.ref),
      onDelete: f.onDelete,
      onUpdate: driver === 'oracle' ? '' : f.onUpdate,
    })
    form = null
  }

  // ---- apply
  let apply = $state<{ sql: string[]; busy: boolean; error: string; note: string } | null>(null)
  const transactional = $derived(driver === 'postgres' || driver === 'mssql')

  function request() {
    return {
      connId,
      schema,
      table: $state.snapshot(table),
      drop: (fks ?? []).filter((x) => dropped[x.name]).map((x) => $state.snapshot(x)),
      create: $state.snapshot(created),
    }
  }

  async function openApply() {
    try {
      const sql = await api.PreviewForeignKeys(request())
      apply = { sql: sql ?? [], busy: false, error: '', note: '' }
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  async function doApply() {
    const a = apply
    if (!a) return
    a.busy = true
    let r: AlterResult
    try {
      r = await api.ApplyForeignKeys(request())
    } catch (e) {
      a.busy = false
      a.error = errText(e)
      return
    }
    if (r.error) {
      onlog?.(a.sql.slice(0, r.rolledBack ? a.sql.length : r.applied + 1), '', r.error + '\n' + (r.rolledBack ? t('(모두 취소됨)') : t('({a}/{n}개 적용됨)', { a: r.applied, n: r.total })))
      a.busy = false
      a.error = r.error
      a.note = r.rolledBack
        ? t('하나라도 실패하면 전부 취소됩니다. 아무것도 바뀌지 않았습니다.')
        : t('{n}개 중 {a}개는 이미 적용됐습니다 (이 DB는 DDL을 되돌릴 수 없습니다).', { n: r.total, a: r.applied })
      if (!r.rolledBack && r.applied > 0) {
        load()
        onchanged?.()
      }
      return
    }
    apply = null
    onlog?.(a.sql, t('외래 키 변경 {n}건 적용', { n: r.applied }))
    toast(t('외래 키 변경 {n}건 적용됨', { n: r.applied }))
    load()
    onchanged?.()
  }

  function openSql(sql: string[]) {
    apply = null
    openConsole(connId, { schema, sql: sql.join(';\n') + ';\n', title: `${t('외래 키')}: ${table.name}` })
  }
</script>

{#if error}
  <div class="msg"><pre>{error}</pre></div>
{:else if fks === null}
  <div class="placeholder"><span class="spin"></span> {t('외래 키 불러오는 중…')}</div>
{:else}
  <div class="wrap">
    <div class="bar">
      <span class="title">{t('외래 키')} <span class="count">{fks.length}</span></span>
      {#if !readOnlyReason}
        <button class="btn mini" onclick={openAdd}><Icon name="plus" size={13} /> {t('외래 키 추가')}</button>
      {/if}
      <span class="grow"></span>
      {#if pending > 0}
        <span class="pending">{t('외래 키 {n}건 변경', { n: pending })}</span>
        <button class="btn mini primary" onclick={openApply}><Icon name="check" size={13} /> {t('적용')}</button>
        <button class="btn mini" onclick={() => ((dropped = {}), (created = []))}><Icon name="undo" size={13} /> {t('되돌리기')}</button>
      {/if}
    </div>
    <table>
      <thead>
        <tr>
          <th class="n">#</th>
          <th>{t('이름')}</th>
          <th>{t('컬럼')}</th>
          <th>{t('참조 테이블')}</th>
          <th>{t('참조 컬럼')}</th>
          {#if driver !== 'oracle'}<th>ON UPDATE</th>{/if}
          <th>ON DELETE</th>
          <th class="act"></th>
        </tr>
      </thead>
      <tbody>
        {#snippet row(f: TableFK, n: string)}
          <td class="n">{n}</td>
          <td class="name mono">{f.name}</td>
          <td class="mono cols">{f.columns.join(', ')}</td>
          <td class="mono ref">{refName(f)}</td>
          <td class="mono cols">{f.refColumns.join(', ')}</td>
          {#if driver !== 'oracle'}<td class="dim">{f.onUpdate}</td>{/if}
          <td class="dim">{f.onDelete}</td>
        {/snippet}
        {#each fks as f, i (f.name)}
          <tr class:dropped={dropped[f.name]} class:flash={flash.has(f.name)} data-flash={flash.has(f.name) && i === fks.findIndex((x) => flash.has(x.name)) ? highlight?.n : undefined}>
            {@render row(f, String(i + 1))}
            <td class="act">
              {#if !readOnlyReason}
                {#if dropped[f.name]}
                  <button class="icon-btn sm" title={t('삭제 취소')} onclick={() => delete dropped[f.name]}><Icon name="undo" size={13} /></button>
                {:else}
                  <button class="icon-btn sm" title={t('삭제')} onclick={() => (dropped[f.name] = true)}><Icon name="trash" size={13} /></button>
                {/if}
              {/if}
            </td>
          </tr>
        {/each}
        {#each created as f, i (i)}
          <tr class="added">
            {@render row(f, '+')}
            <td class="act"><button class="icon-btn sm" title={t('추가 취소')} onclick={() => created.splice(i, 1)}><Icon name="close" size={13} /></button></td>
          </tr>
        {/each}
        {#if !fks.length && !created.length}
          <tr><td colspan="8" class="empty">{t('외래 키가 없습니다')}</td></tr>
        {/if}
      </tbody>
    </table>
  </div>
{/if}

{#if form}
  <Modal title={t('외래 키 추가')} onclose={() => (form = null)} width={620} dismissible={false} onsubmit={saveForm}>
    <div class="form">
      <div class="frow">
        <span class="lbl">{t('참조 테이블')}</span>
        <Dropdown
          value={form.ref}
          options={tables.map((tb, i) => ({ value: i, label: (tb.schema && driver === 'mssql' ? tb.schema + '.' : '') + tb.name, hint: tb.comment }))}
          onchange={pickRef}
          placeholder={t('고르세요')}
          mono
          width="100%"
        />
      </div>
      <div class="frow top">
        <span class="lbl">{t('컬럼')}</span>
        <div class="pairs">
          {#each form.pairs as p, i (i)}
            <div class="pair">
              <Dropdown value={p.col} options={colOpts(cols)} onchange={(v) => (p.col = v)} placeholder={t('이 테이블 컬럼')} mono width="100%" />
              <Icon name="chevron" size={12} />
              <Dropdown value={p.ref} options={colOpts(refCols)} onchange={(v) => (p.ref = v)} placeholder={t('참조할 컬럼')} mono width="100%" />
              <button class="icon-btn sm" title={t('빼기')} onclick={() => form?.pairs.splice(i, 1)}><Icon name="close" size={12} /></button>
            </div>
          {/each}
          {#if form.ref >= 0}
            <button class="btn mini addpair" onclick={() => form?.pairs.push({ col: '', ref: '' })}><Icon name="plus" size={12} /> {t('컬럼 짝 추가')}</button>
          {/if}
        </div>
      </div>
      <div class="frow">
        <span class="lbl">ON DELETE</span>
        <Dropdown value={form.onDelete} options={ruleOpts} onchange={(v) => form && (form.onDelete = v)} width="130px" />
        {#if driver !== 'oracle'}
          <span class="lbl right">ON UPDATE</span>
          <Dropdown value={form.onUpdate} options={ruleOpts} onchange={(v) => form && (form.onUpdate = v)} width="130px" />
        {/if}
      </div>
      <div class="frow">
        <label class="lbl" for="fk-name">{t('이름')}</label>
        <input
          id="fk-name"
          type="text"
          class="mono grow"
          value={formName}
          maxlength={maxName}
          oninput={(e) => form && ((form.name = e.currentTarget.value), (form.named = true))}
          spellcheck="false"
        />
        {#if form.named}<button class="btn mini" title={t('컬럼에서 이름 만들기')} onclick={() => form && (form.named = false)}>{t('자동')}</button>{/if}
      </div>
    </div>
    {#snippet footer()}
      <span class="ferr">{formError}</span>
      <span style="flex:1"></span>
      <button class="btn" onclick={() => (form = null)}>{t('취소')}</button>
      <button class="btn primary" onclick={saveForm} disabled={!!formError}>{t('추가')}</button>
    {/snippet}
  </Modal>
{/if}

{#if apply}
  <Modal title={t('외래 키 변경 적용 — {n}개 문장', { n: apply.sql.length })} onclose={() => (apply = null)} width={760} dismissible={false} onsubmit={() => apply && !apply.busy && !apply.error && doApply()}>
    <p class="note">
      {#if transactional}
        {t('아래 문장을 하나의 트랜잭션으로 실행합니다. 하나라도 실패하면 모두 취소됩니다.')}
      {:else}
        {t('아래 문장을 차례로 실행합니다. 이 DB는 DDL을 되돌릴 수 없어서, 중간에 실패하면 그 앞까지는 적용된 채로 남습니다.')}
      {/if}
      {t('외래 키를 만들 때 기존 행이 모두 맞는지 검사하므로, 큰 테이블은 시간이 걸릴 수 있습니다.')}
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
    margin-top: 14px;
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
  .grow {
    flex: 1;
  }
  .pending {
    color: var(--warn);
    font-weight: 600;
    margin-right: 4px;
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
  tr.dropped td:not(.act) {
    text-decoration: line-through;
    opacity: 0.5;
  }
  tr.added {
    background: color-mix(in srgb, var(--ok) 12%, transparent);
  }
  .n {
    width: 1%;
    text-align: right;
    color: var(--text-faint);
    font-size: 11px;
  }
  .name {
    font-weight: 600;
  }
  .cols {
    color: var(--syn-ident);
    white-space: normal;
  }
  .ref {
    color: var(--key-fk);
  }
  .act {
    width: 1%;
    padding: 0 4px;
  }
  .dim {
    color: var(--text-faint);
  }
  .empty {
    color: var(--text-faint);
    text-align: center;
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
  .form {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .frow {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .frow.top {
    align-items: flex-start;
  }
  .lbl {
    width: 80px;
    flex-shrink: 0;
    color: var(--text-dim);
  }
  .lbl.right {
    width: auto;
    margin-left: 12px;
  }
  .frow.top .lbl {
    padding-top: 4px;
  }
  .pairs {
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex: 1;
  }
  .pair {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .frow input {
    height: 26px;
  }
  .addpair {
    align-self: flex-start;
  }
  .ferr {
    color: var(--danger);
    font-size: 12px;
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
  tr.flash td {
    animation: flash 1.8s ease-out;
  }
  @keyframes flash {
    0%,
    40% {
      background: color-mix(in srgb, var(--accent) 30%, transparent);
    }
    100% {
      background: transparent;
    }
  }
</style>
