// ERD layout: boxes for tables, lines for foreign keys, parents above their
// children. A port of the layered layout in tables.php's erd.php:
//   1. ranks: a table that references nothing is rank 0, one that references
//      it rank 1, and so on (cycles are cut);
//   2. connected groups are laid out one by one, so unrelated tables don't mix;
//   3. inside a rank, order by the median of the neighbours' positions, then
//      swap neighbours while that removes crossings, keeping the best order;
//   4. x pulls each box toward its neighbours' centres, keeping the gaps;
//   5. the groups are packed on shelves to a 2.2:1 picture;
//   6. tables in no foreign key go in a grid underneath.
// The horizontal layout is done once; the view options only change box
// heights, which stack() turns into y.
import type { Column, ForeignKey, SchemaGraph } from './types'

export const K = {
  BOX_W: 210, // minimum width; grows with the content up to BOX_MAX
  BOX_MAX: 330,
  HEAD_H: 26,
  LINE_H: 17,
  MAX_LINES: 9, // columns shown before "… n more"
  GAP_X: 46,
  GAP_Y: 82,
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
  rank: number // row inside its band
  band: number // shelf; the grid is a band of its own
  iso: boolean // in no foreign key
}

export interface Edge {
  fk: ForeignKey
  a: string // child table
  b: string // parent table
  ac: string // the column the line leaves from (the first of a composite key)
  bc: string
}

export interface Layout {
  nodes: Map<string, Node>
  edges: Edge[]
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

const isAudit = (name: string, kind: string) => kind !== 'pk' && /^(created|updated|deleted)_|^version$/i.test(name)

// Upper case outside quotes: ENUM('a','b') keeps its values.
const upperSQL = (s: string) => s.replace(/('(?:[^']|'')*')|[^']+/g, (m, q) => q ?? m.toUpperCase())

// INT(11)'s 11 is a display width, not a limit (MySQL 8 dropped it).
// TINYINT(1) stays: by convention it's a boolean.
function typeText(raw: string): [string, string] {
  let t = upperSQL(raw)
  if (!/^TINYINT\(1\)/.test(t)) t = t.replace(/^((?:TINY|SMALL|MEDIUM|BIG)?INT)\(\d+\)/, '$1')
  return t.length > 26 ? [t.slice(0, 25) + '…', t] : [t, '']
}

function line(c: Column, kind: Line['kind']): Line {
  const [type, typeFull] = typeText(c.type)
  return {
    name: c.name,
    type,
    typeFull,
    kind,
    nullable: c.nullable,
    ai: /auto_increment/i.test(c.extra ?? ''),
    comment: c.comment ?? '',
    audit: isAudit(c.name, kind),
  }
}

