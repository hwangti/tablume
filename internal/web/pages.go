package web

import (
	_ "embed"
	"html/template"
	"net/http"
)

//go:embed logo.svg
var logoSVG []byte

func serveLogo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(logoSVG)
}

// Shared look (the app's colours) and the WebAuthn glue.
const pageHead = `<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark light">
<link rel="icon" href="/auth/logo.svg">
<style>
  :root {
    --bg: #1e1f22; --panel: #2b2d30; --input: #1e1f22; --border: #393b40; --hover: #36383d;
    --text: #dfe1e5; --dim: #8c8f96; --accent: #3574f0; --danger: #f75464; --ok: #5fb865;
    --font: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Apple SD Gothic Neo', 'Malgun Gothic', 'Noto Sans KR', sans-serif;
  }
  @media (prefers-color-scheme: light) {
    :root { --bg: #f7f8fa; --panel: #ffffff; --input: #ffffff; --border: #dfe1e5; --hover: #ebecf0;
      --text: #1e1f22; --dim: #6c707e; --danger: #d9343f; --ok: #208a3c; }
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; min-height: 100%; }
  body { background: var(--bg); color: var(--text); font: 14px/1.5 var(--font); display: flex; justify-content: center; padding: 12vh 16px 32px; }
  main { width: 100%; max-width: 420px; }
  main.wide { max-width: 640px; }
  .card { background: var(--panel); border: 1px solid var(--border); border-radius: 10px; padding: 28px; }
  .brand { display: flex; align-items: center; gap: 10px; margin-bottom: 20px; }
  .brand img { width: 36px; height: 36px; }
  .brand h1 { font-size: 20px; font-weight: 600; margin: 0; }
  p { margin: 0 0 14px; }
  .dim { color: var(--dim); font-size: 13px; }
  label { display: block; margin: 0 0 6px; color: var(--dim); font-size: 13px; }
  input { width: 100%; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--input); color: var(--text); font: inherit; outline: none; }
  input:focus { border-color: var(--accent); }
  button { font: inherit; cursor: pointer; border-radius: 6px; padding: 8px 14px; border: 1px solid var(--border); background: var(--panel); color: var(--text); }
  button:hover:not(:disabled) { background: var(--hover); }
  button:disabled { opacity: .5; cursor: default; }
  button.primary { width: 100%; background: var(--accent); border-color: var(--accent); color: #fff; padding: 10px 14px; font-weight: 500; }
  button.primary:hover:not(:disabled) { background: #2f68d8; }
  button.danger { color: var(--danger); }
  button.small { padding: 4px 10px; font-size: 13px; }
  .row { display: flex; gap: 8px; align-items: center; }
  .stack > * + * { margin-top: 14px; }
  .msg { margin-top: 14px; font-size: 13px; }
  .msg:empty { margin: 0; }
  .msg.error { color: var(--danger); }
  .msg.ok { color: var(--ok); }
  a { color: var(--accent); text-decoration: none; }
  a:hover { text-decoration: underline; }
  .sep { border: none; border-top: 1px solid var(--border); margin: 22px 0; }
</style>
<script>
  const dec = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4)), (c) => c.charCodeAt(0)).buffer
  const enc = (b) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')

  async function post(url, body) {
    const r = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body ?? {}) })
    const j = await r.json().catch(() => ({}))
    if (!r.ok) throw new Error(j.error || r.status + ' ' + r.statusText)
    return j
  }

  function why(e) {
    if (e && e.name === 'NotAllowedError') return '취소되었거나 시간이 지났습니다. 다시 시도하세요.'
    if (e && e.name === 'InvalidStateError') return '이 기기(또는 이 패스키 저장소)에는 이미 등록된 패스키가 있습니다.'
    if (e && e.name === 'SecurityError') return '이 주소에서는 패스키를 쓸 수 없습니다 (HTTPS 또는 서버의 TABLUME_URL 설정 확인).'
    return (e && e.message) || String(e)
  }

  async function passkeyLogin() {
    const o = await post('/auth/login/begin')
    const pk = o.publicKey
    pk.challenge = dec(pk.challenge)
    for (const c of pk.allowCredentials || []) c.id = dec(c.id)
    const cred = await navigator.credentials.get({ publicKey: pk })
    const r = cred.response
    await post('/auth/login/finish', {
      id: cred.id, rawId: enc(cred.rawId), type: cred.type,
      response: {
        authenticatorData: enc(r.authenticatorData), clientDataJSON: enc(r.clientDataJSON),
        signature: enc(r.signature), userHandle: r.userHandle ? enc(r.userHandle) : undefined,
      },
      clientExtensionResults: cred.getClientExtensionResults(),
    })
  }

  async function passkeyRegister(token, name) {
    const o = await post('/auth/register/begin', { token, name })
    const pk = o.publicKey
    pk.challenge = dec(pk.challenge)
    pk.user.id = dec(pk.user.id)
    for (const c of pk.excludeCredentials || []) c.id = dec(c.id)
    const cred = await navigator.credentials.create({ publicKey: pk })
    const r = cred.response
    await post('/auth/register/finish', {
      id: cred.id, rawId: enc(cred.rawId), type: cred.type,
      response: {
        attestationObject: enc(r.attestationObject), clientDataJSON: enc(r.clientDataJSON),
        transports: r.getTransports ? r.getTransports() : [],
      },
      clientExtensionResults: cred.getClientExtensionResults(),
    })
  }

  // A name for the new passkey, from the browser it was made in.
  function deviceName() {
    const ua = navigator.userAgent
    const os = /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) ? 'iPad' : /Android/.test(ua) ? 'Android'
      : /Mac OS X/.test(ua) ? (navigator.maxTouchPoints > 1 ? 'iPad' : 'Mac') : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : '기기'
    const br = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : ''
    return br ? os + ' · ' + br : os
  }

  function say(el, text, kind) {
    el.textContent = text
    el.className = 'msg' + (kind ? ' ' + kind : '')
  }
</script>
`

