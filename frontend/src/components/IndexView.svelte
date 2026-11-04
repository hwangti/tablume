<script lang="ts">
  // The table's indexes (the "인덱스" side of the structure tab). Indexes can
  // be added, changed (dropped and created again) and dropped; like column
  // changes, the statements are shown before they run.
  import { t } from '../lib/i18n.svelte'
  import { api, errText, type AlterResult } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { toast, openConsole } from '../lib/state.svelte'
  import type { Column, Index, NewIndex, Table } from '../lib/types'
  import Icon from './Icon.svelte'
  import SqlPreview from './SqlPreview.svelte'
  import Modal from './Modal.svelte'

  let {
    connId,
    schema,
    table,
    driver = '',
    onchanged,
    onlog,
  }: { connId: string; schema: string; table: Table; driver?: string; onchanged?: () => void; onlog?: (sql: string[], outcome: string, error?: string) => void } = $props()

  let indexes = $state<Index[] | null>(null)
  let cols = $state<Column[]>([])
  let error = $state('')
  let loadedFor = ''

  // pending changes: indexes to drop (by name) and new ones to create
  let dropped = $state<Record<string, true>>({})
  let created = $state<NewIndex[]>([])
  let pending = $derived(Object.keys(dropped).length + created.length)

  function load() {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    loadedFor = key
    indexes = null
    error = ''
    dropped = {}
    created = []
    const t = $state.snapshot(table)
    Promise.all([api.ListIndexes(connId, schema, t), api.ListColumns(connId, schema, t)])
      .then(([ix, c]) => {
        if (key !== loadedFor) return
        indexes = ix ?? []
        cols = c ?? []
      })
      .catch((e) => key === loadedFor && (error = errText(e)))
  }

  $effect(() => {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    if (key !== loadedFor) load()
  })

  let readOnlyReason = $derived(table.type.toUpperCase().includes('VIEW') ? t('뷰의 인덱스는 여기서 바꿀 수 없습니다') : '')
  let sel = $state('')

  function dropBlock(ix: Index): string {
    if (ix.primary) return t('기본 키는 여기서 지울 수 없습니다')
    if (driver === 'sqlite' && ix.constraint) return t('PRIMARY KEY / UNIQUE 제약에 딸린 인덱스는 테이블을 다시 만들어야 지울 수 있습니다')
    return ''
  }

  function toggleDrop(ix: Index) {
    if (dropped[ix.name]) delete dropped[ix.name]
    else dropped[ix.name] = true
  }

  // ---- add / change dialog
  type Form = { name: string; named: boolean; unique: boolean; cols: { name: string; desc: boolean }[]; replaces: string; at: number }
  let form = $state<Form | null>(null)

  const maxName = $derived(driver === 'oracle' ? 30 : driver === 'postgres' ? 63 : driver === 'mssql' ? 128 : 64)

  function suggest(f: Form): string {
    let n = [f.unique ? 'ux' : 'idx', table.name, ...f.cols.map((c) => c.name)].join('_').replace(/[^\p{L}\p{N}_]/gu, '_')
    if (driver === 'oracle') n = n.toUpperCase()
    return n.slice(0, maxName)
  }

  function openAdd() {
    form = { name: '', named: false, unique: false, cols: [], replaces: '', at: -1 }
  }

  // Columns of an existing index as plain names, or null when it has
  // expressions / prefixes the dialog can't show.
  function plainCols(ix: Index): { name: string; desc: boolean }[] | null {
    const out = []
    for (const s of ix.columns) {
      const desc = s.endsWith(' DESC')
      const name = desc ? s.slice(0, -5) : s
      const c = cols.find((x) => x.name === name || x.name === name.replace(/^"(.*)"$/, '$1'))
      if (!c) return null
      out.push({ name: c.name, desc })
    }
    return out
  }

  function editBlock(ix: Index): string {
    if (ix.primary) return t('기본 키는 여기서 바꿀 수 없습니다')
    if (ix.constraint) return t('제약(UNIQUE 등)에 딸린 인덱스는 여기서 바꿀 수 없습니다')
    if (ix.where || ix.include?.length) return t('조건이나 INCLUDE가 있는 인덱스는 SQL로 바꾸세요')
    if (!plainCols(ix)) return t('식이나 길이 지정이 들어간 인덱스는 SQL로 바꾸세요')
    return ''
  }

  function openEdit(ix: Index) {
    form = { name: ix.name, named: true, unique: ix.unique, cols: plainCols(ix) ?? [], replaces: ix.name, at: -1 }
  }

  function openEditNew(i: number) {
    const n = created[i]
    form = { name: n.name, named: true, unique: n.unique, cols: n.columns.map((c) => ({ ...c })), replaces: '', at: i }
  }

  function addCol(name: string) {
    if (!form || form.cols.some((c) => c.name === name)) return
    form.cols.push({ name, desc: false })
  }

  function moveCol(i: number, d: -1 | 1) {
    if (!form) return
    const j = i + d
    if (j < 0 || j >= form.cols.length) return
    const [c] = form.cols.splice(i, 1)
    form.cols.splice(j, 0, c)
  }

  let formName = $derived(form ? (form.named ? form.name : suggest(form)) : '')
  let formError = $derived.by(() => {
    if (!form) return ''
    if (!form.cols.length) return t('컬럼을 하나 이상 고르세요')
    if (!formName.trim()) return t('이름을 넣으세요')
    const taken =
      indexes?.some((x) => x.name.toLowerCase() === formName.trim().toLowerCase() && x.name !== form!.replaces && !dropped[x.name]) ||
      created.some((x, i) => i !== form!.at && x.name.toLowerCase() === formName.trim().toLowerCase())
    return taken ? t('같은 이름의 인덱스가 이미 있습니다') : ''
  })

  function saveForm() {
    const f = form
    if (!f || formError) return
    const n: NewIndex = { name: formName.trim(), unique: f.unique, columns: f.cols.map((c) => ({ ...c })) }
    if (f.at >= 0) created[f.at] = n
    else {
      if (f.replaces) dropped[f.replaces] = true
      created.push(n)
    }
    form = null
  }

  // ---- apply
  let apply = $state<{ sql: string[]; busy: boolean; error: string; note: string } | null>(null)
  const transactional = $derived(driver === 'postgres' || driver === 'mssql' || driver === 'sqlite')

  function request() {
    return {
      connId,
      schema,
      table: $state.snapshot(table),
      drop: (indexes ?? []).filter((x) => dropped[x.name]).map((x) => $state.snapshot(x)),
      create: $state.snapshot(created),
    }
  }

  async function openApply() {
    try {
      const sql = await api.PreviewIndexes(request())
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
      r = await api.ApplyIndexes(request())
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
    onlog?.(a.sql, t('인덱스 변경 {n}건 적용', { n: r.applied }))
    toast(t('인덱스 변경 {n}건 적용됨', { n: r.applied }))
    load()
    onchanged?.()
  }

  // the statements, not run, in a new console (to adjust or run later)
  function openSql(sql: string[]) {
    apply = null
    openConsole(connId, { schema, sql: sql.join(';\n') + ';\n', title: `${t('인덱스')}: ${table.name}` })
  }

  function formKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.target as HTMLElement).tagName === 'INPUT' && (e.target as HTMLInputElement).type === 'text') {
      e.preventDefault()
      saveForm()
    }
  }
