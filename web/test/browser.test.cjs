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
  let browser;
  try {
  browser = await chromium.launch({...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH?{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}:{})});
  const context = await browser.newContext({ locale: 'zh-CN' });
  const page = await context.newPage();
  const errors=[], external=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('console',m=>{if(m.type()==='error') errors.push(m.text());});
  await page.route('**/*',route=>{if(new URL(route.request().url()).hostname!=='127.0.0.1'){external.push(route.request().url());return route.abort();}return route.continue();});
  const url = `http://127.0.0.1:${server.address().port}/?demo=1`;
  const snapshot = () => page.evaluate(()=>window.__test.inspect());
  const direction = async d => {await page.locator(`[data-d="${d}"]`).click();await page.waitForFunction(d=>document.querySelector(`[data-d="${d}"]`).getAttribute('aria-pressed')==='true',d);};
  let cases=0;
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
    for(const [lang,label] of [['en','Left-to-right'],['pt-br','esquerda'],['zh-cn','从左到右']]) {
      await page.locator(`#segLang button[data-lang="${lang}"]`).click();
      await page.waitForFunction(label=>document.querySelector('#cvF').getAttribute('aria-label').includes(label),label);
    }

    // Test configuration drawer: opening, interactive controls, live yaml, toolbar sync, and reset
    await page.click('#bCfg');
    await page.waitForFunction(() => !document.querySelector('#cfgp').hidden);
    assert.equal(await page.locator('#bCfg').getAttribute('aria-pressed'), 'true');
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('enabled: true')));

    // Speed sync: panel -> toolbar
    await page.click('#segCfgSpeed button[data-val="2x"]');
    assert.equal(await page.locator('#segSpeed button[data-s="2"]').getAttribute('aria-pressed'), 'true');
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('animation_speed: "2x"')));
    assert.equal(await page.evaluate(() => localStorage.getItem('lr-speed')), '2');

    // Speed sync: toolbar -> panel
    await page.click('#segSpeed button[data-s="0.5"]');
    assert.equal(await page.locator('#segCfgSpeed button[data-val="0.5x"]').getAttribute('aria-pressed'), 'true');
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('animation_speed: "0.5x"')));

    // Layout sync: panel -> toolbar (split was stored before the reload above)
    assert.equal(await page.locator('#segCfgLayout button[data-val="split"]').getAttribute('aria-pressed'), 'true');
    await page.click('#segCfgLayout button[data-val="tabs"]');
    assert.equal((await snapshot()).layout, 'tabs');
    assert.equal(await page.locator('#segLayout button[data-l="tabs"]').getAttribute('aria-pressed'), 'true');
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('layout: "tabs"')));

    // Reduced animation toggle
    await page.click('#card_reduced_animation .cfg-switch');
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('reduced_animation: true')));
    assert.equal(await page.evaluate(() => localStorage.getItem('lr-reduced')), 'true');

    // Active window preset
    const preset5 = page.locator('#card_active_window_minutes button[data-v="5"]');
    if (await preset5.count() > 0) {
      await preset5.click();
      assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('active_window_minutes: 5')));
      assert.equal(await page.evaluate(() => localStorage.getItem('lr-active-window')), '5');
    }

    // Copy YAML
    await page.click('#bCopyCfg');
    await page.waitForTimeout(100);

    // Reset to defaults
    await page.click('#bResetCfg');
    assert.equal(await page.evaluate(() => localStorage.getItem('lr-speed')), null);
    assert.equal(await page.evaluate(() => localStorage.getItem('lr-reduced')), null);
    assert.equal(await page.evaluate(() => localStorage.getItem('lr-active-window')), null);
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('animation_speed: "1x"')));
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.startsWith('# ')), 'yaml is marked as a partial snippet');

    // Active window never exceeds retention, even with a larger stored override
    const yamlNum = async key => +(await page.locator('#cfgy').textContent()).match(new RegExp(key + ': (\\d+)'))[1];
    await page.evaluate(() => window.__test.config({ retention_s: 3600 }));
    await page.click('#card_active_window_minutes button[data-v="60"]');
    assert.equal(await yamlNum('active_window_minutes'), 60);
    await page.evaluate(() => window.__test.config({ retention_s: 1200 }));
    assert.equal(await page.locator('#rng_active_window_minutes').getAttribute('max'), '20');
    assert.equal(await yamlNum('retention_minutes'), 20);
    assert.equal(await yamlNum('active_window_minutes'), 20);
    assert.deepEqual(await page.locator('#card_active_window_minutes .cfg-preset-btn').evaluateAll(bs => bs.map(b => b.dataset.v)), ['5', '10', '20']);
    await page.click('#bResetCfg');
    await page.evaluate(() => window.__test.config({}));

    // System reduced motion is not reported as a local override
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.evaluate(() => window.__test.config({}));
    assert.equal(await page.locator('#sw_reduced_animation').isChecked(), false);
    assert.equal(await page.locator('#card_reduced_animation').evaluate(e => e.classList.contains('modified')), false);
    assert.ok(await page.locator('#cfgy').textContent().then(t => t.includes('reduced_animation: false')));
    assert.equal(await page.locator('#cfgStatus').textContent(), '');
    await page.emulateMedia({ reducedMotion: 'no-preference' });

    // Close drawer
    await page.click('#bCfg');
    await page.waitForFunction(() => document.querySelector('#cfgp').hidden);
    assert.equal(await page.locator('#bCfg').getAttribute('aria-pressed'), 'false');
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
  } finally {if (browser) await browser.close();await new Promise(resolve=>server.close(resolve));}
});