var loginTmpl = template.Must(template.New("login").Parse(pageHead + `<title>Tablume 로그인</title>
</head>
<body>
<main>
  <div class="card">
    <div class="brand"><img src="/auth/logo.svg" alt=""><h1>Tablume</h1></div>
    {{if and .Token (ne .Token "-")}}
      <div class="stack">
        <p>이 기기에 패스키를 등록하면 Face ID · Touch ID · Windows Hello(PIN)로 로그인합니다.</p>
        <div>
          <label for="name">패스키 이름</label>
          <input id="name" maxlength="60" autocomplete="off">
        </div>
        <button id="go" class="primary">이 기기에 패스키 등록</button>
      </div>
      <div id="msg" class="msg"></div>
      {{if .HasPasskeys}}<hr class="sep"><p class="dim">이미 쓸 수 있는 패스키가 있으면 <a href="/login">로그인</a>하세요.</p>{{end}}
      <script>
        const tok = {{.Token}}
        const name = document.getElementById('name')
        name.value = deviceName()
        const go = document.getElementById('go')
        const msg = document.getElementById('msg')
        async function run() {
          go.disabled = true
          say(msg, '')
          try {
            await passkeyRegister(tok, name.value)
            say(msg, '등록했습니다. 여는 중…', 'ok')
            location.replace('/')
          } catch (e) {
            say(msg, why(e), 'error')
            go.disabled = false
          }
        }
        go.onclick = run
        name.onkeydown = (e) => e.key === 'Enter' && run()
      </script>
    {{else if .HasPasskeys}}
      {{if eq .Token "-"}}<p class="msg error" style="margin-top:0">등록 링크가 만료되었거나 이미 사용되었습니다.</p>{{end}}
      <p class="dim">등록한 패스키로 로그인합니다. 다른 기기의 패스키는 휴대폰으로 QR 코드를 찍어 쓸 수 있습니다.</p>
      <button id="go" class="primary" autofocus>패스키로 로그인</button>
      <div id="msg" class="msg"></div>
      <script>
        const go = document.getElementById('go')
        const msg = document.getElementById('msg')
        go.onclick = async () => {
          go.disabled = true
          say(msg, '')
          try {
            await passkeyLogin()
            location.replace('/')
          } catch (e) {
            say(msg, why(e), 'error')
            go.disabled = false
          }
        }
      </script>
    {{else}}
      {{if eq .Token "-"}}<p class="msg error" style="margin-top:0">등록 링크가 만료되었거나 올바르지 않습니다.</p>{{end}}
      <p>아직 등록된 패스키가 없습니다.</p>
      <p class="dim">서버를 시작할 때 로그에 나온 등록 주소(<code>/login?token=…</code>)를 열어 첫 패스키를 등록하세요.</p>
    {{end}}
  </div>
</main>
</body>
</html>
`))

