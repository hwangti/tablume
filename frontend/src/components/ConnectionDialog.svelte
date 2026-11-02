<script lang="ts">
  import { api, errText } from '../lib/api'
  import { app, loadConnections, disconnect, connect, toast, confirmBox } from '../lib/state.svelte'
  import { DRIVERS as ALL_DRIVERS, type ConnConfig, type Driver } from '../lib/types'
  // Tauri experiment: MariaDB/MySQL only.
  const DRIVERS = ALL_DRIVERS.filter((d) => d.id === 'mariadb' || d.id === 'mysql')
  import Modal from './Modal.svelte'
  import { untrack } from 'svelte'

  let { initial }: { initial: ConnConfig } = $props()

  // The dialog edits a copy of the connection it was opened with.
  const start = untrack(() => $state.snapshot(initial))
  let c = $state<ConnConfig>({
    ...start,
    password: '',
    sshPassword: '',
    sshPort: start.sshPort || 22,
    sshAuth: start.sshAuth || 'password',
  })
  // A saved SSH secret exists when the connection already used SSH.
  const sshSaved = !!start.ssh && start.id !== ''
  let isNew = start.id === ''
  let testing = $state(false)
  let saving = $state(false)
  let result = $state<{ ok: boolean; text: string } | null>(null)
  let showAdvanced = $state(!!start.params)

  const colors = ['', '#e5534b', '#e0a84b', '#57ab5a', '#3574f0', '#986ee2', '#6c707e']

  function setDriver(d: Driver) {
    const prev = DRIVERS.find((x) => x.id === c.driver)
    const next = DRIVERS.find((x) => x.id === d)!
    if (!c.port || c.port === prev?.port) c.port = next.port
    c.driver = d
    if (d === 'oracle' && !c.database) c.database = 'ORCL'
    result = null
  }

  let dbLabel = $derived(
    c.driver === 'oracle' ? (c.oracleSid ? 'SID' : '서비스 이름') : c.driver === 'sqlite' ? '파일' : '데이터베이스',
  )

  let paramsHint = $derived(
    {
      mysql: '예: tls=true&loc=Local',
      mariadb: '예: tls=true&loc=Local',
      postgres: '예: sslmode=require',
      mssql: '예: encrypt=true&TrustServerCertificate=false',
      oracle: '예: SSL=true&SSL VERIFY=false',
      sqlite: '예: _pragma=foreign_keys(1)',
    }[c.driver],
  )

  async function pickKey() {
    const p = await api.PickKeyFile()
    if (p) c.sshKeyFile = p
  }

  async function pickFile() {
    const p = await api.PickSQLiteFile()
    if (p) {
      c.database = p
      if (!c.name) c.name = p.split(/[\\/]/).pop() ?? ''
    }
  }

  async function test() {
    testing = true
    result = null
    try {
      const v = await api.TestConnection($state.snapshot(c))
      let text = `연결 성공 — ${v.version}`
      if (c.ssh && v.sshHostKey) {
        if (!c.sshHostKey) c.sshHostKey = v.sshHostKey // pinned when saved
        text += `\nSSH 호스트 키: ${v.sshHostKey}`
      }
      result = { ok: true, text }
    } catch (e) {
      result = { ok: false, text: errText(e) }
    } finally {
      testing = false
    }
  }

  async function save() {
    if (!c.name.trim()) c.name = c.driver === 'sqlite' ? c.database.split(/[\\/]/).pop() || 'SQLite' : `${c.host || 'localhost'}`
    saving = true
    try {
      const wasConnected = app.conn[c.id]?.status === 'connected'
      const saved = await api.SaveConnection($state.snapshot(c))
      await loadConnections()
      app.editing = null
      if (wasConnected) {
        await disconnect(saved.id)
        await connect(saved.id)
      }
      if (isNew) toast(`'${saved.name}' 저장됨`)
    } catch (e) {
      result = { ok: false, text: errText(e) }
    } finally {
      saving = false
    }
  }

  function close() {
    app.editing = null
  }

  async function remove() {
    if (!(await confirmBox(`'${c.name}' 연결을 삭제할까요?`))) return
    await api.DeleteConnection(c.id)
    app.tabs = app.tabs.filter((t) => t.connId !== c.id)
    await loadConnections()
    app.editing = null
  }

</script>

