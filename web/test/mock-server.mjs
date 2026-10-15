#!/usr/bin/env node
// Mock of the Live Routes plugin routes. No dependencies.
// usage: node mock-server.mjs [--port=18417] [--auth=none|key|forbidden] [--scenario=busy-one-exit]
// --scenario=busy-one-exit (opt-in): ~4 req/s, 2 clients, 3 rules, a single exit account.
// Control endpoints: GET /__quiet?on=1|0 stops/starts synthesis, GET /__stats.
// Opt-in: GET /__evict?n=K drops the K oldest records (one "evict" event naming their ids);
// GET /__reset starts a new epoch with no records and freezes synthesis (an empty snapshot).
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..', '..');
const arg = (n, d) => { const a = process.argv.find(x => x.startsWith('--' + n + '=')); return a ? a.slice(n.length + 3) : d; };
const PORT = +arg('port', 18417), AUTH = arg('auth', 'none'), SCENARIO = arg('scenario', '');
const PAGE = '/v0/resource/plugins/live-routes/live', API = '/v0/resource/plugins/live-routes/api/';
const fixture = JSON.parse(fs.readFileSync(path.join(root, 'testdata/contract/snapshot.json'), 'utf8'));
const KEY = 'test-key', SESSION_ID = 'tok-' + Math.random().toString(36).slice(2) + Math.random().toString(36).slice(2);

let epoch = Math.random().toString(16).slice(2, 10);
let seq = 0;
const dict = JSON.parse(JSON.stringify(fixture.dict));
dict.clients.push({ id: 3, kind: 'opencode', label: 'OpenCode 0.15', ip: '192.168.1.31', key: 2 });
dict.sessions.push({ id: 4, sid: 'claude:90ac11de-72b0-4c8e-a1f3-55d0c2e4b7a1', short: '90ac11de', client: 1 }, { id: 5, sid: 'opencode:ses_5b2a9', short: 'ses_5b2a', client: 3 });
dict.rules.push({ id: 4, alias: 'sonnet', model: 'claude-sonnet-4-5' });
const config = { ...fixture.config, layout: 'tabs', require_key: AUTH === 'key' };
let ROUTE = { 1: [1, 2], 2: [2], 3: [3], 4: [1, 2, 3] };
let CL = [{ c: 1, s: [1, 2, 4], r: [1, 2, 4] }, { c: 2, s: [3], r: [3] }, { c: 3, s: [5], r: [4, 2] }];
if (SCENARIO === 'busy-one-exit') { ROUTE = { 1: [1], 2: [1], 3: [1] }; CL = [{ c: 1, s: [1, 2, 4], r: [1, 2] }, { c: 2, s: [3], r: [2, 3] }]; }
const SPAWN = SCENARIO === 'busy-one-exit' ? [120, 380] : [600, 1800];

const records = new Map(); // id -> rec (everything)
const ordOf = new Map(); // id -> arrival number: the history cursor "<epoch>:<n>" pages by it, newest first
let ordN = 0, frozen = false;
const ordRec = r => { if (!ordOf.has(r.id)) ordOf.set(r.id, ++ordN); return r; };
const log = []; // {seq, ev}
const pick = a => a[Math.floor(Math.random() * a.length)];
const rnd = (a, b) => a + Math.random() * (b - a);
const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
function emit(ev) { ev.seq = ++seq; log.push(ev); if (log.length > 4000) log.shift(); }
function upsert(rec) { if (frozen) return; records.set(rec.id, ordRec(rec)); emit({ type: 'upsert', rec: { ...rec } }); }