function linesOf(cols: Column[], fkCols: Set<string>): Line[] {
  const keyed = cols.filter((c) => c.pk || fkCols.has(c.name))
  return [
    ...keyed.map((c) => line(c, c.pk ? 'pk' : 'fk')),
    ...cols.filter((c) => !c.pk && !fkCols.has(c.name)).map((c) => line(c, '')),
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

type Rel = [string, string, number, number] // child, parent, their ranks

/** Lays out one group: rows, order inside them, x from 0. */
function place(nodes: string[], rank: Map<string, number>, bw: Map<string, number>, edges: Edge[]) {
  const inGroup = new Set(nodes)
  const byRank = new Map<number, string[]>()
  for (const n of nodes) (byRank.get(rank.get(n)!) ?? byRank.set(rank.get(n)!, []).get(rank.get(n)!)!).push(n)
  // Inside a group the ranks run 0, 1, 2 … without gaps (a node of rank r
  // has a parent of rank r-1 in it), so the row index is the rank.
  let layers = [...byRank.keys()].sort((a, b) => a - b).map((r) => byRank.get(r)!.sort(byName))
  const rowOf = new Map<string, number>()
  layers.forEach((l, r) => l.forEach((n) => rowOf.set(n, r)))
  const R = layers.length

  // Only lines between rows move the layout: no self-references, none inside a row.
  const rel: Rel[] = []
  for (const e of edges) {
    if (e.a === e.b || !inGroup.has(e.a) || !inGroup.has(e.b)) continue
    const ra = rowOf.get(e.a)!
    const rb = rowOf.get(e.b)!
    if (ra !== rb) rel.push([e.a, e.b, ra, rb])
  }
  const nbr = new Map<string, [string, number][]>()
  for (const [a, b, ra, rb] of rel) {
    ;(nbr.get(a) ?? nbr.set(a, []).get(a)!).push([b, rb])
    ;(nbr.get(b) ?? nbr.set(b, []).get(b)!).push([a, ra])
  }
  const posOf = (ls: string[][]) => {
    const p = new Map<string, number>()
    for (const l of ls) l.forEach((n, i) => p.set(n, i))
    return p
  }

  // Crossings as seen: (row, slot) as coordinates, lines that skip rows included.
  const crossings = (pos: Map<string, number>) => {
    const seg = rel.map(([a, b, ra, rb]) => (ra <= rb ? [ra, pos.get(a)!, rb, pos.get(b)!] : [rb, pos.get(b)!, ra, pos.get(a)!]))
    let c = 0
    for (let i = 0; i < seg.length; i++) {
      const [y1, x1, y2, x2] = seg[i]
      const s1 = (x2 - x1) / (y2 - y1)
      for (let j = i + 1; j < seg.length; j++) {
        const [y3, x3, y4, x4] = seg[j]
        const lo = Math.max(y1, y3)
        const hi = Math.min(y2, y4)
        if (hi <= lo) continue
        const s2 = (x4 - x3) / (y4 - y3)
        const d1 = x1 + s1 * (lo - y1) - (x3 + s2 * (lo - y3))
        const d2 = x1 + s1 * (hi - y1) - (x3 + s2 * (hi - y3))
        if ((d1 < 0 && d2 > 0) || (d1 > 0 && d2 < 0)) c++
      }
    }
    return c
  }

  const median = (vs: number[]) => {
    vs.sort((a, b) => a - b)
    const c = vs.length
    return c % 2 ? vs[c >> 1] : (vs[c / 2 - 1] + vs[c / 2]) / 2
  }

  // Median sweep, looking one way only so the signal stays clear.
  const sweep = (ls: string[][], down: boolean) => {
    ls = ls.map((l) => [...l])
    const rows = down ? [...Array(R).keys()].slice(1) : [...Array(R).keys()].reverse().slice(1)
    for (const r of rows) {
      const pos = posOf(ls)
      const key = new Map<string, number>()
      ls[r].forEach((n, i) => {
        const vs = (nbr.get(n) ?? []).filter(([, rm]) => (down ? rm < r : rm > r)).map(([m]) => pos.get(m)!)
        key.set(n, vs.length ? median(vs) : i)
      })
      ls[r].sort((a, b) => key.get(a)! - key.get(b)! || byName(a, b))
    }
    return ls
  }

  // Swap neighbours while that uncrosses lines to the same row.
  const transpose = (ls: string[][]) => {
    ls = ls.map((l) => [...l])
    for (let improved = true, guard = 0; improved && guard < 8; guard++) {
      improved = false
      const pos = posOf(ls)
      for (const l of ls) {
        for (let i = 0; i + 1 < l.length; i++) {
          const [u, v] = [l[i], l[i + 1]]
          let ab = 0
          let ba = 0
          for (const [m, rm] of nbr.get(u) ?? [])
            for (const [k, rk] of nbr.get(v) ?? []) {
              if (rm !== rk) continue
              if (pos.get(m)! > pos.get(k)!) ab++
              else if (pos.get(m)! < pos.get(k)!) ba++
            }
          if (ab > ba) {
            ;[l[i], l[i + 1]] = [v, u]
            pos.set(u, i + 1)
            pos.set(v, i)
            improved = true
          }
        }
      }
    }
    return ls
  }

  // A heuristic: keep the best order seen, not the last.
  let best = layers
  let bestC = crossings(posOf(layers))
  const c0 = bestC
  for (let it = 0; it < 8 && bestC > 0; it++) {
    layers = transpose(sweep(layers, it % 2 === 0))
    const c = crossings(posOf(layers))
    if (c < bestC) [bestC, best] = [c, layers]
  }
  // Polish: try each swap against the full count (long lines included).
  // O(nodes × lines²), so skipped for big schemas.
  layers = best
  for (let pass = 0; pass < (rel.length <= 200 ? 2 : 0) && bestC > 0; pass++) {
    let moved = false
    for (let r = 0; r < R; r++) {
      for (let i = 0; i + 1 < layers[r].length; i++) {
        const tryL = layers.map((l) => [...l])
        ;[tryL[r][i], tryL[r][i + 1]] = [tryL[r][i + 1], tryL[r][i]]
        const c = crossings(posOf(tryL))
        if (c < bestC) [bestC, layers, moved] = [c, tryL, true]
      }
    }
    if (!moved) break
  }

  // x: left edges, keeping "previous right edge + GAP_X"; centres pull
  // toward the neighbours' centres (lines leave from the middle).
  const x = new Map<string, number>()
  for (const l of layers) {
    let cur = 0
    for (const n of l) x.set(n, cur), (cur += bw.get(n)! + K.GAP_X)
  }
  for (let it = 0; it < 6; it++) {
    const down = it % 2 === 0
    for (const r of down ? [...Array(R).keys()] : [...Array(R).keys()].reverse()) {
      const want = new Map<string, number>()
      for (const n of layers[r]) {
        const vs = (nbr.get(n) ?? []).filter(([, rm]) => (down ? rm < r : rm > r)).map(([m]) => x.get(m)! + bw.get(m)! / 2)
        want.set(n, vs.length ? median(vs) - bw.get(n)! / 2 : x.get(n)!)
      }
      // Packing from the left only pushes right, so pack from both sides
      // and take the middle; both keep the gaps, so the mean does too.
      const L = new Map<string, number>()
      let prev = -Infinity
      for (const n of layers[r]) L.set(n, Math.max(want.get(n)!, prev + K.GAP_X)), (prev = L.get(n)! + bw.get(n)!)
      const Rt = new Map<string, number>()
      let next = Infinity
      for (const n of [...layers[r]].reverse()) Rt.set(n, Math.min(want.get(n)!, next - K.GAP_X - bw.get(n)!)), (next = Rt.get(n)!)
      for (const n of layers[r]) x.set(n, (L.get(n)! + Rt.get(n)!) / 2)
    }
    const min = Math.min(...x.values())
    for (const [k, v] of x) x.set(k, v - min)
  }
  return { x, layers, c0, c: bestC }
}

export function layout(g: SchemaGraph, m: Measure): Layout {
  const cols = new Map(g.tables.map((t) => [t.name, t]))
  const edges: Edge[] = g.fks
    .filter((f) => !f.refSchema && cols.has(f.table) && cols.has(f.refTable))
    .map((f) => ({ fk: f, a: f.table, b: f.refTable, ac: f.columns[0] ?? '', bc: f.refColumns[0] ?? '' }))
  const fkCols = new Map<string, Set<string>>()
  for (const f of g.fks) for (const c of f.columns) (fkCols.get(f.table) ?? fkCols.set(f.table, new Set()).get(f.table)!).add(c)

  const inFk = new Set(edges.flatMap((e) => [e.a, e.b]))
  const nodesIn = g.tables.filter((t) => inFk.has(t.name)).map((t) => t.name)
  const isoNames = g.tables.filter((t) => !t.view && !inFk.has(t.name)).map((t) => t.name)

  const nodes = new Map<string, Node>()
  const heights = new Map<string, number>()
  for (const n of [...nodesIn, ...isoNames]) {
    const t = cols.get(n)!
    const lines = linesOf(t.columns, fkCols.get(n) ?? new Set())
    nodes.set(n, { name: n, comment: t.comment, lines, w: boxW(n, lines, m), x: 0, rank: 0, band: 0, iso: !inFk.has(n) })
    heights.set(n, firstH(lines))
  }
  const bw = new Map([...nodes].map(([n, b]) => [n, b.w]))
  const rank = ranks(nodesIn, edges)

  let cross = 0
  let cross0 = 0
  const groups = components(nodesIn, edges).map((names) => {
    const p = place(names, rank, bw, edges)
    cross += p.c
    cross0 += p.c0
    const rowH = p.layers.map((l) => Math.max(...l.map((n) => heights.get(n)!)))
    p.layers.forEach((l, r) => l.forEach((n) => Object.assign(nodes.get(n)!, { x: p.x.get(n)!, rank: r })))
    return {
      names,
      w: Math.max(...names.map((n) => p.x.get(n)! + bw.get(n)!)),
      h: rowH.reduce((a, b) => a + b, 0) + (rowH.length - 1) * K.GAP_Y,
    }
  })

  // Shelves: one long strip gets very wide when there are many groups.
  const len = groups.reduce((s, G) => s + G.w + K.GAP_GROUP, 0)
  const wide = Math.max(0, ...groups.map((G) => G.w))
  const tall = Math.max(0, ...groups.map((G) => G.h))
  const rows = tall > 0 ? Math.max(1, Math.round(Math.sqrt(len / (K.ASPECT * tall)))) : 1
  const target = Math.max(wide, Math.ceil(len / rows))
  let band = 0
  let curX = 0
  let maxX = 0
  for (const G of groups) {
    if (curX > 0 && curX + G.w > target) band++, (curX = 0)
    for (const n of G.names) {
      const b = nodes.get(n)!
      b.x += K.PAD + curX
      b.band = band
    }
    maxX = Math.max(maxX, K.PAD + curX + G.w)
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
      maxX = Math.max(maxX, K.PAD + cx + b.w)
      cx += b.w + K.ISO_GAP
    }
  }

  const layers = nodesIn.length ? Math.max(...nodesIn.map((n) => nodes.get(n)!.rank)) + 1 : 0
  return {
    nodes,
    edges,
    isoBand,
    mainW,
    stats: { tables: nodesIn.length, iso: isoNames.length, edges: edges.length, layers, cross, cross0 },
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

/** y of every shown box: rows in a band share the tallest box's height. */
export function stack(L: Layout, h: Map<string, number>, o: ViewOptions) {
  const rowH = new Map<string, number>()
  const ranks = new Map<number, Set<number>>()
  for (const b of L.nodes.values()) {
    if (b.iso && !o.iso) continue
    const k = b.band + ':' + b.rank
    rowH.set(k, Math.max(rowH.get(k) ?? 0, h.get(b.name)!))
    ;(ranks.get(b.band) ?? ranks.set(b.band, new Set()).get(b.band)!).add(b.rank)
  }
  const yOf = new Map<string, number>()
  let top = K.PAD
  let isoY = 0
  for (const band of [...ranks.keys()].sort((a, b) => a - b)) {
    const iso = band === L.isoBand
    const gap = iso ? K.ISO_GAP : K.GAP_Y
    let y = iso ? top + K.ISO_LBL : top
    if (iso) isoY = y
    for (const r of [...ranks.get(band)!].sort((a, b) => a - b)) {
      yOf.set(band + ':' + r, y)
      y += rowH.get(band + ':' + r)! + gap
    }
    top = y - gap + K.GAP_BAND
  }
  const y = new Map<string, number>()
  for (const b of L.nodes.values()) if (yOf.has(b.band + ':' + b.rank)) y.set(b.name, yOf.get(b.band + ':' + b.rank)!)
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
