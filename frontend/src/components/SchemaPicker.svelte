<script lang="ts">
  // Picks the schemas a connection shows in the tree (DataGrip-style):
  // all of them, the session's default one, names one by one, or patterns.
  // Every change is saved with the connection right away.
  import Icon from './Icon.svelte'
  import {
    app,
    connById,
    globMatch,
    isSystemSchema,
    refreshSchemas,
    schemaFilter,
    schemaShown,
    setSchemaFilter,
  } from '../lib/state.svelte'
  import type { SchemaFilter } from '../lib/types'

  let { connId, x, y, onclose }: { connId: string; x: number; y: number; onclose: () => void } = $props()

  let search = $state('')
  let addingPattern = $state(false)
  let pattern = $state('')
  let refreshing = $state(false)
  let el: HTMLDivElement
  let patternEl = $state<HTMLInputElement>()

  let c = $derived(connById(connId))
  let st = $derived(app.conn[connId])
  let f = $derived(schemaFilter(c))
  let names = $derived(st?.schemas.map((s) => s.name) ?? [])
  let current = $derived(st?.current ?? '')
  let user = $derived(names.filter((n) => !isSystemSchema(c, n)))
  let system = $derived(names.filter((n) => isSystemSchema(c, n)))
  let shown = $derived(names.filter((n) => schemaShown(connId, n)).length)

  const q = (n: string) => !search.trim() || n.toLowerCase().includes(search.trim().toLowerCase())

  function save(next: Partial<SchemaFilter>) {
    setSchemaFilter(connId, { ...$state.snapshot(f), ...next })
  }

  // checked: picked by name, or covered by "all"; mixed: shown through the
  // default schema or a pattern only
  function stateOf(n: string): 'on' | 'mixed' | 'off' {
    if (f.names.includes(n) || (f.all && !isSystemSchema(c, n))) return 'on'
    if ((f.current && n === current) || f.patterns.some((p) => globMatch(p, n))) return 'mixed'
    return 'off'
  }

  function toggle(n: string) {
    if (f.all && !isSystemSchema(c, n)) {
      // leaving "all": keep the others, drop this one
      const keep = user.filter((u) => u !== n && u !== current)
      save({ all: false, names: [...new Set([...f.names.filter((x) => x !== n), ...keep])] })
      return
    }
    save({ names: f.names.includes(n) ? f.names.filter((x) => x !== n) : [...f.names, n] })
  }

  function setAll(on: boolean) {
    save(on ? { all: true, names: f.names.filter((n) => isSystemSchema(c, n)) } : { all: false })
  }

  function addPattern() {
    const p = pattern.trim()
    if (p && !f.patterns.includes(p)) save({ patterns: [...f.patterns, p] })
    pattern = ''
    addingPattern = false
  }

  async function refresh() {
    refreshing = true
    try {
      await refreshSchemas(connId)
    } finally {
      refreshing = false
    }
  }

  // keep the panel inside the window
  let pos = $state({ left: 0, top: 0 })
  $effect(() => {
    const w = 300
    const h = Math.min(520, window.innerHeight - 20)
    pos = { left: Math.max(8, Math.min(x, window.innerWidth - w - 8)), top: Math.max(8, Math.min(y, window.innerHeight - h - 8)) }
  })

  function outside(e: MouseEvent) {
    if (el && !el.contains(e.target as Node)) onclose()
  }
</script>

<svelte:window onmousedown={outside} onkeydown={(e) => e.key === 'Escape' && !addingPattern && onclose()} />