</script>

{#if error}
  <div class="msg"><pre>{error}</pre></div>
{:else if indexes === null}
  <div class="placeholder"><span class="spin"></span> {t('인덱스 불러오는 중…')}</div>
{:else}
  <div class="wrap">
    <div class="bar">
      <span class="title">{t('인덱스')} <span class="count">{indexes.length}</span></span>
      {#if readOnlyReason}
        <span class="ro" title={readOnlyReason}><Icon name="lock" size={12} /> {t('읽기 전용')}</span>
      {:else}
        <button class="btn mini" onclick={openAdd}><Icon name="plus" size={13} /> {t('인덱스 추가')}</button>
      {/if}
      <span class="grow"></span>
      {#if pending > 0}
        <span class="pending">{t('인덱스 {n}건 변경', { n: pending })}</span>
        <button class="btn mini primary" onclick={openApply}><Icon name="check" size={13} /> {t('적용')}</button>
        <button class="btn mini" onclick={() => ((dropped = {}), (created = []))}><Icon name="undo" size={13} /> {t('되돌리기')}</button>
      {:else if !readOnlyReason}
        <span class="hint">{t('더블클릭하면 고칠 수 있습니다')}</span>
      {/if}
    </div>
    <table>
      <thead>
        <tr>
          <th class="n">#</th>
          <th class="k">{t('키')}</th>
          <th>{t('이름')}</th>
          <th>{t('컬럼')}</th>
          <th class="c">{t('유니크')}</th>
          <th>{t('종류')}</th>
          <th>{t('조건 / INCLUDE')}</th>
          <th class="act"></th>
        </tr>
      </thead>
      <tbody>
        {#each indexes as ix, i (ix.name)}
          {@const blockEdit = editBlock(ix)}
          {@const blockDrop = dropBlock(ix)}
          <tr
            class:sel={sel === ix.name}
            class:dropped={dropped[ix.name]}
            onclick={() => (sel = ix.name)}
            ondblclick={() => !readOnlyReason && !blockEdit && !dropped[ix.name] && openEdit(ix)}
          >
            <td class="n">{i + 1}</td>
            <td class="k">
              {#if ix.primary}<span title={t('기본 키 (PK)')}><Icon name="key" size={12} color="var(--key-pk)" /></span>
              {:else if ix.unique}<span title={ix.constraint ? `${t('유니크 제약')} ${ix.constraint}` : t('유니크 인덱스')}><Icon name="unique" size={12} color="var(--key-uk)" /></span>{/if}
            </td>
            <td class="name mono">{ix.name}</td>
            <td class="mono cols">{ix.columns.join(', ')}</td>
            <td class="c">{#if ix.unique}<Icon name="check" size={13} />{/if}</td>
            <td class="dim">{ix.type}{ix.constraint && !ix.primary ? ' · ' + t('제약') : ''}</td>
            <td class="mono dim">
              {#if ix.where}WHERE {ix.where}{/if}
              {#if ix.include?.length}{ix.where ? ' · ' : ''}INCLUDE ({ix.include.join(', ')}){/if}
            </td>
            <td class="act">
              {#if !readOnlyReason}
                {#if dropped[ix.name]}
                  <button class="icon-btn sm" title={t('삭제 취소')} onclick={() => toggleDrop(ix)}><Icon name="undo" size={13} /></button>
                {:else}
                  <button class="icon-btn sm" title={blockEdit || t('고치기 (지우고 다시 만듭니다)')} disabled={!!blockEdit} onclick={() => openEdit(ix)}><Icon name="edit" size={13} /></button>
                  <button class="icon-btn sm" title={blockDrop || t('삭제')} disabled={!!blockDrop} onclick={() => toggleDrop(ix)}><Icon name="trash" size={13} /></button>
                {/if}
              {/if}
            </td>
          </tr>
        {/each}
        {#each created as n, i (i)}
          <tr class="added" ondblclick={() => openEditNew(i)}>
            <td class="n">+</td>
            <td class="k">{#if n.unique}<Icon name="unique" size={12} color="var(--key-uk)" />{/if}</td>
            <td class="name mono">{n.name}</td>
            <td class="mono cols">{n.columns.map((c) => c.name + (c.desc ? ' DESC' : '')).join(', ')}</td>
            <td class="c">{#if n.unique}<Icon name="check" size={13} />{/if}</td>
            <td class="dim">{t('새 인덱스')}</td>
            <td></td>
            <td class="act">
              <button class="icon-btn sm" title={t('고치기')} onclick={() => openEditNew(i)}><Icon name="edit" size={13} /></button>
              <button class="icon-btn sm" title={t('추가 취소')} onclick={() => created.splice(i, 1)}><Icon name="close" size={13} /></button>
            </td>
          </tr>
        {/each}
        {#if !indexes.length && !created.length}
          <tr><td colspan="8" class="empty">{t('인덱스가 없습니다')}</td></tr>
        {/if}
      </tbody>
    </table>
  </div>
{/if}

{#if form}
  <Modal title={form.replaces ? `${t('인덱스 고치기')} — ${form.replaces}` : t('인덱스 추가')} onclose={() => (form = null)} width={620} dismissible={false} onsubmit={saveForm}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="form" onkeydown={formKey}>
      <div class="row">
        <label class="lbl" for="ix-name">{t('이름')}</label>
        <input
          id="ix-name"
          type="text"
          class="mono"
          value={formName}
          maxlength={maxName}
          oninput={(e) => form && ((form.name = e.currentTarget.value), (form.named = true))}
          spellcheck="false"
        />
        {#if form.named}<button class="btn mini" title={t('컬럼에서 이름 만들기')} onclick={() => form && (form.named = false)}>{t('자동')}</button>{/if}
      </div>
      <div class="row">
        <span class="lbl"></span>
        <label class="chk"><input type="checkbox" bind:checked={form.unique} /> {t('유니크 (같은 값이 두 번 들어갈 수 없음)')}</label>
      </div>
      <div class="pick">
        <div class="side">
          <div class="cap">{t('테이블 컬럼')} <span class="dim">— {t('눌러서 추가')}</span></div>
          <div class="list">
            {#each cols as c (c.name)}
              {@const on = form.cols.some((x) => x.name === c.name)}
              <button class="col" class:on onclick={() => addCol(c.name)} disabled={on}>
                <span class="mono">{c.name}</span>
                <span class="ty mono">{c.type.toLowerCase()}</span>
              </button>
            {/each}
          </div>
        </div>
        <div class="side">
          <div class="cap">{t('인덱스 컬럼')} <span class="dim">— {t('순서대로')}</span></div>
          <div class="list">
            {#each form.cols as c, i (c.name)}
              <div class="sel-col">
                <span class="ord">{i + 1}</span>
                <span class="mono grow">{c.name}</span>
                <button class="dir" title={t('정렬 방향')} onclick={() => (c.desc = !c.desc)}>{c.desc ? 'DESC' : 'ASC'}</button>
                <button class="icon-btn sm" title={t('위로')} disabled={i === 0} onclick={() => moveCol(i, -1)}><Icon name="chevronUp" size={12} /></button>
                <button class="icon-btn sm" title={t('아래로')} disabled={i === form.cols.length - 1} onclick={() => moveCol(i, 1)}><Icon name="chevronDown" size={12} /></button>
                <button class="icon-btn sm" title={t('빼기')} onclick={() => form?.cols.splice(i, 1)}><Icon name="close" size={12} /></button>
              </div>
            {:else}
              <div class="none">{t('왼쪽에서 컬럼을 고르세요')}</div>
            {/each}
          </div>
        </div>
      </div>
      {#if form.replaces}<p class="note">{t('적용하면 기존 인덱스를 지우고 새로 만듭니다.')}</p>{/if}
    </div>
    {#snippet footer()}
      <span class="ferr">{formError}</span>
      <span style="flex:1"></span>
      <button class="btn" onclick={() => (form = null)}>{t('취소')}</button>
      <button class="btn primary" onclick={saveForm} disabled={!!formError}>{form?.replaces || (form && form.at >= 0) ? t('바꾸기') : t('추가')}</button>
    {/snippet}
  </Modal>
{/if}

{#if apply}
  <Modal title={t('인덱스 변경 적용 — {n}개 문장', { n: apply.sql.length })} onclose={() => (apply = null)} width={760} dismissible={false} onsubmit={() => apply && !apply.busy && !apply.error && doApply()}>
    <p class="note">
      {#if transactional}
        {t('아래 문장을 하나의 트랜잭션으로 실행합니다. 하나라도 실패하면 모두 취소됩니다.')}
      {:else}
        {t('아래 문장을 차례로 실행합니다. 이 DB는 DDL을 되돌릴 수 없어서, 중간에 실패하면 그 앞까지는 적용된 채로 남습니다.')}
      {/if}
      {t('큰 테이블은 인덱스를 만드는 동안 시간이 걸리고 쓰기가 막힐 수 있습니다.')}
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
  tr.dropped td {
    background: color-mix(in srgb, var(--danger) 14%, transparent);
  }
  tr.dropped td:not(.act) {
    text-decoration: line-through;
    color: var(--text-faint);
  }
  tr.added td {
    background: color-mix(in srgb, var(--ok) 14%, transparent);
  }
  .n {
    width: 1%;
    text-align: right;
    color: var(--text-faint);
    font-size: 11px;
  }
  .k,
  .c {
    width: 1%;
  }
  td.k span,
  td.c {
    vertical-align: middle;
  }
  td.k span {
    display: inline-flex;
  }
  td.c {
    text-align: center;
    color: var(--ok);
  }
  .name {
    font-weight: 600;
  }
  .cols {
    color: var(--syn-ident);
  }
  .act {
    width: 1%;
    padding: 0 4px;
  }
  .act .icon-btn {
    width: 22px;
    height: 20px;
  }
  .dim {
    color: var(--text-faint);
  }
  .empty {
    padding: 16px;
    text-align: center;
    color: var(--text-faint);
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
  /* dialog */
  .form {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .lbl {
    width: 40px;
    color: var(--text-dim);
  }
  .row input[type='text'] {
    flex: 1;
  }
  .chk {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .pick {
    display: flex;
    gap: 10px;
    margin-top: 4px;
  }
  .side {
    flex: 1;
    min-width: 0;
  }
  .cap {
    margin-bottom: 4px;
    font-weight: 600;
  }
  .list {
    height: 260px;
    overflow: auto;
    padding: 3px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-input);
  }
  .col {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 3px 8px;
    border: none;
    border-radius: 4px;
    background: none;
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }
  .col:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .col.on {
    opacity: 0.4;
    cursor: default;
  }
  .ty {
    margin-left: auto;
    color: var(--syn-type);
    font-size: 11.5px;
  }
  .sel-col {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 2px 4px 2px 8px;
  }
  .ord {
    width: 16px;
    color: var(--text-faint);
    font-size: 11px;
  }
  .dir {
    min-width: 42px;
    padding: 1px 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-elev);
    color: var(--syn-keyword);
    font-family: var(--font-mono);
    font-size: 11px;
    cursor: pointer;
  }
  .none {
    padding: 12px;
    color: var(--text-faint);
    text-align: center;
  }
  .ferr {
    color: var(--warn);
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