<Modal title={isNew ? '새 연결' : '연결 편집'} onclose={close} width={560} dismissible={false}>
  <div class="drivers">
    {#each DRIVERS as d (d.id)}
      <button class="drv" class:on={c.driver === d.id} onclick={() => setDriver(d.id)}>{d.label}</button>
    {/each}
  </div>

  <div class="form">
    <label for="f-name">이름</label>
    <div class="row">
      <input id="f-name" bind:value={c.name} placeholder="예: 운영 DB" />
      <div class="colors">
        {#each colors as col (col)}
          <button
            class="color"
            class:on={c.color === col}
            style:background={col || 'transparent'}
            onclick={() => (c.color = col)}
            title={col ? '' : '색 없음'}
            aria-label="색상"
          ></button>
        {/each}
      </div>
    </div>

    {#if c.driver === 'sqlite'}
      <label for="f-file">파일</label>
      <div class="row">
        <input id="f-file" bind:value={c.database} placeholder="/path/to/database.db" />
        <button class="btn" onclick={pickFile}>찾기…</button>
      </div>
    {:else}
      <label for="f-host">호스트</label>
      <div class="row">
        <input id="f-host" bind:value={c.host} placeholder={c.driver === 'mssql' ? 'localhost 또는 host\\instance' : 'localhost'} />
        <span class="dim">포트</span>
        <input class="port" type="number" bind:value={c.port} />
      </div>

      <label for="f-user">사용자</label>
      <input id="f-user" bind:value={c.user} autocomplete="off" />

      <label for="f-pw">비밀번호</label>
      <div class="row">
        <input
          id="f-pw"
          type="password"
          bind:value={c.password}
          placeholder={!isNew && start.savePassword ? '(저장됨 — 바꿀 때만 입력)' : ''}
          autocomplete="new-password"
        />
        <label class="check"><input type="checkbox" bind:checked={c.savePassword} /> 저장</label>
      </div>

      <label for="f-db">{dbLabel}</label>
      <div class="row">
        <input
          id="f-db"
          bind:value={c.database}
          placeholder={c.driver === 'oracle' ? 'ORCL, FREEPDB1, XEPDB1 …' : '(선택)'}
        />
        {#if c.driver === 'oracle'}
          <label class="check"><input type="checkbox" bind:checked={c.oracleSid} /> SID 사용</label>
        {/if}
      </div>
    {/if}

    {#if c.driver !== 'sqlite'}
      <span></span>
      <label class="check ssh-toggle"><input type="checkbox" bind:checked={c.ssh} /> SSH 터널 사용</label>
      {#if c.ssh}
        <label for="f-sshhost">SSH 호스트</label>
        <div class="row">
          <input id="f-sshhost" bind:value={c.sshHost} placeholder="bastion.example.com" />
          <span class="dim">포트</span>
          <input class="port" type="number" bind:value={c.sshPort} />
        </div>
        <label for="f-sshuser">SSH 사용자</label>
        <input id="f-sshuser" bind:value={c.sshUser} autocomplete="off" />
        <span class="lbl">인증</span>
        <div class="row">
          <label class="check"><input type="radio" bind:group={c.sshAuth} value="password" /> 비밀번호</label>
          <label class="check"><input type="radio" bind:group={c.sshAuth} value="key" /> 키 파일</label>
        </div>
        {#if c.sshAuth === 'key'}
          <label for="f-sshkey">키 파일</label>
          <div class="row">
            <input id="f-sshkey" bind:value={c.sshKeyFile} placeholder="(비우면 ~/.ssh/id_ed25519, id_ecdsa, id_rsa)" />
            <button class="btn" onclick={pickKey}>찾기…</button>
          </div>
        {/if}
        <label for="f-sshpw">{c.sshAuth === 'key' ? '키 암호' : 'SSH 비밀번호'}</label>
        <input
          id="f-sshpw"
          type="password"
          bind:value={c.sshPassword}
          placeholder={sshSaved ? '(저장됨 — 바꿀 때만 입력)' : c.sshAuth === 'key' ? '(암호가 없으면 비워 두세요)' : ''}
          autocomplete="new-password"
        />
        {#if c.sshHostKey}
          <span class="lbl">호스트 키</span>
          <div class="row">
            <span class="mono hostkey" title="처음 연결할 때 저장된 SSH 서버 키입니다. 다른 키를 가진 서버에는 연결하지 않습니다.">{c.sshHostKey}</span>
            <button class="btn mini" onclick={() => (c.sshHostKey = '')}>초기화</button>
          </div>
        {/if}
        <span></span>
        <span class="hint">위의 호스트·포트는 SSH 서버에서 본 DB 주소입니다 (예: localhost:3306).</span>
      {/if}
    {/if}

    {#if showAdvanced}
      <label for="f-params">추가 옵션</label>
      <input id="f-params" class="mono" bind:value={c.params} placeholder={paramsHint} />
    {:else}
      <span></span>
      <button class="link" onclick={() => (showAdvanced = true)}>추가 옵션…</button>
    {/if}
  </div>

  {#if result}
    <div class="result" class:ok={result.ok}>{result.text}</div>
  {/if}

  {#snippet footer()}
    {#if !isNew}
      <button class="btn danger" onclick={remove}>삭제</button>
    {/if}
    <button class="btn" onclick={test} disabled={testing}>{testing ? '테스트 중…' : '연결 테스트'}</button>
    <span style="flex:1"></span>
    <button class="btn" onclick={close}>취소</button>
    <button class="btn primary" onclick={save} disabled={saving}>저장</button>
  {/snippet}
</Modal>

<style>
  .drivers {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 16px;
  }
  .drv {
    padding: 6px 12px;
    border: 1px solid var(--border);
    border-radius: 16px;
    background: transparent;
    cursor: pointer;
  }
  .drv.on {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-text);
  }
  .form {
    display: grid;
    grid-template-columns: 90px 1fr;
    gap: 10px 12px;
    align-items: center;
  }
  .form > label,
  .form > .lbl {
    color: var(--text-dim);
    text-align: right;
  }
  .form > label.ssh-toggle {
    color: var(--text);
    text-align: left;
  }
  .hostkey {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11.5px;
    color: var(--text-dim);
  }
  .hint {
    font-size: 11.5px;
    color: var(--text-faint);
    margin-top: -4px;
  }
  .row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .row > input:first-child {
    flex: 1;
    min-width: 0;
  }
  .port {
    width: 80px;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 4px;
    white-space: nowrap;
  }
  .colors {
    display: flex;
    gap: 4px;
  }
  .color {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 1px solid var(--border);
    cursor: pointer;
    padding: 0;
  }
  .color.on {
    outline: 2px solid var(--text);
    outline-offset: 1px;
  }
  .link {
    justify-self: start;
    border: none;
    background: none;
    color: var(--accent);
    cursor: pointer;
    padding: 0;
  }
  .result {
    margin-top: 14px;
    padding: 8px 10px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--danger) 14%, transparent);
    color: var(--danger);
    white-space: pre-wrap;
    word-break: break-word;
    user-select: text;
    -webkit-user-select: text;
  }
  .result.ok {
    background: color-mix(in srgb, var(--ok) 14%, transparent);
    color: var(--ok);
  }
</style>
