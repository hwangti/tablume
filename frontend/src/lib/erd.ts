// ERD layout: boxes for tables, lines for foreign keys, parents left of
// their children. Lines leave a box from the side, at the row of their
// column, so the columns of a layered layout run left to right:
//   1. ranks: a table that references nothing is rank 0, one that references
//      it rank 1, and so on (cycles are cut); a rank is a column, or several
//      side by side when one column would make the group much taller than
//      wide (spread);
//   2. connected groups are laid out one by one, so unrelated tables don't mix;
//   3. a line that skips columns gets a waypoint in each: it counts in the
//      ordering below and keeps a little room where the line passes (the
//      line itself is one curve, and may pass behind a box);
//   4. inside a column, order by the mean of the neighbours' rows, then swap
//      neighbours while that removes crossings, keeping the best order. Rows
//      count: two lines into one box cross or not by the rows they meet;
//   5. the groups are packed on shelves to a 2.2:1 picture;
//   6. tables in no foreign key go in a grid underneath.
// The order and x are worked out once; the view options change box heights
// and rows, from which stack() works out y, lining rows up across columns
// (but never pulling a box more than GAP_FAR from the one above it).
import type { Column, ForeignKey, SchemaGraph } from './types'

export const K = {
  BOX_W: 210, // minimum width; grows with the content up to BOX_MAX
  BOX_MAX: 330,
  HEAD_H: 26,
  LINE_H: 17,
  MAX_LINES: 9, // columns shown before "… n more"
  GAP_COL: 110, // between columns: room for the crow's feet and the curve
  GAP_V: 30, // between boxes in a column
  GAP_FAR: 120, // the most a box is pulled away from the one before it
  GAP_WP: 14, // around a waypoint
  PAD: 40,
  GAP_GROUP: 130, // between unconnected groups on a shelf
  GAP_BAND: 120, // between shelves
  ASPECT: 2.2,
  ISO_GAP: 28, // the grid has no lines to route, so it's tighter
  ISO_LBL: 30, // room for the grid's caption
  CMT_H: 15, // the header grows by this with the table comment
  CMT_LINE: 12, // a column comment's line
  BOX_PAD: 8,
  MARK_C: 17, // crow's foot length at the child end
  MARK_P: 13.5, // at the parent end
}

/** Text width in px for a size; bold for titles. */
export type Measure = (text: string, px: number, bold?: boolean) => number

export interface Line {
  name: string
  type: string // as shown: upper case, display width dropped, long ENUMs cut
  typeFull: string // '' unless cut
  kind: 'pk' | 'fk' | ''
  nullable: boolean
  ai: boolean
  comment: string
  audit: boolean // created_* / updated_* / deleted_* / version
}

export interface Node {
  name: string
  comment: string
  lines: Line[] // keys first, then the rest, each in column order
  w: number
  x: number
  rank: number // column inside its group; row in the grid
  band: number // shelf; the grid is a band of its own
  iso: boolean // in no foreign key
}

export interface Edge {
  fk: ForeignKey
  a: string // child table
  b: string // parent table
  ac: string // the column the line leaves from (the first of a composite key)
  bc: string
  via: string[] // waypoints, from the child's side
}

export interface Layout {
  nodes: Map<string, Node>
  edges: Edge[]
  groups: Group[]
  isoBand: number // -1: no grid
  mainW: number // width without the grid
  stats: { tables: number; iso: number; edges: number; layers: number; cross: number; cross0: number }
}

export interface ViewOptions {
  iso: boolean // tables in no FK
  audit: boolean
  all: boolean // every column, not the first MAX_LINES
  tableComment: boolean
  columnComment: boolean
  nullable: boolean
  ai: boolean
}

export const DEFAULT_OPTIONS: ViewOptions = {
  iso: true,
  audit: false,
  all: false,
  tableComment: true,
  columnComment: true,
  nullable: true,
  ai: true,
}

/** The audit columns unless the caller names its own. */
export const DEFAULT_AUDIT = ['created_*', 'updated_*', 'deleted_*', 'version']
const isAudit = (name: string) => /^(created|updated|deleted)_|^version$/i.test(name)

