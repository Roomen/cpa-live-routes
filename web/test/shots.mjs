#!/usr/bin/env node
// Playwright checks + screenshots for web/index.html against web/test/mock-server.mjs.
import { createRequire } from 'node:module';
import { spawn, execSync } from 'node:child_process';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(here, 'out');
fs.mkdirSync(OUT, { recursive: true });
for (const f of fs.readdirSync(OUT)) if (f.endsWith('.png') && (!process.argv.includes('--auth-only') || f.startsWith('auth-'))) fs.unlinkSync(path.join(OUT, f));
const pwPath = process.env.PLAYWRIGHT_MODULE || path.join(execSync('npm root -g').toString().trim(), 'playwright');
const { chromium } = createRequire(import.meta.url)(pwPath);

const ALLOWED = new Set(['lr-view', 'lr-layout', 'lr-speed', 'lr-range']);
const failures = [], passes = [];
const check = (ok, msg) => { (ok ? passes : failures).push(msg); console.log((ok ? 'PASS ' : 'FAIL ') + msg); };
const sleep = ms => new Promise(r => setTimeout(r, ms));

const servers = [];
async function startMock(port, auth, scenario) {
  const p = spawn(process.execPath, [path.join(here, 'mock-server.mjs'), `--port=${port}`, `--auth=${auth}`, ...(scenario ? [`--scenario=${scenario}`] : [])], { stdio: ['ignore', 'pipe', 'inherit'] });
  servers.push(p);
  await new Promise((res, rej) => { p.stdout.on('data', d => { if (String(d).includes('listening')) res(); }); p.on('exit', c => rej(new Error('mock exited ' + c))); setTimeout(() => rej(new Error('mock start timeout')), 8000); });
  return `http://127.0.0.1:${port}`;
}

const INIT = `
  window.__rafN = 0; const _raf = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = cb => _raf(t => { window.__rafN++; cb(t); });
  window.__idb = 0; try { const o = IDBFactory.prototype.open; IDBFactory.prototype.open = function () { window.__idb++; return o.apply(this, arguments); }; } catch (e) {}
`;

async function newPage(browser, origin, opts, label, log) {
  const ctx = await browser.newContext({ viewport: opts.viewport, colorScheme: opts.scheme || 'light', reducedMotion: opts.reduced ? 'reduce' : 'no-preference', deviceScaleFactor: opts.dsf || 1, hasTouch: !!opts.touch });
  const initLS = opts.ls || {};
  await ctx.addInitScript(INIT);
  if (Object.keys(initLS).length) await ctx.addInitScript(l => { try { if (!sessionStorage.getItem('__init')) { for (const k in l) localStorage.setItem(k, l[k]); sessionStorage.setItem('__init', '1'); } } catch (e) {} }, initLS);
  const page = await ctx.newPage();
  const errs = [], badReq = [], reqs = [], dataReqs = [];
  page.on('console', m => { if (m.type() === 'error') errs.push('console: ' + m.text()); });
  page.on('pageerror', e => errs.push('pageerror: ' + e.message));
  page.on('request', req => {
    const u = new URL(req.url());
    reqs.push(u.pathname + u.search);
    if (u.pathname.startsWith('/v0/management/live-routes/')) dataReqs.push({ path: u.pathname, method: req.method(), auth: req.headers().authorization });
  });
  await page.route('**/*', route => {
    const u = new URL(route.request().url());
    if (u.origin !== origin) { badReq.push(route.request().url()); return route.abort(); }
    return route.continue();
  });
  return { ctx, page, errs, badReq, reqs, dataReqs, label };
}

async function openLive(h, url) {
  await h.page.goto(url);
  if (new URL(url).searchParams.get('demo') !== '1') {
    await h.page.waitForSelector('#gKeyBox:not([hidden])');
    await h.page.fill('#gKey', 'test-key'); await h.page.click('#gGo');
  }
}

