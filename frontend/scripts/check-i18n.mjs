// Lists Korean UI text with no English entry in src/lib/i18n-en.ts, and
// English entries no longer used. Every quoted string with Hangul outside
// comments counts as UI text (the Korean text is the key of t()).
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const src = new URL('../src/', import.meta.url).pathname
const skip = new Set(['lib/i18n-en.ts', 'lib/i18n.svelte.ts'])
const hangul = /[가-힣]/

function files(dir, out = []) {
  for (const n of readdirSync(dir)) {
    const p = join(dir, n)
    if (statSync(p).isDirectory()) files(p, out)
    else if (/\.(svelte|ts)$/.test(n) && !skip.has(p.slice(src.length))) out.push(p)
  }
  return out
}

// Strings in code: '…', "…" and `…` (without ${}), comments left out.
function strings(text) {
  const out = []
  const re = /\/\/[^\n]*|\/\*[\s\S]*?\*\/|<!--[\s\S]*?-->|'((?:\\.|[^'\\\n])*)'|"((?:\\.|[^"\\\n])*)"|`((?:\\.|[^`\\$\n])*)`/g
  for (const m of text.matchAll(re)) {
    const s = m[1] ?? m[2] ?? m[3]
    // a markup attribute with {…} in it: the strings inside
    if (m[2] !== undefined && s.includes('{')) {
      out.push(...strings(s.replace(/^[^{]*\{|\}[^{}]*$/g, ' ').replace(/\}[^{}]*\{/g, ' ')))
      continue
    }
    if (s !== undefined && hangul.test(s)) out.push(s.replace(/\\n/g, '\n').replace(/\\(.)/g, '$1'))
  }
  return out
}

const en = readFileSync(join(src, 'lib/i18n-en.ts'), 'utf8')
const keys = new Set(strings(en.replace(/^[\s\S]*?\{/, '')).filter((_, i, a) => true))
// (the table's own lines are "'key': 'English'", keys are the Hangul ones)

const used = new Set()
const missing = []
for (const f of files(src)) {
  const text = readFileSync(f, 'utf8')
  if (f.endsWith('.svelte')) {
    // markup text outside {…}: Korean there isn't translated
    let m = text.replace(/<(script|style)[\s\S]*?<\/\1>|<!--[\s\S]*?-->/g, '')
    for (let prev = ''; prev !== m; ) (prev = m), (m = m.replace(/\{[^{}]*\}/g, ''))
    for (const line of m.split('\n')) if (hangul.test(line)) missing.push(`${f.slice(src.length)}: (markup) ${line.trim()}`)
  }
  for (const s of strings(text)) {
    if (s === '한국어') continue // the language's own name
    used.add(s)
    if (!keys.has(s)) missing.push(`${f.slice(src.length)}: ${s}`)
  }
}
const unused = [...keys].filter((k) => !used.has(k))
for (const m of missing) console.log('missing  ', m)
for (const u of unused) console.log('unused   ', u)
if (missing.length) process.exit(1)
