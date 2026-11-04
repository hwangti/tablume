<script lang="ts">
  // Browser build: picks a file from the server's files folder (the only
  // folder the server reads SQLite databases and SSH keys from).
  import { esc, locale, t } from '../lib/i18n.svelte'
  import { onMount } from 'svelte'
  import Modal from './Modal.svelte'
  import Icon from './Icon.svelte'
  import { api, errText, type FileList } from '../lib/api'
  import { formatBytes } from '../lib/format'
  import type { ServerPick } from '../lib/webfiles.svelte'

  let { req }: { req: ServerPick } = $props()

  let list = $state<FileList | null>(null)
  let error = $state('')
  let name = $state('') // the file picked (or typed) in the listed folder
  let listEl = $state<HTMLDivElement>()

  const sep = $derived(list?.root.includes('\\') && !list.root.includes('/') ? '\\' : '/')
  const join = (dir: string, n: string) => (dir.endsWith(sep) ? dir + n : dir + sep + n)

  // folder path below the root, as clickable parts
  const crumbs = $derived.by(() => {
    if (!list) return []
    const rest = list.dir.slice(list.root.length).split(/[\\/]/).filter(Boolean)
    const out = [{ label: t('파일 폴더'), path: list.root }]
    let p = list.root
    for (const part of rest) {
      p = join(p, part)
      out.push({ label: part, path: p })
    }
    return out
  })
  const atRoot = $derived(!list || list.dir === list.root)
  const parent = $derived(crumbs.length > 1 ? crumbs[crumbs.length - 2].path : '')

  async function load(dir: string, select = '') {
    error = ''
    try {
      list = await api.BrowseFiles(dir)
      // a file path starts in its folder with the file picked
      const base = select || (dir && !list.entries.some((e) => e.dir && dir.endsWith(e.name)) ? dir.split(/[\\/]/).pop() ?? '' : '')
      name = list.entries.some((e) => !e.dir && e.name === base) ? base : req.allowNew ? name : ''
      listEl?.scrollTo(0, 0)
    } catch (e) {
      error = errText(e)
    }
  }
  onMount(() => load(req.start))

  function pick() {
    if (!list || !name.trim()) return
    if (!req.allowNew && !list.entries.some((e) => !e.dir && e.name === name)) return
    req.resolve(join(list.dir, name.trim()))
  }

  const fmtDate = (s: string) => {
    const d = new Date(s)
    return isNaN(+d) ? '' : d.toLocaleString(locale(), { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  }
</script>

<Modal title={req.title} onclose={() => req.resolve(null)} width={620} onsubmit={pick}>
  <div class="crumbs">
    <Icon name="folder" size={14} />
    {#each crumbs as c, i (c.path)}
      {#if i}<span class="slash">/</span>{/if}
      <button class="crumb" disabled={i === crumbs.length - 1} onclick={() => load(c.path)}>{c.label}</button>
    {/each}
  </div>
  <div class="list" bind:this={listEl}>
    {#if !atRoot}
      <button class="row" ondblclick={() => load(parent)} onclick={() => load(parent)}>
        <Icon name="folder" size={14} /><span class="n">..</span><span></span><span></span>
      </button>
    {/if}
    {#each list?.entries ?? [] as e (e.name)}
      <button
        class="row"
        class:on={!e.dir && e.name === name}
        onclick={() => (e.dir ? load(join(list!.dir, e.name)) : (name = e.name))}
        ondblclick={() => !e.dir && ((name = e.name), pick())}
      >
        <Icon name={e.dir ? 'folder' : 'file'} size={14} />
        <span class="n">{e.name}</span>
        <span class="sz">{e.dir ? '' : formatBytes(e.size)}</span>
        <span class="dt">{fmtDate(e.modified)}</span>
      </button>
    {:else}
      {#if list}<div class="empty">{t('빈 폴더입니다')}{#if atRoot} — {@html t('서버의 <code>{root}</code>에 파일을 넣으세요', { root: esc(list.root) })}{/if}</div>{/if}
    {/each}
  </div>
  {#if error}<div class="err">{error}</div>{/if}
  {#if req.allowNew}
    <label class="new">
      <span>{t('파일 이름')}</span>
      <input class="mono" bind:value={name} placeholder={t('없는 이름이면 새 데이터베이스를 만듭니다')} spellcheck="false" />
    </label>
  {/if}
  <p class="hint">{t('웹 버전은 서버의 파일 폴더 안에 있는 파일만 쓸 수 있습니다.')}</p>
  {#snippet footer()}
    <span style="flex:1"></span>
    <button class="btn" onclick={() => req.resolve(null)}>{t('취소')}</button>
    <button class="btn primary" onclick={pick} disabled={!name.trim() || (!req.allowNew && !list?.entries.some((e) => !e.dir && e.name === name))}>{t('선택')}</button>
  {/snippet}
</Modal>

<style>
  .crumbs {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 2px;
    margin-bottom: 8px;
    color: var(--text-dim);
  }
  .crumbs :global(svg) {
    margin-right: 4px;
  }
  .crumb {
    padding: 1px 4px;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--accent);
    cursor: pointer;
  }
  .crumb:disabled {
    color: var(--text);
    cursor: default;
  }
  .crumb:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .slash {
    color: var(--text-faint);
  }
  .list {
    height: 300px;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-input);
  }
  .row {
    display: grid;
    grid-template-columns: 16px minmax(0, 1fr) 80px 140px;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 4px 8px;
    border: none;
    background: none;
    color: var(--text);
    text-align: left;
    cursor: default;
  }
  .row:hover {
    background: var(--bg-hover);
  }
  .row.on {
    background: var(--bg-select);
  }
  .row :global(svg) {
    color: var(--text-dim);
  }
  .n {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sz,
  .dt {
    color: var(--text-dim);
    font-size: 12px;
    text-align: right;
    white-space: nowrap;
  }
  .empty {
    padding: 16px;
    color: var(--text-dim);
    text-align: center;
  }
  .err {
    margin-top: 8px;
    color: var(--danger);
  }
  .new {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 10px;
  }
  .new span {
    flex-shrink: 0;
    color: var(--text-dim);
  }
  .new input {
    flex: 1;
  }
  .hint {
    margin: 8px 0 0;
    color: var(--text-faint);
    font-size: 12px;
  }
</style>
