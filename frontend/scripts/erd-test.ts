// ERD layout checks on generated schemas: a hub (one table most others
// reference) and clusters linked in a chain. The hub sits in the middle,
// linked tables near each other, the picture neither a tall nor a long
// strip, and no boxes overlap, with the default view and with every column
// shown. Run: node scripts/erd-test.ts
import assert from 'node:assert/strict'
import { DEFAULT_OPTIONS, K, layout, stack } from '../src/lib/erd.ts'
import type { ForeignKey, GraphTable } from '../src/lib/types.ts'

let seed = 7
const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648

function schema(parentsOf: (i: number) => number[], names: string[]) {
  const tables: GraphTable[] = []
  const fks: ForeignKey[] = []
  names.forEach((name, i) => {
    const columns = [{ name: 'id', type: 'bigint', nullable: false, pk: true }]
    for (const p of new Set(parentsOf(i))) {
      columns.push({ name: `${names[p]}_id`, type: 'bigint', nullable: false, pk: false })
      fks.push({ name: `fk${i}_${p}`, table: name, columns: [`${names[p]}_id`], refSchema: '', refTable: names[p], refColumns: ['id'], onUpdate: '', onDelete: '', one: false, required: true, looseRef: false })
    }
    for (let c = 3 + Math.floor(rnd() * 9); c > 0; c--) columns.push({ name: `c${c}`, type: 'varchar(100)', nullable: true, pk: false })
    tables.push({ name, view: false, comment: '', columns })
  })
  return { tables, fks }
}

function check(label: string, g: { tables: GraphTable[]; fks: ForeignKey[] }, hub: string, maxAspect: number) {
  const L = layout(g, (t, px) => t.length * px * 0.58)
  const linked = [...L.nodes.values()].filter((b) => !b.iso)
  for (const lines of [K.MAX_LINES + 1, 30]) {
    const h = new Map([...L.nodes].map(([n, b]) => [n, K.HEAD_H + Math.min(b.lines.length, lines) * K.LINE_H + K.BOX_PAD]))
    const P = stack(L, h, DEFAULT_OPTIONS)
    for (let i = 0; i < linked.length; i++)
      for (let j = i + 1; j < linked.length; j++) {
        const [a, b] = [linked[i], linked[j]]
        const [ya, yb] = [P.y.get(a.name)!, P.y.get(b.name)!]
        assert.ok(!(a.x < b.x + b.w && b.x < a.x + a.w && ya < yb + h.get(b.name)! && yb < ya + h.get(a.name)!), `${label}: ${a.name} over ${b.name}`)
      }
    if (lines > K.MAX_LINES + 1) break // every column shown: only that nothing overlaps

    assert.ok(P.w / P.h > 0.7 && P.w / P.h < maxAspect, `${label}: picture ${P.w}x${P.h}`)
    const c = (n: string) => [L.nodes.get(n)!.x + L.nodes.get(n)!.w / 2, P.y.get(n)! + h.get(n)! / 2]
    const xs = linked.map((b) => c(b.name)[0])
    const ys = linked.map((b) => c(b.name)[1])
    const [x0, x1, y0, y1] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)]
    const [hx, hy] = c(hub)
    const off = Math.hypot((2 * hx - x0 - x1) / (x1 - x0), (2 * hy - y0 - y1) / (y1 - y0)) / Math.SQRT2
    assert.ok(off < 0.35, `${label}: ${hub} is ${off.toFixed(2)} of the way out to a corner`)

    const len = L.edges.map((e) => Math.hypot(c(e.a)[0] - c(e.b)[0], c(e.a)[1] - c(e.b)[1])).sort((a, b) => a - b)
    const boxW = linked.reduce((s, b) => s + b.w, 0) / linked.length
    assert.ok(len[len.length >> 1] < 3 * boxW, `${label}: median line ${Math.round(len[len.length >> 1])} px`)
    // (the hub's 50 tables ring it, the outer ones some 8 boxes off)
    assert.ok(len[len.length - 1] < 10 * boxW, `${label}: longest line ${Math.round(len[len.length - 1])} px`)
  }
}

// 95 tables, most referencing `member`
const hub = Array.from({ length: 95 }, (_, i) => (i ? `t${i}` : 'member'))
check('hub', schema((i) => (i < 8 ? [] : Array.from({ length: 1 + Math.floor(rnd() * 2.2) }, () => (rnd() < 0.45 ? 0 : Math.floor(rnd() * i)))), hub), 'member', 2.5)

// 7 clusters of 11 around their own hub, each linked to the one before: no
// longer a long strip (2.3:1 before the pull to the middle)
const chain = Array.from({ length: 77 }, (_, i) => `c${Math.floor(i / 11)}_${i % 11}`)
check('chain', schema((i) => (i % 11 ? [i - (i % 11) + (rnd() < 0.6 ? 0 : Math.floor(rnd() * (i % 11)))] : i ? [i - 11 + Math.floor(rnd() * 11)] : []), chain), 'c3_0', 1.9)
console.log('erd ok')
