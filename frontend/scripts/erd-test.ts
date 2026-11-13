// ERD layout checks on a generated hub schema (one table most others
// reference): the hub sits in the middle, linked tables near each other,
// the picture about as wide as tall, and no boxes overlapping, with the
// default view and with every column shown. Run: node scripts/erd-test.ts
import assert from 'node:assert/strict'
import { DEFAULT_OPTIONS, K, layout, stack } from '../src/lib/erd.ts'
import type { ForeignKey, GraphTable } from '../src/lib/types.ts'

let seed = 7
const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648
const name = (i: number) => (i ? `t${i}` : 'member')
const tables: GraphTable[] = []
const fks: ForeignKey[] = []
for (let i = 0; i < 95; i++) {
  const columns = [{ name: 'id', type: 'bigint', nullable: false, pk: true }]
  const parents = new Set<number>()
  if (i >= 8) for (let k = 1 + Math.floor(rnd() * 2.2); k > 0; k--) parents.add(rnd() < 0.45 ? 0 : Math.floor(rnd() * i))
  for (const p of parents) {
    columns.push({ name: `${name(p)}_id`, type: 'bigint', nullable: false, pk: false })
    fks.push({ name: `fk${i}_${p}`, table: name(i), columns: [`${name(p)}_id`], refSchema: '', refTable: name(p), refColumns: ['id'], onUpdate: '', onDelete: '', one: false, required: true, looseRef: false })
  }
  for (let c = 3 + Math.floor(rnd() * 9); c > 0; c--) columns.push({ name: `c${c}`, type: 'varchar(100)', nullable: true, pk: false })
  tables.push({ name: name(i), view: false, comment: '', columns })
}

const L = layout({ tables, fks }, (t, px) => t.length * px * 0.58)
const linked = [...L.nodes.values()].filter((b) => !b.iso)
for (const lines of [K.MAX_LINES + 1, 30]) {
  const h = new Map([...L.nodes].map(([n, b]) => [n, K.HEAD_H + Math.min(b.lines.length, lines) * K.LINE_H + K.BOX_PAD]))
  const P = stack(L, h, DEFAULT_OPTIONS)
  for (let i = 0; i < linked.length; i++)
    for (let j = i + 1; j < linked.length; j++) {
      const [a, b] = [linked[i], linked[j]]
      const [ya, yb] = [P.y.get(a.name)!, P.y.get(b.name)!]
      assert.ok(!(a.x < b.x + b.w && b.x < a.x + a.w && ya < yb + h.get(b.name)! && yb < ya + h.get(a.name)!), `${a.name} over ${b.name}`)
    }
  if (lines > K.MAX_LINES + 1) break // every column shown: only that nothing overlaps

  assert.ok(P.w / P.h > 0.7 && P.w / P.h < 2.5, `aspect ${P.w}x${P.h}`)
  const c = (n: string) => [L.nodes.get(n)!.x + L.nodes.get(n)!.w / 2, P.y.get(n)! + h.get(n)! / 2]
  const xs = linked.map((b) => c(b.name)[0])
  const ys = linked.map((b) => c(b.name)[1])
  const [x0, x1, y0, y1] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)]
  const [hx, hy] = c('member')
  const off = Math.hypot((2 * hx - x0 - x1) / (x1 - x0), (2 * hy - y0 - y1) / (y1 - y0)) / Math.SQRT2
  assert.ok(off < 0.35, `the hub is ${off.toFixed(2)} of the way out to a corner`)

  const len = L.edges.map((e) => Math.hypot(c(e.a)[0] - c(e.b)[0], c(e.a)[1] - c(e.b)[1])).sort((a, b) => a - b)
  const boxW = linked.reduce((s, b) => s + b.w, 0) / linked.length
  assert.ok(len[len.length >> 1] < 3 * boxW, `median line ${Math.round(len[len.length >> 1])} px`)
  assert.ok(len[len.length - 1] < 8 * boxW, `longest line ${Math.round(len[len.length - 1])} px`)
}
console.log('erd ok')