// Upper case outside quotes: ENUM('a','b') keeps its values.
const upperSQL = (s: string) => s.replace(/('(?:[^']|'')*')|[^']+/g, (m, q) => q ?? m.toUpperCase())

// INT(11)'s 11 is a display width, not a limit (MySQL 8 dropped it).
// TINYINT(1) stays: by convention it's a boolean.
function typeText(raw: string): [string, string] {
  let t = upperSQL(raw)
  if (!/^TINYINT\(1\)/.test(t)) t = t.replace(/^((?:TINY|SMALL|MEDIUM|BIG)?INT)\(\d+\)/, '$1')
  return t.length > 26 ? [t.slice(0, 25) + '…', t] : [t, '']
}

function line(c: Column, kind: Line['kind'], audit: (column: string) => boolean): Line {
  const [type, typeFull] = typeText(c.type)
  return {
    name: c.name,
    type,
    typeFull,
    kind,
    nullable: c.nullable,
    ai: /auto_increment/i.test(c.extra ?? ''),
    comment: c.comment ?? '',
    audit: kind !== 'pk' && audit(c.name),
  }
}

function linesOf(cols: Column[], fkCols: Set<string>, audit: (column: string) => boolean): Line[] {
  const keyed = cols.filter((c) => c.pk || fkCols.has(c.name))
  return [
    ...keyed.map((c) => line(c, c.pk ? 'pk' : 'fk', audit)),
    ...cols.filter((c) => !c.pk && !fkCols.has(c.name)).map((c) => line(c, '', audit)),
  ]
}

// Width from every line (audit columns too): the layout must not change
// with the options. A name too long for it is cut with … when drawn.
function boxW(name: string, lines: Line[], m: Measure): number {
  let w = 20 + m(name, 12, true)
  for (const l of lines) w = Math.max(w, 20 + 11 + m(l.name, 11) + 14 + m(l.type, 10))
  return Math.min(K.BOX_MAX, Math.max(K.BOX_W, Math.ceil(w / 2) * 2))
}

/** The box's height with the default options, before the view stacks it. */
function firstH(lines: Line[]): number {
  const plain = lines.filter((l) => !l.audit).length
  let shown = Math.min(plain, K.MAX_LINES)
  if (plain - shown === 1) shown++ // "… 1 more" takes the line it hides
  return K.HEAD_H + shown * K.LINE_H + (plain > shown ? K.LINE_H : 0) + K.BOX_PAD
}

const byName = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0)

function ranks(nodes: string[], edges: Edge[]): Map<string, number> {
  const up = new Map<string, Set<string>>() // child -> parents
  for (const e of edges) if (e.a !== e.b) (up.get(e.a) ?? up.set(e.a, new Set()).get(e.a)!).add(e.b)
  const rank = new Map<string, number>()
  const calc = (n: string, path: Set<string>): number => {
    if (rank.has(n)) return rank.get(n)!
    if (path.has(n)) return 0 // a cycle: cut it here
    path.add(n)
    let r = 0
    for (const p of up.get(n) ?? []) r = Math.max(r, calc(p, path) + 1)
    path.delete(n)
    rank.set(n, r)
    return r
  }
  for (const n of nodes) calc(n, new Set())
  return rank
}

/**
 * Columns for one group. One column per rank makes a tall strip when many
 * tables share a rank (a hub everything references): then the tables are
 * dealt into more columns, each still right of its parents, until the group
 * comes out about `aspect` wide. Tables with the longest chains after them
 * go first, so the chains keep their columns; the rest go as far left as
 * there is room, near their parents.
 */