var passkeysTmpl = template.Must(template.New("passkeys").Parse(pageHead + `<title>패스키 관리 — Tablume</title>
<style>
  .list { margin: 0 0 6px; padding: 0; list-style: none; }
  .list li { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid var(--border); }
  .list li:first-child { border-top: 1px solid var(--border); }
  .list .info { flex: 1; min-width: 0; }
  .list .name { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .badge { font-size: 11px; color: var(--accent); border: 1px solid var(--accent); border-radius: 4px; padding: 0 5px; margin-left: 6px; font-weight: 400; }
  .link { display: none; margin-top: 12px; }
  .link input { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
  h2 { font-size: 15px; font-weight: 600; margin: 0 0 10px; }
  .top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 18px; }
  .top .brand { margin: 0; }
</style>
</head>
<body>
<main class="wide">
  <div class="card">
    <div class="top">
      <div class="brand"><img src="/auth/logo.svg" alt=""><h1>패스키 관리</h1></div>
      <a href="/">Tablume로 돌아가기</a>
    </div>
    <ul id="list" class="list"></ul>
    <div id="msg" class="msg"></div>

    <hr class="sep">
    <h2>이 기기에 추가</h2>
    <p class="dim">지금 쓰는 브라우저나 기기에 패스키를 하나 더 만듭니다.</p>
    <div class="row">
      <input id="name" maxlength="60" autocomplete="off" style="flex:1">
      <button id="add">패스키 추가</button>
    </div>

    <hr class="sep">
    <h2>다른 기기 등록</h2>
    <p class="dim">등록한 패스키를 쓸 수 없는 기기(예: iCloud 키체인이 없는 Windows PC)에서 열 일회용 링크를 만듭니다. {{/* 15분 */}}15분 동안 한 번만 쓸 수 있습니다.</p>
    <button id="invite">등록 링크 만들기</button>
    <div id="link" class="link">
      <div class="row"><input id="url" readonly style="flex:1"><button id="copy">복사</button></div>
      <p id="exp" class="dim" style="margin:6px 0 0"></p>
    </div>

    <hr class="sep">
    <button id="logout" class="danger">이 기기에서 로그아웃</button>
  </div>
</main>
<script>
  const msg = document.getElementById('msg')
  const list = document.getElementById('list')
  const fmt = (s) => new Date(s).toLocaleString()

  async function load() {
    const r = await fetch('/auth/passkeys')
    if (r.status === 401) return location.replace('/login')
    const keys = await r.json()
    list.replaceChildren()
    for (const k of keys) {
      const li = document.createElement('li')
      const info = document.createElement('div')
      info.className = 'info'
      const n = document.createElement('div')
      n.className = 'name'
      n.textContent = k.name
      if (k.current) {
        const b = document.createElement('span')
        b.className = 'badge'
        b.textContent = '이 브라우저'
        n.append(b)
      }
      const d = document.createElement('div')
      d.className = 'dim'
      d.textContent = '등록 ' + fmt(k.created) + ' · 마지막 사용 ' + fmt(k.lastUsed)
      info.append(n, d)
      const ren = document.createElement('button')
      ren.className = 'small'
      ren.textContent = '이름 바꾸기'
      ren.onclick = async () => {
        const v = prompt('패스키 이름', k.name)
        if (!v || v === k.name) return
        try { await post('/auth/passkeys/rename', { id: k.id, name: v }); load() } catch (e) { say(msg, why(e), 'error') }
      }
      const del = document.createElement('button')
      del.className = 'small danger'
      del.textContent = '삭제'
      del.disabled = keys.length <= 1
      del.title = keys.length <= 1 ? '마지막 패스키는 지울 수 없습니다' : ''
      del.onclick = async () => {
        if (!confirm('"' + k.name + '" 패스키를 지울까요? 이 패스키로 로그인한 기기는 로그아웃됩니다.' + (k.current ? ' (이 브라우저 포함)' : ''))) return
        try { await post('/auth/passkeys/delete', { id: k.id }); k.current ? location.replace('/login') : load() } catch (e) { say(msg, why(e), 'error') }
      }
      li.append(info, ren, del)
      list.append(li)
    }
  }

  const name = document.getElementById('name')
  name.value = deviceName()
  document.getElementById('add').onclick = async (ev) => {
    ev.target.disabled = true
    say(msg, '')
    try {
      await passkeyRegister('', name.value)
      say(msg, '패스키를 추가했습니다.', 'ok')
      load()
    } catch (e) {
      say(msg, why(e), 'error')
    }
    ev.target.disabled = false
  }

  document.getElementById('invite').onclick = async () => {
    try {
      const r = await post('/auth/invite')
      document.getElementById('url').value = r.url
      document.getElementById('exp').textContent = new Date(r.expires).toLocaleTimeString() + '까지 쓸 수 있습니다.'
      document.getElementById('link').style.display = 'block'
      document.getElementById('url').select()
    } catch (e) {
      say(msg, why(e), 'error')
    }
  }
  document.getElementById('copy').onclick = async (ev) => {
    const u = document.getElementById('url')
    try { await navigator.clipboard.writeText(u.value) } catch { u.select(); document.execCommand('copy') }
    ev.target.textContent = '복사됨'
    setTimeout(() => (ev.target.textContent = '복사'), 1500)
  }
  document.getElementById('logout').onclick = async () => {
    await post('/auth/logout').catch(() => {})
    location.replace('/login')
  }
  load().catch((e) => say(msg, why(e), 'error'))
</script>
</body>
</html>
`))
