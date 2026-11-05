<script lang="ts">
  // The table's columns as a table (the "컬럼" side of the structure tab). Type,
  // NULL, default and comment can be edited, columns added and dropped, the
  // table comment changed, and on MySQL/MariaDB the rows dragged into
  // another order: the changes become ALTER statements that are shown
  // before they run.
  import { t } from '../lib/i18n.svelte'
  import { api, errText } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { toast, openConsole, retargetTable } from '../lib/state.svelte'
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
    revert()
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
    else if (v !== loadedVersion && !pending) load()
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

  // ---- dropping columns
  let dropped = $state<Record<string, true>>({})
  function toggleDrop(c: Column) {
    if (dropped[c.name]) delete dropped[c.name]
    else dropped[c.name] = true
  }

  // ---- the table's name and comment (null = unchanged)
  let nameDraft = $state<string | null>(null)
  let tcDraft = $state<string | null>(null)
  let tableComment = $derived(table.comment ?? '')
  function propKey(e: KeyboardEvent & { currentTarget: HTMLInputElement }, reset: () => void) {
    e.stopPropagation()
    if (e.key === 'Enter') e.currentTarget.blur()
    if (e.key === 'Escape') (reset(), e.currentTarget.blur())
  }

  // ---- finding a column (name or comment)
  let find = $state('')

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

  // new columns; id keeps a row's place while its name is being typed
  type NewCol = Draft & { name: string; id: number }
  let added = $state<NewCol[]>([])
  let nextId = 0
  const newKey = (a: NewCol) => '\0' + a.id
  function addRow() {
    const a = { id: ++nextId, name: '', type: '', nullable: true, default: '', comment: '' }
    added.push(a)
    order?.push(newKey(a))
  }
  function removeNew(a: NewCol) {
    added = added.filter((x) => x.id !== a.id)
    if (order) order = order.filter((k) => k !== newKey(a))
  }
  let addError = $derived.by(() => {
    if (nameDraft !== null && !nameDraft.trim()) return t('테이블 이름이 비어 있습니다')
    const seen = new Set((cols ?? []).map((c) => c.name.toLowerCase()))
    for (const a of added) {
      const n = a.name.trim()
      if (!n) return t('새 컬럼의 이름이 비어 있습니다')
      if (!a.type.trim()) return t('{name}: 타입이 비어 있습니다', { name: n })
      if (seen.has(n.toLowerCase())) return t('같은 이름의 컬럼이 이미 있습니다: {name}', { name: n })
      seen.add(n.toLowerCase())
    }
    return ''
  })
  function revert() {
    draft = {}
    added = []
    order = null
    moved = {}
    dropped = {}
    tcDraft = null
    nameDraft = null
  }

  // ---- order (MySQL/MariaDB): a row is dragged by its number
  const canMove = $derived(!readOnlyReason && !find && (driver === 'mysql' || driver === 'mariadb'))
  let order = $state<string[] | null>(null) // row keys; null = as loaded, new ones last
  let moved = $state<Record<string, true>>({}) // rows the user dragged
  type Row = { key: string; col?: Column; add?: NewCol }
  let rows = $derived.by((): Row[] => {
    const all: Row[] = [...(cols ?? []).map((c) => ({ key: c.name, col: c })), ...added.map((a) => ({ key: newKey(a), add: a }))]
    if (!order) return all
    const by = new Map(all.map((r) => [r.key, r]))
    return order.flatMap((k) => by.get(k) ?? [])
  })
  let shown = $derived.by(() => {
    const f = find.trim().toLowerCase()
    if (!f) return rows
    return rows.filter((r) => (r.col ? r.col.name + ' ' + (r.col.comment ?? '') : r.add!.name).toLowerCase().includes(f))
  })
  let rowNo = $derived(new Map(rows.map((r, i) => [r.key, i + 1])))

  let tbody = $state<HTMLTableSectionElement>()
  let drag = $state<{ key: string; to: number } | null>(null)

  function grab(e: PointerEvent, key: string) {
    if (!canMove || e.button !== 0 || !tbody) return
    e.preventDefault()
    const body = tbody
    let sc = body.parentElement
    while (sc && !(sc.scrollHeight > sc.clientHeight && /auto|scroll/.test(getComputedStyle(sc).overflowY))) sc = sc.parentElement
    let y = e.clientY
    drag = { key, to: -1 }
    let raf = 0
    // the row the pointer is above the middle of (rows.length = after the last)
    const aim = () => {
      const trs = body.rows
      let to = trs.length
      for (let i = 0; i < trs.length; i++) {
        const r = trs[i].getBoundingClientRect()
        if (y < r.top + r.height / 2) {
          to = i
          break
        }
      }
      if (drag) drag.to = to
    }
    const tick = () => {
      if (sc) {
        // near the top (under the sticky header) or bottom edge: scroll
        const r = sc.getBoundingClientRect()
        if (y < r.top + 56) sc.scrollTop -= 14
        else if (y > r.bottom - 28) sc.scrollTop += 14
      }
      aim()
      raf = requestAnimationFrame(tick)
    }
    const move = (ev: PointerEvent) => (y = ev.clientY)
    const end = (drop: boolean) => {
      cancelAnimationFrame(raf)
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('keydown', key_, true)
      document.body.classList.remove('dragging-row')
      if (drop && drag && drag.to >= 0) moveTo(drag.key, drag.to)
      drag = null
    }
    const up = (ev: PointerEvent) => ((y = ev.clientY), aim(), end(true))
    const key_ = (ev: KeyboardEvent) => ev.key === 'Escape' && (ev.stopPropagation(), end(false))
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('keydown', key_, true)
    document.body.classList.add('dragging-row')
    tick()
  }

  function moveTo(key: string, to: number) {
    const keys = rows.map((r) => r.key)
    const from = keys.indexOf(key)
    if (from < 0 || to === from || to === from + 1) return
    keys.splice(from, 1)
    keys.splice(to > from ? to - 1 : to, 0, key)
    const natural = [...(cols ?? []).map((c) => c.name), ...added.map(newKey)]
    if (keys.every((k, i) => k === natural[i])) {
      order = null
      moved = {}
    } else {
      order = keys
      moved[key] = true
    }
    sel = keys.indexOf(key)
  }

  let edits = $derived((cols ? cols.filter((c) => draft[c.name] && !dropped[c.name]).length : 0) + added.length + Object.keys(dropped).length)
  let pending = $derived(edits + (order ? 1 : 0) + (tcDraft !== null ? 1 : 0) + (nameDraft !== null ? 1 : 0))

  function startEdit(c: Column, field: Exclude<Field, 'nullable'>) {
    if (readOnlyReason || dropped[c.name]) return
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

  // with a new order the server needs every column (it restates the moved ones)
  function changes() {
    return (cols ?? [])
      .filter((c) => !dropped[c.name] && (order || draft[c.name]))
      .map((c) => {
        const d = current(c)
        return {
          old: $state.snapshot(c),
          new: { ...$state.snapshot(c), type: d.type.trim(), nullable: d.nullable, default: d.default.trim(), comment: d.comment },
        }
      })
  }

  function request() {
    const add = added.map((a) => ({ name: a.name.trim(), type: a.type.trim(), nullable: a.nullable, default: a.default.trim(), comment: a.comment, pk: false, unique: false, fk: '', extra: '' }))
    const names = order ? rows.filter((r) => !(r.col && dropped[r.col.name])).map((r) => (r.col ? r.col.name : r.add!.name.trim())) : undefined
    return {
      connId,
      schema,
      table: $state.snapshot(table),
      changes: changes(),
      add,
      order: names,
      drop: Object.keys(dropped),
      tableComment: tcDraft ?? undefined,
      rename: nameDraft?.trim() || undefined,
    }
  }

  // ---- apply: preview the statements, then run them
  let apply = $state<{ sql: string[]; busy: boolean; error: string; note: string } | null>(null)
  const transactional = $derived(driver === 'postgres' || driver === 'mssql')

  async function openApply() {
    try {
      const sql = await api.PreviewAlter(request())
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
      const r = await api.ApplyAlter(request())
      if (r.error) {
        onlog?.(a.sql.slice(0, r.rolledBack ? a.sql.length : r.applied + 1), '', r.error + '\n' + (r.rolledBack ? t('(모두 취소됨)') : t('({a}/{n}개 적용됨)', { a: r.applied, n: r.total })))
        a.busy = false
        a.error = r.error
        a.note = r.rolledBack
          ? t('하나라도 실패하면 전부 취소됩니다. 아무것도 바뀌지 않았습니다.')
          : t('{n}개 중 {a}개는 이미 적용됐습니다 (이 DB는 DDL을 되돌릴 수 없습니다).', { n: r.total, a: r.applied })
        if (!r.rolledBack && r.applied > 0) {
          revert()
          onchanged ? onchanged() : load()
        }
        return
      }
      apply = null
      onlog?.(a.sql, t('컬럼 변경 {n}건 적용', { n: r.applied }))
      toast(t('컬럼 변경 {n}건 적용됨', { n: r.applied }))
      // the tab's table follows (this view's table prop is that object)
      const patch: { name?: string; comment?: string } = {}
      if (tcDraft !== null) patch.comment = tcDraft
      if (nameDraft?.trim()) patch.name = nameDraft.trim()
      const old = $state.snapshot(table)
      revert()
      if (patch.name || patch.comment !== undefined) retargetTable(connId, schema, old, patch)
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
    <div class="props">
      <label class="prop">
        <span>{t('테이블')}</span>
        <input
          class="mono"
          class:edited={nameDraft !== null}
          value={nameDraft ?? table.name}
          disabled={!!readOnlyReason}
          oninput={(e) => (nameDraft = e.currentTarget.value === table.name ? null : e.currentTarget.value)}
          onkeydown={(e) => propKey(e, () => (nameDraft = null))}
          spellcheck="false"
        />
      </label>
      <label class="prop wide">
        <span>{t('코멘트')}</span>
        <input
          class:edited={tcDraft !== null}
          value={tcDraft ?? tableComment}
          placeholder={readOnlyReason ? '' : t('테이블 코멘트 없음')}
          disabled={!!readOnlyReason}
          oninput={(e) => (tcDraft = e.currentTarget.value === tableComment ? null : e.currentTarget.value)}
          onkeydown={(e) => propKey(e, () => (tcDraft = null))}
        />
      </label>
    </div>
    <div class="bar">
      <span class="title">{t('컬럼')} <span class="count">{cols.length}</span></span>
      {#if !readOnlyReason}
        <button class="btn mini" onclick={addRow}><Icon name="plus" size={13} /> {t('컬럼 추가')}</button>
      {/if}
      <input class="find" type="search" placeholder={t('컬럼 찾기')} bind:value={find} onkeydown={(e) => (e.stopPropagation(), e.key === 'Escape' && (find = ''))} />
      <span class="grow"></span>
      {#if readOnlyReason}
        <span class="ro" title={readOnlyReason}><Icon name="lock" size={12} /> {t('읽기 전용')}</span>
      {:else if pending > 0}
        {#if addError}<span class="adderr">{addError}</span>{/if}
        {#if edits}<span class="pending">{t('컬럼 {n}개 변경', { n: edits })}</span>{/if}
        {#if order}<span class="pending">{t('순서 변경')}</span>{/if}
        {#if nameDraft !== null}<span class="pending">{t('이름 변경')}</span>{/if}
        {#if tcDraft !== null}<span class="pending">{t('테이블 코멘트 변경')}</span>{/if}
        <button class="btn mini primary" onclick={openApply} disabled={!!addError} title={addError}><Icon name="check" size={13} /> {t('적용')}</button>
        <button class="btn mini" onclick={revert}><Icon name="undo" size={13} /> {t('되돌리기')}</button>
      {:else}
        <span class="hint">{canMove ? t('타입·NULL·기본값·코멘트는 더블클릭, 순서는 번호를 끌어서 바꿀 수 있습니다') : t('타입·NULL·기본값·코멘트를 더블클릭해서 바꿀 수 있습니다')}</span>
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
          <th class="act"></th>
        </tr>
      </thead>
      <tbody bind:this={tbody}>
        {#snippet num(i: number, key: string, label: string | number)}
          <td class="n" class:grip={canMove} class:edited={moved[key]} title={canMove ? t('끌어서 순서 바꾸기') : undefined} onpointerdown={(e) => grab(e, key)}>{label}</td>
        {/snippet}
        {#each shown as r, i (r.key)}
          {#if r.col}
          {@const c = r.col}
          {@const cur = current(c)}
          <tr class:sel={sel === i} class:dropped={dropped[c.name]} class:drop={drag?.to === i} class:drop-end={drag?.to === rows.length && i === rows.length - 1} class:dragging={drag?.key === r.key} onclick={() => (sel = i)}>
            {@render num(i, r.key, rowNo.get(r.key) ?? i + 1)}
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
            <td class="act">
              {#if !readOnlyReason}
                {#if dropped[c.name]}
                  <button class="icon-btn sm" title={t('삭제 취소')} onclick={() => toggleDrop(c)}><Icon name="undo" size={13} /></button>
                {:else}
                  <button class="icon-btn sm" title={t('컬럼 삭제')} onclick={() => toggleDrop(c)}><Icon name="trash" size={13} /></button>
                {/if}
              {/if}
            </td>
          </tr>
          {:else if r.add}
          {@const a = r.add}
          <tr class="added" class:drop={drag?.to === i} class:drop-end={drag?.to === rows.length && i === rows.length - 1} class:dragging={drag?.key === r.key}>
            {@render num(i, r.key, '+')}
            <td class="k"></td>
            <td class="new"><input class="new-input mono" placeholder={t('이름')} bind:value={a.name} use:focus onkeydown={(e) => e.stopPropagation()} spellcheck="false" /></td>
            <td class="new"><input class="new-input mono" placeholder="varchar(50)" bind:value={a.type} onkeydown={(e) => e.stopPropagation()} spellcheck="false" /></td>
            <td class="c">
              <label class="nullbox" title={t('NULL 허용')}>
                <input type="checkbox" bind:checked={a.nullable} />
                {#if a.nullable}<span class="dim">NULL</span>{:else}<b>NOT NULL</b>{/if}
              </label>
            </td>
            <td class="new"><input class="new-input mono" placeholder={t('기본값')} bind:value={a.default} onkeydown={(e) => e.stopPropagation()} spellcheck="false" /></td>
            <td></td>
            <td></td>
            <td class="new"><input class="new-input" placeholder={t('코멘트')} bind:value={a.comment} onkeydown={(e) => e.stopPropagation()} /></td>
            <td class="act"><button class="icon-btn sm" title={t('추가 취소')} onclick={() => removeNew(a)}><Icon name="close" size={13} /></button></td>
          </tr>
          {/if}
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
      {#if order}
        {t('컬럼 위치를 옮기면 테이블을 통째로 다시 쓸 수 있습니다. 행이 많으면 오래 걸리고 그동안 쓰기가 막힐 수 있습니다.')}
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
  .act {
    width: 1%;
    padding: 0 4px;
  }
  tr.dropped td:not(.act) {
    text-decoration: line-through;
    opacity: 0.5;
  }
  .props {
    display: flex;
    gap: 16px;
    padding: 8px 12px 2px;
  }
  .prop {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-dim);
  }
  .prop.wide {
    flex: 1;
    max-width: 640px;
  }
  .prop input {
    height: 26px;
    width: 240px;
    color: var(--text);
  }
  .prop.wide input {
    flex: 1;
    width: auto;
  }
  .prop input.edited {
    background: color-mix(in srgb, var(--warn) 18%, var(--bg-input));
  }
  .find {
    width: 160px;
    height: 26px;
    padding: 0 7px;
    font-size: 12.5px;
  }
  td.grip {
    cursor: grab;
  }
  td.grip:hover {
    color: var(--text);
  }
  :global(body.dragging-row) {
    cursor: grabbing;
    user-select: none;
  }
  tr.dragging {
    opacity: 0.45;
  }
  tr.drop td {
    box-shadow: inset 0 2px 0 var(--accent);
  }
  tr.drop-end td {
    box-shadow: inset 0 -2px 0 var(--accent);
  }
  tr.added {
    background: color-mix(in srgb, var(--ok) 12%, transparent);
  }
  td.new {
    padding: 0;
  }
  .new-input {
    width: 100%;
    min-width: 110px;
    height: 23px;
    padding: 0 8px;
    border: none;
    border-radius: 0;
    background: transparent;
    font-size: 12.5px;
  }
  .new-input:focus {
    outline: none;
    box-shadow: inset 0 0 0 2px var(--accent);
  }
  .adderr {
    color: var(--danger);
    font-size: 11.5px;
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