async function storageAudit(h, label) {
  const r = await h.page.evaluate(async () => {
    const keys = Object.keys(localStorage);
    let dbs = 0; try { dbs = indexedDB.databases ? (await indexedDB.databases()).length : 0; } catch (e) {}
    return { keys, session: Object.keys(sessionStorage).filter(k => k !== '__init' && k !== 'lr-key').length, cookie: document.cookie, idb: window.__idb, dbs, href: location.href };
  });
  check(r.keys.every(k => ALLOWED.has(k)), `${label}: localStorage keys ⊆ allowed (${r.keys.join(',') || 'key'})`);
  check(r.session === 0 && r.cookie === '' && r.idb === 0 && r.dbs === 0, `${label}: no sessionStorage (except the tab key)/cookie/IndexedDB`);
  return r;
}
function endChecks(h, label) {
  check(h.reqs.every(r => !r.startsWith('/v0/resource/plugins/live-routes/api/') && !r.startsWith('/v0/management/live-routes/session')), `${label}: no legacy data/session endpoint`);
  check(h.dataReqs.every(r => r.method === 'GET' && /^Bearer .+/.test(r.auth || '')), `${label}: every data call carries a Bearer management key`);
  check(h.errs.length === 0, `${label}: no console/page errors${h.errs.length ? ' -> ' + h.errs.slice(0, 3).join(' | ') : ''}`);
  check(h.badReq.length === 0, `${label}: only mock-origin requests${h.badReq.length ? ' -> ' + h.badReq.join(',') : ''}`);
}

async function scenario(browser, origin, src, layout, theme, phone, view) {
  const label = `${src}/${phone ? 'phone' : layout}/${theme}/${view}`;
  const ls = {}; if (layout === 'split') ls['lr-layout'] = 'split'; if (view === 'time') ls['lr-view'] = 'time';
  const viewport = phone ? { width: 390, height: 844 } : { width: 1280, height: 800 };
  const h = await newPage(browser, origin, { viewport, scheme: theme, ls, dsf: phone ? 2 : 1, touch: phone }, label);
  const url = origin + '/v0/resource/plugins/live-routes/live' + (src === 'demo' ? '?demo=1' : '');
  await openLive(h, url);
  let sawInflight = false;
  const t0 = Date.now();
  while (Date.now() - t0 < 5600) {
    const s = await h.page.evaluate(() => ({ r: window.__lr.records, i: window.__lr.inflight }));
    if (s.i >= 1) sawInflight = true; await sleep(250);
  }
  const s = await h.page.evaluate(() => ({ r: window.__lr.records, i: window.__lr.inflight, n: window.__lr.nodes, lay: window.__lr.layout }));
  check(s.r > 0, `${label}: records loaded (${s.r}), nodes ${s.n}, layout ${s.lay}`);
  if (src === 'mock') check(sawInflight || s.i >= 1, `${label}: queued/routed record rendered (inflight ${s.i})`);
  if (src === 'demo') check(!h.reqs.some(r => r.startsWith('/v0/management/live-routes/')), `${label}: demo makes no api calls`);
  if (layout === 'split' && !phone) check(s.lay === 'split', `${label}: split layout active`);
  if (phone) {
    const sw = await h.page.evaluate(() => ({ sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth, bw: document.body.scrollWidth }));
    check(sw.sw <= 390 && sw.bw <= 390, `${label}: no horizontal scroll (scrollWidth ${sw.sw})`);
    check(s.lay === 'tabs', `${label}: phone forced to tabs`);
  }
  await storageAudit(h, label);
  const file = `${src}-${phone ? 'phone' : layout}-${theme}-${view}.png`;
  await h.page.screenshot({ path: path.join(OUT, file), fullPage: true });
  endChecks(h, label);
  await h.ctx.close();
  return file;
}

