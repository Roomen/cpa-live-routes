const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, '../index.html'), 'utf8');

// Execute the shipped geometry, not a second implementation. No DOM or dependencies.
function geometry(direction, session, exit) {
  const between = (a, b) => html.slice(html.indexOf(a), html.indexOf(b));
  const src = `const flowDirection = ${JSON.stringify(direction)};
    const leftToRight = () => flowDirection === 'ltr';
    const clamp = (x,a,b) => Math.max(a,Math.min(b,x));
    const expanded = new Set(${session ? '[1]' : '[]'}), sKey = new Map([[1,'s1']]);
    const hub = {x:400,w:100};
    const F = {nodes: new Map([
      ['c1',{type:'c',x:100,y:80,w:80}], ['s1',{type:'s',x:250,y:100,w:60}],
      ['r1',{type:'r',ry:120}], ['x1',{type:'x',x:600,y:140,w:90}]
    ])};
    ${between('const pathKeys =', '\n\nfunction computeFlow')}
    ${html.slice(html.indexOf('const portOut ='), html.indexOf('\n', html.indexOf('const resolveS =')))}`;
  // Keep only the geometry section, up to resolveS; later drawing needs the DOM.
  const end = src.indexOf('\n', src.indexOf('const resolveS ='));
  const code = src.slice(0, end < 0 ? undefined : end);
  const ctx = vm.createContext({});
  vm.runInContext(code + `\nthis.result = {p: buildPath({client:1,session:${session ? 1 : 0},rule:1,exit:${exit ? 1 : 0}}), keys:pathKeys({client:1,session:${session ? 1 : 0},rule:1,exit:${exit ? 1 : 0}}),posAt,restS};`, ctx);
  return ctx.result;
}

for (const direction of ['rtl','ltr']) for (const session of [false,true]) for (const exit of [false,true]) {
  test(`${direction}: session=${session}, exit=${exit}: continuous forward and return path`, () => {
    const {p, keys, posAt, restS} = geometry(direction, session, exit);
    assert.ok(p);
    assert.deepEqual(Array.from(keys), [...(session ? direction === 'ltr' ? ['c1','s1'] : ['s1','c1'] : ['c1']), 'r1', ...(exit ? ['x1'] : [])]);
    assert.equal(p.segs.length, (session ? 4 : 2) + (exit ? 1 : 0));
    for (let i=1;i<p.segs.length;i++) {
      assert.deepEqual(p.segs[i-1][3], p.segs[i][0], `gap at segment ${i}`);
      for (const sign of [-1,1]) {
        const a = posAt(p, i/p.segs.length-sign*1e-8), b = posAt(p, i/p.segs.length+sign*1e-8);
        assert.ok(Math.hypot(a.x-b.x,a.y-b.y) < .001, 'animation jumps at boundary');
      }
    }
    const q = posAt(p,restS(p,0,false));
    assert.equal(q.x, direction === 'ltr' ? 400 : 500);
    if (exit) assert.equal(posAt(p,restS(p,0,true)).x, direction === 'ltr' ? 600 : 690);
    for (let k=0;k<=5;k++) assert.ok(restS(p,k,false) <= restS(p,0,false));
  });
}

test('the unpatched LTR session path fails the continuity regression', () => {
  const {p} = geometry('ltr',true,true);
  // Removing the session bridge reproduces the original customization's defect.
  const broken = p.segs.filter((_,i) => i !== 1);
  assert.notDeepEqual(broken[0][3],broken[1][0]);
  assert.equal(broken[1][0].x-broken[0][3].x,60);
});