function spread(names: string[], rank: Map<string, number>, edges: Edge[], h: (n: string) => number, w: (n: string) => number, aspect: number) {
  const inG = new Set(names)
  const up = new Map<string, string[]>()
  const down = new Map<string, string[]>()
  for (const e of edges) {
    // (a line against the ranks, from a cut cycle, binds nothing)
    if (!inG.has(e.a) || !inG.has(e.b) || rank.get(e.b)! >= rank.get(e.a)!) continue
    ;(up.get(e.a) ?? up.set(e.a, []).get(e.a)!).push(e.b)
    ;(down.get(e.b) ?? down.set(e.b, []).get(e.b)!).push(e.a)
  }
  const colH = new Map<number, number>()
  const colW = new Map<number, number>()
  for (const n of names) {
    const r = rank.get(n)!
    colH.set(r, (colH.get(r) ?? 0) + h(n) + K.GAP_V)
    colW.set(r, Math.max(colW.get(r) ?? 0, w(n)))
  }
  const ws = [...colW.values()]
  const W = ws.reduce((s, v) => s + v + K.GAP_COL, 0)
  const total = [...colH.values()].reduce((s, v) => s + v, 0)
  if (W >= aspect * Math.max(...colH.values())) return rank
  // C columns of total / C each: C · (column width) ≈ aspect · total / C
  const C = Math.max(ws.length, Math.round(Math.sqrt((aspect * total * ws.length) / W)))
  const cap = total / C

  const tail = new Map<string, number>()
  const tailOf = (n: string): number => {
    if (!tail.has(n)) tail.set(n, Math.max(-1, ...(down.get(n) ?? []).map(tailOf)) + 1)
    return tail.get(n)!
  }
  const col = new Map<string, number>()
  const after = (n: string) => Math.max(-1, ...(up.get(n) ?? []).map((p) => col.get(p) ?? Infinity))
  let left = names.filter((n) => !up.has(n))
  const waiting = new Set(names.filter((n) => up.has(n)))
  for (let k = 0; left.length || waiting.size; k++) {
    for (const n of waiting) if (after(n) < k) waiting.delete(n), left.push(n)
    left.sort((a, b) => tailOf(b) - tailOf(a) || after(a) - after(b) || rank.get(a)! - rank.get(b)! || byName(a, b))
    let used = 0
    const rest: string[] = []
    for (const n of left) {
      if (used && used + h(n) > cap) rest.push(n)
      else col.set(n, k), (used += h(n) + K.GAP_V)
    }
    left = rest
  }
  return col
}

/** Connected groups, the biggest first. */
function components(nodes: string[], edges: Edge[]): string[][] {
  const adj = new Map<string, string[]>()
  for (const e of edges) {
    if (e.a === e.b) continue
    ;(adj.get(e.a) ?? adj.set(e.a, []).get(e.a)!).push(e.b)
    ;(adj.get(e.b) ?? adj.set(e.b, []).get(e.b)!).push(e.a)
  }
  const seen = new Set<string>()
  const out: string[][] = []
  for (const n of nodes) {
    if (seen.has(n)) continue
    const g: string[] = []
    const stack = [n]
    while (stack.length) {
      const c = stack.pop()!
      if (seen.has(c)) continue
      seen.add(c)
      g.push(c)
      for (const k of adj.get(c) ?? []) if (!seen.has(k)) stack.push(k)
    }
    out.push(g.sort(byName))
  }
  return out.sort((a, b) => b.length - a.length || byName(a[0], b[0]))
}

// A slot in a column: a table, or a waypoint of a line that crosses the
// column (an id starting with WP): it counts when ordering the column and
// keeps a little room where the line passes.
const WP = '\u0001'
export const isWaypoint = (id: string) => id.startsWith(WP)

/** Lines between neighbouring columns: their ends, each with a fraction of
 *  the box's height (the column's row) so lines to one box keep their order. */
type Seg = { u: string; fu: number; v: string; fv: number }
/** A slot's lines to the column on its left (-1) or right (+1). */
type Nb = { other: string; side: -1 | 1; col: string; otherCol: string }

export interface Group {
  layers: string[][] // columns, left (parents) to right; each top to bottom
  colX: number[] // the columns' left edges, from the group's
  colW: number[]
  nb: Map<string, Nb[]>
  x0: number // the group's left edge in the picture
  band: number
  w: number
}

/** Orders the slots of each column for few crossings: mean sweeps both
 *  ways and neighbour swaps, keeping the best order seen. */