async function authKey(browser) {
  const origin = await startMock(18418, 'key');
  const h = await newPage(browser, origin, { viewport: { width: 1000, height: 700 } }, 'auth=key');
  await h.page.goto(origin + '/v0/resource/plugins/live-routes/live');
  await h.page.waitForSelector('#gKeyBox:not([hidden])', { timeout: 5000 });
  check(await h.page.getAttribute('#gKey', 'autocomplete') === 'off' && await h.page.getAttribute('#gKey', 'type') === 'password', 'auth=key: key form shown (password input, autocomplete=off)');
  await h.page.screenshot({ path: path.join(OUT, 'auth-key-form.png') });
  await h.page.fill('#gKey', 'wrong-key'); await h.page.click('#gGo');
  await h.page.waitForFunction(() => document.querySelector('#gErr').textContent.length > 0, null, { timeout: 4000 });
  check(true, 'auth=key: wrong key shows error and form stays');
  await h.page.fill('#gKey', 'test-key'); await h.page.click('#gGo');
  await h.page.waitForFunction(() => window.__lr.records > 0, null, { timeout: 8000 });
  check(true, 'auth=key: valid key -> data loads');
  check(await h.page.inputValue('#gKey').catch(() => '') === '', 'auth=key: key input cleared');
  await sleep(2500);
  const a = await h.page.evaluate(() => {
    const dump = []; for (let i = 0; i < localStorage.length; i++) dump.push(localStorage.key(i) + '=' + localStorage.getItem(localStorage.key(i)));
    return { dump: dump.join(';'), href: location.href, cookie: document.cookie, ss: Object.keys(sessionStorage), tabKey: sessionStorage.getItem('lr-key'), html: document.documentElement.outerHTML.includes('test-key') };
  });
  check(!a.dump.includes('test-key') && !a.href.includes('test-key') && !a.cookie.includes('test-key'), 'auth=key: without "remember" the key is not in localStorage/URL/cookie');
  check(a.ss.join() === 'lr-key' && a.tabKey === 'test-key', 'auth=key: key kept only in this tab\'s sessionStorage');
  check(!a.html, 'auth=key: key string not left in the DOM');
  await storageAudit(h, 'auth=key');
  const noTokenInUrl = h.reqs.every(r => !r.includes('tok-') && !r.includes('test-key'));
  check(noTokenInUrl, 'auth=key: token/key not sent in any URL');
  await h.page.screenshot({ path: path.join(OUT, 'auth-key-after.png') });
  await h.page.click('#segRange button[data-r="600000"]');
  await h.page.click('#segView button[data-v="time"]');
  await sleep(300);
  let heldHistory;
  await h.page.route('**/v0/management/live-routes/records?*', route => { heldHistory = route; });
  const historyBox = await h.page.locator('#cvT').boundingBox();
  await h.page.mouse.move(historyBox.x + historyBox.width / 2, historyBox.y + historyBox.height / 2);
  for (let i = 0; i < 12; i++) { await h.page.mouse.wheel(0, -400); await sleep(60); }
  await sleep(1200);
  check(!!heldHistory, 'auth=key: history request held while polling continues');
  check(['snapshot', 'events', 'records'].every(name => h.dataReqs.some(r => r.path.endsWith('/' + name) && r.auth === 'Bearer test-key')), 'auth=key: snapshot/events/history all use management Bearer key');
  check(h.dataReqs.filter(r => r.auth !== 'Bearer wrong-key').every(r => r.auth === 'Bearer test-key'), 'auth=key: successful requests keep using the supplied management key');
  let revoked = false;
  await h.page.route('**/v0/management/live-routes/events?*', route => {
    revoked = true; return route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"invalid management key"}' });
  });
  await h.page.waitForSelector('#gKeyBox:not([hidden])', { timeout: 10000 });
  check(revoked && await h.page.inputValue('#gKey') === '', 'auth=key: a later 401 clears key and asks for reauthentication');
  check(await h.page.evaluate(() => sessionStorage.getItem('lr-key') === null && localStorage.getItem('lr-key') === null), 'auth=key: a later 401 drops the cached key');
  const pausedAt = h.dataReqs.length; await sleep(1400);
  check(h.dataReqs.length === pausedAt, 'auth=key: polling stops until key is reentered');
  await h.page.unroute('**/v0/management/live-routes/events?*');
  const newSnapshot = h.page.waitForResponse(r => r.url().endsWith('/snapshot') && r.status() === 200);
  await h.page.fill('#gKey', 'test-key'); await h.page.click('#gGo');
  await newSnapshot;
  if (heldHistory) await heldHistory.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"expired old key"}' });
  await sleep(400);
  check(await h.page.isHidden('#gate'), 'auth=key: delayed 401 from old history request cannot clear the new login');
  const beforeReload = h.dataReqs.length;
  await h.page.reload();
  await h.page.waitForFunction(() => window.__lr.records > 0, null, { timeout: 8000 });
  check(await h.page.isHidden('#gate') && h.dataReqs.slice(beforeReload).every(r => r.auth === 'Bearer test-key'), 'auth=key: reload reuses the tab key without the form');
  const other = await h.ctx.newPage(); await other.goto(origin + '/v0/resource/plugins/live-routes/live');
  await other.waitForSelector('#gKeyBox:not([hidden])', { timeout: 5000 });
  check(true, 'auth=key: without "remember" a new tab asks for the key');
  await other.close();
  await h.page.click('#bOut');
  await h.page.waitForSelector('#gKeyBox:not([hidden])');
  const afterOut = h.dataReqs.length; await sleep(1400);
  check(h.dataReqs.length === afterOut && await h.page.evaluate(() => sessionStorage.getItem('lr-key') === null), 'auth=key: logout clears the key and stops polling');
  check(await h.page.evaluate(() => window.__lr.records === 0 && window.__lr.inflight === 0), 'auth=key: logout drops the loaded records');
  let heldSnap; await h.page.route('**/v0/management/live-routes/snapshot', route => { heldSnap = route; });
  await h.page.fill('#gKey', 'any-key'); await h.page.click('#gGo');
  await h.page.waitForFunction(() => document.querySelector('#gate').hidden, null, { timeout: 4000 }); await sleep(600);
  const pend = await h.page.evaluate(() => ({ recs: window.__lr.records, log: document.querySelector('#list').children.length }));
  check(!!heldSnap && pend.recs === 0 && pend.log === 0, 'auth=key: after logout, old records stay hidden while a new key is being checked');
  await h.page.unroute('**/v0/management/live-routes/snapshot');
  await heldSnap.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"invalid management key"}' });
  await h.page.waitForSelector('#gKeyBox:not([hidden])');
  await h.page.fill('#gKey', 'test-key'); await h.page.check('#gRemember'); await h.page.click('#gGo');
  await h.page.waitForFunction(() => window.__lr.records > 0, null, { timeout: 8000 });
  check(await h.page.evaluate(() => localStorage.getItem('lr-key') === 'test-key'), 'auth=key: "remember" stores the key in localStorage');
  const tab2 = await h.ctx.newPage(); await tab2.goto(origin + '/v0/resource/plugins/live-routes/live');
  await tab2.waitForFunction(() => window.__lr.records > 0, null, { timeout: 8000 });
  check(await tab2.isHidden('#gate'), 'auth=key: a remembered key opens new tabs without the form');
  await tab2.route('**/v0/management/live-routes/events?*', route => route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"invalid management key"}' }));
  await tab2.waitForSelector('#gKeyBox:not([hidden])', { timeout: 10000 });
  check(await tab2.evaluate(() => localStorage.getItem('lr-key') === null), 'auth=key: a 401 drops the remembered key');
  await tab2.close();

  h.errs = h.errs.filter(e => !e.includes('status of 401')); // the browser logs the expected 401 responses of the auth flow
  endChecks(h, 'auth=key');
  await h.ctx.close();
}
async function authForbidden(browser) {
  const origin = await startMock(18419, 'forbidden');
  const h = await newPage(browser, origin, { viewport: { width: 1000, height: 600 } }, 'auth=forbidden');
  await openLive(h, origin + '/v0/resource/plugins/live-routes/live');
  await h.page.waitForSelector('#gForb:not([hidden])', { timeout: 5000 });
  const t = await h.page.textContent('#gForb');
  check(t.includes('CPA 管理接口拒绝访问') && t.includes('远程管理访问设置'), 'auth=forbidden: notice explains management access settings');
  check(t.includes('请先在 CPA 配置管理密钥') && t.includes('remote-management.secret-key') && t.includes('MANAGEMENT_PASSWORD') && t.includes('本机访问也需要配置'), 'auth=forbidden: notice explains management key prerequisite');
  check(t.includes('失败次数过多被暂时封禁') && t.includes('5 次') && t.includes('30 分钟'), 'auth=forbidden: notice explains temporary authentication ban');
  await h.page.screenshot({ path: path.join(OUT, 'auth-forbidden.png') });
  const requestsBeforeRetry = h.dataReqs.length;
  await h.page.click('#gRetry');
  await h.page.waitForSelector('#gKeyBox:not([hidden])');
  check(await h.page.isHidden('#gForb') && await h.page.inputValue('#gKey') === '', 'auth=forbidden: retry opens an empty key form');
  await sleep(1200);
  check(h.dataReqs.length === requestsBeforeRetry, 'auth=forbidden: retry waits for explicit key submission');
  await h.page.fill('#gKey', 'test-key'); await h.page.click('#gGo');
  await h.page.waitForSelector('#gForb:not([hidden])');
  check(h.dataReqs.length > requestsBeforeRetry, 'auth=forbidden: key submission retries management access');
  // 403 errors from fetch are expected as network responses but must not surface as page errors
  check(h.errs.filter(e => !e.includes('403')).length === 0, 'auth=forbidden: no page errors');
  check(h.badReq.length === 0, 'auth=forbidden: only mock-origin requests');
  await h.ctx.close();
}