// rebase fixture records to "now" and add history older than the snapshot window
const shift = Date.now() - fixture.now;
let n = 0;
for (const r of SCENARIO === 'busy-one-exit' ? [] : fixture.records) {
  const rr = { ...r, t0: r.t0 + shift }; if (rr.t1) rr.t1 += shift;
  if (rr.state === 'queued' || rr.state === 'routed') continue; // live ones are produced by the synthesizer
  records.set(rr.id, rr);
}
function finalRec(b, t1, forceFail) {
  const o = { id: b.id, trace: b.trace, t0: b.t0, t1, client: b.client, session: b.session, rule: b.rule, exit: b.exit, stream: true, attempts: 1 };
  const r = Math.random();
  if (forceFail || r < .12) Object.assign(o, { state: 'failed', status: 429, lat: t1 - b.t0, attempts: 2, err: 'rate_limit_error: This request would exceed your account\'s rate limit. Please try again later.', retry_after: Math.round(rnd(15, 60)) });
  else if (r < .15) Object.assign(o, { state: 'canceled', status: 499, lat: t1 - b.t0 });
  else { const i = Math.round(rnd(2e3, 9e4)); const out = Math.round(rnd(150, 3500)); Object.assign(o, { state: 'done', status: 200, ttft: Math.round(rnd(300, 2000)), lat: t1 - b.t0, tok: { in: i, out, cache: Math.round(i * .6), total: i + out } }); }
  return o;
}
function newBase(t0) {
  const c = pick(CL), rule = pick(c.r);
  return { id: crypto.randomUUID(), trace: Math.random().toString(16).slice(2, 10), t0, client: c.c, session: pick(c.s), rule, exit: pick(ROUTE[rule]) };
}
// history: 60 completed records between 12 and 28 min ago (older than the snapshot's 8 min window)
for (let i = 0; i < 60; i++) {
  const t0 = Date.now() - rnd(12, 28) * 60000, b = newBase(t0); records.set(b.id, finalRec(b, t0 + rnd(1500, 12000)));
}
for (let i = 0; i < 40; i++) {
  const t0 = Date.now() - rnd(10, 470) * 1000, b = newBase(t0); records.set(b.id, finalRec(b, t0 + rnd(1500, 12000)));
}
// arrival order of the initial history is T0 order
[...records.values()].sort((a, b) => a.t0 - b.t0).forEach(ordRec);
let quiet = false, started = 0;
function spawn() {
  if (!quiet && !frozen) {
    const b = newBase(Date.now()); started++;
    upsert({ id: b.id, trace: b.trace, t0: b.t0, state: 'queued', client: b.client, session: b.session, rule: b.rule, stream: true });
    setTimeout(() => {
      upsert({ id: b.id, trace: b.trace, t0: b.t0, state: 'routed', client: b.client, session: b.session, rule: b.rule, exit: b.exit, stream: true, attempts: 1 });
      setTimeout(() => upsert(finalRec(b, Date.now())), rnd(1800, 8000));
    }, rnd(400, 900));
  }
  setTimeout(spawn, rnd(SPAWN[0], SPAWN[1]));
}
setTimeout(spawn, 300);
// first request already in flight on startup
{ const b = newBase(Date.now() - 1200); upsert({ id: b.id, trace: b.trace, t0: b.t0, state: 'queued', client: b.client, session: b.session, rule: b.rule, stream: true });
  setTimeout(() => { upsert({ id: b.id, t0: b.t0, state: 'routed', client: b.client, session: b.session, rule: b.rule, exit: b.exit, stream: true, attempts: 1 }); setTimeout(() => upsert(finalRec(b, Date.now())), 6000); }, 600); }

const json = (res, code, obj) => { res.writeHead(code, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(obj)); };
const sorted = () => [...records.values()].sort((a, b) => a.t0 - b.t0);