function order(names: string[], rank: Map<string, number>, edges: Edge[], frac: (n: string, col: string) => number) {
  const inG = new Set(names)
  const used = [...new Set(names.map((n) => rank.get(n)!))].sort((a, b) => a - b)
  const colOf = new Map(names.map((n) => [n, used.indexOf(rank.get(n)!)]))
  const R = used.length
  const layers: string[][] = Array.from({ length: R }, () => [])
  for (const n of [...names].sort(byName)) layers[colOf.get(n)!].push(n)

  const segs: Seg[][] = Array.from({ length: Math.max(0, R - 1) }, () => [])
  const nb = new Map<string, Nb[]>()
  const addNb = (n: string, x: Nb) => (nb.get(n) ?? nb.set(n, []).get(n)!).push(x)
  const fr = (n: string, col: string) => (isWaypoint(n) ? 0.5 : frac(n, col))
  edges.forEach((e, ei) => {
    // (only this group's: the other groups' lines keep their waypoints)
    if (e.a === e.b || !inG.has(e.a) || !inG.has(e.b)) return
    e.via = []
    const ca = colOf.get(e.a)!
    const cb = colOf.get(e.b)!
    if (ca === cb) return // in one column (a cut cycle): drawn beside it
    const step = cb < ca ? -1 : 1
    for (let k = ca + step; k !== cb; k += step) {
      const id = `${WP}${ei}:${k}`
      layers[k].push(id)
      e.via.push(id)
    }
    const pts = [e.a, ...e.via, e.b]
    for (let i = 0; i + 1 < pts.length; i++) {
      const [p, q] = [pts[i], pts[i + 1]]
      const pc = i === 0 ? e.ac : ''
      const qc = i + 2 === pts.length ? e.bc : ''
      const [l, lc, r, rc] = step < 0 ? [q, qc, p, pc] : [p, pc, q, qc]
      segs[Math.min(ca + i * step, ca + (i + 1) * step)].push({ u: l, fu: fr(l, lc), v: r, fv: fr(r, rc) })
      addNb(l, { other: r, side: 1, col: lc, otherCol: rc })
      addNb(r, { other: l, side: -1, col: rc, otherCol: lc })
    }
  })

  const posOf = (ls: string[][]) => {
    const p = new Map<string, number>()
    for (const l of ls) l.forEach((n, i) => p.set(n, i))
    return p
  }
  const crossPair = (ss: Seg[], pos: Map<string, number>) => {
    let c = 0
    for (let i = 0; i < ss.length; i++) {
      const a = ss[i]
      const au = pos.get(a.u)! + a.fu
      const av = pos.get(a.v)! + a.fv
      for (let j = i + 1; j < ss.length; j++) {
        const b = ss[j]
        if ((au - pos.get(b.u)! - b.fu) * (av - pos.get(b.v)! - b.fv) < 0) c++
      }
    }
    return c
  }
  const crossings = (ls: string[][]) => {
    const pos = posOf(ls)
    return segs.reduce((s, ss) => s + crossPair(ss, pos), 0)
  }

  // Each slot to the mean of its neighbours on one side; the ones with none stay.
  const sweep = (ls: string[][], right: boolean) => {
    ls = ls.map((l) => [...l])
    const ks = right ? [...Array(R).keys()].slice(1) : [...Array(R).keys()].reverse().slice(1)
    for (const k of ks) {
      const pos = posOf(ls)
      const key = new Map<string, number>()
      ls[k].forEach((n, i) => {
        const vs = (nb.get(n) ?? []).filter((x) => x.side === (right ? -1 : 1)).map((x) => pos.get(x.other)! + fr(x.other, x.otherCol))
        key.set(n, vs.length ? vs.reduce((a, b) => a + b, 0) / vs.length : i)
      })
      ls[k].sort((a, b) => key.get(a)! - key.get(b)! || pos.get(a)! - pos.get(b)!)
    }
    return ls
  }

  // Swapping neighbours u, v only changes how u's lines cross v's.
  const transpose = (ls: string[][]) => {
    ls = ls.map((l) => [...l])
    const pos = posOf(ls)
    const between = (u: string, v: string) => {
      let c = 0
      for (const a of nb.get(u) ?? [])
        for (const b of nb.get(v) ?? []) {
          if (a.side !== b.side) continue
          const d1 = pos.get(u)! + fr(u, a.col) - pos.get(v)! - fr(v, b.col)
          const d2 = pos.get(a.other)! + fr(a.other, a.otherCol) - pos.get(b.other)! - fr(b.other, b.otherCol)
          if (d1 * d2 < 0) c++
        }
      return c
    }
    for (let improved = true, guard = 0; improved && guard < 12; guard++) {
      improved = false
      for (const l of ls)
        for (let i = 0; i + 1 < l.length; i++) {
          const [u, v] = [l[i], l[i + 1]]
          const before = between(u, v)
          if (!before) continue
          pos.set(u, i + 1), pos.set(v, i)
          if (between(u, v) < before) {
            ;[l[i], l[i + 1]] = [v, u]
            improved = true
          } else pos.set(u, i), pos.set(v, i + 1)
        }
    }
    return ls
  }

  let cur = layers
  let best = layers
  let bestC = crossings(layers)
  const c0 = bestC
  for (let it = 0; it < 24 && bestC > 0; it++) {
    cur = transpose(sweep(cur, it % 2 === 0))
    const c = crossings(cur)
    if (c < bestC) [bestC, best] = [c, cur]
  }
  return { layers: best, nb, c0, c: bestC }
}