// samples every visible link and counts points that fall inside a node the link does not connect
function linksUnderNodes(g) {
  const at = (s, u) => { const m = 1 - u; return { x: m * m * m * s[0].x + 3 * m * m * u * s[1].x + 3 * m * u * u * s[2].x + u * u * u * s[3].x, y: m * m * m * s[0].y + 3 * m * m * u * s[1].y + 3 * m * u * u * s[2].y + u * u * u * s[3].y }; };
  const hits = [];
  for (const e of g.edges) for (const n of g.nodes) {
    if (n.k === e.a || n.k === e.b) continue;
    const inside = e.segs.some(s => { for (let i = 0; i <= 40; i++) { const p = at(s, i / 40); if (p.x > n.x + 2 && p.x < n.x + n.w - 2 && p.y > n.y + 2 && p.y < n.y + n.h - 2) return true; } return false; });
    if (inside) hits.push(e.a + '>' + e.b + ' under ' + n.k);
  }
  return hits;
}

async function expandedLinkChecks(browser, origin) {
  for (const [name, width] of [['desk', 1280], ['mid', 900]]) {
    const label = 'expanded-links/' + name;
    const h = await newPage(browser, origin, { viewport: { width, height: 800 } }, label);
    await h.page.goto(origin + '/v0/resource/plugins/live-routes/live?demo=1');
    await h.page.waitForFunction(() => window.__lr.nodes > 5, null, { timeout: 8000 });
    await sleep(800);
    // clients with open sessions are expanded by default; click any that is still collapsed, one at a time (positions move after each click)
    for (let i = 0; i < 6; i++) {
      const pts = await h.page.evaluate(() => { const r = document.querySelector('#cvF').getBoundingClientRect(), ex = window.__lr.expanded; return window.__lr.geom.nodes.filter(n => n.k[0] === 'c').map(n => ({ x: r.left + n.x + n.w / 2, y: r.top + n.y + n.h / 2, ex: ex.includes(+n.k.slice(1)) })); });
      if (i >= pts.length) break;
      if (!pts[i].ex) { await h.page.mouse.click(pts[i].x, pts[i].y); await sleep(700); }
    }
    await h.page.mouse.move(2, 2); await sleep(1500);
    const g = await h.page.evaluate(() => window.__lr.geom), hits = linksUnderNodes(g);
    check(g.nodes.some(n => n.k[0] === 's') && hits.length === 0, `${label}: no link runs under an unrelated node (${g.nodes.filter(n => n.k[0] === 's').length} sessions${hits.length ? '; ' + hits.slice(0, 4).join(', ') : ''})`);
    await h.page.locator('#cvF').screenshot({ path: path.join(OUT, `expanded-${name}-flow.png`) });
    endChecks(h, label);
    await h.ctx.close();
  }
}