<div class="picker" bind:this={el} style:left="{pos.left}px" style:top="{pos.top}px">
  <div class="head">
    <span class="title">표시할 스키마</span>
    <span class="cnt">{shown}/{names.length}</span>
    <button class="icon-btn small" title="서버에서 스키마 목록 다시 가져오기" onclick={refresh} disabled={refreshing}>
      {#if refreshing}<span class="spin"></span>{:else}<Icon name="refresh" size={14} />{/if}
    </button>
  </div>
  <div class="search">
    <Icon name="filter" size={13} />
    <!-- svelte-ignore a11y_autofocus -->
    <input bind:value={search} placeholder="스키마 검색" spellcheck="false" autofocus />
  </div>

  <div class="list">
    <label class="opt">
      <input type="checkbox" checked={f.all} onchange={(e) => setAll((e.currentTarget as HTMLInputElement).checked)} />
      <span>전체 스키마</span>
      <span class="hint">시스템 스키마 제외</span>
    </label>
    <label class="opt">
      <input type="checkbox" checked={f.current} onchange={(e) => save({ current: (e.currentTarget as HTMLInputElement).checked })} />
      <span>기본 스키마</span>
      {#if current}<span class="hint">({current})</span>{/if}
    </label>
    {#each f.patterns as p (p)}
      <div class="opt pat">
        <Icon name="filter" size={12} />
        <span class="mono">{p}</span>
        <span class="hint">{names.filter((n) => globMatch(p, n)).length}개</span>
        <button class="x-btn" title="패턴 지우기" onclick={() => save({ patterns: f.patterns.filter((x) => x !== p) })}>
          <Icon name="close" size={12} />
        </button>
      </div>
    {/each}
    {#if addingPattern}
      <div class="opt pat">
        <input
          class="pat-input mono"
          bind:this={patternEl}
          bind:value={pattern}
          placeholder="예: HR_*, *_LOG"
          spellcheck="false"
          onkeydown={(e) => {
            if (e.key === 'Enter') addPattern()
            if (e.key === 'Escape') (e.stopPropagation(), (addingPattern = false), (pattern = ''))
          }}
          onblur={addPattern}
        />
      </div>
    {:else}
      <button class="link add" onclick={() => ((addingPattern = true), setTimeout(() => patternEl?.focus()))}>+ 패턴 추가</button>
    {/if}

    <div class="sep"></div>
    {#each user.filter(q) as n (n)}
      {@const s = stateOf(n)}
      <label class="item" class:dim={s === 'off'}>
        <input type="checkbox" checked={s === 'on'} indeterminate={s === 'mixed'} onchange={() => toggle(n)} />
        <span class="name">{n}</span>
        {#if n === current}<span class="hint">기본</span>{/if}
      </label>
    {/each}
    {#if system.filter(q).length}
      <div class="group">시스템 스키마</div>
      {#each system.filter(q) as n (n)}
        {@const s = stateOf(n)}
        <label class="item sys">
          <input type="checkbox" checked={s === 'on'} indeterminate={s === 'mixed'} onchange={() => toggle(n)} />
          <span class="name">{n}</span>
          {#if n === current}<span class="hint">기본</span>{/if}
        </label>
      {/each}
    {/if}
    {#if search && !names.some(q)}
      <div class="empty">일치하는 스키마가 없습니다</div>
    {/if}
  </div>
</div>

<style>
  .picker {
    position: fixed;
    z-index: 110;
    display: flex;
    flex-direction: column;
    width: 300px;
    max-height: min(520px, calc(100vh - 20px));
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 8px 4px 12px;
  }
  .title {
    font-weight: 600;
  }
  .cnt {
    flex: 1;
    color: var(--text-faint);
    font-size: 11.5px;
    font-variant-numeric: tabular-nums;
  }
  .small {
    width: 22px;
    height: 22px;
  }
  .search {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 4px 8px 6px;
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text-faint);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: none;
    padding: 4px 0;
    background: transparent;
  }
  .list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 4px 8px;
  }
  .opt,
  .item {
    display: flex;
    align-items: center;
    gap: 7px;
    height: 24px;
    padding: 0 8px;
    border-radius: 4px;
    white-space: nowrap;
    cursor: pointer;
  }
  .opt:hover,
  .item:hover {
    background: var(--bg-hover);
  }
  .pat {
    color: var(--text-dim);
    cursor: default;
  }
  .pat .x-btn {
    margin-left: auto;
  }
  .pat-input {
    flex: 1;
    min-width: 0;
    padding: 2px 6px;
  }
  .mono {
    font-family: var(--font-mono);
    font-size: 12px;
  }
  .hint {
    color: var(--text-faint);
    font-size: 11.5px;
  }
  .item .hint {
    margin-left: auto;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item.dim .name {
    color: var(--text-dim);
  }
  .item.sys .name {
    color: var(--text-faint);
  }
  .link {
    border: none;
    background: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 12px;
  }
  .add {
    margin: 2px 0 2px 30px;
    padding: 2px 0;
  }
  .add:hover {
    text-decoration: underline;
  }
  .sep {
    margin: 6px 4px;
    border-top: 1px solid var(--border);
  }
  .group {
    margin: 8px 8px 2px;
    color: var(--text-faint);
    font-size: 11px;
  }
  .empty {
    padding: 8px;
    color: var(--text-faint);
    font-size: 12px;
  }
  .spin {
    width: 10px;
    height: 10px;
    border: 2px solid var(--text-faint);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
