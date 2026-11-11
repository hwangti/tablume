<script lang="ts">
  import { t } from '../lib/i18n.svelte'
  import { isWeb } from '../lib/platform'
  import { pickServerFile } from '../lib/webfiles.svelte'
  import { api, errText } from '../lib/api'
  import { app, loadConnections, disconnect, connect, toast, confirmBox, closeGone } from '../lib/state.svelte'
  import { DRIVERS, type ConnConfig, type Driver } from '../lib/types'
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

  // Databases in this build, plus the connection's own when editing one
  // whose support was left out (so its settings still show).
  const drivers = DRIVERS.filter((d) => app.drivers.includes(d.id) || d.id === start.driver)

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
    c.driver === 'oracle' ? (c.oracleSid ? 'SID' : t('서비스 이름')) : c.driver === 'sqlite' ? t('파일') : t('데이터베이스'),
  )

  let paramsHint = $derived(
    {
      mysql: t('예: tls=true 또는 tls=skip-verify, tls-ca=/경로/ca.pem'),
      mariadb: t('예: tls=true 또는 tls=skip-verify, tls-ca=/경로/ca.pem'),
      postgres: t('예: {x}', { x: 'sslmode=require' }),
      mssql: t('예: {x}', { x: 'encrypt=true&TrustServerCertificate=false' }),
      oracle: t('예: {x}', { x: 'SSL=true&SSL VERIFY=false' }),
      sqlite: t('예: {x}', { x: '_pragma=foreign_keys(1)' }),
    }[c.driver],
  )

  async function pickKey() {
    const p = isWeb ? await pickServerFile(t('SSH 개인 키 파일'), c.sshKeyFile) : await api.PickKeyFile()
    if (p) c.sshKeyFile = p
  }

  async function pickFile() {
    const p = isWeb ? await pickServerFile(t('SQLite 데이터베이스 파일'), c.database, true) : await api.PickSQLiteFile()
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
      let text = `${t('연결 성공')} — ${v.version}`
      if (c.ssh && v.sshHostKey) {
        if (!c.sshHostKey) c.sshHostKey = v.sshHostKey // pinned when saved
        text += `\n${t('SSH 호스트 키')}: ${v.sshHostKey}`
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
      if (wasConnected && (await disconnect(saved.id))) await connect(saved.id)
      if (isNew) toast(t("'{name}' 저장됨", { name: saved.name }))
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
    if (!(await confirmBox(t("'{name}' 연결을 삭제할까요?", { name: c.name })))) return
    await api.DeleteConnection(c.id)
    closeGone(app.tabs.filter((t) => t.connId === c.id))
    await loadConnections()
    app.editing = null
  }

</script>

<Modal title={isNew ? t('새 연결') : t('연결 편집')} onclose={close} width={560} dismissible={false}>
  <div class="drivers">
    {#each drivers as d (d.id)}
      <button class="drv" class:on={c.driver === d.id} onclick={() => setDriver(d.id)}>{d.label}</button>
    {/each}
  </div>

  <div class="form">
    <label for="f-name">{t('이름')}</label>
    <div class="row">
      <input id="f-name" bind:value={c.name} placeholder={t('예: 운영 DB')} />
      <div class="colors">
        {#each colors as col (col)}
          <button
            class="color"
            class:on={c.color === col}
            style:background={col || 'transparent'}
            onclick={() => (c.color = col)}
            title={col ? '' : t('색 없음')}
            aria-label={t('색상')}
          ></button>
        {/each}
      </div>
    </div>

    <span></span>
    <label class="check" title={t('SELECT 등 조회만 실행합니다. 데이터 편집, 구조 변경, 테이블·스키마 만들기·삭제를 막고, MySQL·MariaDB·PostgreSQL·SQLite는 DB 세션도 읽기 전용으로 엽니다.')}>
      <input type="checkbox" bind:checked={c.readOnly} />
      {t('읽기 전용')} <span class="dim">{t('(조회만 실행)')}</span>
    </label>

    {#if c.driver === 'sqlite'}
      <label for="f-file">{t('파일')}</label>
      <div class="row">
        <input id="f-file" bind:value={c.database} placeholder={isWeb ? t('파일 폴더 안의 경로 (예: app.db)') : '/path/to/database.db'} />
        <button class="btn" onclick={pickFile}>{t('찾기…')}</button>
      </div>
    {:else}
      <label for="f-host">{t('호스트')}</label>
      <div class="row">
        <input id="f-host" bind:value={c.host} placeholder={c.driver === 'mssql' ? t('localhost 또는 host\\instance') : 'localhost'} />
        <span class="dim">{t('포트')}</span>
        <input class="port" type="number" bind:value={c.port} />
      </div>

      <label for="f-user">{t('사용자')}</label>
      <input id="f-user" bind:value={c.user} autocomplete="off" />

      <label for="f-pw">{t('비밀번호')}</label>
      <div class="row">
        <input
          id="f-pw"
          type="password"
          bind:value={c.password}
          placeholder={!isNew && start.savePassword ? t('(저장됨 — 바꿀 때만 입력)') : ''}
          autocomplete="new-password"
        />
        <label class="check"><input type="checkbox" bind:checked={c.savePassword} /> {t('저장')}</label>
      </div>

      <label for="f-db">{dbLabel}</label>
      <div class="row">
        <input
          id="f-db"
          bind:value={c.database}
          placeholder={c.driver === 'oracle' ? 'ORCL, FREEPDB1, XEPDB1 …' : t('(선택)')}
        />
        {#if c.driver === 'oracle'}
          <label class="check"><input type="checkbox" bind:checked={c.oracleSid} /> {t('SID 사용')}</label>
        {/if}
      </div>
    {/if}

    {#if c.driver !== 'sqlite'}
      <span></span>
      <label class="check ssh-toggle"><input type="checkbox" bind:checked={c.ssh} /> {t('SSH 터널 사용')}</label>
      {#if c.ssh}
        <label for="f-sshhost">{t('SSH 호스트')}</label>
        <div class="row">
          <input id="f-sshhost" bind:value={c.sshHost} placeholder="bastion.example.com" />
          <span class="dim">{t('포트')}</span>
          <input class="port" type="number" bind:value={c.sshPort} />
        </div>
        <label for="f-sshuser">{t('SSH 사용자')}</label>
        <input id="f-sshuser" bind:value={c.sshUser} autocomplete="off" />
        <span class="lbl">{t('인증')}</span>
        <div class="row">
          <div class="seg" role="group">
            <button type="button" class:on={c.sshAuth !== 'key'} onclick={() => (c.sshAuth = 'password')}>{t('비밀번호')}</button>
            <button type="button" class:on={c.sshAuth === 'key'} onclick={() => (c.sshAuth = 'key')}>{t('키 파일')}</button>
          </div>
        </div>
        {#if c.sshAuth === 'key'}
          <label for="f-sshkey">{t('키 파일')}</label>
          <div class="row">
            <input id="f-sshkey" bind:value={c.sshKeyFile} placeholder={isWeb ? t('파일 폴더 안의 키 파일') : t('(비우면 ~/.ssh/id_ed25519, id_ecdsa, id_rsa)')} />
            <button class="btn" onclick={pickKey}>{t('찾기…')}</button>
          </div>
        {/if}
        <label for="f-sshpw">{c.sshAuth === 'key' ? t('키 암호') : t('SSH 비밀번호')}</label>
        <input
          id="f-sshpw"
          type="password"
          bind:value={c.sshPassword}
          placeholder={sshSaved ? t('(저장됨 — 바꿀 때만 입력)') : c.sshAuth === 'key' ? t('(암호가 없으면 비워 두세요)') : ''}
          autocomplete="new-password"
        />
        {#if c.sshHostKey}
          <span class="lbl">{t('호스트 키')}</span>
          <div class="row">
            <span class="mono hostkey" title={t('처음 연결할 때 저장된 SSH 서버 키입니다. 다른 키를 가진 서버에는 연결하지 않습니다.')}>{c.sshHostKey}</span>
            <button class="btn mini" onclick={() => (c.sshHostKey = '')}>{t('초기화')}</button>
          </div>
        {/if}
        <span></span>
        <span class="hint">{t('위의 호스트·포트는 SSH 서버에서 본 DB 주소입니다 (예: localhost:3306).')}</span>
      {/if}
    {/if}

    {#if showAdvanced}
      <label for="f-params">{t('추가 옵션')}</label>
      <input id="f-params" class="mono" bind:value={c.params} placeholder={paramsHint} />
    {:else}
      <span></span>
      <button class="link" onclick={() => (showAdvanced = true)}>{t('추가 옵션…')}</button>
    {/if}
  </div>

  {#if result}
    <div class="result" class:ok={result.ok}>{result.text}</div>
  {/if}

  {#snippet footer()}
    {#if !isNew}
      <button class="btn danger" onclick={remove}>{t('삭제')}</button>
    {/if}
    <button class="btn" onclick={test} disabled={testing}>{testing ? t('테스트 중…') : t('연결 테스트')}</button>
    <span style="flex:1"></span>
    <button class="btn" onclick={close}>{t('취소')}</button>
    <button class="btn primary" onclick={save} disabled={saving}>{t('저장')}</button>
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
    flex-wrap: wrap; /* a phone: what doesn't fit goes under the field */
    gap: 8px;
    align-items: center;
  }
  .row > input:first-child {
    flex: 1 1 140px;
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
  /* a phone: labels over their fields, which get the whole width */
  @media (max-width: 480px) {
    .form {
      grid-template-columns: minmax(0, 1fr);
      gap: 4px;
    }
    .form > label,
    .form > .lbl {
      text-align: left;
      margin-top: 6px;
    }    /* (the label column's spacers) */
    .form > span:empty {
      display: none;
    }
  }
</style>
