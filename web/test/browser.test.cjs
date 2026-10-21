const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const { chromium } = require('playwright');
const fixture = require('./fixture.cjs');
const output = path.join(__dirname,'out');

test('offline browser matrix: directions, session trees, layouts, widths and themes', {timeout:180000}, async () => {
  fs.mkdirSync(output,{recursive:true});
  const html = fs.readFileSync(path.join(__dirname,'../index.html'),'utf8');
  assert.ok(html.includes('(function boot() {'), 'fixture injection anchor');
  const served = html.replace('(function boot() {',fixture+'\n(function boot() {').replace('startDemo(); kick(); return;',"window.__test.load(); kick(); return;");
  const server = http.createServer((req,res)=>{res.setHeader('Content-Type','text/html; charset=utf-8');res.end(req.url.startsWith('/demo?')?html:served);});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser = await chromium.launch({...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH?{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}:{})});
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors=[], external=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>{if(new URL(route.request().url()).hostname!=='127.0.0.1'){external.push(route.request().url());return route.abort();}return route.continue();});
  const url = `http://127.0.0.1:${server.address().port}/?demo=1`;
  const snapshot = () => page.evaluate(()=>window.__test.inspect());
  const direction = async d => {await page.locator(`[data-d="${d}"]`).click();await page.waitForFunction(d=>document.querySelector(`[data-d="${d}"]`).getAttribute('aria-pressed')==='true',d);};
  let cases=0;
  try {
    await page.setViewportSize({width:1280,height:900});await page.goto(url);
    await page.waitForFunction(()=>window.__lr?.nodes>0);
    assert.equal((await snapshot()).direction,'rtl','fresh browser preserves upstream default');
    for(const width of [1280,390]) for(const theme of ['light','dark']) for(const layout of ['tabs','split']) for(const d of ['rtl','ltr']) for(const mode of ['expanded','collapsed','none','children','folded']) {
      await page.setViewportSize({width,height:900});await page.emulateMedia({colorScheme:theme});
      // Split is intentionally tabs below 900px; set the stored preference as well.
      if(width>=900) await page.locator(`[data-l="${layout}"]`).click();
      else await page.evaluate(l=>localStorage.setItem('lr-layout',l),layout);
      await page.evaluate(mode=>window.__test.load(mode),mode);await direction(d);
      await page.waitForFunction(()=>window.__test.settled());
      const s = await snapshot();
      assert.equal(s.layout,width<900?'tabs':layout);
      assert.equal(s.direction,d);
      const c=s.nodes.find(n=>n.key==='c1'), x=s.nodes.find(n=>n.key==='x1');
      assert.ok(d==='ltr'?c.x<s.hub.x&&s.hub.x<x.x:x.x<s.hub.x&&s.hub.x<c.x);
      const sessions=s.nodes.filter(n=>n.type==='s');
      assert.equal(sessions.length===0,mode==='none'||mode==='collapsed');
      if(mode==='children') assert.equal(sessions.length,3);
      if(mode==='folded') assert.ok(sessions.some(n=>n.fold?.length===5));
      for(const n of s.nodes) {
        assert.equal(n.hit,n.key,`stale hit box: ${n.key}`);
        if(n.type!=='r') {assert.ok(n.x>=0&&n.x+n.w<=s.W+.01,`off-canvas card ${n.key}`);assert.ok(n.y-n.h/2>=0&&n.y+n.h/2<=s.FH+.5,`off-canvas vertical card ${n.key}`);}
      }
      for(const {path:p} of s.paths) {
        assert.ok(p);
        for(let i=1;i<p.segs.length;i++) assert.deepEqual(p.segs[i-1][3],p.segs[i][0]);
        for(const g of p.segs) { assert.ok(g.every(pt=>Number.isFinite(pt.x)&&Number.isFinite(pt.y))); assert.ok(d==='ltr'?g[3].x>=g[0].x:g[3].x<=g[0].x,'path travels in selected direction'); }
      }
      const labels=await page.evaluate(()=>window.__test.drawLabels());
      const entry=labels.find(l=>l.text.startsWith(width<560?'凭证 ·':'入口 ·')), client=labels.find(l=>l.text.startsWith(width<560?'客户端 ·':'harness /'));
      assert.ok(d==='ltr'?client.x<entry.x:entry.x<client.x,'column labels follow nodes');
      if(mode==='expanded'||mode==='folded') await page.screenshot({path:path.join(output,`${width}-${theme}-${layout}-${d}-${mode}.png`),fullPage:true});
      cases++;
    }
    // Canvas pointer interaction uses actual browser coordinates after direction changes.
    await page.setViewportSize({width:1280,height:900});await page.locator('[data-l="tabs"]').click();
    for(const d of ['rtl','ltr']) {
      await page.evaluate(()=>window.__test.load());await direction(d);
      await page.waitForTimeout(150);
      let p=await page.evaluate(()=>window.__lr.firstClient);await page.mouse.click(p.x,p.y);
      await page.waitForFunction(()=>window.__lr.sessions===0);
      p=await page.evaluate(()=>window.__lr.firstClient);await page.mouse.click(p.x,p.y);
      await page.waitForFunction(()=>window.__lr.sessions>0);
      for(const state of ['queued','routed','done','failed']) {
        if(state==='failed') await page.evaluate(()=>window.__test.state('routed'));
        const flight=await page.evaluate(state=>window.__test.state(state),state);
        assert.equal(flight.kind,state==='queued'||state==='routed'?'fwd':'ret');
        if(flight.kind==='ret') assert.equal(flight.ok,state==='done');
        const before=await snapshot();
        const other=d==='ltr'?'rtl':'ltr';await direction(other);
        const after=await snapshot();
        assert.equal(after.records,before.records);
        assert.ok(after.flights.some(f=>f.kind===flight.kind),'direction change keeps active flight');
        for(const {path:p} of after.paths) for(let i=1;i<p.segs.length;i++) assert.deepEqual(p.segs[i-1][3],p.segs[i][0]);
        await page.screenshot({path:path.join(output,`state-${d}-${state}.png`),fullPage:true});
        await direction(d);
      }
    }
    await page.evaluate(()=>window.__test.pinTimeline());
    await page.locator('[data-v="time"]').click();
    const timeline=await snapshot();const image=await page.locator('#cvT').screenshot();
    await direction('rtl');await direction('ltr');
    const after=await snapshot();
    for(const key of ['view','rangeMs','viewEnd','records','paused']) assert.equal(after[key],timeline[key],`timeline ${key}`);
    assert.deepEqual(await page.locator('#cvT').screenshot(),image,'timeline pixels unchanged');
    await page.locator('[data-l="split"]').click();
    await page.reload();await page.waitForFunction(()=>window.__lr?.nodes>0);
    assert.equal((await snapshot()).direction,'ltr');assert.equal((await snapshot()).layout,'split');
    assert.equal(await page.evaluate(()=>localStorage.getItem('lr-range')),'30000');
    for(const [lang,label] of [['en','Left-to-right'],['pt-BR','esquerda'],['zh-CN','从左到右']]) {
      await page.evaluate(lang=>document.documentElement.lang=lang,lang);
      await page.waitForFunction(label=>document.querySelector('#cvF').getAttribute('aria-label').includes(label),label);
    }
    // Unrecognized and inaccessible storage both retain RTL without breaking rendering.
    await page.evaluate(()=>localStorage.setItem('lr-flow-direction','invalid'));await page.reload();await page.waitForFunction(()=>window.__lr?.nodes>0);assert.equal((await snapshot()).direction,'rtl');
    await page.addInitScript(()=>{Storage.prototype.getItem=()=>{throw new Error('blocked');};Storage.prototype.setItem=()=>{throw new Error('blocked');};});
    await page.reload();await page.waitForFunction(()=>window.__lr?.nodes>0);assert.equal((await snapshot()).direction,'rtl');await direction('ltr');
    // Smoke-test the uninstrumented shipped page with its own offline simulator too.
    await page.goto(url.replace('/?','/demo?')+'&kids=8&quiet=1');await page.waitForFunction(()=>window.__lr?.nodes>0);
    await direction('ltr');await direction('rtl');
    assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
    fs.writeFileSync(path.join(output,'results.json'),JSON.stringify({matrixCases:cases,errors,external,checks:['pointer collapse/expand','queued/routed/success/failure flights survive switch','continuous paths','timeline state and pixel equality','reload preferences','language labels','invalid/blocked storage']},null,2));
    console.log(`${cases} matrix cases plus interaction, animation, persistence and timeline checks passed`);
  } finally {await browser.close();await new Promise(resolve=>server.close(resolve));}
});
