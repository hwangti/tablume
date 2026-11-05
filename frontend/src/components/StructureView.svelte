<script lang="ts">
  // The table's columns as a table (the "컬럼" side of the structure tab). Type,
  // NULL, default and comment can be edited, columns added and dropped, the
  // table comment changed, and on MySQL/MariaDB the rows dragged into
  // another order: the changes become ALTER statements that are shown
  // before they run.
  import { t } from '../lib/i18n.svelte'
  import { api, errText, type TableOptions } from '../lib/api'
  import { copyText } from '../lib/clipboard'
  import { toast, openConsole, retargetTable } from '../lib/state.svelte'
  import type { Column, Table } from '../lib/types'
  import Icon from './Icon.svelte'
  import SqlPreview from './SqlPreview.svelte'
  import Modal from './Modal.svelte'
  import Dropdown from './Dropdown.svelte'
  import CellViewer from './CellViewer.svelte'
  import ColumnDialog, { type ColumnForm } from './ColumnDialog.svelte'

  let {
    connId,
    schema,
    table,
    driver = '',
    version = 0,
    onchanged,
    onlog,
    onkey,
    create = false,
    oncreated,
  }: {
    connId: string
    schema: string
    table: Table
    driver?: string
    version?: number
    onchanged?: () => void
    onlog?: (sql: string[], outcome: string, error?: string) => void
    onkey?: (kind: 'fk' | 'index', column: string) => void // a key icon was clicked
    // create: a table being designed (no columns yet; "만들기" runs CREATE TABLE)
    create?: boolean
    oncreated?: (t: Table) => void
  } = $props()

  let cols = $state<Column[] | null>(null)
  let indexed = $state(new Set<string>())
  let error = $state('')
  let loadedFor = ''

  function load() {
    const key = `${connId}|${schema}|${table.schema}.${table.name}`
    loadedFor = key
    cols = null
    error = ''
    revert()
    opts = null
    const tb = $state.snapshot(table)
    if (create) {
      cols = []
      firstRow()
      if (driver === 'mysql' || driver === 'mariadb')
        api
          .TableOptions(connId, schema, tb)
          .then((o) => key === loadedFor && (opts = o))
          .catch(() => {})
      return
    }
    api
      .ListColumns(connId, schema, tb)
      .then((c) => key === loadedFor && (cols = c ?? []))
      .catch((e) => key === loadedFor && (error = errText(e)))
    // columns in a plain (not unique) index, for the index icon
    api
      .ListIndexes(connId, schema, tb)
      .then((ix) => {
        if (key !== loadedFor) return
        const plain = (s: string) => s.replace(/ DESC$/, '').replace(/\(\d+\)$/, '').replace(/^["`[](.*)["`\]]$/, '$1')
        indexed = new Set((ix ?? []).filter((x) => !x.unique).flatMap((x) => x.columns.map(plain)))
      })
      .catch(() => {})
    if (driver === 'mysql' || driver === 'mariadb')
      api
        .TableOptions(connId, schema, tb)
        .then((o) => key === loadedFor && (opts = o))
        .catch(() => {})
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
  type Field = 'name' | 'type' | 'nullable' | 'default' | 'comment' | 'auto' | 'collation' | 'onUpdate' | 'invisible'
  type Draft = { name: string; type: string; nullable: boolean; default: string; comment: string; auto: boolean; collation: string; onUpdate: string; invisible: boolean }
  // MySQL/MariaDB: a column's collation and ON UPDATE as the server has them
  const colOpt = (c: Column) => opts?.columns?.[c.name] ?? { collation: '', onUpdate: '', invisible: false }
  const isAuto = (c: Column) => /auto_increment|identity/i.test(c.extra ?? '')
  let draft = $state<Record<string, Draft>>({})
  let editing = $state<{ name: string; field: Exclude<Field, 'nullable' | 'auto' | 'invisible'>; value: string } | null>(null)

  let readOnlyReason = $derived(
    driver === 'sqlite'
      ? t('SQLite는 컬럼을 바꾸려면 테이블을 새로 만들어야 해서 여기서는 편집할 수 없습니다')
      : table.type.toUpperCase().includes('VIEW')
        ? t('뷰는 편집할 수 없습니다')
        : '',
  )

  // a default is an SQL expression; NULL (or nothing) is no default, which a
  // nullable column reads as DEFAULT NULL (the text 'NULL' is quoted)
  const defaultHint = t("SQL 식입니다. 문자열은 'a'처럼 따옴표로 감쌉니다. 비우거나 NULL이면 기본값 없음 (NULL 허용 컬럼은 NULL)")
  const noDefault = (v: string) => (v.trim().toUpperCase() === 'NULL' ? '' : v.trim())
  const def = (c: Column) => noDefault(c.default ?? '')

  // ---- dropping columns
  let dropped = $state<Record<string, true>>({})
  function toggleDrop(c: Column) {
    if (dropped[c.name]) delete dropped[c.name]
    else dropped[c.name] = true
  }

  // ---- the table's name, comment and engine (null = unchanged)
  let opts = $state<TableOptions | null>(null)
  let engineDraft = $state<string | null>(null)
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
    return draft[c.name] ?? { name: c.name, type: c.type, nullable: c.nullable, default: def(c), comment: c.comment ?? '', auto: isAuto(c), ...colOpt(c) }
  }

  function changed(c: Column, f: Field): boolean {
    const d = draft[c.name]
    if (!d) return false
    if (f === 'name') return d.name.trim() !== c.name
    if (f === 'auto') return d.auto !== isAuto(c)
    if (f === 'collation' || f === 'onUpdate') return d[f].trim() !== colOpt(c)[f]
    if (f === 'invisible') return !!d.invisible !== !!colOpt(c).invisible
    if (f === 'nullable') return d.nullable !== c.nullable
    if (f === 'default') return d.default.trim() !== def(c)
    if (f === 'type') return d.type.trim().toLowerCase() !== c.type.toLowerCase()
    return d.comment !== (c.comment ?? '')
  }

  const FIELDS: Field[] = ['name', 'type', 'nullable', 'default', 'comment', 'auto', 'collation', 'onUpdate', 'invisible']
  function setField(c: Column, f: Field, v: string | boolean) {
    setDraft(c, { ...current(c), [f]: v })
  }
  function setDraft(c: Column, d: Draft) {
    d = { ...d, default: noDefault(d.default) }
    draft[c.name] = d
    if (!FIELDS.some((x) => changed(c, x))) delete draft[c.name]
  }

  // ---- ✎: a column in the detail dialog
  let detail = $state<{ title: string; value: ColumnForm; autoReason: string; info: [string, string][]; save: (v: ColumnForm) => void } | null>(null)
  const mysqlish = $derived(driver === 'mysql' || driver === 'mariadb')
  const autoEditable = $derived(driver === 'mysql' || driver === 'mariadb' || driver === 'postgres')
  function openDetail(c: Column) {
    const info: [string, string][] = []
    const keys = [c.pk && 'PK', c.unique && 'UNIQUE', c.fk && 'FK', indexed.has(c.name) && 'INDEX'].filter(Boolean).join(', ')
    if (keys) info.push([t('키'), keys])
    if (c.fk) info.push([t('참조'), c.fk])
    if (c.extra) info.push([t('기타'), c.extra])
    detail = {
      title: `${t('컬럼 수정')} — ${c.name}`,
      value: current(c),
      autoReason: autoEditable ? '' : t('이 DB에서는 기존 컬럼의 자동 증가를 바꿀 수 없습니다 (테이블을 새로 만들어야 합니다)'),
      info,
      save: (v) => setDraft(c, v),
    }
  }
  function openDetailNew(a: NewCol) {
    detail = { title: t('새 컬럼'), value: { ...a }, autoReason: '', info: [], save: (v) => Object.assign(a, v) }
  }
  // changes the table has no column for (the number cell is marked)
  function hiddenChanges(c: Column): string {
    const out: string[] = []
    if (changed(c, 'collation')) out.push(t('콜레이션'))
    if (changed(c, 'onUpdate')) out.push('ON UPDATE')
    if (changed(c, 'invisible')) out.push(t('숨김 (INVISIBLE)'))
    return out.join(', ')
  }

  // what the 기타 column shows: the auto increment as it will be
  function extraShown(c: Column, d: Draft): string {
    if (d.auto === isAuto(c)) return c.extra ?? ''
    return d.auto ? (driver === 'postgres' ? 'identity by default' : 'auto_increment') : ''
  }

  // new columns; id keeps a row's place while its name is being typed
  // pk: part of the primary key (a table being made); prefill: the row a new
  // table starts with (not focused: the table's name is typed first)
  type NewCol = Draft & { name: string; id: number; pk?: boolean; prefill?: boolean }
  let added = $state<NewCol[]>([])
  let nextId = 0
  const newKey = (a: NewCol) => '\0' + a.id
  function addRow() {
    const a = { id: ++nextId, name: '', type: '', nullable: true, default: '', comment: '', auto: false, collation: '', onUpdate: '', invisible: false }
    added.push(a)
    order?.push(newKey(a))
  }
  function firstRow() {
    const type = driver === 'oracle' ? 'NUMBER' : driver === 'sqlite' ? 'integer' : 'bigint'
    added.push({ id: ++nextId, name: 'id', type, nullable: false, default: '', comment: '', auto: true, collation: '', onUpdate: '', invisible: false, pk: true, prefill: true })
  }
  function togglePk(a: NewCol) {
    a.pk = !a.pk
    if (a.pk) a.nullable = false // a key column is NOT NULL
  }
  function removeNew(a: NewCol) {
    added = added.filter((x) => x.id !== a.id)
    if (order) order = order.filter((k) => k !== newKey(a))
  }
  let addError = $derived.by(() => {
    if (create && !(nameDraft ?? '').trim()) return t('테이블 이름을 넣으세요')
    if (create && !added.length) return t('컬럼을 하나 이상 넣으세요')
    if (nameDraft !== null && !nameDraft.trim()) return t('테이블 이름이 비어 있습니다')
    const seen = new Set<string>()
    for (const c of cols ?? []) {
      if (dropped[c.name]) continue
      const n = current(c).name.trim()
      if (!n) return t('{name}: 새 이름이 비어 있습니다', { name: c.name })
      if (seen.has(n.toLowerCase())) return t('같은 이름의 컬럼이 이미 있습니다: {name}', { name: n })
      seen.add(n.toLowerCase())
    }
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
    engineDraft = null
  }

  // ---- order (MySQL/MariaDB): a row is dragged by its number
  // (a table being made: the order is just the columns' order)
  const canMove = $derived(!readOnlyReason && !find && (create || driver === 'mysql' || driver === 'mariadb'))
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
  let pending = $derived(edits + (order ? 1 : 0) + (tcDraft !== null ? 1 : 0) + (nameDraft !== null ? 1 : 0) + (engineDraft !== null ? 1 : 0))

  function startEdit(c: Column, field: Exclude<Field, 'nullable' | 'auto' | 'invisible'>) {
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

  // ---- "…": a comment in the large editor (as in the data grid)
  let viewing = $state<{ title: string; value: string; save: (v: string) => void } | null>(null)
  function bigComment(c: Column) {
    commitEdit()
    viewing = { title: `${c.name} — ${t('코멘트')}`, value: current(c).comment, save: (v) => setField(c, 'comment', v) }
  }

  // ⌘C / Ctrl+C on a selected row: the row as tab-separated text
  function wrapKey(e: KeyboardEvent) {
    const r = shown[sel]
    if ((e.metaKey || e.ctrlKey) && e.key === 'c' && r?.col && !editing && !(e.target instanceof HTMLInputElement)) {
      e.preventDefault()
      copyRow(r.col)
    }
  }

  function inputKey(e: KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'F2' && editing?.field === 'comment') {
      e.preventDefault()
      const c = cols?.find((x) => x.name === editing!.name)
      if (c) bigComment(c)
    } else if (e.key === 'Enter') {
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
  function focusIf(node: HTMLInputElement, on: boolean) {
    if (on) focus(node)
  }

  // with a new order the server needs every column (it restates the moved ones)
  function changes() {
    return (cols ?? [])
      .filter((c) => !dropped[c.name] && (order || draft[c.name]))
      .map((c) => {
        const d = current(c)
        return {
          old: { ...$state.snapshot(c), ...colOpt(c) },
          new: { ...$state.snapshot(c), collation: d.collation.trim(), onUpdate: d.onUpdate.trim(), invisible: d.invisible, extra: d.auto === isAuto(c) ? c.extra : d.auto ? 'auto_increment' : '', name: d.name.trim(), type: d.type.trim(), nullable: d.nullable, default: d.default.trim(), comment: d.comment },
        }
      })
  }

  function request() {
    const add = added.map((a) => ({ name: a.name.trim(), type: a.type.trim(), nullable: a.nullable, default: noDefault(a.default), comment: a.comment, pk: false, unique: false, fk: '', extra: a.auto ? 'auto_increment' : '', collation: a.collation, onUpdate: a.onUpdate.trim(), invisible: a.invisible }))
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
      engine: engineDraft ?? undefined,
    }
  }

  // ---- apply: preview the statements, then run them
  let apply = $state<{ sql: string[]; busy: boolean; error: string; note: string } | null>(null)
  const transactional = $derived(driver === 'postgres' || driver === 'mssql')

  const newColumn = (a: NewCol): Column => ({
    name: a.name.trim(),
    type: a.type.trim(),
    nullable: a.nullable,
    default: noDefault(a.default),
    comment: a.comment,
    pk: !!a.pk,
    unique: false,
    fk: '',
    extra: a.auto ? 'auto_increment' : '',
    collation: a.collation,
    onUpdate: a.onUpdate.trim(),
    invisible: a.invisible,
  })

  function createRequest() {
    return {
      connId,
      schema,
      table: { ...$state.snapshot(table), name: (nameDraft ?? '').trim(), comment: tcDraft ?? '' },
      columns: rows.flatMap((r) => (r.add ? [newColumn(r.add)] : [])),
      engine: engineDraft ?? '',
    }
  }

  async function openApply() {
    try {
      const sql = create ? await api.PreviewCreateTable(createRequest()) : await api.PreviewAlter(request())
      apply = { sql: sql ?? [], busy: false, error: '', note: '' }
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  async function doApply() {
    const a = apply
    if (!a) return
    a.busy = true
    if (create) return made(a)
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

  // CREATE TABLE ran (all of it, or the table and some comments): the tab
  // becomes the new table's
  async function made(a: NonNullable<typeof apply>) {
    try {
      const res = await api.CreateTable(createRequest())
      const r = res.result
      if (r.error) {
        onlog?.(a.sql.slice(0, r.rolledBack ? a.sql.length : r.applied + 1), '', r.error)
        a.busy = false
        a.error = r.error
        a.note = r.rolledBack || !r.applied ? t('하나라도 실패하면 전부 취소됩니다. 아무것도 바뀌지 않았습니다.') : t('테이블은 만들어졌고, 뒤의 문장 일부가 실패했습니다.')
        if (!r.rolledBack && r.applied > 0) oncreated?.(res.table)
        return
      }
      apply = null
      onlog?.(a.sql, t('테이블 만듦: {name}', { name: res.table.name }))
      toast(t('테이블 만듦: {name}', { name: res.table.name }))
      oncreated?.(res.table)
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
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_static_element_interactions -->
  <div class="wrap" tabindex="-1" onkeydown={wrapKey}>
    <div class="props">
      <label class="prop">
        <span>{t('테이블')}</span>
        <input
          class="mono"
          class:edited={nameDraft !== null}
          value={nameDraft ?? table.name}
          placeholder={create ? t('새 테이블 이름') : ''}
          use:focusIf={create}
          disabled={!!readOnlyReason}
          oninput={(e) => (nameDraft = e.currentTarget.value === table.name ? null : e.currentTarget.value)}
          onkeydown={(e) => propKey(e, () => (nameDraft = null))}
          spellcheck="false"
        />
      </label>
      <label class="prop wide">
        <span>{t('코멘트')}</span>
        <span class="with-dots">
          <input
            class:edited={tcDraft !== null}
            value={tcDraft ?? tableComment}
            placeholder={readOnlyReason ? '' : t('테이블 코멘트 없음')}
            disabled={!!readOnlyReason}
            oninput={(e) => (tcDraft = e.currentTarget.value === tableComment ? null : e.currentTarget.value)}
            onkeydown={(e) => propKey(e, () => (tcDraft = null))}
          />
          {#if !readOnlyReason}
            <button
              class="dots"
              title={t('큰 창에서 편집')}
              onclick={(e) => (e.preventDefault(), (viewing = { title: t('테이블 코멘트'), value: tcDraft ?? tableComment, save: (v) => (tcDraft = v === tableComment ? null : v) }))}>…</button
            >
          {/if}
        </span>
      </label>
      {#if opts?.engines?.length}
        <div class="prop">
          <span>{t('엔진')}</span>
          <span class="engine" class:edited={engineDraft !== null}>
            <Dropdown
              value={engineDraft ?? opts.engine}
              placeholder={t('기본값')}
              options={opts.engines.map((e) => ({ value: e, label: e }))}
              onchange={(v) => (engineDraft = v === opts?.engine ? null : v)}
              width="140px"
            />
          </span>
        </div>
      {/if}
    </div>
    <div class="bar">
      <span class="title">{t('컬럼')} <span class="count">{create ? added.length : cols.length}</span></span>
      {#if !readOnlyReason}
        <button class="btn mini" onclick={addRow}><Icon name="plus" size={13} /> {t('컬럼 추가')}</button>
      {/if}
      <input class="find" type="search" placeholder={t('컬럼 찾기')} bind:value={find} onkeydown={(e) => (e.stopPropagation(), e.key === 'Escape' && (find = ''))} />
      <span class="grow"></span>
      {#if readOnlyReason}
        <span class="ro" title={readOnlyReason}><Icon name="lock" size={12} /> {t('읽기 전용')}</span>
      {:else if create}
        {#if addError}<span class="adderr">{addError}</span>{/if}
        <button class="btn mini primary" onclick={openApply} disabled={!!addError} title={addError}><Icon name="check" size={13} /> {t('만들기')}</button>
      {:else if pending > 0}
        {#if addError}<span class="adderr">{addError}</span>{/if}
        {#if edits}<span class="pending">{t('컬럼 {n}개 변경', { n: edits })}</span>{/if}
        {#if order}<span class="pending">{t('순서 변경')}</span>{/if}
        {#if nameDraft !== null}<span class="pending">{t('이름 변경')}</span>{/if}
        {#if tcDraft !== null}<span class="pending">{t('테이블 코멘트 변경')}</span>{/if}
        {#if engineDraft !== null}<span class="pending">{t('엔진 변경')}</span>{/if}
        <button class="btn mini primary" onclick={openApply} disabled={!!addError} title={addError}><Icon name="check" size={13} /> {t('적용')}</button>
        <button class="btn mini" onclick={revert}><Icon name="undo" size={13} /> {t('되돌리기')}</button>
      {:else}
        <span class="hint">{canMove ? t('이름·타입·NULL·기본값·코멘트는 더블클릭, 순서는 번호를 끌어서 바꿀 수 있습니다') : t('이름·타입·NULL·기본값·코멘트를 더블클릭해서 바꿀 수 있습니다')}</span>
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
          <th>{t('코멘트')}</th>
          <th class="act"></th>
        </tr>
      </thead>
      <tbody bind:this={tbody}>
        <!-- marked when moved or changed (hidden: what changed that the table doesn't show) -->
        {#snippet num(i: number, key: string, label: string | number, hidden = '', touched = false)}
          <td
            class="n"
            class:grip={canMove}
            class:edited={moved[key] || touched || !!hidden}
            title={[hidden && t('바뀜: {what}', { what: hidden }), canMove && t('끌어서 순서 바꾸기')].filter(Boolean).join('\n') || undefined}
            onpointerdown={(e) => grab(e, key)}>{label}</td
          >
        {/snippet}
        {#each shown as r, i (r.key)}
          {#if r.col}
          {@const c = r.col}
          {@const cur = current(c)}
          <tr class:sel={sel === i} class:dropped={dropped[c.name]} class:drop={drag?.to === i} class:drop-end={drag?.to === rows.length && i === rows.length - 1} class:dragging={drag?.key === r.key} onclick={() => (sel = i)}>
            {@render num(i, r.key, rowNo.get(r.key) ?? i + 1, hiddenChanges(c), !!draft[c.name])}
            <td class="k">
              {#if c.pk}<button class="kb" title={t('기본 키 (PK)')} onclick={() => onkey?.('index', c.name)}><Icon name="key" size={12} color="var(--key-pk)" /></button>{/if}
              {#if c.fk}<button class="kb" title="{t('외래 키 (FK)')} → {c.fk}" onclick={() => onkey?.('fk', c.name)}><Icon name="link" size={12} color="var(--key-fk)" /></button>{/if}
              {#if c.unique}<button class="kb" title={t('유니크 (UK)')} onclick={() => onkey?.('index', c.name)}><Icon name="unique" size={12} color="var(--key-uk)" /></button>{/if}
              {#if indexed.has(c.name)}<button class="kb" title={t('인덱스')} onclick={() => onkey?.('index', c.name)}><Icon name="sortLines" size={12} color="var(--text-dim)" /></button>{/if}
            </td>
            {#snippet cell(field: 'name' | 'type' | 'default' | 'comment', cls: string, shown: string, empty = '')}
              <td class="{cls} edit" class:edited={changed(c, field)} title={field === 'default' ? defaultHint : undefined} ondblclick={() => startEdit(c, field)}>
                {#if editing?.name === c.name && editing.field === field}
                  <span class="edit-wrap">
                    <input class="cell-input mono" class:with-dots={field === 'comment'} bind:value={editing.value} use:focus onkeydown={inputKey} onblur={commitEdit} spellcheck="false" />
                    {#if field === 'comment'}<button
                        class="dots"
                        title="{t('큰 창에서 편집')} (F2)"
                        onmousedown={(e) => e.preventDefault()}
                        ondblclick={(e) => e.stopPropagation()}
                        onclick={(e) => (e.stopPropagation(), bigComment(c))}>…</button
                      >{/if}
                  </span>
                {:else if shown}{shown}{:else}<span class="dim">{empty}</span>{/if}
              </td>
            {/snippet}
            {@render cell('name', 'name mono', cur.name)}
            {@render cell('type', 'type mono', cur.type.toLowerCase())}
            <td class="c" class:edited={changed(c, 'nullable')}>
              <label class="nullbox" title={readOnlyReason || t('NULL 허용')}>
                <input type="checkbox" checked={cur.nullable} disabled={!!readOnlyReason} onchange={(e) => setField(c, 'nullable', (e.currentTarget as HTMLInputElement).checked)} />
                {#if cur.nullable}<span class="dim">NULL</span>{:else}<b>NOT NULL</b>{/if}
              </label>
            </td>
            {@render cell('default', 'mono def', cur.default, cur.nullable ? 'NULL' : '')}
            <td class="dim" class:edited={changed(c, 'auto')}>{extraShown(c, cur)}</td>
            {@render cell('comment', 'comment', cur.comment)}
            <td class="act">
              {#if !readOnlyReason}
                {#if dropped[c.name]}
                  <button class="icon-btn sm" title={t('삭제 취소')} onclick={() => toggleDrop(c)}><Icon name="undo" size={13} /></button>
                {:else}
                  <button class="icon-btn sm" title={t('수정')} onclick={() => openDetail(c)}><Icon name="edit" size={13} /></button>
                  <button class="icon-btn sm" title={t('컬럼 삭제')} onclick={() => toggleDrop(c)}><Icon name="trash" size={13} /></button>
                {/if}
              {/if}
            </td>
          </tr>
          {:else if r.add}
          {@const a = r.add}
          <tr class="added" class:drop={drag?.to === i} class:drop-end={drag?.to === rows.length && i === rows.length - 1} class:dragging={drag?.key === r.key}>
            {@render num(i, r.key, '+')}
            <td class="k">
              {#if create}<button class="kb" title={t('기본 키 (PK)')} onclick={() => togglePk(a)}
                  ><Icon name="key" size={12} color={a.pk ? 'var(--key-pk)' : 'var(--text-faint)'} /></button
                >{/if}
            </td>
            <td class="new"><input class="new-input mono" placeholder={t('이름')} bind:value={a.name} use:focusIf={!a.prefill} onkeydown={(e) => e.stopPropagation()} spellcheck="false" /></td>
            <td class="new"><input class="new-input mono" placeholder={t('타입')} title={t('예: varchar(50)')} bind:value={a.type} onkeydown={(e) => e.stopPropagation()} spellcheck="false" /></td>
            <td class="c">
              <label class="nullbox" title={t('NULL 허용')}>
                <input type="checkbox" bind:checked={a.nullable} />
                {#if a.nullable}<span class="dim">NULL</span>{:else}<b>NOT NULL</b>{/if}
              </label>
            </td>
            <td class="new"><input class="new-input mono" placeholder={t('기본값')} title={defaultHint} bind:value={a.default} onkeydown={(e) => e.stopPropagation()} spellcheck="false" /></td>
            <td class="dim">{a.auto ? 'auto_increment' : ''}</td>
            <td class="new rel">
              <input class="new-input with-dots" placeholder={t('코멘트')} bind:value={a.comment} onkeydown={(e) => e.stopPropagation()} />
              <button class="dots" title={t('큰 창에서 편집')} onclick={() => (viewing = { title: `${a.name || t('새 컬럼')} — ${t('코멘트')}`, value: a.comment, save: (v) => (a.comment = v) })}>…</button>
            </td>
            <td class="act">
              <button class="icon-btn sm" title={t('수정')} onclick={() => openDetailNew(a)}><Icon name="edit" size={13} /></button>
              <button class="icon-btn sm" title={t('추가 취소')} onclick={() => removeNew(a)}><Icon name="close" size={13} /></button>
            </td>
          </tr>
          {/if}
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if detail}
  <ColumnDialog
    title={detail.title}
    value={detail.value}
    autoReason={detail.autoReason}
    info={detail.info}
    collations={mysqlish ? (opts?.collations ?? []) : null}
    onsave={detail.save}
    onclose={() => (detail = null)}
  />
{/if}

{#if viewing}
  <CellViewer
    column={viewing.title}
    value={viewing.value}
    editable
    nullable={false}
    onsave={(v) => viewing?.save(v ?? '')}
    onclose={() => (viewing = null)}
  />
{/if}

{#if apply}
  <Modal title={create ? t('테이블 만들기 — {n}개 문장', { n: apply.sql.length }) : t('컬럼 변경 적용 — {n}개 문장', { n: apply.sql.length })} onclose={() => (apply = null)} width={760} dismissible={false} onsubmit={() => apply && !apply.busy && !apply.error && doApply()}>
    <p class="note">
      {#if transactional}
        {t('아래 문장을 하나의 트랜잭션으로 실행합니다. 하나라도 실패하면 모두 취소됩니다.')}
      {:else}
        {t('아래 문장을 차례로 실행합니다. 이 DB는 DDL을 되돌릴 수 없어서, 중간에 실패하면 그 앞까지는 적용된 채로 남습니다.')}
      {/if}
      {#if engineDraft !== null && !create}
        {t('엔진을 바꾸면 테이블을 통째로 다시 씁니다. 행이 많으면 오래 걸리고 그동안 쓰기가 막힐 수 있습니다.')}
      {:else if order}
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
    white-space: nowrap;
    font-size: 0; /* no gap between the buttons */
  }
  tr.dropped td:not(.act) {
    text-decoration: line-through;
    opacity: 0.5;
  }
  .props {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 20px;
    padding: 8px 12px;
    margin-bottom: 4px;
    background: var(--bg-panel);
  }
  .wrap:focus {
    outline: none;
  }
  .with-dots {
    position: relative;
    display: flex;
    flex: 1;
  }
  .rel {
    position: relative;
  }
  input.with-dots,
  .with-dots input {
    padding-right: 28px;
  }
  .dots {
    position: absolute;
    top: 50%;
    right: 3px;
    transform: translateY(-50%);
    height: 18px;
    padding: 0 5px;
    border: 1px solid var(--border);
    border-radius: 3px;
    background: var(--bg-elev);
    color: var(--text-dim);
    font-size: 11px;
    line-height: 1;
    cursor: pointer;
  }
  .dots:hover {
    color: var(--text);
    background: var(--bg-hover);
  }
  /* the input as a block (no line box under it), "…" centred on it */
  .edit-wrap {
    position: relative;
    display: flex; /* (a block one gets an empty line for the "…" button) */
  }
  .edit-wrap .cell-input {
    flex: 1;
  }
  .engine.edited :global(.dd) {
    background: color-mix(in srgb, var(--warn) 18%, var(--bg-input));
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
  .kb {
    display: inline-flex;
    vertical-align: middle;
    padding: 2px;
    border: none;
    border-radius: 3px;
    background: none;
    cursor: pointer;
  }
  .kb:hover {
    background: var(--bg-hover);
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
  /* a comment over several lines keeps the rows' spacing above and below */
  .comment {
    white-space: pre-wrap;
    min-width: 200px;
    padding-top: 4px;
    padding-bottom: 4px;
    line-height: 1.3;
    color: var(--text-dim);
  }
  .comment:has(.cell-input) {
    padding: 0;
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
