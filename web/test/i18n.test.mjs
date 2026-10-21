#!/usr/bin/env node
// Run with PLAYWRIGHT_MODULE=/path/to/playwright node web/test/i18n.test.mjs.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { createRequire } from 'node:module';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const html = fs.readFileSync(path.join(root, 'web/index.html'));
const csp = fs.readFileSync(path.join(root, 'internal/api/server.go'), 'utf8').match(/pageCSP\s*=\s*"([^"]+)"/)[1];
const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE || path.join(execSync('npm root -g').toString().trim(), 'playwright'));
const server = http.createServer((req, res) => {
  if (req.url.startsWith('/v0/management/')) { res.writeHead(401, { 'Content-Type': 'application/json' }); res.end('{}'); return; }
  if (req.url.split('?')[0] !== '/v0/resource/plugins/live-routes/live') { res.writeHead(404); res.end(); return; }
  res.writeHead(200, { 'Content-Type': 'text/html', 'Content-Security-Policy': csp }); res.end(html);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {}) });
const strings = {
  'en': ['Live Routes', 'Demo mode', 'Resume', 'Retention', 'Management key'],
  'pt-br': ['Rotas ao Vivo', 'Modo demo', 'Continuar', 'Retenção', 'Chave de gerenciamento'],
  'zh-cn': ['实时链路', '演示模式', '继续', '保留时长', '管理密钥']
};
let count = 0;
try {
  for (const initial of Object.keys(strings)) {
    const context = await browser.newContext({ locale: initial, viewport: { width: 1280, height: 900 } });
    await context.addInitScript(() => {
      window.__canvasText = [];
      const fillText = CanvasRenderingContext2D.prototype.fillText;
      CanvasRenderingContext2D.prototype.fillText = function (text, ...args) {
        window.__canvasText.push(String(text));
        return fillText.call(this, text, ...args);
      };
    });
    const page = await context.newPage(), errors = [], scripts = [];
    page.on('pageerror', e => errors.push(e.message));
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    page.on('request', r => { if (r.resourceType() === 'script') scripts.push(r.url()); });
    await page.goto(origin + '/v0/resource/plugins/live-routes/live?demo=1');
    await page.waitForFunction(() => window.__lr.records > 0 && document.querySelectorAll('#list li').length > 0);
    assert.equal(await page.locator('h1').textContent(), strings[initial][0]);
    assert.equal((await page.locator('html').getAttribute('lang')).toLowerCase(), initial);
    assert.deepEqual(scripts, [], 'page must be self-contained under the production CSP');
    for (const lang of Object.keys(strings)) {
      const [heading, pill, resume, retention] = strings[lang];
      await page.click(`#segLang button[data-lang="${lang}"]`);
      assert.equal(await page.locator('h1').textContent(), heading);
      assert.equal(await page.locator('#pillT').textContent(), pill);
      assert.equal(await page.locator(`#segLang button[data-lang="${lang}"]`).getAttribute('aria-pressed'), 'true');
      assert.equal(await page.locator('#segLang button[aria-pressed="true"]').count(), 1);
      assert.ok((await page.locator('#cfgg').textContent()).includes(retention));
      assert.ok(Number(await page.locator('#cReq').textContent()) > 0);
      const recentLabel = { en: 'Requests in the last', 'pt-br': 'Requisições nos últimos', 'zh-cn': '近' }[lang];
      assert.ok((await page.locator('#lReq').textContent()).startsWith(recentLabel), 'live request count describes the recent range');
      for (const mode of ['flow', 'time', 'split']) {
        await page.click(`#segLayout button[data-l="${mode === 'split' ? 'split' : 'tabs'}"]`);
        if (mode !== 'split') await page.click(`#segView button[data-v="${mode}"]`);
        await page.evaluate(() => { window.__canvasText = []; });
        await page.waitForTimeout(350);
        const canvasText = await page.evaluate(() => window.__canvasText);
        const expected = mode === 'flow' ? { en: 'models', 'pt-br': 'modelos', 'zh-cn': '个模型' }[lang]
          : { en: 'Follow · now', 'pt-br': 'Seguir · agora', 'zh-cn': '跟随现在' }[lang];
        assert.ok(canvasText.some(text => text.includes(expected)), `${lang}/${mode}: translated canvas text`);
        if (mode !== 'time') {
          const entry = { en: 'Entry', 'pt-br': 'Entrada', 'zh-cn': '入口' }[lang];
          assert.ok(canvasText.some(text => text.startsWith(entry + ' · OAuth / API')), 'credential column describes entry credentials');
        }
        assert.deepEqual(errors, [], `${initial}/${lang}/${mode}: browser errors`);
        count++;
      }
      await page.click('#bPause');
      assert.equal(await page.locator('#bPause').textContent(), resume);
      // Reapplying the language must preserve paused state and its dynamic label.
      await page.click(`#segLang button[data-lang="${lang}"]`);
      assert.equal(await page.locator('#bPause').textContent(), resume);
      assert.notEqual(await page.locator('#pillT').textContent(), pill);
      await page.click('#bPause');
    }
    await page.reload();
    assert.equal(await page.locator('h1').textContent(), strings['zh-cn'][0], 'saved language survives reload');
    await page.goto(origin + '/v0/resource/plugins/live-routes/live');
    await page.waitForSelector('#gKeyBox:not([hidden])');
    for (const lang of Object.keys(strings)) {
      await page.click(`#segLang button[data-lang="${lang}"]`);
      assert.equal(await page.locator('#gKey').getAttribute('placeholder'), strings[lang][4]);
    }
    assert.deepEqual(errors, []);
    await context.close();
  }
  console.log(`PASS i18n: ${count} language/view combinations, CSP, dynamic state, config, auth, and persistence`);
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