async function busyChecks(browser, origin) {
  const cases = [['desk-light', { width: 1280, height: 800 }, 'light', {}, 1, false], ['desk-dark', { width: 1280, height: 800 }, 'dark', {}, 1, false], ['desk-split-light', { width: 1280, height: 800 }, 'light', { 'lr-layout': 'split' }, 1, false]];
  for (const [name, viewport, scheme, ls, dsf, phone] of cases) {
    const label = 'busy-one-exit/' + name;
    const h = await newPage(browser, origin, { viewport, scheme, ls, dsf, touch: phone }, label);
    await openLive(h, origin + '/v0/resource/plugins/live-routes/live');
    await h.page.waitForFunction(() => window.__lr.records > 20 && window.__lr.nodes > 3, null, { timeout: 10000 });
    await sleep(6000);
    const g = await h.page.evaluate(() => window.__lr.geom);
    const box = n => ({ l: n.x - n.gm, t: n.y - n.gm, r: n.x + n.w + n.gm, b: n.y + n.h + n.gm });
    const bs = g.nodes.filter(n => n.k[0] === 'c').map(box);
    check(bs.length >= 2 && bs.every(b => b.l >= 0 && b.t >= 0 && b.r <= g.W && b.b <= g.FH), `${label}: client outlines inside flow card (${bs.length} clients)`);
    let ov = false; for (let i = 0; i < bs.length; i++) for (let j = i + 1; j < bs.length; j++) { const a = bs[i], b = bs[j]; if (a.l < b.r && b.l < a.r && a.t < b.b && b.t < a.b) ov = true; }
    check(!ov, `${label}: client outlines do not intersect`);
    await h.page.locator('#cvF').screenshot({ path: path.join(OUT, `busy-${name}-flow.png`) });
    await h.page.screenshot({ path: path.join(OUT, `busy-${name}-page.png`), fullPage: true });
    endChecks(h, label);
    await h.ctx.close();
  }
}

