// ERD layout: boxes for tables, lines for foreign keys. Tables that are
// linked sit near each other, and a table with many links in the middle of
// them, so the picture reads like the data does:
//   1. connected groups are laid out one by one, so unrelated tables don't mix;
//   2. in a group, every line pulls its two tables together and every two
//      tables push apart (springs); the most-linked tables end up central;
//   3. boxes that still overlap are pushed apart;
//   4. the groups are packed on shelves to a 2.2:1 picture;
//   5. tables in no foreign key go in a grid underneath.
// Lines are one curve each, from the side of a box at the row of their
// column, and may pass behind a box. Positions are worked out once, with the
// default view's box heights; stack() moves boxes down where the view options
// make the ones above taller.
import type { Column, ForeignKey, SchemaGraph } from './types'

export const K = {
  BOX_W: 210, // minimum width; grows with the content up to BOX_MAX
  BOX_MAX: 330,
  HEAD_H: 26,
  LINE_H: 17,
  MAX_LINES: 9, // columns shown before "… n more"
  GAP_H: 80, // between boxes side by side: room for the crow's feet and the curve
  GAP_V: 30, // between boxes one above the other
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
  y: number // top, with the default view (tables in a foreign key)
  row: number // row in the grid (tables in none)
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
  grid: boolean // tables in no foreign key, under the rest
  mainW: number // width without the grid
  stats: { tables: number; iso: number; edges: number }
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

/**
 * One group's boxes, by springs: each line pulls its two tables together
 * (by distance²), each two tables push apart (by 1 / distance), cooling
 * down step by step. Distances count in boxes, a box being wider than
 * tall, so the picture comes out about as many boxes wide as high. The
 * start is a spiral, the most-linked tables innermost, so a layout is the
 * same every time. Then boxes that overlap are pushed apart, along the
 * axis they overlap least. Returns the boxes' centres.
 */
function springs(names: string[], edges: Edge[], w: (n: string) => number, h: (n: string) => number) {
  const n = names.length
  const at = new Map(names.map((v, i) => [v, i]))
  const links: [number, number][] = []
  const deg = new Array<number>(n).fill(0)
  for (const e of edges) {
    const a = at.get(e.a)
    const b = at.get(e.b)
    if (a === undefined || b === undefined || a === b) continue
    links.push([a, b])
    deg[a]++
    deg[b]++
  }
  const W = names.map((v) => w(v) + K.GAP_H)
  const H = names.map((v) => h(v) + K.GAP_V)
  const sx = W.reduce((s, v) => s + v, 0) / n
  const sy = H.reduce((s, v) => s + v, 0) / n
  const x = new Float64Array(n)
  const y = new Float64Array(n)
  const inner = [...names.keys()].sort((a, b) => deg[b] - deg[a] || byName(names[a], names[b]))
  inner.forEach((i, r) => {
    x[i] = Math.sqrt(r) * Math.cos(r * 2.4)
    y[i] = Math.sqrt(r) * Math.sin(r * 2.4)
  })
  // ponytail: every pair every step, O(n²); a quadtree (Barnes–Hut) if schemas of thousands of linked tables get slow
  const k = 0.6 // a line's length, in boxes (the overlap pass then spaces them out)
  const dx = new Float64Array(n)
  const dy = new Float64Array(n)
  const STEPS = 300
  for (let it = 0; it < STEPS; it++) {
    const temp = (0.5 + Math.sqrt(n) * 0.1) * (1 - it / STEPS)
    dx.fill(0)
    dy.fill(0)
    for (let i = 0; i < n; i++)
      for (let j = i + 1; j < n; j++) {
        let ux = x[i] - x[j]
        let uy = y[i] - y[j]
        let d2 = ux * ux + uy * uy
        if (d2 < 1e-6) (ux = 1e-3 * (i - j)), (uy = 1e-3), (d2 = ux * ux + uy * uy)
        const f = (k * k) / d2
        dx[i] += ux * f
        dy[i] += uy * f
        dx[j] -= ux * f
        dy[j] -= uy * f
      }
    for (const [a, b] of links) {
      const ux = x[a] - x[b]
      const uy = y[a] - y[b]
      const f = Math.hypot(ux, uy) / k
      dx[a] -= ux * f
      dy[a] -= uy * f
      dx[b] += ux * f
      dy[b] += uy * f
    }
    for (let i = 0; i < n; i++) {
      const d = Math.hypot(dx[i], dy[i])
      if (d > temp) (dx[i] *= temp / d), (dy[i] *= temp / d)
      x[i] += dx[i]
      y[i] += dy[i]
    }
  }

  const cx = Array.from(x, (v) => v * sx)
  const cy = Array.from(y, (v) => v * sy)
  for (let pass = 0; pass < 300; pass++) {
    let moved = false
    for (let i = 0; i < n; i++)
      for (let j = i + 1; j < n; j++) {
        const ox = (W[i] + W[j]) / 2 - Math.abs(cx[i] - cx[j])
        const oy = (H[i] + H[j]) / 2 - Math.abs(cy[i] - cy[j])
        if (ox <= 0 || oy <= 0) continue
        moved = true
        if (ox / sx < oy / sy) {
          const s = (cx[i] < cx[j] || (cx[i] === cx[j] && i < j) ? -1 : 1) * (ox / 2 + 0.5)
          cx[i] += s
          cx[j] -= s
        } else {
          const s = (cy[i] < cy[j] || (cy[i] === cy[j] && i < j) ? -1 : 1) * (oy / 2 + 0.5)
          cy[i] += s
          cy[j] -= s
        }
      }
    if (!moved) break
  }
  return names.map((v, i) => ({ name: v, x: cx[i], y: cy[i] }))
}

export function layout(g: SchemaGraph, m: Measure, audit: (column: string) => boolean = isAudit): Layout {
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
    const lines = linesOf(t.columns, fkCols.get(n) ?? new Set(), audit)
    nodes.set(n, { name: n, comment: t.comment, lines, w: boxW(n, lines, m), x: 0, y: 0, row: 0, iso: !inFk.has(n) })
    heights.set(n, firstH(lines))
  }
  const bw = (n: string) => nodes.get(n)!.w
  const bh = (n: string) => heights.get(n)!

  // each group from its top left corner
  const groups = components(nodesIn, edges).map((names) => {
    const c = springs(names, edges, bw, bh)
    const x0 = Math.min(...c.map((p) => p.x - bw(p.name) / 2))
    const y0 = Math.min(...c.map((p) => p.y - bh(p.name) / 2))
    const at = c.map((p) => ({ name: p.name, x: p.x - bw(p.name) / 2 - x0, y: p.y - bh(p.name) / 2 - y0 }))
    return {
      at,
      w: Math.max(...at.map((p) => p.x + bw(p.name))),
      h: Math.max(...at.map((p) => p.y + bh(p.name))),
    }
  })

  // Shelves: one long strip gets very wide when there are many groups.
  const len = groups.reduce((s, G) => s + G.w + K.GAP_GROUP, 0)
  const tall = Math.max(0, ...groups.map((G) => G.h))
  const rows = tall > 0 ? Math.max(1, Math.round(Math.sqrt(len / (K.ASPECT * tall)))) : 1
  const target = Math.max(0, ...groups.map((G) => G.w), Math.ceil(len / rows))
  let curX = 0
  let top = K.PAD
  let shelfH = 0
  let maxX = 0
  for (const G of groups) {
    if (curX > 0 && curX + G.w > target) (top += shelfH + K.GAP_BAND), (curX = 0), (shelfH = 0)
    for (const p of G.at) Object.assign(nodes.get(p.name)!, { x: K.PAD + curX + p.x, y: top + p.y })
    shelfH = Math.max(shelfH, G.h)
    maxX = Math.max(maxX, K.PAD + curX + G.w)
    curX += G.w + K.GAP_GROUP
  }
  const mainW = Math.round(maxX) + K.PAD

  // The grid: as wide as the diagram, or near ASPECT when that's narrow.
  if (isoNames.length) {
    const area = isoNames.reduce((s, n) => s + (bw(n) + K.ISO_GAP) * (bh(n) + K.ISO_GAP), 0)
    const gridW = Math.max(maxX - K.PAD, Math.sqrt(area * K.ASPECT), ...isoNames.map(bw))
    let row = 0
    let cx = 0
    for (const n of isoNames) {
      const b = nodes.get(n)!
      if (cx > 0 && cx + b.w > gridW) row++, (cx = 0)
      Object.assign(b, { x: K.PAD + cx, row })
      cx += b.w + K.ISO_GAP
    }
  }

  return {
    nodes,
    edges,
    grid: isoNames.length > 0,
    mainW,
    stats: { tables: nodesIn.length, iso: isoNames.length, edges: edges.length },
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
 * y of every shown box: where the layout put it, pushed down where a box
 * above, taller with these view options than the layout's, reaches into it.
 */
export function stack(L: Layout, h: Map<string, number>, o: ViewOptions) {
  const y = new Map<string, number>()
  const placed: { x: number; w: number; y: number; h: number }[] = []
  let top = K.PAD
  const linked = [...L.nodes.values()].filter((b) => !b.iso).sort((a, b) => a.y - b.y || a.x - b.x)
  for (const b of linked) {
    const bh = h.get(b.name)!
    let t = b.y
    for (let again = true; again; ) {
      again = false
      for (const p of placed)
        if (p.x < b.x + b.w && b.x < p.x + p.w && p.y < t + bh + K.GAP_V && t < p.y + p.h + K.GAP_V) (t = p.y + p.h + K.GAP_V), (again = true)
    }
    placed.push({ x: b.x, w: b.w, y: t, h: bh })
    y.set(b.name, t)
    top = Math.max(top, t + bh + K.GAP_BAND)
  }
  let isoY = 0
  const showIso = o.iso && L.grid
  if (showIso) {
    const rowH = new Map<number, number>()
    for (const b of L.nodes.values()) if (b.iso) rowH.set(b.row, Math.max(rowH.get(b.row) ?? 0, h.get(b.name)!))
    isoY = top + K.ISO_LBL
    const rowY = new Map<number, number>()
    let cur = isoY
    for (const r of [...rowH.keys()].sort((a, b) => a - b)) rowY.set(r, cur), (cur += rowH.get(r)! + K.ISO_GAP)
    for (const b of L.nodes.values()) if (b.iso) y.set(b.name, rowY.get(b.row)!)
    top = cur - K.ISO_GAP + K.GAP_BAND
  }
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