export function layout(g: SchemaGraph, m: Measure, audit: (column: string) => boolean = isAudit): Layout {
  const cols = new Map(g.tables.map((t) => [t.name, t]))
  const edges: Edge[] = g.fks
    .filter((f) => !f.refSchema && cols.has(f.table) && cols.has(f.refTable))
    .map((f) => ({ fk: f, a: f.table, b: f.refTable, ac: f.columns[0] ?? '', bc: f.refColumns[0] ?? '', via: [] }))
  const fkCols = new Map<string, Set<string>>()
  for (const f of g.fks) for (const c of f.columns) (fkCols.get(f.table) ?? fkCols.set(f.table, new Set()).get(f.table)!).add(c)

  const inFk = new Set(edges.flatMap((e) => [e.a, e.b]))
  const nodesIn = g.tables.filter((t) => inFk.has(t.name)).map((t) => t.name)
  const isoNames = g.tables.filter((t) => !t.view && !inFk.has(t.name)).map((t) => t.name)

  const nodes = new Map<string, Node>()
  const heights = new Map<string, number>()
  for (const n of [...nodesIn, ...isoNames]) {
    const t = cols.get(n)!
    const lines = linesOf(t.columns, fkCols.get(n) ?? new Set(), audit)
    nodes.set(n, { name: n, comment: t.comment, lines, w: boxW(n, lines, m), x: 0, rank: 0, band: 0, iso: !inFk.has(n) })
    heights.set(n, firstH(lines))
  }
  const bw = new Map([...nodes].map(([n, b]) => [n, b.w]))
  const rank = ranks(nodesIn, edges)

  // Where a column's row sits in its box, as drawn with the default options.
  const frac = (n: string, col: string) => {
    const shown = nodes.get(n)!.lines.filter((l) => !l.audit)
    const i = shown.findIndex((l) => l.name === col)
    const vis = Math.min(shown.length, K.MAX_LINES)
    return i < 0 || i >= vis ? 0.5 : (i + 1) / (vis + 2)
  }

  let cross = 0
  let cross0 = 0
  let layerN = 0
  const groups: Group[] = components(nodesIn, edges).map((names) => {
    const o = order(names, spread(names, rank, edges, (n) => heights.get(n)!, (n) => bw.get(n)!, K.ASPECT), edges, frac)
    cross += o.c
    cross0 += o.c0
    layerN = Math.max(layerN, o.layers.length)
    const colW = o.layers.map((l) => Math.max(0, ...l.filter((n) => !isWaypoint(n)).map((n) => bw.get(n)!)))
    const colX: number[] = []
    let x = 0
    for (const w of colW) colX.push(x), (x += w + K.GAP_COL)
    o.layers.forEach((l, k) =>
      l.forEach((n) => {
        if (!isWaypoint(n)) Object.assign(nodes.get(n)!, { x: colX[k] + (colW[k] - bw.get(n)!) / 2, rank: k })
      }),
    )
    return { layers: o.layers, colX, colW, nb: o.nb, x0: 0, band: 0, w: x - K.GAP_COL }
  })

  // Shelves: one long strip gets very wide when there are many groups.
  // Heights here are the default view's; stack() works out the real ones.
  const tallOf = (G: Group) =>
    Math.max(...G.layers.map((l) => l.reduce((s, n) => s + (isWaypoint(n) ? K.GAP_WP : heights.get(n)! + K.GAP_V), 0)))
  const len = groups.reduce((s, G) => s + G.w + K.GAP_GROUP, 0)
  const wide = Math.max(0, ...groups.map((G) => G.w))
  const tall = Math.max(0, ...groups.map(tallOf))
  const rows = tall > 0 ? Math.max(1, Math.round(Math.sqrt(len / (K.ASPECT * tall)))) : 1
  const target = Math.max(wide, Math.ceil(len / rows))
  let band = 0
  let curX = 0
  let maxX = 0
  for (const G of groups) {
    if (curX > 0 && curX + G.w > target) band++, (curX = 0)
    G.x0 = K.PAD + curX
    G.band = band
    for (const l of G.layers)
      for (const n of l) {
        if (isWaypoint(n)) continue
        const b = nodes.get(n)!
        b.x += G.x0
        b.band = band
      }
    maxX = Math.max(maxX, G.x0 + G.w)
    curX += G.w + K.GAP_GROUP
  }
  const mainW = Math.round(maxX) + K.PAD

  // The grid: as wide as the diagram, or near ASPECT when that's narrow.
  let isoBand = -1
  if (isoNames.length) {
    isoBand = groups.length ? band + 1 : 0
    const area = isoNames.reduce((s, n) => s + (bw.get(n)! + K.ISO_GAP) * (heights.get(n)! + K.ISO_GAP), 0)
    const gridW = Math.max(maxX - K.PAD, Math.sqrt(area * K.ASPECT), ...isoNames.map((n) => bw.get(n)!))
    let row = 0
    let cx = 0
    for (const n of isoNames) {
      const b = nodes.get(n)!
      if (cx > 0 && cx + b.w > gridW) row++, (cx = 0)
      Object.assign(b, { x: K.PAD + cx, rank: row, band: isoBand })
      cx += b.w + K.ISO_GAP
    }
  }

  return {
    nodes,
    edges,
    groups,
    isoBand,
    mainW,
    stats: { tables: nodesIn.length, iso: isoNames.length, edges: edges.length, layers: layerN, cross, cross0 },
  }
}