http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  if (u.pathname === '/__quiet') { quiet = u.searchParams.get('on') === '1'; return json(res, 200, { quiet }); }
  if (u.pathname === '/__evict') {
    const k = +u.searchParams.get('n') || 1, ids = [...records.values()].sort((a, b) => ordOf.get(a.id) - ordOf.get(b.id)).slice(0, k).map(r => r.id);
    for (const id of ids) { records.delete(id); ordOf.delete(id); }
    emit({ type: 'evict', ids }); return json(res, 200, { evicted: ids.length });
  }
  if (u.pathname === '/__reset') {
    frozen = quiet = true; records.clear(); ordOf.clear(); ordN = 0; log.length = 0; seq = 0;
    epoch = Math.random().toString(16).slice(2, 10); return json(res, 200, { epoch });
  }
  if (u.pathname === '/__stats') return json(res, 200, { records: records.size, seq, started });
  if (u.pathname === PAGE) {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store',
      'Content-Security-Policy': "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:" });
    return res.end(fs.readFileSync(path.join(root, 'web/index.html')));
  }
  if (u.pathname === '/v0/management/live-routes/session' && req.method === 'POST') {
    if (req.headers.authorization === 'Bearer ' + KEY) return json(res, 200, { token: SESSION_ID, expires_at: Date.now() + 43200000, ttl_s: 43200 });
    return json(res, 401, { code: 'unauthorized', message: 'bad key' });
  }
  if (u.pathname.startsWith(API)) {
    if (AUTH === 'forbidden') return json(res, 403, { code: 'forbidden_host', message: 'open this page from 127.0.0.1 or localhost, or enable require_key' });
    if (AUTH === 'key' && req.headers['x-live-routes-token'] !== SESSION_ID) return json(res, 401, { code: 'unauthorized', message: 'view token missing or invalid' });
    const what = u.pathname.slice(API.length), now = Date.now();
    if (what === 'snapshot') {
      const all = sorted(), cutoff = now - 8 * 60000;
      const rs = all.filter(r => r.t0 >= cutoff || r.state === 'queued' || r.state === 'routed');
      const done = rs.filter(r => r.state !== 'queued' && r.state !== 'routed'), truncated = rs.length < all.length;
      return json(res, 200, { v: 2, epoch, cursor: epoch + ':' + seq, now, oldest: rs.length ? rs[0].t0 : 0, config, dict,
        records: rs, truncated: truncated || undefined, next: truncated ? epoch + ':' + Math.min(...done.map(r => ordOf.get(r.id))) : undefined, stats: { records: records.size, inflight: all.filter(r => r.state === 'queued' || r.state === 'routed').length, evicted: 0, inflight_dropped: 0 } });
    }
    if (what === 'events') {
      const since = u.searchParams.get('since') || '', [ep, sq] = since.split(':');
      if (ep !== epoch || !(+sq <= seq)) return json(res, 200, { cursor: epoch + ':' + seq, now, reset: true, dict: {}, events: [] });
      const evs = log.filter(e => e.seq > +sq), more = evs.length > 2000;
      const out = evs.slice(0, 2000);
      return json(res, 200, { cursor: epoch + ':' + (out.length ? out[out.length - 1].seq : seq), now, more: more || undefined, dict, events: out });
    }
    if (what === 'records') {
      const bf = u.searchParams.get('before') || '', limit = clamp(+u.searchParams.get('limit') || 500, 1, 2000);
      const [bep, bn] = bf.split(':'), upper = bf ? (bep === epoch ? +bn : 0) : Infinity;
      const rs = [...records.values()].filter(r => ordOf.get(r.id) < upper).sort((a, b) => ordOf.get(b.id) - ordOf.get(a.id)), page = rs.slice(0, limit), more = rs.length > limit;
      return json(res, 200, { records: page, dict, oldest: rs.length ? Math.min(...[...records.values()].map(r => r.t0)) : 0, more: more || undefined, next: more ? epoch + ':' + ordOf.get(page[page.length - 1].id) : undefined });
    }
    return json(res, 404, { code: 'not_found', message: 'no such route' });
  }
  res.writeHead(404); res.end('not found');
}).listen(PORT, '127.0.0.1', () => console.log(`mock listening on http://127.0.0.1:${PORT}${PAGE} (auth=${AUTH})`));