async function idleChecks(browser, origin, quietOrigin) {
  // (a) demo + prefers-reduced-motion: rAF must not run at all while data keeps arriving
  let h = await newPage(browser, origin, { viewport: { width: 1280, height: 800 }, reduced: true }, 'idle/demo-reduced');
  await h.page.goto(origin + '/v0/resource/plugins/live-routes/live?demo=1&quiet=4');
  await sleep(9000); // demo generation stops after 4 s, in-flight requests complete within ~20 s max; reduced mode never uses rAF anyway
  const r0 = await h.page.evaluate(() => window.__rafN); await sleep(2000); const r1 = await h.page.evaluate(() => window.__rafN);
  check(r1 - r0 <= 1 && r1 <= 2, `idle/demo-reduced: rAF callbacks over 2 s = ${r1 - r0} (total ${r1})`);
  await h.ctx.close();
  // (b) live, normal motion: after the mock goes quiet and all requests finish, rAF stops
  h = await newPage(browser, quietOrigin, { viewport: { width: 1280, height: 800 } }, 'idle/live-quiet');
  await openLive(h, quietOrigin + '/v0/resource/plugins/live-routes/live');
  await h.page.waitForFunction(() => window.__lr.records > 0, null, { timeout: 8000 });
  await sleep(3000);
  const busy0 = await h.page.evaluate(() => window.__rafN); await sleep(1000); const busy1 = await h.page.evaluate(() => window.__rafN);
  check(busy1 - busy0 > 10, `idle/live: rAF runs while requests are in flight (${busy1 - busy0}/s)`);
  await fetch(quietOrigin + '/__quiet?on=1');
  await h.page.waitForFunction(() => window.__lr.inflight === 0 && window.__lr.flights === 0, null, { timeout: 30000, polling: 500 });
  await sleep(2500);
  const a0 = await h.page.evaluate(() => window.__rafN); await sleep(2000); const a1 = await h.page.evaluate(() => window.__rafN);
  check(a1 - a0 <= 1, `idle/live: rAF callbacks over 2 s with nothing animating = ${a1 - a0}`);
  endChecks(h, 'idle/live-quiet');
  await h.ctx.close();
}