// ---------------------------------------------------------------- per view

export interface Row {
  line: Line
  y: number // baseline, from the box's top
  h: number // the room it takes (with its comment), for hit testing
  name: string // cut to fit unless the box is shown in full
  tail: { text: string; cls: 'ai' | 'nul' | 'ty' }[]
  comment: string // '' when hidden
}

export interface BoxView {
  headH: number
  comment: string // '' when hidden
  rows: Row[]
  more: number
  moreY: number
  h: number
}

/** Cuts text to max px with a trailing …; a binary search, so few measurements. */
export function clip(t: string, px: number, max: number, m: Measure): string {
  if (max <= 0) return ''
  if (m(t, px) <= max) return t
  let lo = 0
  let hi = t.length
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (m(t.slice(0, mid) + '…', px) <= max) lo = mid
    else hi = mid - 1
  }
  return lo ? t.slice(0, lo) + '…' : ''
}

/** Which lines show and where. full: nothing is cut (the box under the pointer). */
export function boxView(b: Node, o: ViewOptions, m: Measure, markW: number, full: boolean): BoxView {
  const withCmt = !!b.comment && o.tableComment
  const headH = K.HEAD_H + (withCmt ? K.CMT_H : 0)
  let y = headH + 14
  // Audit columns are filtered out first, so the limit counts what's left.
  const vis = b.lines.filter((l) => o.audit || !l.audit)
  let lim = o.all ? vis.length : Math.min(vis.length, K.MAX_LINES)
  if (vis.length - lim === 1) lim = vis.length
  const rows: Row[] = []
  for (const l of vis.slice(0, lim)) {
    const tail: Row['tail'] = []
    if (o.ai && l.ai) tail.push({ text: 'AI', cls: 'ai' })
    if (o.nullable && l.nullable) tail.push({ text: 'NULL', cls: 'nul' })
    tail.push({ text: (full && l.typeFull) || l.type, cls: 'ty' })
    const tw = m(tail.map((p) => p.text).join(' '), 10)
    const ind = l.kind ? markW : 0
    const r: Row = { line: l, y, h: K.LINE_H, name: full ? l.name : clip(l.name, 11, b.w - 30 - tw - ind, m), tail, comment: '' }
    y += K.LINE_H
    if (l.comment && o.columnComment) {
      r.comment = full ? l.comment : clip(l.comment, 9.5, b.w - 31, m)
      y += K.CMT_LINE
      r.h += K.CMT_LINE
    }
    rows.push(r)
  }
  const more = vis.length - lim
  const moreY = y
  if (more) y += K.LINE_H
  return {
    headH,
    comment: withCmt ? (full ? b.comment : clip(b.comment, 10.5, b.w - 20, m)) : '',
    rows,
    more,
    moreY,
    h: y - 14 + K.BOX_PAD,
  }
}

