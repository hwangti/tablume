// Split tab groups. The main window's tab area is a tree: a split lays its
// children out in a row or a column, and each leaf is a tab group (a tab
// strip and the active tab under it). Plain functions on plain objects;
// the app keeps the tree in its state and replaces it with what these return.

export type Group = { kind: 'group'; id: string; active: string }
export type Split = { kind: 'split'; dir: 'row' | 'col'; sizes: number[]; children: Pane[] }
export type Pane = Group | Split
export type Side = 'left' | 'right' | 'top' | 'bottom'
/** Fractions of the whole tab area (0–1). */
export type Rect = { x: number; y: number; w: number; h: number }
/** The line between children i and i+1 of the split at path; at is its x (row) or y (col). */
export type Bar = { path: number[]; i: number; dir: 'row' | 'col'; at: number; rect: Rect }

export const group = (id: string, active = ''): Group => ({ kind: 'group', id, active })

export function groups(p: Pane): Group[] {
  return p.kind === 'group' ? [p] : p.children.flatMap(groups)
}

/** Puts the group add beside the group target. */
export function splitAt(root: Pane, target: string, side: Side, add: Group): Pane {
  const dir = side === 'left' || side === 'right' ? 'row' : 'col'
  const after = side === 'right' || side === 'bottom'
  const walk = (p: Pane): Pane => {
    if (p.kind === 'group') return p.id === target ? { kind: 'split', dir, sizes: [0.5, 0.5], children: after ? [p, add] : [add, p] } : p
    const i = p.children.findIndex((c) => c.kind === 'group' && c.id === target)
    if (i >= 0 && p.dir === dir) {
      // already in a split that way: the target's share is halved
      const sizes = [...p.sizes]
      sizes.splice(i, 1, p.sizes[i] / 2, p.sizes[i] / 2)
      const children = [...p.children]
      children.splice(after ? i + 1 : i, 0, add)
      return { ...p, sizes, children }
    }
    return { ...p, children: p.children.map(walk) }
  }
  return walk(root)
}

/** Takes a group out (never the last one); its share goes to the rest. */
export function removeGroup(root: Pane, id: string): Pane {
  const walk = (p: Pane): Pane | null => {
    if (p.kind === 'group') return p.id === id ? null : p
    const children: Pane[] = []
    const sizes: number[] = []
    p.children.forEach((c, i) => {
      const n = walk(c)
      if (n) children.push(n), sizes.push(p.sizes[i])
    })
    return children.length ? { ...p, children, sizes } : null
  }
  return normalize(walk(root) ?? root)
}

/** Sizes add up to 1, a split of one is its child, a split inside a split the same way is merged into it. */
export function normalize(p: Pane): Pane {
  if (p.kind === 'group') return p
  const total = p.sizes.reduce((a, b) => a + b, 0) || 1
  const children: Pane[] = []
  const sizes: number[] = []
  p.children.forEach((c0, i) => {
    const c = normalize(c0)
    const s = p.sizes[i] / total
    if (c.kind === 'split' && c.dir === p.dir) c.children.forEach((cc, j) => (children.push(cc), sizes.push(s * c.sizes[j])))
    else children.push(c), sizes.push(s)
  })
  return children.length === 1 ? children[0] : { kind: 'split', dir: p.dir, sizes, children }
}

/** Where each group goes, and the lines between them. */
export function layout(p: Pane, r: Rect = { x: 0, y: 0, w: 1, h: 1 }, path: number[] = [], out = { rects: new Map<string, Rect>(), bars: [] as Bar[] }) {
  if (p.kind === 'group') {
    out.rects.set(p.id, r)
    return out
  }
  let off = 0
  p.children.forEach((c, i) => {
    const s = p.sizes[i]
    layout(c, p.dir === 'row' ? { x: r.x + off * r.w, y: r.y, w: s * r.w, h: r.h } : { x: r.x, y: r.y + off * r.h, w: r.w, h: s * r.h }, [...path, i], out)
    off += s
    if (i < p.children.length - 1) out.bars.push({ path, i, dir: p.dir, rect: r, at: p.dir === 'row' ? r.x + off * r.w : r.y + off * r.h })
  })
  return out
}

function update(root: Pane, path: number[], fn: (s: Split) => Split): Pane {
  if (root.kind === 'group') return root
  if (!path.length) return fn(root)
  const [i, ...rest] = path
  return { ...root, children: root.children.map((c, k) => (k === i ? update(c, rest, fn) : c)) }
}

/** Moves the line after child i of the split at path to pos (a fraction of that split); neither side goes under min. */
export function resize(root: Pane, path: number[], i: number, pos: number, min: number): Pane {
  return update(root, path, (s) => {
    const start = s.sizes.slice(0, i).reduce((a, b) => a + b, 0)
    const pair = s.sizes[i] + s.sizes[i + 1]
    const m = Math.min(min, pair / 2)
    const a = Math.max(m, Math.min(pair - m, pos - start))
    const sizes = [...s.sizes]
    sizes[i] = a
    sizes[i + 1] = pair - a
    return { ...s, sizes }
  })
}

/** The split at path shared out evenly. */
export function equalize(root: Pane, path: number[]): Pane {
  return update(root, path, (s) => ({ ...s, sizes: s.children.map(() => 1 / s.children.length) }))
}

/** A tree read back from the workspace file, or null if it isn't one. */
export function validate(v: unknown): Pane | null {
  const ok = (p: any): boolean =>
    p?.kind === 'group'
      ? typeof p.id === 'string' && typeof p.active === 'string'
      : p?.kind === 'split' &&
        (p.dir === 'row' || p.dir === 'col') &&
        Array.isArray(p.children) &&
        Array.isArray(p.sizes) &&
        p.children.length >= 2 &&
        p.sizes.length === p.children.length &&
        p.sizes.every((s: unknown) => typeof s === 'number' && s > 0) &&
        p.children.every(ok)
  if (!ok(v)) return null
  const ids = groups(v as Pane).map((g) => g.id)
  return new Set(ids).size === ids.length ? normalize(v as Pane) : null
}

/** Which part of a group a dragged tab is over: an edge (split that way) or the middle (move into it). */
export function zoneAt(r: Rect, x: number, y: number): Side | 'center' {
  const fx = (x - r.x) / r.w
  const fy = (y - r.y) / r.h
  const d: [Side, number][] = [
    ['left', fx],
    ['right', 1 - fx],
    ['top', fy],
    ['bottom', 1 - fy],
  ]
  const [side, dist] = d.reduce((a, b) => (b[1] < a[1] ? b : a))
  return dist < 0.25 ? side : 'center'
}

/** The part of r a drop on that zone would take. */
export function zoneRect(r: Rect, z: Side | 'center'): Rect {
  if (z === 'left') return { ...r, w: r.w / 2 }
  if (z === 'right') return { ...r, x: r.x + r.w / 2, w: r.w / 2 }
  if (z === 'top') return { ...r, h: r.h / 2 }
  if (z === 'bottom') return { ...r, y: r.y + r.h / 2, h: r.h / 2 }
  return r
}