async function interactions(browser, origin) {
  const h = await newPage(browser, origin, { viewport: { width: 1280, height: 800 } }, 'interact');
  await openLive(h, origin + '/v0/resource/plugins/live-routes/live');
  await h.page.waitForFunction(() => window.__lr.records > 0 && window.__lr.nodes > 0, null, { timeout: 8000 });
  await sleep(800);
  // open sessions show without a click; clicking a client collapses it, clicking again expands it
  const before = await h.page.evaluate(() => window.__lr.sessions);
  let fc = await h.page.evaluate(() => window.__lr.firstClient);
  await h.page.mouse.click(fc.x, fc.y);
  await sleep(900);
  const mid = await h.page.evaluate(() => window.__lr.sessions);
  fc = await h.page.evaluate(() => window.__lr.firstClient);
  await h.page.mouse.click(fc.x, fc.y);
  await sleep(500);
  const after = await h.page.evaluate(() => window.__lr.sessions);
  check(before > 0 && mid < before && after > mid, `interact: sessions shown by default, click client collapses / expands (${before} -> ${mid} -> ${after})`);
  await sleep(1500);
  const eh = linksUnderNodes(await h.page.evaluate(() => window.__lr.geom));
  check(eh.length === 0, `interact: expanded links clear of unrelated nodes${eh.length ? ' (' + eh.slice(0, 4).join(', ') + ')' : ''}`);
  await h.page.screenshot({ path: path.join(OUT, 'live-tabs-light-expanded.png') });
  await h.page.click('#segRange button[data-r="600000"]');
  await h.page.click('#segView button[data-v="time"]');
  await sleep(500);
  const box = await h.page.locator('#cvT').boundingBox();
  await h.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  for (let i = 0; i < 12; i++) { await h.page.mouse.wheel(0, -400); await sleep(60); }
  await sleep(1200);
  check(await h.page.evaluate(() => window.__lr.panned), 'interact: wheel pans back, panned state active');
  check(await h.page.isVisible('#bNow'), 'interact: "回到现在" button visible after panning');
  check(h.reqs.some(r => r.includes('/v0/management/live-routes/records?before=') && r.includes('limit=2000')), 'interact: older records fetched (management/live-routes/records?before=<cursor>&limit=2000)');
  await h.page.screenshot({ path: path.join(OUT, 'live-tabs-light-panned.png') });
  await h.page.click('#bNow'); await sleep(300);
  check(!(await h.page.evaluate(() => window.__lr.panned)), 'interact: back to now');
  const ls = await h.page.evaluate(() => JSON.stringify(Object.keys(localStorage).sort()));
  check(JSON.parse(ls).every(k => ALLOWED.has(k)), 'interact: localStorage keys only ' + ls);
  await h.page.click('#bPause'); await sleep(300);
  check((await h.page.textContent('#bPause')) === '继续', 'interact: pause toggles');
  endChecks(h, 'interact');
  await h.ctx.close();
}