/**
 * y of every shown box. Inside a group each slot is
 * pulled so the rows its lines leave from line up with the other ends
 * (straight lines), keeping the column's order and the gaps between.
 * anchor: a column's row, from the top of its box.
 */
export function stack(L: Layout, h: Map<string, number>, o: ViewOptions, anchor: (n: string, col: string) => number) {
  const y = new Map<string, number>()
  const hOf = (n: string) => (isWaypoint(n) ? 0 : h.get(n)!)
  const off = (n: string, col: string) => (isWaypoint(n) ? 0 : anchor(n, col))
  const gap = (a: string, b: string) => (isWaypoint(a) || isWaypoint(b) ? K.GAP_WP : K.GAP_V)
  const median = (vs: number[]) => {
    vs.sort((a, b) => a - b)
    const c = vs.length
    return c % 2 ? vs[c >> 1] : (vs[c / 2 - 1] + vs[c / 2]) / 2
  }

  const groupH: number[] = []
  const groupT: Map<string, number>[] = []
  for (const G of L.groups) {
    const t = new Map<string, number>()
    for (const l of G.layers) {
      let cur = 0
      l.forEach((n, i) => (i && (cur += gap(l[i - 1], n)), t.set(n, cur), (cur += hOf(n))))
    }
    const R = G.layers.length
    for (let it = 0; it < 12; it++) {
      // one way, the other, and last both: the median over every neighbour
      const side = it === 11 ? 0 : it % 2 ? 1 : -1
      const ks = side === 1 ? [...Array(R).keys()].reverse() : [...Array(R).keys()]
      for (const k of ks) {
        const l = G.layers[k]
        const want = l.map((n) => {
          const vs = (G.nb.get(n) ?? []).filter((x) => !side || x.side === side).map((x) => t.get(x.other)! + off(x.other, x.otherCol) - off(n, x.col))
          return vs.length ? median(vs) : t.get(n)!
        })
        // Packing from the top only pushes down, so pack from both ends
        // and take the middle; both keep the gaps, so the mean does too.
        const top: number[] = []
        let prev = -Infinity
        l.forEach((n, i) => (top.push(Math.max(want[i], i ? prev + gap(l[i - 1], n) : -Infinity)), (prev = top[i] + hOf(n))))
        const bot: number[] = []
        let next = Infinity
        for (let i = l.length - 1; i >= 0; i--) {
          bot[i] = Math.min(want[i], i < l.length - 1 ? next - gap(l[i], l[i + 1]) - hOf(l[i]) : Infinity)
          next = bot[i]
        }
        l.forEach((n, i) => t.set(n, (top[i] + bot[i]) / 2))
        // A box its lines pull far off would sit alone (the lines are
        // long anyway): no gap over GAP_FAR, the boxes after move up.
        let lift = 0
        l.forEach((n, i) => {
          if (i) lift += Math.max(0, t.get(n)! - lift - (t.get(l[i - 1])! + hOf(l[i - 1])) - Math.max(gap(l[i - 1], n), K.GAP_FAR))
          t.set(n, t.get(n)! - lift)
        })
      }
    }
    const min = Math.min(...t.values())
    let max = 0
    for (const [n, v] of t) t.set(n, v - min), (max = Math.max(max, v - min + hOf(n)))
    groupH.push(max)
    groupT.push(t)
  }

  // Bands of groups, one under the other, then the grid.
  let top = K.PAD
  const bands = [...new Set(L.groups.map((G) => G.band))].sort((a, b) => a - b)
  for (const band of bands) {
    let bh = 0
    L.groups.forEach((G, gi) => {
      if (G.band !== band) return
      const t = groupT[gi]
      for (const l of G.layers) for (const n of l) if (!isWaypoint(n)) y.set(n, top + t.get(n)!)
      bh = Math.max(bh, groupH[gi])
    })
    top += bh + K.GAP_BAND
  }
  let isoY = 0
  if (o.iso && L.isoBand >= 0) {
    const rowH = new Map<number, number>()
    for (const b of L.nodes.values()) if (b.iso) rowH.set(b.rank, Math.max(rowH.get(b.rank) ?? 0, h.get(b.name)!))
    isoY = top + K.ISO_LBL
    const rowY = new Map<number, number>()
    let cur = isoY
    for (const r of [...rowH.keys()].sort((a, b) => a - b)) rowY.set(r, cur), (cur += rowH.get(r)! + K.ISO_GAP)
    for (const b of L.nodes.values()) if (b.iso) y.set(b.name, rowY.get(b.rank)!)
    top = cur - K.ISO_GAP + K.GAP_BAND
  }
  const showIso = o.iso && L.isoBand >= 0
  const fullW = Math.round(Math.max(L.mainW - K.PAD, ...[...L.nodes.values()].filter((b) => b.iso).map((b) => b.x + b.w))) + K.PAD
  return { y, isoY, w: showIso ? fullW : L.mainW, h: Math.round(top - K.GAP_BAND + K.PAD) }
}

