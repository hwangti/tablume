// node scripts/panes-test.ts — checks the split tree (src/lib/panes.ts).
import assert from 'node:assert/strict'
import { group, groups, splitAt, removeGroup, layout, resize, equalize, validate, zoneAt, normalize } from '../src/lib/panes.ts'

const ids = (p: Parameters<typeof groups>[0]) => groups(p).map((g) => g.id).join(',')

// a, then b to its right, then c below b
let t = splitAt(group('a'), 'a', 'right', group('b'))
assert.equal(ids(t), 'a,b')
t = splitAt(t, 'b', 'bottom', group('c'))
assert.equal(ids(t), 'a,b,c')
assert.equal(t.kind === 'split' && t.dir, 'row')

// d left of a: same direction, a's half is halved
t = splitAt(t, 'a', 'left', group('d'))
assert.equal(ids(t), 'd,a,b,c')
assert.deepEqual(t.kind === 'split' && t.sizes, [0.25, 0.25, 0.5])

const { rects, bars } = layout(t)
assert.deepEqual(rects.get('c'), { x: 0.5, y: 0.5, w: 0.5, h: 0.5 })
assert.equal(bars.length, 3)

// taking b out leaves c alone in its column: the column goes, c takes its place
t = removeGroup(t, 'b')
assert.equal(ids(t), 'd,a,c')
assert.ok(t.kind === 'split' && t.children.every((c) => c.kind === 'group'))
// the last group stays
assert.equal(ids(removeGroup(group('x'), 'x')), 'x')

// resize keeps the pair's total and the minimum
let r = resize(t, [], 0, 0.01, 0.1)
assert.ok(r.kind === 'split' && Math.abs(r.sizes[0] - 0.1) < 1e-9 && Math.abs(r.sizes[0] + r.sizes[1] - 0.5) < 1e-9)
r = equalize(r, [])
assert.ok(r.kind === 'split' && r.sizes.every((s) => Math.abs(s - 1 / 3) < 1e-9))

// a nested split the same way is merged
const n = normalize({ kind: 'split', dir: 'row', sizes: [1, 1], children: [group('p'), { kind: 'split', dir: 'row', sizes: [1, 1], children: [group('q'), group('r')] }] })
assert.ok(n.kind === 'split' && n.children.length === 3 && n.sizes[2] === 0.25)

// workspace file
assert.equal(validate({ kind: 'split', dir: 'row', sizes: [1], children: [group('a')] }), null)
assert.equal(validate({ kind: 'split', dir: 'row', sizes: [1, 1], children: [group('a'), group('a')] }), null)
assert.equal(validate('x'), null)
assert.equal(ids(validate(JSON.parse(JSON.stringify(t)))!), 'd,a,c')

// drop zones
const box = { x: 0, y: 0, w: 100, h: 100 }
assert.equal(zoneAt(box, 50, 50), 'center')
assert.equal(zoneAt(box, 95, 50), 'right')
assert.equal(zoneAt(box, 50, 10), 'top')

console.log('panes ok')
