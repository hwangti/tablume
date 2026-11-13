// ERD layout checks: a hub schema (one table most others reference) comes
// out about as wide as tall, parents stay left of their children, and no
// box sits far below the one before it. Run: node scripts/erd-test.ts
import assert from 'node:assert/strict'
import { DEFAULT_OPTIONS, K, isWaypoint, layout, stack } from '../src/lib/erd.ts'
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
const h = new Map([...L.nodes].map(([n, b]) => [n, K.HEAD_H + Math.min(b.lines.length, K.MAX_LINES + 1) * K.LINE_H + K.BOX_PAD]))
const P = stack(L, h, DEFAULT_OPTIONS, () => K.HEAD_H)
assert.ok(P.w / P.h > 0.7 && P.w / P.h < 2.5, `aspect ${P.w}x${P.h}`)

for (const e of L.edges) {
  const [a, b] = [L.nodes.get(e.a)!, L.nodes.get(e.b)!]
  if (a.band === b.band && e.a !== e.b) assert.ok(a.x > b.x, `${e.b} left of ${e.a}`)
}
for (const G of L.groups)
  for (const l of G.layers) {
    // each slot (a box, or a line passing: a waypoint) at most GAP_FAR below the last
    let last = ''
    let slots = 0
    for (const n of l) {
      slots++
      if (isWaypoint(n)) continue
      if (last) {
        const gap = P.y.get(n)! - P.y.get(last)! - h.get(last)!
        assert.ok(gap <= slots * K.GAP_FAR + 1, `${n} ${Math.round(gap)} px below ${last}`)
      }
      ;[last, slots] = [n, 0]
    }
  }
console.log('erd ok')