type Box = { x: number; y: number; w: number; h: number; anchor: (col: string) => number }

/**
 * A relation's line, hooked to the rows of its columns. Boxes apart side
 * by side: facing sides; overlapping (a parent right above): both leave
 * the same side, so the line doesn't cross the boxes. The line stops where
 * the crow's foot starts; the marker draws the rest.
 */
export function edgePath(A: Box, B: Box, ac: string, bc: string, self: boolean): string {
  const f = (n: number) => n.toFixed(1)
  if (self) {
    const sx = A.x + A.w + K.MARK_C
    const ex = A.x + A.w + K.MARK_P
    const sy = A.anchor(ac)
    const ey = A.anchor(bc)
    const g = sy === ey ? 6 : 0
    return `M${f(sx)},${f(sy - g)} C${f(sx + 34)},${f(sy - g - 8)} ${f(ex + 34)},${f(ey + g + 8)} ${f(ex)},${f(ey + g)}`
  }
  const AL = A.x
  const AR = A.x + A.w
  const BL = B.x
  const BR = B.x + B.w
  let sa: number
  let sb: number
  if (AR < BL) [sa, sb] = [1, -1]
  else if (BR < AL) [sa, sb] = [-1, 1]
  else sa = sb = AL + AR >= BL + BR ? 1 : -1
  const x1 = (sa > 0 ? AR : AL) + sa * K.MARK_C
  const y1 = A.anchor(ac)
  const x2 = (sb > 0 ? BR : BL) + sb * K.MARK_P
  const y2 = B.anchor(bc)
  const d = sa === sb ? Math.max(40, Math.min(90, Math.abs(y2 - y1) * 0.3)) : Math.max(34, Math.abs(x2 - x1) * 0.45)
  return `M${f(x1)},${f(y1)} C${f(x1 + sa * d)},${f(y1)} ${f(x2 + sb * d)},${f(y2)} ${f(x2)},${f(y2)}`
}