async function evictAndReset(browser, origin) {
  const ctl = async p => (await fetch(origin + p)).json();
  const h = await newPage(browser, origin, { viewport: { width: 1280, height: 800 } }, 'reset');
  await openLive(h, origin + '/v0/resource/plugins/live-routes/live');
  await h.page.waitForFunction(() => window.__lr.records > 20 && window.__lr.nodes > 0 && document.querySelectorAll('#list li').length > 0, null, { timeout: 10000 });
  await ctl('/__quiet?on=1');
  await sleep(2500); // let the page take the last upserts
  const loaded = await h.page.evaluate(() => window.__lr.records), total = (await ctl('/__stats')).records;
  // evict: the server drops its oldest records by arrival; the 5 oldest the page holds must disappear, nothing else
  await ctl('/__evict?n=' + (total - loaded + 5));
  await h.page.waitForFunction(n => window.__lr.records === n, loaded - 5, { timeout: 8000 }).catch(() => { });
  check(await h.page.evaluate(() => window.__lr.records) === loaded - 5, `reset: evict event removes exactly the named records (${loaded} -> ${loaded - 5})`);
  // reset: new epoch, empty snapshot
  await ctl('/__reset');
  await h.page.waitForFunction(() => window.__lr.records === 0, null, { timeout: 12000 }).catch(() => { });
  await sleep(600);
  const s = await h.page.evaluate(() => ({ r: window.__lr.records, rows: document.querySelectorAll('#list li').length, nodes: window.__lr.nodes, feed: document.querySelector('#feedN').textContent, inflight: window.__lr.inflight, flights: window.__lr.flights }));
  check(s.r === 0, `reset: empty snapshot clears records (${s.r})`);
  check(s.rows === 0 && s.feed === '', `reset: recent-requests list is empty (rows ${s.rows}, "${s.feed}")`);
  check(s.nodes === 0 && s.inflight === 0 && s.flights === 0, `reset: flow nodes cleared (nodes ${s.nodes}, inflight ${s.inflight}, flights ${s.flights})`);
  endChecks(h, 'reset');
  await h.ctx.close();
}

const browser = await chromium.launch();
try {
  if (process.argv.includes('--auth-only')) { await authKey(browser); await authForbidden(browser); } else {
  const origin = await startMock(18417, 'key');
  const files = [];
  const jobs = [];
  for (const src of ['mock', 'demo']) {
    for (const theme of ['light', 'dark']) {
      jobs.push(() => scenario(browser, origin, src, 'tabs', theme, false, 'flow'));
      jobs.push(() => scenario(browser, origin, src, 'tabs', theme, false, 'time'));
      jobs.push(() => scenario(browser, origin, src, 'split', theme, false, 'flow'));
    }
    jobs.push(() => scenario(browser, origin, src, 'tabs', 'light', true, 'flow'));
    jobs.push(() => scenario(browser, origin, src, 'tabs', 'light', true, 'time'));
  }
  for (let i = 0; i < jobs.length; i += 4) files.push(...await Promise.all(jobs.slice(i, i + 4).map(j => j())));
  await interactions(browser, origin);
  await expandedLinkChecks(browser, origin);
  await authKey(browser);
  await authForbidden(browser);
  const quietOrigin = await startMock(18420, 'key');
  await idleChecks(browser, origin, quietOrigin);
  await busyChecks(browser, await startMock(18421, 'key', 'busy-one-exit'));
  await evictAndReset(browser, await startMock(18422, 'key'));
  }
} catch (e) {
  failures.push('exception: ' + (e && e.stack || e)); console.log('FAIL exception', e);
} finally {
  await browser.close(); servers.forEach(p => p.kill());
}
console.log('\nscreenshots:\n' + fs.readdirSync(OUT).filter(f => f.endsWith('.png')).sort().map(f => '  ' + path.join(OUT, f)).join('\n'));
console.log(`\n${passes.length} passed, ${failures.length} failed`);
process.exit(failures.length ? 1 : 0);
